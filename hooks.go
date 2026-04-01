package main

import (
        "context"
        "encoding/json"
        "fmt"
        "log"
        "sort"
        "strings"
        "sync"
        "time"
)

// HookEvent 定义 Hook 触发的事件类型
type HookEvent string

const (
        HookEventBeforeModelCall HookEvent = "BeforeModelCall"
        HookEventBeforeToolCall  HookEvent = "BeforeToolCall"
        HookEventAfterToolCall   HookEvent = "AfterToolCall"
)

// HookOutcome 定义 Hook 的返回结果类型
type HookOutcome string

const (
        HookOutcomeAllow  HookOutcome = "allow"
        HookOutcomeBlock  HookOutcome = "block"
        HookOutcomeModify HookOutcome = "modify"
)

// HookResult 表示 Hook 执行的结果
type HookResult struct {
        Action        HookOutcome            `json:"action"`
        Reason        string                 `json:"reason,omitempty"`
        Patch         map[string]interface{} `json:"patch,omitempty"`
        ModifiedInput map[string]interface{} `json:"modified_input,omitempty"`
}

// HookFunc Hook 回调函数类型
type HookFunc func(ctx context.Context, event HookEvent, payload interface{}) *HookResult

// HookDef 定义 Hook 的结构
type HookDef struct {
        Name        string     `json:"name"`
        Description string     `json:"description"`
        Events      []HookEvent `json:"events"`
        Handler     HookFunc   `json:"-"` // Go 回调函数
        Priority    int        `json:"priority"`
        Enabled     bool       `json:"enabled"`
}

// HookInfo 用于 API 返回的 Hook 信息
type HookInfo struct {
        Name        string      `json:"name"`
        Description string      `json:"description"`
        Events      []HookEvent `json:"events"`
        Priority    int         `json:"priority"`
        Enabled     bool        `json:"enabled"`
}

// HookPayloadBeforeModel BeforeModelCall 事件的负载
type HookPayloadBeforeModel struct {
        Event        string `json:"event"`
        ChatID       int64  `json:"chat_id"`
        Channel      string `json:"channel,omitempty"`
        Iteration    int    `json:"iteration"`
        SystemPrompt string `json:"system_prompt,omitempty"`
        MessagesLen  int    `json:"messages_len"`
        ToolsLen     int    `json:"tools_len"`
}

// HookPayloadBeforeTool BeforeToolCall 事件的负载
type HookPayloadBeforeTool struct {
        Event     string                 `json:"event"`
        ChatID    int64                  `json:"chat_id"`
        Channel   string                 `json:"channel,omitempty"`
        Iteration int                    `json:"iteration"`
        ToolName  string                 `json:"tool_name"`
        ToolInput map[string]interface{} `json:"tool_input"`
}

// HookPayloadAfterTool AfterToolCall 事件的负载
type HookPayloadAfterTool struct {
        Event     string                 `json:"event"`
        ChatID    int64                  `json:"chat_id"`
        Channel   string                 `json:"channel,omitempty"`
        Iteration int                    `json:"iteration"`
        ToolName  string                 `json:"tool_name"`
        ToolInput map[string]interface{} `json:"tool_input"`
        Result    *ToolResultInfo        `json:"result"`
}

// ToolResultInfo 工具结果信息
type ToolResultInfo struct {
        Content    string `json:"content"`
        IsError    bool   `json:"is_error"`
        StatusCode int    `json:"status_code,omitempty"`
        DurationMs int64  `json:"duration_ms,omitempty"`
}

// HookToolCallRecord 记录单次工具调用（用于 Hook 追踪）
type HookToolCallRecord struct {
        ToolName  string
        Command   string    // 对于 shell 工具，记录命令
        Input     string    // 工具输入的 JSON 表示
        IsError   bool
        Timestamp time.Time
}

// RepeatedCallTracker 追踪重复的工具调用
type RepeatedCallTracker struct {
        mu            sync.RWMutex
        records       map[int64][]HookToolCallRecord
        maxItems      int
        threshold     int
        lastCmdPerTool map[int64]map[string]string // chatID -> toolName -> lastCommand
}

// 全局重复调用跟踪器
var globalCallTracker *RepeatedCallTracker

