package account

import (
	"database/sql"
	"fmt"
	"time"
)

// Account represents a RedBook account
type Account struct {
	ID                int64      `json:"id"`
	UserID            int64      `json:"user_id"`
	Name              string     `json:"name"`
	ProfileDir        string     `json:"profile_dir"`
	AccountType       string     `json:"account_type"` // "brand" or "agency"
	ChromeUserDataDir string     `json:"-"`
	CookiesJSON       string     `json:"-"`
	Status            string     `json:"status"`
	IntervalMin       int        `json:"interval_min"` // minimum interval in minutes
	IntervalMax       int        `json:"interval_max"` // maximum interval in minutes
	DailyLimit        int        `json:"daily_limit"`
	ClaudeAPIKey      string     `json:"-"`
	WebhookURL        string     `json:"webhook_url,omitempty"`
	LastPostAt        *time.Time `json:"last_post_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

// AccountManager manages accounts
type AccountManager struct {
	db *sql.DB
}

// NewAccountManager creates a new account manager
func NewAccountManager(db *sql.DB) *AccountManager {
	return &AccountManager{db: db}
}

// Get returns an account by ID
func (am *AccountManager) Get(id int64) (*Account, error) {
	var acc Account
	var profileDir, chromeUserDataDir, cookiesJSON, claudeAPIKey, webhookURL sql.NullString
	var lastPostAt sql.NullTime

	err := am.db.QueryRow(`
		SELECT id, user_id, name, profile_dir, account_type, chrome_user_data_dir,
		       cookies_json, status, interval_min, interval_max, daily_limit,
		       claude_api_key, webhook_url, last_post_at, created_at
		FROM accounts WHERE id = ?
	`, id).Scan(
		&acc.ID, &acc.UserID, &acc.Name, &profileDir, &acc.AccountType,
		&chromeUserDataDir, &cookiesJSON, &acc.Status, &acc.IntervalMin, &acc.IntervalMax,
		&acc.DailyLimit, &claudeAPIKey, &webhookURL, &lastPostAt, &acc.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("account not found: %d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	if profileDir.Valid {
		acc.ProfileDir = profileDir.String
	}
	if chromeUserDataDir.Valid {
		acc.ChromeUserDataDir = chromeUserDataDir.String
	}
	if cookiesJSON.Valid {
		acc.CookiesJSON = cookiesJSON.String
	}
	if claudeAPIKey.Valid {
		acc.ClaudeAPIKey = claudeAPIKey.String
	}
	if webhookURL.Valid {
		acc.WebhookURL = webhookURL.String
	}
	if lastPostAt.Valid {
		acc.LastPostAt = &lastPostAt.Time
	}

	return &acc, nil
}

// List returns all accounts for a user
func (am *AccountManager) List(userID int64) ([]*Account, error) {
	rows, err := am.db.Query(`
		SELECT id, user_id, name, profile_dir, account_type, chrome_user_data_dir,
		       cookies_json, status, interval_min, interval_max, daily_limit,
		       claude_api_key, webhook_url, last_post_at, created_at
		FROM accounts WHERE user_id = ?
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []*Account
	for rows.Next() {
		var acc Account
		var profileDir, chromeUserDataDir, cookiesJSON, claudeAPIKey, webhookURL sql.NullString
		var lastPostAt sql.NullTime

		err := rows.Scan(
			&acc.ID, &acc.UserID, &acc.Name, &profileDir, &acc.AccountType,
			&chromeUserDataDir, &cookiesJSON, &acc.Status, &acc.IntervalMin, &acc.IntervalMax,
			&acc.DailyLimit, &claudeAPIKey, &webhookURL, &lastPostAt, &acc.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan account: %w", err)
		}

		if profileDir.Valid {
			acc.ProfileDir = profileDir.String
		}
		if chromeUserDataDir.Valid {
			acc.ChromeUserDataDir = chromeUserDataDir.String
		}
		if cookiesJSON.Valid {
			acc.CookiesJSON = cookiesJSON.String
		}
		if claudeAPIKey.Valid {
			acc.ClaudeAPIKey = claudeAPIKey.String
		}
		if webhookURL.Valid {
			acc.WebhookURL = webhookURL.String
		}
		if lastPostAt.Valid {
			acc.LastPostAt = &lastPostAt.Time
		}

		accounts = append(accounts, &acc)
	}

	return accounts, rows.Err()
}

