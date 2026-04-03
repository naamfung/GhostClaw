package main

import (
    "context"
    "fmt"
    "log"
    "strings"
    "sync"
    "time"

    "github.com/google/uuid"
)

// SubagentStatus 子代理状态
type SubagentStatus string

const (
    SubagentRunning   SubagentStatus = "running"
    SubagentCompleted SubagentStatus = "completed"
    SubagentFailed    SubagentStatus = "failed"
    SubagentCancelled SubagentStatus = "cancelled"
)

// SubagentTask 子代理任务
type SubagentTask struct {
    ID            string          `json:"id"`
    Task          string          `json:"task"`
    SessionID     string          `json:"session_id"`
    Role          *Role           `json:"-"` // 关联的角色（用于权限控制）
    StartTime     time.Time       `json:"start_time"`
    Status        SubagentStatus  `json:"status"`
    Result        string          `json:"result,omitempty"`
    Iterations    int             `json:"iterations"`
    MaxIterations int             `json:"max_iterations"`

    mu       sync.RWMutex
    ctx      context.Context
    cancel   context.CancelFunc
    done     chan struct{}
}

// SubagentManager 子代理管理器
type SubagentManager struct {
    tasks         map[string]*SubagentTask
    mu            sync.RWMutex
    ctx           context.Context
    cancel        context.CancelFunc
    resultHandler SubagentResultHandler
}

// SubagentResultHandler 子代理结果处理函数
type SubagentResultHandler func(task *SubagentTask)

// 子代理工具黑名单
var subagentToolBlacklist = map[string]bool{
    "spawn":        true,
    "message":      true,
    "spawn_check":  true,
    "spawn_cancel": true,
    "spawn_list":   true,
}

// nilChannel 是一个空的 Channel 实现，用于子代理工具调用时避免 nil pointer panic
// 所有方法都是空实现，不会产生任何输出
type nilChannel struct{}

func (c *nilChannel) WriteChunk(chunk StreamChunk) error { return nil }
func (c *nilChannel) ID() string                         { return "nil" }
func (c *nilChannel) Close() error                       { return nil }
func (c *nilChannel) GetSessionID() string               { return "" }
func (c *nilChannel) HealthCheck() map[string]interface{} {
	return map[string]interface{}{
		"id":      "nil",
		"status":  "operational",
		"message": "Nil channel health check",
	}
}

// NewSubagentManager 创建子代理管理器
func NewSubagentManager() *SubagentManager {
    ctx, cancel := context.WithCancel(context.Background())
    return &SubagentManager{
        tasks:  make(map[string]*SubagentTask),
        ctx:    ctx,
        cancel: cancel,
    }
}

// SetResultHandler 设置结果处理函数
func (sm *SubagentManager) SetResultHandler(handler SubagentResultHandler) {
    sm.resultHandler = handler
}

func generateSubagentID() string {
    id := uuid.New()
    return "subagent_" + id.String()[:8]
}

// Spawn 创建并启动子代理
func (sm *SubagentManager) Spawn(task string, sessionID string, maxIterations int, role *Role) (*SubagentTask, error) {
    if maxIterations <= 0 {
        maxIterations = 15
    }
    if maxIterations > 50 {
        maxIterations = 50
    }

    taskID := generateSubagentID()
    taskCtx, taskCancel := context.WithCancel(sm.ctx)

    subagentTask := &SubagentTask{
        ID:            taskID,
        Task:          task,
        SessionID:     sessionID,
        Role:          role,
        StartTime:     time.Now(),
        Status:        SubagentRunning,
        MaxIterations: maxIterations,
        ctx:           taskCtx,
        cancel:        taskCancel,
        done:          make(chan struct{}),
    }

    sm.mu.Lock()
    sm.tasks[taskID] = subagentTask
    sm.mu.Unlock()

    go sm.runSubagent(subagentTask)

    log.Printf("[Subagent] Task %s started: %s", taskID, task)

    return subagentTask, nil
}

