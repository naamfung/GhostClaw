package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/toon-format/toon-go"
)

// getMaxDirectOutput 返回 stdout/stderr 直接輸出的最大字符數。
// 使用 DynamicToolThreshold() 動態計算，與 ReadFileLines 檔案大小檢查算法完全一致。
func getMaxDirectOutput() int {
	return DynamicToolThreshold()
}

// saveOutputToFile 将过长内容保存到文件，返回文件路径（若内容不长则返回空字符串）
func saveOutputToFile(content, prefix, command string) (string, error) {
	if len(content) <= getMaxDirectOutput() {
		return "", nil
	}

	outputDir := filepath.Join(globalDataDir, "output")
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

	// mode 参数统一控制执行模式（替代旧的 async/sync 两个互斥 boolean）
	// "sync": 强制同步执行；"async": 强制异步执行；默认 "" = 自动检测
	forceAsync := false
	forceSync := false
	if mode, ok := argsMap["mode"].(string); ok {
		switch mode {
		case "async":
			forceAsync = true
		case "sync":
			forceSync = true
		case "auto":
			// 默认行为，不强制覆盖
		default:
			return fmt.Sprintf("Error: invalid mode '%s'. Valid values: 'sync', 'async', 'auto'", mode), false
		}
	}

	wakeAfterMinutes := globalToolsConfig.SmartShell.DefaultWakeMins
	if wakeAfterMinutes <= 0 {
		wakeAfterMinutes = 5
	}
	if waf, ok := argsMap["WakeAfterMinutes"].(float64); ok && waf > 0 {
		wakeAfterMinutes = int(waf)
	}

	// 解析 timeout_secs：異步任務的最大執行時間
	timeoutSecs := 0
	if ts, ok := argsMap["TimeoutSecs"]; ok {
		switch v := ts.(type) {
		case float64:
			timeoutSecs = int(v)
		case string:
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				timeoutSecs = parsed
			}
		}
	}

	// 解析 force：繞過阻塞命令檢測
	force := false
	if forceVal, ok := argsMap["force"].(bool); ok {
		force = forceVal
	}

	// 解析 description：異步任務描述
	description := ""
	if desc, ok := argsMap["description"].(string); ok {
		description = desc
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
		return handleSmartShellSync(ctx, command, ch, false, timeoutSecs, force)
	case "interactive":
		return handleSmartShellInteractive(command, suggestion, wakeAfterMinutes, ch, description)
	case "long_running", "async_forced":
		return handleSmartShellAsync(command, wakeAfterMinutes, ch, timeoutSecs, description)
	case "unknown":
		return handleSmartShellSync(ctx, command, ch, true, timeoutSecs, force)
	default:
		return handleSmartShellSync(ctx, command, ch, true, timeoutSecs, force)
	}
}

// handleSmartShellSync 同步执行命令
// timeoutSecs: 用戶通過 SmartShell 參數傳入的超時秒數，0 表示使用默認值
// force: 繞過阻塞命令檢測，直接執行
func handleSmartShellSync(ctx context.Context, command string, ch Channel, isUnknown bool, timeoutSecs int, force bool) (string, bool) {
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
	// 用戶顯式傳入的 timeout_secs 覆蓋默認值
	if timeoutSecs > 0 {
		timeout = timeoutSecs
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	result := runShellWithTimeout(ctxWithTimeout, command, force, false)

	if result.ConfirmRequired {
		response := map[string]interface{}{
			"Mode":           "confirm_required",
			"ConfirmMessage": result.ConfirmMessage,
			"Suggestions":    result.Suggestions,
			"Message":        "⚠️ 此命令可能需要交互确认。请确认后重新执行，或使用 SmartShell(command, mode=\"async\") 异步执行。",
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
				"   SmartShell(command, mode=\"async\")\n\n"+
				"2. 如果此命令应该快速完成但卡住了，请检查命令是否正确。", timeout)
		} else {
			message = fmt.Sprintf("⏱️ 命令执行超时（%d秒）。建议使用异步模式：SmartShell(command, mode=\"async\")", timeout)
		}
		response := map[string]interface{}{
			"Mode":           "timeout",
			"Command":        command,
			"TimeoutSeconds": timeout,
			"IsUnknownCmd":   isUnknown,
			"Message":        message,
		}
		resultTOON, _ := toon.Marshal(response)
		return string(resultTOON), false
	}

	// 处理 stdout
	stdout := result.Stdout
	stdoutFile, err := saveOutputToFile(stdout, "stdout", command)
	if err == nil && stdoutFile != "" {
		maxDirect := getMaxDirectOutput()
		tail := TailRunes(stdout, maxDirect)
		stdout = fmt.Sprintf(
			"[stdout 过长，完整内容已保存至: %s]\n\n--- 最后 %d 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符",
			stdoutFile, maxDirect, tail, len(result.Stdout),
		)
	} else if len(stdout) > getMaxDirectOutput() {
		maxDirect := getMaxDirectOutput()
		tail := TailRunes(stdout, maxDirect)
		stdout = fmt.Sprintf(
			"[stdout 过长已截断（无法保存文件）]\n\n--- 最后 %d 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符",
			maxDirect, tail, len(result.Stdout),
		)
	}

	// 处理 stderr
	stderr := result.Stderr
	stderrFile, err := saveOutputToFile(stderr, "stderr", command)
	if err == nil && stderrFile != "" {
		maxDirect := getMaxDirectOutput()
		tail := TailRunes(stderr, maxDirect)
		stderr = fmt.Sprintf(
			"[stderr 过长，完整内容已保存至: %s]\n\n--- 最后 %d 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符",
			stderrFile, maxDirect, tail, len(result.Stderr),
		)
	} else if len(stderr) > getMaxDirectOutput() {
		maxDirect := getMaxDirectOutput()
		tail := TailRunes(stderr, maxDirect)
		stderr = fmt.Sprintf(
			"[stderr 过长已截断（无法保存文件）]\n\n--- 最后 %d 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符",
			maxDirect, tail, len(result.Stderr),
		)
	}

	// OpenCLI 未知命令 help fallback：只有當命令本身係 opencli 開頭先觸發
	if strings.HasPrefix(strings.TrimSpace(command), "opencli") &&
		strings.Contains(strings.ToLower(result.Stderr), "error: unknown command") {
		helpResult := runShellWithTimeout(context.Background(), "opencli help", false, false)
		if helpResult.Err == nil && helpResult.Stdout != "" {
			stderr += "\n\n=== OpenCLI 帮助信息 ===\n" + helpResult.Stdout
		}
	}

	response := map[string]interface{}{
		"Mode":     "sync",
		"Command":  command,
		"Stdout":   stdout,
		"Stderr":   stderr,
		"ExitCode": result.ExitCode,
	}
	if stdoutFile != "" {
		response["StdoutFile"] = stdoutFile
	}
	if stderrFile != "" {
		response["StderrFile"] = stderrFile
	}
	if result.Err != nil {
		response["Error"] = result.Err.Error()
	}

	resultTOON, _ := toon.Marshal(response)
	return string(resultTOON), true
}

