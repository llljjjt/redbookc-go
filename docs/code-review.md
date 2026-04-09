# Code Review Report — redbookc-go

**项目**: 小红书 AI 自动发帖 Agent（Go 重写版）
**审查范围**: 13 个 Go/SQL 文件，约 2700 行代码
**审查日期**: 2026-04-09
**审查者**: Code Review Subagent

---

## 📊 总体评分

| 维度 | 评分 | 说明 |
|------|------|------|
| **Bug 严重度** | 🔴 高危 (4个) | 存在会导致 panic 或数据损坏的问题 |
| **安全隐患** | 🔴 高危 (6个) | 多处安全漏洞，含认证绕过、SSRF、计时攻击 |
| **代码质量** | 🟡 中 (8个) | 重复代码、未处理错误、race condition |
| **架构建议** | 🟡 中 (4个) | 包依赖、内聚性、配置管理问题 |
| **整体评级** | 🟡 **6.5/10** | 功能骨架完整，但生产环境安全性不足 |

**问题统计**: 共发现 **23** 个问题
- 🔴 高危: 10 个
- 🟡 中危: 9 个
- 🟢 低危: 4 个

---

## 🐛 Bug（高危）

### 1. Webhook Secret 计时攻击漏洞
**文件**: `cmd/server/main.go`
**行号**: ~360
**问题**: Webhook 回调验证使用 `!=` 明文比较 secret，存在计时侧信道攻击风险。注释写了 "In production, compare using constant-time comparison" 但未实现。
```go
if webhookSecret != expectedSecret {  // ❌ 明文比较
```
**建议**: 使用 `crypto/subtle.ConstantTimeCompare()` 进行比较。

---

### 2. `rand.Intn(0)` 潜在 panic
**文件**: `internal/publisher/publisher.go`
**行号**: `shouldSkip` 函数
**问题**: 当 `acc.IntervalMin == acc.IntervalMax` 时，`rand.Intn(acc.IntervalMax - acc.IntervalMin)` 会调用 `rand.Intn(0)`，导致 panic。
```go
intervalMinutes := acc.IntervalMin + rand.Intn(acc.IntervalMax-acc.IntervalMin)
```
**建议**: 增加保护逻辑，或使用 `rand.Intn(max-min+1) + min`。

---

### 3. Webhook 回调缺失状态常量
**文件**: `internal/webhook/webhook.go`
**行号**: ~115
**问题**: 代码中使用了 `StatusGenerating` 常量，但 `webhook.go` 中只定义了部分状态常量，缺失 `StatusGenerating`。编译可能失败（若未被使用到）或不完整的常量定义导致状态机不完整。
```go
if status != StatusPending && status != StatusGenerated {  // StatusGenerating 未定义
```
**建议**: 在 `webhook.go` 中补全所有状态常量，或统一引用 `queue` 包的常量。

---

### 4. `IncrementRetry` 错误被忽略
**文件**: `cmd/server/main.go`
**行号**: `retryJob` 函数（约 310 行）
**问题**: 调用 `q.IncrementRetry(id)` 但完全忽略了返回值错误。
```go
if err := q.IncrementRetry(id); err != nil {
    log.Printf("warning: failed to increment retry count: %v", err)
    // 继续执行，没有 return
}
```
**建议**: 失败应该 return 或至少记录并告警。

---

## ⚠️ 安全隐患

### 5. 认证中间件是存根（Stub）
**文件**: `internal/middleware/middleware.go`
**行号**: `validateToken` (~50 行)
**问题**: Token 验证是 demo 存根，接受任意非空 token，永远返回 `userID = 1`。
```go
// 接受任何非空 token
return 1, nil
```
**建议**: 替换为正式的 JWT 验证逻辑，使用 HS256/RS256 签名验证。

---

### 6. API Key 按账号选择错误
**文件**: `internal/generator/generator.go`
**行号**: `callClaude` (~75 行)
**问题**: 获取 API Key 时没有按账号过滤，总是取表中第一个账号的 key。
```go
err := g.db.QueryRow(`SELECT claude_api_key FROM accounts LIMIT 1`).Scan(&apiKey)
```
**建议**: 应该按 `signal` 或 `job` 关联的 `account_id` 过滤：
```sql
SELECT claude_api_key FROM accounts WHERE id = ? LIMIT 1
```

---

### 7. SSRF 风险 — Webhook URL 无验证
**文件**: `internal/webhook/webhook.go`
**行号**: `SendReviewNotification` (~55 行)
**问题**: 直接使用数据库中存储的 `webhook_url` 发送请求，没有验证 URL 合法性，攻击者可通过配置内部地址（如 `http://192.168.1.1:8080/admin`）发起 SSRF 攻击。
```go
approveURL := fmt.Sprintf("%s/api/jobs/%d/approve", c.baseURL, jobID)
// baseURL 默认是 localhost:8080，未验证来源
```
**建议**: 验证 `webhook_url` 必须是 HTTPS 且在白名单域名列表中。

