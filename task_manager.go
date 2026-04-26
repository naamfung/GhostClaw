package main

import (
        "bytes"
        "context"
        "crypto/md5"
        "encoding/json"
        "fmt"
        "log"
        "os"
        "os/exec"
        "path/filepath"
        "strings"
        "sync"
        "text/template"
        "time"

        "github.com/google/uuid"
        "github.com/toon-format/toon-go"
)

// BackgroundTaskStatus 后台任务状态类型
type BackgroundTaskStatus string

const (
        BgTaskRunning    BackgroundTaskStatus = "running"
        BgTaskCompleted  BackgroundTaskStatus = "completed"
        BgTaskFailed     BackgroundTaskStatus = "failed"
        BgTaskTerminated BackgroundTaskStatus = "terminated"
)

// BackgroundTask 后台任务
type BackgroundTask struct {
        ID          string               `json:"id"`
        Command     string               `json:"command"`
        Description string               `json:"description,omitempty"`
        PID         int                  `json:"pid"`
        StartTime   time.Time            `json:"start_time"`
        Status      BackgroundTaskStatus `json:"status"`
        ExitCode    int                  `json:"exit_code,omitempty"`
        Stdout      *safeBuffer          `json:"-"`
        Stderr      *safeBuffer          `json:"-"`
        WakeAfter   time.Time            `json:"wake_after"`
        SessionID   string               `json:"session_id,omitempty"`
        TimeoutSecs int                  `json:"timeout_secs,omitempty"` // 0 表示無超時限制

        mu        sync.RWMutex
        process   *os.Process
        done      chan struct{}
        wakeSent  bool
        cancelCtx context.CancelFunc // per-task timeout cancel
        cmdCtx    context.Context    // per-task command context（用於檢測超時）
}

// safeBuffer 线程安全的缓冲区
type safeBuffer struct {
        mu  sync.RWMutex
        buf bytes.Buffer
}

func (sb *safeBuffer) Write(p []byte) (int, error) {
        sb.mu.Lock()
        defer sb.mu.Unlock()
        return sb.buf.Write(p)
}

func (sb *safeBuffer) String() string {
        sb.mu.RLock()
        defer sb.mu.RUnlock()
        return sb.buf.String()
}

func (sb *safeBuffer) Len() int {
        sb.mu.RLock()
        defer sb.mu.RUnlock()
        return sb.buf.Len()
}

// TaskManager 后台任务管理器
type TaskManager struct {
        tasks       map[string]*BackgroundTask
        mu          sync.RWMutex
        wakeChan    chan string
        wakeHandler WakeHandlerFunc
        ctx         context.Context
        cancel      context.CancelFunc
}

// WakeHandlerFunc 唤醒处理函数类型
type WakeHandlerFunc func(task *BackgroundTask)

// NewTaskManager 创建任务管理器
func NewTaskManager() *TaskManager {
        ctx, cancel := context.WithCancel(context.Background())
        tm := &TaskManager{
                tasks:    make(map[string]*BackgroundTask),
                wakeChan: make(chan string, 100),
                ctx:      ctx,
                cancel:   cancel,
        }
        go tm.wakeScheduler()
        return tm
}

// generateTaskID 生成任务ID
func generateTaskID() string {
        id := uuid.New()
        return "task_" + id.String()[:8]
}

// StartDelayedExec 启动延迟执行任务
// timeoutSecs: 每個異步任務的最大執行時間（秒），0 表示無超時
func (tm *TaskManager) StartDelayedExec(command string, wakeAfterMinutes int, description string, sessionID string, timeoutSecs int) (*BackgroundTask, error) {
        if wakeAfterMinutes < 1 {
                wakeAfterMinutes = 1
        }
        if wakeAfterMinutes > 1440 {
                wakeAfterMinutes = 1440
        }

        taskID := generateTaskID()

        task := &BackgroundTask{
                ID:          taskID,
                Command:     command,
                Description: description,
                StartTime:   time.Now(),
                Status:      BgTaskRunning,
                Stdout:      &safeBuffer{},
                Stderr:      &safeBuffer{},
                WakeAfter:   time.Now().Add(time.Duration(wakeAfterMinutes) * time.Minute),
                SessionID:   sessionID,
                TimeoutSecs: timeoutSecs,
                done:        make(chan struct{}),
        }

        // 爲異步任務構建獨立的 timeout context，而非使用 TaskManager 的全局 context
        var cmdCtx context.Context
        if timeoutSecs > 0 {
                cmdCtx, task.cancelCtx = context.WithTimeout(context.Background(), time.Duration(timeoutSecs)*time.Second)
        } else {
                cmdCtx, task.cancelCtx = context.WithCancel(tm.ctx)
        }
        task.cmdCtx = cmdCtx
        cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
        cmd.Stdout = task.Stdout
        cmd.Stderr = task.Stderr
        cmd.SysProcAttr = getSysProcAttr()

        if err := cmd.Start(); err != nil {
                return nil, fmt.Errorf("failed to start command: %w", err)
        }

        task.process = cmd.Process
        task.PID = cmd.Process.Pid

        tm.mu.Lock()
        tm.tasks[taskID] = task
        tm.mu.Unlock()

        go tm.monitorTask(task, cmd)

        log.Printf("[TaskManager] Task %s started, PID: %d, wake after %d minutes, timeout: %ds", taskID, task.PID, wakeAfterMinutes, timeoutSecs)

        return task, nil
}

// monitorTask 监控任务执行
func (tm *TaskManager) monitorTask(task *BackgroundTask, cmd *exec.Cmd) {
        defer close(task.done)

        err := cmd.Wait()

        task.mu.Lock()
        defer task.mu.Unlock()

        // 檢測是否因 timeout 被殺
        if cmd.ProcessState != nil && cmd.ProcessState.String() == "signal: killed" {
                if task.cancelCtx != nil && task.TimeoutSecs > 0 && task.cmdCtx != nil {
                        // 檢查 context 是否因超時被取消
                        select {
                        case <-task.cmdCtx.Done():
                                if task.cmdCtx.Err() == context.DeadlineExceeded {
                                        log.Printf("[TaskManager] Task %s exceeded timeout (%ds), killed", task.ID, task.TimeoutSecs)
                                        if task.Status == BgTaskRunning {
                                                task.Status = BgTaskFailed
                                                task.ExitCode = -2 // -2 表示超時
                                        }
                                }
                        default:
                        }
                }
        }

        if err != nil && task.Status == BgTaskRunning {
                if exitErr, ok := err.(*exec.ExitError); ok {
                        task.ExitCode = exitErr.ExitCode()
                        task.Status = BgTaskFailed
                } else {
                        task.Status = BgTaskFailed
                        task.ExitCode = -1
                }
        } else if err == nil && task.Status == BgTaskRunning {
                task.ExitCode = 0
                task.Status = BgTaskCompleted
        }

        log.Printf("[TaskManager] Task %s finished with status: %s, exit code: %d", task.ID, task.Status, task.ExitCode)

        if !task.wakeSent && (task.Status == BgTaskCompleted || task.Status == BgTaskFailed) {
                select {
                case tm.wakeChan <- task.ID:
                        log.Printf("[TaskManager] Task %s finished, triggering immediate wake notification", task.ID)
                default:
                        // wakeChan 滿時直接在當前 goroutine 同步調用，確保不丟失
                        log.Printf("[TaskManager] Wake channel full, directly invoking processWakeUp for task %s", task.ID)
                        tm.processWakeUp(task.ID)
                }
        }
}

// wakeScheduler 唤醒调度器
func (tm *TaskManager) wakeScheduler() {
        ticker := time.NewTicker(30 * time.Second)
        defer ticker.Stop()

        for {
                select {
                case <-tm.ctx.Done():
                        return
                case <-ticker.C:
                        tm.checkWakeUps()
                case taskID := <-tm.wakeChan:
                        tm.processWakeUp(taskID)
                }
        }
}

// checkWakeUps 检查是否需要唤醒
func (tm *TaskManager) checkWakeUps() {
        tm.mu.RLock()
        defer tm.mu.RUnlock()

        now := time.Now()
        for _, task := range tm.tasks {
                task.mu.RLock()
                // 雙重檢查：
                // 1. Running + 已到喚醒時間（正常定時喚醒）
                // 2. Completed/Failed + 未喚醒（補救 monitorTask wakeChan 丟失的情況）
                shouldWake := (!task.wakeSent) &&
                        ((task.Status == BgTaskRunning && now.After(task.WakeAfter)) ||
                                (task.Status == BgTaskCompleted || task.Status == BgTaskFailed))
                task.mu.RUnlock()

                if shouldWake {
                        select {
                        case tm.wakeChan <- task.ID:
                        default:
                                log.Printf("[TaskManager] Wake channel full, directly invoking processWakeUp for task %s", task.ID)
                                tm.processWakeUp(task.ID)
                        }
                }
        }
}

