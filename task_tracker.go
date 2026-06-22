package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// TaskComplexity 任務複雜度評估
type TaskComplexity int

const (
	ComplexitySimple   TaskComplexity = 1 // 簡單任務：單次問答、查詢
	ComplexityModerate TaskComplexity = 2 // 中等任務：需要少量工具調用
	ComplexityComplex  TaskComplexity = 3 // 複雜任務：多步驟、需要規劃
	ComplexityUnknown  TaskComplexity = 0 // 未知：初始狀態
)

// TaskIntent 用戶意圖分類
// 用於決定是否啟用工作模式（結構化任務追蹤 + 退出守衛）
type TaskIntent int

const (
	IntentChat TaskIntent = 0 // 閒聊/問答（Free Mode，無退出守衛）
	IntentTask TaskIntent = 1 // 結構化任務（Work Mode，todo 驅動退出守衛）
)

// TaskState 任務狀態追蹤
type TaskState struct {
	mu sync.RWMutex

	// 任務識別
	InitialQuery string         // 用戶原始請求
	Complexity   TaskComplexity // 評估的複雜度
	Intent       TaskIntent     // 意圖分類（chat/task/other）
	StartTime    time.Time      // 任務開始時間

	// 進度追蹤
	TotalSteps     int // 預估總步驟數
	CompletedSteps int // 已完成步驟數
	FailedSteps    int // 失敗步驟數
	CancelledSteps int // 取消步驟數

	// 工具調用追蹤
	ToolCallsByType  map[string]int   // 按類型統計工具調用次數
	RecentToolCalls  []ToolCallRecord // 最近工具調用記錄
	LastSuccessTime  time.Time        // 最後一次成功時間
	LastFailedTime   time.Time        // 最後一次失敗時間
	LastFailedTool   string           // 最後一次失敗的工具
	ConsecutiveFails int              // 連續失敗次數
	RepeatedFailures map[string]int   // 按工具統計重複失敗

	// 狀態標記
	IsStuck               bool   // 是否卡住
	IsCompleted           bool   // 是否完成
	StuckReason           string // 卡住原因
	NeedsIntervention     bool   // 是否需要用戶干預
	LastAlertStep         int    // 最後注入 alert 時的 totalSteps（用於冷卻）
	ConsecutiveNoProgress int    // 連續無進展步數
}

// ToolCallRecord 工具調用記錄
type ToolCallRecord struct {
	Time     time.Time
	ToolName string
	Status   TaskStatus
	Input    string // 簡化的輸入摘要
	Output   string // 簡化的輸出摘要
}

// TaskTracker 任務追蹤器
type TaskTracker struct {
	currentTask *TaskState
	mu          sync.RWMutex
}

// NewTaskTracker 創建任務追蹤器
func NewTaskTracker() *TaskTracker {
	return &TaskTracker{}
}

// StartNewTask 開始新任務
// intent 由調用方通過 LLM 分類（ClassifyUserIntent）確定
func (tt *TaskTracker) StartNewTask(query string, intent TaskIntent) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	tt.currentTask = &TaskState{
		InitialQuery:     query,
		Complexity:       ComplexityModerate, // TASK 一律以工作模式處理
		Intent:           intent,
		StartTime:        time.Now(),
		ToolCallsByType:  make(map[string]int),
		RecentToolCalls:  make([]ToolCallRecord, 0, 20),
		RepeatedFailures: make(map[string]int),
	}
}

// IsWorkMode 是否處於工作模式
// 工作模式 = 意圖為 TASK（由 LLM 二元分類判定）
// 觸發系統提示注入 + AgentLoop 退出守衛
func (tt *TaskTracker) IsWorkMode() bool {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	return tt.currentTask != nil && tt.currentTask.Intent == IntentTask
}

