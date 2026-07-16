package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupEvolverTestDB 创建测试 DB + Session + SessionMessage 表 + 设置 globalDB + globalSessionPersist
func setupEvolverTestDB(t *testing.T) (*gorm.DB, *SessionPersistManager, string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/ghostclaw.db"

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")

	if err := db.AutoMigrate(&SessionHistory{}, &SessionMessage{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	oldDB := globalDB
	globalDB = db
	t.Cleanup(func() {
		globalDB = oldDB
	})

	mgr := NewSessionPersistManager()
	oldPersist := globalSessionPersist
	globalSessionPersist = mgr
	t.Cleanup(func() {
		globalSessionPersist = oldPersist
	})
	return db, mgr, tmpDir
}

// ============================================================
// TestCategorizeMessages — 按角色分類
// ============================================================
func TestCategorizeMessages(t *testing.T) {
	se := &SelfEvolver{}
	messages := []Message{
		{Role: "system", Content: "You are a helper"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi!"},
		{Role: "tool", Content: "result", ToolCallID: "call_1"},
		{Role: "assistant", Content: "Done"},
		{Role: "system", Content: "You are updated"},
		{Role: "user", Content: "Help me"},
		{Role: "tool", Content: "error", ToolCallID: "call_2"},
	}

	sys, usr, ast, tool := se.categorizeMessages(messages)

	if len(sys) != 2 {
		t.Errorf("expected 2 system messages, got %d", len(sys))
	}
	if len(usr) != 2 {
		t.Errorf("expected 2 user messages, got %d", len(usr))
	}
	if len(ast) != 2 {
		t.Errorf("expected 2 assistant messages, got %d", len(ast))
	}
	if len(tool) != 2 {
		t.Errorf("expected 2 tool messages, got %d", len(tool))
	}
}

// ============================================================
// TestCategorizeMessagesEmpty — 空消息列表
// ============================================================
func TestCategorizeMessagesEmpty(t *testing.T) {
	se := &SelfEvolver{}
	sys, usr, ast, tool := se.categorizeMessages(nil)
	if len(sys) != 0 || len(usr) != 0 || len(ast) != 0 || len(tool) != 0 {
		t.Error("all categories should be empty for nil input")
	}

	sys, usr, ast, tool = se.categorizeMessages([]Message{})
	if len(sys) != 0 || len(usr) != 0 || len(ast) != 0 || len(tool) != 0 {
		t.Error("all categories should be empty for empty input")
	}
}

// ============================================================
// TestExtractErrorChains — 提取錯誤鏈
// ============================================================
func TestExtractErrorChains(t *testing.T) {
	se := &SelfEvolver{}

	toolMsgs := []Message{
		{Role: "tool", Content: "error: permission denied", ToolCallID: "c1"},
		{Role: "tool", Content: "chmod executed", ToolCallID: "c1"},
		{Role: "tool", Content: "success", ToolCallID: "c2"},
		{Role: "tool", Content: "failed to connect", ToolCallID: "c3"},
		{Role: "tool", Content: "retry connection", ToolCallID: "c3"},
		{Role: "tool", Content: "connected", ToolCallID: "c3"},
	}

	chains := se.extractErrorChains(toolMsgs)

	if len(chains) != 2 {
		t.Fatalf("expected 2 error chains, got %d", len(chains))
	}

	// 鏈 0：error → chmod → success（同 tool_call c1，success 之後 c3 嘅 error 開始新鏈）
	if len(chains[0]) != 3 {
		t.Errorf("chain 0: expected 3 msgs, got %d", len(chains[0]))
	}

	// 鏈 1：failed → retry → connected（同 tool_call c3）
	if len(chains[1]) != 3 {
		t.Errorf("chain 1: expected 3 msgs, got %d", len(chains[1]))
	}
}

// ============================================================
// TestExtractErrorChainsEmpty — 冇錯誤
// ============================================================
func TestExtractErrorChainsEmpty(t *testing.T) {
	se := &SelfEvolver{}

	toolMsgs := []Message{
		{Role: "tool", Content: "file contents here", ToolCallID: "c1"},
		{Role: "tool", Content: "command output", ToolCallID: "c2"},
	}

	chains := se.extractErrorChains(toolMsgs)
	if len(chains) != 0 {
		t.Errorf("expected 0 chains for no errors, got %d", len(chains))
	}

	chains = se.extractErrorChains(nil)
	if len(chains) != 0 {
		t.Errorf("expected 0 chains for nil, got %d", len(chains))
	}
}

// ============================================================
// TestExtractErrorChainsUnfinished — 未完成嘅錯誤鏈
// ============================================================
func TestExtractErrorChainsUnfinished(t *testing.T) {
	se := &SelfEvolver{}

	toolMsgs := []Message{
		{Role: "tool", Content: "error: not found", ToolCallID: "c1"},
	}

	chains := se.extractErrorChains(toolMsgs)
	if len(chains) != 1 {
		t.Fatalf("expected 1 unfinished chain, got %d", len(chains))
	}
	if len(chains[0]) != 1 {
		t.Errorf("unfinished chain should have 1 msg, got %d", len(chains[0]))
	}
}

// ============================================================
// TestCanRunCooldown — 冷卻機制
// ============================================================
func TestCanRunCooldown(t *testing.T) {
	se := &SelfEvolver{
		minSessionInterval: 100 * time.Millisecond,
	}

	// 第一次應該可以
	if !se.canRun("session") {
		t.Error("first session run should be allowed")
	}
	// 即刻第二次應該被拒絕
	if se.canRun("session") {
		t.Error("second session run within cooldown should be denied")
	}

	// 等冷卻過咗
	time.Sleep(150 * time.Millisecond)

	if !se.canRun("session") {
		t.Error("session run after cooldown should be allowed")
	}
}

// ============================================================
// TestBuildPromptAnalysisPrompt — prompt 構建
// ============================================================
func TestBuildPromptAnalysisPrompt(t *testing.T) {
	se := &SelfEvolver{}

	sysMsgs := []Message{
		{Role: "system", Content: "You are a helpful coding assistant"},
	}
	usrMsgs := []Message{
		{Role: "user", Content: "Write a function"},
	}
	astMsgs := []Message{
		{Role: "assistant", Content: "Here's the code..."},
	}
	toolMsgs := []Message{
		{Role: "tool", Content: "compilation error: undefined variable", ToolCallID: "c1"},
	}

	result := se.buildPromptAnalysisPrompt(sysMsgs, usrMsgs, astMsgs, toolMsgs)

	if !strings.Contains(result, "System Prompt") {
		t.Error("should contain System Prompt section")
	}
	if !strings.Contains(result, "用戶請求") {
		t.Error("should contain user requests section")
	}
	if !strings.Contains(result, "Assistant 行為") {
		t.Error("should contain assistant behavior section")
	}
	if !strings.Contains(result, "工具調用統計") {
		t.Error("should contain tool statistics section")
	}
	if !strings.Contains(result, "錯誤數: 1") {
		t.Error("should count 1 error")
	}
}

// ============================================================
// TestBuildPromptAnalysisPromptEmpty — 空輸入
// ============================================================
func TestBuildPromptAnalysisPromptEmpty(t *testing.T) {
	se := &SelfEvolver{}
	result := se.buildPromptAnalysisPrompt(nil, nil, nil, nil)
	// 應該唔會 panic，返回空或基本框架
	if !strings.Contains(result, "System Prompt") {
		t.Error("should at least contain section headers")
	}
}

// ============================================================
// TestBuildToolPatternPrompt — 工具模式 prompt
// ============================================================
func TestBuildToolPatternPrompt(t *testing.T) {
	se := &SelfEvolver{}

	toolMsgs := []Message{
		{Role: "tool", Content: "read_file result: package main", ToolCallID: "c1"},
		{Role: "tool", Content: "write_file success", ToolCallID: "c2"},
		{Role: "tool", Content: "bash output: test passed", ToolCallID: "c3"},
	}

	result := se.buildToolPatternPrompt(toolMsgs, 3)
	if !strings.Contains(result, "工具調用總數") {
		t.Error("should contain tool call count")
	}
	if !strings.Contains(result, "工具使用頻率") {
		t.Error("should contain tool frequency")
	}
}

// ============================================================
// TestBuildErrorRecoveryPrompt — 錯誤恢復 prompt
// ============================================================
func TestBuildErrorRecoveryPrompt(t *testing.T) {
	se := &SelfEvolver{}

	chains := [][]Message{
		{
			{Role: "tool", Content: "error: permission denied", ToolCallID: "c1"},
			{Role: "tool", Content: "chmod 755 success", ToolCallID: "c1"},
		},
	}

	result := se.buildErrorRecoveryPrompt(chains)
	if !strings.Contains(result, "錯誤鏈總數") {
		t.Error("should contain error chain count")
	}
	if !strings.Contains(result, "錯誤鏈 1") {
		t.Error("should contain first error chain")
	}
	if !strings.Contains(result, "permission denied") {
		t.Error("should contain error message content")
	}
}

// ============================================================
// TestBuildErrorRecoveryPromptEmpty — 空鏈
// ============================================================
func TestBuildErrorRecoveryPromptEmpty(t *testing.T) {
	se := &SelfEvolver{}
	result := se.buildErrorRecoveryPrompt(nil)
	if !strings.Contains(result, "錯誤鏈總數: 0") {
		t.Error("should show 0 chains")
	}
}

// ============================================================
// TestBuildCrossSessionPrompt — 跨 session prompt
// ============================================================
func TestBuildCrossSessionPrompt(t *testing.T) {
	se := &SelfEvolver{}

	messages := []Message{
		{Role: "user", Content: "Write a web server"},
		{Role: "assistant", Content: "Here's the Go code..."},
		{Role: "user", Content: "Add authentication"},
		{Role: "assistant", Content: "Adding JWT auth..."},
	}

	result := se.buildCrossSessionPrompt(messages)
	if !strings.Contains(result, "跨會話消息總數") {
		t.Error("should contain message count")
	}
	if !strings.Contains(result, "Write a web server") {
		t.Error("should contain user request sample")
	}
}

// ============================================================
// TestProcessAnalysisResult — 結果解析 + 存入 memory
// ============================================================
func TestProcessAnalysisResult(t *testing.T) {
	// 設置 UnifiedMemory
	oldMem := globalUnifiedMemory
	tmpDir := t.TempDir()
	var errUM error
	globalUnifiedMemory, errUM = NewUnifiedMemory(tmpDir)
	_ = errUM
	t.Cleanup(func() {
		globalUnifiedMemory = oldMem
	})

	se := &SelfEvolver{}

	result := `### Insights
- prompt_missing_error_handling: 系統提示缺少錯誤處理指引，導致工具失敗後無重試策略
- tool_read_then_write_pattern: read_file 後頻繁接 write_file，可考慮合併操作
- user_prefers_conciseness: 用戶偏好像簡潔回應

### Patterns
- error_recovery_chmod: 遇到 permission denied 時 chmod 可解決大部分問題`

	se.processAnalysisResult(result, "test_prefix")

	// 唔會 panic，memory 寫入成功與否由 processAnalysisResult 內部 log 處理
}

// ============================================================
// TestProcessAnalysisResultEmpty — 空結果
// ============================================================
func TestProcessAnalysisResultEmpty(t *testing.T) {
	oldMem := globalUnifiedMemory
	tmpDir := t.TempDir()
	var errUM error
	globalUnifiedMemory, errUM = NewUnifiedMemory(tmpDir)
	_ = errUM
	t.Cleanup(func() {
		globalUnifiedMemory = oldMem
	})

	se := &SelfEvolver{}
	// 唔應該 panic
	se.processAnalysisResult("", "test")
	se.processAnalysisResult("### Insights\n", "test")
	se.processAnalysisResult("garbage text without format", "test")
}

// ============================================================
// TestCountToolCalls — 工具調用計數
// ============================================================
func TestCountToolCalls(t *testing.T) {
	se := &SelfEvolver{}

	toolMsgs := []Message{
		{Role: "tool", Content: "result 1", ToolCallID: "c1"},
		{Role: "tool", Content: "result 2", ToolCallID: ""},
		{Role: "tool", Content: "result 3", ToolCallID: "c3"},
	}

	count := se.countToolCalls(toolMsgs)
	if count != 3 {
		t.Errorf("expected 3 tool messages, got %d", count)
	}

	count = se.countToolCalls(nil)
	if count != 0 {
		t.Errorf("expected 0 for nil, got %d", count)
	}
}

// ============================================================
// TestAnalyzeSessionIntegration — 綜合分析完整流程（DB，合併原 4 個獨立測試）
// ============================================================
func TestAnalyzeSessionIntegration(t *testing.T) {
	_, mgr, _ := setupEvolverTestDB(t)

	// 設置 UnifiedMemory（避免 nil pointer）
	oldMem := globalUnifiedMemory
	tmpDir := t.TempDir()
	var errUM error
	globalUnifiedMemory, errUM = NewUnifiedMemory(tmpDir)
	_ = errUM
	t.Cleanup(func() {
		globalUnifiedMemory = oldMem
	})

	// 寫入含 system prompt + tool + error 嘅完整消息鏈（覆蓋 4 個維度）
	now := time.Now().Unix()
	messages := []Message{
		{Role: "system", Content: "You are a helpful coding assistant. Always check syntax before running code.", Timestamp: now},
		{Role: "user", Content: "Write a Go HTTP server", Timestamp: now + 1},
		{Role: "assistant", Content: "Here's the server code...", Timestamp: now + 2},
		{Role: "tool", Content: "error: permission denied", ToolCallID: "call_1", Timestamp: now + 3},
		{Role: "tool", Content: "chmod 755 success", ToolCallID: "call_1", Timestamp: now + 4},
		{Role: "tool", Content: "go build: success", ToolCallID: "call_2", Timestamp: now + 5},
		{Role: "assistant", Content: "The server compiled successfully.", Timestamp: now + 6},
	}

	mgr.SaveSession("session_integration_test", "Integration Test", "helper", "default", 10, 50, 60, 1, messages)

	se := &SelfEvolver{
		minSessionInterval:           0, // 禁用冷卻
		sessionsAnalyzed:             make(map[string]bool),
		minToolCallsForAnalysis:      2, // 低閾值以觸發工具分析
		minSessionsForCrossAnalysis:  999, // 高閾值以跳過跨 session（單 session 測試）
	}

	// 調用綜合分析（唔會 panic，LLM 調用會失敗因為冇真實 API，但流程應該完整）
	se.AnalyzeSession(t.Context(), "session_integration_test")

	// markSessionAnalyzed 只喺 LLM 成功後先調用，測試環境 LLM 會失敗所以唔會標記
	// 只需確認冇 panic
}

// ============================================================
// TestAnalyzeSessionInsufficientData — 數據不足
// ============================================================
func TestAnalyzeSessionInsufficientData(t *testing.T) {
	_, mgr, _ := setupEvolverTestDB(t)

	oldMem := globalUnifiedMemory
	tmpDir := t.TempDir()
	var errUM error
	globalUnifiedMemory, errUM = NewUnifiedMemory(tmpDir)
	_ = errUM
	t.Cleanup(func() {
		globalUnifiedMemory = oldMem
	})

	// 只有 3 條消息（少過 4 條閾值）
	messages := []Message{
		{Role: "user", Content: "Hi", Timestamp: time.Now().Unix()},
		{Role: "assistant", Content: "Hello", Timestamp: time.Now().Unix() + 1},
		{Role: "user", Content: "Bye", Timestamp: time.Now().Unix() + 2},
	}
	mgr.SaveSession("short_test", "Short", "", "", 0, 0, 0, 0, messages)

	se := &SelfEvolver{
		minSessionInterval: 0,
		sessionsAnalyzed:   make(map[string]bool),
	}

	// 應該因為數據不足而跳過
	se.AnalyzeSession(t.Context(), "short_test")
}

// ============================================================
// TestLoadFullMessageChain — DB 載入完整消息鏈
// ============================================================
func TestLoadFullMessageChain(t *testing.T) {
	_, mgr, _ := setupEvolverTestDB(t)

	now := time.Now().Unix()
	messages := []Message{
		{Role: "system", Content: "You are a coder", Timestamp: now},
		{Role: "user", Content: "Write code", Timestamp: now + 1},
		{Role: "assistant", Content: "Here's code", Timestamp: now + 2},
		{Role: "tool", Content: "compiled", ToolCallID: "c1", Timestamp: now + 3},
	}
	mgr.SaveSession("load_chain_test", "Load Chain", "helper", "default", 0, 0, 0, 0, messages)

	se := &SelfEvolver{
		sessionsAnalyzed: make(map[string]bool),
	}

	loaded := se.loadFullMessageChain("load_chain_test")
	if len(loaded) != 4 {
		t.Errorf("expected 4 messages, got %d", len(loaded))
	}

	// loadFullMessageChain 唔標記 session（標記由各分析 method 喺 LLM 成功後調用）
	se.mu.Lock()
	count := se.analyzedSessionCount
	se.mu.Unlock()
	if count != 0 {
		t.Errorf("loadFullMessageChain should not mark session, count=%d", count)
	}
}

// ============================================================
// TestLoadMultiSessionMessages — 多 session 載入
// ============================================================
func TestLoadMultiSessionMessages(t *testing.T) {
	_, mgr, _ := setupEvolverTestDB(t)

	now := time.Now().Unix()
	for i := 0; i < 3; i++ {
		messages := []Message{
			{Role: "user", Content: fmt.Sprintf("Task %d", i), Timestamp: now + int64(i*10)},
			{Role: "assistant", Content: fmt.Sprintf("Result %d", i), Timestamp: now + int64(i*10) + 1},
			{Role: "tool", Content: fmt.Sprintf("output %d", i), ToolCallID: fmt.Sprintf("c%d", i), Timestamp: now + int64(i*10) + 2},
			{Role: "assistant", Content: fmt.Sprintf("Final %d", i), Timestamp: now + int64(i*10) + 3},
		}
		mgr.SaveSession(fmt.Sprintf("multi_session_%d", i), fmt.Sprintf("S%d", i), "", "", 0, 0, 0, 0, messages)
		time.Sleep(5 * time.Millisecond)
	}

	se := &SelfEvolver{}

	all := se.loadMultiSessionMessages(2)
	if len(all) == 0 {
		t.Error("should load messages from multiple sessions")
	}
	// 每個 session 4 條消息，取最近 30 條（全部），2 個 session = 8 條
	if len(all) != 8 {
		t.Errorf("expected 8 messages from 2 sessions, got %d", len(all))
	}
}

// ============================================================
// TestLoadMultiSessionMessagesEmpty — 冇 session 時
// ============================================================
func TestLoadMultiSessionMessagesEmpty(t *testing.T) {
	_, _, _ = setupEvolverTestDB(t)

	se := &SelfEvolver{}
	all := se.loadMultiSessionMessages(5)
	if len(all) != 0 {
		t.Errorf("expected 0 messages when no sessions exist, got %d", len(all))
	}
}

// ============================================================
// TestSelfEvolverCooldownPersistence — 冷卻跨調用保持一致
// ============================================================
func TestSelfEvolverCooldownPersistence(t *testing.T) {
	se := &SelfEvolver{
		minSessionInterval: 200 * time.Millisecond,
	}

	// 第一次 session
	if !se.canRun("session") {
		t.Error("first call should be allowed")
	}

	// 等 100ms（未夠冷卻）
	time.Sleep(100 * time.Millisecond)
	if se.canRun("session") {
		t.Error("should still be in cooldown after 100ms")
	}

	// 等夠 200ms
	time.Sleep(150 * time.Millisecond)
	if !se.canRun("session") {
		t.Error("should be allowed after cooldown period")
	}
}

// ============================================================
// TestSelfEvolverNilGlobals — nil global 時唔 panic
// ============================================================
func TestSelfEvolverNilGlobals(t *testing.T) {
	oldPersist := globalSessionPersist
	oldMem := globalUnifiedMemory
	oldDB := globalDB
	globalSessionPersist = nil
	globalUnifiedMemory = nil
	globalDB = nil
	t.Cleanup(func() {
		globalSessionPersist = oldPersist
		globalUnifiedMemory = oldMem
		globalDB = oldDB
	})

	se := &SelfEvolver{
		minSessionInterval: 0,
		sessionsAnalyzed:   make(map[string]bool),
	}

	// 全部應該安全返回，唔 panic（public methods 檢查 nil global）
	se.AnalyzeSession(t.Context(), "test")
}

// ============================================================
// TestExtractErrorChainsMultipleErrors — 連續多個錯誤
// ============================================================
func TestExtractErrorChainsMultipleErrors(t *testing.T) {
	se := &SelfEvolver{}

	toolMsgs := []Message{
		{Role: "tool", Content: "error: not found", ToolCallID: "c1"},
		{Role: "tool", Content: "error: still not found", ToolCallID: "c1"},
		{Role: "tool", Content: "found it!", ToolCallID: "c1"},
	}

	chains := se.extractErrorChains(toolMsgs)
	// 新邏輯：連續多個 error 各自開始新鏈
	// chain 0: ["error: not found"]
	// chain 1: ["error: still not found", "found it!"]
	if len(chains) != 2 {
		t.Fatalf("expected 2 chains (consecutive errors split), got %d", len(chains))
	}
	if len(chains[0]) != 1 {
		t.Errorf("chain 0 should have 1 msg (first error), got %d", len(chains[0]))
	}
	if len(chains[1]) != 2 {
		t.Errorf("chain 1 should have 2 msgs (second error + recovery), got %d", len(chains[1]))
	}
}

// ============================================================
// TestExtractErrorChainsCaseInsensitive — 不同大小寫嘅 error 關鍵字
// ============================================================
func TestExtractErrorChainsCaseInsensitive(t *testing.T) {
	se := &SelfEvolver{}

	toolMsgs := []Message{
		{Role: "tool", Content: "Error: something wrong", ToolCallID: "c1"},
		{Role: "tool", Content: "Fixed", ToolCallID: "c1"},
		{Role: "tool", Content: "FAILED to execute", ToolCallID: "c2"},
		{Role: "tool", Content: "Retry succeeded", ToolCallID: "c2"},
	}

	chains := se.extractErrorChains(toolMsgs)
	if len(chains) != 2 {
		t.Errorf("expected 2 chains (case insensitive), got %d", len(chains))
	}
}

// ============================================================
// TestBuildErrorRecoveryPromptMaxChains — 超過 3 條鏈時只顯示前 3 條
// ============================================================
func TestBuildErrorRecoveryPromptMaxChains(t *testing.T) {
	se := &SelfEvolver{}

	// 創建 5 條錯誤鏈
	var chains [][]Message
	for i := 0; i < 5; i++ {
		chains = append(chains, []Message{
			{Role: "tool", Content: fmt.Sprintf("error %d", i), ToolCallID: fmt.Sprintf("c%d", i)},
			{Role: "tool", Content: fmt.Sprintf("recovery %d", i), ToolCallID: fmt.Sprintf("c%d", i)},
		})
	}

	result := se.buildErrorRecoveryPrompt(chains)
	if !strings.Contains(result, "錯誤鏈 3") {
		t.Error("should show chain 3")
	}
	// 第 4、5 條鏈唔應該出現
	if strings.Contains(result, "錯誤鏈 4") || strings.Contains(result, "錯誤鏈 5") {
		t.Error("should only show first 3 chains max")
	}
}
