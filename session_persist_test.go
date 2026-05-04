package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupSessionTestDB 创建临时 SQLite DB + AutoMigrate Session + SessionMessage + 设置 globalDB
func setupSessionTestDB(t *testing.T) (*gorm.DB, *SessionPersistManager, string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "ghostclaw.db")

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}

	// 启用 WAL 和 foreign keys
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")

	if err := db.AutoMigrate(&SessionHistory{}, &SessionMessage{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	// 设置全局 DB
	oldDB := globalDB
	globalDB = db
	t.Cleanup(func() {
		globalDB = oldDB
	})

	mgr := NewSessionPersistManager()
	return db, mgr, tmpDir
}

// makeTestMessage 创建一个简单的测试消息
func makeTestMessage(role, content string, timestamp int64) Message {
	return Message{
		Role:      role,
		Content:   content,
		Timestamp: timestamp,
	}
}

// makeToolCallMessage 创建一个 tool_call 消息
func makeToolCallMessage(role, toolCallID string, content interface{}, toolCalls interface{}, timestamp int64) Message {
	return Message{
		Role:       role,
		Content:    content,
		ToolCallID: toolCallID,
		ToolCalls:  toolCalls,
		Timestamp:  timestamp,
	}
}

// makeReasoningMessage 创建一个带 reasoning 的消息
func makeReasoningMessage(role, content string, reasoningContent interface{}, thinkingSig string, timestamp int64) Message {
	return Message{
		Role:              role,
		Content:           content,
		ReasoningContent:  reasoningContent,
		ThinkingSignature: thinkingSig,
		Timestamp:         timestamp,
	}
}

// ============================================================
// TestSaveAndLoadSession — SaveSession → LoadSession，验证所有字段完整来回
// ============================================================
func TestSaveAndLoadSession(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	messages := []Message{
		makeTestMessage("user", "Hello", now),
		makeTestMessage("assistant", "Hi there!", now+1),
		makeTestMessage("user", "How are you?", now+2),
		makeTestMessage("assistant", "I'm doing well, thank you!", now+3),
	}

	saved, err := mgr.SaveSession("test_session_1", "Test Session", "helper", "default", 100, 200, 300, 2, messages)
	if err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}
	if saved == nil {
		t.Fatal("SaveSession returned nil")
	}
	if saved.ID != "test_session_1" {
		t.Errorf("expected ID 'test_session_1', got '%s'", saved.ID)
	}
	if saved.Description != "Test Session" {
		t.Errorf("expected description 'Test Session', got '%s'", saved.Description)
	}
	if saved.Role != "helper" {
		t.Errorf("expected role 'helper', got '%s'", saved.Role)
	}
	if saved.InputTokens != 100 {
		t.Errorf("expected input_tokens 100, got %d", saved.InputTokens)
	}
	if saved.OutputTokens != 200 {
		t.Errorf("expected output_tokens 200, got %d", saved.OutputTokens)
	}
	if saved.TotalTokens != 300 {
		t.Errorf("expected total_tokens 300, got %d", saved.TotalTokens)
	}
	if saved.TurnCount != 2 {
		t.Errorf("expected turn_count 2, got %d", saved.TurnCount)
	}
	if len(saved.History) != 4 {
		t.Errorf("expected 4 messages, got %d", len(saved.History))
	}

	// 加载并验证
	loaded, err := mgr.LoadSession("test_session_1")
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadSession returned nil")
	}
	if loaded.ID != "test_session_1" {
		t.Errorf("loaded ID mismatch: got '%s'", loaded.ID)
	}
	if loaded.Description != "Test Session" {
		t.Errorf("loaded description mismatch: got '%s'", loaded.Description)
	}
	if loaded.InputTokens != 100 || loaded.OutputTokens != 200 || loaded.TotalTokens != 300 || loaded.TurnCount != 2 {
		t.Errorf("loaded token stats mismatch: input=%d output=%d total=%d turns=%d",
			loaded.InputTokens, loaded.OutputTokens, loaded.TotalTokens, loaded.TurnCount)
	}
	if len(loaded.History) != 4 {
		t.Errorf("expected 4 loaded messages, got %d", len(loaded.History))
	}

	// 验证消息内容
	for i, msg := range loaded.History {
		content, ok := msg.Content.(string)
		if !ok {
			t.Errorf("message %d content is not string", i)
			continue
		}
		expectedContent, _ := messages[i].Content.(string)
		if content != expectedContent {
			t.Errorf("message %d content mismatch: expected '%s', got '%s'", i, expectedContent, content)
		}
		if msg.Role != messages[i].Role {
			t.Errorf("message %d role mismatch: expected '%s', got '%s'", i, messages[i].Role, msg.Role)
		}
	}
}

// ============================================================
// TestSaveMessages — SaveMessages 批量写入 → LoadSession 验证顺序
// ============================================================
func TestSaveMessages(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	// 先保存 session 元数据
	globalDB.Save(&SessionHistory{
		ID:          "msg_test_session",
		Description: "Messages Test",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	})

	// 批量写入消息
	now := time.Now().Unix()
	messages := []Message{
		makeTestMessage("user", "Question 1", now),
		makeTestMessage("assistant", "Answer 1", now+1),
		makeTestMessage("user", "Question 2", now+2),
		makeTestMessage("assistant", "Answer 2", now+3),
	}

	if err := mgr.SaveMessages("msg_test_session", messages); err != nil {
		t.Fatalf("SaveMessages failed: %v", err)
	}

	// 验证顺序
	loaded, err := mgr.LoadSession("msg_test_session")
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	if len(loaded.History) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(loaded.History))
	}

	for i, msg := range loaded.History {
		expected, _ := messages[i].Content.(string)
		actual, _ := msg.Content.(string)
		if expected != actual {
			t.Errorf("message %d: expected '%s', got '%s'", i, expected, actual)
		}
	}

	// 批量替换消息（只有 2 条新消息）
	newMessages := []Message{
		makeTestMessage("user", "New Q1", now+10),
		makeTestMessage("assistant", "New A1", now+11),
	}
	if err := mgr.SaveMessages("msg_test_session", newMessages); err != nil {
		t.Fatalf("SaveMessages (replace) failed: %v", err)
	}

	loaded2, _ := mgr.LoadSession("msg_test_session")
	if len(loaded2.History) != 2 {
		t.Errorf("expected 2 messages after replace, got %d", len(loaded2.History))
	}
}

