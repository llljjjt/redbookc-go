# RedBookC-Go QA 报告

**项目：** RedBookC-Go  
**QA 日期：** 2026-04-09  
**QA 范围：** cmd/server, internal/publisher, internal/queue, internal/engine, internal/generator, internal/webhook  
**执行方式：** 代码静态审查（非编译运行）

---

## QA 执行摘要

| 类别 | 数量 |
|------|------|
| **总问题数** | **16** |
| 🔴 阻塞问题（Blocker） | 5 |
| 🟠 严重问题（High） | 6 |
| 🟡 中等问题（Medium） | 4 |
| 🟢 轻微问题（Low） | 2 |
| ✅ 通过 | 4 |

---

## 一、冒烟测试结果

### ✅ 通过项

| 模块 | 检查项 | 结论 |
|------|--------|------|
| queue | 状态常量定义 | ✅ StatusPending/StatusApproved/StatusPublished/StatusFailed 定义完整 |
| queue | 事务一致性 | ✅ Dequeue 单条记录，UpdateStatus 有 RowsAffected 检查 |
| webhook | SSRF 防护 | ✅ isValidWebhookURL 覆盖内网 IP 段、黑名单域名、强制 HTTPS |
| webhook | Webhook 回调幂等性 | ✅ HandleCallback 对非待审状态返回明确错误 |

### 🔴 阻塞问题（Blocker）

#### B-1: Playwright 发布流程完全未实现

**文件：** `internal/publisher/publisher.go` → `postJob()`

```go
// TODO: Playwright 操作步骤
// 1. 加载 Chrome Profile (使用 --user-data-dir)
// 2. 注入反检测脚本
// 3. 打开小红书创作者后台
// ...
// 模拟发布成功  ← 直接返回成功！
fmt.Printf("[publisher] job %d published successfully\n", job.ID)
return nil
```

**影响：** 发布流程所有 Playwright 操作都是注释和 TODO，调用 `postJob` 永远返回成功，无法真正发帖。

**严重程度：** 🔴 阻塞  
**建议：** 必须实现完整的 Playwright 发布流程（登录→上传图片→填文案→发布→确认）。

---

#### B-2: Generator 永远使用 fallback，Claude API 从未被调用

**文件：** `internal/generator/generator.go` → `callClaude()`

```go
func (g *Generator) callClaude(...) (string, error) {
    // TODO: 接入 Anthropic SDK
    // client := anthropic.NewClient(apiKey)
    // ...
    // 临时使用模拟
    return g.fallbackGenerate(sig), nil  // ← 永远走这里！
}
```

**影响：** 不管 API Key 是否配置正确，所有文案生成都走 mock 函数，无法使用 Claude 生成真实内容。

**严重程度：** 🔴 阻塞  
**建议：** 接入 Anthropic SDK 实现真正的 API 调用。

---

#### B-3: RSS Feeds 数组为空，Engine 无法抓取任何信号

**文件：** `internal/engine/engine.go` → `Poll()`

```go
feeds := []string{
    // 微信公众号 RSS 示例 (使用第三方 RSS 服务)
    // "https://rsshub.app/wechat/mp/..."  ← 全部注释！
}
```

**影响：** `Poll()` 遍历空数组，什么都不会抓取，整个 RSS 信号源完全失效。

**严重程度：** 🔴 阻塞  
**建议：** Feed URL 应从数据库 accounts 表读取，而非硬编码。

---

#### B-4: Generator 从未被任何组件调用（完整 Pipeline 断裂）

**检查结果：** 整个代码库搜索 `generator.Generate` 无任何调用点。

```
Engine.Start()        → runLoop() → Poll()（空数组）
Publisher.RunOnce()   → postJob()（TODO）
main.go              → 没有触发 generator 的路由
```

**影响：** 即使 RSS 抓到了信号，也没有任何逻辑触发文案生成。信号 → 生成 → 发布的完整流程完全断裂。

**严重程度：** 🔴 阻塞  
**建议：** 需要新增 `/api/signals` 路由、或在 Engine 中自动触发 Generator。

---

#### B-5: 完整的 RSS→生成→发布 Pipeline 缺失

**架构问题：** 现有组件是孤立存在的，没有串联：

