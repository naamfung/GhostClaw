package main

import (
    "context"
    "encoding/json"
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
    Depth         int             `json:"depth"`                // 当前嵌套深度（0=顶层，1=子代理的子代理，等）
    CredentialOverride string     `json:"credential_override"` // 如果设置，使用此特定 API key 而非全局
    ModelOverride      string     `json:"model_override"`      // 如果设置，使用此特定模型而非全局

    mu       sync.RWMutex
    ctx      context.Context
    cancel   context.CancelFunc
    done     chan struct{}
}

// SubagentManager 子代理管理器
type SubagentManager struct {
    tasks                     map[string]*SubagentTask
    mu                        sync.RWMutex
    ctx                       context.Context
    cancel                    context.CancelFunc
    resultHandler             SubagentResultHandler
    MaxDepth                  int    // 最大允许的嵌套深度（默认：2）
    DefaultSubagentModel      string // 子代理使用的默认模型（可配置）
    DefaultSubagentCredential string // 子代理使用的默认凭据 ID（可配置）
    sessionDepth              map[string]int // 每个 session 的当前嵌套深度
    sessionDepthMu            sync.Mutex
    spawnCount                int64 // 跟踪 spawn 次数，用于触发清理
}

// SubagentResultHandler 子代理结果处理函数
type SubagentResultHandler func(task *SubagentTask)