// ============================================================
// TestAppendMessage — AppendMessage 单条追加 → 验证 seq 正确
// ============================================================
func TestAppendMessage(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	// 保存 session 元数据
	globalDB.Save(&SessionHistory{
		ID:          "append_test",
		Description: "Append Test",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	})

	now := time.Now().Unix()
	// 先批量写入 2 条消息
	mgr.SaveMessages("append_test", []Message{
		makeTestMessage("user", "First", now),
		makeTestMessage("assistant", "Second", now+1),
	})

	// 追加单条消息
	msg3 := makeTestMessage("user", "Third", now+2)
	if err := mgr.AppendMessage("append_test", msg3); err != nil {
		t.Fatalf("AppendMessage failed: %v", err)
	}

	// 再追加一条
	msg4 := makeTestMessage("assistant", "Fourth", now+3)
	if err := mgr.AppendMessage("append_test", msg4); err != nil {
		t.Fatalf("AppendMessage 2 failed: %v", err)
	}

	// 验证
	loaded, err := mgr.LoadSession("append_test")
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	if len(loaded.History) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(loaded.History))
	}

	expectedContents := []string{"First", "Second", "Third", "Fourth"}
	for i, msg := range loaded.History {
		content, _ := msg.Content.(string)
		if content != expectedContents[i] {
			t.Errorf("message %d: expected '%s', got '%s'", i, expectedContents[i], content)
		}
	}
}

// ============================================================
// TestListSessions — 多个 sessions → ListSessions 按 updated_at 排序
// ============================================================
func TestListSessions(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	// 创建多个 sessions
	now := time.Now().Unix()
	for i := 0; i < 3; i++ {
		mgr.SaveSession(
			fmt.Sprintf("list_session_%d", i),
			fmt.Sprintf("Session %d", i),
			"helper", "default",
			0, 0, 0, 0,
			[]Message{makeTestMessage("user", fmt.Sprintf("msg %d", i), now+int64(i*10))},
		)
		time.Sleep(10 * time.Millisecond) // 确保 updated_at 有差异
	}

	sessions, err := mgr.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(sessions))
	}

	// 验证按 updated_at DESC 排序
	for i := 1; i < len(sessions); i++ {
		if sessions[i-1].UpdatedAt.Before(sessions[i].UpdatedAt) {
			t.Errorf("sessions not sorted by updated_at DESC: %s < %s",
				sessions[i-1].UpdatedAt, sessions[i].UpdatedAt)
		}
	}

	// 验证消息未被加载（ListSessions 不加载消息）
	for _, s := range sessions {
		if len(s.History) != 0 {
			t.Errorf("ListSessions should not load messages, but session %s has %d messages", s.ID, len(s.History))
		}
	}
}

// ============================================================
// TestDeleteSession — DeleteSession → LoadSession 返 nil → 消息也被删除
// ============================================================
func TestDeleteSession(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	mgr.SaveSession("delete_test", "To be deleted", "helper", "default", 0, 0, 0, 0,
		[]Message{
			makeTestMessage("user", "Hello", now),
			makeTestMessage("assistant", "Hi", now+1),
		})

	// 确认存在
	loaded, _ := mgr.LoadSession("delete_test")
	if loaded == nil {
		t.Fatal("session should exist before delete")
	}

	// 删除
	if err := mgr.DeleteSession("delete_test"); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	// 确认不存在
	loaded, err := mgr.LoadSession("delete_test")
	if err != nil {
		t.Fatalf("LoadSession after delete returned error: %v", err)
	}
	if loaded != nil {
		t.Error("LoadSession should return nil after delete")
	}

	// 确认消息也被删除
	var count int64
	globalDB.Model(&SessionMessage{}).Where("session_id = ?", "delete_test").Count(&count)
	if count != 0 {
		t.Errorf("expected 0 messages after delete, got %d", count)
	}
}

// ============================================================
// TestUpdateSession — UpdateSession → 验证元数据更新、消息替换
// ============================================================
func TestUpdateSession(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	mgr.SaveSession("update_test", "Original Desc", "helper", "default", 10, 20, 30, 1,
		[]Message{
			makeTestMessage("user", "Original Q", now),
			makeTestMessage("assistant", "Original A", now+1),
		})

	// 模拟 tracker 存在（设置 tracker stats 会被 UpdateSession 读取）
	// 由于 UpdateSession 会调用 GetGlobalSession().GetTracker()，
	// 在测试中 tracker 可能是 nil，我们先验证消息替换
	newMessages := []Message{
		makeTestMessage("user", "Updated Q", now+10),
		makeTestMessage("assistant", "Updated A", now+11),
		makeTestMessage("user", "Updated Q2", now+12),
	}
	if err := mgr.UpdateSession("update_test", newMessages); err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}

	loaded, _ := mgr.LoadSession("update_test")
	if len(loaded.History) != 3 {
		t.Errorf("expected 3 messages after update, got %d", len(loaded.History))
	}
	content, _ := loaded.History[0].Content.(string)
	if content != "Updated Q" {
		t.Errorf("expected first message 'Updated Q', got '%s'", content)
	}
}

// ============================================================
// TestSessionID_NoDefault — 新 session ID 为 timestamp 格式，非 "default"
// ============================================================
func TestSessionID_NoDefault(t *testing.T) {
	s := newGlobalSession()
	if s.ID == "default" {
		t.Error("newGlobalSession ID should not be 'default'")
	}
	// 验证是 timestamp 格式 20060102_150405
	if len(s.ID) != 15 {
		t.Errorf("expected timestamp format (15 chars), got '%s' (%d chars)", s.ID, len(s.ID))
	}
	// 验证可以 parse 为时间
	_, err := time.Parse("20060102_150405", s.ID)
	if err != nil {
		t.Errorf("session ID '%s' is not valid timestamp format: %v", s.ID, err)
	}
}

// ============================================================
// TestLoadRecentMessages — 写入 100 条消息 → LoadRecentMessages(10) 返最近 10 条
// ============================================================
func TestLoadRecentMessages(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	globalDB.Save(&SessionHistory{
		ID:          "recent_test",
		Description: "Recent Test",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	})

	// 写入 100 条消息
	now := time.Now().Unix()
	var allMessages []Message
	for i := 0; i < 100; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		allMessages = append(allMessages, makeTestMessage(role, fmt.Sprintf("Message %03d", i), now+int64(i)))
	}
	mgr.SaveMessages("recent_test", allMessages)

	// 加载最近 10 条
	recent, err := mgr.LoadRecentMessages("recent_test", 10)
	if err != nil {
		t.Fatalf("LoadRecentMessages failed: %v", err)
	}
	if len(recent) != 10 {
		t.Errorf("expected 10 messages, got %d", len(recent))
	}

	// 验证是最后 10 条 (seq 90-99)
	for i, msg := range recent {
		content, _ := msg.Content.(string)
		expected := fmt.Sprintf("Message %03d", 90+i)
		if content != expected {
			t.Errorf("recent message %d: expected '%s', got '%s'", i, expected, content)
		}
	}
}

