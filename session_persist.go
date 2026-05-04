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
        "gorm.io/gorm"
)

// SavedSession 保存的会话数据结构（内存中使用）
type SavedSession struct {
        ID           string
        Description  string
        CreatedAt    time.Time
        UpdatedAt    time.Time
        History      []Message
        Role         string
        Actor        string
        InputTokens  int
        OutputTokens int
        TotalTokens  int
        TurnCount    int
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
// v2：兩表設計 — session_histories（元數據）+ session_messages（逐條消息）
// ============================================================

// SessionPersistManager 会话持久化管理器
type SessionPersistManager struct {
        appendMu sync.Mutex // 保護 AppendMessage 的 SELECT+INSERT 原子性
}

// NewSessionPersistManager 创建会话持久化管理器（基于数据库）
func NewSessionPersistManager() *SessionPersistManager {
        if globalDB == nil {
                log.Println("[SessionPersist] Warning: globalDB is nil, session persistence will not work")
        }
        return &SessionPersistManager{}
}

// messageToRow 将 Message 转换为 SessionMessage 行
func messageToRow(msg Message, sessionID string, seq int) SessionMessage {
        row := SessionMessage{
                SessionID:         sessionID,
                Seq:               seq,
                Role:              msg.Role,
                ToolCallID:        msg.ToolCallID,
                ThinkingSignature: msg.ThinkingSignature,
                Timestamp:         msg.Timestamp,
                CreatedAt:         time.Now(),
        }

        if msg.Content != nil {
                if str, ok := msg.Content.(string); ok {
                        row.Content = str
                } else {
                        if data, err := json.Marshal(msg.Content); err == nil {
                                row.ContentJSON = string(data)
                        }
                }
        }

        if msg.ToolCalls != nil {
                if data, err := json.Marshal(msg.ToolCalls); err == nil {
                        row.ToolCalls = string(data)
                }
        }

        if msg.ReasoningContent != nil {
                if str, ok := msg.ReasoningContent.(string); ok {
                        row.ReasoningContent = str
                } else {
                        if data, err := json.Marshal(msg.ReasoningContent); err == nil {
                                row.ReasoningContent = string(data)
                        }
                }
        }

        // 估算 token 數量（用於 token 模式滑窗）
        row.TokenCount = estimateMessageTokens(row)
        return row
}

// estimateMessageTokens 估算一條 SessionMessage 嘅 token 數量
func estimateMessageTokens(row SessionMessage) int {
        total := 0
        if row.Content != "" {
                total += ImprovedEstimateTokens(row.Content)
        }
        if row.ContentJSON != "" {
                total += ImprovedEstimateTokens(row.ContentJSON)
        }
        if row.ToolCalls != "" {
                total += ImprovedEstimateTokens(row.ToolCalls)
        }
        if row.ReasoningContent != "" {
                total += ImprovedEstimateTokens(row.ReasoningContent)
        }
        // 至少 1 token（避免空消息被完全忽略）
        if total == 0 {
                return 1
        }
        return total
}

// rowToMessage 将 SessionMessage 行转换为 Message
func rowToMessage(row SessionMessage) Message {
        m := Message{
                Role:              row.Role,
                ToolCallID:        row.ToolCallID,
                ThinkingSignature: row.ThinkingSignature,
                Timestamp:         row.Timestamp,
        }

        if row.Content != "" {
                m.Content = row.Content
        } else if row.ContentJSON != "" {
                var content interface{}
                if err := json.Unmarshal([]byte(row.ContentJSON), &content); err == nil {
                        m.Content = content
                }
        }

        if row.ReasoningContent != "" {
                if strings.HasPrefix(row.ReasoningContent, "{") || strings.HasPrefix(row.ReasoningContent, "[") {
                        var rc interface{}
                        if err := json.Unmarshal([]byte(row.ReasoningContent), &rc); err == nil {
                                m.ReasoningContent = rc
                        } else {
                                m.ReasoningContent = row.ReasoningContent
                        }
                } else {
                        m.ReasoningContent = row.ReasoningContent
                }
        }

        if row.ToolCalls != "" {
                var toolCalls interface{}
                if err := json.Unmarshal([]byte(row.ToolCalls), &toolCalls); err == nil {
                        m.ToolCalls = toolCalls
                }
        }

        return m
}

// SaveSession 保存会话元数据到 session_histories + 批量写入消息到 session_messages
func (m *SessionPersistManager) SaveSession(sessionID, description, role, actor string, inputTokens, outputTokens, totalTokens, turnCount int, messages []Message) (*SavedSession, error) {
        now := time.Now()

        // 1. 保存会话元数据
        sess := SessionHistory{
                ID:           sessionID,
                Description:  description,
                Role:         role,
                Actor:        actor,
                CreatedAt:    now,
                UpdatedAt:    now,
                InputTokens:  inputTokens,
                OutputTokens: outputTokens,
                TotalTokens:  totalTokens,
                TurnCount:    turnCount,
        }
        if result := globalDB.Save(&sess); result.Error != nil {
                return nil, fmt.Errorf("保存会话元数据失败: %w", result.Error)
        }

        // 2. 批量替换消息
        if err := m.SaveMessages(sessionID, messages); err != nil {
                return nil, err
        }

        return &SavedSession{
                ID:           sessionID,
                Description:  description,
                CreatedAt:    now,
                UpdatedAt:    now,
                History:      messages,
                Role:         role,
                Actor:        actor,
                InputTokens:  inputTokens,
                OutputTokens: outputTokens,
                TotalTokens:  totalTokens,
                TurnCount:    turnCount,
        }, nil
}

// SaveMessages 批量保存消息 — 先刪舊消息再插入新消息（事務包裹）
func (m *SessionPersistManager) SaveMessages(sessionID string, messages []Message) error {
        return globalDB.Transaction(func(tx *gorm.DB) error {
                // 刪除舊消息
                if err := tx.Where("session_id = ?", sessionID).Delete(&SessionMessage{}).Error; err != nil {
                        return fmt.Errorf("删除旧消息失败: %w", err)
                }

                // 批量插入新消息
                for i, msg := range messages {
                        row := messageToRow(msg, sessionID, i)
                        if err := tx.Create(&row).Error; err != nil {
                                return fmt.Errorf("插入消息失败 (seq=%d): %w", i, err)
                        }
                }
                return nil
        })
}

// UpdateSession 更新会话元数据和消息
func (m *SessionPersistManager) UpdateSession(sessionID string, messages []Message) error {
        session := GetGlobalSession()
        now := time.Now()

        updates := map[string]interface{}{
                "updated_at": now,
        }
        if tracker := session.GetTracker(); tracker != nil {
                stats := tracker.GetStats()
                updates["input_tokens"] = stats.InputTokens
                updates["output_tokens"] = stats.OutputTokens
                updates["total_tokens"] = stats.TotalTokens
                updates["turn_count"] = stats.TurnCount
        }

        result := globalDB.Model(&SessionHistory{}).
                Where("id = ?", sessionID).
                Updates(updates)
        if result.Error != nil {
                return fmt.Errorf("更新会话元数据失败: %w", result.Error)
        }

        // 替換消息
        if err := m.SaveMessages(sessionID, messages); err != nil {
                return err
        }

        return nil
}

// UpdateSessionMeta 只更新会话元数据（token stats、description），唔改消息
// 用於 autoSaveHistory：內存滑窗數據唔寫入 DB，DB 只保留 AddToHistory 寫入嘅原始消息
func (m *SessionPersistManager) UpdateSessionMeta(sessionID, description, role, actor string, inputTokens, outputTokens, totalTokens, turnCount int) error {
        updates := map[string]interface{}{
                "updated_at":    time.Now(),
                "description":   description,
                "role":          role,
                "actor":         actor,
                "input_tokens":  inputTokens,
                "output_tokens": outputTokens,
                "total_tokens":  totalTokens,
                "turn_count":    turnCount,
        }
        result := globalDB.Model(&SessionHistory{}).
                Where("id = ?", sessionID).
                Updates(updates)
        if result.Error != nil {
                return fmt.Errorf("更新会话元数据失败: %w", result.Error)
        }
        return nil
}

// LoadSession 從新兩表加載會話元數據和消息
func (m *SessionPersistManager) LoadSession(sessionID string) (*SavedSession, error) {
        // 1. 加載會話元數據
        var sess SessionHistory
        query := globalDB.Order("updated_at DESC").Limit(1)
        if sessionID != "" {
                query = query.Where("id = ?", sessionID)
        }
        if result := query.First(&sess); result.Error != nil {
                if result.Error == gorm.ErrRecordNotFound {
                        return nil, nil // 首次運行，正常
                }
                return nil, fmt.Errorf("查询会话失败: %w", result.Error)
        }

        // 2. 加載消息（按 seq 排序）
        var rows []SessionMessage
        if err := globalDB.Where("session_id = ?", sess.ID).
                Order("seq ASC").
                Find(&rows).Error; err != nil {
                return nil, fmt.Errorf("查询会话消息失败: %w", err)
        }

        messages := make([]Message, 0, len(rows))
        for i := range rows {
                messages = append(messages, rowToMessage(rows[i]))
        }

        return &SavedSession{
                ID:           sess.ID,
                Description:  sess.Description,
                CreatedAt:    sess.CreatedAt,
                UpdatedAt:    sess.UpdatedAt,
                History:      messages,
                Role:         sess.Role,
                Actor:        sess.Actor,
                InputTokens:  sess.InputTokens,
                OutputTokens: sess.OutputTokens,
                TotalTokens:  sess.TotalTokens,
                TurnCount:    sess.TurnCount,
        }, nil
}

// ListSessions 列出所有保存的会话（不含消息内容）
func (m *SessionPersistManager) ListSessions() ([]SavedSession, error) {
        var rows []SessionHistory
        if result := globalDB.Order("updated_at DESC").Find(&rows); result.Error != nil {
                return nil, fmt.Errorf("查询会话列表失败: %w", result.Error)
        }

        sessions := make([]SavedSession, 0, len(rows))
        for i := range rows {
                sessions = append(sessions, SavedSession{
                        ID:           rows[i].ID,
                        Description:  rows[i].Description,
                        CreatedAt:    rows[i].CreatedAt,
                        UpdatedAt:    rows[i].UpdatedAt,
                        Role:         rows[i].Role,
                        Actor:        rows[i].Actor,
                        InputTokens:  rows[i].InputTokens,
                        OutputTokens: rows[i].OutputTokens,
                        TotalTokens:  rows[i].TotalTokens,
                        TurnCount:    rows[i].TurnCount,
                })
        }

        return sessions, nil
}

// DeleteSession 删除会话及其所有消息
func (m *SessionPersistManager) DeleteSession(sessionID string) error {
        return globalDB.Transaction(func(tx *gorm.DB) error {
                // 先刪消息（FK CASCADE 可能唔生效，手動刪）
                if err := tx.Where("session_id = ?", sessionID).Delete(&SessionMessage{}).Error; err != nil {
                        return fmt.Errorf("删除会话消息失败: %w", err)
                }
                // 再刪會話
                result := tx.Where("id = ?", sessionID).Delete(&SessionHistory{})
                if result.Error != nil {
                        return fmt.Errorf("删除会话失败: %w", result.Error)
                }
                if result.RowsAffected == 0 {
                        // 精確匹配冇結果，嘗試前綴匹配（向後兼容）
                        var sess SessionHistory
                        if err := tx.Where("id LIKE ?", sessionID+"%").First(&sess).Error; err == nil {
                                tx.Where("session_id = ?", sess.ID).Delete(&SessionMessage{})
                                tx.Where("id = ?", sess.ID).Delete(&SessionHistory{})
                        }
                }
                return nil
        })
}

// AppendMessage 追加單條消息到 session_messages
// 使用 mutex + 事務確保 SELECT MAX(seq) + INSERT 原子性
func (m *SessionPersistManager) AppendMessage(sessionID string, msg Message) error {
        m.appendMu.Lock()
        defer m.appendMu.Unlock()

        return globalDB.Transaction(func(tx *gorm.DB) error {
                var maxSeq int
                tx.Model(&SessionMessage{}).
                        Where("session_id = ?", sessionID).
                        Select("COALESCE(MAX(seq), -1)").
                        Scan(&maxSeq)

                row := messageToRow(msg, sessionID, maxSeq+1)
                return tx.Create(&row).Error
        })
}

// LoadRecentMessages 加載會話中最近 N 條消息（俾自學習系統用）
func (m *SessionPersistManager) LoadRecentMessages(sessionID string, limit int) ([]Message, error) {
        var rows []SessionMessage
        if err := globalDB.Where("session_id = ?", sessionID).
                Order("seq DESC").
                Limit(limit).
                Find(&rows).Error; err != nil {
                return nil, fmt.Errorf("查询最近消息失败: %w", err)
        }

        // 因為係 DESC 排序，要反轉返 seq 順序
        messages := make([]Message, 0, len(rows))
        for i := len(rows) - 1; i >= 0; i-- {
                messages = append(messages, rowToMessage(rows[i]))
        }
        return messages, nil
}

// LoadSessionWindow 加載會話元數據 + 最近 N 條消息（滑窗模式）
// 用於啟動時按滑窗限制載入，避免載入全部歷史
func (m *SessionPersistManager) LoadSessionWindow(sessionID string, limit int) (*SavedSession, error) {
        // 1. 加載會話元數據
        var sess SessionHistory
        query := globalDB.Order("updated_at DESC").Limit(1)
        if sessionID != "" {
                query = query.Where("id = ?", sessionID)
        }
        if result := query.First(&sess); result.Error != nil {
                if result.Error == gorm.ErrRecordNotFound {
                        return nil, nil
                }
                return nil, fmt.Errorf("查询会话失败: %w", result.Error)
        }

        // 2. 加載最近 N 條消息
        messages, err := m.LoadRecentMessages(sess.ID, limit)
        if err != nil {
                return nil, err
        }

        return &SavedSession{
                ID:           sess.ID,
                Description:  sess.Description,
                CreatedAt:    sess.CreatedAt,
                UpdatedAt:    sess.UpdatedAt,
                History:      messages,
                Role:         sess.Role,
                Actor:        sess.Actor,
                InputTokens:  sess.InputTokens,
                OutputTokens: sess.OutputTokens,
                TotalTokens:  sess.TotalTokens,
                TurnCount:    sess.TurnCount,
        }, nil
}

// LoadSessionWindowByTokens 用 token 數量滑窗載入會話
// 從最新消息開始累加 TokenCount，超過 maxTokens 就停止，返還未超過嘅最近消息
func (m *SessionPersistManager) LoadSessionWindowByTokens(sessionID string, maxTokens int) (*SavedSession, error) {
        // 1. 加載會話元數據
        var sess SessionHistory
        query := globalDB.Order("updated_at DESC").Limit(1)
        if sessionID != "" {
                query = query.Where("id = ?", sessionID)
        }
        if result := query.First(&sess); result.Error != nil {
                if result.Error == gorm.ErrRecordNotFound {
                        return nil, nil
                }
                return nil, fmt.Errorf("查询会话失败: %w", result.Error)
        }

        // 2. 加載消息（從最新到最舊），累加 token 直到超過上限
        var rows []SessionMessage
        if err := globalDB.Where("session_id = ?", sess.ID).
                Order("seq DESC").
                Find(&rows).Error; err != nil {
                return nil, fmt.Errorf("查询会话消息失败: %w", err)
        }

        // 3. 從最新開始累加 token，超過 maxTokens 就停
        accumulated := 0
        cutoff := 0
        for i, row := range rows {
                accumulated += row.TokenCount
                if accumulated > maxTokens && i > 0 {
                        // 超過上限，退後到上一條（唔包當前呢條）
                        cutoff = i
                        break
                }
        }

        // 取 cutoff 之前嘅消息（最新嗰批），反轉返 seq 順序
        var kept []SessionMessage
        if cutoff > 0 {
                kept = rows[:cutoff]
        } else {
                kept = rows
        }

        messages := make([]Message, 0, len(kept))
        for i := len(kept) - 1; i >= 0; i-- {
                messages = append(messages, rowToMessage(kept[i]))
        }

        return &SavedSession{
                ID:           sess.ID,
                Description:  sess.Description,
                CreatedAt:    sess.CreatedAt,
                UpdatedAt:    sess.UpdatedAt,
                History:      messages,
                Role:         sess.Role,
                Actor:        sess.Actor,
                InputTokens:  sess.InputTokens,
                OutputTokens: sess.OutputTokens,
                TotalTokens:  sess.TotalTokens,
                TurnCount:    sess.TurnCount,
        }, nil
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

        // 獲取當前 role / actor
        var currentRole, currentActor string
        if globalStage != nil {
                currentActor = globalStage.GetCurrentActor()
        }
        if globalActorManager != nil && currentActor != "" {
                if actor, ok := globalActorManager.GetActor(currentActor); ok {
                        currentRole = actor.Role
                }
        }
        if session.Role == "" {
                session.Role = currentRole
        }
        if session.Actor == "" {
                session.Actor = currentActor
        }

        // 使用新兩表保存
        saved, err := m.SaveSession(session.ID, session.Description, session.Role, session.Actor, 0, 0, 0, 0, session.History)
        if err != nil {
                return nil, fmt.Errorf("保存导入会话到数据库失败: %w", err)
        }

        return saved, nil
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
        backupDir := filepath.Join(globalDataDir, "backups")
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
        globalDB.Model(&SessionHistory{}).Count(&count)
        if count > 0 {
                return false // 數據庫中已有數據，無需重建
        }

        backupDir := filepath.Join(globalDataDir, "backups")
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

                // 使用新兩表保存
                sessRow := SessionHistory{
                        ID:          session.ID,
                        Description: session.Description,
                        Role:        session.Role,
                        Actor:       session.Actor,
                        CreatedAt:   session.CreatedAt,
                        UpdatedAt:   session.UpdatedAt,
                }
                if result := globalDB.Create(&sessRow); result.Error != nil {
                        log.Printf("[Backup-Rebuild] Failed to import backup session %s to DB: %v", fname, result.Error)
                        continue
                }

                // 批量插入消息
                for i, msg := range session.History {
                        row := messageToRow(msg, session.ID, i)
                        if err := globalDB.Create(&row).Error; err != nil {
                                log.Printf("[Backup-Rebuild] Failed to import message seq=%d: %v", i, err)
                        }
                }

                log.Printf("[Backup-Rebuild] Successfully rebuilt session from backup: %s (%d messages, created: %s)",
                        fname, len(session.History), session.CreatedAt.Format(time.RFC3339))
                return true
        }

        log.Printf("[Backup-Rebuild] All %d backup files failed to import", len(toonFiles))
        return false
}