// NewRepeatedCallTracker 创建新的跟踪器
func NewRepeatedCallTracker(maxItems, threshold int) *RepeatedCallTracker {
        return &RepeatedCallTracker{
                records:        make(map[int64][]HookToolCallRecord),
                maxItems:       maxItems,
                threshold:      threshold,
                lastCmdPerTool: make(map[int64]map[string]string),
        }
}

// Record 记录一次工具调用
func (t *RepeatedCallTracker) Record(chatID int64, toolName, command string, isError bool) {
        t.mu.Lock()
        defer t.mu.Unlock()

        record := HookToolCallRecord{
                ToolName:  toolName,
                Command:   command,
                IsError:   isError,
                Timestamp: time.Now(),
        }

        records := t.records[chatID]
        records = append(records, record)
        if len(records) > t.maxItems {
                records = records[len(records)-t.maxItems:]
        }
        t.records[chatID] = records

        // 更新最后命令映射
        if _, ok := t.lastCmdPerTool[chatID]; !ok {
                t.lastCmdPerTool[chatID] = make(map[string]string)
        }
        t.lastCmdPerTool[chatID][toolName] = command
}

// CheckRepeatedCall 检查是否有重复调用（连续相同命令）
func (t *RepeatedCallTracker) CheckRepeatedCall(chatID int64, toolName, command string) (int, bool, string) {
        t.mu.RLock()
        records := t.records[chatID]
        t.mu.RUnlock()

        if len(records) == 0 {
                return 0, false, ""
        }

        // 统计连续完全相同的命令（从最新往前）
        consecutiveCount := 0
        var sampleCommands []string
        hasFailure := false

        for i := len(records) - 1; i >= 0; i-- {
                r := records[i]
                if r.ToolName != toolName {
                        break
                }
                if r.Command == command {
                        consecutiveCount++
                        if r.IsError {
                                hasFailure = true
                        }
                        if len(sampleCommands) < 3 {
                                sampleCommands = append(sampleCommands, r.Command)
                        }
                } else {
                        break // 命令不同，停止计数
                }
        }

        if consecutiveCount >= t.threshold {
                var warning string
                if hasFailure {
                        warning = fmt.Sprintf(
                                "⚠️ 检测到重复失败: 工具 [%s] 已连续执行相同命令 %d 次（其中有失败）。\n"+
                                        "模型可能陷入了重复尝试循环。\n"+
                                        "建议：检查命令是否正确，或尝试其他方法。\n"+
                                        "重复执行的命令：\n%s",
                                toolName, consecutiveCount,
                                strings.Join(sampleCommands, "\n"),
                        )
                } else {
                        warning = fmt.Sprintf(
                                "⚠️ 检测到无效重复: 工具 [%s] 已连续执行相同命令 %d 次，但结果似乎未帮助完成任务。\n"+
                                        "模型可能陷入了无效循环。\n"+
                                        "建议：命令执行成功但未获得有效结果，请尝试不同的方法或改变策略。\n"+
                                        "重复执行的命令：\n%s",
                                toolName, consecutiveCount,
                                strings.Join(sampleCommands, "\n"),
                        )
                }
                return consecutiveCount, true, warning
        }

        return consecutiveCount, false, ""
}

// CheckAndResetIfDifferent 检查上一条命令是否不同，如果不同则返回 true（表示连续计数已自然归零）
// 注意：这里不执行任何删除操作，因为 CheckRepeatedCall 已经基于命令比较来统计连续次数。
// 此方法仅用于日志或外部判断，保持与原有接口兼容。
func (t *RepeatedCallTracker) CheckAndResetIfDifferent(chatID int64, toolName, command string) bool {
        t.mu.RLock()
        defer t.mu.RUnlock()

        if _, ok := t.lastCmdPerTool[chatID]; !ok {
                return false
        }
        lastCmd, exists := t.lastCmdPerTool[chatID][toolName]
        if exists && lastCmd != command {
                // 命令已改变，连续计数会自然归零，无需清空任何记录
                return true
        }
        return false
}

// Clear 清除指定 chat 的所有记录（仅用于会话结束等场景）
func (t *RepeatedCallTracker) Clear(chatID int64) {
        t.mu.Lock()
        defer t.mu.Unlock()
        delete(t.records, chatID)
        delete(t.lastCmdPerTool, chatID)
}