// ListAll returns all accounts (no user filter)
func (am *AccountManager) ListAll() ([]*Account, error) {
	rows, err := am.db.Query(`
		SELECT id, user_id, name, profile_dir, account_type, chrome_user_data_dir,
		       cookies_json, status, interval_min, interval_max, daily_limit,
		       claude_api_key, webhook_url, last_post_at, created_at
		FROM accounts WHERE status = 'active'
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list all accounts: %w", err)
	}
	defer rows.Close()

	var accounts []*Account
	for rows.Next() {
		var acc Account
		var profileDir, chromeUserDataDir, cookiesJSON, claudeAPIKey, webhookURL sql.NullString
		var lastPostAt sql.NullTime

		err := rows.Scan(
			&acc.ID, &acc.UserID, &acc.Name, &profileDir, &acc.AccountType,
			&chromeUserDataDir, &cookiesJSON, &acc.Status, &acc.IntervalMin, &acc.IntervalMax,
			&acc.DailyLimit, &claudeAPIKey, &webhookURL, &lastPostAt, &acc.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan account: %w", err)
		}

		if profileDir.Valid {
			acc.ProfileDir = profileDir.String
		}
		if chromeUserDataDir.Valid {
			acc.ChromeUserDataDir = chromeUserDataDir.String
		}
		if cookiesJSON.Valid {
			acc.CookiesJSON = cookiesJSON.String
		}
		if claudeAPIKey.Valid {
			acc.ClaudeAPIKey = claudeAPIKey.String
		}
		if webhookURL.Valid {
			acc.WebhookURL = webhookURL.String
		}
		if lastPostAt.Valid {
			acc.LastPostAt = &lastPostAt.Time
		}

		accounts = append(accounts, &acc)
	}

	return accounts, rows.Err()
}

// Create creates a new account
func (am *AccountManager) Create(acc *Account) (int64, error) {
	result, err := am.db.Exec(`
		INSERT INTO accounts (user_id, name, profile_dir, account_type, chrome_user_data_dir,
		                      cookies_json, status, interval_min, interval_max, daily_limit,
		                      claude_api_key, webhook_url, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, acc.UserID, acc.Name, acc.ProfileDir, acc.AccountType, acc.ChromeUserDataDir,
		acc.CookiesJSON, acc.Status, acc.IntervalMin, acc.IntervalMax, acc.DailyLimit,
		acc.ClaudeAPIKey, acc.WebhookURL, time.Now())
	if err != nil {
		return 0, fmt.Errorf("failed to create account: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert id: %w", err)
	}
	acc.ID = id
	return id, nil
}

// Update updates an existing account
func (am *AccountManager) Update(id int64, acc *Account) error {
	result, err := am.db.Exec(`
		UPDATE accounts SET
			name = ?, profile_dir = ?, account_type = ?, chrome_user_data_dir = ?,
			cookies_json = ?, status = ?, interval_min = ?, interval_max = ?,
			daily_limit = ?, claude_api_key = ?, webhook_url = ?
		WHERE id = ?
	`, acc.Name, acc.ProfileDir, acc.AccountType, acc.ChromeUserDataDir,
		acc.CookiesJSON, acc.Status, acc.IntervalMin, acc.IntervalMax,
		acc.DailyLimit, acc.ClaudeAPIKey, acc.WebhookURL, id)
	if err != nil {
		return fmt.Errorf("failed to update account: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("account not found: %d", id)
	}
	return nil
}

// Delete deletes an account
func (am *AccountManager) Delete(id int64) error {
	result, err := am.db.Exec(`DELETE FROM accounts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete account: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("account not found: %d", id)
	}
	return nil
}

// CanPost checks if an account can post (respects interval and daily limit)
func (am *AccountManager) CanPost(accountID int64) (bool, error) {
	acc, err := am.Get(accountID)
	if err != nil {
		return false, err
	}

	if acc.Status != "active" {
		return false, fmt.Errorf("account is not active")
	}

	now := time.Now()
	today := now.Format("2006-01-02")

	// Check daily limit
	var postedToday int
	err = am.db.QueryRow(`
		SELECT COUNT(*) FROM jobs
		WHERE account_id = ? AND status = 'published'
		AND DATE(published_at) = ?
	`, accountID, today).Scan(&postedToday)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to check daily count: %w", err)
	}
	if postedToday >= acc.DailyLimit {
		return false, fmt.Errorf("daily limit reached (%d/%d)", postedToday, acc.DailyLimit)
	}

	// Check interval (cooldown between posts)
	if acc.LastPostAt != nil {
		intervalMinutes := acc.IntervalMin + (acc.IntervalMax-acc.IntervalMin)/2
		cooldown := time.Duration(intervalMinutes) * time.Minute
		if now.Sub(*acc.LastPostAt) < cooldown {
			return false, fmt.Errorf("account in cooldown period")
		}
	}

	return true, nil
}

// UpdateLastPostAt updates the last post timestamp for an account
func (am *AccountManager) UpdateLastPostAt(accountID int64) error {
	_, err := am.db.Exec(`
		UPDATE accounts SET last_post_at = ? WHERE id = ?
	`, time.Now(), accountID)
	return err
}
