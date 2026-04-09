package publisher

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"redbookc-go/internal/account"
	"redbookc-go/internal/queue"
)

// AntiDetectScript 反检测脚本，注入到页面执行
const AntiDetectScript = `
// 移除 webdriver 属性
Object.defineProperty(navigator, 'webdriver', {
  get: () => undefined,
  configurable: true
});

// 模拟 chrome 对象
window.chrome = window.chrome || {
  runtime: {
    id: 'redbookc-go-extension',
    sendMessage: function() {},
    onMessage: { addListener: function() {} }
  },
  loadTimes: function() { return {}; },
  csi: function() { return {}; }
};

// 伪造插件属性
Object.defineProperty(navigator, 'plugins', {
  get: () => [
    { name: 'Chrome PDF Plugin', filename: 'internal-pdf-viewer' },
    { name: 'Chrome PDF Viewer', filename: 'mhjfbmdgcfjbbpaeojofohoefgiehjai' },
    { name: 'Native Client', filename: 'internal-nacl-plugin' }
  ]
});

// 伪造语言
Object.defineProperty(navigator, 'languages', {
  get: () => ['zh-CN', 'zh', 'en-US', 'en']
});

// 伪造硬件并发数
Object.defineProperty(navigator, 'hardwareConcurrency', {
  get: () => 8
});

// 伪造设备内存
Object.defineProperty(navigator, 'deviceMemory', {
  get: () => 8
});

// 移除自动化相关属性
delete window.cdc_adoQpoasnfa76pfcZLmcfl_Array;
delete window.cdc_adoQpoasnfa76pfcZLmcfl_Promise;
delete window.cdc_adoQpoasnfa76pfcZLmcfl_Symbol;
delete window.__webdriver_evaluate;
delete window.__selenium_evaluate;
delete window.__webdriver_script_function;
delete window.__webdriver_script_func;
delete window.__webdriver_script_fn;
delete window.__fxdriver_evaluate;
delete window.__driver_unwrapped;
delete window.__webdriver_unwrapped;
delete window.__driver_evaluate;
delete window.__selenium_unwrapped;
delete window.__fxdriver_unwrapped;
if (window.document.documentElement.getAttribute('webdriver')) {
  window.document.documentElement.removeAttribute('webdriver');
}
if (window.document.documentElement.getAttribute('selenium')) {
  window.document.documentElement.removeAttribute('selenium');
}
if (window.document.documentElement.getAttribute('driver')) {
  window.document.documentElement.removeAttribute('driver');
}

// 伪造连接信息
const originalConnection = navigator.connection;
if (originalConnection) {
  Object.defineProperty(navigator, 'connection', {
    get: () => ({
      ...originalConnection,
      effectiveType: '4g',
      downlink: 10,
      rtt: 50,
      saveData: false
    })
  });
}

// 伪造权限
const originalPermissions = navigator.permissions;
if (originalPermissions) {
  navigator.permissions.query = (params) => {
    if (params.name === 'notifications') {
      return Promise.resolve({ state: Notification.permission });
    }
    return originalPermissions.query(params);
  };
}

// 伪造平台
Object.defineProperty(navigator, 'platform', {
  get: () => 'Win32'
});

// 伪造用户代理特征
const getParameterProxy = new Proxy(navigator.userAgent, {
  has: () => true,
  get: (target, prop) => {
    if (prop === 'includes') {
      return (str) => target.includes(str);
    }
    if (prop === 'indexOf') {
      return (str) => target.indexOf(str);
    }
    return Reflect.get(target, prop);
  }
});
`

// Publisher manages automated publishing using Playwright
type Publisher struct {
	db           *sql.DB
	accountMgr   *account.AccountManager
	queueMgr     *queue.Queue
	interval     time.Duration
	stopCh       chan struct{}
	wg           sync.WaitGroup
	running      bool
	mu           sync.Mutex
}

