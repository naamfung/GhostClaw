package main

import (
        "crypto/sha256"
        "encoding/json"
        "fmt"
        "log"
        "os"
        "path/filepath"
        "sort"
        "strings"
        "sync"
        "time"

        "github.com/toon-format/toon-go"
)

// SavedSession 保存的会话数据结构（内存中使用）
type SavedSession struct {
        ID          string
        Description string
        CreatedAt   time.Time
        UpdatedAt   time.Time
        History     []Message
        Role        string
        Actor       string
}

// SessionEntry TOON 兼容的会话条目（用于导入/导出文件格式）
type SessionEntry struct {
        ID          string         `toon:"id"`
        Description string         `toon:"description"`
        CreatedAt   string         `toon:"created_at"`
        UpdatedAt   string         `toon:"updated_at"`
        History     []MessageEntry `toon:"history"`
        Role        string         `toon:"role,omitempty"`
        Actor       string         `toon:"actor,omitempty"`
}

// MessageEntry TOON 兼容的消息条目（用于导入/导出文件格式）
type MessageEntry struct {
        Role             string `toon:"role"`
        Content          string `toon:"content,omitempty"`
        ContentJSON      string `toon:"content_json,omitempty"`
        ToolCalls        string `toon:"tool_calls,omitempty"`
        ToolCallID       string `toon:"tool_call_id,omitempty"`
        ReasoningContent string `toon:"reasoning_content,omitempty"`
        ThinkingSignature string `toon:"thinking_signature,omitempty"`
}

// ToEntry 将 SavedSession 转换为 TOON 兼容的 SessionEntry（用于导出）
func (s *SavedSession) ToEntry() SessionEntry {
        entries := make([]MessageEntry, 0, len(s.History))
        for _, m := range s.History {
                entries = append(entries, messageToEntry(m))
        }
        return SessionEntry{
                ID:          s.ID,
                Description: s.Description,
                CreatedAt:   s.CreatedAt.Format(time.RFC3339),
                UpdatedAt:   s.UpdatedAt.Format(time.RFC3339),
                History:     entries,
                Role:        s.Role,
                Actor:       s.Actor,
        }
}

// ToSession 将 SessionEntry 转换回 SavedSession（用于导入）
func (e *SessionEntry) ToSession() SavedSession {
        s := SavedSession{
                ID:          e.ID,
                Description: e.Description,
                Role:        e.Role,
                Actor:       e.Actor,
        }
        if t, err := time.Parse(time.RFC3339, e.CreatedAt); err == nil {
                s.CreatedAt = t
        }
        if t, err := time.Parse(time.RFC3339, e.UpdatedAt); err == nil {
                s.UpdatedAt = t
        }
        s.History = make([]Message, 0, len(e.History))
        for _, me := range e.History {
                s.History = append(s.History, entryToMessage(me))
        }
        return s
}

// messageToEntry 将 Message 转换为 MessageEntry
func messageToEntry(m Message) MessageEntry {
        me := MessageEntry{
                Role:       m.Role,
                ToolCallID: m.ToolCallID,
        }

        if m.Content != nil {
                if str, ok := m.Content.(string); ok {
                        me.Content = str
                } else {
                        if data, err := json.Marshal(m.Content); err == nil {
                                me.ContentJSON = string(data)
                        }
                }
        }

        if m.ToolCalls != nil {
                if data, err := json.Marshal(m.ToolCalls); err == nil {
                        me.ToolCalls = string(data)
                }
        }

        if m.ReasoningContent != nil {
                if str, ok := m.ReasoningContent.(string); ok {
                        me.ReasoningContent = str
                } else {
                        if data, err := json.Marshal(m.ReasoningContent); err == nil {
                                me.ReasoningContent = string(data)
                        }
                }
        }

        if m.ThinkingSignature != "" {
                me.ThinkingSignature = m.ThinkingSignature
        }

        return me
}

// entryToMessage 将 MessageEntry 转换回 Message
func entryToMessage(me MessageEntry) Message {
        m := Message{
                Role:              me.Role,
                ToolCallID:        me.ToolCallID,
                ThinkingSignature: me.ThinkingSignature,
        }

        if me.Content != "" {
                m.Content = me.Content
        } else if me.ContentJSON != "" {
                var content interface{}
                if err := json.Unmarshal([]byte(me.ContentJSON), &content); err == nil {
                        m.Content = content
                }
        }

        // ReasoningContent 反序列化：先嘗試 JSON unmarshal（非字符串類型曾被 JSON 序列化存儲）
        if me.ReasoningContent != "" {
                if strings.HasPrefix(me.ReasoningContent, "{") || strings.HasPrefix(me.ReasoningContent, "[") {
                        var rc interface{}
                        if err := json.Unmarshal([]byte(me.ReasoningContent), &rc); err == nil {
                                m.ReasoningContent = rc
                        } else {
                                m.ReasoningContent = me.ReasoningContent
                        }
                } else {
                        m.ReasoningContent = me.ReasoningContent
                }
        }

        if me.ToolCalls != "" {
                var toolCalls interface{}
                if err := json.Unmarshal([]byte(me.ToolCalls), &toolCalls); err == nil {
                        m.ToolCalls = toolCalls
                }
        }

        return m
}

