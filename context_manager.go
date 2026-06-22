package main

import (
	"strings"
)

// ============================================================================
// 改进的 Token 估算
// ============================================================================

// ImprovedEstimateTokens 提供更精确的 token 估算
// 基于常见分词器（BPE/SentencePiece）的实际行为校准：
// - CJK 字符：约 1.5 token/字符（单个中文字通常被编码为 2-3 个子词片段）
// - 拉丁字母：约 1 token/4 字符（平均英文单词约 1.3 token）
// - 数字/符号：约 1 token/3 字符
// - JSON/markup 结构开销：+15%
func ImprovedEstimateTokens(text string) int {
	runes := []rune(text)
	var cjkCount, latinCount, digitCount, otherCount int
	for _, r := range runes {
		switch {
		case isCJK(r):
			cjkCount++
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			latinCount++
		case r >= '0' && r <= '9':
			digitCount++
		default:
			otherCount++
		}
	}
	// 按字符类型加权估算
	tokens := float64(cjkCount)*1.5 + float64(latinCount)/4.0 + float64(digitCount)/3.0 + float64(otherCount)/3.0
	// JSON / 结构化文本额外开销（引号、花括号等标记符号增多）
	if strings.Contains(text, "\"") || strings.Contains(text, "{") {
		tokens *= 1.15
	}
	if tokens < 1 {
		return 1
	}
	return int(tokens)
}

// isCJK 判断字符是否属于中日韩统一表意文字范围
func isCJK(r rune) bool {
	return (r >= 0x4e00 && r <= 0x9fff) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4dbf) || // CJK Unified Ideographs Extension A
		(r >= 0x20000 && r <= 0x2a6df) || // CJK Unified Ideographs Extension B
		(r >= 0x2a700 && r <= 0x2b73f) || // CJK Unified Ideographs Extension C
		(r >= 0x2b740 && r <= 0x2b81f) || // CJK Unified Ideographs Extension D
		(r >= 0x2b820 && r <= 0x2ceaf) || // CJK Unified Ideographs Extension E
		(r >= 0x30000 && r <= 0x3134f) || // CJK Unified Ideographs Extension F
		(r >= 0x2e80 && r <= 0x2eff) || // CJK Radicals Supplement
		(r >= 0x31c0 && r <= 0x31ef) || // CJK Strokes
		(r >= 0xff00 && r <= 0xffef) || // Halfwidth and Fullwidth Forms
		(r >= 0xf900 && r <= 0xfaff) // CJK Compatibility Ideographs
}

// ============================================================================
// 改进的模型上下文长度查询（安全默认值 4096）
// ============================================================================

// userContextLengthOverrides 用戶通過 config.toon 中 ContextLength 或 MaxTokens 指定的上下文長度
// key: lowercase model ID, value: context window size in tokens
// 由 config_manager.go 的 syncGlobalsLocked() 在加載/熱重載時填充
var userContextLengthOverrides map[string]int

// SetUserContextLengthOverrides 設置用戶配置的上下文長度覆蓋（由 ConfigManager 調用）
func SetUserContextLengthOverrides(overrides map[string]int) {
	userContextLengthOverrides = overrides
}

// detectContextLengthFromSuffix 從 model ID 的 suffix 推斷上下文窗口大小
// 例如 "[1m]" → 1048576, "[128k]" → 131072, "[200k]" → 204800
func detectContextLengthFromSuffix(modelID string) int {
	// 匹配 [數字+單位] 模式
	if len(modelID) == 0 {
		return 0
	}
	// 常見 suffix patterns，按 specificity 排序
	suffixPatterns := []struct {
		suffix string
		limit  int
	}{
		{"[1m]", 1048576},
		{"[2m]", 2097152},
		{"[512k]", 524288},
		{"[256k]", 262144},
		{"[200k]", 204800},
		{"[128k]", 131072},
		{"[64k]", 65536},
		{"[32k]", 32768},
		{"[16k]", 16384},
		{"[8k]", 8192},
	}
	for _, p := range suffixPatterns {
		if strings.Contains(modelID, p.suffix) {
			return p.limit
		}
	}
	return 0
}