| 组件 | 状态 | 问题 |
|------|------|------|
| Engine (RSS抓取) | 🔴 失效 | feeds 空数组 |
| Generator (Claude生成) | 🔴 失效 | Anthropic SDK 未接入 |
| Publisher (Playwright发布) | 🔴 失效 | Playwright 操作未实现 |
| Queue (任务队列) | ✅ 正常 | 仅作为数据存储 |
| Webhook (人工审核) | ✅ 正常 | SSRF 保护完善 |

**影响：** 即使所有模块单独可用，当前代码无法组成一个端到端可用的系统。

**严重程度：** 🔴 阻塞  
**建议：** 设计完整的自动化 Pipeline 流程图并逐步实现。

---

### 🟠 严重问题（High）

#### H-1: API Key 验证是空实现

**文件：** `internal/middleware/middleware.go` → `validateAPIKey()`

```go
func validateAPIKey(key string) bool {
    // TODO: implement actual API key validation
    return len(key) >= 8  // ← 任何 >= 8 字符都通过！
}
```

**影响：** 任何 8 字符以上的字符串都可以作为 API Key 通过认证，无安全性可言。

---

#### H-2: Job 重试无最大次数限制

**文件：** `internal/queue/queue.go` 和 `internal/publisher/publisher.go`

```go
// retryJob API handler 中无任何最大重试次数检查
// Dequeue 也无最大重试次数过滤
```

**影响：** 一个持续失败的 Job 会无限重试，浪费资源和产生大量错误日志。

---

#### H-3: 敏感词列表不完整（英文加密货币词汇缺失）

**文件：** `internal/engine/engine.go` 和 `internal/generator/generator.go`

```go
SensitiveWordList = []string{
    "比特币", "BTC", "ETH", "以太坊",  // 中文 ✓
    // 缺失: bitcoin, BTC, crypto, NFT, DeFi, 炒币 ...
}
```

**影响：** 英文加密货币内容不会被过滤，如 "bitcoin trading" 可以通过。

---

#### H-4: CORS 允许所有来源（生产环境风险）

**文件：** `internal/middleware/middleware.go` → `CORSMiddleware()`

```go
c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
```

**影响：** 任意第三方网站可向 API 发起跨域请求，应配置白名单。

---

#### H-5: RateLimitMiddleware 已实现但未全局启用

**文件：** `internal/middleware/middleware.go`（已实现） vs `cmd/server/main.go`（未使用）

```go
// middleware.go 有完整实现
func RateLimitMiddleware(requestsPerMinute int) gin.HandlerFunc { ... }

// main.go 的 r.Use() 中没有调用它
r.Use(middleware.CORSMiddleware())
r.Use(middleware.SecureHeaders())
r.Use(middleware.RequestID())
// 缺少: r.Use(middleware.RateLimitMiddleware(60))
```

---

#### H-6: Webhook baseURL 默认为 localhost，生产环境无法回调

**文件：** `internal/webhook/webhook.go` → `NewWebhookClient()`

```go
baseURL: "http://localhost:8080"  // ← 硬编码 localhost
```

**影响：** 外部 Webhook 系统回调时 approve/reject URL 指向 localhost，无法访问。应在 main.go 初始化时通过环境变量或配置传入正确的公网地址。

---

### 🟡 中等问题（Medium）

#### M-1: getUserID 返回 0 时静默失败

**文件：** `cmd/server/main.go` → `getUserID()`

```go
func getUserID(c *gin.Context) int64 {
    if v, exists := c.Get("user_id"); exists {
        if id, ok := v.(int64); ok {
            return id
        }
    }
    return 0  // ← 认证失败时返回 0，不报错
}
```

**影响：** 认证中间件失败时，用户 ID 为 0，后面的 Create/List 操作会把数据挂在 user_id=0 下，造成数据混乱。

---

#### M-2: CheckRedirect 导致重定向请求失败

**文件：** `internal/engine/engine.go` → `NewEngine()`

```go
CheckRedirect: func(req *http.Request, via []*http.Request) error {
    return http.ErrUseLastResponse  // ← 301/302 会变成错误
},
```

**影响：** RSSHub 等服务使用 302 重定向的场景下，请求会直接失败，无法获取真正的 feed 内容。

---

#### M-3: StatusGenerated 在 queue.go 中未定义但在 webhook.go 中使用