// SessionFile TOON 文件结构（用于导入/导出文件格式）
type SessionFile struct {
        Session SessionEntry `toon:"session"`
}

// ============================================================
// SessionPersistManager — 基于 GORM/SQLite 的会话持久化管理器
// 完全替代文件系统存储，所有会话数据存储在 ghostclaw.db 中
// ============================================================

// SessionPersistManager 会话持久化管理器
type SessionPersistManager struct{}

// NewSessionPersistManager 创建会话持久化管理器（基于数据库）
func NewSessionPersistManager() *SessionPersistManager {
        if globalDB == nil {
                log.Println("[SessionPersist] Warning: globalDB is nil, session persistence will not work")
        }
        return &SessionPersistManager{}
}

// SaveSession 保存会话到数据库
func (m *SessionPersistManager) SaveSession(sessionID string, history []Message, description string) (*SavedSession, error) {
        now := time.Now()

        // 使用传入的 sessionID 作为 persistID（不再额外拼接时间戳）
        persistID := sessionID

        // 获取当前角色和演员信息
        var currentRole, currentActor string
        if globalStage != nil {
                currentActor = globalStage.GetCurrentActor()
        }
        if globalActorManager != nil && currentActor != "" {
                if actor, ok := globalActorManager.GetActor(currentActor); ok {
                        currentRole = actor.Role
                }
        }

        // 获取 token 追蹤統計
        var inputTokens, outputTokens, totalTokens, turnCount int
        session := GetGlobalSession()
        if tracker := session.GetTracker(); tracker != nil {
                stats := tracker.GetStats()
                inputTokens = stats.InputTokens
                outputTokens = stats.OutputTokens
                totalTokens = stats.TotalTokens
                turnCount = stats.TurnCount
        }

        // 序列化消息历史为 JSON
        historyJSON, err := json.Marshal(history)
        if err != nil {
                return nil, fmt.Errorf("序列化会话历史失败: %w", err)
        }

        row := SessionHistories{
                ID:          persistID,
                Description: description,
                Role:        currentRole,
                Actor:       currentActor,
                HistoryJSON: string(historyJSON),
                CreatedAt:   now,
                UpdatedAt:   now,
                InputTokens:  inputTokens,
                OutputTokens: outputTokens,
                TotalTokens:  totalTokens,
                TurnCount:    turnCount,
        }

        // 使用 Save 而非 Create：Save 在主鍵存在時執行 UPDATE，不存在時執行 INSERT（UPSERT），
        // 避免 idle/token 重置後 persistEmptySession 再次插入相同 ID 導致 UNIQUE constraint 失敗。
        if result := globalDB.Save(&row); result.Error != nil {
                return nil, fmt.Errorf("保存会话到数据库失败: %w", result.Error)
        }

        saved := &SavedSession{
                ID:          persistID,
                Description: description,
                CreatedAt:   now,
                UpdatedAt:   now,
                History:     history,
                Role:        currentRole,
                Actor:       currentActor,
        }

        return saved, nil
}

// UpdateSession 更新数据库中已有的会话
func (m *SessionPersistManager) UpdateSession(sessionID string, history []Message) error {
        // 序列化消息历史为 JSON
        historyJSON, err := json.Marshal(history)
        if err != nil {
                return fmt.Errorf("序列化会话历史失败: %w", err)
        }

        // 获取 token 追蹤統計
        updates := map[string]interface{}{
                "history_json": string(historyJSON),
                "updated_at":   time.Now(),
        }
        session := GetGlobalSession()
        if tracker := session.GetTracker(); tracker != nil {
                stats := tracker.GetStats()
                updates["input_tokens"] = stats.InputTokens
                updates["output_tokens"] = stats.OutputTokens
                updates["total_tokens"] = stats.TotalTokens
                updates["turn_count"] = stats.TurnCount
        }

        result := globalDB.Model(&SessionHistories{}).
                Where("id = ?", sessionID).
                Updates(updates)
        if result.Error != nil {
                return fmt.Errorf("更新会话失败: %w", result.Error)
        }
        if result.RowsAffected == 0 {
                return fmt.Errorf("会话 %s 不存在", sessionID)
        }

        return nil
}

