package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// TaskComplexity 任务复杂度评估
type TaskComplexity int

const (
	ComplexitySimple    TaskComplexity = 1 // 简单任务：单次问答、查询
	ComplexityModerate  TaskComplexity = 2 // 中等任务：需要少量工具调用
	ComplexityComplex   TaskComplexity = 3 // 复杂任务：多步骤、需要规划
	ComplexityUnknown   TaskComplexity = 0 // 未知：初始状态
)

// TaskState 任务状态追踪
type TaskState struct {
	mu sync.RWMutex

	// 任务识别
	InitialQuery     string         // 用户原始请求
	Complexity       TaskComplexity // 评估的复杂度
	StartTime        time.Time      // 任务开始时间

	// 进度追踪
	TotalSteps       int            // 预估总步骤数
	CompletedSteps   int            // 已完成步骤数
	FailedSteps      int            // 失败步骤数
	CancelledSteps   int            // 取消步骤数

	// 工具调用追踪
	ToolCallsByType  map[string]int // 按类型统计工具调用次数
	RecentToolCalls  []ToolCallRecord // 最近工具调用记录
	LastSuccessTime  time.Time      // 最后一次成功时间
	LastFailedTime   time.Time      // 最后一次失败时间
	LastFailedTool   string         // 最后一次失败的工具
	ConsecutiveFails int            // 连续失败次数
	RepeatedFailures map[string]int // 按工具统计重复失败

	// 状态标记
	IsStuck          bool           // 是否卡住
	IsCompleted      bool           // 是否完成
	StuckReason      string         // 卡住原因
	NeedsIntervention bool          // 是否需要用户干预
}

// ToolCallRecord 工具调用记录
type ToolCallRecord struct {
	Time     time.Time
	ToolName string
	Status   TaskStatus
	Input    string // 简化的输入摘要
	Output   string // 简化的输出摘要
}

// TaskTracker 任务追踪器
type TaskTracker struct {
	currentTask *TaskState
	mu          sync.RWMutex
}

// NewTaskTracker 创建任务追踪器
func NewTaskTracker() *TaskTracker {
	return &TaskTracker{}
}

// StartNewTask 开始新任务
func (tt *TaskTracker) StartNewTask(query string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	tt.currentTask = &TaskState{
		InitialQuery:     query,
		Complexity:       ComplexityUnknown,
		StartTime:        time.Now(),
		ToolCallsByType:  make(map[string]int),
		RecentToolCalls:  make([]ToolCallRecord, 0, 20),
		RepeatedFailures: make(map[string]int),
	}

	// 评估任务复杂度
	tt.currentTask.Complexity = tt.estimateComplexity(query)
}

// estimateComplexity 评估任务复杂度
func (tt *TaskTracker) estimateComplexity(query string) TaskComplexity {
	queryLower := strings.ToLower(query)

	// 简单任务特征
	simplePatterns := []string{
		"what is", "what's", "tell me", "explain", "define",
		"是什么", "什么是", "解释", "说明", "介绍一下",
		"help", "how to", "怎么", "如何",
	}
	for _, p := range simplePatterns {
		if strings.Contains(queryLower, p) {
			return ComplexitySimple
		}
	}

	// 复杂任务特征
	complexPatterns := []string{
		"create", "build", "implement", "develop", "write a",
		"refactor", "migrate", "integrate", "set up", "configure",
		"创建", "实现", "开发", "编写", "构建",
		"重构", "迁移", "集成", "配置",
		"and then", "after that", "step by step", "multiple",
		"然后", "接着", "步骤", "多个",
	}
	for _, p := range complexPatterns {
		if strings.Contains(queryLower, p) {
			return ComplexityComplex
		}
	}

	// 检查是否有多个任务
	taskSeparators := []string{" and ", " also ", " then ", " after ", "、", "，还有", "并且"}
	for _, sep := range taskSeparators {
		if strings.Count(queryLower, sep) >= 2 {
			return ComplexityComplex
		}
	}

	return ComplexityModerate
}