// ============================================================
// TestLoadSessionWindow — 滑窗載入：寫入 200 條 → LoadSessionWindow(50) 只返最近 50 條 + 元數據
// ============================================================
func TestLoadSessionWindow(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	var messages []Message
	for i := 0; i < 200; i++ {
		messages = append(messages, makeTestMessage("user", fmt.Sprintf("Msg %03d", i), now+int64(i)))
	}
	mgr.SaveSession("window_test", "Window Test", "helper", "default", 500, 1000, 1500, 100, messages)

	// 滑窗載入最近 50 條
	saved, err := mgr.LoadSessionWindow("window_test", 50)
	if err != nil {
		t.Fatalf("LoadSessionWindow failed: %v", err)
	}
	if saved == nil {
		t.Fatal("LoadSessionWindow returned nil")
	}

	// 驗證元數據完整
	if saved.ID != "window_test" {
		t.Errorf("expected ID 'window_test', got '%s'", saved.ID)
	}
	if saved.InputTokens != 500 || saved.TotalTokens != 1500 {
		t.Errorf("token stats mismatch: input=%d total=%d", saved.InputTokens, saved.TotalTokens)
	}

	// 驗證只載入 50 條（最近嘅）
	if len(saved.History) != 50 {
		t.Fatalf("expected 50 messages in window, got %d", len(saved.History))
	}

	// 驗證係最後 50 條（seq 150-199）
	for i, msg := range saved.History {
		content, _ := msg.Content.(string)
		expected := fmt.Sprintf("Msg %03d", 150+i)
		if content != expected {
			t.Errorf("window msg %d: expected '%s', got '%s'", i, expected, content)
		}
	}
}

// ============================================================
// TestRestartLoadsLatestSession — 模擬重啟：LoadSessionWindow("") 載入最新 session
// ============================================================
func TestRestartLoadsLatestSession(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()

	// 創建第一個 session（模擬舊 session）
	mgr.SaveSession("20260503_100000", "Old session", "helper", "default", 0, 0, 0, 0,
		[]Message{makeTestMessage("user", "Old message", now)})
	time.Sleep(10 * time.Millisecond)

	// 創建第二個 session（模擬最近一次運行）
	mgr.SaveSession("20260504_120000", "Latest session", "helper", "default", 100, 200, 300, 5,
		[]Message{
			makeTestMessage("user", "Latest message 1", now+10),
			makeTestMessage("assistant", "Latest reply 1", now+11),
			makeTestMessage("user", "Latest message 2", now+12),
		})

	// 模擬重啟：LoadSessionWindow("") → 應該載入最新 session
	saved, err := mgr.LoadSessionWindow("", 128)
	if err != nil {
		t.Fatalf("LoadSessionWindow('') failed: %v", err)
	}
	if saved == nil {
		t.Fatal("LoadSessionWindow('') returned nil")
	}
	if saved.ID != "20260504_120000" {
		t.Errorf("restart should load latest session, got '%s'", saved.ID)
	}
	if len(saved.History) != 3 {
		t.Errorf("expected 3 messages from latest session, got %d", len(saved.History))
	}
	// token stats 應該來自最新 session
	if saved.TotalTokens != 300 {
		t.Errorf("expected total_tokens 300 from latest session, got %d", saved.TotalTokens)
	}
}

// ============================================================
// TestLoadSessionFull — LoadSession 載入完整歷史（唔受滑窗限制）
// ============================================================
func TestLoadSessionFull(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	var messages []Message
	for i := 0; i < 200; i++ {
		messages = append(messages, makeTestMessage("user", fmt.Sprintf("Full %03d", i), now+int64(i)))
	}
	mgr.SaveSession("full_test", "Full Load Test", "helper", "default", 0, 0, 0, 0, messages)

	// LoadSession 應該載入全部 200 條
	saved, err := mgr.LoadSession("full_test")
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	if len(saved.History) != 200 {
		t.Errorf("LoadSession should load all 200 messages, got %d", len(saved.History))
	}
}

// ============================================================
// TestLoadSessionWindowByTokens — token 滑窗：寫入大量消息，驗證 token 累加退後
// ============================================================
func TestLoadSessionWindowByTokens(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	// 寫入 100 條消息，每條內容長度不同（產生唔同 token 估算）
	var messages []Message
	for i := 0; i < 100; i++ {
		// 遞增內容長度：前半短、後半長
		content := fmt.Sprintf("Msg %03d", i)
		if i >= 50 {
			content = fmt.Sprintf("Message number %03d with additional content to increase token count significantly for testing purposes", i)
		}
		messages = append(messages, makeTestMessage("user", content, now+int64(i)))
	}
	mgr.SaveSession("token_window_test", "Token Window", "helper", "default", 0, 0, 0, 0, messages)

	// 用一個好細嘅 maxTokens（例如 50 token）載入 → 只會攞到少量最近消息
	saved, err := mgr.LoadSessionWindowByTokens("token_window_test", 50)
	if err != nil {
		t.Fatalf("LoadSessionWindowByTokens failed: %v", err)
	}
	if saved == nil {
		t.Fatal("LoadSessionWindowByTokens returned nil")
	}

	// 應該至少有一條消息
	if len(saved.History) == 0 {
		t.Error("token window should return at least 1 message")
	}

	// 所有返回嘅消息應該係最近嘅（即 seq 最大嗰批）
	// 反轉驗證係 seq 遞增
	for i := 1; i < len(saved.History); i++ {
		if saved.History[i].Timestamp < saved.History[i-1].Timestamp {
			t.Errorf("messages should be in chronological order at index %d", i)
		}
	}
}