// LoadSession 從數據庫加載指定會話
// 傳入空 sessionID 時回退到載入最新會話（向後兼容）
func (m *SessionPersistManager) LoadSession(sessionID string) (*SavedSession, error) {
        var rows []SessionHistories
        query := globalDB.Order("updated_at DESC").Limit(1)
        if sessionID != "" {
                query = query.Where("id = ?", sessionID)
        }
        query.Find(&rows)
        if len(rows) > 0 {
                return dbRowToSavedSession(&rows[0])
        }

        // 未找到記錄（首次運行屬於正常情況）
        return nil, nil
}

// dbRowToSavedSession 将数据库行转换为 SavedSession
func dbRowToSavedSession(row *SessionHistories) (*SavedSession, error) {
        var history []Message
        if row.HistoryJSON != "" {
                if err := json.Unmarshal([]byte(row.HistoryJSON), &history); err != nil {
                        return nil, fmt.Errorf("解析会话历史失败: %w", err)
                }
        }

        return &SavedSession{
                ID:          row.ID,
                Description: row.Description,
                CreatedAt:   row.CreatedAt,
                UpdatedAt:   row.UpdatedAt,
                History:     history,
                Role:        row.Role,
                Actor:       row.Actor,
        }, nil
}

// ListSessions 列出数据库中所有保存的会话
func (m *SessionPersistManager) ListSessions() ([]SavedSession, error) {
        var rows []SessionHistories
        if result := globalDB.Order("updated_at DESC").Find(&rows); result.Error != nil {
                return nil, fmt.Errorf("查询会话列表失败: %w", result.Error)
        }

        sessions := make([]SavedSession, 0, len(rows))
        for i := range rows {
                saved, err := dbRowToSavedSession(&rows[i])
                if err != nil {
                        log.Printf("[SessionPersist] 跳过损坏的会话 %s: %v", rows[i].ID, err)
                        continue
                }
                sessions = append(sessions, *saved)
        }

        // 已按 updated_at DESC 排序，无需再次排序
        sort.Slice(sessions, func(i, j int) bool {
                return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
        })

        return sessions, nil
}

// DeleteSession 从数据库删除会话
func (m *SessionPersistManager) DeleteSession(sessionID string) error {
        // 精确匹配
        result := globalDB.Where("id = ?", sessionID).Delete(&SessionHistories{})
        if result.Error != nil {
                return fmt.Errorf("删除会话失败: %w", result.Error)
        }

        // 模糊匹配：如果精确匹配没有删除，尝试前缀匹配
        if result.RowsAffected == 0 {
                result = globalDB.Where("id LIKE ?", sessionID+"%").Delete(&SessionHistories{})
                if result.Error != nil {
                        return fmt.Errorf("删除会话失败: %w", result.Error)
                }
        }

        return nil
}

// ExportSession 导出会话到 TOON 文件（供备份/迁移使用）
func (m *SessionPersistManager) ExportSession(sessionID string, exportPath string) error {
        saved, err := m.LoadSession(sessionID)
        if err != nil {
                return err
        }
        if saved == nil {
                return fmt.Errorf("会话 %s 不存在", sessionID)
        }

        // 如果是相对路径，使用程序自身目录
        if !filepath.IsAbs(exportPath) {
                exportPath = filepath.Join(globalExecDir, exportPath)
        }

        // 确保文件扩展名是 .toon
        if !strings.HasSuffix(exportPath, ".toon") {
                exportPath += ".toon"
        }

        sf := SessionFile{Session: saved.ToEntry()}
        data, err := toon.Marshal(sf)
        if err != nil {
                return fmt.Errorf("序列化会话失败: %w", err)
        }

        return os.WriteFile(exportPath, data, 0644)
}