// processWakeUp 处理唤醒
func (tm *TaskManager) processWakeUp(taskID string) {
        tm.mu.RLock()
        task, exists := tm.tasks[taskID]
        tm.mu.RUnlock()

        if !exists {
                return
        }

        task.mu.Lock()
        if task.wakeSent {
                task.mu.Unlock()
                return
        }
        task.wakeSent = true
        task.mu.Unlock()

        log.Printf("[TaskManager] Waking up for task %s", taskID)

        if tm.wakeHandler != nil {
                tm.wakeHandler(task)
        }
}

// SetWakeHandler 设置唤醒处理函数
func (tm *TaskManager) SetWakeHandler(handler WakeHandlerFunc) {
        tm.wakeHandler = handler
}

// CheckTask 检查任务状态
func (tm *TaskManager) CheckTask(taskID string) (*BackgroundTask, error) {
        tm.mu.RLock()
        defer tm.mu.RUnlock()

        task, exists := tm.tasks[taskID]
        if !exists {
                return nil, fmt.Errorf("task %s not found", taskID)
        }

        return task, nil
}

// TerminateTask 终止任务
func (tm *TaskManager) TerminateTask(taskID string, force bool) error {
        tm.mu.RLock()
        task, exists := tm.tasks[taskID]
        tm.mu.RUnlock()

        if !exists {
                return fmt.Errorf("task %s not found", taskID)
        }

        task.mu.Lock()
        defer task.mu.Unlock()

        if task.Status != BgTaskRunning {
                return fmt.Errorf("task %s is not running (status: %s)", taskID, task.Status)
        }

        if task.process == nil {
                return fmt.Errorf("task %s has no process", taskID)
        }

        var err error
        if force {
                err = killProcessGroup(task.PID)
                log.Printf("[TaskManager] Force killing task %s (PID: %d)", taskID, task.PID)
        } else {
                err = terminateProcessGroup(task.PID)
                log.Printf("[TaskManager] Terminating task %s (PID: %d)", taskID, task.PID)
        }

        task.Status = BgTaskTerminated

        return err
}

// ListTasks 列出所有任务
func (tm *TaskManager) ListTasks() []*BackgroundTask {
        tm.mu.RLock()
        defer tm.mu.RUnlock()

        tasks := make([]*BackgroundTask, 0, len(tm.tasks))
        for _, task := range tm.tasks {
                tasks = append(tasks, task)
        }
        return tasks
}

// GetTaskInfo 获取任务信息
func (tm *TaskManager) GetTaskInfo(taskID string) (map[string]interface{}, error) {
        task, err := tm.CheckTask(taskID)
        if err != nil {
                return nil, err
        }

        task.mu.RLock()
        defer task.mu.RUnlock()

        info := map[string]interface{}{
                "task_id":         task.ID,
                "command":         task.Command,
                "description":     task.Description,
                "pid":             task.PID,
                "status":          string(task.Status),
                "exit_code":       task.ExitCode,
                "start_time":      task.StartTime.Format(time.RFC3339),
                "runtime_minutes": time.Since(task.StartTime).Minutes(),
                "stdout":          truncateTaskOutput(task.Stdout.String()),
                "stderr":          truncateTaskOutput(task.Stderr.String()),
        }

        return info, nil
}

// RemoveTask 移除已完成或已终止的任务
func (tm *TaskManager) RemoveTask(taskID string) error {
        tm.mu.Lock()
        defer tm.mu.Unlock()

        task, exists := tm.tasks[taskID]
        if !exists {
                return fmt.Errorf("task %s not found", taskID)
        }

        task.mu.RLock()
        status := task.Status
        task.mu.RUnlock()

        if status == BgTaskRunning {
                return fmt.Errorf("cannot remove running task %s, terminate it first", taskID)
        }

        delete(tm.tasks, taskID)
        log.Printf("[TaskManager] Task %s removed", taskID)
        return nil
}

// Stop 停止任务管理器
func (tm *TaskManager) Stop() {
        tm.cancel()

        tm.mu.Lock()
        defer tm.mu.Unlock()

        for _, task := range tm.tasks {
                task.mu.Lock()
                if task.Status == BgTaskRunning && task.process != nil {
                        killProcessGroup(task.PID)
                        task.Status = BgTaskTerminated
                }
                task.mu.Unlock()
        }
}

// ExtendWakeTime 延长唤醒时间
func (tm *TaskManager) ExtendWakeTime(taskID string, additionalMinutes int) error {
        tm.mu.RLock()
        task, exists := tm.tasks[taskID]
        tm.mu.RUnlock()

        if !exists {
                return fmt.Errorf("task %s not found", taskID)
        }

        task.mu.Lock()
        defer task.mu.Unlock()

        if task.Status != BgTaskRunning {
                return fmt.Errorf("task %s is not running", taskID)
        }

        if additionalMinutes < 1 {
                additionalMinutes = 1
        }
        if additionalMinutes > 1440 {
                additionalMinutes = 1440
        }

        task.WakeAfter = time.Now().Add(time.Duration(additionalMinutes) * time.Minute)
        task.wakeSent = false

        log.Printf("[TaskManager] Task %s wake time extended by %d minutes", taskID, additionalMinutes)
        return nil
}

// truncateTaskOutput 截断任务输出（用于 info 接口）
func truncateTaskOutput(output string) string {
        const maxLen = 10000
        if len(output) > maxLen {
                return TruncateString(output, maxLen) + "\n(output truncated)"
        }
        return output
}