// ============================================================
// TestLoadSessionWindowByTokensThreshold — 模擬 80% 閾值行為
// ============================================================
func TestLoadSessionWindowByTokensThreshold(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	var messages []Message
	// 每條消息約 80-100 token（長內容）
	longContent := "This is a very long message designed to consume approximately eighty to one hundred tokens for testing the sliding window threshold behavior in token mode. " +
		"It contains sufficient text to ensure that the token estimator produces a meaningful count that can be accumulated and compared against the configured maximum."
	for i := 0; i < 50; i++ {
		messages = append(messages, makeTestMessage("user", fmt.Sprintf("%s [msg %03d]", longContent, i), now+int64(i)))
	}
	mgr.SaveSession("threshold_test", "Threshold Test", "helper", "default", 0, 0, 0, 0, messages)

	// 每條約 80 token，50 條 = 4000 token
	// 設 maxTokens=2000 → 應該只載入約 25 條（最近嘅 25 條）
	estPerMsg := ImprovedEstimateTokens(longContent)
	t.Logf("Estimated tokens per message: %d", estPerMsg)

	saved, err := mgr.LoadSessionWindowByTokens("threshold_test", 2000)
	if err != nil {
		t.Fatalf("LoadSessionWindowByTokens failed: %v", err)
	}

	// 應該少過 50 條
	if len(saved.History) >= 50 {
		t.Errorf("token window should truncate messages: expected <50, got %d", len(saved.History))
	}
	if len(saved.History) == 0 {
		t.Error("token window should return at least some messages")
	}

	// 載入嘅應該係最近嘅消息
	firstContent, _ := saved.History[0].Content.(string)
	lastContent, _ := saved.History[len(saved.History)-1].Content.(string)
	t.Logf("Window: %d messages, first=%q, last=%q", len(saved.History), firstContent, lastContent)

	// 最後一條應該係 "msg 049"（最新）
	if len(saved.History) > 0 {
		lastMsg := saved.History[len(saved.History)-1]
		lc, _ := lastMsg.Content.(string)
		if lc[len(lc)-7:] != "[msg 049]" {
			t.Logf("last message is: %q (expected ending with [msg 049])", lc)
		}
	}
}

// ============================================================
// TestTokenCountPersisted — 驗證 TokenCount 正確寫入 DB
// ============================================================
func TestTokenCountPersisted(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	content := "This is a test message with some content to estimate tokens"
	mgr.SaveSession("tokencount_test", "Token Count", "", "", 0, 0, 0, 0,
		[]Message{makeTestMessage("user", content, now)})

	// 直接從 DB 查 TokenCount
	var row SessionMessage
	if err := globalDB.Where("session_id = ?", "tokencount_test").First(&row).Error; err != nil {
		t.Fatalf("query message failed: %v", err)
	}

	if row.TokenCount <= 0 {
		t.Errorf("TokenCount should be > 0, got %d", row.TokenCount)
	}

	// 估算應該合理
	expectedMin := len(content) / 5
	if row.TokenCount < expectedMin {
		t.Errorf("TokenCount too low: got %d, expected at least %d", row.TokenCount, expectedMin)
	}

	t.Logf("Content: %q (%d chars) → TokenCount: %d", content, len(content), row.TokenCount)
}

// ============================================================
// TestExportImportSession — Export → Import → 验证消息完整来回
// ============================================================
// TestUpdateSessionMeta — 只更新元數據，唔影響消息
// ============================================================
func TestUpdateSessionMeta(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	mgr.SaveSession("meta_test", "Original Desc", "helper", "default", 10, 20, 30, 1,
		[]Message{
			makeTestMessage("user", "Keep this message", now),
			makeTestMessage("assistant", "Keep this too", now+1),
		})

	// 更新元數據（唔改消息）
	err := mgr.UpdateSessionMeta("meta_test", "Updated Desc", "coder", "dev", 100, 200, 300, 5)
	if err != nil {
		t.Fatalf("UpdateSessionMeta failed: %v", err)
	}

	// 加載驗證：元數據已更新
	loaded, _ := mgr.LoadSession("meta_test")
	if loaded.Description != "Updated Desc" {
		t.Errorf("expected description 'Updated Desc', got '%s'", loaded.Description)
	}
	if loaded.Role != "coder" {
		t.Errorf("expected role 'coder', got '%s'", loaded.Role)
	}
	if loaded.InputTokens != 100 || loaded.TotalTokens != 300 {
		t.Errorf("token stats not updated: input=%d total=%d", loaded.InputTokens, loaded.TotalTokens)
	}

	// 驗證消息冇被影響（仍然係原始 2 條）
	if len(loaded.History) != 2 {
		t.Errorf("messages should be unchanged: expected 2, got %d", len(loaded.History))
	}
	content, _ := loaded.History[0].Content.(string)
	if content != "Keep this message" {
		t.Errorf("message content changed: '%s'", content)
	}
}

// ============================================================
// TestExportImportSession — Export → Import → 验证消息完整来回
// ============================================================
func TestExportImportSession(t *testing.T) {
	_, mgr, tmpDir := setupSessionTestDB(t)

	now := time.Now().Unix()
	mgr.SaveSession("export_test", "Export Test Session", "helper", "default", 0, 0, 0, 0,
		[]Message{
			makeTestMessage("user", "Can you help?", now),
			makeTestMessage("assistant", "Sure, what do you need?", now+1),
			makeTestMessage("user", "Write a function", now+2),
		})

	// 导出
	exportPath := filepath.Join(tmpDir, "export_test")
	if err := mgr.ExportSession("export_test", exportPath); err != nil {
		t.Fatalf("ExportSession failed: %v", err)
	}

	// 验证导出文件存在
	exportFile := exportPath + ".toon"
	if _, err := os.Stat(exportFile); os.IsNotExist(err) {
		t.Fatal("export file was not created")
	}

	// 导入
	imported, err := mgr.ImportSession(exportFile)
	if err != nil {
		t.Fatalf("ImportSession failed: %v", err)
	}
	if imported == nil {
		t.Fatal("ImportSession returned nil")
	}
	if len(imported.History) != 3 {
		t.Errorf("expected 3 imported messages, got %d", len(imported.History))
	}

	// 验证导入的会话可以加载
	loaded, _ := mgr.LoadSession(imported.ID)
	if loaded == nil {
		t.Fatal("imported session not loadable")
	}
}

// ============================================================
// TestCascadeDelete — 删 session → session_messages 一齐被删
// ============================================================
func TestCascadeDelete(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	mgr.SaveSession("cascade_test", "Cascade Test", "helper", "default", 0, 0, 0, 0,
		[]Message{
			makeTestMessage("user", "Msg 1", now),
			makeTestMessage("assistant", "Msg 2", now+1),
			makeTestMessage("user", "Msg 3", now+2),
			makeTestMessage("assistant", "Msg 4", now+3),
			makeTestMessage("user", "Msg 5", now+4),
		})

	// 确认消息存在
	var count int64
	globalDB.Model(&SessionMessage{}).Where("session_id = ?", "cascade_test").Count(&count)
	if count != 5 {
		t.Fatalf("expected 5 messages before delete, got %d", count)
	}

	// 删除 session
	if err := mgr.DeleteSession("cascade_test"); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	// 确认消息也被删除
	globalDB.Model(&SessionMessage{}).Where("session_id = ?", "cascade_test").Count(&count)
	if count != 0 {
		t.Errorf("expected 0 messages after cascade delete, got %d", count)
	}
}