// ImportSession 从 TOON 文件导入会话到数据库
func (m *SessionPersistManager) ImportSession(importPath string) (*SavedSession, error) {
        // 如果是相对路径，使用程序自身目录
        if !filepath.IsAbs(importPath) {
                importPath = filepath.Join(globalExecDir, importPath)
        }

        data, err := os.ReadFile(importPath)
        if err != nil {
                return nil, fmt.Errorf("读取导入文件失败: %w", err)
        }

        // TOON 格式解析
        var sf SessionFile
        if err := toon.Unmarshal(data, &sf); err != nil {
                return nil, fmt.Errorf("解析导入文件失败: %w", err)
        }

        // 转换为内存格式
        session := sf.Session.ToSession()

        // 生成新的 ID
        now := time.Now()
        session.ID = fmt.Sprintf("imported_%s_%s", now.Format("20060102_150405"), session.ID)
        session.CreatedAt = now
        session.UpdatedAt = now

        // 序列化消息历史为 JSON
        historyJSON, err := json.Marshal(session.History)
        if err != nil {
                return nil, fmt.Errorf("序列化导入会话历史失败: %w", err)
        }

        // 写入数据库
        row := SessionHistories{
                ID:          session.ID,
                Description: session.Description,
                Role:        session.Role,
                Actor:       session.Actor,
                HistoryJSON: string(historyJSON),
                CreatedAt:   now,
                UpdatedAt:   now,
        }
        if result := globalDB.Create(&row); result.Error != nil {
                return nil, fmt.Errorf("保存导入会话到数据库失败: %w", result.Error)
        }

        return &session, nil
}

// InitSessionPersist 初始化会话持久化管理器（基于数据库）
func InitSessionPersist() {
        globalSessionPersist = NewSessionPersistManager()
}

// ============================================================
// 自動備份與重建機制
// 數據庫損壞時從 .toon 備份文件自動恢復會話數據
// ============================================================

var (
        lastBackupTime      time.Time
        lastBackupFingerprint string // 上次備份的內容指紋（消息數+末條時間戳），用於判斷內容是否變化
        lastBackupMu        sync.Mutex
)

const backupThrottleInterval = 2 * time.Minute // 備份節流：至少間隔 2 分鐘
const maxBackupFiles = 5                         // 最多保留 5 個備份文件

// computeBackupFingerprint 計算當前消息歷史的指紋。
// 格式：「消息數量:最後一條消息的時間戳:末尾內容 SHA256 前 8 字節」
// 如果歷史為空，返回空字符串。
func computeBackupFingerprint(history []Message) string {
        if len(history) == 0 {
                return ""
        }
        last := history[len(history)-1]
        // 對最後一條消息做 JSON 序列化後哈希，使 thinking block / 內容變更可被檢測
        lastJSON, _ := json.Marshal(last)
        hash := sha256.Sum256(lastJSON)
        return fmt.Sprintf("%d:%d:%x", len(history), last.Timestamp, hash[:4])
}

// BackupSessionToFile 將當前會話備份到 .toon 文件。
// 雙條件觸發：(1) 消息內容有變化 (2) 距上次備份至少 2 分鐘。
// 內容無變化時跳過寫入，避免產生冗餘備份文件。
func BackupSessionToFile(session *GlobalSession) {
        history := session.GetHistory()
        if len(history) == 0 {
                return
        }

        // 計算當前內容指紋
        currentFingerprint := computeBackupFingerprint(history)

        lastBackupMu.Lock()
        // 條件1：內容是否有變化？
        contentChanged := currentFingerprint != lastBackupFingerprint
        // 條件2：時間間隔是否超過 2 分鐘？
        timeElapsed := time.Since(lastBackupTime) >= backupThrottleInterval

        if !contentChanged {
                lastBackupMu.Unlock()
                return // 內容無變化，不需要寫入
        }
        if !timeElapsed {
                lastBackupMu.Unlock()
                return // 內容有變化但尚未到 2 分鐘間隔，跳過
        }

        // 雙條件滿足：記錄本次備份的指紋和時間
        lastBackupFingerprint = currentFingerprint
        lastBackupTime = time.Now()
        lastBackupMu.Unlock()

        log.Printf("[Backup] Triggering backup: content changed (fingerprint=%s) and throttle interval met", currentFingerprint)

        // 構建描述
        description := "auto_backup"
        for _, msg := range history {
                if msg.Role == "user" {
                        if content, ok := msg.Content.(string); ok && content != "" {
                                if len(content) > 50 {
                                        description = content[:50] + "..."
                                } else {
                                        description = content
                                }
                                break
                        }
                }
        }

        saved := &SavedSession{
                ID:          session.ID,
                Description: description,
                CreatedAt:   session.CreatedAt,
                UpdatedAt:   time.Now(),
                History:     history,
        }

        sf := SessionFile{Session: saved.ToEntry()}
        data, err := toon.Marshal(sf)
        if err != nil {
                log.Printf("[Backup] Failed to marshal session for backup: %v", err)
                return
        }

        // 創建備份目錄
        backupDir := filepath.Join(globalExecDir, "data", "backups")
        if err := os.MkdirAll(backupDir, 0755); err != nil {
                log.Printf("[Backup] Failed to create backup directory: %v", err)
                return
        }

        // 寫入帶時間戳的備份文件
        timestamp := time.Now().Format("20060102_150405")
        backupPath := filepath.Join(backupDir, fmt.Sprintf("session_%s.toon", timestamp))

        if err := os.WriteFile(backupPath, data, 0644); err != nil {
                log.Printf("[Backup] Failed to write backup file: %v", err)
                return
        }

        // 清理過期備份
        cleanupOldBackups(backupDir)

        log.Printf("[Backup] Session backed up to %s (%d messages)", backupPath, len(history))
}