// handleSmartShellAsync 异步执行命令
func handleSmartShellAsync(command string, wakeAfterMinutes int, ch Channel, timeoutSecs int, description string) (string, bool) {
	if globalTaskManager == nil {
		return "Error: task manager not initialized", false
	}

	sessionID := ch.GetSessionID()

	task, err := globalTaskManager.StartDelayedExec(command, wakeAfterMinutes, description, sessionID, timeoutSecs)
	if err != nil {
		return fmt.Sprintf("Error: failed to start async execution: %v", err), false
	}

	msg := fmt.Sprintf("✅ 任務已異步啟動（PID: %d），將在 %d 分鐘後喚醒你。", task.PID, wakeAfterMinutes)
	if timeoutSecs > 0 {
		msg += fmt.Sprintf("\n⏱️ 最大執行時間: %d 秒（超時將自動終止並喚醒）。", timeoutSecs)
	}
	msg += "\n\n⏳ **重要提示**：你不需要輪詢任務狀態。\n系統會在任務完成或超時後主動通知你。\n\n你可以繼續處理其他工作。"

	result := map[string]interface{}{
		"mode":               "async",
		"TaskId":             task.ID,
		"pid":                task.PID,
		"status":             "running",
		"command":            command,
		"wake_after_minutes": wakeAfterMinutes,
		"TimeoutSecs":        timeoutSecs,
		"message":            msg,
	}

	resultTOON, _ := toon.Marshal(result)
	return string(resultTOON), true
}

// handleSmartShellInteractive 处理交互式命令
func handleSmartShellInteractive(command string, suggestion CommandSuggestion, wakeAfterMinutes int, ch Channel, description string) (string, bool) {
	if globalTaskManager == nil {
		return "Error: task manager not initialized", false
	}

	sessionID := ch.GetSessionID()

	task, err := globalTaskManager.StartDelayedExec(command, wakeAfterMinutes, description, sessionID, 0)
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
		"Mode":             "interactive",
		"TaskId":           task.ID,
		"PID":              task.PID,
		"Status":           "running",
		"Command":          command,
		"WakeAfterMinutes": wakeAfterMinutes,
		"Suggestion":       suggestion.Suggestion,
		"NonInteractiveEq": suggestion.NonInteractiveEq,
		"Message":          msg,
	}

	resultTOON, _ := toon.Marshal(result)
	return string(resultTOON), true
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
	if waf, ok := argsMap["WakeAfterMinutes"].(float64); ok {
		wakeAfterMinutes = int(waf)
	}

	description := ""
	if desc, ok := argsMap["description"].(string); ok {
		description = desc
	}

	sessionID := ch.GetSessionID()

	task, err := globalTaskManager.StartDelayedExec(command, wakeAfterMinutes, description, sessionID, 0)
	if err != nil {
		return fmt.Sprintf("Error: failed to start delayed execution: %v", err), false
	}

	result := map[string]interface{}{
		"TaskId":           task.ID,
		"PID":              task.PID,
		"Status":           "running",
		"Command":          command,
		"WakeAfterMinutes": wakeAfterMinutes,
		"Message": fmt.Sprintf("✅ 任务已启动（PID: %d），将在 %d 分钟后唤醒你。\n\n"+
			"⏳ **重要提示**：你现在不需要调用 check 工具轮询任务状态。\n"+
			"系统会在 %d 分钟后主动通知你任务的执行结果。\n\n"+
			"你可以继续处理其他工作。如需提前检查或终止，可使用：\n"+
			"• TaskCheck - 检查任务状态（不建议频繁调用）\n"+
			"• TaskWait - 延长等待时间\n"+
			"• TaskTerminate - 终止任务", task.PID, wakeAfterMinutes, wakeAfterMinutes),
	}

	resultTOON, _ := toon.Marshal(result)
	return string(resultTOON), true
}

