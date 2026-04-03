package main

import (
        "context"
        "crypto/md5"
        "fmt"
        "os"
        "path/filepath"
        "time"

        "github.com/toon-format/toon-go"
)

// 直接返回的最大字符数（超过则保存到文件）
const maxDirectOutput = 1000

// safeTruncate 安全截断 UTF-8 字符串，确保不截断多字节字符
func safeTruncate(s string, maxLen int) string {
        if len(s) <= maxLen {
                return s
        }
        runes := []rune(s)
        if len(runes) <= maxLen {
                return s
        }
        return string(runes[:maxLen]) + "..."
}

// tailContent 返回字符串末尾最多 maxChars 个字符（安全处理 UTF-8）
func tailContent(s string, maxChars int) string {
        if len(s) <= maxChars {
                return s
        }
        runes := []rune(s)
        if len(runes) <= maxChars {
                return s
        }
        return string(runes[len(runes)-maxChars:])
}

// saveOutputToFile 将过长内容保存到文件，返回文件路径（若内容不长则返回空字符串）
func saveOutputToFile(content, prefix, command string) (string, error) {
        if len(content) <= maxDirectOutput {
                return "", nil
        }

        outputDir := filepath.Join(globalExecDir, "output")
        if err := os.MkdirAll(outputDir, 0755); err != nil {
                return "", err
        }

        timestamp := time.Now().UnixNano()
        hash := md5.Sum([]byte(command))
        safeCmd := fmt.Sprintf("%x", hash)[:8]
        filename := fmt.Sprintf("%s_%d_%s.txt", prefix, timestamp, safeCmd)
        filePath := filepath.Join(outputDir, filename)

        if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
                return "", err
        }
        return filePath, nil
}

// ==================== 原有工具处理函数 ====================

// handleSmartShell 智能判断命令执行模式
func handleSmartShell(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        command, ok := argsMap["command"].(string)
        if !ok || command == "" {
                return "Error: missing or invalid 'command' parameter", false
        }

        forceAsync := false
        if a, ok := argsMap["async"].(bool); ok {
                forceAsync = a
        }
        forceSync := false
        if s, ok := argsMap["sync"].(bool); ok {
                forceSync = s
        }

        if forceAsync && forceSync {
                return "Error: cannot specify both async=true and sync=true", false
        }

        wakeAfterMinutes := globalToolsConfig.SmartShell.DefaultWakeMins
        if wakeAfterMinutes <= 0 {
                wakeAfterMinutes = 5
        }
        if waf, ok := argsMap["wake_after_minutes"].(float64); ok && waf > 0 {
                wakeAfterMinutes = int(waf)
        }

        var execMode string
        var suggestion CommandSuggestion

        if forceAsync {
                execMode = "async_forced"
        } else if forceSync {
                execMode = "sync_forced"
        } else {
                suggestion = DetectCommandType(command)
                execMode = suggestion.Type
        }

        switch execMode {
        case "quick", "sync_forced":
                return handleSmartShellSync(ctx, command, ch, false)
        case "interactive":
                return handleSmartShellInteractive(command, suggestion, wakeAfterMinutes, ch)
        case "long_running", "async_forced":
                return handleSmartShellAsync(command, wakeAfterMinutes, ch)
        case "unknown":
                return handleSmartShellSync(ctx, command, ch, true)
        default:
                return handleSmartShellSync(ctx, command, ch, true)
        }
}