// cleanupOldBackups 清理過多的備份文件，只保留最近的 maxBackupFiles 個
func cleanupOldBackups(backupDir string) {
        entries, err := os.ReadDir(backupDir)
        if err != nil {
                return
        }

        var toonFiles []os.DirEntry
        for _, e := range entries {
                if !e.IsDir() && strings.HasPrefix(e.Name(), "session_") && strings.HasSuffix(e.Name(), ".toon") {
                        toonFiles = append(toonFiles, e)
                }
        }

        if len(toonFiles) <= maxBackupFiles {
                return
        }

        // 按文件名排序（包含時間戳，名稱越大越新）
        sort.Slice(toonFiles, func(i, j int) bool {
                return toonFiles[i].Name() < toonFiles[j].Name()
        })

        // 刪除最舊的文件
        removed := len(toonFiles) - maxBackupFiles
        for i := 0; i < removed; i++ {
                removePath := filepath.Join(backupDir, toonFiles[i].Name())
                if err := os.Remove(removePath); err != nil {
                        log.Printf("[Backup] Failed to remove old backup %s: %v", toonFiles[i].Name(), err)
                } else {
                        log.Printf("[Backup] Removed old backup: %s", toonFiles[i].Name())
                }
        }
}

// RebuildFromBackups 從備份 .toon 文件重建會話數據到數據庫。
// 用於數據庫損壞後啟動時的自動恢復。返回是否成功重建。
func RebuildFromBackups() bool {
        if globalDB == nil {
                return false
        }

        // 先檢查數據庫中是否已有會話記錄
        var count int64
        globalDB.Model(&SessionHistories{}).Count(&count)
        if count > 0 {
                return false // 數據庫中已有數據，無需重建
        }

        backupDir := filepath.Join(globalExecDir, "data", "backups")
        entries, err := os.ReadDir(backupDir)
        if err != nil {
                return false // 備份目錄不存在，首次運行屬於正常
        }

        // 收集所有 session_*.toon 備份文件
        var toonFiles []string
        for _, e := range entries {
                if !e.IsDir() && strings.HasPrefix(e.Name(), "session_") && strings.HasSuffix(e.Name(), ".toon") {
                        toonFiles = append(toonFiles, e.Name())
                }
        }

        if len(toonFiles) == 0 {
                return false // 沒有備份文件
        }

        // 按文件名降序排列，優先嘗試最新的備份
        sort.Sort(sort.Reverse(sort.StringSlice(toonFiles)))

        for _, fname := range toonFiles {
                fpath := filepath.Join(backupDir, fname)
                data, err := os.ReadFile(fpath)
                if err != nil {
                        log.Printf("[Backup-Rebuild] Failed to read backup %s: %v", fname, err)
                        continue
                }

                var sf SessionFile
                if err := toon.Unmarshal(data, &sf); err != nil {
                        log.Printf("[Backup-Rebuild] Failed to parse backup %s: %v", fname, err)
                        continue
                }

                session := sf.Session.ToSession()
                if len(session.History) == 0 {
                        log.Printf("[Backup-Rebuild] Backup %s has empty history, skipping", fname)
                        continue
                }

                historyJSON, err := json.Marshal(session.History)
                if err != nil {
                        log.Printf("[Backup-Rebuild] Failed to marshal history from backup %s: %v", fname, err)
                        continue
                }

                row := SessionHistories{
                        ID:          session.ID,
                        Description: session.Description,
                        Role:        session.Role,
                        Actor:       session.Actor,
                        HistoryJSON: string(historyJSON),
                        CreatedAt:   session.CreatedAt,
                        UpdatedAt:   session.UpdatedAt,
                }

                if result := globalDB.Create(&row); result.Error != nil {
                        log.Printf("[Backup-Rebuild] Failed to import backup %s to DB: %v", fname, result.Error)
                        continue
                }

                log.Printf("[Backup-Rebuild] Successfully rebuilt session from backup: %s (%d messages, created: %s)",
                        fname, len(session.History), session.CreatedAt.Format(time.RFC3339))
                return true
        }

        log.Printf("[Backup-Rebuild] All %d backup files failed to import", len(toonFiles))
        return false
}