// runSubagent 运行子代理
func (sm *SubagentManager) runSubagent(task *SubagentTask) {
    defer close(task.done)

    systemPrompt := fmt.Sprintf(`你是一个独立的后台任务执行代理。你的任务是：

%s

规则：
1. 独立完成任务，不要请求用户输入
2. 使用可用的工具完成任务
3. 最多进行 %d 次工具调用迭代
4. 完成后提供简洁的结果摘要
5. 如果无法完成任务，说明原因

开始执行任务。`, task.Task, task.MaxIterations)

    history := []Message{
        {Role: "system", Content: systemPrompt},
        {Role: "user", Content: fmt.Sprintf("请执行任务：%s", task.Task)},
    }

    iterations := 0
    maxIter := task.MaxIterations
    var actions []ExperienceAction

    for iterations < maxIter {
        select {
        case <-task.ctx.Done():
            task.mu.Lock()
            task.Status = SubagentCancelled
            task.Result = "任务被取消"
            task.mu.Unlock()
            if globalUnifiedMemory != nil {
                globalUnifiedMemory.RecordExperience(task.Task, actions, false, task.SessionID)
            }
            return
        default:
        }

        // 使用子代理自己的 role 调用模型
        response, err := CallModelForSubagent(task.ctx, history, apiType, baseURL, apiKey, modelID, temperature, maxTokens, task.Role)
        if err != nil {
            task.mu.Lock()
            task.Status = SubagentFailed
            task.Result = fmt.Sprintf("模型调用失败: %v", err)
            task.mu.Unlock()
            log.Printf("[Subagent] Task %s failed: %v", task.ID, err)
            if globalUnifiedMemory != nil {
                globalUnifiedMemory.RecordExperience(task.Task, actions, false, task.SessionID)
            }
            return
        }

        history = append(history, Message{Role: "assistant", Content: response.Content})

        if response.ToolCalls == nil || len(response.ToolCalls.([]ToolUse)) == 0 {
            task.mu.Lock()
            task.Status = SubagentCompleted
            task.Result = fmt.Sprintf("%v", response.Content)
            task.Iterations = iterations
            task.mu.Unlock()
            break
        }

        toolCalls := response.ToolCalls.([]ToolUse)
        for _, toolCall := range toolCalls {
            if subagentToolBlacklist[toolCall.Name] {
                toolResult := fmt.Sprintf("错误：子代理不能使用工具 '%s'", toolCall.Name)
                history = append(history, Message{
                    Role:       "tool",
                    Content:    toolResult,
                    ToolCallID: toolCall.ID,
                })
                continue
            }

            // 使用 nilChannel 避免 panic
            toolResult := executeToolForSubagent(task.ctx, toolCall.Name, toolCall.Input, task.Role)

            actions = append(actions, ExperienceAction{
                ToolName: toolCall.Name,
                Input:    toolCall.Input,
                Output:   truncateString(toolResult, 200),
            })

            history = append(history, Message{
                Role:       "tool",
                Content:    toolResult,
                ToolCallID: toolCall.ID,
            })
        }

        iterations++
        task.mu.Lock()
        task.Iterations = iterations
        task.mu.Unlock()

        if response.StopReason == "end_turn" || response.StopReason == "stop" {
            task.mu.Lock()
            task.Status = SubagentCompleted
            task.Result = fmt.Sprintf("%v", response.Content)
            task.mu.Unlock()
            break
        }
    }

    if iterations >= maxIter {
        task.mu.Lock()
        if task.Status == SubagentRunning {
            task.Status = SubagentCompleted
            task.Result = "达到最大迭代次数，任务可能未完全完成"
        }
        task.mu.Unlock()
    }

    log.Printf("[Subagent] Task %s completed with status: %s, iterations: %d", task.ID, task.Status, iterations)

    if globalUnifiedMemory != nil {
        success := task.Status == SubagentCompleted
        if err := globalUnifiedMemory.RecordExperience(task.Task, actions, success, task.SessionID); err != nil {
            log.Printf("[Subagent] Failed to record experience: %v", err)
        } else {
            log.Printf("[Subagent] Experience recorded for task %s (success=%v)", task.ID, success)
        }
    }

    if sm.resultHandler != nil {
        sm.resultHandler(task)
    }
}

// CallModelForSubagent 为子代理调用模型（非流式，支持 role 权限过滤）
func CallModelForSubagent(ctx context.Context, history []Message, apiType, baseURL, apiKey, modelID string,
    temperature float64, maxTokens int, role *Role) (Response, error) {

    // 使用 CallModel 流式接口，收集所有 chunk 后组装响应
    chunkChan, err := CallModel(ctx, history, apiType, baseURL, apiKey, modelID, temperature, maxTokens, false, false, role)
    if err != nil {
        return Response{}, err
    }

    var content strings.Builder
    var reasoning strings.Builder
    var toolCalls []map[string]interface{}
    var finishReason string

    for chunk := range chunkChan {
        if chunk.Error != "" {
            return Response{}, fmt.Errorf("model error: %s", chunk.Error)
        }
        if chunk.Content != "" {
            content.WriteString(chunk.Content)
        }
        if chunk.ReasoningContent != "" {
            reasoning.WriteString(chunk.ReasoningContent)
        }
        if chunk.ToolCalls != nil {
            toolCalls = chunk.ToolCalls
        }
        if chunk.Done {
            finishReason = chunk.FinishReason
            break
        }
    }

    response := Response{
        StopReason: finishReason,
    }

    if toolCalls != nil {
        response.Content = toolCalls
    } else {
        response.Content = content.String()
    }

    if reasoning.Len() > 0 {
        response.ReasoningContent = reasoning.String()
    }

    return response, nil
}

