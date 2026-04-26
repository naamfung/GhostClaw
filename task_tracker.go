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
        ComplexitySimple    TaskComplexity = 1 // 簡單任務：單次問答、查詢
        ComplexityModerate  TaskComplexity = 2 // 中等任務：需要少量工具調用
        ComplexityComplex   TaskComplexity = 3 // 複雜任務：多步驟、需要規劃
        ComplexityUnknown   TaskComplexity = 0 // 未知：初始狀態
)

// TaskIntent 用戶意圖分類
// 用於決定是否啟用工作模式（結構化任務追蹤 + 退出守衛）
type TaskIntent int

const (
        IntentChat  TaskIntent = 0 // 閒聊/問答（Free Mode，無退出守衛）
        IntentTask  TaskIntent = 1 // 結構化任務（Work Mode，todo 驅動退出守衛）
        IntentOther TaskIntent = 2 // 其他（命令、模糊意圖）
)

// TaskState 任務狀態追蹤
type TaskState struct {
        mu sync.RWMutex

        // 任務識別
        InitialQuery     string         // 用戶原始請求
        Complexity       TaskComplexity // 評估的複雜度
        Intent           TaskIntent     // 意圖分類（chat/task/other）
        StartTime        time.Time      // 任務開始時間

        // 進度追蹤
        TotalSteps       int            // 預估總步驟數
        CompletedSteps   int            // 已完成步驟數
        FailedSteps      int            // 失敗步驟數
        CancelledSteps   int            // 取消步驟數

        // 工具調用追蹤
        ToolCallsByType  map[string]int // 按類型統計工具調用次數
        RecentToolCalls  []ToolCallRecord // 最近工具調用記錄
        LastSuccessTime  time.Time      // 最後一次成功時間
        LastFailedTime   time.Time      // 最後一次失敗時間
        LastFailedTool   string         // 最後一次失敗的工具
        ConsecutiveFails int            // 連續失敗次數
        RepeatedFailures map[string]int // 按工具統計重複失敗

        // 狀態標記
        IsStuck          bool           // 是否卡住
        IsCompleted      bool           // 是否完成
        StuckReason      string         // 卡住原因
        NeedsIntervention bool          // 是否需要用戶干預
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
func (tt *TaskTracker) StartNewTask(query string) {
        tt.mu.Lock()
        defer tt.mu.Unlock()

        complexity := tt.estimateComplexity(query)
        intent := tt.classifyIntent(query, complexity)

        tt.currentTask = &TaskState{
                InitialQuery:     query,
                Complexity:       complexity,
                Intent:           intent,
                StartTime:        time.Now(),
                ToolCallsByType:  make(map[string]int),
                RecentToolCalls:  make([]ToolCallRecord, 0, 20),
                RepeatedFailures: make(map[string]int),
        }
}

// estimateComplexity 評估任務複雜度
func (tt *TaskTracker) estimateComplexity(query string) TaskComplexity {
        queryLower := strings.ToLower(query)

        // 簡單任務特徵
        simplePatterns := []string{
                "what is", "what's", "tell me", "explain", "define",
                "是什麼", "什麼是", "解釋", "說明", "介紹一下",
                "help", "how to", "怎麼", "如何",
        }
        for _, p := range simplePatterns {
                if strings.Contains(queryLower, p) {
                        return ComplexitySimple
                }
        }

        // 複雜任務特徵
        complexPatterns := []string{
                "create", "build", "implement", "develop", "write a",
                "refactor", "migrate", "integrate", "set up", "configure",
                "創建", "實現", "開發", "編寫", "構建",
                "重構", "遷移", "集成", "配置",
                "and then", "after that", "step by step", "multiple",
                "然後", "接著", "步驟", "多個",
        }
        for _, p := range complexPatterns {
                if strings.Contains(queryLower, p) {
                        return ComplexityComplex
                }
        }

        // 檢查是否有多個任務
        taskSeparators := []string{" and ", " also ", " then ", " after ", "、", "，還有", "並且"}
        for _, sep := range taskSeparators {
                if strings.Count(queryLower, sep) >= 2 {
                        return ComplexityComplex
                }
        }

        return ComplexityModerate
}

// classifyIntent 分類用戶意圖（task vs chat vs other）
// task 意圖會觸發工作模式：系統提示模型使用 todos，退出守衛基於 todo 狀態
func (tt *TaskTracker) classifyIntent(query string, complexity TaskComplexity) TaskIntent {
        queryLower := strings.ToLower(query)

        // === Task 模式特徵（高優先級） ===
        taskPatterns := []string{
                // 英文任務動詞
                "create", "build", "implement", "develop", "write a",
                "refactor", "migrate", "integrate", "set up", "configure",
                "fix", "debug", "modify", "update", "deploy", "install",
                "rename", "delete", "remove", "generate", "convert",
                // 中文任務動詞
                "創建", "實現", "開發", "編寫", "構建",
                "重構", "遷移", "集成", "配置", "修復",
                "修改", "更新", "部署", "安裝",
                "重命名", "刪除", "生成", "轉換",
                "幫我", "請", "做一個", "寫一個", "建一個",
                // 多步驟標誌
                "and then", "after that", "step by step", "multiple",
                "然後", "接著", "步驟", "多個", "分步",
        }
        for _, p := range taskPatterns {
                if strings.Contains(queryLower, p) {
                        return IntentTask
                }
        }

        // === Chat 模式特徵 ===
        chatPatterns := []string{
                "what is", "what's", "tell me", "explain", "define",
                "是什麼", "什麼是", "解釋", "說明", "介紹一下",
                "你好", "嗨", "hello", "hi", "hey",
                "謝謝", "感謝", "thanks", "thank you",
                "你是誰", "how are", "how do",
        }
        for _, p := range chatPatterns {
                if strings.Contains(queryLower, p) {
                        return IntentChat
                }
        }

        // === 默認：根據複雜度判斷 ===
        if complexity >= ComplexityModerate {
                return IntentTask
        }
        return IntentOther
}

// IsWorkMode 是否處於工作模式
// 工作模式 = 意圖為 task 且複雜度 >= moderate
// 觸發系統提示注入 + AgentLoop 退出守衛
func (tt *TaskTracker) IsWorkMode() bool {
        tt.mu.RLock()
        defer tt.mu.RUnlock()

        if tt.currentTask == nil {
                return false
        }
        return tt.currentTask.Intent == IntentTask && tt.currentTask.Complexity >= ComplexityModerate
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
                task.RepeatedFailures[toolName] = 0
        case TaskStatusFailed:
                task.FailedSteps++
                task.LastFailedTime = now
                task.LastFailedTool = toolName
                task.ConsecutiveFails++
                task.RepeatedFailures[toolName]++
        case TaskStatusCancelled:
                task.CancelledSteps++
                task.ConsecutiveFails = 0
        }

        // 檢測是否卡住
        tt.detectStuckState()
}