// saveOutputToFileForWake 将过长内容保存到文件（供唤醒消息使用）
func saveOutputToFileForWake(content, prefix, command string) (string, error) {
        if len(content) <= getMaxDirectOutput() {
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

// GetTaskWakeMessage 生成任务唤醒消息（含大输出保存和尾部展示）
func GetTaskWakeMessage(task *BackgroundTask) string {
        task.mu.RLock()
        defer task.mu.RUnlock()

        var statusEmoji string
        switch task.Status {
        case BgTaskRunning:
                statusEmoji = "⏳"
        case BgTaskCompleted:
                statusEmoji = "✅"
        case BgTaskFailed:
                statusEmoji = "❌"
        case BgTaskTerminated:
                statusEmoji = "⏹️"
        default:
                statusEmoji = "❓"
        }

        runtime := time.Since(task.StartTime)

        msg := fmt.Sprintf(`
⏰ 任务唤醒通知

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
%s 任务ID: %s
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📋 基本信息:
  • 命令: %s
  • 进程ID: %d
  • 状态: %s
  • 已运行: %.1f 分钟
`, statusEmoji, task.ID, task.Command, task.PID, task.Status, runtime.Minutes())

        if task.Description != "" {
                msg += fmt.Sprintf("  • 描述: %s\n", task.Description)
        }

        // 处理 stdout
        stdout := task.Stdout.String()
        stdoutFile, err := saveOutputToFileForWake(stdout, "async_stdout", task.Command)
        if err == nil && stdoutFile != "" {
                maxDirect := getMaxDirectOutput()
                tail := TailRunes(stdout, maxDirect)
                msg += fmt.Sprintf("\n📤 标准输出:\n[输出过长，完整内容已保存至: %s]\n\n--- 最后 %d 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符\n",
                        stdoutFile, maxDirect, tail, len(stdout))
        } else if len(stdout) > getMaxDirectOutput() {
                maxDirect := getMaxDirectOutput()
                tail := TailRunes(stdout, maxDirect)
                msg += fmt.Sprintf("\n📤 标准输出:\n[输出过长已截断（无法保存文件）]\n\n--- 最后 %d 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符\n",
                        maxDirect, tail, len(stdout))
        } else if stdout != "" {
                msg += fmt.Sprintf("\n📤 标准输出:\n%s\n", stdout)
        }

        // 处理 stderr
        stderr := task.Stderr.String()
        stderrFile, err := saveOutputToFileForWake(stderr, "async_stderr", task.Command)
        if err == nil && stderrFile != "" {
                maxDirect := getMaxDirectOutput()
                tail := TailRunes(stderr, maxDirect)
                msg += fmt.Sprintf("\n⚠️ 标准错误:\n[输出过长，完整内容已保存至: %s]\n\n--- 最后 %d 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符\n",
                        stderrFile, maxDirect, tail, len(stderr))
        } else if len(stderr) > getMaxDirectOutput() {
                maxDirect := getMaxDirectOutput()
                tail := TailRunes(stderr, maxDirect)
                msg += fmt.Sprintf("\n⚠️ 标准错误:\n[输出过长已截断（无法保存文件）]\n\n--- 最后 %d 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符\n",
                        maxDirect, tail, len(stderr))
        } else if stderr != "" {
                msg += fmt.Sprintf("\n⚠️ 标准错误:\n%s\n", stderr)
        }

        // 根据状态提供操作建议
        switch task.Status {
        case BgTaskRunning:
                msg += `
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
可执行操作:
  1. 继续等待 - 告诉我等待多少分钟（如 "等待10分钟"）
  2. 检查结果 - 使用 task_check 工具
  3. 终止任务 - 使用 task_terminate 工具
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━`
        case BgTaskCompleted:
                msg += `
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✅ 任务已成功完成！
如需清理任务记录，可使用 task_remove 工具。
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━`
        case BgTaskFailed:
                if task.ExitCode == -2 {
                        msg += fmt.Sprintf(`
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
❌ 任務因超時（%d秒）被強制終止。
命令可能需要更多時間，建議增大 timeout_secs 或使用更長的 wake_after_minutes。
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━`, task.TimeoutSecs)
                } else {
                        msg += `
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
❌ 任务执行失败，请检查上方输出了解错误原因。
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━`
                }
        }

        return msg
}

// ==================== 命令检测相关 ====================

// CommandSuggestion 命令类型检测结果
type CommandSuggestion struct {
        Type             string `json:"type"`
        Message          string `json:"message"`
        Suggestion       string `json:"suggestion,omitempty"`
        NonInteractiveEq string `json:"non_interactive_eq,omitempty"`
}

// DetectCommandType 检测命令类型
func DetectCommandType(command string) CommandSuggestion {
        lowerCmd := strings.ToLower(command)

        // 快速命令白名单
        quickPatterns := []string{
                "ls", "cat ", "head ", "tail ", "wc ", "touch ", "file ",
                "mkdir ", "rmdir ", "rm ", "cp ", "mv ", "ln ",
                "echo ", "printf ", "grep ", "sed ", "awk ", "cut ", "sort ", "uniq ",
                "pwd", "whoami", "hostname", "uname", "date", "uptime", "df ", "du ",
                "ps ", "pgrep ", "pkill ", "kill ", "killall ",
                "ping -c ", "host ", "nslookup ", "dig ", "ip ", "ifconfig",
                "which ", "whereis ", "type ", "stat ", "realpath ", "readlink ",
                "git status", "git log", "git diff", "git branch", "git remote",
                "git rev", "git show", "git tag",
                "env", "export ", "printenv", "set ", "unset ",
                "expr ", "bc ", "let ",
        }
        for _, p := range quickPatterns {
                if strings.Contains(lowerCmd, p) {
                        return CommandSuggestion{Type: "quick", Message: "快速命令，将同步执行"}
                }
        }

        // SSH/SCP 特殊检测
        if strings.Contains(lowerCmd, "ssh ") || strings.Contains(lowerCmd, "scp ") {
                hasSshpass := strings.Contains(lowerCmd, "sshpass")
                if strings.Contains(lowerCmd, "ssh ") {
                        hasRemoteCommand := false
                        sshIdx := strings.Index(lowerCmd, "ssh ")
                        afterSsh := lowerCmd[sshIdx:]
                        atIdx := strings.Index(afterSsh, "@")
                        if atIdx > 0 {
                                afterAt := afterSsh[atIdx:]
                                singleQuoteIdx := strings.Index(afterAt, "'")
                                doubleQuoteIdx := strings.Index(afterAt, "\"")
                                if singleQuoteIdx > 0 || doubleQuoteIdx > 0 {
                                        hasRemoteCommand = true
                                }
                        }
                        if hasRemoteCommand {
                                return CommandSuggestion{Type: "quick", Message: "SSH 远程命令，将同步执行"}
                        } else if hasSshpass {
                                return CommandSuggestion{Type: "quick", Message: "SSH 命令（已使用 sshpass），将同步执行"}
                        }
                        return CommandSuggestion{
                                Type:             "interactive",
                                Message:          "ssh 不带命令会进入交互式 shell",
                                Suggestion:       "使用 sshpass 或密钥认证，并添加远程命令",
                                NonInteractiveEq: "sshpass -p 'password' ssh user@host 'command'",
                        }
                }
                if strings.Contains(lowerCmd, "scp ") {
                        if hasSshpass {
                                return CommandSuggestion{Type: "long_running", Message: "SCP 文件传输（已使用 sshpass），将异步执行"}
                        }
                        return CommandSuggestion{
                                Type:             "interactive",
                                Message:          "scp 需要密码",
                                Suggestion:       "使用 sshpass 或密钥认证",
                                NonInteractiveEq: "sshpass -p 'password' scp",
                        }
                }
        }

        // 交互式命令检测
        interactiveMap := map[string]CommandSuggestion{
                "vim":        {Type: "interactive", Message: "vim 是交互式编辑器", Suggestion: "使用 sed/awk 进行文本处理"},
                "nano":       {Type: "interactive", Message: "nano 是交互式编辑器", Suggestion: "使用 sed 进行文本替换"},
                "less":       {Type: "interactive", Message: "less 是分页器", Suggestion: "使用 cat 或 head -n 100 查看", NonInteractiveEq: "cat"},
                "more":       {Type: "interactive", Message: "more 是分页器", Suggestion: "使用 cat 查看", NonInteractiveEq: "cat"},
                "top":        {Type: "interactive", Message: "top 是交互式监控", Suggestion: "使用 top -b -n 1 或 ps aux", NonInteractiveEq: "top -b -n 1"},
                "htop":       {Type: "interactive", Message: "htop 是交互式监控", Suggestion: "使用 top -b -n 1 或 ps aux"},
                "git log":    {Type: "interactive", Message: "git log 会分页", Suggestion: "使用 git --no-pager log -n 20", NonInteractiveEq: "git --no-pager log -n 20"},
                "git diff":   {Type: "interactive", Message: "git diff 会分页", Suggestion: "使用 git --no-pager diff", NonInteractiveEq: "git --no-pager diff"},
                "git commit": {Type: "interactive", Message: "git commit 会打开编辑器", Suggestion: "使用 git commit -m \"message\"", NonInteractiveEq: "git commit -m \"\""},
                "python":     {Type: "interactive", Message: "python 无参数会进入 REPL", Suggestion: "使用 python script.py 或 python -c 'code'"},
                "python3":    {Type: "interactive", Message: "python3 无参数会进入 REPL", Suggestion: "使用 python3 script.py 或 python3 -c 'code'"},
                "node":       {Type: "interactive", Message: "node 无参数会进入 REPL", Suggestion: "使用 node script.js 或 node -e 'code'"},
                "sudo -i":    {Type: "interactive", Message: "sudo -i 会启动 root shell", Suggestion: "使用 sudo command"},
                "sudo su":    {Type: "interactive", Message: "sudo su 会启动交互式 shell", Suggestion: "使用 sudo command"},
                "su ":        {Type: "interactive", Message: "su 会启动交互式 shell", Suggestion: "使用 sudo command"},
                "screen":     {Type: "interactive", Message: "screen 是终端复用器", Suggestion: "需要交互"},
                "tmux":       {Type: "interactive", Message: "tmux 是终端复用器", Suggestion: "需要交互"},
        }
        for pattern, sug := range interactiveMap {
                if strings.Contains(lowerCmd, pattern) {
                        return sug
                }
        }

        // 长时间运行命令
        longPatterns := []string{
                "apt update", "apt upgrade", "apt install", "apt-get",
                "yum update", "yum upgrade", "yum install",
                "dnf update", "dnf upgrade", "dnf install",
                "pacman -S", "pacman -Syu",
                "pkg install", "pkg update", "pkg upgrade",
                "portsnap fetch", "portsnap extract", "portsnap update",
                "freebsd-update fetch", "freebsd-update install",
                "make", "cmake", "ninja",
                "npm install", "npm update", "npm run build",
                "yarn install", "yarn build",
                "pnpm install", "pnpm build",
                "pip install", "pip3 install",
                "cargo build", "cargo install",
                "go build", "go install", "go get",
                "docker build", "docker-compose build",
                "git clone", "git fetch", "git pull --rebase",
                "rsync", "scp ", "sftp ",
                "wget ", "curl -O", "curl -o",
                "tar ", "unzip ", "7z ",
                "ffmpeg", "handbrake",
                "systemctl start", "systemctl restart",
                "service ", "/etc/init.d/",
        }
        for _, p := range longPatterns {
                if strings.Contains(lowerCmd, p) {
                        return CommandSuggestion{Type: "long_running", Message: fmt.Sprintf("检测到长时命令: %s，将异步执行", p)}
                }
        }

        return CommandSuggestion{Type: "unknown", Message: "未知命令类型，将使用保守策略执行"}
}

// ==================== 平台相关函数 ====================
// 平台相关函数已移至 task_manager_windows.go 和 task_manager_unix.go 文件中

// ==================== 循环检测器 ====================

// LoopDetectionConfig 循环检测配置（从 TOON 文件加载）
type LoopDetectionConfig struct {
        Thresholds struct {
                MaxHistory         int `toon:"MaxHistory" json:"MaxHistory"`
                InterruptThreshold int `toon:"InterruptThreshold" json:"InterruptThreshold"`
                WarningThreshold   int `toon:"WarningThreshold" json:"WarningThreshold"`
                PatternWindow      int `toon:"PatternWindow" json:"PatternWindow"`
        } `toon:"Thresholds" json:"Thresholds"`
        Warnings struct {
                LoopInterrupt struct {
                        Title      string `toon:"Title" json:"Title"`
                        Message    string `toon:"Message" json:"Message"`
                        Suggestion string `toon:"Suggestion" json:"Suggestion"`
                } `toon:"LoopInterrupt" json:"LoopInterrupt"`
                LoopWarning struct {
                        Title      string `toon:"Title" json:"Title"`
                        Message    string `toon:"Message" json:"Message"`
                        Suggestion string `toon:"Suggestion" json:"Suggestion"`
                } `toon:"LoopWarning" json:"LoopWarning"`
                FailureInterrupt struct {
                        Title      string `toon:"Title" json:"Title"`
                        Message    string `toon:"Message" json:"Message"`
                        Suggestion string `toon:"Suggestion" json:"Suggestion"`
                } `toon:"FailureInterrupt" json:"FailureInterrupt"`
                FailureWarning struct {
                        Title      string `toon:"Title" json:"Title"`
                        Message    string `toon:"Message" json:"Message"`
                        Suggestion string `toon:"Suggestion" json:"Suggestion"`
                } `toon:"FailureWarning" json:"FailureWarning"`
                SequenceInterrupt struct {
                        Title      string `toon:"Title" json:"Title"`
                        Message    string `toon:"Message" json:"Message"`
                        Suggestion string `toon:"Suggestion" json:"Suggestion"`
                } `toon:"SequenceInterrupt" json:"SequenceInterrupt"`
                SequenceWarning struct {
                        Title      string `toon:"Title" json:"Title"`
                        Message    string `toon:"Message" json:"Message"`
                        Suggestion string `toon:"Suggestion" json:"Suggestion"`
                } `toon:"SequenceWarning" json:"SequenceWarning"`
        } `toon:"Warnings" json:"Warnings"`
        DataCollection struct {
                Enabled       bool   `toon:"Enabled" json:"Enabled"`
                DetailLevel   string `toon:"DetailLevel" json:"DetailLevel"`
                CollectArgs   bool   `toon:"CollectArgs" json:"CollectArgs"`
                CollectResult bool   `toon:"CollectResult" json:"CollectResult"`
                OutputPath    string `toon:"OutputPath" json:"OutputPath"`
        } `toon:"DataCollection" json:"DataCollection"`
        Evolution struct {
                AutoOptimize         bool `toon:"AutoOptimize" json:"AutoOptimize"`
                OptimizationInterval int  `toon:"OptimizationInterval" json:"OptimizationInterval"`
                MinSamples           int  `toon:"MinSamples" json:"MinSamples"`
        } `toon:"Evolution" json:"Evolution"`
}

// LoopDetectionEvent 循环检测事件（用于数据收集）
type LoopDetectionEvent struct {
        Timestamp       time.Time `json:"timestamp"`
        EventType       string    `json:"event_type"`
        ToolName        string    `json:"tool_name"`
        Fingerprint     string    `json:"fingerprint"`
        LoopCount       int       `json:"loop_count"`
        LoopPattern     []string  `json:"loop_pattern,omitempty"`
        WarningMessage  string    `json:"warning_message"`
        Suggestion      string    `json:"suggestion"`
        ShouldInterrupt bool      `json:"should_interrupt"`
        SessionID       string    `json:"session_id,omitempty"`
        ActorName       string    `json:"actor_name,omitempty"`
}

// LoopEventCollector 循环事件收集器
type LoopEventCollector struct {
        events     []LoopDetectionEvent
        mu         sync.RWMutex
        outputPath string
        enabled    bool
}

// NewLoopEventCollector 创建事件收集器
func NewLoopEventCollector(outputPath string, enabled bool) *LoopEventCollector {
        return &LoopEventCollector{
                events:     make([]LoopDetectionEvent, 0),
                outputPath: outputPath,
                enabled:    enabled,
        }
}

// RecordEvent 记录事件
func (lec *LoopEventCollector) RecordEvent(event LoopDetectionEvent) {
        if !lec.enabled {
                return
        }

        lec.mu.Lock()
        lec.events = append(lec.events, event)
        lec.mu.Unlock()

        go lec.saveEventToFile(event)
}

// saveEventToFile 将事件保存到文件
func (lec *LoopEventCollector) saveEventToFile(event LoopDetectionEvent) {
        if lec.outputPath == "" {
                return
        }

        data, err := json.Marshal(event)
        if err != nil {
                log.Printf("[LoopEventCollector] Failed to marshal event: %v", err)
                return
        }

        f, err := os.OpenFile(lec.outputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
        if err != nil {
                log.Printf("[LoopEventCollector] Failed to open event file: %v", err)
                return
        }
        defer f.Close()

        if _, err := f.Write(append(data, '\n')); err != nil {
                log.Printf("[LoopEventCollector] Failed to write event: %v", err)
        }
}

// GetEvents 获取所有事件
func (lec *LoopEventCollector) GetEvents() []LoopDetectionEvent {
        lec.mu.RLock()
        defer lec.mu.RUnlock()

        result := make([]LoopDetectionEvent, len(lec.events))
        copy(result, lec.events)
        return result
}

// 需要循环检测的工具黑名单列表（只在列表中的工具方进行检测）
var monitoredTools = map[string]bool{
        "shell":         true,
        "smart_shell":   true,
        "shell_delayed": true,
        "ssh_exec":      true,
}

// LoopDetector 循环检测器
// 用于检测模型是否在重复执行相同的工具调用，防止死循环
type LoopDetector struct {
        history            []LoopToolCallRecord
        maxHistory         int
        interruptThreshold int // 中断阈值（达到此次数则中断）
        warningThreshold   int // 警告阈值（达到此次数则警告）
        patternWindow      int // 模式检测窗口大小
        mu                 sync.RWMutex
        config             *LoopDetectionConfig // 配置（可选）
        eventCollector     *LoopEventCollector  // 事件收集器（可选）
}

// LoopToolCallRecord 循环检测用的工具调用记录
type LoopToolCallRecord struct {
        ToolName    string                 `json:"tool_name"`
        Args        map[string]interface{} `json:"args,omitempty"`
        Fingerprint string                 `json:"fingerprint"` // 用于快速比较的指纹
        Timestamp   time.Time              `json:"timestamp"`
        Result      string                 `json:"result,omitempty"` // 结果摘要
        IsError     bool                   `json:"is_error"`
}

// LoopDetectionResult 循环检测结果
type LoopDetectionResult struct {
        IsLoop          bool     `json:"is_loop"`
        LoopCount       int      `json:"loop_count"`       // 循环次数
        LoopPattern     []string `json:"loop_pattern"`     // 循环的工具序列
        WarningMessage  string   `json:"warning_message"`  // 警告信息
        Suggestion      string   `json:"suggestion"`       // 建议信息
        ShouldInterrupt bool     `json:"should_interrupt"` // 是否应该中断
}

// LoadLoopDetectionConfig 从文件加载循环检测配置
func LoadLoopDetectionConfig(configPath string) (*LoopDetectionConfig, error) {
        if configPath == "" {
                configPath = "data/loop_detection_config.toon"
        }

        data, err := os.ReadFile(configPath)
        if err != nil {
                return nil, fmt.Errorf("failed to read loop detection config: %w", err)
        }

        var config LoopDetectionConfig
        if err := toon.Unmarshal(data, &config); err != nil {
                return nil, fmt.Errorf("failed to parse loop detection config: %w", err)
        }

        return &config, nil
}

// getLoopDetectionConfigPath 获取配置文件路径
func getLoopDetectionConfigPath() string {
        if globalExecDir != "" {
                return filepath.Join(globalExecDir, "data", "loop_detection_config.toon")
        }
        return "data/loop_detection_config.toon"
}

// NewLoopDetector 创建循环检测器
func NewLoopDetector(maxHistory, interruptThreshold, warningThreshold int) *LoopDetector {
        if maxHistory < 10 {
                maxHistory = 50
        }
        if interruptThreshold < 2 {
                interruptThreshold = 2
        }
        if warningThreshold < 1 {
                warningThreshold = interruptThreshold - 1
        }
        return &LoopDetector{
                history:            make([]LoopToolCallRecord, 0, maxHistory),
                maxHistory:         maxHistory,
                interruptThreshold: interruptThreshold,
                warningThreshold:   warningThreshold,
                patternWindow:      5,
        }
}

// NewLoopDetectorWithConfig 使用配置创建循环检测器
func NewLoopDetectorWithConfig(config *LoopDetectionConfig) *LoopDetector {
        if config == nil {
                return NewLoopDetector(100, 3, 2)
        }

        ld := &LoopDetector{
                history:            make([]LoopToolCallRecord, 0, config.Thresholds.MaxHistory),
                maxHistory:         config.Thresholds.MaxHistory,
                interruptThreshold: config.Thresholds.InterruptThreshold,
                warningThreshold:   config.Thresholds.WarningThreshold,
                patternWindow:      config.Thresholds.PatternWindow,
                config:             config,
        }

        if config.DataCollection.Enabled {
                outputPath := config.DataCollection.OutputPath
                if outputPath != "" && !filepath.IsAbs(outputPath) {
                        if globalExecDir != "" {
                                outputPath = filepath.Join(globalExecDir, "data", outputPath)
                        } else {
                                outputPath = filepath.Join("data", outputPath)
                        }
                }
                ld.eventCollector = NewLoopEventCollector(outputPath, true)
        }

        return ld
}

// generateFingerprint 生成工具调用的指纹（用于快速比较）
func generateFingerprint(toolName string, args map[string]interface{}) string {
        // 对于shell命令，使用命令内容作为指纹
        if toolName == "shell" || toolName == "smart_shell" || toolName == "shell_delayed" {
                if cmd, ok := args["command"].(string); ok {
                        return toolName + ":" + cmd
                }
        }

        // 对于 ssh_exec，使用命令内容作为指纹
        if toolName == "ssh_exec" {
                if cmd, ok := args["command"].(string); ok {
                        return toolName + ":" + cmd
                }
                if sessionID, ok := args["session_id"].(string); ok {
                        return toolName + ":" + sessionID
                }
        }

        // 对于文件操作，使用文件名作为指纹的一部分
        if toolName == "read_file_line" || toolName == "read_all_lines" {
                if filename, ok := args["filename"].(string); ok {
                        return toolName + ":" + filename
                }
        }

        // 对于写文件操作，使用文件名 + 行号/内容摘要作为指纹
        if toolName == "write_file_line" {
                if filename, ok := args["filename"].(string); ok {
                        if lineNum, ok := args["line_number"].(float64); ok {
                                return toolName + ":" + filename + ":" + fmt.Sprintf("%d", int(lineNum))
                        }
                        return toolName + ":" + filename
                }
        }
        if toolName == "write_all_lines" {
                if filename, ok := args["filename"].(string); ok {
                        return toolName + ":" + filename
                }
        }

        // 对于浏览器操作，使用URL作为指纹的一部分
        if strings.HasPrefix(toolName, "browser_") {
                if url, ok := args["url"].(string); ok {
                        return toolName + ":" + url
                }
        }

        // 对于 memory_recall，使用查询内容作为指纹（避免误报）
        if toolName == "memory_recall" {
                if query, ok := args["query"].(string); ok && query != "" {
                        return toolName + ":" + query
                }
                return toolName
        }

        // 默认：尝试从参数中提取关键信息生成指纹
        // 优先使用 filename、path、url、name、id 等常见标识字段
        keyFields := []string{"filename", "path", "url", "name", "id", "key", "target", "source"}
        for _, field := range keyFields {
                if value, ok := args[field].(string); ok && value != "" {
                        return toolName + ":" + field + "=" + value
                }
        }

        // 如果没有任何标识字段，使用工具名 + 参数数量作为指纹
        // 这样可以区分不同复杂度的调用
        return toolName + ":args=" + fmt.Sprintf("%d", len(args))
}

// RecordAndCheck 记录工具调用并检测循环
func (ld *LoopDetector) RecordAndCheck(toolName string, args map[string]interface{}, result string, isError bool) LoopDetectionResult {
        // 如果工具不在监控列表中，直接放行，不记录不检查
        if !monitoredTools[toolName] {
                return LoopDetectionResult{IsLoop: false}
        }

        ld.mu.Lock()
        defer ld.mu.Unlock()

        // 创建记录
        record := LoopToolCallRecord{
                ToolName:    toolName,
                Args:        args,
                Fingerprint: generateFingerprint(toolName, args),
                Timestamp:   time.Now(),
                Result:      TruncateString(result, 100),
                IsError:     isError,
        }

        // 添加到历史
        ld.history = append(ld.history, record)
        if len(ld.history) > ld.maxHistory {
                ld.history = ld.history[1:]
        }

        // 检测循环
        return ld.detectLoop()
}

// formatMessage 格式化警告消息（使用配置模板）
func (ld *LoopDetector) formatMessage(templateStr, fingerprint string, count int, pattern []string) string {
        if ld.config == nil {
                return templateStr
        }

        tmpl, err := template.New("warning").Parse(templateStr)
        if err != nil {
                return templateStr
        }

        data := struct {
                Fingerprint string
                Count       int
                Pattern     string
        }{
                Fingerprint: fingerprint,
                Count:       count,
                Pattern:     strings.Join(pattern, " -> "),
        }

        var buf bytes.Buffer
        if err := tmpl.Execute(&buf, data); err != nil {
                return templateStr
        }

        return buf.String()
}

// getSuggestion 获取建议信息
func (ld *LoopDetector) getSuggestion(defaultSuggestion string) string {
        return defaultSuggestion
}

// recordEvent 记录循环检测事件
func (ld *LoopDetector) recordEvent(eventType, toolName, fingerprint string, count int, pattern []string, warningMsg, suggestion string, shouldInterrupt bool) {
        if ld.eventCollector == nil {
                return
        }

        event := LoopDetectionEvent{
                Timestamp:       time.Now(),
                EventType:       eventType,
                ToolName:        toolName,
                Fingerprint:     fingerprint,
                LoopCount:       count,
                LoopPattern:     pattern,
                WarningMessage:  warningMsg,
                Suggestion:      suggestion,
                ShouldInterrupt: shouldInterrupt,
        }

        ld.eventCollector.RecordEvent(event)
}

// detectLoop 检测循环模式
func (ld *LoopDetector) detectLoop() LoopDetectionResult {
        result := LoopDetectionResult{
                IsLoop:      false,
                LoopCount:   0,
                LoopPattern: []string{},
        }

        if len(ld.history) < ld.interruptThreshold && len(ld.history) < ld.warningThreshold {
                return result
        }

        // 方法1：检测相同的指纹重复出现
        fingerprintCounts := make(map[string]int)
        recentHistory := ld.history
        if len(recentHistory) > ld.patternWindow*3 {
                recentHistory = recentHistory[len(recentHistory)-ld.patternWindow*3:]
        }

        for _, record := range recentHistory {
                fingerprintCounts[record.Fingerprint]++
        }

        // 检查是否有超过阈值的指纹
        for fingerprint, count := range fingerprintCounts {
                if count >= ld.interruptThreshold {
                        result.IsLoop = true
                        result.LoopCount = count
                        result.LoopPattern = []string{fingerprint}
                        result.ShouldInterrupt = true

                        if ld.config != nil {
                                result.WarningMessage = ld.formatMessage(ld.config.Warnings.LoopInterrupt.Message, fingerprint, count, nil)
                                result.Suggestion = ld.config.Warnings.LoopInterrupt.Suggestion
                        } else {
                                result.WarningMessage = fmt.Sprintf(
                                        "🚫 ⚠️ **循环检测警告**\n\n检测到相同操作「%s」已重复执行 %d 次。\n\n这可能表明陷入了死循环，建议：\n"+
                                                "1. 分析操作失败的根本原因\n"+
                                                "2. 尝试不同的解决方案\n"+
                                                "3. 检查相关配置或日志文件\n"+
                                                "4. 考虑请求人工协助\n\n任务已被系统终止，因为检测到重复循环。",
                                        fingerprint, count)
                                result.Suggestion = "请分析之前的操作结果，找出问题根源，而不是重复相同的操作。"
                        }

                        ld.recordEvent("loop_interrupt", "", fingerprint, count, nil, result.WarningMessage, result.Suggestion, true)
                        return result
                } else if count >= ld.warningThreshold {
                        result.IsLoop = true
                        result.LoopCount = count
                        result.LoopPattern = []string{fingerprint}
                        result.ShouldInterrupt = false

                        if ld.config != nil {
                                result.WarningMessage = ld.formatMessage(ld.config.Warnings.LoopWarning.Message, fingerprint, count, nil)
                                result.Suggestion = ld.config.Warnings.LoopWarning.Suggestion
                        } else {
                                result.WarningMessage = fmt.Sprintf(
                                        "⚠️ **循环检测警告**\n\n检测到相同操作「%s」已重复执行 %d 次。\n\n请调整策略，避免继续重复相同的操作。",
                                        fingerprint, count)
                                result.Suggestion = "请分析之前的操作结果，找出问题根源，而不是重复相同的操作。"
                        }

                        ld.recordEvent("loop_warning", "", fingerprint, count, nil, result.WarningMessage, result.Suggestion, false)
                        return result
                }
        }

        // 方法2：检测操作序列模式（如 A->B->C->A->B->C）
        if len(ld.history) >= ld.patternWindow*2 {
                pattern := ld.detectPatternSequence()
                if pattern.IsLoop {
                        return pattern
                }
        }

        // 方法3：检测连续相同指纹的失败循环（修正：按指纹统计，而非工具名）
        consecutiveFailures := 0
        var lastFingerprint string
        for i := len(ld.history) - 1; i >= 0; i-- {
                record := ld.history[i]
                if record.IsError {
                        if i == len(ld.history)-1 {
                                // 最近一条，记录指纹
                                lastFingerprint = record.Fingerprint
                                consecutiveFailures = 1
                        } else if record.Fingerprint == lastFingerprint {
                                consecutiveFailures++
                        } else {
                                // 指纹不同，停止计数
                                break
                        }
                } else {
                        break
                }
        }

        if consecutiveFailures >= ld.interruptThreshold {
                result.IsLoop = true
                result.LoopCount = consecutiveFailures
                result.ShouldInterrupt = true

                if ld.config != nil {
                        result.WarningMessage = ld.formatMessage(ld.config.Warnings.FailureInterrupt.Message, lastFingerprint, consecutiveFailures, nil)
                        result.Suggestion = ld.config.Warnings.FailureInterrupt.Suggestion
                } else {
                        result.WarningMessage = fmt.Sprintf(
                                "🚫 ⚠️ **连续失败警告**\n\n检测到相同操作「%s」连续失败 %d 次。\n\n建议：\n"+
                                        "1. 仔细分析错误信息\n"+
                                        "2. 检查是否有权限、路径或配置问题\n"+
                                        "3. 尝试简化的操作步骤\n"+
                                        "4. 考虑换一种方法解决问题\n\n任务已被系统终止。",
                                lastFingerprint, consecutiveFailures)
                        result.Suggestion = "连续失败表明当前方法可能不可行，建议尝试其他方案。"
                }

                ld.recordEvent("failure_interrupt", "", lastFingerprint, consecutiveFailures, nil, result.WarningMessage, result.Suggestion, true)
                return result
        } else if consecutiveFailures >= ld.warningThreshold {
                result.IsLoop = true
                result.LoopCount = consecutiveFailures
                result.ShouldInterrupt = false

                if ld.config != nil {
                        result.WarningMessage = ld.formatMessage(ld.config.Warnings.FailureWarning.Message, lastFingerprint, consecutiveFailures, nil)
                        result.Suggestion = ld.config.Warnings.FailureWarning.Suggestion
                } else {
                        result.WarningMessage = fmt.Sprintf(
                                "⚠️ **连续失败警告**\n\n检测到相同操作「%s」连续失败 %d 次。请调整策略，避免继续重复。",
                                lastFingerprint, consecutiveFailures)
                        result.Suggestion = "连续失败表明当前方法可能不可行，建议尝试其他方案。"
                }

                ld.recordEvent("failure_warning", "", lastFingerprint, consecutiveFailures, nil, result.WarningMessage, result.Suggestion, false)
                return result
        }

        return result
}

// detectPatternSequence 检测操作序列模式
func (ld *LoopDetector) detectPatternSequence() LoopDetectionResult {
        result := LoopDetectionResult{
                IsLoop: false,
        }

        historyLen := len(ld.history)

        // 尝试检测不同长度的模式
        for patternLen := 2; patternLen <= ld.patternWindow; patternLen++ {
                if historyLen < patternLen*2 {
                        continue
                }

                // 获取最近的两段序列
                recent := ld.history[historyLen-patternLen:]
                previous := ld.history[historyLen-patternLen*2 : historyLen-patternLen]

                // 比较两个序列是否相同
                match := true
                for i := 0; i < patternLen; i++ {
                        if recent[i].Fingerprint != previous[i].Fingerprint {
                                match = false
                                break
                        }
                }

                if match {
                        // 扩展检测：检查是否有更多重复
                        repeatCount := 2
                        for start := historyLen - patternLen*3; start >= 0; start -= patternLen {
                                if start+patternLen > historyLen {
                                        break
                                }
                                segment := ld.history[start : start+patternLen]
                                segmentMatch := true
                                for i := 0; i < patternLen; i++ {
                                        if segment[i].Fingerprint != recent[i].Fingerprint {
                                                segmentMatch = false
                                                break
                                        }
                                }
                                if segmentMatch {
                                        repeatCount++
                                } else {
                                        break
                                }
                        }

                        pattern := make([]string, patternLen)
                        for i := 0; i < patternLen; i++ {
                                pattern[i] = recent[i].Fingerprint
                        }

                        if repeatCount >= ld.interruptThreshold {
                                result.IsLoop = true
                                result.LoopCount = repeatCount
                                result.LoopPattern = pattern
                                result.ShouldInterrupt = true

                                if ld.config != nil {
                                        result.WarningMessage = ld.formatMessage(ld.config.Warnings.SequenceInterrupt.Message, "", repeatCount, pattern)
                                        result.Suggestion = ld.config.Warnings.SequenceInterrupt.Suggestion
                                } else {
                                        result.WarningMessage = fmt.Sprintf(
                                                "🚫 ⚠️ **序列循环警告**\n\n检测到操作序列已重复 %d 次：\n%v\n\n建议：\n"+
                                                        "1. 这个操作序列似乎没有解决问题\n"+
                                                        "2. 请分析每次操作的结果\n"+
                                                        "3. 尝试打破这个循环，采用不同的策略\n\n任务已被系统终止。",
                                                repeatCount, pattern)
                                        result.Suggestion = "操作序列形成循环，请尝试不同的解决方法。"
                                }

                                ld.recordEvent("sequence_interrupt", "", "", repeatCount, pattern, result.WarningMessage, result.Suggestion, true)
                                return result
                        } else if repeatCount >= ld.warningThreshold {
                                result.IsLoop = true
                                result.LoopCount = repeatCount
                                result.LoopPattern = pattern
                                result.ShouldInterrupt = false

                                if ld.config != nil {
                                        result.WarningMessage = ld.formatMessage(ld.config.Warnings.SequenceWarning.Message, "", repeatCount, pattern)
                                        result.Suggestion = ld.config.Warnings.SequenceWarning.Suggestion
                                } else {
                                        result.WarningMessage = fmt.Sprintf(
                                                "⚠️ **序列循环警告**\n\n检测到操作序列已重复 %d 次：\n%v\n\n请尝试打破这个循环。",
                                                repeatCount, pattern)
                                        result.Suggestion = "操作序列形成循环，请尝试不同的解决方法。"
                                }

                                ld.recordEvent("sequence_warning", "", "", repeatCount, pattern, result.WarningMessage, result.Suggestion, false)
                                return result
                        }
                }
        }

        return result
}

// GetHistory 获取历史记录（用于调试）
func (ld *LoopDetector) GetHistory() []LoopToolCallRecord {
        ld.mu.RLock()
        defer ld.mu.RUnlock()

        result := make([]LoopToolCallRecord, len(ld.history))
        copy(result, ld.history)
        return result
}

// Clear 清除历史记录
func (ld *LoopDetector) Clear() {
        ld.mu.Lock()
        defer ld.mu.Unlock()

        ld.history = make([]LoopToolCallRecord, 0, ld.maxHistory)
}

// GetStats 获取统计信息
func (ld *LoopDetector) GetStats() map[string]interface{} {
        ld.mu.RLock()
        defer ld.mu.RUnlock()

        toolCounts := make(map[string]int)
        for _, record := range ld.history {
                toolCounts[record.ToolName]++
        }

        return map[string]interface{}{
                "total_calls":         len(ld.history),
                "max_history":         ld.maxHistory,
                "interrupt_threshold": ld.interruptThreshold,
                "warning_threshold":   ld.warningThreshold,
                "tool_counts":         toolCounts,
        }
}

// 全局循环检测器实例
var globalLoopDetector *LoopDetector

// InitGlobalLoopDetector 初始化全局循环检测器
func InitGlobalLoopDetector() {
        if globalLoopDetector == nil {
                configPath := getLoopDetectionConfigPath()
                config, err := LoadLoopDetectionConfig(configPath)
                if err != nil {
                        log.Printf("[LoopDetector] Failed to load config from %s: %v, using defaults", configPath, err)
                        // 使用默认配置
                        defaultConfig := getDefaultLoopDetectionConfig()
                        // 将默认配置写回文件
                        if data, marshalErr := toon.Marshal(defaultConfig); marshalErr == nil {
                                if writeErr := os.WriteFile(configPath, data, 0644); writeErr == nil {
                                        log.Printf("[LoopDetector] Written default config to %s", configPath)
                                } else {
                                        log.Printf("[LoopDetector] Failed to write default config: %v", writeErr)
                                }
                        } else {
                                log.Printf("[LoopDetector] Failed to marshal default config: %v", marshalErr)
                        }
                        globalLoopDetector = NewLoopDetectorWithConfig(defaultConfig)
                } else {
                        globalLoopDetector = NewLoopDetectorWithConfig(config)
                        log.Printf("[LoopDetector] Initialized with config from %s", configPath)
                }
        }
}

// CheckLoop 检测循环的便捷函数
func CheckLoop(toolName string, args map[string]interface{}, result string, isError bool) *LoopDetectionResult {
        if globalLoopDetector == nil {
                return nil
        }

        detectionResult := globalLoopDetector.RecordAndCheck(toolName, args, result, isError)
        if detectionResult.IsLoop {
                return &detectionResult
        }
        return nil
}

// ==================== 循环检测配置优化器 ====================

// LoopDetectionOptimizer 循环检测配置优化器
// 供自我进化系统内部使用，通过函数式接口安全地优化配置
type LoopDetectionOptimizer struct {
        mu             sync.RWMutex
        currentConfig  *LoopDetectionConfig
        eventCollector *LoopEventCollector

        // 优化统计
        optimizationStats LoopDetectionOptimizationStats
}

// LoopDetectionOptimizationStats 循环检测优化统计
type LoopDetectionOptimizationStats struct {
        TotalOptimizations   int       `json:"total_optimizations"`
        LastOptimizationTime time.Time `json:"last_optimization_time"`
        ConfigChanges        int       `json:"config_changes"`
        WarningImprovements  int       `json:"warning_improvements"`
}

// ConfigValidationError 配置验证错误
type ConfigValidationError struct {
        Field   string `json:"field"`
        Message string `json:"message"`
}

// OptimizationProposal 优化建议
type OptimizationProposal struct {
        Type           string       `json:"type"`
        Description    string       `json:"description"`
        Priority       string       `json:"priority"`
        ExpectedImpact float64      `json:"expected_impact"`
        Apply          func() error `json:"-"`
}

// NewLoopDetectionOptimizer 创建循环检测配置优化器
func NewLoopDetectionOptimizer(config *LoopDetectionConfig, collector *LoopEventCollector) *LoopDetectionOptimizer {
        if config == nil {
                config = getDefaultLoopDetectionConfig()
        }

        return &LoopDetectionOptimizer{
                currentConfig:  config,
                eventCollector: collector,
                optimizationStats: LoopDetectionOptimizationStats{
                        TotalOptimizations: 0,
                },
        }
}

// getDefaultLoopDetectionConfig 获取默认配置
func getDefaultLoopDetectionConfig() *LoopDetectionConfig {
        config := &LoopDetectionConfig{}
        config.Thresholds.MaxHistory = 100
        config.Thresholds.InterruptThreshold = 9 //当相同工具调用重复次数达到此阈值时，会 中断任务
        config.Thresholds.WarningThreshold = 3   //当相同工具调用重复次数达到此阈值时，会 发出警告
        config.Thresholds.PatternWindow = 10

        config.Warnings.LoopInterrupt.Title = "🚫 ⚠️ **循环检测警告**"
        config.Warnings.LoopInterrupt.Message = "检测到相同操作「{{.Fingerprint}}」已重复执行 {{.Count}} 次。\n\n这可能表明陷入了死循环。"
        config.Warnings.LoopInterrupt.Suggestion = "请分析之前的操作结果，找出问题根源，而不是重复相同的操作。"

        config.DataCollection.Enabled = true
        config.DataCollection.OutputPath = "loop_detection_events.jsonl"

        return config
}

// GetCurrentConfig 获取当前配置（只读副本）
func (ldo *LoopDetectionOptimizer) GetCurrentConfig() LoopDetectionConfig {
        ldo.mu.RLock()
        defer ldo.mu.RUnlock()

        // 返回副本，防止外部修改
        configCopy := *ldo.currentConfig
        return configCopy
}

// ValidateConfig 验证配置有效性
func (ldo *LoopDetectionOptimizer) ValidateConfig(config *LoopDetectionConfig) []ConfigValidationError {
        var errors []ConfigValidationError

        if config == nil {
                errors = append(errors, ConfigValidationError{Field: "config", Message: "配置不能为空"})
                return errors
        }

        // 验证阈值
        if config.Thresholds.MaxHistory < 10 || config.Thresholds.MaxHistory > 1000 {
                errors = append(errors, ConfigValidationError{
                        Field:   "Thresholds.MaxHistory",
                        Message: "MaxHistory 必须在 10-1000 之间",
                })
        }

        if config.Thresholds.InterruptThreshold < 2 || config.Thresholds.InterruptThreshold > 20 {
                errors = append(errors, ConfigValidationError{
                        Field:   "Thresholds.InterruptThreshold",
                        Message: "InterruptThreshold 必须在 2-20 之间",
                })
        }

        if config.Thresholds.WarningThreshold < 1 || config.Thresholds.WarningThreshold >= config.Thresholds.InterruptThreshold {
                errors = append(errors, ConfigValidationError{
                        Field:   "Thresholds.WarningThreshold",
                        Message: "WarningThreshold 必须小于 InterruptThreshold",
                })
        }

        if config.Thresholds.PatternWindow < 2 || config.Thresholds.PatternWindow > 20 {
                errors = append(errors, ConfigValidationError{
                        Field:   "Thresholds.PatternWindow",
                        Message: "PatternWindow 必须在 2-20 之间",
                })
        }

        // 验证警告消息模板
        warningTypes := []struct {
                name    string
                message string
        }{
                {"LoopInterrupt.Message", config.Warnings.LoopInterrupt.Message},
                {"LoopWarning.Message", config.Warnings.LoopWarning.Message},
                {"FailureInterrupt.Message", config.Warnings.FailureInterrupt.Message},
                {"FailureWarning.Message", config.Warnings.FailureWarning.Message},
                {"SequenceInterrupt.Message", config.Warnings.SequenceInterrupt.Message},
                {"SequenceWarning.Message", config.Warnings.SequenceWarning.Message},
        }

        for _, wt := range warningTypes {
                if wt.message == "" {
                        errors = append(errors, ConfigValidationError{
                                Field:   wt.name,
                                Message: "警告消息不能为空",
                        })
                        continue
                }
                // 验证模板语法
                if _, err := template.New("test").Parse(wt.message); err != nil {
                        errors = append(errors, ConfigValidationError{
                                Field:   wt.name,
                                Message: fmt.Sprintf("模板语法错误: %v", err),
                        })
                }
        }

        return errors
}

// UpdateThresholds 更新阈值（带验证）
func (ldo *LoopDetectionOptimizer) UpdateThresholds(maxHistory, interruptThreshold, warningThreshold, patternWindow int) error {
        ldo.mu.Lock()
        defer ldo.mu.Unlock()

        // 创建新配置进行验证
        newConfig := *ldo.currentConfig
        newConfig.Thresholds.MaxHistory = maxHistory
        newConfig.Thresholds.InterruptThreshold = interruptThreshold
        newConfig.Thresholds.WarningThreshold = warningThreshold
        newConfig.Thresholds.PatternWindow = patternWindow

        // 验证
        if errors := ldo.ValidateConfig(&newConfig); len(errors) > 0 {
                return fmt.Errorf("配置验证失败: %v", errors)
        }

        // 应用更改
        ldo.currentConfig = &newConfig
        ldo.optimizationStats.ConfigChanges++

        // 如果全局检测器存在，更新其配置
        if globalLoopDetector != nil {
                globalLoopDetector.mu.Lock()
                globalLoopDetector.maxHistory = maxHistory
                globalLoopDetector.interruptThreshold = interruptThreshold
                globalLoopDetector.warningThreshold = warningThreshold
                globalLoopDetector.patternWindow = patternWindow
                globalLoopDetector.config = &newConfig
                globalLoopDetector.mu.Unlock()
        }

        log.Printf("[LoopDetectionOptimizer] Thresholds updated: maxHistory=%d, interrupt=%d, warning=%d, patternWindow=%d",
                maxHistory, interruptThreshold, warningThreshold, patternWindow)

        return nil
}

// UpdateWarningMessage 更新警告消息（带验证）
func (ldo *LoopDetectionOptimizer) UpdateWarningMessage(warningType, title, message, suggestion string) error {
        ldo.mu.Lock()
        defer ldo.mu.Unlock()

        // 验证模板
        if _, err := template.New("warning").Parse(message); err != nil {
                return fmt.Errorf("消息模板语法错误: %w", err)
        }

        // 创建新配置
        newConfig := *ldo.currentConfig

        switch warningType {
        case "loop_interrupt":
                newConfig.Warnings.LoopInterrupt.Title = title
                newConfig.Warnings.LoopInterrupt.Message = message
                newConfig.Warnings.LoopInterrupt.Suggestion = suggestion
        case "loop_warning":
                newConfig.Warnings.LoopWarning.Title = title
                newConfig.Warnings.LoopWarning.Message = message
                newConfig.Warnings.LoopWarning.Suggestion = suggestion
        case "failure_interrupt":
                newConfig.Warnings.FailureInterrupt.Title = title
                newConfig.Warnings.FailureInterrupt.Message = message
                newConfig.Warnings.FailureInterrupt.Suggestion = suggestion
        case "failure_warning":
                newConfig.Warnings.FailureWarning.Title = title
                newConfig.Warnings.FailureWarning.Message = message
                newConfig.Warnings.FailureWarning.Suggestion = suggestion
        case "sequence_interrupt":
                newConfig.Warnings.SequenceInterrupt.Title = title
                newConfig.Warnings.SequenceInterrupt.Message = message
                newConfig.Warnings.SequenceInterrupt.Suggestion = suggestion
        case "sequence_warning":
                newConfig.Warnings.SequenceWarning.Title = title
                newConfig.Warnings.SequenceWarning.Message = message
                newConfig.Warnings.SequenceWarning.Suggestion = suggestion
        default:
                return fmt.Errorf("未知的警告类型: %s", warningType)
        }

        // 应用更改
        ldo.currentConfig = &newConfig
        ldo.optimizationStats.WarningImprovements++

        // 更新全局检测器配置
        if globalLoopDetector != nil {
                globalLoopDetector.mu.Lock()
                globalLoopDetector.config = &newConfig
                globalLoopDetector.mu.Unlock()
        }

        log.Printf("[LoopDetectionOptimizer] Warning message updated for type: %s", warningType)

        return nil
}

// AnalyzeEventData 分析事件数据并生成优化建议
func (ldo *LoopDetectionOptimizer) AnalyzeEventData(events []LoopDetectionEvent) []OptimizationProposal {
        var proposals []OptimizationProposal

        if len(events) < 10 {
                return proposals // 数据不足
        }

        // 统计警告有效性
        warningCount := 0
        interruptAfterWarning := 0
        warningEffectiveness := make(map[string]int)

        for _, event := range events {
                if !event.ShouldInterrupt {
                        warningCount++
                        // 检查后续是否有中断
                        for _, nextEvent := range events {
                                if nextEvent.Timestamp.After(event.Timestamp) &&
                                        nextEvent.Fingerprint == event.Fingerprint &&
                                        nextEvent.ShouldInterrupt {
                                        interruptAfterWarning++
                                        break
                                }
                        }
                }
                warningEffectiveness[event.EventType]++
        }

        // 分析1：警告有效性
        if warningCount > 0 {
                effectivenessRate := float64(warningCount-interruptAfterWarning) / float64(warningCount)
                if effectivenessRate < 0.3 {
                        // 警告效果不佳，建议改进
                        proposals = append(proposals, OptimizationProposal{
                                Type:           "warning_improvement",
                                Description:    fmt.Sprintf("警告有效性仅 %.1f%%，建议改进警告提示信息", effectivenessRate*100),
                                Priority:       "high",
                                ExpectedImpact: 0.8,
                                Apply: func() error {
                                        // 建议添加更具体的指导
                                        newMessage := ldo.currentConfig.Warnings.LoopWarning.Message + "\n\n请尝试：\n1. 检查之前的操作结果\n2. 思考失败原因\n3. 采用不同的方法"
                                        return ldo.UpdateWarningMessage("loop_warning",
                                                ldo.currentConfig.Warnings.LoopWarning.Title,
                                                newMessage,
                                                ldo.currentConfig.Warnings.LoopWarning.Suggestion)
                                },
                        })
                }
        }

        // 分析2：阈值调整建议
        if interruptAfterWarning > warningCount/2 {
                // 太多警告后仍然中断，可能需要降低警告阈值
                currentWarningThreshold := ldo.currentConfig.Thresholds.WarningThreshold
                if currentWarningThreshold > 1 {
                        proposals = append(proposals, OptimizationProposal{
                                Type:           "threshold_adjustment",
                                Description:    fmt.Sprintf("建议降低 WarningThreshold 从 %d 到 %d", currentWarningThreshold, currentWarningThreshold-1),
                                Priority:       "medium",
                                ExpectedImpact: 0.6,
                                Apply: func() error {
                                        return ldo.UpdateThresholds(
                                                ldo.currentConfig.Thresholds.MaxHistory,
                                                ldo.currentConfig.Thresholds.InterruptThreshold,
                                                currentWarningThreshold-1,
                                                ldo.currentConfig.Thresholds.PatternWindow,
                                        )
                                },
                        })
                }
        }

        // 分析3：频繁循环检测
        if warningCount > len(events)/3 {
                proposals = append(proposals, OptimizationProposal{
                        Type:           "sensitivity_adjustment",
                        Description:    "循环检测过于敏感，建议增加阈值",
                        Priority:       "low",
                        ExpectedImpact: 0.4,
                        Apply: func() error {
                                return ldo.UpdateThresholds(
                                        ldo.currentConfig.Thresholds.MaxHistory,
                                        ldo.currentConfig.Thresholds.InterruptThreshold+1,
                                        ldo.currentConfig.Thresholds.WarningThreshold+1,
                                        ldo.currentConfig.Thresholds.PatternWindow,
                                )
                        },
                })
        }

        return proposals
}

// ApplyOptimization 应用优化建议
func (ldo *LoopDetectionOptimizer) ApplyOptimization(proposal OptimizationProposal) error {
        if proposal.Apply == nil {
                return fmt.Errorf("优化建议没有实现 Apply 函数")
        }

        if err := proposal.Apply(); err != nil {
                return fmt.Errorf("应用优化失败: %w", err)
        }

        ldo.mu.Lock()
        ldo.optimizationStats.TotalOptimizations++
        ldo.optimizationStats.LastOptimizationTime = time.Now()
        ldo.mu.Unlock()

        log.Printf("[LoopDetectionOptimizer] Applied optimization: %s (priority: %s, impact: %.2f)",
                proposal.Description, proposal.Priority, proposal.ExpectedImpact)

        return nil
}

// GetOptimizationStats 获取优化统计
func (ldo *LoopDetectionOptimizer) GetOptimizationStats() LoopDetectionOptimizationStats {
        ldo.mu.RLock()
        defer ldo.mu.RUnlock()
        return ldo.optimizationStats
}

// ResetToDefaults 重置为默认配置
func (ldo *LoopDetectionOptimizer) ResetToDefaults() error {
        return ldo.UpdateConfig(getDefaultLoopDetectionConfig())
}

// UpdateConfig 完整更新配置（带完整验证）
func (ldo *LoopDetectionOptimizer) UpdateConfig(config *LoopDetectionConfig) error {
        if config == nil {
                return fmt.Errorf("配置不能为空")
        }

        // 验证
        if errors := ldo.ValidateConfig(config); len(errors) > 0 {
                return fmt.Errorf("配置验证失败: %v", errors)
        }

        ldo.mu.Lock()
        defer ldo.mu.Unlock()

        // 应用新配置
        ldo.currentConfig = config
        ldo.optimizationStats.ConfigChanges++

        // 更新全局检测器
        if globalLoopDetector != nil {
                globalLoopDetector.mu.Lock()
                globalLoopDetector.maxHistory = config.Thresholds.MaxHistory
                globalLoopDetector.interruptThreshold = config.Thresholds.InterruptThreshold
                globalLoopDetector.warningThreshold = config.Thresholds.WarningThreshold
                globalLoopDetector.patternWindow = config.Thresholds.PatternWindow
                globalLoopDetector.config = config
                globalLoopDetector.mu.Unlock()
        }

        log.Println("[LoopDetectionOptimizer] Config fully updated")
        return nil
}

// 全局优化器实例
var globalLoopDetectionOptimizer *LoopDetectionOptimizer

// InitLoopDetectionOptimizer 初始化循环检测配置优化器
func InitLoopDetectionOptimizer() {
        if globalLoopDetectionOptimizer == nil {
                var config *LoopDetectionConfig
                var collector *LoopEventCollector

                if globalLoopDetector != nil {
                        globalLoopDetector.mu.RLock()
                        config = globalLoopDetector.config
                        collector = globalLoopDetector.eventCollector
                        globalLoopDetector.mu.RUnlock()
                }

                globalLoopDetectionOptimizer = NewLoopDetectionOptimizer(config, collector)
                log.Println("[LoopDetectionOptimizer] Initialized")
        }
}

// GetLoopDetectionOptimizer 获取循环检测配置优化器
func GetLoopDetectionOptimizer() *LoopDetectionOptimizer {
        return globalLoopDetectionOptimizer
}
