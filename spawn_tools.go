package main

import (
    "context"
    "fmt"
    "log"
    "sync"
)

// 全局会话 ID 计数器
var sessionIDCounter int
var sessionIDMutex sync.Mutex

// getCurrentSessionID 获取当前会话 ID
func getCurrentSessionID() string {
    sessionIDMutex.Lock()
    defer sessionIDMutex.Unlock()
    sessionIDCounter++
    return fmt.Sprintf("session_%d", sessionIDCounter)
}

// getCurrentRoleForSpawn 获取当前角色（用于子代理）
func getCurrentRoleForSpawn() *Role {
    if globalRoleManager == nil || globalActorManager == nil || globalStage == nil {
        return nil
    }
    currentActor := globalStage.GetCurrentActor()
    if actor, ok := globalActorManager.GetActor(currentActor); ok {
        if role, ok := globalRoleManager.GetRole(actor.Role); ok {
            return role
        }
    }
    return nil
}

// handleSpawn 处理 spawn 工具调用
func handleSpawn(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, error) {
    task, ok := argsMap["task"].(string)
    if !ok || task == "" {
        return "Error: 缺少任务描述 (task)", nil
    }

    maxIterations := 15
    if mi, ok := argsMap["max_iterations"].(float64); ok {
        maxIterations = int(mi)
    }

    // 可选的模型覆盖
    modelOverride := ""
    if m, ok := argsMap["model"].(string); ok {
        modelOverride = m
    }

    // 可选的凭据覆盖
    credentialOverride := ""
    if c, ok := argsMap["credential_id"].(string); ok {
        credentialOverride = c
    }

    sessionID := getCurrentSessionID()

    // 注意：不在這裡覆蓋 SetResultHandler！
    // main.go 初始化時已設置 resultHandler（包含 bus 通知），
    // 在這裡覆蓋會導致 bus 通知丟失。
    if globalSubagentManager == nil {
        log.Printf("[Spawn] ERROR: globalSubagentManager is nil, this should not happen after initialization")
        globalSubagentManager = NewSubagentManager()
    }

    // 获取当前角色
    currentRole := getCurrentRoleForSpawn()

    subagentTask, err := globalSubagentManager.SpawnWithOverrides(task, sessionID, maxIterations, currentRole, 0, modelOverride, credentialOverride)
    if err != nil {
        return fmt.Sprintf("Error: 创建子代理失败: %v", err), nil
    }

    result := fmt.Sprintf("子代理已创建:\n- 任务ID: %s\n- 任务: %s\n- 最大迭代: %d\n- 深度: %d\n- 状态: running",
        subagentTask.ID, task, maxIterations, subagentTask.Depth)
    if modelOverride != "" {
        result += fmt.Sprintf("\n- 模型覆盖: %s", modelOverride)
    }
    if credentialOverride != "" {
        result += fmt.Sprintf("\n- 凭据覆盖: %s", credentialOverride)
    }
    result += "\n\n子代理将在后台执行任务。使用 spawn_check 检查进度，spawn_list 查看所有任务。"

    return result, nil
}

// handleSpawnBatch 处理 spawn_batch 工具调用（并行批量创建子代理）
func handleSpawnBatch(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, error) {
    tasksRaw, ok := argsMap["tasks"]
    if !ok {
        return "Error: 缺少任务列表 (tasks)", nil
    }

    var taskStrings []string
    switch v := tasksRaw.(type) {
    case []interface{}:
        for _, item := range v {
            if s, ok := item.(string); ok && s != "" {
                taskStrings = append(taskStrings, s)
            }
        }
    default:
        return "Error: tasks 参数必须是字符串数组", nil
    }

    if len(taskStrings) == 0 {
        return "Error: 任务列表为空", nil
    }

    maxIterations := 15
    if mi, ok := argsMap["max_iterations"].(float64); ok {
        maxIterations = int(mi)
    }

    // 可选的模型覆盖
    modelOverride := ""
    if m, ok := argsMap["model"].(string); ok {
        modelOverride = m
    }

    // 可选的凭据覆盖
    credentialOverride := ""
    if c, ok := argsMap["credential_id"].(string); ok {
        credentialOverride = c
    }

    sessionID := getCurrentSessionID()

    // 注意：不在這裡覆蓋 SetResultHandler！（同 handleSpawn 的原因）
    if globalSubagentManager == nil {
        log.Printf("[SpawnBatch] ERROR: globalSubagentManager is nil, this should not happen after initialization")
        globalSubagentManager = NewSubagentManager()
    }

    // 获取当前角色
    currentRole := getCurrentRoleForSpawn()

    spawnedTasks, err := globalSubagentManager.SpawnMultipleWithOverrides(taskStrings, sessionID, maxIterations, currentRole, modelOverride, credentialOverride)
    if err != nil {
        return fmt.Sprintf("Error: 批量创建子代理失败: %v", err), nil
    }

    result := fmt.Sprintf("已创建 %d 个并行子代理任务:\n\n", len(spawnedTasks))
    for i, t := range spawnedTasks {
        result += fmt.Sprintf("%d. 任务ID: %s\n   任务: %s\n   最大迭代: %d\n   深度: %d\n\n",
            i+1, t.ID, TruncateString(t.Task, 60), maxIterations, t.Depth)
    }
    result += "所有子代理将在后台并行执行。使用 spawn_check 检查各任务进度，spawn_list 查看所有任务。"

    return result, nil
}

