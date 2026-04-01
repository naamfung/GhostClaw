package main

import (
    "bytes"
    "context"
    "crypto/md5"
    "fmt"
    "log"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "strconv"
    "strings"
    "sync"
    "syscall"
    "time"

    "github.com/google/uuid"
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
    ID          string              `json:"id"`
    Command     string              `json:"command"`
    Description string              `json:"description,omitempty"`
    PID         int                 `json:"pid"`
    StartTime   time.Time           `json:"start_time"`
    Status      BackgroundTaskStatus `json:"status"`
    ExitCode    int                 `json:"exit_code,omitempty"`
    Stdout      *safeBuffer         `json:"-"`
    Stderr      *safeBuffer         `json:"-"`
    WakeAfter   time.Time           `json:"wake_after"`
    SessionID   string              `json:"session_id,omitempty"`

    mu       sync.RWMutex
    process  *os.Process
    done     chan struct{}
    wakeSent bool
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
func (tm *TaskManager) StartDelayedExec(command string, wakeAfterMinutes int, description string, sessionID string) (*BackgroundTask, error) {
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
        done:        make(chan struct{}),
    }

    cmd := exec.CommandContext(tm.ctx, "sh", "-c", command)
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

    log.Printf("[TaskManager] Task %s started, PID: %d, wake after %d minutes", taskID, task.PID, wakeAfterMinutes)

    return task, nil
}

