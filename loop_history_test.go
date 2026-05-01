package main

import "testing"

// ============================================================================
// RunHistoryCompression trigger mode tests
// ============================================================================
// These tests verify the compression mode switch logic (token vs message count).
// They intentionally do NOT trigger the full compression pipeline (Phase 1/2/3)
// to avoid LLM API calls. The full compression pipeline is tested separately
// in context_compressor_test.go.

func saveAndRestoreCompressionGlobals() func() {
	oldMode := globalCompressionMode
	oldThreshold := globalCompressionThreshold
	return func() {
		globalCompressionMode = oldMode
		globalCompressionThreshold = oldThreshold
	}
}

// ============================================================================
// Token mode — trigger via estimated token count vs context window
// ============================================================================

func TestRunHistoryCompression_TokenMode_UnderThreshold_ReturnsEarly(t *testing.T) {
	restore := saveAndRestoreCompressionGlobals()
	defer restore()

	globalCompressionMode = "token"
	globalCompressionThreshold = 0.8
	// contextWindow=4096 (empty modelID default), threshold=0.8 → 3276 tokens
	// 3 short messages ≈ 50 tokens → well under threshold → early return

	compressor := NewContextCompressor()
	msgs := []Message{
		makeMsg("system", "You are helpful"),
		makeMsg("user", "hi"),
		makeMsg("assistant", "hello"),
	}
	originalLen := len(msgs)

	result := RunHistoryCompression(msgs, "", compressor)

	if len(result) != originalLen {
		t.Errorf("token mode under threshold: got %d messages, want %d (early return)", len(result), originalLen)
	}
}

func TestRunHistoryCompression_TokenMode_LargeContextWindow_StaysUnderThreshold(t *testing.T) {
	restore := saveAndRestoreCompressionGlobals()
	defer restore()

	globalCompressionMode = "token"
	globalCompressionThreshold = 0.8

	compressor := NewContextCompressor()
	// "gpt-4" has a known large context window in the database (8192+)
	// A few short messages should be well under 80% of the window
	msgs := []Message{
		makeMsg("system", "You are helpful"),
		makeMsg("user", "tell me about AI"),
		makeMsg("assistant", "AI stands for Artificial Intelligence."),
	}
	originalLen := len(msgs)

	result := RunHistoryCompression(msgs, "gpt-4", compressor)

	if len(result) != originalLen {
		t.Errorf("token mode large context window: got %d messages, want %d (early return)", len(result), originalLen)
	}
}

// ============================================================================
// Message mode — trigger via message count vs adaptiveMaxHistory
// ============================================================================

func TestRunHistoryCompression_MessageMode_UnderLimit_ReturnsEarly(t *testing.T) {
	restore := saveAndRestoreCompressionGlobals()
	defer restore()

	globalCompressionMode = "message"

	compressor := NewContextCompressor()
	// adaptiveMaxHistory for empty model (4096 context) is ~3
	// 2 messages < 3 → early return
	msgs := []Message{
		makeMsg("system", "You are helpful"),
		makeMsg("user", "hello"),
	}
	originalLen := len(msgs)

	result := RunHistoryCompression(msgs, "", compressor)

	if len(result) != originalLen {
		t.Errorf("message mode under limit: got %d messages, want %d (early return)", len(result), originalLen)
	}
}

// ============================================================================
// Default mode behavior (empty/unset → message count)
// ============================================================================

func TestRunHistoryCompression_DefaultMode_BehavesAsMessageCount(t *testing.T) {
	restore := saveAndRestoreCompressionGlobals()
	defer restore()

	// Unset mode → default case → message count logic
	globalCompressionMode = ""

	compressor := NewContextCompressor()
	msgs := []Message{
		makeMsg("system", "You are helpful"),
		makeMsg("user", "hello"),
	}
	originalLen := len(msgs)

	result := RunHistoryCompression(msgs, "", compressor)

	if len(result) != originalLen {
		t.Errorf("default mode (empty) under limit: got %d messages, want %d (early return)", len(result), originalLen)
	}
}

// ============================================================================
// Edge cases
// ============================================================================

func TestRunHistoryCompression_EmptyMessages(t *testing.T) {
	restore := saveAndRestoreCompressionGlobals()
	defer restore()

	globalCompressionMode = "token"
	globalCompressionThreshold = 0.8

	compressor := NewContextCompressor()
	result := RunHistoryCompression([]Message{}, "", compressor)

	if len(result) != 0 {
		t.Errorf("empty messages should return empty: got %d", len(result))
	}
}

func TestRunHistoryCompression_SingleSystemMessage(t *testing.T) {
	restore := saveAndRestoreCompressionGlobals()
	defer restore()

	globalCompressionMode = "token"
	globalCompressionThreshold = 0.8

	compressor := NewContextCompressor()
	msgs := []Message{makeMsg("system", "You are helpful")}

	result := RunHistoryCompression(msgs, "", compressor)

	if len(result) != 1 {
		t.Errorf("single system message should remain: got %d", len(result))
	}
}

func TestRunHistoryCompression_BothModes_SkipCompressionForShortConversation(t *testing.T) {
	compressor := NewContextCompressor()
	msgs := []Message{
		makeMsg("system", "You are helpful"),
		makeMsg("user", "hi"),
	}

	// Token mode
	func() {
		restore := saveAndRestoreCompressionGlobals()
		defer restore()
		globalCompressionMode = "token"
		globalCompressionThreshold = 0.8
		result := RunHistoryCompression(msgs, "", compressor)
		if len(result) != len(msgs) {
			t.Errorf("token mode: got %d, want %d", len(result), len(msgs))
		}
	}()

	// Message mode
	func() {
		restore := saveAndRestoreCompressionGlobals()
		defer restore()
		globalCompressionMode = "message"
		result := RunHistoryCompression(msgs, "", compressor)
		if len(result) != len(msgs) {
			t.Errorf("message mode: got %d, want %d", len(result), len(msgs))
		}
	}()
}
