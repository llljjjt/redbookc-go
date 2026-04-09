package queue

import (
	"database/sql"
	"fmt"
	"time"
)

// Job represents a publishing job in the queue
type Job struct {
	ID           int64      `json:"id"`
	AccountID    int64      `json:"account_id"`
	SignalID     int64      `json:"signal_id"`
	Content      string     `json:"content"`
	ImagePath    string     `json:"image_path"`
	PublishMode  string     `json:"publish_mode"` // "auto" or "review"
	Status       string     `json:"status"`       // pending/generating/generated/approved/published/failed
	PublishAt    *time.Time `json:"publish_at"`
	ApprovedAt   *time.Time `json:"approved_at"`
	PublishedAt  *time.Time `json:"published_at"`
	ErrorMessage string     `json:"error_message"`
	RetryCount   int        `json:"retry_count"`
	CreatedAt    time.Time  `json:"created_at"`
}

// Job status constants
const (
	StatusPending    = "pending"
	StatusGenerating = "generating"
	StatusGenerated  = "generated"
	StatusApproved   = "approved"
	StatusPublished  = "published"
	StatusFailed     = "failed"
)

// Queue manages publishing jobs
type Queue struct {
	db *sql.DB
}

// NewQueue creates a new queue manager
func NewQueue(db *sql.DB) *Queue {
	return &Queue{db: db}
}