// executeToolForSubagent 为子代理执行工具，使用 nilChannel 避免 panic
func executeToolForSubagent(ctx context.Context, toolName string, args map[string]interface{}, role *Role) string {
    // 使用空的 channel 实现，避免 nil pointer panic
    dummyCh := &nilChannel{}
    result := executeTool(ctx, "", toolName, args, dummyCh, role)
    contentStr, _ := result.Content.(string)
    if result.Meta.Status == TaskStatusFailed {
        return "Error: " + contentStr
    }
    return contentStr
}

// Check 检查子代理状态
func (sm *SubagentManager) Check(taskID string) (*SubagentTask, error) {
    sm.mu.RLock()
    defer sm.mu.RUnlock()

    task, exists := sm.tasks[taskID]
    if !exists {
        return nil, fmt.Errorf("subagent task %s not found", taskID)
    }

    return task, nil
}

// Cancel 取消子代理
func (sm *SubagentManager) Cancel(taskID string) error {
    sm.mu.RLock()
    task, exists := sm.tasks[taskID]
    sm.mu.RUnlock()

    if !exists {
        return fmt.Errorf("subagent task %s not found", taskID)
    }

    task.mu.Lock()
    defer task.mu.Unlock()

    if task.Status != SubagentRunning {
        return fmt.Errorf("subagent task %s is not running (status: %s)", taskID, task.Status)
    }

    task.cancel()
    task.Status = SubagentCancelled

    log.Printf("[Subagent] Task %s cancelled", taskID)

    return nil
}

// List 列出所有子代理任务
func (sm *SubagentManager) List() []*SubagentTask {
    sm.mu.RLock()
    defer sm.mu.RUnlock()

    tasks := make([]*SubagentTask, 0, len(sm.tasks))
    for _, task := range sm.tasks {
        tasks = append(tasks, task)
    }
    return tasks
}

// ListBySession 列出指定会话的子代理任务
func (sm *SubagentManager) ListBySession(sessionID string) []*SubagentTask {
    sm.mu.RLock()
    defer sm.mu.RUnlock()

    tasks := make([]*SubagentTask, 0)
    for _, task := range sm.tasks {
        if task.SessionID == sessionID {
            tasks = append(tasks, task)
        }
    }
    return tasks
}

// CancelBySession 取消指定会话的所有子代理
func (sm *SubagentManager) CancelBySession(sessionID string) int {
    sm.mu.RLock()
    tasks := make([]*SubagentTask, 0)
    for _, task := range sm.tasks {
        if task.SessionID == sessionID && task.Status == SubagentRunning {
            tasks = append(tasks, task)
        }
    }
    sm.mu.RUnlock()

    count := 0
    for _, task := range tasks {
        if err := sm.Cancel(task.ID); err == nil {
            count++
        }
    }

    return count
}

// Remove 移除已完成或已取消的子代理
func (sm *SubagentManager) Remove(taskID string) error {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    task, exists := sm.tasks[taskID]
    if !exists {
        return fmt.Errorf("subagent task %s not found", taskID)
    }

    task.mu.RLock()
    status := task.Status
    task.mu.RUnlock()

    if status == SubagentRunning {
        return fmt.Errorf("cannot remove running subagent task %s, cancel it first", taskID)
    }

    delete(sm.tasks, taskID)
    log.Printf("[Subagent] Task %s removed", taskID)
    return nil
}

// GetTaskInfo 获取任务信息（用于返回给模型）
func (sm *SubagentManager) GetTaskInfo(taskID string) (map[string]interface{}, error) {
    task, err := sm.Check(taskID)
    if err != nil {
        return nil, err
    }

    task.mu.RLock()
    defer task.mu.RUnlock()

    info := map[string]interface{}{
        "task_id":         task.ID,
        "task":            task.Task,
        "session_id":      task.SessionID,
        "status":          string(task.Status),
        "iterations":      task.Iterations,
        "max_iterations":  task.MaxIterations,
        "start_time":      task.StartTime.Format(time.RFC3339),
        "runtime_seconds": time.Since(task.StartTime).Seconds(),
    }

    if task.Result != "" {
        info["result"] = task.Result
    }

    return info, nil
}

// Stop 停止子代理管理器
func (sm *SubagentManager) Stop() {
    sm.cancel()

    sm.mu.Lock()
    defer sm.mu.Unlock()

    for _, task := range sm.tasks {
        task.mu.Lock()
        if task.Status == SubagentRunning {
            task.cancel()
            task.Status = SubagentCancelled
        }
        task.mu.Unlock()
    }
}

