package signal

import (
	"database/sql"
	"time"
)

// Signal represents an RSS signal item
type Signal struct {
	ID        int64      `json:"id"`
	Source    string     `json:"source"`
	Title     string     `json:"title"`
	URL       string     `json:"url"`
	Content   string     `json:"content"`
	FetchedAt time.Time  `json:"fetched_at"`
	UsedAt    *time.Time `json:"used_at"`
}

// Manager manages signals
type Manager struct {
	db *sql.DB
}

// NewManager creates a new signal manager
func NewManager(db *sql.DB) *Manager {
	return &Manager{db: db}
}

// Get returns a signal by ID
func (m *Manager) Get(id int64) (*Signal, error) {
	var s Signal
	var url, content sql.NullString
	var usedAt sql.NullTime

	err := m.db.QueryRow(`
		SELECT id, source, title, url, content, fetched_at, used_at
		FROM signals WHERE id = ?
	`, id).Scan(&s.ID, &s.Source, &s.Title, &url, &content, &s.FetchedAt, &usedAt)
	if err != nil {
		return nil, err
	}
	if url.Valid {
		s.URL = url.String
	}
	if content.Valid {
		s.Content = content.String
	}
	if usedAt.Valid {
		s.UsedAt = &usedAt.Time
	}
	return &s, nil
}

// MarkUsed marks a signal as used
func (m *Manager) MarkUsed(id int64) error {
	_, err := m.db.Exec(`UPDATE signals SET used_at = ? WHERE id = ?`, time.Now(), id)
	return err
}
