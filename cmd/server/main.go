package main

import (
	"context"
	"crypto/subtle"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mattn/go-sqlite3"

	"redbookc-go/internal/account"
	"redbookc-go/internal/engine"
	"redbookc-go/internal/generator"
	"redbookc-go/internal/middleware"
	"redbookc-go/internal/publisher"
	"redbookc-go/internal/queue"
	"redbookc-go/internal/stats"
	"redbookc-go/internal/webhook"
	"redbookc-go/pkg/database"
)

func main() {
	// 1. Resolve data directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal("failed to get user home dir: ", err)
	}
	dbPath := filepath.Join(homeDir, ".redbookc-go", "data.db")

	// 2. Initialize database
	db, err := database.InitDB(dbPath)
	if err != nil {
		log.Fatal("failed to init database: ", err)
	}
	defer database.CloseDB()

	// 3. Initialize modules
	accMgr := account.NewAccountManager(db)
	q := queue.NewQueue(db)
	gen := generator.NewGenerator(db)
	pub := publisher.NewPublisher(db)
	wh := webhook.NewWebhookClient(db)
	eng := engine.NewEngine(db)
	statsMgr := stats.NewStats(db)

	// 4. Start background workers
	ctx := context.Background()
	go eng.Start(ctx)
	go pub.Start(ctx)

	// 5. Setup Gin router
	r := gin.Default()

	// Apply global middleware
	r.Use(middleware.CORSMiddleware())
	r.Use(middleware.SecureHeaders())
	r.Use(middleware.RequestID())

	// Health check (no auth required)
	r.GET("/health", healthCheck)

	// Setup API routes
	setupRoutes(r, accMgr, q, gen, pub, wh, statsMgr)

	// 6. Start server
	port := getEnv("PORT", "8080")
	log.Printf("Starting server on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal("failed to start server: ", err)
	}
}

func setupRoutes(
	r *gin.Engine,
	accMgr *account.AccountManager,
	q *queue.Queue,
	gen *generator.Generator,
	pub *publisher.Publisher,
	wh *webhook.WebhookClient,
	statsMgr *stats.Stats,
) {
	api := r.Group("/api")
	api.Use(middleware.AuthRequired())

	// === Account Management ===
	api.GET("/accounts", func(c *gin.Context) { listAccounts(c, accMgr) })
	api.POST("/accounts", func(c *gin.Context) { createAccount(c, accMgr) })
	api.PUT("/accounts/:id", func(c *gin.Context) { updateAccount(c, accMgr) })
	api.DELETE("/accounts/:id", func(c *gin.Context) { deleteAccount(c, accMgr) })
	api.GET("/accounts/:id", func(c *gin.Context) { getAccount(c, accMgr) })

	// === Job Queue ===
	api.GET("/jobs", func(c *gin.Context) { listJobs(c, q) })
	api.POST("/jobs", func(c *gin.Context) { createJob(c, q, accMgr, wh) })
	api.GET("/jobs/:id", func(c *gin.Context) { getJob(c, q) })
	api.POST("/jobs/:id/approve", func(c *gin.Context) { approveJob(c, q) })
	api.POST("/jobs/:id/reject", func(c *gin.Context) { rejectJob(c, q) })
	api.POST("/jobs/:id/retry", func(c *gin.Context) { retryJob(c, q, accMgr) })
	api.DELETE("/jobs/:id", func(c *gin.Context) { deleteJob(c, q) })

	// === Stats ===
	api.GET("/stats", func(c *gin.Context) { getStats(c, statsMgr) })
	api.GET("/stats/accounts/:id", func(c *gin.Context) { getAccountStats(c, statsMgr) })

	// === Webhook Callback (no auth required - authenticated via secret) ===
	r.POST("/api/webhook/callback", func(c *gin.Context) { webhookCallback(c, wh) })
}

// === Health Check ===

func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// === Account Handlers ===

func listAccounts(c *gin.Context, am *account.AccountManager) {
	userID := getUserID(c)
	accounts, err := am.List(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, accounts)
}

func getAccount(c *gin.Context, am *account.AccountManager) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	acc, err := am.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, acc)
}