// monitorTask 监控任务执行
func (tm *TaskManager) monitorTask(task *BackgroundTask, cmd *exec.Cmd) {
    defer close(task.done)

    err := cmd.Wait()

    task.mu.Lock()
    defer task.mu.Unlock()

    if err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok {
            task.ExitCode = exitErr.ExitCode()
            if task.Status == BgTaskRunning {
                task.Status = BgTaskFailed
            }
        } else if task.Status == BgTaskRunning {
            task.Status = BgTaskFailed
            task.ExitCode = -1
        }
    } else {
        task.ExitCode = 0
        if task.Status == BgTaskRunning {
            task.Status = BgTaskCompleted
        }
    }

    log.Printf("[TaskManager] Task %s finished with status: %s, exit code: %d", task.ID, task.Status, task.ExitCode)

    if !task.wakeSent && (task.Status == BgTaskCompleted || task.Status == BgTaskFailed) {
        select {
        case tm.wakeChan <- task.ID:
            log.Printf("[TaskManager] Task %s finished, triggering immediate wake notification", task.ID)
        default:
            log.Printf("[TaskManager] Wake channel full, cannot send immediate wake for task %s", task.ID)
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
        shouldWake := task.Status == BgTaskRunning &&
            now.After(task.WakeAfter) &&
            !task.wakeSent
        task.mu.RUnlock()

        if shouldWake {
            select {
            case tm.wakeChan <- task.ID:
            default:
                log.Printf("[TaskManager] Wake channel full, skipping wake for task %s", task.ID)
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
        return output[:maxLen] + "\n... (output truncated)"
    }
    return output
}

// saveOutputToFileForWake 将过长内容保存到文件（供唤醒消息使用）
func saveOutputToFileForWake(content, prefix, command string) (string, error) {
    const maxDirectOutput = 1000
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
        tail := tailContent(stdout, 500)
        msg += fmt.Sprintf("\n📤 标准输出:\n[输出过长，完整内容已保存至: %s]\n\n--- 最后 500 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符\n",
            stdoutFile, tail, len(stdout))
    } else if len(stdout) > 1000 {
        tail := tailContent(stdout, 500)
        msg += fmt.Sprintf("\n📤 标准输出:\n[输出过长已截断（无法保存文件）]\n\n--- 最后 500 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符\n",
            tail, len(stdout))
    } else if stdout != "" {
        msg += fmt.Sprintf("\n📤 标准输出:\n%s\n", stdout)
    }

    // 处理 stderr
    stderr := task.Stderr.String()
    stderrFile, err := saveOutputToFileForWake(stderr, "async_stderr", task.Command)
    if err == nil && stderrFile != "" {
        tail := tailContent(stderr, 500)
        msg += fmt.Sprintf("\n⚠️ 标准错误:\n[输出过长，完整内容已保存至: %s]\n\n--- 最后 500 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符\n",
            stderrFile, tail, len(stderr))
    } else if len(stderr) > 1000 {
        tail := tailContent(stderr, 500)
        msg += fmt.Sprintf("\n⚠️ 标准错误:\n[输出过长已截断（无法保存文件）]\n\n--- 最后 500 字符 ---\n%s\n--- 结束 ---\n原始长度: %d 字符\n",
            tail, len(stderr))
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
        msg += `
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
❌ 任务执行失败，请检查上方输出了解错误原因。
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━`
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
        "vim":     {Type: "interactive", Message: "vim 是交互式编辑器", Suggestion: "使用 sed/awk 进行文本处理"},
        "nano":    {Type: "interactive", Message: "nano 是交互式编辑器", Suggestion: "使用 sed 进行文本替换"},
        "less":    {Type: "interactive", Message: "less 是分页器", Suggestion: "使用 cat 或 head -n 100 查看", NonInteractiveEq: "cat"},
        "more":    {Type: "interactive", Message: "more 是分页器", Suggestion: "使用 cat 查看", NonInteractiveEq: "cat"},
        "top":     {Type: "interactive", Message: "top 是交互式监控", Suggestion: "使用 top -b -n 1 或 ps aux", NonInteractiveEq: "top -b -n 1"},
        "htop":    {Type: "interactive", Message: "htop 是交互式监控", Suggestion: "使用 top -b -n 1 或 ps aux"},
        "git log": {Type: "interactive", Message: "git log 会分页", Suggestion: "使用 git --no-pager log -n 20", NonInteractiveEq: "git --no-pager log -n 20"},
        "git diff": {Type: "interactive", Message: "git diff 会分页", Suggestion: "使用 git --no-pager diff", NonInteractiveEq: "git --no-pager diff"},
        "git commit": {Type: "interactive", Message: "git commit 会打开编辑器", Suggestion: "使用 git commit -m \"message\"", NonInteractiveEq: "git commit -m \"\""},
        "python":  {Type: "interactive", Message: "python 无参数会进入 REPL", Suggestion: "使用 python script.py 或 python -c 'code'"},
        "python3": {Type: "interactive", Message: "python3 无参数会进入 REPL", Suggestion: "使用 python3 script.py 或 python3 -c 'code'"},
        "node":    {Type: "interactive", Message: "node 无参数会进入 REPL", Suggestion: "使用 node script.js 或 node -e 'code'"},
        "sudo -i": {Type: "interactive", Message: "sudo -i 会启动 root shell", Suggestion: "使用 sudo command"},
        "sudo su": {Type: "interactive", Message: "sudo su 会启动交互式 shell", Suggestion: "使用 sudo command"},
        "su ":     {Type: "interactive", Message: "su 会启动交互式 shell", Suggestion: "使用 sudo command"},
        "screen":  {Type: "interactive", Message: "screen 是终端复用器", Suggestion: "需要交互"},
        "tmux":    {Type: "interactive", Message: "tmux 是终端复用器", Suggestion: "需要交互"},
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
func getSysProcAttr() *syscall.SysProcAttr {
    if runtime.GOOS == "windows" {
        return nil
    }
    return &syscall.SysProcAttr{
        Setpgid: true,
    }
}

func killProcessGroup(pid int) error {
    if runtime.GOOS == "windows" {
        cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
        return cmd.Run()
    }
    return syscall.Kill(-pid, syscall.SIGKILL)
}

func terminateProcessGroup(pid int) error {
    if runtime.GOOS == "windows" {
        cmd := exec.Command("taskkill", "/T", "/PID", strconv.Itoa(pid))
        return cmd.Run()
    }
    return syscall.Kill(-pid, syscall.SIGTERM)
}

// ==================== 循环检测器 ====================

// 需要循环检测的工具列表（只有列表中的工具才进行检测，避免误报）
var monitoredTools = map[string]bool{
    "shell":          true,
    "smart_shell":    true,
    "shell_delayed":  true,
    "ssh_exec":       true,
    "read_file_line": true,
    "read_all_lines": true,
    "write_file_line": true,
    "write_all_lines": true,
    "browser_click":      true,
    "browser_type":       true,
    "browser_scroll":     true,
    "browser_wait_element": true,
    "browser_extract_links": true,
    "browser_extract_images": true,
    "browser_execute_js": true,
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
}

// LoopToolCallRecord 循环检测用的工具调用记录
type LoopToolCallRecord struct {
    ToolName   string                 `json:"tool_name"`
    Args       map[string]interface{} `json:"args,omitempty"`
    Fingerprint string               `json:"fingerprint"` // 用于快速比较的指纹
    Timestamp  time.Time             `json:"timestamp"`
    Result     string                `json:"result,omitempty"` // 结果摘要
    IsError    bool                  `json:"is_error"`
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

// generateFingerprint 生成工具调用的指纹（用于快速比较）
func generateFingerprint(toolName string, args map[string]interface{}) string {
    // 对于shell命令，使用命令内容作为指纹
    if toolName == "shell" || toolName == "smart_shell" {
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

    // 默认：使用工具名称
    return toolName
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
        Result:      truncateString(result, 100),
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
			result.WarningMessage = fmt.Sprintf(
				"🚫 ⚠️ **循环检测警告**\n\n检测到相同操作「%s」已重复执行 %d 次。\n\n这可能表明陷入了死循环，建议：\n"+
					"1. 分析操作失败的根本原因\n"+
					"2. 尝试不同的解决方案\n"+
					"3. 检查相关配置或日志文件\n"+
					"4. 考虑请求人工协助\n\n任务已被系统终止，因为检测到重复循环。",
				fingerprint, count)
			result.Suggestion = "请分析之前的操作结果，找出问题根源，而不是重复相同的操作。"
			result.ShouldInterrupt = true
			return result
		} else if count >= ld.warningThreshold {
			result.IsLoop = true
			result.LoopCount = count
			result.LoopPattern = []string{fingerprint}
			result.WarningMessage = fmt.Sprintf(
				"⚠️ **循环检测警告**\n\n检测到相同操作「%s」已重复执行 %d 次。\n\n请调整策略，避免继续重复相同的操作。",
				fingerprint, count)
			result.Suggestion = "请分析之前的操作结果，找出问题根源，而不是重复相同的操作。"
			result.ShouldInterrupt = false
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
		result.WarningMessage = fmt.Sprintf(
			"🚫 ⚠️ **连续失败警告**\n\n检测到相同操作「%s」连续失败 %d 次。\n\n建议：\n"+
				"1. 仔细分析错误信息\n"+
				"2. 检查是否有权限、路径或配置问题\n"+
				"3. 尝试简化的操作步骤\n"+
				"4. 考虑换一种方法解决问题\n\n任务已被系统终止。",
			lastFingerprint, consecutiveFailures)
		result.Suggestion = "连续失败表明当前方法可能不可行，建议尝试其他方案。"
		result.ShouldInterrupt = true
		return result
	} else if consecutiveFailures >= ld.warningThreshold {
		result.IsLoop = true
		result.LoopCount = consecutiveFailures
		result.WarningMessage = fmt.Sprintf(
			"⚠️ **连续失败警告**\n\n检测到相同操作「%s」连续失败 %d 次。请调整策略，避免继续重复。",
			lastFingerprint, consecutiveFailures)
		result.Suggestion = "连续失败表明当前方法可能不可行，建议尝试其他方案。"
		result.ShouldInterrupt = false
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
                result.WarningMessage = fmt.Sprintf(
                    "🚫 ⚠️ **序列循环警告**\n\n检测到操作序列已重复 %d 次：\n%v\n\n建议：\n"+
                        "1. 这个操作序列似乎没有解决问题\n"+
                        "2. 请分析每次操作的结果\n"+
                        "3. 尝试打破这个循环，采用不同的策略\n\n任务已被系统终止。",
                    repeatCount, pattern)
                result.Suggestion = "操作序列形成循环，请尝试不同的解决方法。"
                result.ShouldInterrupt = true
                return result
            } else if repeatCount >= ld.warningThreshold {
                result.IsLoop = true
                result.LoopCount = repeatCount
                result.LoopPattern = pattern
                result.WarningMessage = fmt.Sprintf(
                    "⚠️ **序列循环警告**\n\n检测到操作序列已重复 %d 次：\n%v\n\n请尝试打破这个循环。",
                    repeatCount, pattern)
                result.Suggestion = "操作序列形成循环，请尝试不同的解决方法。"
                result.ShouldInterrupt = false
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
        "total_calls":          len(ld.history),
        "max_history":          ld.maxHistory,
        "interrupt_threshold":  ld.interruptThreshold,
        "warning_threshold":    ld.warningThreshold,
        "tool_counts":          toolCounts,
    }
}

// 全局循环检测器实例
var globalLoopDetector *LoopDetector

// InitGlobalLoopDetector 初始化全局循环检测器
func InitGlobalLoopDetector() {
    if globalLoopDetector == nil {
        // 中断阈值 3，警告阈值 2
        globalLoopDetector = NewLoopDetector(100, 3, 2)
        log.Println("[LoopDetector] Initialized with max_history=100, interrupt=3, warning=2")
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
