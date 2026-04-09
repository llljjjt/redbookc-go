package stats

import (
	"database/sql"
	"fmt"
	"time"
)

// Stats holds overall statistics
type Stats struct {
	TotalAccounts    int `json:"total_accounts"`
	ActiveAccounts   int `json:"active_accounts"`
	TotalJobs        int `json:"total_jobs"`
	PendingJobs      int `json:"pending_jobs"`
	PublishedJobs    int `json:"published_jobs"`
	FailedJobs       int `json:"failed_jobs"`
	PublishedToday   int `json:"published_today"`
	PublishedThisWeek int `json:"published_this_week"`
	PublishedThisMonth int `json:"published_this_month"`
}

// AccountStats holds statistics for a specific account
type AccountStats struct {
	AccountID       int64  `json:"account_id"`
	AccountName     string `json:"account_name"`
	TotalJobs       int    `json:"total_jobs"`
	PendingJobs     int    `json:"pending_jobs"`
	PublishedJobs   int    `json:"published_jobs"`
	FailedJobs      int    `json:"failed_jobs"`
	PublishedToday  int    `json:"published_today"`
	SuccessRate     float64 `json:"success_rate"`
	LastPublishedAt *time.Time `json:"last_published_at"`
}

// Stats manages statistics
type StatsManager struct {
	db *sql.DB
}

// NewStats creates a new stats manager
func NewStats(db *sql.DB) *StatsManager {
	return &StatsManager{db: db}
}

// GetAllStats returns overall statistics
func (s *StatsManager) GetAllStats() (*Stats, error) {
	stats := &Stats{}

	// Count accounts
	err := s.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&stats.TotalAccounts)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to count accounts: %w", err)
	}

	err = s.db.QueryRow(`SELECT COUNT(*) FROM accounts WHERE status = 'active'`).Scan(&stats.ActiveAccounts)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to count active accounts: %w", err)
	}

	// Count jobs by status
	err = s.db.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&stats.TotalJobs)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to count jobs: %w", err)
	}

	err = s.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE status = 'pending'`).Scan(&stats.PendingJobs)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to count pending jobs: %w", err)
	}

	err = s.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE status = 'published'`).Scan(&stats.PublishedJobs)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to count published jobs: %w", err)
	}

	err = s.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE status = 'failed'`).Scan(&stats.FailedJobs)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to count failed jobs: %w", err)
	}

	// Today's stats
	today := time.Now().Format("2006-01-02")
	err = s.db.QueryRow(`
		SELECT COUNT(*) FROM jobs
		WHERE status = 'published' AND DATE(published_at) = ?
	`, today).Scan(&stats.PublishedToday)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to count today's published jobs: %w", err)
	}

	// This week's stats (Monday to Sunday)
	now := time.Now()
	weekStart := now.AddDate(0, 0, -int(now.Weekday())+1).Format("2006-01-02")
	err = s.db.QueryRow(`
		SELECT COUNT(*) FROM jobs
		WHERE status = 'published' AND DATE(published_at) >= ?
	`, weekStart).Scan(&stats.PublishedThisWeek)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to count this week's published jobs: %w", err)
	}

	// This month's stats
	monthStart := time.Now().Format("2006-01") + "-01"
	err = s.db.QueryRow(`
		SELECT COUNT(*) FROM jobs
		WHERE status = 'published' AND DATE(published_at) >= ?
	`, monthStart).Scan(&stats.PublishedThisMonth)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to count this month's published jobs: %w", err)
	}

	return stats, nil
}

// GetAccountStats returns statistics for a specific account
func (s *StatsManager) GetAccountStats(accountID int64) (*AccountStats, error) {
	stats := &AccountStats{AccountID: accountID}

	// Get account name
	var accountName sql.NullString
	err := s.db.QueryRow(`SELECT name FROM accounts WHERE id = ?`, accountID).Scan(&accountName)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("account not found: %d", accountID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}
	if accountName.Valid {
		stats.AccountName = accountName.String
	}

	// Count jobs
	err = s.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE account_id = ?`, accountID).Scan(&stats.TotalJobs)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to count jobs: %w", err)
	}

	err = s.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE account_id = ? AND status = 'pending'`, accountID).Scan(&stats.PendingJobs)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to count pending jobs: %w", err)
	}

	err = s.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE account_id = ? AND status = 'published'`, accountID).Scan(&stats.PublishedJobs)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to count published jobs: %w", err)
	}

	err = s.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE account_id = ? AND status = 'failed'`, accountID).Scan(&stats.FailedJobs)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to count failed jobs: %w", err)
	}

	// Today's published
	today := time.Now().Format("2006-01-02")
	err = s.db.QueryRow(`
		SELECT COUNT(*) FROM jobs
		WHERE account_id = ? AND status = 'published' AND DATE(published_at) = ?
	`, accountID, today).Scan(&stats.PublishedToday)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to count today's published jobs: %w", err)
	}

	// Calculate success rate
	if stats.TotalJobs > 0 {
		stats.SuccessRate = float64(stats.PublishedJobs) / float64(stats.TotalJobs) * 100
	}

	// Last published at
	var lastPublishedAt sql.NullTime
	err = s.db.QueryRow(`
		SELECT MAX(published_at) FROM jobs
		WHERE account_id = ? AND status = 'published'
	`, accountID).Scan(&lastPublishedAt)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get last published time: %w", err)
	}
	if lastPublishedAt.Valid {
		stats.LastPublishedAt = &lastPublishedAt.Time
	}

	return stats, nil
}
