package main

import (
        "encoding/json"
        "fmt"
        "log"
        "os"
        "path/filepath"
        "sort"
        "strings"
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

        return me
}

// entryToMessage 将 MessageEntry 转换回 Message
func entryToMessage(me MessageEntry) Message {
        m := Message{
                Role:             me.Role,
                ToolCallID:       me.ToolCallID,
                ReasoningContent: me.ReasoningContent,
        }

        if me.Content != "" {
                m.Content = me.Content
        } else if me.ContentJSON != "" {
                var content interface{}
                if err := json.Unmarshal([]byte(me.ContentJSON), &content); err == nil {
                        m.Content = content
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

        // 生成唯一 ID（使用时间戳 + sessionID）
        persistID := fmt.Sprintf("%s_%s", now.Format("20060102_150405"), sessionID)

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
        }

        if result := globalDB.Create(&row); result.Error != nil {
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

        result := globalDB.Model(&SessionHistories{}).
                Where("id = ?", sessionID).
                Updates(map[string]interface{}{
                        "history_json": string(historyJSON),
                        "updated_at":   time.Now(),
                })
        if result.Error != nil {
                return fmt.Errorf("更新会话失败: %w", result.Error)
        }
        if result.RowsAffected == 0 {
                return fmt.Errorf("会话 %s 不存在", sessionID)
        }

        return nil
}

// LoadSession 从数据库加载会话
func (m *SessionPersistManager) LoadSession(sessionID string) (*SavedSession, error) {
        // 模糊匹配：以 sessionID 为后缀查找（因为 SaveSession 的 ID 格式为 "timestamp_sessionID"）
        var rows []SessionHistories
        globalDB.Where("id LIKE ?", "%"+sessionID).Order("updated_at DESC").Limit(1).Find(&rows)
        if len(rows) > 0 {
                return dbRowToSavedSession(&rows[0])
        }

        // 兜底：精确匹配
        var row SessionHistories
        result := globalDB.Where("id = ?", sessionID).First(&row)
        if result.Error == nil {
                return dbRowToSavedSession(&row)
        }

        // 未找到记录（首次运行属于正常情况）
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

        // 如果是相对路径，使用当前工作目录
        if !filepath.IsAbs(exportPath) {
                wd, _ := os.Getwd()
                exportPath = filepath.Join(wd, exportPath)
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
        // 如果是相对路径，使用当前工作目录
        if !filepath.IsAbs(importPath) {
                wd, _ := os.Getwd()
                importPath = filepath.Join(wd, importPath)
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
