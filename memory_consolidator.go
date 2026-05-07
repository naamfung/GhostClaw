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

    "gopkg.in/yaml.v3"
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
        ConsolidationRatio:       0.05, // 增加默认值，从 0.01 调整为 0.05
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
    mc.mu.Lock()
    lock, ok := mc.consolidationLocks[sessionKey]
    if !ok {
        lock = &sync.Mutex{}
        mc.consolidationLocks[sessionKey] = lock
    }
    // 持鎖期間先獲取 per-session lock，避免 TOCTOU
    lock.Lock()
    mc.mu.Unlock()
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

    useAPIType, useBaseURL, useAPIKey, useModelID, useTemp, useMaxTokens, _, _ := getEffectiveAPIConfig()
    response, err := CallModelSync(ctx, chatMessages, useAPIType, useBaseURL, useAPIKey, useModelID, useTemp, useMaxTokens, false, false)
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
    log.Println("########################")
    log.Println("[MemoryConsolidator] Checking consolidation status...")
    
    shouldConsolidate, excessTokens := mc.ShouldConsolidate(sessionKey)
    log.Printf("[MemoryConsolidator] Should consolidate: %v, Excess tokens: %d", shouldConsolidate, excessTokens)
    
    if !shouldConsolidate {
        log.Println("[MemoryConsolidator] Consolidation not needed")
        log.Println("########################")
        return nil
    }
    
    mc.mu.RLock()
    messages := mc.sessionMessages[sessionKey]
    offset := mc.sessionOffset[sessionKey]
    mc.mu.RUnlock()
    
    log.Printf("[MemoryConsolidator] Current state - Total messages: %d, Consolidated: %d, Unconsolidated: %d", len(messages), offset, len(messages)-offset)
    
    if offset >= len(messages) {
        log.Println("[MemoryConsolidator] No messages to consolidate")
        log.Println("########################")
        return nil
    }
    
    // 在同一鎖內重新獲取 messages 和 boundary，防止 TOCTOU
    boundary, safeMessages, safeOffset := mc.getConsolidationSlice(sessionKey)
    if boundary <= safeOffset || safeOffset >= len(safeMessages) {
        log.Println("[MemoryConsolidator] Boundary invalid or no messages, skipping consolidation")
        log.Println("########################")
        return nil
    }
    
    toConsolidate := safeMessages[safeOffset:boundary]
    log.Printf("[MemoryConsolidator] Consolidating %d messages", len(toConsolidate))
    
    err := mc.Consolidate(ctx, sessionKey, toConsolidate)
    if err != nil {
        log.Printf("[MemoryConsolidator] Consolidation failed: %v", err)
    } else {
        log.Println("[MemoryConsolidator] Consolidation completed successfully")
    }
    
    log.Println("########################")
    return err
}

func (mc *MemoryConsolidator) findConsolidationBoundary(sessionKey string, startIdx int) int {
    mc.mu.RLock()
    defer mc.mu.RUnlock()
    messages := mc.sessionMessages[sessionKey]
    if startIdx >= len(messages) {
        return startIdx
    }
    
    // 尝试找到下一个用户消息作为边界
    for i := startIdx + 1; i < len(messages); i++ {
        if messages[i].Role == "user" {
            return i
        }
    }
    
    // 如果没有找到用户消息，使用一个合理的边界
    // 至少整合 2 条消息（如果有足够的消息）
    if len(messages) - startIdx >= 2 {
        return startIdx + 2
    }
    
    // 如果消息不足，使用所有剩余消息
    return len(messages)
}