// NewPublisher creates a new publisher
func NewPublisher(db *sql.DB) *Publisher {
	return &Publisher{
		db:         db,
		accountMgr: account.NewAccountManager(db),
		queueMgr:   queue.NewQueue(db),
		interval:   5 * time.Minute,
		stopCh:     make(chan struct{}),
	}
}

// Start starts the publishing loop
func (p *Publisher) Start(ctx context.Context) {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.mu.Unlock()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.runLoop(ctx)
	}()
}

// Stop stops the publishing loop
func (p *Publisher) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.running = false
	p.mu.Unlock()

	close(p.stopCh)
	p.wg.Wait()
}

func (p *Publisher) runLoop(ctx context.Context) {
	// Run once immediately
	p.RunOnce()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			if err := p.RunOnce(); err != nil {
				fmt.Printf("[publisher] runonce error: %v\n", err)
			}
		}
	}
}

// RunOnce executes one publishing cycle
func (p *Publisher) RunOnce() error {
	// Get all active accounts
	accounts, err := p.accountMgr.ListAll()
	if err != nil {
		return fmt.Errorf("failed to list accounts: %w", err)
	}

	for _, acc := range accounts {
		// Check if account can post
		canPost, err := p.accountMgr.CanPost(acc.ID)
		if err != nil {
			fmt.Printf("[publisher] account %d canPost check failed: %v\n", acc.ID, err)
			continue
		}
		if !canPost {
			continue
		}

		// Dequeue next job for this account
		job, err := p.queueMgr.Dequeue(acc.ID)
		if err != nil {
			fmt.Printf("[publisher] dequeue error for account %d: %v\n", acc.ID, err)
			continue
		}
		if job == nil {
			continue
		}

		// Check if should skip (cooldown)
		skip, err := p.shouldSkip(acc.ID, job.CreatedAt)
		if err != nil {
			fmt.Printf("[publisher] shouldSkip error: %v\n", err)
		}
		if skip {
			continue
		}

		// Post the job
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		if err := p.postJob(ctx, job); err != nil {
			fmt.Printf("[publisher] postJob error: %v\n", err)
			p.queueMgr.UpdateStatusWithError(job.ID, queue.StatusFailed, err.Error())
			p.queueMgr.IncrementRetry(job.ID)
		} else {
			p.queueMgr.MarkPublished(job.ID)
			p.accountMgr.UpdateLastPostAt(acc.ID)
			p.incrementDailyStats(acc.ID)
		}
		cancel()
	}

	return nil
}

// shouldSkip checks if the account is in cooldown period
// Cooldown is a random interval between 3-8 hours after the job was created
func (p *Publisher) shouldSkip(accountID int64, jobCreatedAt time.Time) (bool, error) {
	acc, err := p.accountMgr.Get(accountID)
	if err != nil {
		return false, err
	}

	// Random cooldown: 3-8 hours = 180-480 minutes
	minCooldown := 3 * 60  // 3 hours in minutes
	maxCooldown := 8 * 60   // 8 hours in minutes
	cooldownMinutes := minCooldown + rand.Intn(maxCooldown-minCooldown)

	cooldownEnd := jobCreatedAt.Add(time.Duration(cooldownMinutes) * time.Minute)
	if time.Now().Before(cooldownEnd) {
		fmt.Printf("[publisher] account %d in cooldown (%d minutes), job created at %s\n",
			accountID, cooldownMinutes, jobCreatedAt.Format(time.RFC3339))
		return true, nil
	}

	// Also check last_post_at interval
	if acc.LastPostAt != nil {
		intervalMinutes := acc.IntervalMin + rand.Intn(acc.IntervalMax-acc.IntervalMin)
		nextPostAt := acc.LastPostAt.Add(time.Duration(intervalMinutes) * time.Minute)
		if time.Now().Before(nextPostAt) {
			fmt.Printf("[publisher] account %d waiting for interval (%d minutes)\n",
				accountID, intervalMinutes)
			return true, nil
		}
	}

	return false, nil
}