// HookManager 管理 Hook 的注册、启用/禁用和执行
type HookManager struct {
        hooks   []HookDef
        enabled bool
        mu      sync.RWMutex
}

// 全局 Hook 管理器
var globalHookManager *HookManager

// InitHookManager 初始化全局 Hook 管理器
func InitHookManager(config *Config) *HookManager {
        enabled := true
        if config.Hooks != nil && config.Hooks.Enabled != nil {
                enabled = *config.Hooks.Enabled
        }

        manager := &HookManager{
                hooks:   make([]HookDef, 0),
                enabled: enabled,
        }

        // 注册内置 Hook
        manager.registerBuiltinHooks()

        globalHookManager = manager
        return manager
}

// GetHookManager 获取全局 Hook 管理器
func GetHookManager() *HookManager {
        return globalHookManager
}

// registerBuiltinHooks 注册内置 Hook
func (m *HookManager) registerBuiltinHooks() {
        // 初始化全局调用跟踪器（保留最近100条记录，阈值6次）
        globalCallTracker = NewRepeatedCallTracker(100, 6)

        // 危险命令检测 Hook
        m.Register(HookDef{
                Name:        "dangerous-command-check",
                Description: "检测危险命令（如 rm -rf、dd 等）并发出警告",
                Events:      []HookEvent{HookEventBeforeToolCall},
                Handler:     hookDangerousCommand,
                Priority:    10,
                Enabled:     true,
        })

        // 重复命令检测 Hook
        m.Register(HookDef{
                Name:        "repeated-commands-check",
                Description: "检测重复的命令调用（无论成功或失败），防止模型陷入循环",
                Events:      []HookEvent{HookEventAfterToolCall},
                Handler:     hookRepeatedCommands,
                Priority:    5, // 高优先级
                Enabled:     true,
        })

        // 审计日志 Hook
        m.Register(HookDef{
                Name:        "audit-log",
                Description: "记录所有工具调用到审计日志",
                Events:      []HookEvent{HookEventBeforeToolCall, HookEventAfterToolCall},
                Handler:     hookAuditLog,
                Priority:    1000, // 低优先级，最后执行
                Enabled:     true,
        })
}

// hookDangerousCommand 危险命令检测 Hook
func hookDangerousCommand(ctx context.Context, event HookEvent, payload interface{}) *HookResult {
        beforeTool, ok := payload.(*HookPayloadBeforeTool)
        if !ok {
                return &HookResult{Action: HookOutcomeAllow}
        }

        // 只检查 shell 工具
        if beforeTool.ToolName != "shell" && beforeTool.ToolName != "shell_exec" {
                return &HookResult{Action: HookOutcomeAllow}
        }

        // 获取命令
        cmd, ok := beforeTool.ToolInput["command"].(string)
        if !ok {
                return &HookResult{Action: HookOutcomeAllow}
        }

        // 危险命令模式
        dangerousPatterns := []string{
                "rm -rf /",
                "rm -rf /*",
                "dd if=",
                "mkfs",
                ":(){ :|:& };:",
                "> /dev/sd",
                "chmod -R 777 /",
                "curl | sh",
                "wget | sh",
                "curl | bash",
                "wget | bash",
        }

        cmdLower := strings.ToLower(cmd)
        for _, pattern := range dangerousPatterns {
                if strings.Contains(cmdLower, strings.ToLower(pattern)) {
                        return &HookResult{
                                Action: HookOutcomeModify,
                                Patch: map[string]interface{}{
                                        "warning": fmt.Sprintf("⚠️ 检测到潜在危险命令: %s", pattern),
                                        "risk":    "high",
                                },
                                Reason: fmt.Sprintf("命令包含危险模式: %s", pattern),
                        }
                }
        }

        return &HookResult{Action: HookOutcomeAllow}
}