// GetModelContextLengthSafe 獲取模型上下文長度（token 數量）。
// 優先級：用戶配置（ContextLength 或 MaxTokens） > model ID suffix 推斷 > 安全默認值 4096。
// 注意：不再維護 hardcoded 模型數據庫，因為模型能力迭代頻繁，
// 同一名稱可能對接不同能力的模型（如 deepseek-chat 由 64K 升級到 1M）。
// 用戶應通過 config.toon 的 ContextLength 或 MaxTokens 顯式指定上下文長度。
func GetModelContextLengthSafe(modelID string) int {
	if modelID == "" {
		return 4096
	}

	lowerID := strings.ToLower(modelID)

	// 1) 優先：用戶通過 config.toon ContextLength 或 MaxTokens 顯式指定的上下文長度
	if userContextLengthOverrides != nil {
		if limit, ok := userContextLengthOverrides[lowerID]; ok && limit > 0 {
			return limit
		}
	}

	// 2) Model ID suffix 智能推斷（如 [1m]、[128k]、[200k]）
	if limit := detectContextLengthFromSuffix(lowerID); limit > 0 {
		return limit
	}

	// 3) 安全默認值：4096
	return 4096
}

// ============================================================================
// 自适应历史消息限制
// ============================================================================

// HistoryTokenCoefficient 每條歷史消息預留的 token 預算
// 用於從 context window 計算動態 MaxHistory：rawMax = contextWindow / HistoryTokenCoefficient
const HistoryTokenCoefficient = 2048

// MaxHistoryUpperBound 動態 MaxHistory 的絕對上限，防止超大 context window 導致 OOM
const MaxHistoryUpperBound = 128

// MaxHistoryLowerBound 動態 MaxHistory 的絕對下限，至少保留少量歷史消息
const MaxHistoryLowerBound = 3

// CalculateAdaptiveMaxHistory 根据实际上下文窗口大小动态计算最大历史消息数
// 替代原来硬编码的 MaxHistoryMessages = 30
//
// 参数：
//   - contextWindow: 模型的上下文窗口大小（token 数）
//   - systemPromptTokens: 系统提示词占用的 token 数
//   - toolTokens: 工具定义占用的 token 数
//   - maxOutputTokens: 预留给输出的最大 token 数
func CalculateAdaptiveMaxHistory(contextWindow int, systemPromptTokens int, toolTokens int, maxOutputTokens int) int {
	// 從 context window 計算動態上限
	// rawMax = contextWindow / HistoryTokenCoefficient，限制在 [LowerBound, UpperBound]
	dynamicUpper := contextWindow / HistoryTokenCoefficient
	if dynamicUpper < MaxHistoryLowerBound {
		dynamicUpper = MaxHistoryLowerBound
	}
	if dynamicUpper > MaxHistoryUpperBound {
		dynamicUpper = MaxHistoryUpperBound
	}

	// 先從總上下文窗口扣除輸出預留，剩餘的 60% 分配給歷史消息
	effectiveContext := contextWindow - maxOutputTokens
	if effectiveContext < 0 {
		effectiveContext = contextWindow
	}
	availableForHistory := float64(effectiveContext)*0.6 - float64(systemPromptTokens) - float64(toolTokens)
	if availableForHistory < 0 {
		availableForHistory = 500 // 绝对最小值，保证至少有少量上下文
	}
	// 假设平均每条消息约 200 token
	avgMessageTokens := 200.0
	maxHistory := int(availableForHistory / avgMessageTokens)
	// 限制在 LowerBound 到 dynamicUpper 之間
	if maxHistory < MaxHistoryLowerBound {
		maxHistory = MaxHistoryLowerBound
	}
	if maxHistory > dynamicUpper {
		maxHistory = dynamicUpper
	}
	return maxHistory
}