// RecordToolCall 記錄工具調用
func (tt *TaskTracker) RecordToolCall(toolName string, status TaskStatus, inputSummary, outputSummary string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	if tt.currentTask == nil {
		return
	}

	task := tt.currentTask
	now := time.Now()

	// 更新工具調用統計
	task.ToolCallsByType[toolName]++

	// 添加到最近調用記錄
	record := ToolCallRecord{
		Time:     now,
		ToolName: toolName,
		Status:   status,
		Input:    TruncateString(inputSummary, 100),
		Output:   TruncateString(outputSummary, 100),
	}
	task.RecentToolCalls = append(task.RecentToolCalls, record)

	// 保留最近20條
	if len(task.RecentToolCalls) > 20 {
		task.RecentToolCalls = task.RecentToolCalls[1:]
	}

	// 更新步驟計數
	switch status {
	case TaskStatusSuccess:
		task.CompletedSteps++
		task.LastSuccessTime = now
		task.ConsecutiveFails = 0
		task.ConsecutiveNoProgress = 0
		task.RepeatedFailures = make(map[string]int)
	case TaskStatusFailed:
		task.FailedSteps++
		task.LastFailedTime = now
		task.LastFailedTool = toolName
		task.ConsecutiveFails++
		task.ConsecutiveNoProgress++
		task.RepeatedFailures[toolName]++
	case TaskStatusCancelled:
		task.CancelledSteps++
		task.ConsecutiveFails = 0
	}

	// 檢測是否卡住
	tt.detectStuckState()
}

// detectStuckState 檢測卡住狀態（累積式：記錄所有觸發條件，而非僅第一條）
func (tt *TaskTracker) detectStuckState() {
	task := tt.currentTask
	if task == nil {
		return
	}

	var reasons []string

	// 條件1：連續失敗次數過多
	if task.ConsecutiveFails >= 3 {
		reasons = append(reasons, fmt.Sprintf("Consecutive failures (%d times) on tool: %s",
			task.ConsecutiveFails, task.LastFailedTool))
	}

	// 條件2：同一工具重複失敗過多
	for tool, count := range task.RepeatedFailures {
		if count >= 3 {
			reasons = append(reasons, fmt.Sprintf("Tool '%s' failed %d times", tool, count))
			break // 只報告第一個達標的工具，避免過長
		}
	}

	// 條件3：長時間無成功（包括從未成功）但有大量調用
	totalCalls := task.CompletedSteps + task.FailedSteps + task.CancelledSteps
	timeSinceSuccess := time.Since(task.LastSuccessTime)
	if timeSinceSuccess > 2*time.Minute && totalCalls > 10 && task.FailedSteps > task.CompletedSteps {
		reasons = append(reasons, "High failure rate over extended period")
	}

	// 條件4：總步數過多 且 連續 >10 步無進展
	totalSteps := task.CompletedSteps + task.FailedSteps + task.CancelledSteps
	if totalSteps > 30 && task.ConsecutiveNoProgress > 10 {
		reasons = append(reasons, "Too many steps without progress, possible infinite loop")
	}

	if len(reasons) > 0 {
		task.IsStuck = true
		task.StuckReason = strings.Join(reasons, "; ")
		task.NeedsIntervention = true
	} else {
		task.IsStuck = false
		task.NeedsIntervention = false
	}
}

// MarkCompleted 標記任務完成
func (tt *TaskTracker) MarkCompleted() {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	if tt.currentTask != nil {
		tt.currentTask.IsCompleted = true
	}
}

// ResetStuckState 重置卡住狀態
func (tt *TaskTracker) ResetStuckState() {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if tt.currentTask != nil {
		tt.currentTask.IsStuck = false
		tt.currentTask.NeedsIntervention = false
		tt.currentTask.ConsecutiveNoProgress = 0
		tt.currentTask.RepeatedFailures = make(map[string]int)
	}
}