func handleTaskCheck(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
	if globalTaskManager == nil {
		return "Error: task manager not initialized", false
	}

	taskID, ok := argsMap["TaskId"].(string)
	if !ok || taskID == "" {
		return "Error: missing or invalid 'TaskId' parameter", false
	}

	info, err := globalTaskManager.GetTaskInfo(taskID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), false
	}

	status := info["Status"].(string)
	var message string
	switch status {
	case "running":
		runtimeMinutes := info["RuntimeMinutes"].(float64)
		message = fmt.Sprintf("\n\n⏳ 任务仍在运行中（已运行 %.1f 分钟）。\n\n"+
			"📋 可选操作：\n"+
			"• 如需继续等待：调用 TaskWait 工具设置等待时间，**然后停止检查，等待系统通知**\n"+
			"• 如需终止任务：使用 TaskTerminate 工具\n\n"+
			"⚠️ **注意**：调用 wait 工具后，不要继续调用 check 工具轮询，系统会在唤醒时间主动通知你。", runtimeMinutes)
	case "Completed":
		message = "\n\n✅ 任务已完成！退出码为 0。"
	case "failed":
		message = "\n\n❌ 任务执行失败。请检查 stderr 了解错误详情。"
	case "terminated":
		message = "\n\n⏹️ 任务已被终止。"
	}

	info["message"] = message

	resultTOON, _ := toon.Marshal(info)
	return string(resultTOON), true
}

func handleTaskTerminate(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
	if globalTaskManager == nil {
		return "Error: task manager not initialized", false
	}

	taskID, ok := argsMap["TaskId"].(string)
	if !ok || taskID == "" {
		return "Error: missing or invalid 'TaskId' parameter", false
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
		"TaskId":    taskID,
		"status":    "terminated",
		"method":    forceStr,
		"timestamp": time.Now().Format(time.RFC3339),
		"message":   fmt.Sprintf("任务 %s 已%s。", taskID, forceStr),
	}

	resultTOON, _ := toon.Marshal(result)
	return string(resultTOON), true
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
		ID          string    `toon:"TaskId"`
		Command     string    `toon:"command"`
		Status      string    `toon:"status"`
		PID         int       `toon:"pid"`
		StartTime   time.Time `toon:"StartTime"`
		RuntimeMin  float64   `toon:"RuntimeMinutes"`
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
	return string(resultTOON), true
}

func handleTaskWait(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
	if globalTaskManager == nil {
		return "Error: task manager not initialized", false
	}

	taskID, ok := argsMap["TaskId"].(string)
	if !ok || taskID == "" {
		return "Error: missing or invalid 'TaskId' parameter", false
	}

	waitMinutes := 5
	if wm, ok := argsMap["WaitMinutes"].(float64); ok {
		waitMinutes = int(wm)
	}

	err := globalTaskManager.ExtendWakeTime(taskID, waitMinutes)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), false
	}

	nextWakeTime := time.Now().Add(time.Duration(waitMinutes) * time.Minute)

	result := map[string]interface{}{
		"TaskId":        taskID,
		"Status":        "Waiting",
		"WaitMinutes":   waitMinutes,
		"NextWakeAfter": nextWakeTime.Format(time.RFC3339),
		"Message": fmt.Sprintf("✅ 已设置 %d 分钟后唤醒（预计时间: %s）。\n\n"+
			"⏳ **重要提示**：你现在不需要再调用任何任务相关工具（check/wait）。\n"+
			"系统会在任务完成或到达唤醒时间时主动通知你。\n\n"+
			"你可以继续处理其他工作，或向用户报告当前状态。", waitMinutes, nextWakeTime.Format("15:04:05")),
	}

	resultTOON, _ := toon.Marshal(result)
	return string(resultTOON), true
}

func handleTaskRemove(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
	if globalTaskManager == nil {
		return "Error: task manager not initialized", false
	}

	taskID, ok := argsMap["TaskId"].(string)
	if !ok || taskID == "" {
		return "Error: missing or invalid 'TaskId' parameter", false
	}

	err := globalTaskManager.RemoveTask(taskID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), false
	}

	return fmt.Sprintf("任务 %s 已从列表中移除。", taskID), true
}