// ============================================================
// TestConcurrentMessageAppend — 并发 AppendMessage → 验证 seq 无冲突
// ============================================================
func TestConcurrentMessageAppend(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	globalDB.Save(&SessionHistory{
		ID:          "concurrent_test",
		Description: "Concurrent Test",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	})

	now := time.Now().Unix()
	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			msg := makeTestMessage("user", fmt.Sprintf("Concurrent %d", idx), now+int64(idx))
			if err := mgr.AppendMessage("concurrent_test", msg); err != nil {
				t.Errorf("AppendMessage %d failed: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	// 加载验证
	loaded, _ := mgr.LoadSession("concurrent_test")
	if len(loaded.History) != numGoroutines {
		t.Errorf("expected %d messages, got %d", numGoroutines, len(loaded.History))
	}

	// 验证 seq 无重复
	seen := make(map[int]bool)
	var rows []SessionMessage
	globalDB.Where("session_id = ?", "concurrent_test").Find(&rows)
	for _, row := range rows {
		if seen[row.Seq] {
			t.Errorf("duplicate seq found: %d", row.Seq)
		}
		seen[row.Seq] = true
	}
}

// ============================================================
// TestFullHistoryRoundTrip — 完整历史（128 条消息）→ Save → Load → 逐条对比
// ============================================================
func TestFullHistoryRoundTrip(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	var messages []Message
	for i := 0; i < 128; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages = append(messages, makeTestMessage(role, fmt.Sprintf("Message number %03d in the conversation", i), now+int64(i)))
	}

	saved, err := mgr.SaveSession("roundtrip_test", "Full Roundtrip", "helper", "default", 500, 1000, 1500, 64, messages)
	if err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}
	if len(saved.History) != 128 {
		t.Fatalf("expected 128 saved messages, got %d", len(saved.History))
	}

	loaded, err := mgr.LoadSession("roundtrip_test")
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	if len(loaded.History) != 128 {
		t.Fatalf("expected 128 loaded messages, got %d", len(loaded.History))
	}

	// 逐条对比
	for i := 0; i < 128; i++ {
		orig := messages[i]
		reloaded := loaded.History[i]

		if orig.Role != reloaded.Role {
			t.Errorf("message %d role mismatch: '%s' vs '%s'", i, orig.Role, reloaded.Role)
		}
		origContent, _ := orig.Content.(string)
		reloadedContent, _ := reloaded.Content.(string)
		if origContent != reloadedContent {
			t.Errorf("message %d content mismatch: '%s' vs '%s'", i, origContent, reloadedContent)
		}
	}

	// 验证 token stats
	if loaded.InputTokens != 500 || loaded.OutputTokens != 1000 || loaded.TotalTokens != 1500 || loaded.TurnCount != 64 {
		t.Errorf("token stats mismatch: input=%d output=%d total=%d turns=%d",
			loaded.InputTokens, loaded.OutputTokens, loaded.TotalTokens, loaded.TurnCount)
	}
}

// ============================================================
// TestMessageSerialization — 各种 Message 类型序列化/反序列化
// ============================================================
func TestMessageSerialization(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	messages := []Message{
		// 1. 纯文本消息
		makeTestMessage("user", "Plain text message", now),

		// 2. 复杂内容 (JSON array)
		{
			Role: "assistant",
			Content: []interface{}{
				map[string]interface{}{"type": "text", "text": "Hello"},
				map[string]interface{}{"type": "image_url", "image_url": map[string]string{"url": "http://example.com/img.jpg"}},
			},
			Timestamp: now + 1,
		},

		// 3. Tool call 消息
		makeToolCallMessage("assistant", "call_123", "Using tool...", []interface{}{
			map[string]interface{}{"id": "call_123", "function": map[string]interface{}{"name": "read_file", "arguments": `{"path":"/tmp/test"}`}},
		}, now+2),

		// 4. Tool result 消息
		makeToolCallMessage("tool", "call_123", "File contents here", nil, now+3),

		// 5. Reasoning 消息
		makeReasoningMessage("assistant", "Final answer", "Let me think about this...", "sig_abc", now+4),
	}

	_, err := mgr.SaveSession("serial_test", "Serialization Test", "helper", "default", 0, 0, 0, 0, messages)
	if err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	loaded, _ := mgr.LoadSession("serial_test")
	if len(loaded.History) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(loaded.History))
	}

	// 验证 tool calls
	msg2 := loaded.History[2]
	if msg2.ToolCallID != "call_123" {
		t.Errorf("expected tool_call_id 'call_123', got '%s'", msg2.ToolCallID)
	}
	if msg2.ToolCalls == nil {
		t.Error("tool_calls should not be nil")
	}

	// 验证 tool result
	msg3 := loaded.History[3]
	if msg3.ToolCallID != "call_123" {
		t.Errorf("expected tool_call_id 'call_123', got '%s'", msg3.ToolCallID)
	}

	// 验证 reasoning
	msg4 := loaded.History[4]
	if msg4.ThinkingSignature != "sig_abc" {
		t.Errorf("expected thinking_signature 'sig_abc', got '%s'", msg4.ThinkingSignature)
	}
	rc, ok := msg4.ReasoningContent.(string)
	if !ok || rc != "Let me think about this..." {
		t.Errorf("expected reasoning_content 'Let me think about this...', got '%v'", msg4.ReasoningContent)
	}
}

// ============================================================
// TestEmptySession — 空会话 Save/Load 不报错
// ============================================================
func TestEmptySession(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	// 保存空会话
	saved, err := mgr.SaveSession("empty_test", "Empty Session", "", "", 0, 0, 0, 0, []Message{})
	if err != nil {
		t.Fatalf("SaveSession with empty messages failed: %v", err)
	}
	if saved == nil {
		t.Fatal("SaveSession returned nil for empty session")
	}

	// 加载空会话
	loaded, err := mgr.LoadSession("empty_test")
	if err != nil {
		t.Fatalf("LoadSession for empty session failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadSession returned nil for empty session")
	}
	if len(loaded.History) != 0 {
		t.Errorf("expected 0 messages, got %d", len(loaded.History))
	}
}

