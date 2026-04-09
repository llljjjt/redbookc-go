package generator

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"redbookc-go/pkg/signal"
)

// Generator generates RedBook (小红书) content using Claude
type Generator struct {
	db *sql.DB
}

// NewGenerator creates a new content generator
func NewGenerator(db *sql.DB) *Generator {
	return &Generator{db: db}
}

// Generate generates RedBook content from a signal
func (g *Generator) Generate(ctx context.Context, sig *signal.Signal, accountID int64) (string, error) {
	prompt := g.BuildPrompt(sig)

	// TODO: 调用 Claude API 生成内容
	// 这里先用模板方法，后续接入 anthropic SDK
	content, err := g.callClaude(ctx, prompt, sig, accountID)
	if err != nil {
		return "", fmt.Errorf("failed to call Claude: %w", err)
	}

	// 标记 signal 已使用
	sm := signal.NewManager(g.db)
	if err := sm.MarkUsed(sig.ID); err != nil {
		fmt.Printf("[generator] failed to mark signal used: %v\n", err)
	}

	return content, nil
}

// BuildPrompt 构建小红书风格的 prompt
func (g *Generator) BuildPrompt(sig *signal.Signal) string {
	return fmt.Sprintf(`你是一个专业的小红书内容创作者。请根据以下信息，生成一篇小红书风格的帖子。

## 原始信号
标题：%s
内容：%s

## 要求
1. 使用简体中文，中文标点
2. 包含 3-5 个 emoji 表情符号（放在合适的位置）
3. 正文控制在 20 字以内（不含标签）
4. 添加 2-3 个话题标签，格式：#标签1 #标签2 #标签3
5. 风格：亲切、自然、有感染力，像真实博主分享
6. 禁止出现以下内容：加密货币、比特币、资金盘、赌博、色情、虚假广告
7. 不要使用特殊符号如 ★ ◆ 等，使用自然的文字表达

## 输出格式
标题：[20字以内的小红书标题]
正文：[20字以内]
标签：[#话题1 #话题2 #话题3]

请直接输出内容，不要添加解释。`, sig.Title, sig.Content)
}

// callClaude 调用 Claude API
func (g *Generator) callClaude(ctx context.Context, prompt string, sig *signal.Signal, accountID int64) (string, error) {
	// 获取账号配置的 API Key
	var apiKey string
	err := g.db.QueryRow(`SELECT claude_api_key FROM accounts WHERE id = ? LIMIT 1`, accountID).Scan(&apiKey)
	if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("failed to get API key: %w", err)
	}

	// 如果没有配置 API Key，使用模拟内容
	if apiKey == "" {
		return g.fallbackGenerate(sig), nil
	}

	// TODO: 接入 Anthropic SDK
	// client := anthropic.NewClient(apiKey)
	// resp, err := client.Messages.New(ctx, &anthropic.MessageNewParams{...})
	// return resp.Content[0].Text, err

	// 临时使用模拟
	return g.fallbackGenerate(sig), nil
}

// fallbackGenerate 当没有 API Key 时生成模拟内容
func (g *Generator) fallbackGenerate(sig *signal.Signal) string {
	// 从标题提取关键词
	keywords := extractKeywords(sig.Title)
	tags := []string{
		"#" + pickTag(keywords),
		"#生活分享",
		"#日常",
	}

	return fmt.Sprintf(`标题：%s
正文：%s %s ✨
标签：%s`,
		truncateString(sig.Title, 20),
		keywords,
		getRandomEmoji(),
		strings.Join(tags, " "),
	)
}

// extractKeywords 从标题提取关键词
func extractKeywords(title string) string {
	// 简单分词：去掉常见停用词
	stopWords := []string{"的", "了", "是", "在", "和", "与", "对", "为", "有", "我", "你", "他", "她", "它"}
	words := strings.Fields(title)
	var result []string
	for _, w := range words {
		isStop := false
		for _, sw := range stopWords {
			if w == sw {
				isStop = true
				break
			}
		}
		if !isStop && len(w) > 1 {
			result = append(result, w)
		}
	}
	if len(result) == 0 {
		return title
	}
	// 取前3个关键词
	if len(result) > 3 {
		result = result[:3]
	}
	return strings.Join(result, " ")
}

// pickTag 选择一个合适的标签
func pickTag(keyword string) string {
	topicMap := map[string][]string{
		"护肤":   {"护肤心得", "护肤分享", "美肤"},
		"穿搭":   {"每日穿搭", "时尚穿搭", "穿搭分享"},
		"美食":   {"美食分享", "吃货日记", "探店"},
		"旅行":   {"旅行打卡", "旅行分享", "周末去哪"},
		"健身":   {"健身日记", "运动打卡", "健身分享"},
		"读书":   {"读书笔记", "书单推荐", "阅读分享"},
		"职场":   {"职场干货", "打工人", "工作日常"},
		"母婴":   {"妈妈分享", "育儿日记", "宝宝好物"},
	}
	for k, tags := range topicMap {
		if strings.Contains(keyword, k) {
			return tags[0]
		}
	}
	return "日常分享"
}

// getRandomEmoji 返回随机 emoji
func getRandomEmoji() string {
	emojis := []string{"💖", "✨", "🌟", "💫", "🔥", "💕", "🌈", "🍀", "💯", "🙌"}
	// 使用时间作为简单随机种子
	return emojis[time.Now().UnixNano()%int64(len(emojis))]
}

// truncateString 截断字符串到指定长度
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}

// ValidateContent 验证生成的内容是否符合规范
func (g *Generator) ValidateContent(content string) error {
	sensitiveWords := []string{
		"比特币", "BTC", "ETH", "以太坊", "加密货币", "虚拟货币",
		"区块链", "token", "炒币", "数字货币", "资金盘", "传销",
		"赌博", "菠菜", "博彩", "约炮", "色情",
	}

	lower := strings.ToLower(content)
	for _, word := range sensitiveWords {
		if strings.Contains(lower, strings.ToLower(word)) {
			return fmt.Errorf("content contains sensitive word: %s", word)
		}
	}
	return nil
}
