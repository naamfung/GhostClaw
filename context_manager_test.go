package main

import (
	"testing"
)

// ============================================================================
// isCJK
// ============================================================================

func TestIsCJK(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		// 基本 CJK
		{'中', true},
		{'文', true},
		{'日', true},
		{'本', true},
		{'語', true},
		{'一', true},  // U+4E00
		{'龥', true},  // U+9FA0 — CJK 主區邊界附近
		// 拉丁/數字
		{'a', false},
		{'Z', false},
		{'0', false},
		{'9', false},
		// 符號
		{' ', false},
		{'.', false},
		{'，', true}, // U+FF0C 全角逗号 — 在 Halfwidth/Fullwidth 範圍 (0xFF00-0xFFEF)
		{'。', false}, // U+3002 — 不在任何 CJK 範圍內
		// Extension A
		{'㐀', true}, // U+3400
		// Compatibility
		{'豈', true}, // U+F900
	}
	for _, tt := range tests {
		got := isCJK(tt.r)
		if got != tt.want {
			t.Errorf("isCJK(%q [U+%04X]) = %v, want %v", tt.r, tt.r, got, tt.want)
		}
	}
}

// ============================================================================
// ImprovedEstimateTokens
// ============================================================================

func TestImprovedEstimateTokens(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		minV  int // 最小預期值
		maxV  int // 最大預期值
	}{
		{"空字符串", "", 1, 1},
		{"纯英文", "hello world this is a test", 5, 10},
		{"纯中文", "你好世界这是一个测试", 7, 20},
		{"中英混合", "hello 你好 world 世界", 7, 15},
		{"JSON 开销", `{"key": "value", "num": 123}`, 8, 15},
		{"长中文", "这是一段比较长的中文文本用来测试token估算的准确性", 15, 50},
		{"纯数字", "1234567890", 2, 6},
		{"特殊字符", "@#$%^&*()", 2, 6},
		{"代码片段", "func hello() { return 42 }", 8, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ImprovedEstimateTokens(tt.text)
			if got < tt.minV || got > tt.maxV {
				t.Errorf("ImprovedEstimateTokens(%q) = %d, want [%d, %d]", tt.text, got, tt.minV, tt.maxV)
			}
		})
	}

	// 基准验证：估算必须 > 0
	t.Run("永不返回 0", func(t *testing.T) {
		got := ImprovedEstimateTokens("")
		if got < 1 {
			t.Errorf("should return at least 1, got %d", got)
		}
	})

	// 单调性：更长文本 -> 更多 token
	t.Run("单调性", func(t *testing.T) {
		short := ImprovedEstimateTokens("hello")
		long := ImprovedEstimateTokens("hello world this is much longer text")
		if long <= short {
			t.Errorf("longer text should have more tokens: short=%d, long=%d", short, long)
		}
	})
}

// ============================================================================
// detectContextLengthFromSuffix
// ============================================================================

func TestDetectContextLengthFromSuffix(t *testing.T) {
	tests := []struct {
		modelID string
		want    int
	}{
		{"", 0},
		{"gpt-4", 0},
		{"model-[128k]", 131072},
		{"claude-3-opus-[200k]", 204800},
		{"gemini-[1m]", 1048576},
		{"llama-[32k]-instruct", 32768},
		{"my-model-[64k]", 65536},
		{"deepseek-[16k]", 16384},
		{"test-[8k]", 8192},
		{"hugemodel-[2m]", 2097152},
		{"model-[256k]-v2", 262144},
		{"model-[512k]-beta", 524288},
	}
	for _, tt := range tests {
		got := detectContextLengthFromSuffix(tt.modelID)
		if got != tt.want {
			t.Errorf("detectContextLengthFromSuffix(%q) = %d, want %d", tt.modelID, got, tt.want)
		}
	}
}

// ============================================================================
// GetModelContextLengthSafe — 5 级 fallback
// ============================================================================

