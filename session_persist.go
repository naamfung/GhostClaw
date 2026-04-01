package main

import (
	"encoding/json"
	"fmt"
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

// SessionEntry TOON 兼容的会话条目（用于序列化）
type SessionEntry struct {
	ID          string        `toon:"id"`
	Description string        `toon:"description"`
	CreatedAt   string        `toon:"created_at"`
	UpdatedAt   string        `toon:"updated_at"`
	History     []MessageEntry `toon:"history"`
	Role        string        `toon:"role,omitempty"`
	Actor       string        `toon:"actor,omitempty"`
}

// MessageEntry TOON 兼容的消息条目（用于序列化）
// 将 interface{} 字段转换为 JSON 字符串
type MessageEntry struct {
	Role             string `toon:"role"`
	Content          string `toon:"content,omitempty"`
	ContentJSON      string `toon:"content_json,omitempty"`
	ToolCalls        string `toon:"tool_calls,omitempty"`
	ToolCallID       string `toon:"tool_call_id,omitempty"`
	ReasoningContent string `toon:"reasoning_content,omitempty"`
}

// ToEntry 将 SavedSession 转换为 TOON 兼容的 SessionEntry
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

// ToSession 将 SessionEntry 转换回 SavedSession
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

	// 处理 Content
	if m.Content != nil {
		if str, ok := m.Content.(string); ok {
			me.Content = str
		} else {
			// 复杂类型序列化为 JSON
			if data, err := json.Marshal(m.Content); err == nil {
				me.ContentJSON = string(data)
			}
		}
	}

	// 处理 ToolCalls
	if m.ToolCalls != nil {
		if data, err := json.Marshal(m.ToolCalls); err == nil {
			me.ToolCalls = string(data)
		}
	}

	// 处理 ReasoningContent
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

	// 处理 Content
	if me.Content != "" {
		m.Content = me.Content
	} else if me.ContentJSON != "" {
		var content interface{}
		if err := json.Unmarshal([]byte(me.ContentJSON), &content); err == nil {
			m.Content = content
		}
	}

	// 处理 ToolCalls
	if me.ToolCalls != "" {
		var toolCalls interface{}
		if err := json.Unmarshal([]byte(me.ToolCalls), &toolCalls); err == nil {
			m.ToolCalls = toolCalls
		}
	}

	return m
}

// SessionFile TOON 文件结构（顶层包装）
type SessionFile struct {
	Session SessionEntry `toon:"session"`
}

// SessionPersistManager 会话持久化管理器
type SessionPersistManager struct {
	dataDir string
}

// NewSessionPersistManager 创建会话持久化管理器
func NewSessionPersistManager() *SessionPersistManager {
	// 获取可执行文件所在目录
	execPath, err := os.Executable()
	if err != nil {
		execPath = "."
	}
	execDir := filepath.Dir(execPath)
	dataDir := filepath.Join(execDir, "sessions")

	// 确保目录存在
	os.MkdirAll(dataDir, 0755)

	return &SessionPersistManager{dataDir: dataDir}
}

// SaveSession 保存会话
func (m *SessionPersistManager) SaveSession(sessionID string, history []Message, description string) (*SavedSession, error) {
	now := time.Now()

	// 生成会话文件名（使用时间戳）
	fileName := fmt.Sprintf("%s_%s.session.toon", now.Format("20060102_150405"), sessionID)
	filePath := filepath.Join(m.dataDir, fileName)

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

	saved := &SavedSession{
		ID:          fileName[:len(fileName)-12], // 移除 .session.toon 后缀
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
		History:     history,
		Role:        currentRole,
		Actor:       currentActor,
	}

	// 使用 TOON 格式保存
	sf := SessionFile{Session: saved.ToEntry()}
	data, err := toon.Marshal(sf)
	if err != nil {
		return nil, fmt.Errorf("序列化会话失败: %w", err)
	}

	// 写入临时文件再重命名，保证原子性
	tmp := filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return nil, fmt.Errorf("写入会话文件失败: %w", err)
	}

	if err := os.Rename(tmp, filePath); err != nil {
		return nil, fmt.Errorf("保存会话文件失败: %w", err)
	}

	return saved, nil
}

// UpdateSession 更新已保存的会话
func (m *SessionPersistManager) UpdateSession(sessionID string, history []Message) error {
	// 尝试加载现有会话
	saved, err := m.LoadSession(sessionID)
	if err != nil {
		return err
	}

	// 更新历史和时间
	saved.History = history
	saved.UpdatedAt = time.Now()

	filePath := m.getSessionFilePath(sessionID)
	sf := SessionFile{Session: saved.ToEntry()}
	data, err := toon.Marshal(sf)
	if err != nil {
		return fmt.Errorf("序列化会话失败: %w", err)
	}

	// 写入临时文件再重命名
	tmp := filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, filePath)
}