// handleSmartShellSync 同步执行命令
func handleSmartShellSync(ctx context.Context, command string, ch Channel, isUnknown bool) (string, bool) {
        timeout := globalToolsConfig.SmartShell.SyncTimeout
        if timeout <= 0 {
                timeout = 60
        }
        if isUnknown {
                unknownTimeout := globalToolsConfig.SmartShell.UnknownTimeout
                if unknownTimeout <= 0 {
                        unknownTimeout = 120
                }
                timeout = unknownTimeout
        }

        ctxWithTimeout, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
        defer cancel()

        result := runShellWithTimeout(ctxWithTimeout, command, false, false)

        if result.ConfirmRequired {
                response := map[string]interface{}{
                        "mode":            "confirm_required",
                        "confirm_message": result.ConfirmMessage,
                        "suggestions":     result.Suggestions,
                        "message":         "⚠️ 此命令可能需要交互确认。请确认后重新执行，或使用 smart_shell(command, async=true) 异步执行。",
                }
                resultTOON, _ := toon.Marshal(response)
                return string(resultTOON), false
        }

        if ctxWithTimeout.Err() == context.DeadlineExceeded {
                var message string
                if isUnknown {
                        message = fmt.Sprintf("⏱️ 命令执行超时（%d秒）。\n\n"+
                                "此命令不在已知命令列表中，系统尝试同步执行但超时。\n\n"+
                                "建议：\n"+
                                "1. 如果这是一个长时间运行的命令，请使用异步模式：\n"+
                                "   smart_shell(command, async=true)\n\n"+
                                "2. 如果此命令应该快速完成但卡住了，请检查命令是否正确。", timeout)
                } else {
                        message = fmt.Sprintf("⏱️ 命令执行超时（%d秒）。建议使用异步模式：smart_shell(command, async=true)", timeout)
                }
                response := map[string]interface{}{
                        "mode":            "timeout",
                        "command":         command,
                        "timeout_seconds": timeout,
                        "is_unknown_cmd":  isUnknown,
                        "message":         message,
                }
                resultTOON, _ := toon.Marshal(response)
                return string(resultTOON), false
        }

        // 处理 stdout
        stdout := result.Stdout
        stdoutFile, err := saveOutputToFile(stdout, "stdout", command)
        if err == nil && stdoutFile != "" {
                // 取最后 500 个字符作为预览（而不是最后 10 行）
                tail := tailContent(stdout, 500)
                stdout = fmt.Sprintf(
                        "[stdout 过长，完整内容已保存至: %s]\n\n--- 最后 500 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符",
                        stdoutFile, tail, len(result.Stdout),
                )
        } else if len(stdout) > maxDirectOutput {
                tail := tailContent(stdout, 500)
                stdout = fmt.Sprintf(
                        "[stdout 过长已截断（无法保存文件）]\n\n--- 最后 500 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符",
                        tail, len(result.Stdout),
                )
        }

        // 处理 stderr
        stderr := result.Stderr
        stderrFile, err := saveOutputToFile(stderr, "stderr", command)
        if err == nil && stderrFile != "" {
                tail := tailContent(stderr, 500)
                stderr = fmt.Sprintf(
                        "[stderr 过长，完整内容已保存至: %s]\n\n--- 最后 500 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符",
                        stderrFile, tail, len(result.Stderr),
                )
        } else if len(stderr) > maxDirectOutput {
                tail := tailContent(stderr, 500)
                stderr = fmt.Sprintf(
                        "[stderr 过长已截断（无法保存文件）]\n\n--- 最后 500 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符",
                        tail, len(result.Stderr),
                )
        }

        response := map[string]interface{}{
                "mode":       "sync",
                "command":    command,
                "stdout":     stdout,
                "stderr":     stderr,
                "exit_code":  result.ExitCode,
        }
        if stdoutFile != "" {
                response["stdout_file"] = stdoutFile
        }
        if stderrFile != "" {
                response["stderr_file"] = stderrFile
        }
        if result.Err != nil {
                response["error"] = result.Err.Error()
        }

        resultTOON, _ := toon.Marshal(response)
        return string(resultTOON), false
}

// handleSmartShellAsync 异步执行命令
func handleSmartShellAsync(command string, wakeAfterMinutes int, ch Channel) (string, bool) {
        if globalTaskManager == nil {
                return "Error: task manager not initialized", false
        }

        sessionID := ch.GetSessionID()

        task, err := globalTaskManager.StartDelayedExec(command, wakeAfterMinutes, "", sessionID)
        if err != nil {
                return fmt.Sprintf("Error: failed to start async execution: %v", err), false
        }

        result := map[string]interface{}{
                "mode":               "async",
                "task_id":            task.ID,
                "pid":                task.PID,
                "status":             "running",
                "command":            command,
                "wake_after_minutes": wakeAfterMinutes,
                "message": fmt.Sprintf("✅ 任务已异步启动（PID: %d），将在 %d 分钟后唤醒你。\n\n"+
                        "⏳ **重要提示**：你不需要轮询任务状态。\n"+
                        "系统会在 %d 分钟后主动通知你任务的执行结果。\n\n"+
                        "你可以继续处理其他工作。", task.PID, wakeAfterMinutes, wakeAfterMinutes),
        }

        resultTOON, _ := toon.Marshal(result)
        return string(resultTOON), false
}

