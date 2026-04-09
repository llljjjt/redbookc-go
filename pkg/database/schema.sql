-- 用户表
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    is_admin INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 小红书账号表
CREATE TABLE IF NOT EXISTS accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    profile_dir TEXT NOT NULL,
    account_type TEXT DEFAULT 'brand',
    chrome_user_data_dir TEXT,
    cookies_json TEXT,
    status TEXT DEFAULT 'active',
    interval_min INTEGER DEFAULT 180,
    interval_max INTEGER DEFAULT 480,
    daily_limit INTEGER DEFAULT 5,
    claude_api_key TEXT,
    webhook_url TEXT,
    last_post_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- 信号表
CREATE TABLE IF NOT EXISTS signals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source TEXT NOT NULL,
    title TEXT NOT NULL,
    url TEXT,
    content TEXT,
    fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    used_at DATETIME
);

-- 任务队列表（核心！）
CREATE TABLE IF NOT EXISTS jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL,
    signal_id INTEGER,
    content TEXT NOT NULL,
    image_path TEXT,
    publish_mode TEXT NOT NULL,
    status TEXT DEFAULT 'pending',
    publish_at DATETIME,
    approved_at DATETIME,
    published_at DATETIME,
    error_message TEXT,
    retry_count INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(id),
    FOREIGN KEY (signal_id) REFERENCES signals(id)
);

-- 统计表
CREATE TABLE IF NOT EXISTS daily_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL,
    date DATE NOT NULL,
    posted_count INTEGER DEFAULT 0,
    failed_count INTEGER DEFAULT 0,
    token_used INTEGER DEFAULT 0,
    FOREIGN KEY (account_id) REFERENCES accounts(id),
    UNIQUE(account_id, date)
);

-- Webhook 日志
CREATE TABLE IF NOT EXISTS webhook_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id INTEGER NOT NULL,
    sent_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    response_code INTEGER,
    response_body TEXT,
    FOREIGN KEY (job_id) REFERENCES jobs(id)
);

-- 创建索引以提高查询性能
CREATE INDEX IF NOT EXISTS idx_accounts_user_id ON accounts(user_id);
CREATE INDEX IF NOT EXISTS idx_signals_source ON signals(source);
CREATE INDEX IF NOT EXISTS idx_signals_fetched_at ON signals(fetched_at);
CREATE INDEX IF NOT EXISTS idx_jobs_account_id ON jobs(account_id);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_publish_mode ON jobs(publish_mode);
CREATE INDEX IF NOT EXISTS idx_jobs_publish_at ON jobs(publish_at);
CREATE INDEX IF NOT EXISTS idx_daily_stats_account_date ON daily_stats(account_id, date);
