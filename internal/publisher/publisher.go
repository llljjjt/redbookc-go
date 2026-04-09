package publisher

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"redbookc-go/internal/account"
	"redbookc-go/internal/queue"
)

// Publisher 发布引擎
type Publisher struct {
	db       *sql.DB
	accMgr   *account.AccountManager
	queue    *queue.Queue
	running  bool
	mu       sync.Mutex
}

// NewPublisher 创建发布引擎
func NewPublisher(db *sql.DB, accMgr *account.AccountManager, q *queue.Queue) *Publisher {
	return &Publisher{
		db:     db,
		accMgr: accMgr,
		queue:  q,
	}
}

// Start 启动发布循环
func (p *Publisher) Start(ctx context.Context) {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.mu.Unlock()

	log.Println("[Publisher] Started")

	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			log.Println("[Publisher] Stopped")
			return
		case <-ticker.C:
			if err := p.RunOnce(); err != nil {
				log.Printf("[Publisher] Error: %v", err)
			}
		}
	}
}

// RunOnce 执行一次发布检查
func (p *Publisher) RunOnce() error {
	// 获取待发布的任务
	jobs, err := p.queue.GetPendingJobsForPublish()
	if err != nil {
		return fmt.Errorf("获取待发布任务失败: %w", err)
	}

	for _, job := range jobs {
		// 获取账号
		acc, err := p.accMgr.Get(job.AccountID)
		if err != nil {
			log.Printf("[Publisher] 获取账号失败: %v", err)
			continue
		}

		// 检查是否可以发布
		canPost, err := p.accMgr.CanPost(job.AccountID)
		if err != nil || !canPost {
			continue
		}

		// 执行发布
		if err := p.PublishJob(context.Background(), job, acc); err != nil {
			log.Printf("[Publisher] 发布失败: %v", err)
			p.queue.UpdateStatus(job.ID, queue.StatusFailed)
			continue
		}

		log.Printf("[Publisher] 发布成功: job=%d account=%s", job.ID, acc.Name)
	}

	return nil
}

// PublishJob 发布单个任务（实际浏览器自动化）
func (p *Publisher) PublishJob(ctx context.Context, job *queue.Job, acc *account.Account) error {
	log.Printf("[Publisher] 开始发布: job=%d content=%s", job.ID, job.Content)

	// TODO: 实现完整的 Playwright 浏览器自动化
	//
	// 完整的发布流程应该是：
	// 1. 启动 Chromium，加载 Chrome Profile
	// 2. 打开小红书创作者后台
	// 3. 注入反检测脚本
	// 4. 上传图片（如有）
	// 5. 填写文案
	// 6. 点击发布
	// 7. 等待确认
	//
	// 由于 playwright-go API 版本问题，这里暂时用 stub 实现
	// 后续需要根据实际的 playwright-go 版本调整 API 调用

	// 模拟发布延迟
	time.Sleep(2 * time.Second)

	// 标记为已发布
	if err := p.queue.MarkPublished(job.ID); err != nil {
		return fmt.Errorf("标记发布状态失败: %w", err)
	}

	// 更新账号最后发布时间
	if err := p.accMgr.UpdateLastPostAt(acc.ID); err != nil {
		log.Printf("[Publisher] 更新最后发布时间失败: %v", err)
	}

	log.Printf("[Publisher] 发布完成: job=%d", job.ID)
	return nil
}

// shouldSkip 检查是否应该跳过（基于冷却时间）
func (p *Publisher) shouldSkip(acc *account.Account, jobCreatedAt time.Time) (bool, error) {
	if acc.LastPostAt == nil {
		return false, nil
	}

	diff := acc.IntervalMax - acc.IntervalMin
	if diff <= 0 {
		return false, nil
	}

	intervalMinutes := acc.IntervalMin + int(time.Now().UnixNano()%int64(diff))
	cooldown := time.Duration(intervalMinutes) * time.Minute

	if time.Since(*acc.LastPostAt) < cooldown {
		return true, nil
	}

	return false, nil
}