**文件：** `internal/webhook/webhook.go` → `HandleCallback()`

```go
// webhook.go 中使用 StatusGenerated
if status != StatusPending && status != StatusGenerated {  // ← 使用
```

**文件：** `internal/queue/queue.go`

```go
// StatusGenerated = "generated"  ← 未定义！
const (
    StatusPending    = "pending"
    StatusGenerating = "generating"
    StatusGenerated  = "generated"  // ← 缺失！
    StatusApproved   = "approved"
    StatusPublished  = "published"
    StatusFailed     = "failed"
)
```

**影响：** 虽然 webhook.go 导入了 queue 包（间接），但状态常量定义不一致会导致逻辑混乱。

---

#### M-4: Webhook SSRF 保护遗漏 IPv6 link-local 完整覆盖

**文件：** `internal/webhook/webhook.go` → `isValidWebhookURL()`

```go
if strings.HasPrefix(host, "fe80:") {  // ← 只匹配 fe80: 前缀
    return false
}
// 遗漏: fe80::1 (fe80::/10 IPv6 link-local)
```

---

### 🟢 轻微问题（Low）

#### L-1: generateRequestID 高并发下有碰撞风险

**文件：** `internal/middleware/middleware.go` → `generateRequestID()`

```go
return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixNano()%10000)
// UnixNano 在极短间隔内可能相同
```

---

#### L-2: Webhook 回调状态码未校验

**文件：** `internal/webhook/webhook.go` → `SendReviewNotification()`

```go
defer resp.Body.Close()  // ← 状态码在 defer 之前就返回了，没检查
if resp.StatusCode < 200 || resp.StatusCode >= 300 {
    return fmt.Errorf("webhook returned non-2xx status: %d", resp.StatusCode)
}
```

---

## 二、Playwright 发布流程审查

**文件：** `internal/publisher/publisher.go`

### 反检测脚本 ✅ 完整
`AntiDetectScript` 覆盖了以下维度：
- ✅ 移除 `navigator.webdriver` 属性
- ✅ 模拟 `chrome` runtime 对象
- ✅ 伪造 `plugins`（PDF Viewer 等）
- ✅ 伪造 `languages` (zh-CN)
- ✅ 伪造 `hardwareConcurrency` / `deviceMemory`
- ✅ 删除 `__webdriver_evaluate` 等 Selenium/CDC 属性
- ✅ 伪造 `navigator.connection` (4g, downlink:10)
- ✅ 伪造 `permissions.query` (notifications)
- ✅ 伪造 `navigator.platform` (Win32)
- ✅ userAgent 代理（部分实现）

### 发布流程步骤 ⚠️ 全部为 TODO

| 步骤 | 状态 | 备注 |
|------|------|------|
| 1. 加载 Chrome Profile | ❌ TODO | --user-data-dir 需 Playwright 支持 |
| 2. 注入反检测脚本 | ❌ 未调用 | AntiDetectScript 已定义但未注入 |
| 3. 打开小红书创作者后台 | ❌ TODO | https://creator.xiaohongshu.com |
| 4. 上传图片（image_path） | ❌ TODO | 需文件上传交互 |
| 5. 填写标题和文案 | ❌ TODO | parseContent 解析正确但未使用 |
| 6. 添加话题标签 | ❌ TODO | tags 已解析但未使用 |
| 7. 点击发布按钮 | ❌ TODO | |
| 8. 等待确认弹窗 | ❌ TODO | |
| 9. 关闭浏览器 | ❌ TODO | |

### 发布结果判定 ⚠️ 无截图确认
当前代码 `MarkPublished` 无条件执行：
```go
} else {
    p.queueMgr.MarkPublished(job.ID)  // ← 无任何确认机制
    ...
}
```
**问题：** 即使 Playwright 操作失败（如网络超时、元素找不到），只要不 panic 报错，job 都会被标记为已发布。

**需手动验证项：**
1. Chrome Profile 加载后是否真正登录
2. 反检测脚本注入后小红书是否能识别为自动化
3. 图片上传的 file input 交互是否正确
4. 发布按钮点击后弹窗确认逻辑
5. 发布失败时的截图捕获（当前无截图）

---

## 三、API 端点验证

**文件：** `cmd/server/main.go`

### 路由清单