func TestGetModelContextLengthSafe(t *testing.T) {
	t.Run("空 modelID 返回默认 4096", func(t *testing.T) {
		got := GetModelContextLengthSafe("")
		if got != 4096 {
			t.Errorf("expected 4096, got %d", got)
		}
	})

	t.Run("用户配置覆盖优先", func(t *testing.T) {
		oldOverrides := userContextLengthOverrides
		defer func() { userContextLengthOverrides = oldOverrides }()
		userContextLengthOverrides = map[string]int{"custom-model": 99999}

		got := GetModelContextLengthSafe("custom-model")
		if got != 99999 {
			t.Errorf("expected user override 99999, got %d", got)
		}
	})

	t.Run("hardcoded database 精确匹配", func(t *testing.T) {
		got := GetModelContextLengthSafe("gpt-4")
		if got != 128000 {
			t.Errorf("expected 128000 for gpt-4, got %d", got)
		}
	})

	t.Run("hardcoded database 子串匹配 (Claude)", func(t *testing.T) {
		got := GetModelContextLengthSafe("claude-3-opus-20240229")
		if got != 200000 {
			t.Errorf("expected 200000, got %d", got)
		}
	})

	t.Run("hardcoded database 子串匹配 (DeepSeek)", func(t *testing.T) {
		got := GetModelContextLengthSafe("deepseek-chat-v2")
		if got != 64000 {
			t.Errorf("expected 64000, got %d", got)
		}
	})

	t.Run("suffix 推断 [128k]", func(t *testing.T) {
		got := GetModelContextLengthSafe("unknown-model-[128k]")
		if got != 131072 {
			t.Errorf("expected 131072, got %d", got)
		}
	})

	t.Run("suffix 推断 [1m]", func(t *testing.T) {
		got := GetModelContextLengthSafe("my-custom-[1m]")
		if got != 1048576 {
			t.Errorf("expected 1048576, got %d", got)
		}
	})

	t.Run("完全未知返回默认 4096", func(t *testing.T) {
		got := GetModelContextLengthSafe("xyz-unknown-model-abc")
		if got != 4096 {
			t.Errorf("expected 4096, got %d", got)
		}
	})

	t.Run("大小写不敏感", func(t *testing.T) {
		got := GetModelContextLengthSafe("GPT-4")
		if got != 128000 {
			t.Errorf("expected 128000, got %d", got)
		}
	})

	t.Run("子串匹配取最长", func(t *testing.T) {
		// "gpt-4" = 5 chars, "gpt-4o" = 6 chars
		got := GetModelContextLengthSafe("gpt-4o-mini")
		if got != 128000 {
			t.Errorf("expected 128000 (gpt-4o-mini matches gpt-4o over gpt-4), got %d", got)
		}
	})

	t.Run("Claude 4.6 Sonnet", func(t *testing.T) {
		got := GetModelContextLengthSafe("claude-sonnet-4-6-20250514")
		if got != 200000 {
			t.Errorf("expected 200000, got %d", got)
		}
	})

	t.Run("Gemini 2.0 Flash", func(t *testing.T) {
		got := GetModelContextLengthSafe("gemini-2.0-flash")
		if got != 1048576 {
			t.Errorf("expected 1048576, got %d", got)
		}
	})
}

// ============================================================================
// CalculateAdaptiveMaxHistory
// ============================================================================

func TestCalculateAdaptiveMaxHistory(t *testing.T) {
	t.Run("标准 128k 模型", func(t *testing.T) {
		got := CalculateAdaptiveMaxHistory(128000, 2000, 1000, 4096)
		// expected: effective=123904, available=123904*0.6-2000-1000=71342.4, /200=356
		// dynamicUpper=128000/2048=62.5→62, maxHistory=356 → clamped to 62
		// Actually: dynamicUpper = 128000/2048 = 62, clamped below to LowerBound (3) and above to UpperBound (128)
		// 62 is within [3, 128]. So dynamicUpper = 62.
		// effectiveContext = 128000-4096 = 123904
		// availableForHistory = 123904*0.6 - 2000 - 1000 = 74342.4 - 3000 = 71342.4
		// maxHistory = 71342.4 / 200 = 356.7 → 356
		// Clamp: 356 > 62 (dynamicUpper) → 62
		if got != 62 {
			t.Errorf("expected 62, got %d", got)
		}
	})

	t.Run("小模型 8k", func(t *testing.T) {
		got := CalculateAdaptiveMaxHistory(8192, 1000, 500, 1024)
		// dynamicUpper = 8192/2048 = 4
		// effectiveContext = 8192-1024 = 7168
		// available = 7168*0.6 - 1000 - 500 = 4300.8 - 1500 = 2800.8
		// maxHistory = 2800.8/200 = 14.0 → 14
		// clamp: 14 > 4 (dynamicUpper) → 4
		// But 4 > LowerBound(3), so it stays 4
		if got != 4 {
			t.Errorf("expected 4, got %d", got)
		}
	})

	t.Run("超大模型 1M", func(t *testing.T) {
		got := CalculateAdaptiveMaxHistory(1048576, 5000, 3000, 8192)
		// dynamicUpper = 1048576/2048 = 512, clamped to UpperBound(128) → 128
		// effective = 1048576-8192 = 1040384
		// available = 1040384*0.6 - 5000 - 3000 = 624230.4 - 8000 = 616230.4
		// maxHistory = 616230.4/200 = 3081
		// clamp: 3081 > 128 → 128
		if got != 128 {
			t.Errorf("expected 128, got %d", got)
		}
	})

	t.Run("极低输出预算 (负 effective)", func(t *testing.T) {
		// maxOutputTokens > contextWindow → effectiveContext goes negative → reset to contextWindow
		got := CalculateAdaptiveMaxHistory(4096, 500, 200, 5000)
		// dynamicUpper = 4096/2048 = 2, clamped to LowerBound → 3
		// effectiveContext = 4096-5000 = -904 < 0 → reset to 4096
		// available = 4096*0.6 - 500 - 200 = 2457.6 - 700 = 1757.6
		// maxHistory = 1757.6/200 = 8.78 → 8
		// clamp: 8 > 3 → 3... wait, 8 is between 3 and 3? No, dynamicUpper is 3.
		// 8 > 3 → 3
		if got != 3 {
			t.Errorf("expected 3, got %d", got)
		}
	})

	t.Run("极小 available (兜底到 500)", func(t *testing.T) {
		// systemPromptTokens + toolTokens consume almost everything
		got := CalculateAdaptiveMaxHistory(8192, 10000, 5000, 1024)
		// dynamicUpper = 8192/2048 = 4
		// effective = 8192-1024 = 7168
		// available = 7168*0.6 - 10000 - 5000 = 4300.8 - 15000 = -10699.2 < 0 → floor 500
		// maxHistory = 500/200 = 2 → 2
		// 2 < LowerBound(3) → 3
		if got != 3 {
			t.Errorf("expected 3 (lower bound), got %d", got)
		}
	})
}