// ============================================================
// TestListSessions_Empty — 冇 sessions 时 ListSessions 返空 slice
// ============================================================
func TestListSessions_Empty(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	sessions, err := mgr.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if sessions == nil {
		t.Error("ListSessions should return empty slice, not nil")
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

// ============================================================
// TestMessageToRowRoundTrip — messageToRow → rowToMessage 往返
// ============================================================
func TestMessageToRowRoundTrip(t *testing.T) {
	now := time.Now().Unix()

	testCases := []Message{
		makeTestMessage("user", "Simple text", now),
		{
			Role: "assistant", Content: []interface{}{
				map[string]interface{}{"type": "text", "text": "Complex content"},
			},
			Timestamp: now + 1,
		},
		{
			Role: "assistant", ToolCalls: []interface{}{
				map[string]interface{}{"id": "call_1", "function": map[string]interface{}{"name": "test_tool", "arguments": "{}"}},
			},
			Timestamp: now + 2,
		},
		{
			Role: "tool", ToolCallID: "call_1", Content: "Tool result",
			Timestamp: now + 3,
		},
		{
			Role: "assistant", Content: "Answer",
			ReasoningContent: "I need to think...", ThinkingSignature: "sig_xyz",
			Timestamp: now + 4,
		},
	}

	for i, original := range testCases {
		row := messageToRow(original, "roundtrip_session", i)
		restored := rowToMessage(row)

		if original.Role != restored.Role {
			t.Errorf("case %d: role mismatch: '%s' vs '%s'", i, original.Role, restored.Role)
		}
		if original.ToolCallID != restored.ToolCallID {
			t.Errorf("case %d: tool_call_id mismatch: '%s' vs '%s'", i, original.ToolCallID, restored.ToolCallID)
		}
		if original.ThinkingSignature != restored.ThinkingSignature {
			t.Errorf("case %d: thinking_signature mismatch: '%s' vs '%s'", i, original.ThinkingSignature, restored.ThinkingSignature)
		}
		if original.Timestamp != restored.Timestamp {
			t.Errorf("case %d: timestamp mismatch: %d vs %d", i, original.Timestamp, restored.Timestamp)
		}
	}
}

// ============================================================
// TestSaveSessionUpdate — 同 ID 再次 SaveSession 应该是 UPDATE
// ============================================================
func TestSaveSessionUpdate(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()

	// 第一次保存
	mgr.SaveSession("upsert_test", "First save", "helper", "default", 10, 20, 30, 1,
		[]Message{makeTestMessage("user", "First message", now)})

	// 第二次保存（同 ID）
	mgr.SaveSession("upsert_test", "Second save", "helper", "default", 100, 200, 300, 5,
		[]Message{
			makeTestMessage("user", "Second message 1", now+10),
			makeTestMessage("assistant", "Second message 2", now+11),
		})

	loaded, _ := mgr.LoadSession("upsert_test")
	if loaded.Description != "Second save" {
		t.Errorf("expected description 'Second save', got '%s'", loaded.Description)
	}
	if loaded.InputTokens != 100 {
		t.Errorf("expected input_tokens 100, got %d", loaded.InputTokens)
	}
	if len(loaded.History) != 2 {
		t.Errorf("expected 2 messages after re-save, got %d", len(loaded.History))
	}

	// 确认 DB 中只有 1 条 session 记录
	var count int64
	globalDB.Model(&SessionHistory{}).Where("id = ?", "upsert_test").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 session record, got %d", count)
	}
}

// ============================================================
// TestLoadSessionDefault — LoadSession("") 加载最新会话
// ============================================================
func TestLoadSessionDefault(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()

	// 创建两个 sessions
	mgr.SaveSession("session_old", "Old Session", "helper", "default", 0, 0, 0, 0,
		[]Message{makeTestMessage("user", "Old", now)})
	time.Sleep(10 * time.Millisecond)

	mgr.SaveSession("session_new", "New Session", "helper", "default", 0, 0, 0, 0,
		[]Message{makeTestMessage("user", "New", now+10)})

	// 空 ID 加载最新
	loaded, err := mgr.LoadSession("")
	if err != nil {
		t.Fatalf("LoadSession('') failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadSession('') returned nil")
	}
	if loaded.ID != "session_new" {
		t.Errorf("expected latest session 'session_new', got '%s'", loaded.ID)
	}
}

// ============================================================
// TestNewSessionDoesNotOverwriteOld — /new 創建新 session，舊數據保留
// 呢個係修復 "default" row 殘留問題嘅核心驗證
// ============================================================
func TestNewSessionDoesNotOverwriteOld(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()

	// 模擬第一次啟動：創建 session "20260504_120000"
	mgr.SaveSession("20260504_120000", "First session", "helper", "default", 100, 200, 300, 5,
		[]Message{
			makeTestMessage("user", "First session msg", now),
			makeTestMessage("assistant", "First session reply", now+1),
		})

	// 模擬 /new：創建新 session "20260504_130000"
	mgr.SaveSession("20260504_130000", "Second session", "helper", "default", 0, 0, 0, 0,
		[]Message{
			makeTestMessage("user", "Second session msg", now+10),
		})

	// 驗證兩個 sessions 都存在
	sessions, _ := mgr.ListSessions()
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions to exist, got %d", len(sessions))
	}

	// 驗證舊 session 數據完整
	old, err := mgr.LoadSession("20260504_120000")
	if err != nil || old == nil {
		t.Fatal("old session should still exist after /new")
	}
	if len(old.History) != 2 {
		t.Errorf("old session should have 2 messages, got %d", len(old.History))
	}
	if old.InputTokens != 100 || old.TotalTokens != 300 {
		t.Errorf("old session token stats lost: input=%d total=%d", old.InputTokens, old.TotalTokens)
	}

	// 驗證新 session 獨立存在
	newSess, err := mgr.LoadSession("20260504_130000")
	if err != nil || newSess == nil {
		t.Fatal("new session should exist")
	}
	if len(newSess.History) != 1 {
		t.Errorf("new session should have 1 message, got %d", len(newSess.History))
	}
	content, _ := newSess.History[0].Content.(string)
	if content != "Second session msg" {
		t.Errorf("new session wrong content: '%s'", content)
	}

	// 模擬重啟：LoadSession("") 應該加載最新（第二個 session）
	latest, err := mgr.LoadSession("")
	if err != nil || latest == nil {
		t.Fatal("LoadSession('') should return latest session")
	}
	if latest.ID != "20260504_130000" {
		t.Errorf("restart should load newest session, got '%s' instead of '20260504_130000'", latest.ID)
	}
}

// ============================================================
// TestLoadNonexistentSession — 加載唔存在嘅 session 返 nil 唔報錯
// ============================================================
func TestLoadNonexistentSession(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	// 未創建任何 session 就查
	loaded, err := mgr.LoadSession("nonexistent")
	if err != nil {
		t.Fatalf("LoadSession should not error for nonexistent session: %v", err)
	}
	if loaded != nil {
		t.Error("LoadSession should return nil for nonexistent session")
	}

	// 創建一個 session 後查另一個 ID
	mgr.SaveSession("real_session", "Real", "", "", 0, 0, 0, 0,
		[]Message{makeTestMessage("user", "Hello", time.Now().Unix())})

	loaded, err = mgr.LoadSession("nonexistent")
	if err != nil {
		t.Fatalf("LoadSession should not error: %v", err)
	}
	if loaded != nil {
		t.Error("LoadSession should return nil for nonexistent session")
	}
}