func createAccount(c *gin.Context, am *account.AccountManager) {
	var input struct {
		Name              string `json:"name" binding:"required"`
		ProfileDir        string `json:"profile_dir"`
		AccountType       string `json:"account_type"`
		ChromeUserDataDir string `json:"chrome_user_data_dir"`
		CookiesJSON       string `json:"cookies_json"`
		Status            string `json:"status"`
		IntervalMin       int    `json:"interval_min"`
		IntervalMax       int    `json:"interval_max"`
		DailyLimit        int    `json:"daily_limit"`
		ClaudeAPIKey      string `json:"claude_api_key"`
		WebhookURL        string `json:"webhook_url"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	acc := &account.Account{
		UserID:            getUserID(c),
		Name:              input.Name,
		ProfileDir:        input.ProfileDir,
		AccountType:       input.AccountType,
		ChromeUserDataDir: input.ChromeUserDataDir,
		CookiesJSON:       input.CookiesJSON,
		Status:            input.Status,
		IntervalMin:       input.IntervalMin,
		IntervalMax:       input.IntervalMax,
		DailyLimit:        input.DailyLimit,
		ClaudeAPIKey:      input.ClaudeAPIKey,
		WebhookURL:        input.WebhookURL,
	}

	id, err := am.Create(acc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	acc.ID = id
	c.JSON(http.StatusCreated, acc)
}

func updateAccount(c *gin.Context, am *account.AccountManager) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var input struct {
		Name              string `json:"name"`
		ProfileDir        string `json:"profile_dir"`
		AccountType       string `json:"account_type"`
		ChromeUserDataDir string `json:"chrome_user_data_dir"`
		CookiesJSON       string `json:"cookies_json"`
		Status            string `json:"status"`
		IntervalMin       int    `json:"interval_min"`
		IntervalMax       int    `json:"interval_max"`
		DailyLimit        int    `json:"daily_limit"`
		ClaudeAPIKey      string `json:"claude_api_key"`
		WebhookURL        string `json:"webhook_url"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	acc := &account.Account{
		Name:              input.Name,
		ProfileDir:        input.ProfileDir,
		AccountType:       input.AccountType,
		ChromeUserDataDir: input.ChromeUserDataDir,
		CookiesJSON:       input.CookiesJSON,
		Status:            input.Status,
		IntervalMin:       input.IntervalMin,
		IntervalMax:       input.IntervalMax,
		DailyLimit:        input.DailyLimit,
		ClaudeAPIKey:      input.ClaudeAPIKey,
		WebhookURL:        input.WebhookURL,
	}

	if err := am.Update(id, acc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "account updated"})
}

func deleteAccount(c *gin.Context, am *account.AccountManager) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	if err := am.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "account deleted"})
}

// === Job Handlers ===

func listJobs(c *gin.Context, q *queue.Queue) {
	accountIDStr := c.Query("account_id")
	var jobs []*queue.Job
	var err error

	if accountIDStr != "" {
		accountID, parseErr := strconv.ParseInt(accountIDStr, 10, 64)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account_id"})
			return
		}
		jobs, err = q.GetPendingJobs(accountID)
	} else {
		jobs, err = q.GetPendingJobsAll()
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, jobs)
}

func getJob(c *gin.Context, q *queue.Queue) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	job, err := q.GetJobByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, job)
}

func createJob(c *gin.Context, q *queue.Queue, am *account.AccountManager, wh *webhook.WebhookClient) {
	var input struct {
		AccountID   int64  `json:"account_id" binding:"required"`
		Content     string `json:"content"`
		ImagePath   string `json:"image_path"`
		PublishMode string `json:"publish_mode"` // "auto" or "review"
		SignalID    int64  `json:"signal_id"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.PublishMode == "" {
		input.PublishMode = "auto"
	}

	job := &queue.Job{
		AccountID:   input.AccountID,
		Content:     input.Content,
		ImagePath:   input.ImagePath,
		PublishMode: input.PublishMode,
		SignalID:    input.SignalID,
	}

	if err := q.Enqueue(job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// If review mode, send webhook notification
	if input.PublishMode == "review" && wh != nil {
		if err := wh.SendReviewNotification(input.AccountID, job.ID, input.Content); err != nil {
			log.Printf("warning: failed to send review webhook: %v", err)
			// Don't fail the job creation - webhook is best effort
		}
	}

	c.JSON(http.StatusCreated, job)
}

func approveJob(c *gin.Context, q *queue.Queue) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	if err := q.Approve(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "job approved"})
}

func rejectJob(c *gin.Context, q *queue.Queue) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	if err := q.UpdateStatusWithError(id, queue.StatusFailed, "rejected manually"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "job rejected"})
}

func retryJob(c *gin.Context, q *queue.Queue, am *account.AccountManager) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	job, err := q.GetJobByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	if job.Status != queue.StatusFailed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "can only retry failed jobs"})
		return
	}

	// Reset job to pending
	if err := q.UpdateStatus(id, queue.StatusPending); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Increment retry count
	if err := q.IncrementRetry(id); err != nil {
		log.Printf("warning: failed to increment retry count: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{"message": "job queued for retry"})
}

func deleteJob(c *gin.Context, q *queue.Queue) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	if err := q.UpdateStatusWithError(id, queue.StatusFailed, "deleted"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "job deleted"})
}

// === Webhook Callback Handler ===

func webhookCallback(c *gin.Context, wh *webhook.WebhookClient) {
	var req webhook.WebhookCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate webhook secret if configured
	webhookSecret := c.GetHeader("X-Webhook-Secret")
	if webhookSecret != "" {
		// Use constant-time comparison to prevent timing attacks
		expectedSecret := getEnv("WEBHOOK_SECRET", "")
		if subtle.ConstantTimeCompare([]byte(webhookSecret), []byte(expectedSecret)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook secret"})
			return
		}
	}

	if err := wh.HandleCallback(req.JobID, req.Approved); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "callback processed"})
}

// === Stats Handlers ===

func getStats(c *gin.Context, s *stats.Stats) {
	allStats, err := s.GetAllStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, allStats)
}

func getAccountStats(c *gin.Context, s *stats.Stats) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account_id"})
		return
	}

	accountStats, err := s.GetAccountStats(accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, accountStats)
}

// === Helpers ===

func getUserID(c *gin.Context) int64 {
	if v, exists := c.Get("user_id"); exists {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 0
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
