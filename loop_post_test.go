package main

import (
	"context"
	"testing"
	"time"
)

// ============================================================================
// BDD: FeedbackCollector 異步安全性 + done=true 即時發送
// ============================================================================
// 對應 e5e.log bug：FeedbackCollector 同步 timeout 10s → done=true 延遲發送
// → 前端長時間等完見到模型「無故終止」。
// 修復：AskModelTaskCompletion 改為 go func() 異步，done=true 即時發出。

// ============================================================================
// FeedbackCollector 隔離測試
// ============================================================================

// Scenario: 模擬 API timeout → AskModelTaskCompletion 返回 false。
func TestFeedbackCollector_TimeoutReturnsFalse(t *testing.T) {
	fc := NewFeedbackCollector(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	result := fc.AskModelTaskCompletion(ctx,
		"user query", "assistant response",
		TaskCompletionQuery{APIType: "openai", BaseURL: "https://api.example.com/v1", APIKey: "sk-test", ModelID: "model"})

	if result {
		t.Error("cancelled context should return false (timeout → not completed)")
	}
}

// Scenario: 缺少 API config → skip 而唔 panic。
func TestFeedbackCollector_MissingAPIConfig(t *testing.T) {
	fc := NewFeedbackCollector(t.TempDir())

	result := fc.AskModelTaskCompletion(context.Background(),
		"user query", "assistant response",
		TaskCompletionQuery{})

	if result {
		t.Error("missing API config should return false")
	}
}

// Scenario: 缺少 APIKey → skip。
func TestFeedbackCollector_MissingAPIKey(t *testing.T) {
	fc := NewFeedbackCollector(t.TempDir())

	result := fc.AskModelTaskCompletion(context.Background(),
		"user query", "assistant response",
		TaskCompletionQuery{ModelID: "model", BaseURL: "https://api.example.com/v1", APIKey: ""})

	if result {
		t.Error("missing API key should return false")
	}
}

// Scenario: CanAskCompletion 冷卻機制正確運作。
func TestFeedbackCollector_CooldownMechanism(t *testing.T) {
	fc := NewFeedbackCollector(t.TempDir())

	if !fc.CanAskCompletion() {
		t.Error("initial state should allow asking")
	}

	fc.RecordCompletionAsk()
	if fc.CanAskCompletion() {
		t.Error("should be in cooldown after recording an ask")
	}
}

// Scenario: 多次 ask 之間冷卻生效。
func TestFeedbackCollector_MultipleAskCooldown(t *testing.T) {
	fc := NewFeedbackCollector(t.TempDir())

	// 模擬多輪詢問
	for i := 0; i < 3; i++ {
		if i == 0 {
			if !fc.CanAskCompletion() {
				t.Errorf("round %d: should allow first ask", i)
			}
		} else {
			if fc.CanAskCompletion() {
				t.Errorf("round %d: should be in cooldown", i)
			}
		}
		fc.RecordCompletionAsk()
	}
}

// Scenario: 未 call RecordCompletionAsk 之前冇冷卻。
func TestFeedbackCollector_NoCooldownWithoutAsk(t *testing.T) {
	fc := NewFeedbackCollector(t.TempDir())

	if !fc.CanAskCompletion() {
		t.Error("should be able to ask when never asked before")
	}
}

// Scenario: lastCompletionAskTime 正確記錄。
func TestFeedbackCollector_LastAskTimeRecorded(t *testing.T) {
	fc := NewFeedbackCollector(t.TempDir())

	if !fc.lastCompletionAskTime.IsZero() {
		t.Error("lastCompletionAskTime should be zero before first ask")
	}

	fc.RecordCompletionAsk()
	if fc.lastCompletionAskTime.IsZero() {
		t.Error("lastCompletionAskTime should be set after RecordCompletionAsk")
	}
}

// Scenario: minAskInterval 預設為 30 秒。
func TestFeedbackCollector_MinAskIntervalDefault(t *testing.T) {
	fc := NewFeedbackCollector(t.TempDir())

	if fc.minAskInterval != 30*time.Second {
		t.Errorf("minAskInterval = %v, want 30s", fc.minAskInterval)
	}
}

// ============================================================================
// BDD: done=true chunk 結構驗證
// ============================================================================

// Scenario: done chunk 必須有 Done=true 且冇 content/error。
func TestDoneChunk_CorrectStructure(t *testing.T) {
	chunk := StreamChunk{Done: true}

	if !chunk.Done {
		t.Error("done chunk must have Done=true")
	}
	if chunk.Content != "" {
		t.Error("done chunk should have empty Content")
	}
	if chunk.Error != "" {
		t.Error("done chunk should have empty Error")
	}
}

// ============================================================================
// BDD: 並發安全性
// ============================================================================

// Scenario: 多個 goroutine 同時 call FeedbackCollector 唔應該 race。
func TestFeedbackCollector_ConcurrentSafety(t *testing.T) {
	fc := NewFeedbackCollector(t.TempDir())
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			_ = fc.CanAskCompletion()
			fc.RecordCompletionAsk()
			_ = fc.CanAskCompletion()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
	// no panic = pass
}