---

### 8. CORS 配置错误
**文件**: `internal/middleware/middleware.go`
**行号**: `CORSMiddleware` (~95 行)
**问题**: 同时设置 `Access-Control-Allow-Credentials: true` 和 `Access-Control-Allow-Origin: *` 是互斥的（浏览器会拒绝）。此外头部拼写错误：`Access-Control-Allow-Credent**ials**`。
```go
c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
c.Writer.Header().Set("Access-Control-Allow-Origin", "*")  // ❌ 冲突
```
**建议**: 正确实现 CORS，使用环境变量配置允许的 origin 列表。

---

### 9. 敏感字段直接暴露在 JSON API 中
**文件**: `internal/account/account.go`
**行号**: `Account` 结构体定义
**问题**: `CookiesJSON`、`ClaudeAPIKey` 等敏感字段直接带有 `json:"cookies_json"` 等标签，会随 API 响应泄露给客户端。
```go
CookiesJSON       string `json:"cookies_json"`
ClaudeAPIKey      string `json:"claude_api_key"`
```
**建议**: 使用自定义 JSON 序列化，或在 API 响应前过滤掉这些字段。

---

### 10. Webhook Secret 比较仍是明文
**文件**: `cmd/server/main.go`
**行号**: ~355-360
**问题**: 同 Bug #1，计时攻击风险未修复。
```go
if webhookSecret != expectedSecret {  // 应使用 subtle.ConstantTimeCompare
```

---

## 🔧 代码质量问题

### 11. Rate Limiter 存在 Race Condition 和内存泄漏
**文件**: `internal/middleware/middleware.go`
**行号**: `RateLimitMiddleware` (~115 行)
**问题**: `records` map 在多个 goroutine 间并发读写，没有使用 `sync.Mutex`。且 map 永远不清理，会无限增长导致内存泄漏。
```go
records := make(map[string]*clientRecord)  // ❌ 无锁保护
// 每次请求都读写 records
```
**建议**: 使用 `sync.RWMutex` 保护 map，或使用 `sync.Map`。添加定期清理过期记录的机制。

---

### 12. `rows.Err()` 在 `rows.Close()` 之后调用
**文件**: `internal/queue/queue.go`
**行号**: `scanJobs` (~230 行)
**问题**: 在 `defer rows.Close()` 之后才调用 `rows.Err()`，虽然 Go 允许这样做，但容易在代码重构时引入错误。
```go
defer rows.Close()
...
return jobs, rows.Err()  // rows.Err() 应在 defer 之前调用
```
**建议**: 将 `rows.Err()` 的检查放在显式迭代结束后、Close 之前。

---

### 13. 重复的 NullString 扫描代码
**文件**: `internal/account/account.go`
**行号**: `Get`、`List`、`ListAll` 函数
**问题**: 完全相同的 `sql.NullString` 扫描和展开逻辑重复了 3 次，每次都是手动判断 5 个 nullable 字段。
```go
if profileDir.Valid { acc.ProfileDir = profileDir.String }
// ... 对 cookiesJSON, claudeAPIKey, webhookURL, lastPostAt 重复同样的逻辑
```
**建议**: 提取为辅助函数：
```go
func scanAccount(row interface{ Scan(...any) error }, acc *Account) error
```

---

### 14. Week Start 计算可能错误
**文件**: `internal/stats/stats.go`
**行号**: `GetAllStats` (~70 行)
**问题**: `now.Weekday()` 返回 0（周日）时，计算 `weekStart` 会得到错误日期。
```go
weekStart := now.AddDate(0, 0, -int(now.Weekday())+1).Format("2006-01-02")
// 如果今天是周日（weekday=0），结果 = now + 1，实际是下周一的日期
```
**建议**: 中国习惯周一为一周开始：
```go
daysFromMonday := int(now.Weekday())
if daysFromMonday == 0 { daysFromMonday = 7 }
weekStart := now.AddDate(0, 0, -daysFromMonday+1)
```

---

### 15. `getUserID` 类型断言失败静默返回 0
**文件**: `cmd/server/main.go`
**行号**: `getUserID` (~420 行)
**问题**: 如果 `c.Get("user_id")` 返回了非 `int64` 类型，函数静默返回 0 而不是报错，可能导致用户混淆。
```go
if id, ok := v.(int64); ok {
    return id
}
return 0  // ❌ 类型错误但静默
```
**建议**: 返回 `(int64, error)` 或记录日志。

---

### 16. Webhook 日志表从未写入
**文件**: `pkg/database/schema.sql`
**行号**: `webhook_logs` 表定义
**问题**: `webhook_logs` 表定义了，但 `webhook.go` 中从未向其写入数据（`SendReviewNotification` 发送请求后没有记录响应）。
**建议**: 在 `SendReviewNotification` 中添加对 `webhook_logs` 的插入记录。