| 方法 | 路径 | 处理器 | 状态 |
|------|------|--------|------|
| GET | /health | healthCheck | ✅ |
| GET | /api/accounts | listAccounts | ✅ |
| POST | /api/accounts | createAccount | ✅ |
| PUT | /api/accounts/:id | updateAccount | ✅ |
| DELETE | /api/accounts/:id | deleteAccount | ✅ |
| GET | /api/accounts/:id | getAccount | ✅ |
| GET | /api/jobs | listJobs | ✅ |
| POST | /api/jobs | createJob | ✅ |
| GET | /api/jobs/:id | getJob | ✅ |
| POST | /api/jobs/:id/approve | approveJob | ✅ |
| POST | /api/jobs/:id/reject | rejectJob | ✅ |
| POST | /api/jobs/:id/retry | retryJob | ✅ |
| DELETE | /api/jobs/:id | deleteJob | ✅ |
| GET | /api/stats | getStats | ✅ |
| GET | /api/stats/accounts/:id | getAccountStats | ✅ |
| POST | /api/webhook/callback | webhookCallback | ✅ |

### 🔴 缺失端点

| 缺失端点 | 用途 | 优先级 |
|---------|------|--------|
| GET /api/signals | 查看抓取的 RSS 信号 | 🔴 阻塞 |
| POST /api/signals/:id/generate | 手动触发单条信号生成文案 | 🔴 阻塞 |
| GET /api/engine/feeds | 配置/查看 RSS Feed 列表 | 🟠 High |
| POST /api/engine/poll | 手动触发一次 RSS 抓取 | 🟠 High |

### ⚠️ 路由问题

1. **DELETE /api/jobs/:id** 语义不正确——删 Job 实际是 `UpdateStatusWithError(id, failed, "deleted")`，并非真正的 SQL DELETE。这会造成误解。

2. **POST /api/jobs 缺少 `Content` 字段验证**：
   ```go
   Content string `json:"content"`  // binding:"required" 缺失
   ```
   可以创建 content 为空的 Job。

---

## 四、回归测试用例

### 模块 1: queue (任务队列)

**用例 1-1: 正常入队和出队**
```
功能: Job 入队后能被正确 Dequeue
输入: Enqueue(job{account_id:1, content:"测试"})
预期输出: Dequeue 返回该 Job，status=pending
```

**用例 1-2: 空队列 Dequeue 返回 nil**
```
功能: 无可用 Job 时 Dequeue 不报错
输入: Dequeue(accountID=999)  // 不存在的账号
预期输出: 返回 nil, nil（无错误）
```

**用例 1-3: 状态流转正确性**
```
功能: approveJob 将 pending 改为 approved
输入: 先 createJob(pending) → 再 approveJob(id)
预期输出: Job status = "approved"，approved_at 非空
```

---

### 模块 2: generator (Claude 生成器)

**用例 2-1: 内容验证-含敏感词拒绝**
```
功能: ValidateContent 检测加密货币敏感词
输入: Content = "投资比特币 BTC 获得高回报"
预期输出: 返回 error，提示 "content contains sensitive word: 比特币"
```

**用例 2-2: fallback 模式生成内容**
```
功能: 无 API Key 时使用 fallback 生成
输入: Generate(ctx, signal{title:"护肤心得", content:"日常护肤分享"}, accountID=1) // account.claude_api_key=""
预期输出: 返回包含"标题"、"正文"、"标签"格式的字符串
```

**用例 2-3: parseContent 正确解析多格式文案**
```
功能: 解析"标题：xxx 正文：xxx 标签：xxx"格式
输入: "标题：今日穿搭\n正文：秋天必备\n标签：#穿搭 #日常"
预期输出: map["标题":"今日穿搭", "正文":"秋天必备", "标签":"#穿搭 #日常"]
```

---

### 模块 3: engine (RSS 抓取)

**用例 3-1: 敏感内容过滤**
```
功能: Filter 正确过滤含敏感词的 RSS 项
输入: RSSItem{title:"炒币日赚千元", description:"教你炒币"}
预期输出: true（应过滤）
```

**用例 3-2: 重复信号去重**
```
功能: 相同 title + url 的信号只保存一条
输入: saveSignal(item{title:"文章1", Link:"http://x.com/a"}) 连续调用两次
预期输出: 第一次返回 nil error，第二次返回 nil error，数据库只有 1 条
```