// ShouldPromptTodo 是否應該提示更新 todo
func (tt *TaskTracker) ShouldPromptTodo() (bool, string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	if tt.currentTask == nil {
		return false, ""
	}

	task := tt.currentTask

	// 簡單任務不需要 todo
	if task.Complexity == ComplexitySimple {
		return false, ""
	}

	// 如果已經卡住，提示用戶干預
	if task.IsStuck && task.NeedsIntervention {
		totalSteps := task.CompletedSteps + task.FailedSteps + task.CancelledSteps
		// 冷卻：至少隔 5 步（LastAlertStep > 0 表示曾經 alert 過）
		if task.LastAlertStep > 0 && totalSteps-task.LastAlertStep < 5 {
			return false, ""
		}
		// 所有 todos 已完成就唔再 alert（但有 todo 列表先檢查）
		if !TODO.IsEmpty() && !TODO.HasUnfinishedItems() {
			return false, ""
		}
		task.LastAlertStep = totalSteps
		return true, fmt.Sprintf(
			"[SYSTEM_ALERT]The task appears to be stuck: %s. Please either: (1) try a different approach, (2) ask the user for guidance, or (3) update your todo list to reflect current progress.[/SYSTEM_ALERT]",
			task.StuckReason,
		)
	}

	// 工作模式下每 8 步提示更新 todo
	totalSteps := task.CompletedSteps + task.FailedSteps + task.CancelledSteps
	if totalSteps > 0 && totalSteps%8 == 0 && task.CompletedSteps > 0 {
		return true, "[PROGRESS_CHECK]Please update your todo list if needed.[/PROGRESS_CHECK]"
	}

	return false, ""
}

// GetProgressReport 獲取進度報告
func (tt *TaskTracker) GetProgressReport() string {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	if tt.currentTask == nil {
		return "No active task"
	}

	task := tt.currentTask

	var report strings.Builder
	report.WriteString(fmt.Sprintf("Task: %s\n", TruncateString(task.InitialQuery, 50)))
	report.WriteString(fmt.Sprintf("Complexity: %s\n", complexityToString(task.Complexity)))
	report.WriteString(fmt.Sprintf("Progress: %d completed, %d failed, %d cancelled\n",
		task.CompletedSteps, task.FailedSteps, task.CancelledSteps))

	if len(task.ToolCallsByType) > 0 {
		report.WriteString("Tools used: ")
		first := true
		for tool, count := range task.ToolCallsByType {
			if !first {
				report.WriteString(", ")
			}
			report.WriteString(fmt.Sprintf("%s(%d)", tool, count))
			first = false
		}
		report.WriteString("\n")
	}

	if task.IsStuck {
		report.WriteString(fmt.Sprintf("⚠️ STUCK: %s\n", task.StuckReason))
	}

	return report.String()
}

// GetStatusSummary 獲取狀態摘要（用於注入到系統提示）
func (tt *TaskTracker) GetStatusSummary() string {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	if tt.currentTask == nil {
		return ""
	}

	task := tt.currentTask

	var summary strings.Builder

	// 如果有失敗，提醒
	if task.FailedSteps > 0 {
		summary.WriteString(fmt.Sprintf(" [Failed attempts: %d]", task.FailedSteps))
	}

	// 如果有取消，說明用戶有干預
	if task.CancelledSteps > 0 {
		summary.WriteString(fmt.Sprintf(" [User cancelled: %d]", task.CancelledSteps))
	}

	return summary.String()
}

// IsSimpleTask 是否簡單任務
func (tt *TaskTracker) IsSimpleTask() bool {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	if tt.currentTask == nil {
		return true
	}
	return tt.currentTask.Complexity == ComplexitySimple
}

// GetConsecutiveFails 獲取連續失敗次數
func (tt *TaskTracker) GetConsecutiveFails() int {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	if tt.currentTask == nil {
		return 0
	}
	return tt.currentTask.ConsecutiveFails
}

// GetComplexity 獲取當前任務複雜度
func (tt *TaskTracker) GetComplexity() TaskComplexity {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	if tt.currentTask == nil {
		return ComplexityUnknown
	}
	return tt.currentTask.Complexity
}

// Helper functions

func complexityToString(c TaskComplexity) string {
	switch c {
	case ComplexitySimple:
		return "Simple"
	case ComplexityModerate:
		return "Moderate"
	case ComplexityComplex:
		return "Complex"
	default:
		return "Unknown"
	}
}