// hookAuditLog 审计日志 Hook
func hookAuditLog(ctx context.Context, event HookEvent, payload interface{}) *HookResult {
        // 简单的审计日志输出
        switch event {
        case HookEventBeforeToolCall:
                if beforeTool, ok := payload.(*HookPayloadBeforeTool); ok {
                        inputJSON, _ := json.Marshal(beforeTool.ToolInput)
                        fmt.Printf("[AUDIT] BeforeToolCall: chat=%d tool=%s input=%s\n",
                                beforeTool.ChatID, beforeTool.ToolName, string(inputJSON))
                }
        case HookEventAfterToolCall:
                if afterTool, ok := payload.(*HookPayloadAfterTool); ok {
                        fmt.Printf("[AUDIT] AfterToolCall: chat=%d tool=%s error=%v\n",
                                afterTool.ChatID, afterTool.ToolName, afterTool.Result.IsError)
                }
        }
        return &HookResult{Action: HookOutcomeAllow}
}

// hookRepeatedCommands 重复命令检测 Hook（同时检测成功和失败的重复）
func hookRepeatedCommands(ctx context.Context, event HookEvent, payload interface{}) *HookResult {
        afterTool, ok := payload.(*HookPayloadAfterTool)
        if !ok {
                return &HookResult{Action: HookOutcomeAllow}
        }

        // 只检查可能重复的工具（shell 相关）
        if afterTool.ToolName != "shell" && afterTool.ToolName != "shell_exec" &&
                afterTool.ToolName != "smart_shell" && afterTool.ToolName != "bash" {
                return &HookResult{Action: HookOutcomeAllow}
        }

        // 提取命令
        command := ""
        if cmd, ok := afterTool.ToolInput["command"].(string); ok {
                command = cmd
        }

        // 先检查上一条命令是否不同，如果不同则重置
        globalCallTracker.CheckAndResetIfDifferent(afterTool.ChatID, afterTool.ToolName, command)

        // 记录此次调用
        globalCallTracker.Record(afterTool.ChatID, afterTool.ToolName, command, afterTool.Result.IsError)

        // 检测重复调用（不管成功还是失败）
        count, triggered, warning := globalCallTracker.CheckRepeatedCall(
                afterTool.ChatID, afterTool.ToolName, command)

        if triggered {
                log.Printf("[HOOK] Repeated call detected: chat=%d tool=%s count=%d isError=%v",
                        afterTool.ChatID, afterTool.ToolName, count, afterTool.Result.IsError)
                return &HookResult{
                        Action: HookOutcomeModify,
                        Patch: map[string]interface{}{
                                "warning":          warning,
                                "repeated_count":   count,
                                "suggested_action": "请检查命令是否正确，或尝试其他方法。如果问题持续，请向用户寻求帮助。",
                        },
                        Reason: fmt.Sprintf("工具 %s 连续执行相同命令 %d 次，可能陷入循环", afterTool.ToolName, count),
                }
        }

        return &HookResult{Action: HookOutcomeAllow}
}

// Register 注册 Hook
func (m *HookManager) Register(hook HookDef) {
        m.mu.Lock()
        defer m.mu.Unlock()

        // 检查是否已存在
        for i, h := range m.hooks {
                if h.Name == hook.Name {
                        m.hooks[i] = hook
                        return
                }
        }

        m.hooks = append(m.hooks, hook)

        // 按优先级排序
        sort.Slice(m.hooks, func(i, j int) bool {
                return m.hooks[i].Priority < m.hooks[j].Priority
        })
}

// Unregister 注销 Hook
func (m *HookManager) Unregister(name string) {
        m.mu.Lock()
        defer m.mu.Unlock()

        for i, h := range m.hooks {
                if h.Name == name {
                        m.hooks = append(m.hooks[:i], m.hooks[i+1:]...)
                        return
                }
        }
}

// List 列出所有 Hook
func (m *HookManager) List() []HookInfo {
        m.mu.RLock()
        defer m.mu.RUnlock()

        result := make([]HookInfo, 0, len(m.hooks))
        for _, h := range m.hooks {
                result = append(result, HookInfo{
                        Name:        h.Name,
                        Description: h.Description,
                        Events:      h.Events,
                        Priority:    h.Priority,
                        Enabled:     h.Enabled,
                })
        }
        return result
}