---

### 模块 4: webhook (人工审核通知)

**用例 4-1: SSRF 防护-拒绝内网 IP**
```
功能: isValidWebhookURL 拒绝内网地址
输入: "https://192.168.1.1/webhook"
预期输出: false
```

**用例 4-2: SSRF 防护-拒绝 localhost**
```
功能: isValidWebhookURL 拒绝 localhost
输入: "https://localhost/webhook"
预期输出: false
```

**用例 4-3: 非法 URL 被拦截**
```
功能: 非 HTTPS URL 被拦截
输入: "http://example.com/webhook"（非 HTTPS）
预期输出: false
```

---

### 模块 5: publisher (Playwright 发布)

**用例 5-1: Chrome Profile 目录不存在则报错**
```
功能: postJob 检测 Chrome Profile 目录有效性
输入: account.ChromeUserDataDir="/invalid/path"
预期输出: 返回 error "Chrome profile directory not found"
```

**用例 5-2: parseContent 兼容无标签格式**
```
功能: 无标签的纯文案也能解析
输入: "今天分享一款好物，真的很棒！"
预期输出: map["正文"] 非空
```

**用例 5-3: shouldSkip 检测冷却期**
```
功能: 距上次发帖不足 intervalMin 分钟时跳过
输入: account.IntervalMin=30, LastPostAt=now-10min
预期输出: shouldSkip=true
```

---

### 模块 6: middleware (认证与安全)

**用例 6-1: 无 Authorization Header 返回 401**
```
功能: AuthRequired 中间件拒绝无 Token 请求
输入: GET /api/accounts (无 Authorization header)
预期输出: HTTP 401 {"error":"no token"}
```

**用例 6-2: 无效 Token 格式返回 401**
```
功能: validateToken 拒绝格式错误的 Token
输入: Authorization: "Bearer invalid-token-format"
预期输出: HTTP 401 {"error":"invalid token format"}
```

**用例 6-3: RateLimit 超出限制返回 429**
```
功能: 同一 IP 请求超过限制时被限流
输入: 同一 IP 发送 61 个请求/分钟（限流 60）
预期输出: HTTP 429 {"error":"rate limit exceeded"}
```

---

## 五、QA 结论

### 🎯 总体判定：**❌ 不可发布（阻塞问题）**

### 🔴 阻塞问题（必须修复后才能发布）

| # | 问题 | 文件 | 建议 |
|---|------|------|------|
| B-1 | Playwright 发布流程未实现 | publisher.go | 实现完整发布流程（登录→上传→填文案→发布→确认） |
| B-2 | Claude API 未接入，永远 fallback | generator.go | 接入 Anthropic SDK |
| B-3 | RSS Feeds 数组为空 | engine.go | 从数据库读取 Feed URL，支持动态配置 |
| B-4 | Generator 从未被调用 | - | 新增 API 端点和 Pipeline 串联逻辑 |
| B-5 | 完整 Pipeline 断裂 | 架构层 | 设计并实现信号→生成→发布的自动化流程 |

### 建议修复优先级

```
第一阶段（发布前必须）:
  1. 实现 Playwright 完整发布流程
  2. 接入 Anthropic Claude API
  3. 实现 Generator 触发机制（/api/signals/:id/generate）
  4. 修复 RSS feeds 从数据库读取

第二阶段（发布质量提升）:
  5. 实现 /api/engine/feeds 管理端点
  6. 添加最大重试次数限制
  7. 补全英文敏感词列表
  8. 全局启用 RateLimitMiddleware
  9. 修复 baseURL 默认 localhost 问题

第三阶段（长期工程化）:
  10. CORS 白名单配置
  11. API Key 真实校验逻辑
  12. 发布结果截图确认机制
  13. 添加单元测试覆盖率
```

### ⚠️ 需手动验证项（Playwright 真实环境）

由于当前环境无法运行浏览器，以下功能需要部署后在真实小红书环境中验证：

1. Chrome Profile 登录状态保持
2. 反检测脚本注入后小红书的自动化检测是否绕过
3. 图片上传元素定位和交互
4. 发布按钮点击和确认弹窗处理
5. 发布失败时的截图捕获是否正常

---

*QA 报告由 OpenClaw Subagent 自动生成 | 2026-04-09*
