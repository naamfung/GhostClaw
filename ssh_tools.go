package main

import (
    "context"
    "fmt"
    "time"
    "github.com/toon-format/toon-go"
)

// handleSSHConnect 处理 ssh_connect 调用
func handleSSHConnect(argsMap map[string]interface{}) (string, error) {
    username, _ := argsMap["username"].(string)
    host, _ := argsMap["host"].(string)
    password, _ := argsMap["password"].(string)
    privateKeyPath, _ := argsMap["private_key_path"].(string)
    port := 22
    if p, ok := argsMap["port"].(float64); ok {
        port = int(p)
    }

    sessionID, err := globalSSHManager.Connect(username, host, password, privateKeyPath, port)
    if err != nil {
        return "", fmt.Errorf("SSH connection failed: %w", err)
    }

    return fmt.Sprintf("SSH connection established successfully.\nSession ID: %s\n\nYou can now use this session_id with ssh_exec to run commands.", sessionID), nil
}

// handleSSHExec 处理 ssh_exec 调用
// 返回 (输出内容, 任务状态)
func handleSSHExec(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, TaskStatus) {
    sessionID, _ := argsMap["session_id"].(string)
    command, _ := argsMap["command"].(string)

    sess, ok := globalSSHManager.GetSession(sessionID)
    if !ok {
        return fmt.Sprintf("Error: SSH session '%s' not found. Use ssh_connect to create one.", sessionID), TaskStatusFailed
    }

    async, _ := argsMap["async"].(bool)
    if async {
        // 异步执行：使用现有的 TaskManager
        if globalTaskManager == nil {
            return "Error: task manager not initialized", TaskStatusFailed
        }
        wakeAfterMinutes := 5
        if waf, ok := argsMap["wake_after_minutes"].(float64); ok && waf > 0 {
            wakeAfterMinutes = int(waf)
        }
        sessionIDForTask := ch.GetSessionID()
        task, err := globalTaskManager.StartDelayedExec(
            fmt.Sprintf("ssh exec on %s: %s", sessionID, command),
            wakeAfterMinutes,
            "",
            sessionIDForTask,
        )
        if err != nil {
            return fmt.Sprintf("Error starting async SSH command: %v", err), TaskStatusFailed
        }

        result := map[string]interface{}{
            "mode":               "async",
            "task_id":            task.ID,
            "status":             "running",
            "command":            command,
            "wake_after_minutes": wakeAfterMinutes,
            "message": fmt.Sprintf("✅ SSH command is running asynchronously on session '%s'. Task ID: %s. You will be notified in %d minutes.",
                sessionID, task.ID, wakeAfterMinutes),
        }
        resultTOON, _ := toon.Marshal(result)
        return string(resultTOON), TaskStatusSuccess
    }

    // 同步执行：超时优先级为 模型参数 > 配置文件 > 程序默认值
    timeout := globalTimeoutConfig.Shell
    if timeout <= 0 {
        timeout = DefaultShellTimeout
    }
    if t, ok := argsMap["timeout_secs"].(float64); ok && t > 0 {
        timeout = int(t)
    }

    // 创建新的 SSH 会话来执行命令
    session, err := sess.Client.NewSession()
    if err != nil {
        return fmt.Sprintf("Error creating SSH session: %v", err), TaskStatusFailed
    }
    defer session.Close()

    // 设置超时
    ctxWithTimeout, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
    defer cancel()
    
    // 将 context 的超时传递给 SSH 命令
    errChan := make(chan error, 1)
    var output []byte
    go func() {
        output, err = session.CombinedOutput(command)
        errChan <- err
    }()

    select {
    case <-ctxWithTimeout.Done():
        return fmt.Sprintf("Command execution timeout after %d seconds.", timeout), TaskStatusFailed
    case err := <-errChan:
        if err != nil {
            return fmt.Sprintf("Command failed: %v\nOutput: %s", err, string(output)), TaskStatusFailed
        }
        return string(output), TaskStatusSuccess
    }
}

// handleSSHList 处理 ssh_list 调用
func handleSSHList() (string, error) {
    sessions := globalSSHManager.ListSessions()
    if len(sessions) == 0 {
        return "No active SSH sessions.", nil
    }
    result := "Active SSH sessions:\n"
    for _, s := range sessions {
        result += fmt.Sprintf("- %s\n", s)
    }
    return result, nil
}

// handleSSHClose 处理 ssh_close 调用
func handleSSHClose(argsMap map[string]interface{}) (string, error) {
    sessionID, _ := argsMap["session_id"].(string)
    if err := globalSSHManager.Close(sessionID); err != nil {
        return "", fmt.Errorf("failed to close session: %w", err)
    }
    return fmt.Sprintf("SSH session '%s' closed successfully.", sessionID), nil
}