// Info 获取单个 Hook 信息
func (m *HookManager) Info(name string) *HookInfo {
        m.mu.RLock()
        defer m.mu.RUnlock()

        for _, h := range m.hooks {
                if h.Name == name {
                        return &HookInfo{
                                Name:        h.Name,
                                Description: h.Description,
                                Events:      h.Events,
                                Priority:    h.Priority,
                                Enabled:     h.Enabled,
                        }
                }
        }
        return nil
}

// SetEnabled 设置 Hook 启用状态
func (m *HookManager) SetEnabled(name string, enabled bool) error {
        m.mu.Lock()
        defer m.mu.Unlock()

        for i, h := range m.hooks {
                if h.Name == name {
                        m.hooks[i].Enabled = enabled
                        return nil
                }
        }

        return fmt.Errorf("hook not found: %s", name)
}

// Run 执行指定事件的所有 Hook
func (m *HookManager) Run(ctx context.Context, event HookEvent, payload interface{}) *HookResult {
        if !m.enabled {
                return &HookResult{Action: HookOutcomeAllow}
        }

        m.mu.RLock()
        hooks := make([]HookDef, len(m.hooks))
        copy(hooks, m.hooks)
        m.mu.RUnlock()

        // 按优先级过滤匹配事件的 Hook
        var matched []HookDef
        for _, h := range hooks {
                if !h.Enabled {
                        continue
                }
                for _, e := range h.Events {
                        if e == event {
                                matched = append(matched, h)
                                break
                        }
                }
        }

        // 执行 Hook
        for _, hook := range matched {
                if hook.Handler == nil {
                        continue
                }

                result := hook.Handler(ctx, event, payload)

                // 处理结果
                switch result.Action {
                case HookOutcomeBlock:
                        return result
                case HookOutcomeModify:
                        return result
                case HookOutcomeAllow:
                        // 继续执行下一个 Hook
                }
        }

        return &HookResult{Action: HookOutcomeAllow}
}

// RunBeforeModel 执行 BeforeModelCall 事件
func (m *HookManager) RunBeforeModel(ctx context.Context, chatID int64, channel string, iteration int, systemPrompt string, messagesLen, toolsLen int) *HookResult {
        payload := &HookPayloadBeforeModel{
                Event:        string(HookEventBeforeModelCall),
                ChatID:       chatID,
                Channel:      channel,
                Iteration:    iteration,
                SystemPrompt: systemPrompt,
                MessagesLen:  messagesLen,
                ToolsLen:     toolsLen,
        }
        return m.Run(ctx, HookEventBeforeModelCall, payload)
}

// RunBeforeTool 执行 BeforeToolCall 事件
func (m *HookManager) RunBeforeTool(ctx context.Context, chatID int64, channel string, iteration int, toolName string, toolInput map[string]interface{}) *HookResult {
        payload := &HookPayloadBeforeTool{
                Event:     string(HookEventBeforeToolCall),
                ChatID:    chatID,
                Channel:   channel,
                Iteration: iteration,
                ToolName:  toolName,
                ToolInput: toolInput,
        }
        return m.Run(ctx, HookEventBeforeToolCall, payload)
}

// RunAfterTool 执行 AfterToolCall 事件
func (m *HookManager) RunAfterTool(ctx context.Context, chatID int64, channel string, iteration int, toolName string, toolInput map[string]interface{}, result *ToolResultInfo) *HookResult {
        payload := &HookPayloadAfterTool{
                Event:     string(HookEventAfterToolCall),
                ChatID:    chatID,
                Channel:   channel,
                Iteration: iteration,
                ToolName:  toolName,
                ToolInput: toolInput,
                Result:    result,
        }
        return m.Run(ctx, HookEventAfterToolCall, payload)
}

// IsEnabled 检查 Hook 系统是否启用
func (m *HookManager) IsEnabled() bool {
        return m.enabled
}

// SetSystemEnabled 设置整个 Hook 系统的启用状态
func (m *HookManager) SetSystemEnabled(enabled bool) {
        m.mu.Lock()
        defer m.mu.Unlock()
        m.enabled = enabled
}

// Reload 重新加载 Hooks（重新注册内置 Hook）
func (m *HookManager) Reload() {
        m.mu.Lock()
        defer m.mu.Unlock()

        // 清空现有 Hook
        m.hooks = make([]HookDef, 0)

        // 重新注册内置 Hook
        m.registerBuiltinHooks()
}
