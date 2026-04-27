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

// modelContextDatabase 扩展的模型上下文长度数据库
// 覆盖主流大模型厂商的常见模型
// 注意：此数据库僅作為 fallback，用戶應優先通過 config.toon 的 ContextLength 或 MaxTokens 指定上下文長度
var modelContextDatabase = map[string]int{
        // ---- OpenAI ----
        "gpt-4":                  128000,
        "gpt-4-1106-preview":     128000,
        "gpt-4-0125-preview":     128000,
        "gpt-4-0613":             8192,
        "gpt-4-32k":              32768,
        "gpt-4-turbo":            128000,
        "gpt-4o":                 128000,
        "gpt-4o-mini":            128000,
        "gpt-3.5-turbo":          16384,
        "gpt-3.5-turbo-1106":     16384,
        "gpt-3.5-turbo-0125":     16384,
        "gpt-3.5-turbo-16k":      16384,
        "gpt-3.5-turbo-instruct": 4096,
        "o1-preview":             128000,
        "o1-mini":                128000,
        "o3-mini":                200000,

        // ---- Anthropic ----
        "claude-3-opus":             200000,
        "claude-3-opus-20240229":    200000,
        "claude-3-sonnet":           200000,
        "claude-3-sonnet-20240229":  200000,
        "claude-3-haiku":            200000,
        "claude-3-haiku-20240307":   200000,
        "claude-3.5-sonnet":         200000,
        "claude-3.5-sonnet-20241022": 200000,
        "claude-3.5-haiku":          200000,
        "claude-4-sonnet":           200000,
        "claude-4-opus":             200000,
        "claude-sonnet-4-6":         200000,
        "claude-sonnet-4-6-20250514": 200000,
        "claude-opus-4-6":           200000,
        "claude-haiku-4-5":          200000,
        "claude-2.1":                200000,
        "claude-2":                  100000,

        // ---- Google ----
        "gemini-pro":          32768,
        "gemini-1.5-pro":      1048576,
        "gemini-1.5-pro-lite": 1048576,
        "gemini-1.5-flash":    1048576,
        "gemini-2.0-flash":    1048576,
        "gemini-2.0-pro":      2097152,
        "gemini-2.5-pro":      1048576,
        "gemini-2.5-flash":    1048576,

        // ---- DeepSeek ----
        "deepseek-chat":    64000,
        "deepseek-coder":   64000,
        "deepseek-reasoner": 64000,
        "deepseek-llm":     128000,

        // ---- Qwen (通义千问) ----
        "qwen-turbo":  131072,
        "qwen-plus":   131072,
        "qwen-max":    32768,
        "qwen-long":   1048576,
        "qwen2-7b":    32768,
        "qwen2-14b":   131072,
        "qwen2-32b":   131072,
        "qwen2-72b":   131072,
        "qwen2.5-7b":  131072,
        "qwen2.5-14b": 131072,
        "qwen2.5-32b": 131072,
        "qwen2.5-72b": 131072,
        "qwen3.5-1.8b": 131072,
        "qwen3.5-14b": 131072,
        "qwen3.5-32b": 131072,
        "qwen3.5-72b": 131072,

        // ---- Meta Llama ----
        "llama-3-8b":  8192,
        "llama-3-70b": 8192,
        "llama-3.1-8b": 131072,
        "llama-3.1-70b": 131072,
        "llama-3.1-405b": 131072,
        "llama-3.2-1b": 131072,
        "llama-3.2-3b": 131072,
        "llama-3.2-11b": 131072,
        "llama-3.3-70b": 131072,
        "llama-4-maverick": 131072,
        "llama-4-scout":    1048576,
        "llama3-70b":  131072,
        "llama3-8b":   131072,
        "llama2-70b":  4096,
        "llama2-13b": 4096,
        "llama2-7b":   4096,

        // ---- GLM (智谱) ----
        "glm-4":         131072,
        "glm-4-flash":   131072,
        "glm-4-plus":    131072,
        "glm-4-long":    1048576,
        "glm-4-air":     131072,
        "glm-4v":        8192,
        "chatglm3-6b":   32768,
        "glm-3-turbo":   100000,

        // ---- Yi (零一万物) ----
        "yi-34b":    4096,
        "yi-6b":     4096,
        "yi-large":  16384,
        "yi-lightning": 16384,

        // ---- Mistral ----
        "mistral-7b":        32768,
        "mistral-large":     131072,
        "mistral-small":     32768,
        "mixtral-8x7b":      32768,
        "mixtral-8x22b":     65536,
        "codestral":         32768,
        "mistral-nemo":      131072,
        "pixtral-large":     131072,

        // ---- MiniMax ----
        "minimax":           204800,
        "minimax-abab6":     204800,
        "minimax-abab6.5":   204800,

        // ---- Kimi (Moonshot) ----
        "kimi":     262144,
        "moonshot": 131072,

        // ---- Baichuan ----
        "baichuan2-7b":  4096,
        "baichuan2-13b": 4096,
        "baichuan4":     131072,

        // ---- Yi / 01.AI ----
        "yi-vl-34b": 4096,
}

// GetModelContextLengthSafe 获取模型上下文长度
// 优先级：用户配置(ContextLength 或 MaxTokens) > hardcoded database 精确匹配 > 子串匹配 > suffix 推斷 > 安全默認值
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

        // 2) Hardcoded database：精確匹配（向後兼容）
        if limit, ok := modelContextDatabase[lowerID]; ok {
                return limit
        }

        // 3) Hardcoded database：子串匹配（取最长匹配）
        var bestMatch string
        var bestLimit int
        for model, limit := range modelContextDatabase {
                if strings.Contains(lowerID, model) {
                        if len(model) > len(bestMatch) {
                                bestMatch = model
                                bestLimit = limit
                        }
                }
        }
        if bestMatch != "" {
                return bestLimit
        }

        // 4) Model ID suffix 智能推斷（如 [1m]、[128k]）
        if limit := detectContextLengthFromSuffix(lowerID); limit > 0 {
                return limit
        }

        // 5) 安全默认值：4096（避免未知模型溢出上下文）
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