// detectStuckState 檢測卡住狀態
func (tt *TaskTracker) detectStuckState() {
        task := tt.currentTask
        if task == nil {
                return
        }

        // 條件1：連續失敗次數過多
        if task.ConsecutiveFails >= 3 {
                task.IsStuck = true
                task.StuckReason = fmt.Sprintf("Consecutive failures (%d times) on tool: %s",
                        task.ConsecutiveFails, task.LastFailedTool)
                task.NeedsIntervention = true
                return
        }

        // 條件2：同一工具重複失敗過多
        for tool, count := range task.RepeatedFailures {
                if count >= 3 {
                        task.IsStuck = true
                        task.StuckReason = fmt.Sprintf("Tool '%s' failed %d times", tool, count)
                        task.NeedsIntervention = true
                        return
                }
        }

        // 條件3：長時間無成功但有大量調用
        if !task.LastSuccessTime.IsZero() {
                timeSinceSuccess := time.Since(task.LastSuccessTime)
                totalCalls := task.CompletedSteps + task.FailedSteps + task.CancelledSteps
                if timeSinceSuccess > 2*time.Minute && totalCalls > 10 && task.FailedSteps > task.CompletedSteps {
                        task.IsStuck = true
                        task.StuckReason = "High failure rate over extended period"
                        task.NeedsIntervention = true
                        return
                }
        }

        // 條件4：總步驟數過多（可能是無限循環）
        totalSteps := task.CompletedSteps + task.FailedSteps + task.CancelledSteps
        if totalSteps > 30 {
                task.IsStuck = true
                task.StuckReason = "Too many steps, possible infinite loop"
                task.NeedsIntervention = true
                return
        }

        task.IsStuck = false
        task.NeedsIntervention = false
}

// MarkCompleted 標記任務完成
func (tt *TaskTracker) MarkCompleted() {
        tt.mu.Lock()
        defer tt.mu.Unlock()

        if tt.currentTask != nil {
                tt.currentTask.IsCompleted = true
        }
}

// ShouldPromptTodo 是否應該提示更新 todo
func (tt *TaskTracker) ShouldPromptTodo() (bool, string) {
        tt.mu.RLock()
        defer tt.mu.RUnlock()

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
                return true, fmt.Sprintf(
                        "[SYSTEM_ALERT]The task appears to be stuck: %s. Please either: (1) try a different approach, (2) ask the user for guidance, or (3) update your todo list to reflect current progress.[/SYSTEM_ALERT]",
                        task.StuckReason,
                )
        }

        // 複雜任務：根據已完成步驟數決定
        totalSteps := task.CompletedSteps + task.FailedSteps + task.CancelledSteps

        if task.Complexity == ComplexityComplex {
                // 複雜任務每5步檢查一次
                if totalSteps > 0 && totalSteps%5 == 0 && task.CompletedSteps > 0 {
                        return true, fmt.Sprintf(
                                "[PROGRESS_CHECK]Completed %d steps, %d failed. Please update your todo list and assess if the approach is working.[/PROGRESS_CHECK]",
                                task.CompletedSteps, task.FailedSteps,
                        )
                }
        } else {
                // 中等任務每8步檢查一次
                if totalSteps > 0 && totalSteps%8 == 0 && task.CompletedSteps > 0 {
                        return true, "[PROGRESS_CHECK]Please update your todo list if needed.[/PROGRESS_CHECK]"
                }
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