// ============================================================
// TestSessionIsolation — 兩個 sessions 嘅消息唔會互相洩漏
// ============================================================
func TestSessionIsolation(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()

	mgr.SaveSession("session_A", "Session A", "", "", 0, 0, 0, 0,
		[]Message{
			makeTestMessage("user", "A message 1", now),
			makeTestMessage("assistant", "A reply 1", now+1),
			makeTestMessage("user", "A message 2", now+2),
		})

	mgr.SaveSession("session_B", "Session B", "", "", 0, 0, 0, 0,
		[]Message{
			makeTestMessage("user", "B message 1", now+10),
			makeTestMessage("assistant", "B reply 1", now+11),
		})

	// 加載 session A，確認只包含 A 嘅消息
	a, _ := mgr.LoadSession("session_A")
	if len(a.History) != 3 {
		t.Fatalf("session A should have 3 messages, got %d", len(a.History))
	}
	for _, msg := range a.History {
		content, _ := msg.Content.(string)
		if content[0] != 'A' {
			t.Errorf("session A contains message from other session: '%s'", content)
		}
	}

	// 加載 session B，確認只包含 B 嘅消息
	b, _ := mgr.LoadSession("session_B")
	if len(b.History) != 2 {
		t.Fatalf("session B should have 2 messages, got %d", len(b.History))
	}
	for _, msg := range b.History {
		content, _ := msg.Content.(string)
		if content[0] != 'B' {
			t.Errorf("session B contains message from other session: '%s'", content)
		}
	}
}

// ============================================================
// TestLoadRecentMessagesBoundary — limit > 可用消息、空 session 等邊界
// ============================================================
func TestLoadRecentMessagesBoundary(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	globalDB.Save(&SessionHistory{
		ID:          "boundary_test",
		Description: "Boundary Test",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	})

	now := time.Now().Unix()
	// 只寫入 5 條消息
	messages := []Message{
		makeTestMessage("user", "Msg 1", now),
		makeTestMessage("assistant", "Msg 2", now+1),
		makeTestMessage("user", "Msg 3", now+2),
		makeTestMessage("assistant", "Msg 4", now+3),
		makeTestMessage("user", "Msg 5", now+4),
	}
	mgr.SaveMessages("boundary_test", messages)

	// limit > 可用消息：應該只返 5 條
	recent, err := mgr.LoadRecentMessages("boundary_test", 50)
	if err != nil {
		t.Fatalf("LoadRecentMessages failed: %v", err)
	}
	if len(recent) != 5 {
		t.Errorf("expected 5 messages when limit > available, got %d", len(recent))
	}

	// 空 session
	globalDB.Save(&SessionHistory{
		ID: "empty_boundary_test", Description: "Empty",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	recent, err = mgr.LoadRecentMessages("empty_boundary_test", 10)
	if err != nil {
		t.Fatalf("LoadRecentMessages on empty session failed: %v", err)
	}
	if len(recent) != 0 {
		t.Errorf("expected 0 messages from empty session, got %d", len(recent))
	}
}

// ============================================================
// TestContentJSONRoundTrip — 非字符串 content 類型來回轉換
// ============================================================
func TestContentJSONRoundTrip(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()

	// 多模態內容（JSON array）
	complexContent := []interface{}{
		map[string]interface{}{"type": "text", "text": "What's in this image?"},
		map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url":    "https://example.com/img.png",
				"detail": "high",
			},
		},
	}

	// 嵌套 map 作為 content
	mapContent := map[string]interface{}{
		"tool_name":   "read_file",
		"result":      "file contents here\nmultiple lines\n第三行",
		"exit_code":   0,
		"duration_ms": float64(123.45),
	}

	messages := []Message{
		{Role: "user", Content: complexContent, Timestamp: now},
		{Role: "tool", Content: mapContent, ToolCallID: "call_nested", Timestamp: now + 1},
	}

	mgr.SaveSession("content_json_test", "Content JSON Test", "", "", 0, 0, 0, 0, messages)

	loaded, _ := mgr.LoadSession("content_json_test")
	if len(loaded.History) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(loaded.History))
	}

	// 驗證多模態內容
	loadedComplex, ok := loaded.History[0].Content.([]interface{})
	if !ok {
		t.Errorf("complex content should be []interface{}, got %T", loaded.History[0].Content)
	} else if len(loadedComplex) != 2 {
		t.Errorf("complex content should have 2 elements, got %d", len(loadedComplex))
	}

	// 驗證 map content
	loadedMap, ok := loaded.History[1].Content.(map[string]interface{})
	if !ok {
		t.Errorf("map content should be map[string]interface{}, got %T", loaded.History[1].Content)
	} else {
		if loadedMap["tool_name"] != "read_file" {
			t.Errorf("map content tool_name mismatch: '%v'", loadedMap["tool_name"])
		}
		if loadedMap["exit_code"] != float64(0) {
			t.Errorf("map content exit_code mismatch: %v (type=%T)", loadedMap["exit_code"], loadedMap["exit_code"])
		}
	}
}

// ============================================================
// TestReasoningContentJSONRoundTrip — reasoning_content JSON 類型來回
// ============================================================
func TestReasoningContentJSONRoundTrip(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()

	// reasoning_content 係複雜結構（非字符串）
	reasoningObj := map[string]interface{}{
		"steps": []interface{}{
			map[string]interface{}{"thought": "step 1", "confidence": 0.9},
			map[string]interface{}{"thought": "step 2", "confidence": 0.8},
		},
		"final_plan": "execute the command",
	}

	messages := []Message{
		{Role: "assistant", Content: "I'll do it", ReasoningContent: reasoningObj, Timestamp: now},
	}

	mgr.SaveSession("reasoning_json_test", "Reasoning JSON Test", "", "", 0, 0, 0, 0, messages)

	loaded, _ := mgr.LoadSession("reasoning_json_test")
	if len(loaded.History) != 1 {
		t.Fatalf("expected 1 message, got %d", len(loaded.History))
	}

	rc, ok := loaded.History[0].ReasoningContent.(map[string]interface{})
	if !ok {
		t.Errorf("reasoning_content should be map[string]interface{}, got %T", loaded.History[0].ReasoningContent)
	} else {
		if rc["final_plan"] != "execute the command" {
			t.Errorf("reasoning final_plan mismatch: '%v'", rc["final_plan"])
		}
	}
}

