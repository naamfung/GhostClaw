package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"
)

// ========== 原有类型定义保持不变 ==========
type MemoryConsolidatorConfig struct {
    ContextWindowTokens      int     `json:"context_window_tokens"`
    MaxCompletionTokens      int     `json:"max_completion_tokens"`
    SafetyBuffer             int     `json:"safety_buffer"`
    ConsolidationRatio       float64 `json:"consolidation_ratio"`
    MaxConsolidationRound    int     `json:"max_consolidation_round"`
    MinMessagesToConsolidate int     `json:"min_messages_to_consolidate"`
}

func DefaultMemoryConsolidatorConfig() MemoryConsolidatorConfig {
    return MemoryConsolidatorConfig{
        ContextWindowTokens:      128000,
        MaxCompletionTokens:      4096,
        SafetyBuffer:             1024,
        ConsolidationRatio:       0.01,
        MaxConsolidationRound:    5,
        MinMessagesToConsolidate: 2,
    }
}

type ConsolidationMessage struct {
    Role      string                 `json:"role"`
    Content   string                 `json:"content"`
    Timestamp time.Time              `json:"timestamp"`
    ToolsUsed []string               `json:"tools_used,omitempty"`
    Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type MemoryConsolidator struct {
    config             MemoryConsolidatorConfig
    memory             *UnifiedMemory
    mu                 sync.RWMutex
    sessionMessages    map[string][]ConsolidationMessage
    sessionOffset      map[string]int
    consolidationLocks map[string]*sync.Mutex
}

func NewMemoryConsolidator(config MemoryConsolidatorConfig, memory *UnifiedMemory) *MemoryConsolidator {
    return &MemoryConsolidator{
        config:             config,
        memory:             memory,
        sessionMessages:    make(map[string][]ConsolidationMessage),
        sessionOffset:      make(map[string]int),
        consolidationLocks: make(map[string]*sync.Mutex),
    }
}

func (mc *MemoryConsolidator) AddMessage(sessionKey string, msg ConsolidationMessage) {
    mc.mu.Lock()
    defer mc.mu.Unlock()
    mc.sessionMessages[sessionKey] = append(mc.sessionMessages[sessionKey], msg)
    if _, ok := mc.consolidationLocks[sessionKey]; !ok {
        mc.consolidationLocks[sessionKey] = &sync.Mutex{}
    }
}

func (mc *MemoryConsolidator) GetMessages(sessionKey string) []ConsolidationMessage {
    mc.mu.RLock()
    defer mc.mu.RUnlock()
    return mc.sessionMessages[sessionKey]
}

func (mc *MemoryConsolidator) GetMessageCount(sessionKey string) int {
    mc.mu.RLock()
    defer mc.mu.RUnlock()
    return len(mc.sessionMessages[sessionKey])
}

func EstimateTokens(text string) int {
    runes := []rune(text)
    zhCount := 0
    for _, r := range runes {
        if (r >= 0x4e00 && r <= 0x9fff) ||
            (r >= 0x3400 && r <= 0x4dbf) ||
            (r >= 0x20000 && r <= 0x2a6df) ||
            (r >= 0x2a700 && r <= 0x2b73f) ||
            (r >= 0x2b740 && r <= 0x2b81f) ||
            (r >= 0x2b820 && r <= 0x2ceaf) ||
            (r >= 0x2ceb0 && r <= 0x2ebef) ||
            (r >= 0x30000 && r <= 0x3134f) ||
            (r >= 0x2e80 && r <= 0x2eff) ||
            (r >= 0x31c0 && r <= 0x31ef) {
            zhCount++
        }
    }
    otherCount := len(runes) - zhCount
    return (otherCount)/4 + zhCount/2
}

func (mc *MemoryConsolidator) EstimateMessagesTokens(messages []ConsolidationMessage) int {
    total := 0
    for _, msg := range messages {
        total += EstimateTokens(msg.Content)
        total += 10
        for _, tool := range msg.ToolsUsed {
            total += EstimateTokens(tool)
        }
    }
    return total
}

func (mc *MemoryConsolidator) EstimatePromptTokens(sessionKey string) int {
    mc.mu.RLock()
    defer mc.mu.RUnlock()
    messages := mc.sessionMessages[sessionKey]
    offset := mc.sessionOffset[sessionKey]
    if offset >= len(messages) {
        return 0
    }
    return mc.EstimateMessagesTokens(messages[offset:])
}

func (mc *MemoryConsolidator) ShouldConsolidate(sessionKey string) (bool, int) {
    mc.mu.RLock()
    defer mc.mu.RUnlock()
    messages := mc.sessionMessages[sessionKey]
    offset := mc.sessionOffset[sessionKey]
    unconsolidatedCount := len(messages) - offset
    if unconsolidatedCount < mc.config.MinMessagesToConsolidate {
        return false, 0
    }
    budget := mc.config.ContextWindowTokens - mc.config.MaxCompletionTokens - mc.config.SafetyBuffer
    threshold := int(float64(budget) * mc.config.ConsolidationRatio)
    estimated := mc.EstimateMessagesTokens(messages[offset:])
    if estimated >= threshold {
        return true, estimated - budget/2
    }
    return false, 0
}

func (mc *MemoryConsolidator) GetBudgetInfo(sessionKey string) map[string]interface{} {
    mc.mu.RLock()
    defer mc.mu.RUnlock()
    budget := mc.config.ContextWindowTokens - mc.config.MaxCompletionTokens - mc.config.SafetyBuffer
    threshold := int(float64(budget) * mc.config.ConsolidationRatio)
    messages := mc.sessionMessages[sessionKey]
    offset := mc.sessionOffset[sessionKey]
    estimated := 0
    if offset < len(messages) {
        estimated = mc.EstimateMessagesTokens(messages[offset:])
    }
    return map[string]interface{}{
        "budget":             budget,
        "threshold":          threshold,
        "current_tokens":     estimated,
        "usage_ratio":        float64(estimated) / float64(budget),
        "total_messages":     len(messages),
        "consolidated":       offset,
        "unconsolidated":     len(messages) - offset,
        "should_consolidate": estimated >= threshold,
    }
}

func (mc *MemoryConsolidator) Consolidate(ctx context.Context, sessionKey string, messages []ConsolidationMessage) error {
    lock, ok := mc.consolidationLocks[sessionKey]
    if !ok {
        lock = &sync.Mutex{}
        mc.mu.Lock()
        mc.consolidationLocks[sessionKey] = lock
        mc.mu.Unlock()
    }
    lock.Lock()
    defer lock.Unlock()

    result, err := mc.callLLMForConsolidation(ctx, messages)
    if err != nil {
        return fmt.Errorf("LLM consolidation failed: %w", err)
    }

    if result.HistoryEntry != "" && mc.memory != nil {
        mc.memory.RecordSession(sessionKey, "consolidator", result.HistoryEntry, len(messages), []string{"auto", "consolidated"})
    }
    if result.MemoryUpdate != "" && mc.memory != nil {
        mc.parseAndSaveMemoryUpdate(result.MemoryUpdate)
    }

    mc.mu.Lock()
    mc.sessionOffset[sessionKey] = mc.sessionOffset[sessionKey] + len(messages)
    mc.mu.Unlock()

    log.Printf("[MemoryConsolidator] Consolidated %d messages for session %s", len(messages), sessionKey)
    return nil
}

func (mc *MemoryConsolidator) parseAndSaveMemoryUpdate(markdown string) {
    lines := strings.Split(markdown, "\n")
    var currentCategory string
    var currentEntries []string

    categoryMap := map[string]MemoryCategory{
        "Facts":        MemoryCategoryFact,
        "Preferences":  MemoryCategoryPreference,
        "Projects":     MemoryCategoryProject,
        "Skills":       MemoryCategorySkill,
        "Contexts":     MemoryCategoryContext,
        "Experiences":  MemoryCategoryExperience,
    }

    for _, line := range lines {
        line = strings.TrimSpace(line)
        if line == "" {
            continue
        }
        if strings.HasPrefix(line, "## ") {
            if currentCategory != "" && len(currentEntries) > 0 {
                mc.saveCategoryEntries(currentCategory, currentEntries)
                currentEntries = nil
            }
            title := strings.TrimPrefix(line, "## ")
            if cat, ok := categoryMap[title]; ok {
                currentCategory = string(cat)
            } else {
                currentCategory = ""
            }
            continue
        }
        if strings.HasPrefix(line, "- ") && currentCategory != "" {
            entryLine := strings.TrimPrefix(line, "- ")
            parts := strings.SplitN(entryLine, ":", 2)
            if len(parts) == 2 {
                key := strings.TrimSpace(parts[0])
                value := strings.TrimSpace(parts[1])
                currentEntries = append(currentEntries, key+"|"+value)
            } else {
                currentEntries = append(currentEntries, "|"+entryLine)
            }
        }
    }
    if currentCategory != "" && len(currentEntries) > 0 {
        mc.saveCategoryEntries(currentCategory, currentEntries)
    }
}

func (mc *MemoryConsolidator) saveCategoryEntries(category string, entries []string) {
    for _, entry := range entries {
        parts := strings.SplitN(entry, "|", 2)
        if len(parts) == 2 && parts[0] != "" {
            key := parts[0]
            value := parts[1]
            mc.memory.SaveEntry(MemoryCategory(category), key, value, nil, "global")
        } else if len(parts) == 2 && parts[0] == "" {
            value := parts[1]
            key := value
            if len(key) > 50 {
                key = key[:50]
            }
            mc.memory.SaveEntry(MemoryCategory(category), key, value, nil, "global")
        }
    }
}

func (mc *MemoryConsolidator) callLLMForConsolidation(ctx context.Context, messages []ConsolidationMessage) (*struct {
    HistoryEntry string
    MemoryUpdate string
}, error) {
    currentMemory := mc.buildCurrentMemoryContext()
    var msgTexts []string
    for _, msg := range messages {
        timeStr := msg.Timestamp.Format("2006-01-02 15:04")
        toolsStr := ""
        if len(msg.ToolsUsed) > 0 {
            toolsStr = fmt.Sprintf(" [tools: %s]", strings.Join(msg.ToolsUsed, ", "))
        }
        msgTexts = append(msgTexts, fmt.Sprintf("[%s] %s%s: %s", timeStr, strings.ToUpper(msg.Role), toolsStr, msg.Content))
    }

    prompt := fmt.Sprintf(`处理以下对话并输出整合结果。

## 当前长期记忆
%s

## 待处理对话
%s

请分析对话，提取重要信息并按照以下格式输出：
1. 历史条目：一段总结关键事件/决策/主题的段落。以 [YYYY-MM-DD HH:MM] 开头。
2. 记忆更新：完整的更新后长期记忆（markdown 格式）。包含所有现有事实和新事实。如果没有新内容，返回原内容。
   - 对于经验类，请将相似经验合并，更新评分（成功+0.1，失败-0.1，范围0-1）。
   - 对于事实、偏好、项目等，按原分类更新。

输出格式：
HISTORY: <历史条目>
MEMORY: <记忆更新>`, currentMemory, strings.Join(msgTexts, "\n"))

    chatMessages := []Message{
        {Role: "system", Content: "你是一个记忆整合代理。请按照要求输出整合结果，特别是对经验的合并和评分更新。"},
        {Role: "user", Content: prompt},
    }

    response, err := CallModelSync(ctx, chatMessages, apiType, baseURL, apiKey, modelID, temperature, maxTokens, false, false)
    if err != nil {
        return nil, fmt.Errorf("model call failed: %w", err)
    }

    result := &struct {
        HistoryEntry string
        MemoryUpdate string
    }{}
    content, ok := response.Content.(string)
    if !ok {
        return result, nil
    }

    lines := strings.Split(content, "\n")
    for _, line := range lines {
        if strings.HasPrefix(line, "HISTORY:") {
            result.HistoryEntry = strings.TrimSpace(strings.TrimPrefix(line, "HISTORY:"))
        } else if strings.HasPrefix(line, "MEMORY:") {
            result.MemoryUpdate = strings.TrimSpace(strings.TrimPrefix(line, "MEMORY:"))
        }
    }
    if result.HistoryEntry == "" && content != "" {
        now := time.Now()
        result.HistoryEntry = fmt.Sprintf("[%s] %s", now.Format("2006-01-02 15:04"), content)
    }
    return result, nil
}

func (mc *MemoryConsolidator) buildCurrentMemoryContext() string {
    if mc.memory == nil {
        return "(memory not available)"
    }
    var sb strings.Builder
    facts := mc.memory.SearchEntries(MemoryCategoryFact, "", 20)
    prefs := mc.memory.SearchEntries(MemoryCategoryPreference, "", 10)
    projects := mc.memory.SearchEntries(MemoryCategoryProject, "", 10)
    skills := mc.memory.SearchEntries(MemoryCategorySkill, "", 10)
    contexts := mc.memory.SearchEntries(MemoryCategoryContext, "", 10)
    experiences := mc.memory.SearchEntries(MemoryCategoryExperience, "", 15)

    if len(facts) > 0 {
        sb.WriteString("## Facts\n")
        for _, f := range facts {
            sb.WriteString(fmt.Sprintf("- %s: %s\n", f.Key, f.Value))
        }
        sb.WriteString("\n")
    }
    if len(prefs) > 0 {
        sb.WriteString("## Preferences\n")
        for _, p := range prefs {
            sb.WriteString(fmt.Sprintf("- %s: %s\n", p.Key, p.Value))
        }
        sb.WriteString("\n")
    }
    if len(projects) > 0 {
        sb.WriteString("## Projects\n")
        for _, p := range projects {
            sb.WriteString(fmt.Sprintf("- %s: %s\n", p.Key, p.Value))
        }
        sb.WriteString("\n")
    }
    if len(skills) > 0 {
        sb.WriteString("## Skills\n")
        for _, s := range skills {
            sb.WriteString(fmt.Sprintf("- %s: %s\n", s.Key, s.Value))
        }
        sb.WriteString("\n")
    }
    if len(contexts) > 0 {
        sb.WriteString("## Contexts\n")
        for _, c := range contexts {
            sb.WriteString(fmt.Sprintf("- %s: %s\n", c.Key, c.Value))
        }
        sb.WriteString("\n")
    }
    if len(experiences) > 0 {
        sb.WriteString("## Experiences\n")
        for _, e := range experiences {
            summary := e.Summary
            if summary == "" {
                summary = e.Key
            }
            sb.WriteString(fmt.Sprintf("- %s [score:%.2f]\n", summary, e.Score))
        }
        sb.WriteString("\n")
    }

    result := sb.String()
    if result == "" {
        return "(empty)"
    }
    return result
}

func (mc *MemoryConsolidator) MaybeConsolidate(ctx context.Context, sessionKey string) error {
    shouldConsolidate, _ := mc.ShouldConsolidate(sessionKey)
    if !shouldConsolidate {
        return nil
    }
    mc.mu.RLock()
    messages := mc.sessionMessages[sessionKey]
    offset := mc.sessionOffset[sessionKey]
    mc.mu.RUnlock()
    if offset >= len(messages) {
        return nil
    }
    boundary := mc.findConsolidationBoundary(sessionKey, offset)
    if boundary <= offset {
        return nil
    }
    toConsolidate := messages[offset:boundary]
    return mc.Consolidate(ctx, sessionKey, toConsolidate)
}

func (mc *MemoryConsolidator) findConsolidationBoundary(sessionKey string, startIdx int) int {
    mc.mu.RLock()
    defer mc.mu.RUnlock()
    messages := mc.sessionMessages[sessionKey]
    if startIdx >= len(messages) {
        return startIdx
    }
    for i := startIdx + 1; i < len(messages); i++ {
        if messages[i].Role == "user" {
            return i
        }
    }
    return len(messages)
}

func (mc *MemoryConsolidator) ClearSession(sessionKey string) {
    mc.mu.Lock()
    defer mc.mu.Unlock()
    delete(mc.sessionMessages, sessionKey)
    delete(mc.sessionOffset, sessionKey)
    delete(mc.consolidationLocks, sessionKey)
}

func (mc *MemoryConsolidator) ResetSessionOffset(sessionKey string) {
    mc.mu.Lock()
    defer mc.mu.Unlock()
    mc.sessionOffset[sessionKey] = 0
}

// WriteDailyLog 写入每日日志
func (mc *MemoryConsolidator) WriteDailyLog(sessionID string, messages []Message) error {
        if len(messages) == 0 {
                return nil
        }
        // 确保 memory 目录存在
        memoryDir := filepath.Join(globalDataDir, "memory")
        if err := os.MkdirAll(memoryDir, 0755); err != nil {
                return err
        }

        today := time.Now().Format("2006-01-02")
        dailyLogPath := filepath.Join(memoryDir, today+".md")

        // 提取关键信息
        var entries []string
        for _, msg := range messages {
                if msg.Role == "user" {
                        if content, ok := msg.Content.(string); ok && content != "" {
                                entries = append(entries, fmt.Sprintf("- [用户] %s", truncateByRune(content, 200)))
                        }
                } else if msg.Role == "assistant" {
                        if msg.ToolCalls != nil {
                                entries = append(entries, "- [工具调用] ...")
                        } else if content, ok := msg.Content.(string); ok && content != "" {
                                entries = append(entries, fmt.Sprintf("- [助手] %s", truncateByRune(content, 200)))
                        }
                } else if msg.Role == "tool" {
                        if content, ok := msg.Content.(string); ok && content != "" {
                                entries = append(entries, fmt.Sprintf("- [工具结果] %s", truncateByRune(content, 100)))
                        }
                }
        }

        if len(entries) == 0 {
                return nil
        }

        // 打开文件（追加或创建）
        f, err := os.OpenFile(dailyLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
        if err != nil {
                return err
        }
        defer f.Close()

        timestamp := time.Now().Format("15:04:05")
        header := fmt.Sprintf("\n## 会话 %s [%s]\n", sessionID, timestamp)
        if _, err := f.WriteString(header); err != nil {
                return err
        }
        for _, entry := range entries {
                if _, err := f.WriteString(entry + "\n"); err != nil {
                        return err
                }
        }
        return nil
}

// truncateByRune 按字符数安全截断字符串，保留完整的 UTF-8 字符
func truncateByRune(s string, maxRunes int) string {
        runes := []rune(s)
        if len(runes) <= maxRunes {
                return s
        }
        return string(runes[:maxRunes]) + "..."
}

// ========== 全局函数 ==========
var globalMemoryConsolidator *MemoryConsolidator

func InitMemoryConsolidator(config MemoryConsolidatorConfig, memory *UnifiedMemory) {
    if globalMemoryConsolidator == nil {
        globalMemoryConsolidator = NewMemoryConsolidator(config, memory)
    }
}

func GetMemoryConsolidator() *MemoryConsolidator {
    return globalMemoryConsolidator
}

// GetConsolidationTools 返回记忆整合工具定义
func GetConsolidationTools() []map[string]interface{} {
    return []map[string]interface{}{
        {
            "type": "function",
            "function": map[string]interface{}{
                "name":        "consolidate_memory",
                "description": "将当前对话中的关键信息整合到长期记忆系统中。当对话内容较长或包含重要信息时，使用此工具进行记忆整合。",
                "parameters": map[string]interface{}{
                    "type": "object",
                    "properties": map[string]interface{}{
                        "history_entry": map[string]interface{}{
                            "type":        "string",
                            "description": "一段总结关键事件/决策/主题的段落。以 [YYYY-MM-DD HH:MM] 开头。",
                        },
                        "memory_update": map[string]interface{}{
                            "type":        "string",
                            "description": "完整的更新后长期记忆（markdown 格式）。",
                        },
                    },
                    "required": []string{"history_entry", "memory_update"},
                },
            },
        },
    }
}

// HandleConsolidateMemory 处理记忆整合工具调用
func HandleConsolidateMemory(args map[string]interface{}) (string, error) {
    historyEntry, _ := args["history_entry"].(string)
    memoryUpdate, _ := args["memory_update"].(string)
    if historyEntry == "" {
        return "", fmt.Errorf("history_entry is required")
    }
    if globalUnifiedMemory != nil {
        globalUnifiedMemory.RecordSession("default", "tool", historyEntry, 0, []string{"manual"})
        if memoryUpdate != "" {
            globalUnifiedMemory.SaveEntry("fact", "manual_consolidation", memoryUpdate, []string{"manual"}, "global")
        }
    }
    return "Memory consolidated successfully", nil
}