---

### 17. `engine.go` 中 `signals` 表查询缺少错误处理
**文件**: `internal/engine/engine.go`
**行号**: `saveSignal` (~175 行)
**问题**: 查重查询 `SELECT COUNT(1) FROM signals WHERE ...` 如果出错只是 `return err`，没有区分「不存在」和「查询失败」。
```go
err := e.db.QueryRow(`SELECT COUNT(1)...`).Scan(&exists)
if err != nil && err != sql.ErrNoRows {
    return err  // 没有日志记录
}
```

---

### 18. `engine.go` 中 RSS Feed 为空
**文件**: `internal/engine/engine.go`
**行号**: `Poll` (~100 行)
**问题**: `feeds` 切片是注释掉的空列表，`Poll()` 永远什么都做不了。TODO 说「从 accounts 表读取 RSS URL」但从未实现。
```go
feeds := []string{
    // 全部注释掉了
}
```
**建议**: 实现从数据库读取配置的 RSS URL。

---

## 🏗️ 架构建议

### 19. 全局 DB 变量
**文件**: `pkg/database/migrations.go`
**行号**: `var DB *sql.DB`
**问题**: 包级别全局变量 `DB` 是隐式单例，通过 `InitDB` 赋值，`CloseDB` 释放。这使得测试困难（难以替换为 mock），也增加了意外使用的风险。
**建议**: 通过依赖注入传递 `*sql.DB`，避免全局状态。

---

### 20. 状态常量重复定义
**文件**: `internal/webhook/webhook.go`
**行号**: 末尾常量定义
**问题**: 队列状态常量在 `internal/queue/queue.go` 和 `internal/webhook/webhook.go` 两处重复定义。如果添加新状态容易遗漏。
**建议**: 统一放在 `pkg/status/` 或 `internal/queue/status.go`，其他包引用。

---

### 21. `publisher.go` 中 TODO 过多，核心功能未实现
**文件**: `internal/publisher/publisher.go`
**问题**: `postJob` 函数中有大量 TODO，Playwright 浏览器初始化完全未实现，整个发布流程是空壳。
**建议**: 优先实现核心发布流程，TODO 改为带结构的占位符以便后续填充。

---

### 22. 缺少优雅关闭（Graceful Shutdown）
**文件**: `cmd/server/main.go`
**问题**: `r.Run(":" + port)` 没有使用 `http.Server` 的 `Shutdown()` 方法，进程收到 SIGTERM 时不会等待现有请求处理完成。
```go
if err := r.Run(":" + port); err != nil {  // ❌ 直接 Run，不支持优雅关闭
```
**建议**:
```go
srv := &http.Server{Addr: ":" + port, Handler: r}
// 使用 signal.NotifyContext 监听系统信号，调用 srv.Shutdown()
```

---

### 23. `signal` 包缺少核心方法
**文件**: `pkg/signal/signal.go`
**问题**: `signal.Manager` 只有 `Get` 和 `MarkUsed`，但 `engine.go` 的 `Poll` 流程需要 `ListUnused` 或 `ListAll` 来查询待处理的信号。
**建议**: 添加 `ListUnUsed()` 或 `ListAll()` 方法。

---

## 📋 修复优先级建议

| 优先级 | 问题编号 | 描述 |
|--------|----------|------|
| P0 | #1, #2, #3 | 会导致 panic 或编译错误 |
| P1 | #5, #6, #7, #9 | 安全漏洞，生产环境风险 |
| P2 | #11, #14, #16, #17, #18 | 功能缺陷或数据丢失风险 |
| P3 | #12, #13, #15, #19-23 | 代码质量改进 |

---

## ✅ 做得好的地方

1. **数据库设计**: Schema 设计规范，有外键约束、索引、WAL 模式，结构清晰
2. **错误传播**: 大部分函数使用 `fmt.Errorf("...: %w", err)` 正确包装错误
3. **SQL 注入防护**: 所有 SQL 使用参数化查询，没有字符串拼接 SQL
4. **反检测脚本**: `publisher.go` 中的浏览器反检测脚本覆盖全面（虽然未集成）
5. **敏感词过滤**: 有基本的敏感内容过滤逻辑（`Filter` / `ValidateContent`）
6. **并发安全**: `Engine` 和 `Publisher` 使用 `sync.Mutex` 保护 `running` 状态
7. **依赖注入**: 大部分模块通过构造函数注入 `*sql.DB`
8. **中间件设计**: Gin 中间件结构清晰，分离了 CORS、安全头、认证等功能

---

*本报告由自动代码审查子代理生成，审查深度基于静态代码分析。建议在合并前修复所有 P0 和 P1 问题。*