// 子代理工具黑名单
var subagentToolBlacklist = map[string]bool{
    "spawn":        true,
    "spawn_batch":   true,
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
        tasks:        make(map[string]*SubagentTask),
        ctx:          ctx,
        cancel:       cancel,
        MaxDepth:     2,
        sessionDepth: make(map[string]int),
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

// incrementSessionDepth 递增指定 session 的深度计数器
func (sm *SubagentManager) incrementSessionDepth(sessionID string) int {
    sm.sessionDepthMu.Lock()
    defer sm.sessionDepthMu.Unlock()
    sm.sessionDepth[sessionID]++
    return sm.sessionDepth[sessionID]
}

// decrementSessionDepth 递减指定 session 的深度计数器
func (sm *SubagentManager) decrementSessionDepth(sessionID string) {
    sm.sessionDepthMu.Lock()
    defer sm.sessionDepthMu.Unlock()
    if sm.sessionDepth[sessionID] > 0 {
        sm.sessionDepth[sessionID]--
    }
}

// getSessionDepth 获取指定 session 的当前深度
func (sm *SubagentManager) getSessionDepth(sessionID string) int {
    sm.sessionDepthMu.Lock()
    defer sm.sessionDepthMu.Unlock()
    return sm.sessionDepth[sessionID]
}

// Spawn 创建并启动子代理
func (sm *SubagentManager) Spawn(task string, sessionID string, maxIterations int, role *Role) (*SubagentTask, error) {
    return sm.SpawnWithOverrides(task, sessionID, maxIterations, role, 0, "", "")
}

// SpawnWithOverrides 创建并启动子代理，支持深度、模型和凭据覆盖
func (sm *SubagentManager) SpawnWithOverrides(task string, sessionID string, maxIterations int, role *Role, depth int, modelOverride, credentialOverride string) (*SubagentTask, error) {
    if maxIterations <= 0 {
        maxIterations = 15
    }
    if maxIterations > 50 {
        maxIterations = 50
    }

    // 深度限制检查
    currentDepth := sm.getSessionDepth(sessionID)
    effectiveDepth := currentDepth + depth
    if effectiveDepth >= sm.MaxDepth {
        return nil, fmt.Errorf("maximum subagent depth reached (current: %d, max: %d)", effectiveDepth, sm.MaxDepth)
    }

    // 如果指定了凭据 ID，验证凭据是否存在
    if credentialOverride != "" {
        if globalCredentialPool != nil {
            if _, err := globalCredentialPool.GetCredentialByID(credentialOverride); err != nil {
                return nil, fmt.Errorf("credential %q not found: %v", credentialOverride, err)
            }
        }
    }

    taskID := generateSubagentID()
    taskCtx, taskCancel := context.WithCancel(sm.ctx)

    subagentTask := &SubagentTask{
        ID:                 taskID,
        Task:               task,
        SessionID:          sessionID,
        Role:               role,
        StartTime:          time.Now(),
        Status:             SubagentRunning,
        MaxIterations:      maxIterations,
        Depth:              effectiveDepth,
        CredentialOverride: credentialOverride,
        ModelOverride:      modelOverride,
        ctx:                taskCtx,
        cancel:             taskCancel,
        done:               make(chan struct{}),
    }

    sm.mu.Lock()
    sm.tasks[taskID] = subagentTask
    sm.mu.Unlock()

    // Periodically clean up stale tasks
    sm.maybeCleanupStaleTasks()

    // 递增 session 深度
    sm.incrementSessionDepth(sessionID)

    go func() {
        sm.runSubagent(subagentTask)
        // 單個 Spawn 任務完成後遞減 session 深度
        sm.decrementSessionDepth(sessionID)
    }()

    log.Printf("[Subagent] Task %s started (depth=%d): %s", taskID, effectiveDepth, task)

    return subagentTask, nil
}

// SpawnMultiple 创建多个子代理任务并并行运行
func (sm *SubagentManager) SpawnMultiple(tasks []string, sessionID string, maxIterations int, role *Role) ([]*SubagentTask, error) {
    return sm.SpawnMultipleWithOverrides(tasks, sessionID, maxIterations, role, "", "")
}

// SpawnMultipleWithOverrides 创建多个子代理任务并并行运行，支持模型和凭据覆盖
func (sm *SubagentManager) SpawnMultipleWithOverrides(tasks []string, sessionID string, maxIterations int, role *Role, modelOverride, credentialOverride string) ([]*SubagentTask, error) {
    if len(tasks) == 0 {
        return nil, fmt.Errorf("no tasks provided")
    }
    if len(tasks) > 10 {
        return nil, fmt.Errorf("too many parallel tasks (max 10, got %d)", len(tasks))
    }
    if maxIterations <= 0 {
        maxIterations = 15
    }
    if maxIterations > 50 {
        maxIterations = 50
    }

    // 深度限制检查：嵌套深度 = 當前調用深度 + 1（一層 Spawn）。
    // SpawnMultiple 不使用 sessionDepth 共享計數器（會把並發數當嵌套深度），
    // 而是在每個 SubagentTask.Depth 中記錄實際嵌套層級。
    currentDepth := sm.getSessionDepth(sessionID)
    effectiveDepth := currentDepth + 1
    if effectiveDepth >= sm.MaxDepth {
        return nil, fmt.Errorf("maximum subagent depth reached (current: %d, max: %d)", effectiveDepth, sm.MaxDepth)
    }

    // 如果指定了凭据 ID，验证凭据是否存在
    if credentialOverride != "" {
        if globalCredentialPool != nil {
            if _, err := globalCredentialPool.GetCredentialByID(credentialOverride); err != nil {
                return nil, fmt.Errorf("credential %q not found: %v", credentialOverride, err)
            }
        }
    }

    effectiveDepth = currentDepth + 1
    var spawnedTasks []*SubagentTask

    // SpawnMultiple 整體遞增一次 session 深度（表示一層嵌套），
    // 而非每個並行任務各遞增一次（否則並發數會被誤算為嵌套深度）。
    // defer 在所有並行任務完成後（waitGroup）遞減。
    sm.incrementSessionDepth(sessionID)
    var wg sync.WaitGroup

    for _, taskDesc := range tasks {
        if taskDesc == "" {
            continue
        }

        taskID := generateSubagentID()
        taskCtx, taskCancel := context.WithCancel(sm.ctx)

        subagentTask := &SubagentTask{
            ID:                 taskID,
            Task:               taskDesc,
            SessionID:          sessionID,
            Role:               role,
            StartTime:          time.Now(),
            Status:             SubagentRunning,
            MaxIterations:      maxIterations,
            Depth:              effectiveDepth,
            CredentialOverride: credentialOverride,
            ModelOverride:      modelOverride,
            ctx:                taskCtx,
            cancel:             taskCancel,
            done:               make(chan struct{}),
        }

        sm.mu.Lock()
        sm.tasks[taskID] = subagentTask
        sm.mu.Unlock()

        wg.Add(1)
        go func(t *SubagentTask) {
            defer wg.Done()
            // 注意：runSubagent 內部的 defer decrementSessionDepth 已移除，
            // 改為由 SpawnMultiple 統一在所有任務完成後遞減一次。
            sm.runSubagent(t)
        }(subagentTask)

        spawnedTasks = append(spawnedTasks, subagentTask)
        log.Printf("[Subagent] Batch task %s started (depth=%d): %s", taskID, effectiveDepth, TruncateString(taskDesc, 60))
    }

    if len(spawnedTasks) == 0 {
        sm.decrementSessionDepth(sessionID) // 沒有實際啟動任務，撤回 increment
        return nil, fmt.Errorf("no valid tasks provided (all empty)")
    }

    // 所有並行任務完成後遞減 session 深度
    go func() {
        wg.Wait()
        sm.decrementSessionDepth(sessionID)
    }()

    return spawnedTasks, nil
}

// runSubagent 运行子代理
func (sm *SubagentManager) runSubagent(task *SubagentTask) {
    defer close(task.done)
    // 注意：sessionDepth 的遞減由調用者（SpawnWithOverrides 或 SpawnMultiple）負責，
    // runSubagent 本身不管理 sessionDepth，以避免並行任務的深度計數問題。

    // 显式深度检查警告
    if task.Depth >= sm.MaxDepth {
        log.Printf("[Subagent] WARNING: Task %s running at depth %d >= max depth %d", task.ID, task.Depth, sm.MaxDepth)
    }

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

        // 使用子代理自己的 role 调用模型（支持凭据和模型路由）
        response, err := CallModelForSubagent(task.ctx, task, history)
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

        // 安全提取工具調用：使用 comma-ok 斷言避免 panic。
        // ToolCalls 的實際類型是 []map[string]interface{}（來自 StreamChunk），
        // 而非 []ToolUse（結構體類型）。
        rawToolCalls, hasToolCalls := response.ToolCalls.([]map[string]interface{})
        if !hasToolCalls || len(rawToolCalls) == 0 {
            task.mu.Lock()
            task.Status = SubagentCompleted
            task.Result = fmt.Sprintf("%v", response.Content)
            task.Iterations = iterations
            task.mu.Unlock()
            break
        }

        for _, tc := range rawToolCalls {
            // 從 map 中安全提取工具名稱和調用 ID
            fnMap, _ := tc["function"].(map[string]interface{})
            toolName, _ := fnMap["name"].(string)
            toolCallID, _ := tc["id"].(string)

            // 解析參數：OpenAI 格式在 function.arguments 中，Anthropic 格式在 input 中
            var toolArgs map[string]interface{}
            if argsStr, ok := fnMap["arguments"].(string); ok && argsStr != "" {
                json.Unmarshal([]byte(argsStr), &toolArgs)
            }
            if inputMap, ok := tc["input"].(map[string]interface{}); ok {
                toolArgs = inputMap
            }
            if toolArgs == nil {
                toolArgs = make(map[string]interface{})
            }

            if subagentToolBlacklist[toolName] {
                toolResult := fmt.Sprintf("错误：子代理不能使用工具 '%s'", toolName)
                history = append(history, Message{
                    Role:       "tool",
                    Content:    toolResult,
                    ToolCallID: toolCallID,
                })
                continue
            }

            // 使用 nilChannel 避免 panic
            toolResult := executeToolForSubagent(task.ctx, toolName, toolArgs, task.Role)

            actions = append(actions, ExperienceAction{
                ToolName: toolName,
                Input:    toolArgs,
                Output:   TruncateString(toolResult, 200),
            })

            history = append(history, Message{
                Role:       "tool",
                Content:    toolResult,
                ToolCallID: toolCallID,
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

    // Clean up stale tasks on each completion
    sm.maybeCleanupStaleTasks()

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

// resolveSubagentCredentials 根据 SubagentTask 的覆盖设置解析最终的 API 参数
// 优先级：1. task.CredentialOverride/ModelOverride  2. sm.DefaultSubagentCredential/Model  3. 全局变量
func (sm *SubagentManager) resolveSubagentCredentials(task *SubagentTask) (resolvedAPIType, resolvedBaseURL, resolvedAPIKey, resolvedModelID string) {
    resolvedAPIType = apiType
    resolvedBaseURL = baseURL
    resolvedAPIKey = apiKey
    resolvedModelID = modelID

    // 1. 模型覆盖
    if task.ModelOverride != "" {
        resolvedModelID = task.ModelOverride
    } else if sm.DefaultSubagentModel != "" {
        resolvedModelID = sm.DefaultSubagentModel
    }

    // 2. 凭据覆盖
    if task.CredentialOverride != "" {
        if globalCredentialPool != nil {
            if cred, err := globalCredentialPool.GetCredentialByID(task.CredentialOverride); err == nil {
                resolvedAPIKey = cred.Key
            } else {
                log.Printf("[Subagent] WARNING: credential %q lookup failed: %v, falling back to global key", task.CredentialOverride, err)
            }
        }
    } else if sm.DefaultSubagentCredential != "" {
        if globalCredentialPool != nil {
            if cred, err := globalCredentialPool.GetCredentialByID(sm.DefaultSubagentCredential); err == nil {
                resolvedAPIKey = cred.Key
            } else {
                log.Printf("[Subagent] WARNING: default credential %q lookup failed: %v, falling back to global key", sm.DefaultSubagentCredential, err)
            }
        }
    }

    return
}

// CallModelForSubagent 为子代理调用模型（非流式，支持 role 权限过滤和凭据路由）
func CallModelForSubagent(ctx context.Context, task *SubagentTask, history []Message) (Response, error) {
    // 解析凭据和模型
    resolvedAPIType, resolvedBaseURL, resolvedAPIKey, resolvedModelID := task.resolveCredentials()

    // 使用 CallModel 流式接口，收集所有 chunk 后组装响应
    chunkChan, err := CallModel(ctx, history, resolvedAPIType, resolvedBaseURL, resolvedAPIKey, resolvedModelID, temperature, maxTokens, false, false, task.Role)
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

    // Content 始終保存文本回覆（可能為空字符串，但必須是 string 類型）
    response.Content = content.String()

    // ToolCalls 存入 Response.ToolCalls（而非 Content），
    // 否則 runSubagent 無法正確偵測到工具調用請求。
    if len(toolCalls) > 0 {
        response.ToolCalls = toolCalls
    }

    if reasoning.Len() > 0 {
        response.ReasoningContent = reasoning.String()
    }

    return response, nil
}

// resolveCredentials 使用全局 SubagentManager 解析凭据
func (task *SubagentTask) resolveCredentials() (resolvedAPIType, resolvedBaseURL, resolvedAPIKey, resolvedModelID string) {
    if globalSubagentManager != nil {
        return globalSubagentManager.resolveSubagentCredentials(task)
    }
    return apiType, baseURL, apiKey, modelID
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
        "depth":           task.Depth,
        "start_time":      task.StartTime.Format(time.RFC3339),
        "runtime_seconds": time.Since(task.StartTime).Seconds(),
    }
    if task.CredentialOverride != "" {
        info["credential_override"] = task.CredentialOverride
    }
    if task.ModelOverride != "" {
        info["model_override"] = task.ModelOverride
    }

    if task.Result != "" {
        info["result"] = task.Result
    }

    return info, nil
}

// cleanupStaleTasks removes tasks that have been in a terminal state for more than 30 minutes.
func (sm *SubagentManager) cleanupStaleTasks() {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    threshold := 30 * time.Minute
    for id, task := range sm.tasks {
        task.mu.RLock()
        status := task.Status
        task.mu.RUnlock()
        if status == SubagentRunning {
            continue
        }
        // Check if the task has actually finished (done channel closed)
        select {
        case <-task.done:
                // Task is done, proceed to check age
        default:
                continue // still running (race between status and done)
        }
        if time.Since(task.StartTime) > threshold {
            delete(sm.tasks, id)
            log.Printf("[Subagent] Cleaned up stale task %s (status: %s, age: %v)", id, status, time.Since(task.StartTime).Round(time.Minute))
        }
    }
}

// maybeCleanupStaleTasks triggers cleanup every 100 spawns or on task completion.
func (sm *SubagentManager) maybeCleanupStaleTasks() {
    sm.mu.Lock()
    sm.spawnCount++
    count := sm.spawnCount
    sm.mu.Unlock()
    if count%100 == 0 {
        sm.cleanupStaleTasks()
    }
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

    // 清理 session 深度跟踪
    sm.sessionDepthMu.Lock()
    sm.sessionDepth = make(map[string]int)
    sm.sessionDepthMu.Unlock()
}