// getConsolidationSlice 在單一 RLock 內獲取 messages、offset 和 boundary，防止 TOCTOU
func (mc *MemoryConsolidator) getConsolidationSlice(sessionKey string) (boundary int, messages []ConsolidationMessage, offset int) {
    mc.mu.RLock()
    defer mc.mu.RUnlock()
    messages = mc.sessionMessages[sessionKey]
    offset = mc.sessionOffset[sessionKey]
    if offset >= len(messages) {
        return offset, messages, offset
    }
    // 就地計算 boundary
    for i := offset + 1; i < len(messages); i++ {
        if messages[i].Role == "user" {
            return i, messages, offset
        }
    }
    if len(messages)-offset >= 2 {
        return offset + 2, messages, offset
    }
    return len(messages), messages, offset
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

// sessionYAMLEntry 單個會話的 YAML 記錄
type sessionYAMLEntry struct {
        SessionID string           `yaml:"session_id"`
        Timestamp string           `yaml:"timestamp"`
        Date      string           `yaml:"date"`
        Messages  []messageSummary `yaml:"messages"`
}

type messageSummary struct {
        Role      string   `yaml:"role"`
        Content   string   `yaml:"content"`
        ToolCalls []string `yaml:"tool_names,omitempty"`
}

// WriteDailyLog 以 YAML 格式寫入每日會話日誌。
// 結構化格式便於機器解析，可在 DB 損壞時恢復會話記錄。
func (mc *MemoryConsolidator) WriteDailyLog(sessionID string, messages []Message) error {
        if len(messages) == 0 {
                return nil
        }
        memoryDir := MemoryDir()
        if err := os.MkdirAll(memoryDir, 0755); err != nil {
                return err
        }

        now := time.Now()
        today := now.Format("2006-01-02")
        dailyLogPath := filepath.Join(memoryDir, today+".yaml")

        var summaries []messageSummary
        for _, msg := range messages {
                // 略過系統消息（記憶圍欄等元數據），只記錄用戶/助手對話
                if msg.Role == "system" {
                        continue
                }
                var s messageSummary
                s.Role = msg.Role
                if content, ok := msg.Content.(string); ok {
                        s.Content = TruncateRunes(content, 300)
                }
                if msg.ToolCalls != nil {
                        s.ToolCalls = []string{"(tool_calls)"}
                }
                if s.Content != "" || len(s.ToolCalls) > 0 {
                        summaries = append(summaries, s)
                }
        }

        if len(summaries) == 0 {
                return nil
        }

        entry := sessionYAMLEntry{
                SessionID: sessionID,
                Timestamp: now.Format("15:04:05"),
                Date:      today,
                Messages:  summaries,
        }

        yamlBytes, err := yaml.Marshal(entry)
        if err != nil {
                return err
        }

        f, err := os.OpenFile(dailyLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
        if err != nil {
                return err
        }
        defer f.Close()

        // YAML 文檔分隔符
        if _, err := f.WriteString("\n---\n"); err != nil {
                return err
        }
        if _, err := f.Write(yamlBytes); err != nil {
                return err
        }
        return nil
}

// ========== 記憶 YAML 備份與恢復 ==========

// memoryBackupEntry 單條記憶的 YAML 結構
type memoryBackupEntry struct {
        Category string `yaml:"category"`
        Key      string `yaml:"key"`
        Value    string `yaml:"value"`
        Scope    string `yaml:"scope"`
        Tags     string `yaml:"tags,omitempty"`
        Score    float64 `yaml:"score"`
}

// memoryBackup 完整記憶備份的 YAML 結構
type memoryBackup struct {
        BackupDate string             `yaml:"backup_date"`
        Memories   []memoryBackupEntry `yaml:"memories"`
}

// BackupMemoriesToYAML 將所有記憶匯出為 YAML 文件。
// 用於定期備份，確保 DB 損壞時可從 YAML 恢復。
func BackupMemoriesToYAML() error {
        if globalDB == nil {
                return fmt.Errorf("database not initialized")
        }
        memoryDir := MemoryDir()
        if err := os.MkdirAll(memoryDir, 0755); err != nil {
                return err
        }

        var records []Memories
        if err := globalDB.Find(&records).Error; err != nil {
                return fmt.Errorf("failed to query memories: %w", err)
        }

        var entries []memoryBackupEntry
        for _, r := range records {
                entries = append(entries, memoryBackupEntry{
                        Category: r.Category,
                        Key:      r.Key,
                        Value:    r.Value,
                        Scope:    r.Scope,
                        Tags:     r.Tags,
                        Score:    r.Score,
                })
        }

        backup := memoryBackup{
                BackupDate: time.Now().Format(time.RFC3339),
                Memories:   entries,
        }

        yamlBytes, err := yaml.Marshal(backup)
        if err != nil {
                return fmt.Errorf("failed to marshal memories: %w", err)
        }

        backupPath := filepath.Join(memoryDir, "memories_backup.yaml")
        if err := os.WriteFile(backupPath, yamlBytes, 0644); err != nil {
                return fmt.Errorf("failed to write backup: %w", err)
        }

        log.Printf("[MemoryBackup] %d memories backed up to %s", len(entries), backupPath)
        return nil
}

// RecoverMemoriesFromYAML 從 YAML 備份文件恢復記憶到數據庫。
// 僅恢復數據庫中不存在的記憶（以 key 判斷），避免重複。
// 返回恢復的記憶數量。
func RecoverMemoriesFromYAML() (int, error) {
        if globalDB == nil {
                return 0, fmt.Errorf("database not initialized")
        }

        memoryDir := MemoryDir()
        backupPath := filepath.Join(memoryDir, "memories_backup.yaml")

        data, err := os.ReadFile(backupPath)
        if err != nil {
                return 0, fmt.Errorf("cannot read backup file: %w", err)
        }

        var backup memoryBackup
        if err := yaml.Unmarshal(data, &backup); err != nil {
                return 0, fmt.Errorf("failed to parse backup YAML: %w", err)
        }

        recovered := 0
        for _, entry := range backup.Memories {
                // 檢查是否已存在（以 key 判斷）
                var existing Memories
                result := globalDB.Where("key = ? AND category = ?", entry.Key, entry.Category).First(&existing)
                if result.Error == nil {
                        continue // 已存在，跳過
                }

                record := Memories{
                        ID:       fmt.Sprintf("recovered-%s-%s-%d", entry.Category, entry.Key, time.Now().UnixNano()),
                        Category: entry.Category,
                        Key:      entry.Key,
                        Value:    entry.Value,
                        Scope:    entry.Scope,
                        Tags:     entry.Tags,
                        Score:    entry.Score,
                }
                if err := globalDB.Create(&record).Error; err != nil {
                        log.Printf("[MemoryRecover] Failed to restore %s/%s: %v", entry.Category, entry.Key, err)
                        continue
                }
                recovered++
        }

        log.Printf("[MemoryRecover] Recovered %d memories from %s", recovered, backupPath)
        return recovered, nil
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
                "name":        "ConsolidateMemory",
                "description": "将当前对话中的关键信息整合到长期记忆系统中。当对话内容较长或包含重要信息时，使用此工具进行记忆整合。",
                "parameters": map[string]interface{}{
                    "type": "object",
                    "properties": map[string]interface{}{
                        "HistoryEntry": map[string]interface{}{
                            "type":        "string",
                            "description": "一段总结关键事件/决策/主题的段落。以 [YYYY-MM-DD HH:MM] 开头。",
                        },
                        "MemoryUpdate": map[string]interface{}{
                            "type":        "string",
                            "description": "完整的更新后长期记忆（markdown 格式）。",
                        },
                    },
                    "required": []string{"HistoryEntry", "MemoryUpdate"},
                },
            },
        },
    }
}

// HandleConsolidateMemory 处理记忆整合工具调用
func HandleConsolidateMemory(args map[string]interface{}) (string, error) {
    historyEntry, _ := args["HistoryEntry"].(string)
    memoryUpdate, _ := args["MemoryUpdate"].(string)
    if historyEntry == "" {
        return "", fmt.Errorf("HistoryEntry is required")
    }
    if globalUnifiedMemory != nil {
        globalUnifiedMemory.RecordSession("default", "tool", historyEntry, 0, []string{"manual"})
        if memoryUpdate != "" {
            globalUnifiedMemory.SaveEntry("fact", "manual_consolidation", memoryUpdate, []string{"manual"}, "global")
        }
    }
    return "Memory consolidated successfully", nil
}