// Enqueue adds a new job to the queue
func (q *Queue) Enqueue(job *Job) error {
	result, err := q.db.Exec(`
		INSERT INTO jobs (account_id, signal_id, content, image_path, publish_mode, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, job.AccountID, job.SignalID, job.Content, job.ImagePath, job.PublishMode, StatusPending, time.Now())
	if err != nil {
		return fmt.Errorf("failed to enqueue job: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id: %w", err)
	}
	job.ID = id
	return nil
}

// Dequeue fetches and locks the next pending job for an account (FIFO)
func (q *Queue) Dequeue(accountID int64) (*Job, error) {
	var job Job
	var imagePath sql.NullString
	var publishAt, approvedAt, publishedAt sql.NullTime

	err := q.db.QueryRow(`
		SELECT id, account_id, signal_id, content, image_path, publish_mode, status,
		       publish_at, approved_at, published_at, error_message, retry_count, created_at
		FROM jobs
		WHERE account_id = ? AND status IN (?, ?)
		ORDER BY created_at ASC
		LIMIT 1
	`, accountID, StatusPending, StatusApproved).Scan(
		&job.ID, &job.AccountID, &job.SignalID, &job.Content,
		&imagePath, &job.PublishMode, &job.Status,
		&publishAt, &approvedAt, &publishedAt,
		&job.ErrorMessage, &job.RetryCount, &job.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // no job available
	}
	if err != nil {
		return nil, fmt.Errorf("failed to dequeue job: %w", err)
	}

	if imagePath.Valid {
		job.ImagePath = imagePath.String
	}
	job.PublishAt = nullTimeToPtr(publishAt)
	job.ApprovedAt = nullTimeToPtr(approvedAt)
	job.PublishedAt = nullTimeToPtr(publishedAt)

	return &job, nil
}

// UpdateStatus updates the status of a job
func (q *Queue) UpdateStatus(jobID int64, status string) error {
	result, err := q.db.Exec(`UPDATE jobs SET status = ? WHERE id = ?`, status, jobID)
	if err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("job not found: %d", jobID)
	}
	return nil
}

// UpdateContent updates the content of a job
func (q *Queue) UpdateContent(jobID int64, content string) error {
	result, err := q.db.Exec(`UPDATE jobs SET content = ? WHERE id = ?`, content, jobID)
	if err != nil {
		return fmt.Errorf("failed to update job content: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("job not found: %d", jobID)
	}
	return nil
}

// UpdateStatusWithError updates job status and error message
func (q *Queue) UpdateStatusWithError(jobID int64, status string, errMsg string) error {
	result, err := q.db.Exec(`
		UPDATE jobs SET status = ?, error_message = ? WHERE id = ?
	`, status, errMsg, jobID)
	if err != nil {
		return fmt.Errorf("failed to update job status with error: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("job not found: %d", jobID)
	}
	return nil
}

// MarkPublished marks a job as published
func (q *Queue) MarkPublished(jobID int64) error {
	now := time.Now()
	_, err := q.db.Exec(`
		UPDATE jobs SET status = ?, published_at = ? WHERE id = ?
	`, StatusPublished, now, jobID)
	return err
}

// IncrementRetry increments the retry count for a job
func (q *Queue) IncrementRetry(jobID int64) error {
	_, err := q.db.Exec(`
		UPDATE jobs SET retry_count = retry_count + 1 WHERE id = ?
	`, jobID)
	return err
}

// GetPendingJobs returns all pending jobs for a specific account
func (q *Queue) GetPendingJobs(accountID int64) ([]*Job, error) {
	rows, err := q.db.Query(`
		SELECT id, account_id, signal_id, content, image_path, publish_mode, status,
		       publish_at, approved_at, published_at, error_message, retry_count, created_at
		FROM jobs
		WHERE account_id = ? AND status = ?
		ORDER BY created_at ASC
	`, accountID, StatusPending)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending jobs: %w", err)
	}
	defer rows.Close()

	return scanJobs(rows)
}

// GetJobsForReview returns jobs awaiting manual review
func (q *Queue) GetJobsForReview(accountID int64) ([]*Job, error) {
	rows, err := q.db.Query(`
		SELECT id, account_id, signal_id, content, image_path, publish_mode, status,
		       publish_at, approved_at, published_at, error_message, retry_count, created_at
		FROM jobs
		WHERE account_id = ? AND publish_mode = 'review' AND status IN (?, ?)
		ORDER BY created_at ASC
	`, accountID, StatusPending, StatusGenerated)
	if err != nil {
		return nil, fmt.Errorf("failed to query review jobs: %w", err)
	}
	defer rows.Close()

	return scanJobs(rows)
}

// Approve approves a job for publishing
func (q *Queue) Approve(jobID int64) error {
	now := time.Now()
	result, err := q.db.Exec(`
		UPDATE jobs SET status = ?, approved_at = ? WHERE id = ?
	`, StatusApproved, now, jobID)
	if err != nil {
		return fmt.Errorf("failed to approve job: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("job not found: %d", jobID)
	}
	return nil
}

// GetJobByID retrieves a job by its ID
func (q *Queue) GetJobByID(jobID int64) (*Job, error) {
	var job Job
	var imagePath sql.NullString
	var publishAt, approvedAt, publishedAt sql.NullTime

	err := q.db.QueryRow(`
		SELECT id, account_id, signal_id, content, image_path, publish_mode, status,
		       publish_at, approved_at, published_at, error_message, retry_count, created_at
		FROM jobs
		WHERE id = ?
	`, jobID).Scan(
		&job.ID, &job.AccountID, &job.SignalID, &job.Content,
		&imagePath, &job.PublishMode, &job.Status,
		&publishAt, &approvedAt, &publishedAt,
		&job.ErrorMessage, &job.RetryCount, &job.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("job not found: %d", jobID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	if imagePath.Valid {
		job.ImagePath = imagePath.String
	}
	job.PublishAt = nullTimeToPtr(publishAt)
	job.ApprovedAt = nullTimeToPtr(approvedAt)
	job.PublishedAt = nullTimeToPtr(publishedAt)

	return &job, nil
}

// GetPendingJobsForPublish returns jobs that are ready to be published
// (status = 'generated' and publish_mode = 'auto')
func (q *Queue) GetPendingJobsForPublish() ([]*Job, error) {
	rows, err := q.db.Query(`
		SELECT id, account_id, signal_id, content, image_path, publish_mode, status,
		       publish_at, approved_at, published_at, error_message, retry_count, created_at
		FROM jobs
		WHERE status = ? AND publish_mode = ? AND (publish_at IS NULL OR publish_at <= datetime('now'))
		ORDER BY created_at ASC
		LIMIT 10
	`, StatusGenerated, "auto")
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs for publish: %w", err)
	}
	defer rows.Close()

	return scanJobs(rows)
}

// GetPendingJobsAll returns all pending jobs across all accounts
func (q *Queue) GetPendingJobsAll() ([]*Job, error) {
	rows, err := q.db.Query(`
		SELECT id, account_id, signal_id, content, image_path, publish_mode, status,
		       publish_at, approved_at, published_at, error_message, retry_count, created_at
		FROM jobs
		WHERE status = ?
		ORDER BY created_at ASC
	`, StatusPending)
	if err != nil {
		return nil, fmt.Errorf("failed to query all pending jobs: %w", err)
	}
	defer rows.Close()

	return scanJobs(rows)
}

// scanJobs scans rows into Job slice
func scanJobs(rows *sql.Rows) ([]*Job, error) {
	var jobs []*Job
	for rows.Next() {
		var job Job
		var imagePath sql.NullString
		var publishAt, approvedAt, publishedAt sql.NullTime

		err := rows.Scan(
			&job.ID, &job.AccountID, &job.SignalID, &job.Content,
			&imagePath, &job.PublishMode, &job.Status,
			&publishAt, &approvedAt, &publishedAt,
			&job.ErrorMessage, &job.RetryCount, &job.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}

		if imagePath.Valid {
			job.ImagePath = imagePath.String
		}
		job.PublishAt = nullTimeToPtr(publishAt)
		job.ApprovedAt = nullTimeToPtr(approvedAt)
		job.PublishedAt = nullTimeToPtr(publishedAt)

		jobs = append(jobs, &job)
	}
	return jobs, rows.Err()
}

func nullTimeToPtr(nt sql.NullTime) *time.Time {
	if nt.Valid {
		return &nt.Time
	}
	return nil
}