// getSessionFilePath 获取会话文件路径
func (m *SessionPersistManager) getSessionFilePath(sessionID string) string {
	// 尝试多种可能的文件名格式
	possibleNames := []string{
		sessionID + ".session.toon",
		sessionID + ".toon",
		sessionID,
	}

	for _, name := range possibleNames {
		path := filepath.Join(m.dataDir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// 默认返回第一种格式
	return filepath.Join(m.dataDir, sessionID+".session.toon")
}

// LoadSession 加载会话
func (m *SessionPersistManager) LoadSession(sessionID string) (*SavedSession, error) {
	filePath := m.getSessionFilePath(sessionID)

	data, err := os.ReadFile(filePath)
	if err != nil {
		// 尝试模糊匹配
		files, _ := os.ReadDir(m.dataDir)
		for _, f := range files {
			if strings.HasPrefix(f.Name(), sessionID) && strings.HasSuffix(f.Name(), ".toon") {
				filePath = filepath.Join(m.dataDir, f.Name())
				data, err = os.ReadFile(filePath)
				break
			}
		}
		if err != nil {
			return nil, fmt.Errorf("读取会话文件失败: %w", err)
		}
	}

	// TOON 格式解析
	var sf SessionFile
	if err := toon.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("解析会话文件失败: %w", err)
	}

	// 转换为内存格式
	session := sf.Session.ToSession()

	// 确保有 ID
	if session.ID == "" {
		session.ID = filepath.Base(filePath)
		if strings.HasSuffix(session.ID, ".session.toon") {
			session.ID = session.ID[:len(session.ID)-12]
		} else if strings.HasSuffix(session.ID, ".toon") {
			session.ID = session.ID[:len(session.ID)-5]
		}
	}

	return &session, nil
}

// ListSessions 列出所有保存的会话
func (m *SessionPersistManager) ListSessions() ([]SavedSession, error) {
	files, err := os.ReadDir(m.dataDir)
	if err != nil {
		return nil, fmt.Errorf("读取会话目录失败: %w", err)
	}

	var sessions []SavedSession
	for _, f := range files {
		// 只处理 .toon 文件
		if !strings.HasSuffix(f.Name(), ".toon") {
			continue
		}

		filePath := filepath.Join(m.dataDir, f.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		// TOON 格式解析
		var sf SessionFile
		if err := toon.Unmarshal(data, &sf); err != nil {
			continue
		}

		// 转换为内存格式
		session := sf.Session.ToSession()

		// 确保有 ID
		if session.ID == "" {
			session.ID = f.Name()
			if strings.HasSuffix(session.ID, ".session.toon") {
				session.ID = session.ID[:len(session.ID)-12]
			} else if strings.HasSuffix(session.ID, ".toon") {
				session.ID = session.ID[:len(session.ID)-5]
			}
		}

		sessions = append(sessions, session)
	}

	// 按更新时间倒序排序
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

// DeleteSession 删除会话
func (m *SessionPersistManager) DeleteSession(sessionID string) error {
	filePath := m.getSessionFilePath(sessionID)

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// 尝试模糊匹配
		files, _ := os.ReadDir(m.dataDir)
		for _, f := range files {
			if strings.HasPrefix(f.Name(), sessionID) && strings.HasSuffix(f.Name(), ".toon") {
				filePath = filepath.Join(m.dataDir, f.Name())
				break
			}
		}
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("删除会话文件失败: %w", err)
	}

	return nil
}

// ExportSession 导出会话到指定文件
func (m *SessionPersistManager) ExportSession(sessionID string, exportPath string) error {
	saved, err := m.LoadSession(sessionID)
	if err != nil {
		return err
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

// ImportSession 从文件导入会话
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

	// 保存到会话目录
	filePath := filepath.Join(m.dataDir, session.ID+".session.toon")
	sf = SessionFile{Session: session.ToEntry()}
	saveData, _ := toon.Marshal(sf)
	if err := os.WriteFile(filePath, saveData, 0644); err != nil {
		return nil, fmt.Errorf("保存导入会话失败: %w", err)
	}

	return &session, nil
}

// InitSessionPersist 初始化会话持久化管理器
func InitSessionPersist() {
	globalSessionPersist = NewSessionPersistManager()
}
