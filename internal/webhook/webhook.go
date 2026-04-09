package webhook

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WebhookPayload is the payload sent for review notifications
type WebhookPayload struct {
	JobID       int64  `json:"job_id"`
	Content     string `json:"content"`
	ImagePath   string `json:"image_path,omitempty"`
	AccountName string `json:"account_name"`
	ApproveURL  string `json:"approve_url"`
	RejectURL   string `json:"reject_url"`
	CreatedAt   string `json:"created_at"`
}

// WebhookCallbackRequest represents the callback from external approval system
type WebhookCallbackRequest struct {
	JobID    int64  `json:"job_id"`
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

// WebhookClient sends webhook notifications for manual review
type WebhookClient struct {
	db         *sql.DB
	httpClient *http.Client
	baseURL    string // base URL for constructing approve/reject URLs
}

// NewWebhookClient creates a new webhook client
func NewWebhookClient(db *sql.DB) *WebhookClient {
	return &WebhookClient{
		db: db,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: "http://localhost:8080",
	}
}

// SetBaseURL sets the base URL for constructing approve/reject URLs
func (c *WebhookClient) SetBaseURL(url string) {
	c.baseURL = url
}

// SendReviewNotification sends a review notification for a job
func (c *WebhookClient) SendReviewNotification(accountID int64, jobID int64, content string) error {
	// Get account info
	var accountName, webhookURL sql.NullString
	err := c.db.QueryRow(`
		SELECT name, webhook_url FROM accounts WHERE id = ?
	`, accountID).Scan(&accountName, &webhookURL)
	if err != nil {
		return fmt.Errorf("failed to get account for webhook: %w", err)
	}

	if !webhookURL.Valid || webhookURL.String == "" {
		return fmt.Errorf("account %d has no webhook_url configured", accountID)
	}

	// Get job info for image path
	var imagePath string
	var publishMode string
	err = c.db.QueryRow(`
		SELECT COALESCE(image_path, ''), publish_mode FROM jobs WHERE id = ?
	`, jobID).Scan(&imagePath, &publishMode)
	if err != nil {
		return fmt.Errorf("failed to get job for webhook: %w", err)
	}

	// Build approve/reject URLs
	approveURL := fmt.Sprintf("%s/api/jobs/%d/approve", c.baseURL, jobID)
	rejectURL := fmt.Sprintf("%s/api/jobs/%d/reject", c.baseURL, jobID)

	// Build payload
	payload := WebhookPayload{
		JobID:       jobID,
		Content:     content,
		ImagePath:   imagePath,
		AccountName: accountName.String,
		ApproveURL:  approveURL,
		RejectURL:   rejectURL,
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	// Serialize payload
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	// Send POST request
	req, err := http.NewRequest("POST", webhookURL.String, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned non-2xx status: %d", resp.StatusCode)
	}

	return nil
}

// HandleCallback processes the approval/rejection callback
func (c *WebhookClient) HandleCallback(jobID int64, approved bool) error {
	// Get job info
	var accountID int64
	var status string
	err := c.db.QueryRow(`
		SELECT account_id, status FROM jobs WHERE id = ?
	`, jobID).Scan(&accountID, &status)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	if status != StatusPending && status != StatusGenerated {
		return fmt.Errorf("job %d is not in a reviewable state (current: %s)", jobID, status)
	}

	if approved {
		// Approve the job - move to approved status so publisher can pick it up
		_, err = c.db.Exec(`
			UPDATE jobs SET status = ?, approved_at = ? WHERE id = ?
		`, StatusApproved, time.Now(), jobID)
	} else {
		// Reject the job - mark as failed
		_, err = c.db.Exec(`
			UPDATE jobs SET status = ?, error_message = ? WHERE id = ?
		`, StatusFailed, "rejected via webhook callback", jobID)
	}

	if err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}

	return nil
}

// Job status constants (mirrored from queue package)
const (
	StatusPending    = "pending"
	StatusGenerating = "generating"
	StatusGenerated  = "generated"
	StatusApproved    = "approved"
	StatusPublished   = "published"
	StatusFailed      = "failed"
)

// SendWebhook sends a raw webhook request to a given URL
func (c *WebhookClient) SendWebhook(url string, payload interface{}) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}