// ============================================================
// TestFTS5Search — 驗證 FTS5 trigger 實際生效，可搜索消息內容
// ============================================================
func TestFTS5Search(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	// 初始化 FTS5（setupSessionTestDB 未調用 initFTS5，其他表唔存在會有 warning，但 session_messages 相關嘅正常）
	initFTS5(globalDB)

	now := time.Now().Unix()
	mgr.SaveSession("fts_test", "FTS Test", "", "", 0, 0, 0, 0, []Message{
		makeTestMessage("user", "How do I implement a binary search tree in Go?", now),
		makeTestMessage("assistant", "Here's how to implement a binary search tree...", now+1),
		makeTestMessage("user", "Can you explain the rotation algorithm for AVL trees?", now+2),
		makeTestMessage("assistant", "AVL tree rotation involves four cases...", now+3),
	})

	// 用簡單 MATCH 查詢驗證 FTS5 可用
	var count int64
	err := globalDB.Raw(
		`SELECT count(*) FROM session_messages_fts WHERE session_messages_fts MATCH ?`,
		`"binary"`,
	).Scan(&count).Error
	if err != nil {
		t.Fatalf("FTS5 search failed: %v", err)
	}
	if count < 1 {
		t.Error("FTS5 should find at least 1 result for 'binary'")
	}

	// 搜索 "AVL"
	var count2 int64
	globalDB.Raw(
		`SELECT count(*) FROM session_messages_fts WHERE session_messages_fts MATCH ?`,
		`"AVL"`,
	).Scan(&count2)
	if count2 != 2 {
		t.Errorf("FTS5 should find exactly 2 results for 'AVL', got %d", count2)
	}

	// 搜索唔存在嘅詞
	var count3 int64
	globalDB.Raw(
		`SELECT count(*) FROM session_messages_fts WHERE session_messages_fts MATCH ?`,
		`"nonexistent"`,
	).Scan(&count3)
	if count3 != 0 {
		t.Errorf("FTS5 should find 0 results for nonexistent word, got %d", count3)
	}
}

// ============================================================
// TestDeleteSessionPrefixMatch — DeleteSession 前綴匹配 fallback
// ============================================================
func TestDeleteSessionPrefixMatch(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	// 創建一個帶 timestamp 前綴嘅 session（模擬 auto-save 生成嘅 ID）
	longID := "20260504_120000_some_uuid_suffix"
	mgr.SaveSession(longID, "Long ID session", "", "", 0, 0, 0, 0,
		[]Message{makeTestMessage("user", "Test", now)})

	// 用短前綴刪除
	err := mgr.DeleteSession("20260504_120000")
	if err != nil {
		t.Fatalf("DeleteSession with prefix match failed: %v", err)
	}

	// 驗證完全刪除
	loaded, _ := mgr.LoadSession(longID)
	if loaded != nil {
		t.Error("session should be deleted via prefix match")
	}
}

// ============================================================
// TestNilContentMessage — content 為 nil 嘅消息
// ============================================================
func TestNilContentMessage(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	messages := []Message{
		{Role: "assistant", Content: nil, ToolCalls: []interface{}{
			map[string]interface{}{"id": "call_nocontent", "function": map[string]interface{}{"name": "test", "arguments": "{}"}},
		}, Timestamp: now},
		{Role: "tool", Content: "result", ToolCallID: "call_nocontent", Timestamp: now + 1},
	}

	mgr.SaveSession("nil_content_test", "Nil Content", "", "", 0, 0, 0, 0, messages)

	loaded, _ := mgr.LoadSession("nil_content_test")
	if len(loaded.History) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(loaded.History))
	}

	// 第一條消息 content 應該為 nil
	if loaded.History[0].Content != nil {
		t.Errorf("expected nil content, got '%v'", loaded.History[0].Content)
	}
	// 但 tool calls 應該保留
	if loaded.History[0].ToolCalls == nil {
		t.Error("tool_calls should not be nil when content is nil")
	}
}

// ============================================================
// TestTokenStatsZeroPreserved — token stats 為 0 時正確保存加載
// ============================================================
func TestTokenStatsZeroPreserved(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	mgr.SaveSession("zero_stats", "Zero Stats", "", "", 0, 0, 0, 0,
		[]Message{makeTestMessage("user", "Hello", now)})

	loaded, _ := mgr.LoadSession("zero_stats")
	if loaded.InputTokens != 0 {
		t.Errorf("expected input_tokens 0, got %d", loaded.InputTokens)
	}
	if loaded.OutputTokens != 0 {
		t.Errorf("expected output_tokens 0, got %d", loaded.OutputTokens)
	}
	if loaded.TotalTokens != 0 {
		t.Errorf("expected total_tokens 0, got %d", loaded.TotalTokens)
	}
	if loaded.TurnCount != 0 {
		t.Errorf("expected turn_count 0, got %d", loaded.TurnCount)
	}
}

// ============================================================
// TestLargeMessageContent — 大型消息內容（接近舊 TEXT 限制）
// ============================================================
func TestLargeMessageContent(t *testing.T) {
	_, mgr, _ := setupSessionTestDB(t)

	now := time.Now().Unix()
	// 創建一條 64KB 嘅消息
	largeContent := make([]byte, 64*1024)
	for i := range largeContent {
		largeContent[i] = byte('A' + (i % 26))
	}

	messages := []Message{
		makeTestMessage("user", "Small message", now),
		makeTestMessage("assistant", string(largeContent), now+1),
		makeTestMessage("user", "Another small message", now+2),
	}

	mgr.SaveSession("large_content_test", "Large Content", "", "", 0, 0, 0, 0, messages)

	loaded, _ := mgr.LoadSession("large_content_test")
	if len(loaded.History) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(loaded.History))
	}

	// 驗證大型內容完整
	large, _ := loaded.History[1].Content.(string)
	if len(large) != 64*1024 {
		t.Errorf("large content length mismatch: expected %d, got %d", 64*1024, len(large))
	}
	if large[0] != 'A' || large[64000] != byte('A'+(64000%26)) {
		t.Error("large content corruption detected")
	}

	// 驗證周圍嘅細消息冇受影響
	small1, _ := loaded.History[0].Content.(string)
	if small1 != "Small message" {
		t.Errorf("small message 1 corrupted: %q", small1)
	}
	small2, _ := loaded.History[2].Content.(string)
	if small2 != "Another small message" {
		t.Errorf("small message 2 corrupted: %q", small2)
	}
}