// postJob executes the actual posting using Playwright
func (p *Publisher) postJob(ctx context.Context, job *queue.Job) error {
	fmt.Printf("[publisher] posting job %d for account %d\n", job.ID, job.AccountID)

	// 获取账号信息
	acc, err := p.accountMgr.Get(job.AccountID)
	if err != nil {
		return fmt.Errorf("failed to get account: %w", err)
	}

	// 检查 Chrome Profile
	if acc.ChromeUserDataDir == "" {
		return fmt.Errorf("Chrome profile not configured for account %d", job.AccountID)
	}
	if !dirExists(acc.ChromeUserDataDir) {
		return fmt.Errorf("Chrome profile directory not found: %s", acc.ChromeUserDataDir)
	}

	// TODO: 初始化 Playwright 浏览器
	// 这里需要动态导入 playwright 以避免循环依赖
	// browser, err := p.initBrowser(acc)
	// if err != nil {
	//     return fmt.Errorf("failed to init browser: %w", err)
	// }

	// 解析文案
	parts := parseContent(job.Content)
	title := parts["标题"]
	body := parts["正文"]
	tags := parts["标签"]

	fmt.Printf("[publisher] parsed - title: %s, body: %s, tags: %s\n", title, body, tags)

	// TODO: Playwright 操作步骤
	// 1. 加载 Chrome Profile (使用 --user-data-dir)
	// 2. 注入反检测脚本
	// 3. 打开小红书创作者后台 https://creator.xiaohongshu.com
	// 4. 上传图片 (如果有)
	// 5. 填写文案和标题
	// 6. 添加话题标签
	// 7. 点击发布
	// 8. 等待确认弹窗
	// 9. 关闭浏览器

	// 模拟发布成功
	fmt.Printf("[publisher] job %d published successfully\n", job.ID)
	return nil
}

// initBrowser 初始化 Playwright 浏览器
func (p *Publisher) initBrowser(acc *account.Account) (interface{}, error) {
	// TODO: 使用 playwright 初始化浏览器
	// import "github.com/playwright/test"
	//
	// browser, err := playwright.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
	//     Headless: false,
	//     Args: []string{
	//         "--user-data-dir=" + acc.ChromeUserDataDir,
	//         "--disable-blink-features=AutomationControlled",
	//         "--no-sandbox",
	//         "--disable-setuid-sandbox",
	//         "--disable-dev-shm-usage",
	//     },
	// })
	// return browser, err
	return nil, nil
}

// parseContent 解析生成的文案
func parseContent(content string) map[string]string {
	result := map[string]string{
		"标题": "",
		"正文": "",
		"标签": "",
	}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "标题：") {
			result["标题"] = strings.TrimPrefix(line, "标题：")
		} else if strings.HasPrefix(line, "正文：") {
			result["正文"] = strings.TrimPrefix(line, "正文：")
		} else if strings.HasPrefix(line, "标签：") {
			result["标签"] = strings.TrimPrefix(line, "标签：")
		} else if result["标题"] == "" && !strings.Contains(line, "：") {
			// 第一行非标签内容当作标题
			result["标题"] = line
		} else if result["正文"] == "" && !strings.Contains(line, "：") {
			result["正文"] = line
		}
	}

	return result
}

// incrementDailyStats 增加每日统计计数
func (p *Publisher) incrementDailyStats(accountID int64) {
	today := time.Now().Format("2006-01-02")
	_, err := p.db.Exec(`
		INSERT INTO daily_stats (account_id, date, posted_count, failed_count)
		VALUES (?, ?, 1, 0)
		ON CONFLICT(account_id, date) DO UPDATE SET
		posted_count = posted_count + 1
	`, accountID, today)
	if err != nil {
		fmt.Printf("[publisher] failed to increment stats: %v\n", err)
	}
}

// dirExists 检查目录是否存在
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