// handleSmartShellInteractive 处理交互式命令
func handleSmartShellInteractive(command string, suggestion CommandSuggestion, wakeAfterMinutes int, ch Channel) (string, bool) {
        if globalTaskManager == nil {
                return "Error: task manager not initialized", false
        }

        sessionID := ch.GetSessionID()

        task, err := globalTaskManager.StartDelayedExec(command, wakeAfterMinutes, "", sessionID)
        if err != nil {
                return fmt.Sprintf("Error: failed to start async execution: %v", err), false
        }

        msg := "⚠️ 检测到交互式命令\n\n"
        msg += fmt.Sprintf("**命令**: `%s`\n\n", command)
        msg += fmt.Sprintf("**问题**: %s\n\n", suggestion.Message)

        if suggestion.Suggestion != "" {
                msg += fmt.Sprintf("**建议**: %s\n\n", suggestion.Suggestion)
        }

        if suggestion.NonInteractiveEq != "" {
                msg += fmt.Sprintf("**非交互式等价命令**: `%s`\n\n", suggestion.NonInteractiveEq)
        }

        msg += "---\n\n"
        msg += fmt.Sprintf("✅ 命令已异步启动（PID: %d），将在 %d 分钟后唤醒。\n", task.PID, wakeAfterMinutes)
        msg += "如果命令卡在交互状态，你可以使用 `task_terminate` 终止它。\n\n"
        msg += "💡 建议：下次使用非交互式等价命令以避免此问题。"

        result := map[string]interface{}{
                "mode":               "interactive",
                "task_id":            task.ID,
                "pid":                task.PID,
                "status":             "running",
                "command":            command,
                "wake_after_minutes": wakeAfterMinutes,
                "suggestion":         suggestion.Suggestion,
                "non_interactive_eq": suggestion.NonInteractiveEq,
                "message":            msg,
        }

        resultTOON, _ := toon.Marshal(result)
        return string(resultTOON), false
}

// ==================== 原有后台任务工具函数 ====================

func handleDelayedExec(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        if globalTaskManager == nil {
                return "Error: task manager not initialized", false
        }

        command, ok := argsMap["command"].(string)
        if !ok || command == "" {
                return "Error: missing or invalid 'command' parameter", false
        }

        wakeAfterMinutes := 5
        if waf, ok := argsMap["wake_after_minutes"].(float64); ok {
                wakeAfterMinutes = int(waf)
        }

        description := ""
        if desc, ok := argsMap["description"].(string); ok {
                description = desc
        }

        sessionID := ch.GetSessionID()

        task, err := globalTaskManager.StartDelayedExec(command, wakeAfterMinutes, description, sessionID)
        if err != nil {
                return fmt.Sprintf("Error: failed to start delayed execution: %v", err), false
        }

        result := map[string]interface{}{
                "task_id":            task.ID,
                "pid":                task.PID,
                "status":             "running",
                "command":            command,
                "wake_after_minutes": wakeAfterMinutes,
                "message": fmt.Sprintf("✅ 任务已启动（PID: %d），将在 %d 分钟后唤醒你。\n\n"+
                        "⏳ **重要提示**：你现在不需要调用 check 工具轮询任务状态。\n"+
                        "系统会在 %d 分钟后主动通知你任务的执行结果。\n\n"+
                        "你可以继续处理其他工作。如需提前检查或终止，可使用：\n"+
                        "• shell_delayed_check - 检查任务状态（不建议频繁调用）\n"+
                        "• shell_delayed_wait - 延长等待时间\n"+
                        "• shell_delayed_terminate - 终止任务", task.PID, wakeAfterMinutes, wakeAfterMinutes),
        }

        resultTOON, _ := toon.Marshal(result)
        return string(resultTOON), false
}

func handleTaskCheck(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        if globalTaskManager == nil {
                return "Error: task manager not initialized", false
        }

        taskID, ok := argsMap["task_id"].(string)
        if !ok || taskID == "" {
                return "Error: missing or invalid 'task_id' parameter", false
        }

        info, err := globalTaskManager.GetTaskInfo(taskID)
        if err != nil {
                return fmt.Sprintf("Error: %v", err), false
        }

        status := info["status"].(string)
        var message string
        switch status {
        case "running":
                runtimeMinutes := info["runtime_minutes"].(float64)
                message = fmt.Sprintf("\n\n⏳ 任务仍在运行中（已运行 %.1f 分钟）。\n\n"+
                        "📋 可选操作：\n"+
                        "• 如需继续等待：调用 shell_delayed_wait 工具设置等待时间，**然后停止检查，等待系统通知**\n"+
                        "• 如需终止任务：使用 shell_delayed_terminate 工具\n\n"+
                        "⚠️ **注意**：调用 wait 工具后，不要继续调用 check 工具轮询，系统会在唤醒时间主动通知你。", runtimeMinutes)
        case "completed":
                message = "\n\n✅ 任务已完成！退出码为 0。"
        case "failed":
                message = "\n\n❌ 任务执行失败。请检查 stderr 了解错误详情。"
        case "terminated":
                message = "\n\n⏹️ 任务已被终止。"
        }

        info["message"] = message

        resultTOON, _ := toon.Marshal(info)
        return string(resultTOON), false
}