// RecordToolCall 记录工具调用
func (tt *TaskTracker) RecordToolCall(toolName string, status TaskStatus, inputSummary, outputSummary string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	if tt.currentTask == nil {
		return
	}

	task := tt.currentTask
	now := time.Now()

	// 更新工具调用统计
	task.ToolCallsByType[toolName]++

	// 添加到最近调用记录
	record := ToolCallRecord{
		Time:     now,
		ToolName: toolName,
		Status:   status,
		Input:    truncateString(inputSummary, 100),
		Output:   truncateString(outputSummary, 100),
	}
	task.RecentToolCalls = append(task.RecentToolCalls, record)

	// 保留最近20条
	if len(task.RecentToolCalls) > 20 {
		task.RecentToolCalls = task.RecentToolCalls[1:]
	}

	// 更新步骤计数
	switch status {
	case TaskStatusSuccess:
		task.CompletedSteps++
		task.LastSuccessTime = now
		task.ConsecutiveFails = 0
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

	// 检测是否卡住
	tt.detectStuckState()
}

// detectStuckState 检测卡住状态
func (tt *TaskTracker) detectStuckState() {
	task := tt.currentTask
	if task == nil {
		return
	}

	// 条件1：连续失败次数过多
	if task.ConsecutiveFails >= 3 {
		task.IsStuck = true
		task.StuckReason = fmt.Sprintf("Consecutive failures (%d times) on tool: %s",
			task.ConsecutiveFails, task.LastFailedTool)
		task.NeedsIntervention = true
		return
	}

	// 条件2：同一工具重复失败过多
	for tool, count := range task.RepeatedFailures {
		if count >= 3 {
			task.IsStuck = true
			task.StuckReason = fmt.Sprintf("Tool '%s' failed %d times", tool, count)
			task.NeedsIntervention = true
			return
		}
	}

	// 条件3：长时间无成功但有大量调用
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

	// 条件4：总步骤数过多（可能是无限循环）
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

// MarkCompleted 标记任务完成
func (tt *TaskTracker) MarkCompleted() {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	if tt.currentTask != nil {
		tt.currentTask.IsCompleted = true
	}
}

// ShouldPromptTodo 是否应该提示更新 todo
func (tt *TaskTracker) ShouldPromptTodo() (bool, string) {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	if tt.currentTask == nil {
		return false, ""
	}

	task := tt.currentTask

	// 简单任务不需要 todo
	if task.Complexity == ComplexitySimple {
		return false, ""
	}

	// 如果已经卡住，提示用户干预
	if task.IsStuck && task.NeedsIntervention {
		return true, fmt.Sprintf(
			"<system_alert>The task appears to be stuck: %s. Please either: (1) try a different approach, (2) ask the user for guidance, or (3) update your todo list to reflect current progress.</system_alert>",
			task.StuckReason,
		)
	}

	// 复杂任务：根据已完成步骤数决定
	totalSteps := task.CompletedSteps + task.FailedSteps + task.CancelledSteps

	if task.Complexity == ComplexityComplex {
		// 复杂任务每5步检查一次
		if totalSteps > 0 && totalSteps%5 == 0 && task.CompletedSteps > 0 {
			return true, fmt.Sprintf(
				"<progress_check>Completed %d steps, %d failed. Please update your todo list and assess if the approach is working.</progress_check>",
				task.CompletedSteps, task.FailedSteps,
			)
		}
	} else {
		// 中等任务每8步检查一次
		if totalSteps > 0 && totalSteps%8 == 0 && task.CompletedSteps > 0 {
			return true, "<progress_check>Please update your todo list if needed.</progress_check>"
		}
	}

	return false, ""
}

// GetProgressReport 获取进度报告
func (tt *TaskTracker) GetProgressReport() string {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	if tt.currentTask == nil {
		return "No active task"
	}

	task := tt.currentTask

	var report strings.Builder
	report.WriteString(fmt.Sprintf("Task: %s\n", truncateString(task.InitialQuery, 50)))
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

// GetStatusSummary 获取状态摘要（用于注入到系统提示）
func (tt *TaskTracker) GetStatusSummary() string {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	if tt.currentTask == nil {
		return ""
	}

	task := tt.currentTask

	var summary strings.Builder

	// 如果有失败，提醒
	if task.FailedSteps > 0 {
		summary.WriteString(fmt.Sprintf(" [Failed attempts: %d]", task.FailedSteps))
	}

	// 如果有取消，说明用户有干预
	if task.CancelledSteps > 0 {
		summary.WriteString(fmt.Sprintf(" [User cancelled: %d]", task.CancelledSteps))
	}

	return summary.String()
}

// IsSimpleTask 是否简单任务
func (tt *TaskTracker) IsSimpleTask() bool {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	if tt.currentTask == nil {
		return true
	}
	return tt.currentTask.Complexity == ComplexitySimple
}

// GetConsecutiveFails 获取连续失败次数
func (tt *TaskTracker) GetConsecutiveFails() int {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	if tt.currentTask == nil {
		return 0
	}
	return tt.currentTask.ConsecutiveFails
}

// GetComplexity 获取当前任务复杂度
func (tt *TaskTracker) GetComplexity() TaskComplexity {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	if tt.currentTask == nil {
		return ComplexityUnknown
	}
	return tt.currentTask.Complexity
}

// Helper functions

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

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
