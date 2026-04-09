package engine

import (
	"context"
	"database/sql"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"redbookc-go/pkg/signal"
)

// RSSItem represents a single item in an RSS feed
type RSSItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

// RSSFeed represents an RSS feed
type RSSFeed struct {
	Channel struct {
		Title       string     `xml:"title"`
		Description string     `xml:"description"`
		Items       []RSSItem  `xml:"item"`
	} `xml:"channel"`
}

// SensitiveWordList 敏感词列表
var SensitiveWordList = []string{
	"比特币", "BTC", "ETH", "以太坊", "加密货币", "虚拟货币",
	"区块链", "token", "炒币", "数字货币", "虚拟货币",
	"ICO", "STO", "资金盘", "传销", "菠菜", "博彩",
	"赌博", "赌场", "裸聊", "约炮", "色情", "成人网站",
}

// Engine RSS信号引擎
type Engine struct {
	db        *sql.DB
	client    *http.Client
	interval  time.Duration
	stopCh    chan struct{}
	wg        sync.WaitGroup
	running   bool
	mu        sync.Mutex
	feeds     []string // RSS Feed URL 列表
}

// NewEngine creates a new RSS engine
func NewEngine(db *sql.DB) *Engine {
	e := &Engine{
		db: db,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		interval: 15 * time.Minute,
		stopCh:   make(chan struct{}),
		feeds:    getDefaultFeeds(),
	}
	// 尝试从数据库加载配置的 RSS URL
	if err := e.loadFeedsFromDB(); err != nil {
		fmt.Printf("[engine] loadFeedsFromDB warning: %v\n", err)
	}
	return e
}

// getDefaultFeeds 返回默认的 RSS Feed 列表
func getDefaultFeeds() []string {
	return []string{
		// 可以添加实际的微信公众号 RSS 地址
		// 示例：腾讯科技、极客公园等
		"https://rss.feedsportal.com/c/34798/f/689521/index.rss",
	}
}

// loadFeedsFromDB 从数据库读取配置的 RSS URL
func (e *Engine) loadFeedsFromDB() error {
	rows, err := e.db.Query(`SELECT DISTINCT rss_url FROM accounts WHERE rss_url IS NOT NULL AND rss_url != ''`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err == nil && url != "" {
			e.feeds = append(e.feeds, url)
		}
	}
	return nil
}

// GetSignal returns a signal by ID
func (e *Engine) GetSignal(id int64) (*signal.Signal, error) {
	var s signal.Signal
	var url, content sql.NullString
	var usedAt sql.NullTime

	err := e.db.QueryRow(`
		SELECT id, source, title, url, content, fetched_at, used_at
		FROM signals WHERE id = ?
	`, id).Scan(&s.ID, &s.Source, &s.Title, &url, &content, &s.FetchedAt, &usedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("signal not found: %d", id)
	}
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

// Start starts the RSS polling engine
func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.mu.Unlock()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runLoop(ctx)
	}()
}

// Stop stops the RSS engine
func (e *Engine) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	e.mu.Unlock()

	close(e.stopCh)
	e.wg.Wait()
}

func (e *Engine) runLoop(ctx context.Context) {
	// Run once immediately
	e.Poll()

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			if err := e.Poll(); err != nil {
				fmt.Printf("[engine] poll error: %v\n", err)
			}
		}
	}
}

// Poll fetches RSS feeds and creates signals
func (e *Engine) Poll() error {
	e.mu.Lock()
	feeds := make([]string, len(e.feeds))
	copy(feeds, e.feeds)
	e.mu.Unlock()

	for _, feedURL := range feeds {
		if err := e.FetchWechatRSS(feedURL); err != nil {
			fmt.Printf("[engine] fetch feed error: %v\n", err)
		}
	}

	return nil
}

// FetchWechatRSS fetches and processes a WeChat public account RSS feed
func (e *Engine) FetchWechatRSS(feedURL string) error {
	req, err := http.NewRequest("GET", feedURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml")

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch feed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read body: %w", err)
	}

	var feed RSSFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return fmt.Errorf("failed to parse RSS: %w", err)
	}

	saved := 0
	for i := range feed.Channel.Items {
		item := &feed.Channel.Items[i]
		if e.Filter(item) {
			continue // 跳过敏感内容
		}
		if err := e.saveSignal(item, feedURL); err != nil {
			fmt.Printf("[engine] save signal error: %v\n", err)
			continue
		}
		saved++
	}

	fmt.Printf("[engine] fetched %d items, saved %d signals from %s\n", len(feed.Channel.Items), saved, feedURL)
	return nil
}

// Filter checks if an RSS item should be filtered out (sensitive content)
func (e *Engine) Filter(item *RSSItem) bool {
	text := item.Title + " " + item.Description
	lower := strings.ToLower(text)

	for _, word := range SensitiveWordList {
		if strings.Contains(lower, strings.ToLower(word)) {
			fmt.Printf("[engine] filtered sensitive word: %s in item: %s\n", word, item.Title)
			return true
		}
	}
	return false
}

// saveSignal saves a new signal to the database (dedup by title+url)
func (e *Engine) saveSignal(item *RSSItem, source string) error {
	// 查重：如果已存在同样 title 和 url 的 signal，跳过
	var exists int
	err := e.db.QueryRow(`
		SELECT COUNT(1) FROM signals WHERE title = ? AND (url = ? OR url = '')
	`, item.Title, item.Link).Scan(&exists)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if exists > 0 {
		return nil // 已存在，跳过
	}

	_, err = e.db.Exec(`
		INSERT INTO signals (source, title, url, content, fetched_at)
		VALUES (?, ?, ?, ?, ?)
	`, source, item.Title, item.Link, item.Description, time.Now())

	return err
}