func handleTaskTerminate(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        if globalTaskManager == nil {
                return "Error: task manager not initialized", false
        }

        taskID, ok := argsMap["task_id"].(string)
        if !ok || taskID == "" {
                return "Error: missing or invalid 'task_id' parameter", false
        }

        force := false
        if f, ok := argsMap["force"].(bool); ok {
                force = f
        }

        err := globalTaskManager.TerminateTask(taskID, force)
        if err != nil {
                return fmt.Sprintf("Error: %v", err), false
        }

        forceStr := "优雅终止 (SIGTERM)"
        if force {
                forceStr = "强制终止 (SIGKILL)"
        }

        result := map[string]interface{}{
                "task_id":   taskID,
                "status":    "terminated",
                "method":    forceStr,
                "timestamp": time.Now().Format(time.RFC3339),
                "message":   fmt.Sprintf("任务 %s 已%s。", taskID, forceStr),
        }

        resultTOON, _ := toon.Marshal(result)
        return string(resultTOON), false
}

func handleTaskList(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        if globalTaskManager == nil {
                return "Error: task manager not initialized", false
        }

        tasks := globalTaskManager.ListTasks()
        if len(tasks) == 0 {
                return "当前没有后台任务。", false
        }

        type taskSummary struct {
                ID          string    `toon:"task_id"`
                Command     string    `toon:"command"`
                Status      string    `toon:"status"`
                PID         int       `toon:"pid"`
                StartTime   time.Time `toon:"start_time"`
                RuntimeMin  float64   `toon:"runtime_minutes"`
                Description string    `toon:"description,omitempty"`
        }

        summaries := make([]taskSummary, 0, len(tasks))
        for _, task := range tasks {
                task.mu.RLock()
                summary := taskSummary{
                        ID:         task.ID,
                        Command:    task.Command,
                        Status:     string(task.Status),
                        PID:        task.PID,
                        StartTime:  task.StartTime,
                        RuntimeMin: time.Since(task.StartTime).Minutes(),
                }
                if task.Description != "" {
                        summary.Description = task.Description
                }
                task.mu.RUnlock()
                summaries = append(summaries, summary)
        }

        resultTOON, _ := toon.Marshal(summaries)
        return string(resultTOON), false
}

func handleTaskWait(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        if globalTaskManager == nil {
                return "Error: task manager not initialized", false
        }

        taskID, ok := argsMap["task_id"].(string)
        if !ok || taskID == "" {
                return "Error: missing or invalid 'task_id' parameter", false
        }

        waitMinutes := 5
        if wm, ok := argsMap["wait_minutes"].(float64); ok {
                waitMinutes = int(wm)
        }

        err := globalTaskManager.ExtendWakeTime(taskID, waitMinutes)
        if err != nil {
                return fmt.Sprintf("Error: %v", err), false
        }

        nextWakeTime := time.Now().Add(time.Duration(waitMinutes) * time.Minute)

        result := map[string]interface{}{
                "task_id":         taskID,
                "status":          "waiting",
                "wait_minutes":    waitMinutes,
                "next_wake_after": nextWakeTime.Format(time.RFC3339),
                "message": fmt.Sprintf("✅ 已设置 %d 分钟后唤醒（预计时间: %s）。\n\n"+
                        "⏳ **重要提示**：你现在不需要再调用任何任务相关工具（check/wait）。\n"+
                        "系统会在任务完成或到达唤醒时间时主动通知你。\n\n"+
                        "你可以继续处理其他工作，或向用户报告当前状态。", waitMinutes, nextWakeTime.Format("15:04:05")),
        }

        resultTOON, _ := toon.Marshal(result)
        return string(resultTOON), false
}

func handleTaskRemove(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        if globalTaskManager == nil {
                return "Error: task manager not initialized", false
        }

        taskID, ok := argsMap["task_id"].(string)
        if !ok || taskID == "" {
                return "Error: missing or invalid 'task_id' parameter", false
        }

        err := globalTaskManager.RemoveTask(taskID)
        if err != nil {
                return fmt.Sprintf("Error: %v", err), false
        }

        return fmt.Sprintf("任务 %s 已从列表中移除。", taskID), false
}
