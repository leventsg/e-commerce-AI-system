package domain

import "unicode"

// TokenEstimator 估算一段文本的 token 数量。
// 用于上下文预算决策：滑动窗口、摘要触发、各层 cap 截断。
// 估算结果只用于"决策"而非精确计量，因此允许（也鼓励）偏高估以留出安全边际。
type TokenEstimator interface {
	Tokens(text string) int
}

// HybridEstimator 混合字符分类估算器：
//   - CJK（汉字/假名/谚文）按 1 字约 1 token；
//   - ASCII 按 4 字符约 1 token；
//   - 其余字符（拉丁扩展、符号等）按 2 字符约 1 token。
//
// 对中文偏高估（常见中文 tokenizer 约 0.6~0.8 token/字），天然带安全边际，
// 避免预算超窗。英文 4 字符/token 是通用经验值。
type HybridEstimator struct{}

// Tokens 返回 text 的估算 token 数。
func (HybridEstimator) Tokens(text string) int {
	cjk, ascii, other := 0, 0, 0
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r):
			cjk++
		case r < 128:
			ascii++
		default:
			other++
		}
	}
	return cjk + (ascii+3)/4 + (other+1)/2
}

// EstimateTokens 使用默认 HybridEstimator 估算单段文本 token。
func EstimateTokens(text string) int {
	return HybridEstimator{}.Tokens(text)
}

// EstimateMessagesTokens 估算一组上下文消息的 token 总量。
func EstimateMessagesTokens(messages []ContextMessage) int {
	est := HybridEstimator{}
	total := 0
	for _, message := range messages {
		total += est.Tokens(message.Content)
	}
	return total
}

// TruncateToTokens 将 text 截断到 maxTokens 以内（按 rune 二分定位截断点）。
// maxTokens<=0 或 est 为 nil 时不截断；内容未超限时原样返回。
// 截断后追加可见标记，便于模型理解这是一段被预算截断的内容。
func TruncateToTokens(text string, maxTokens int, est TokenEstimator) string {
	if maxTokens <= 0 || est == nil || est.Tokens(text) <= maxTokens {
		return text
	}
	runes := []rune(text)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if est.Tokens(string(runes[:mid])) <= maxTokens {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo == 0 {
		return ""
	}
	return string(runes[:lo]) + "\n…[truncated]"
}

// TokenBudget 上下文 token 预算。把上下文压缩的决策依据从"消息条数"切换为"token 预算"：
//   - RecentWindowTokens：最近对话流滑动窗口预算（从新到旧累计，触顶即停）；
//   - SummaryTriggerTokens：未压缩消息累计 token 达到该阈值时触发摘要；
//   - 其余各 MaxTokens：对应非对话流层注入上下文时的截断上限，防止某一层无限膨胀；
//   - MinRecentMessages：预算耗尽时保底保留的最近消息条数；
//   - TotalTokens / SafetyFactor：输入侧总预算参考（乘安全边际后用于观测）。
type TokenBudget struct {
	TotalTokens               int     // 输入侧总预算（观测参考）
	RecentWindowTokens        int     // 最近对话流滑动窗口预算
	SummaryMaxTokens          int     // 会话摘要注入上限
	LatestToolResultMaxTokens int     // 最新工具结果注入上限
	ToolHistoryMaxTokens      int     // 历史工具调用注入上限
	TaskStateMaxTokens        int     // 任务状态注入上限
	UserProfileMaxTokens      int     // 用户画像注入上限
	MinRecentMessages         int     // 预算耗尽时保底保留的最近消息条数
	SummaryTriggerTokens      int     // 未压缩消息累计 token 触发摘要阈值
	SafetyFactor              float64 // 估算安全边际系数（0~1]，默认 0.9
}

// Normalized 返回填充默认值后的预算配置，保证零值字段也有合理行为。
func (b TokenBudget) Normalized() TokenBudget {
	if b.TotalTokens <= 0 {
		b.TotalTokens = 8000
	}
	if b.RecentWindowTokens <= 0 {
		b.RecentWindowTokens = 3000
	}
	if b.SummaryMaxTokens <= 0 {
		b.SummaryMaxTokens = 2000
	}
	if b.LatestToolResultMaxTokens <= 0 {
		b.LatestToolResultMaxTokens = 1500
	}
	if b.ToolHistoryMaxTokens <= 0 {
		b.ToolHistoryMaxTokens = 1200
	}
	if b.TaskStateMaxTokens <= 0 {
		b.TaskStateMaxTokens = 600
	}
	if b.UserProfileMaxTokens <= 0 {
		b.UserProfileMaxTokens = 600
	}
	if b.MinRecentMessages <= 0 {
		b.MinRecentMessages = 2
	}
	if b.SummaryTriggerTokens <= 0 {
		b.SummaryTriggerTokens = 4000
	}
	if b.SafetyFactor <= 0 || b.SafetyFactor > 1 {
		b.SafetyFactor = 0.9
	}
	return b
}

// EffectiveTotalTokens 返回乘安全边际后的总预算，作为观测上限。
func (b TokenBudget) EffectiveTotalTokens() int {
	return int(float64(b.TotalTokens) * b.SafetyFactor)
}