// handleSpawnCheck 处理 spawn_check 工具调用
func handleSpawnCheck(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, error) {
    taskID, ok := argsMap["task_id"].(string)
    if !ok || taskID == "" {
        return "Error: 缺少任务ID (task_id)", nil
    }

    if globalSubagentManager == nil {
        return "Error: 子代理管理器未初始化", nil
    }

    info, err := globalSubagentManager.GetTaskInfo(taskID)
    if err != nil {
        return fmt.Sprintf("Error: %v", err), nil
    }

    result := fmt.Sprintf("子代理任务状态:\n- 任务ID: %s\n- 状态: %s\n- 迭代次数: %d/%d\n- 深度: %d\n- 运行时间: %.1f 秒",
        info["task_id"], info["status"], info["iterations"], info["max_iterations"], info["depth"], info["runtime_seconds"])

    if info["model_override"] != nil {
        result += fmt.Sprintf("\n- 模型覆盖: %s", info["model_override"])
    }
    if info["credential_override"] != nil {
        result += fmt.Sprintf("\n- 凭据覆盖: %s", info["credential_override"])
    }

    if info["result"] != nil && info["result"] != "" {
        result += fmt.Sprintf("\n\n结果:\n%s", info["result"])
    }

    return result, nil
}

// handleSpawnList 处理 spawn_list 工具调用
func handleSpawnList(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, error) {
    if globalSubagentManager == nil {
        return "当前没有子代理任务", nil
    }

    tasks := globalSubagentManager.List()
    if len(tasks) == 0 {
        return "当前没有子代理任务", nil
    }

    result := fmt.Sprintf("共 %d 个子代理任务:\n\n", len(tasks))
    for i, task := range tasks {
        task.mu.RLock()
        result += fmt.Sprintf("%d. [%s] %s\n   状态: %s | 迭代: %d/%d | 深度: %d\n\n",
            i+1, task.ID, TruncateString(task.Task, 50), task.Status, task.Iterations, task.MaxIterations, task.Depth)
        task.mu.RUnlock()
    }

    return result, nil
}

// handleSpawnCancel 处理 spawn_cancel 工具调用
func handleSpawnCancel(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, error) {
    taskID, ok := argsMap["task_id"].(string)
    if !ok || taskID == "" {
        return "Error: 缺少任务ID (task_id)", nil
    }

    if globalSubagentManager == nil {
        return "Error: 子代理管理器未初始化", nil
    }

    err := globalSubagentManager.Cancel(taskID)
    if err != nil {
        return fmt.Sprintf("Error: %v", err), nil
    }

    return fmt.Sprintf("子代理任务 %s 已取消", taskID), nil
}

// logSubagentResult 记录子代理结果
func logSubagentResult(task *SubagentTask) {
    task.mu.RLock()
    defer task.mu.RUnlock()

    logMessage := fmt.Sprintf("[Subagent] Task %s completed - Status: %s, Iterations: %d, Depth: %d",
        task.ID, task.Status, task.Iterations, task.Depth)

    if task.Result != "" {
        logMessage += fmt.Sprintf(", Result length: %d chars", len(task.Result))
    }

    fmt.Println(logMessage)
}
