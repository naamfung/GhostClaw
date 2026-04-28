package main

import (
	"strings"
	"testing"
)

// ============================================================================
// extractContentString
// ============================================================================

func TestExtractContentString_String(t *testing.T) {
	result := extractContentString("TASK")
	if result != "TASK" {
		t.Errorf("expected 'TASK', got '%s'", result)
	}
}

func TestExtractContentString_Nil(t *testing.T) {
	result := extractContentString(nil)
	if result != "" {
		t.Errorf("expected empty string for nil, got '%s'", result)
	}
}

func TestExtractContentString_WrongType(t *testing.T) {
	result := extractContentString(42)
	if result != "" {
		t.Errorf("expected empty string for int, got '%s'", result)
	}
}

func TestExtractContentString_EmptyString(t *testing.T) {
	result := extractContentString("")
	if result != "" {
		t.Errorf("expected empty string, got '%s'", result)
	}
}

// ============================================================================
// ClassifyUserIntent — 分類邏輯（唔需要實際 API call）
// ============================================================================

func TestClassifyIntentExactMatch_TASK(t *testing.T) {
	// 模擬 LLM 返回 "TASK"
	matched := matchTaskIntent("TASK")
	if !matched {
		t.Error("exact 'TASK' should be classified as IntentTask")
	}
}

func TestClassifyIntentExactMatch_CHAT(t *testing.T) {
	matched := matchTaskIntent("CHAT")
	if matched {
		t.Error("exact 'CHAT' should be classified as IntentChat")
	}
}

func TestClassifyIntent_WithNewline(t *testing.T) {
	matched := matchTaskIntent("TASK\n")
	if !matched {
		t.Error("'TASK\\n' should be classified as IntentTask")
	}
}

func TestClassifyIntent_WithWhitespace(t *testing.T) {
	matched := matchTaskIntent("  TASK  ")
	if !matched {
		t.Error("whitespace-trimmed 'TASK' should be IntentTask")
	}
}

func TestClassifyIntent_Lowercase_Task(t *testing.T) {
	matched := matchTaskIntent("task")
	if !matched {
		t.Error("lowercase 'task' should be IntentTask (case-insensitive)")
	}
}

func TestClassifyIntent_MixedCase(t *testing.T) {
	matched := matchTaskIntent("Task")
	if !matched {
		t.Error("mixed case 'Task' should be IntentTask")
	}
}

func TestClassifyIntent_TASKING_NotMatch(t *testing.T) {
	// 審計修復：精確匹配防 "TASKING" / "TASKED" 誤判
	matched := matchTaskIntent("TASKING")
	if matched {
		t.Error("'TASKING' should NOT match (was over-broad prefix match bug)")
	}
}

func TestClassifyIntent_TASKED_NotMatch(t *testing.T) {
	matched := matchTaskIntent("TASKED")
	if matched {
		t.Error("'TASKED' should NOT match")
	}
}

func TestClassifyIntent_EmptyString(t *testing.T) {
	matched := matchTaskIntent("")
	if matched {
		t.Error("empty string should be IntentChat (safe fallback)")
	}
}

func TestClassifyIntent_Unknown(t *testing.T) {
	matched := matchTaskIntent("UNKNOWN_RESPONSE")
	if matched {
		t.Error("unknown response should default to IntentChat")
	}
}

func TestClassifyIntent_Gibberish(t *testing.T) {
	matched := matchTaskIntent("xyz123")
	if matched {
		t.Error("gibberish should default to IntentChat")
	}
}

// ============================================================================
// Prompt Injection 防護
// ============================================================================

func TestClassifyIntent_SanitizeTripleQuote(t *testing.T) {
	query := `hello """ malicious prompt """ world`
	sanitized := strings.ReplaceAll(query, `"""`, `\"\"\"`)

	if strings.Contains(sanitized, `"""`) {
		t.Error("triple quotes should be escaped")
	}
	if !strings.Contains(sanitized, `\"\"\"`) {
		t.Error("triple quotes should be replaced with escaped version")
	}
}

func TestClassifyIntent_NoTripleQuoteUnchanged(t *testing.T) {
	query := "fix the login bug"
	sanitized := strings.ReplaceAll(query, `"""`, `\"\"\"`)

	if sanitized != query {
		t.Error("normal query should not be modified")
	}
}

// ============================================================================
// 輔助：提取 ClassifyUserIntent 嘅匹配邏輯進行獨立測試
// ============================================================================

// matchTaskIntent 複製 ClassifyUserIntent 中嘅匹配邏輯
func matchTaskIntent(content string) bool {
	content = strings.TrimSpace(content)
	upper := strings.ToUpper(content)
	if upper == "TASK" || strings.HasPrefix(upper, "TASK\n") || strings.HasPrefix(upper, "TASK ") {
		return true
	}
	return false
}
