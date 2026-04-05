package main

import (
        "context"
        "errors"
        "log"
        "os"
        "sync"
        "time"

        "github.com/google/uuid"
)



// subscriber 输出广播订阅者
type subscriber struct {
        ch   chan StreamChunk
        done chan struct{}
}

// GlobalSession 全局唯一的会话，所有渠道共享
type GlobalSession struct {
        ID          string
        History     []Message
        CreatedAt   time.Time
        LastSeen    time.Time

        TaskRunning   bool
        currentTaskID string
        TaskCtx       context.Context
        TaskCancel    context.CancelFunc

        OutputQueue   chan StreamChunk       // 用于向后兼容
        subscribers   map[string]*subscriber // 广播订阅者列表
        subscribersMu sync.RWMutex          // subscribers 读写锁

        persistID string
        persistMu sync.Mutex

        Connected bool // 是否至少有一个 WebSocket 连接（仅用于 WS）

        mu sync.RWMutex
}

var globalSession *GlobalSession
var globalSessionOnce sync.Once

// GetGlobalSession 获取全局会话实例
func GetGlobalSession() *GlobalSession {
        globalSessionOnce.Do(func() {
                globalSession = newGlobalSession()
                if err := globalSession.LoadFromPersist(); err != nil && !errors.Is(err, os.ErrNotExist) {
                        log.Printf("Failed to load session: %v", err)
                }
        })
        return globalSession
}

func newGlobalSession() *GlobalSession {
        taskCtx, taskCancel := context.WithCancel(context.Background())
        return &GlobalSession{
                ID:          "default", // 可配置
                History:     make([]Message, 0),
                CreatedAt:   time.Now(),
                LastSeen:    time.Now(),
                OutputQueue: make(chan StreamChunk, 500),
                subscribers: make(map[string]*subscriber),
                TaskCtx:     taskCtx,
                TaskCancel:  taskCancel,
        }
}

// LoadFromPersist 从持久化存储加载历史记录
func (s *GlobalSession) LoadFromPersist() error {
        if globalSessionPersist == nil {
                return nil
        }
        saved, err := globalSessionPersist.LoadSession(s.ID)
        if err != nil {
                // 文件不存在是首次运行的正常情况
                if errors.Is(err, os.ErrNotExist) {
                        return nil
                }
                return err
        }
        if saved == nil {
                // 无持久化数据（首次运行）
                return nil
        }
        s.mu.Lock()
        defer s.mu.Unlock()
        s.History = saved.History
        s.ID = saved.ID
        s.persistID = saved.ID
        s.CreatedAt = saved.CreatedAt
        s.LastSeen = time.Now()
        log.Printf("[GlobalSession] Loaded session %s from persist, %d messages", s.ID, len(s.History))
        return nil
}

// AddToHistory 添加消息到历史并触发自动保存
func (s *GlobalSession) AddToHistory(role, content string) {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.History = append(s.History, Message{Role: role, Content: content, Timestamp: time.Now().Unix()})
        s.LastSeen = time.Now()
        go s.autoSaveHistory()
}

// GetHistory 返回历史消息副本
func (s *GlobalSession) GetHistory() []Message {
        s.mu.RLock()
        defer s.mu.RUnlock()
        h := make([]Message, len(s.History))
        copy(h, s.History)
        return h
}

// SetHistory 替换历史并触发保存
func (s *GlobalSession) SetHistory(h []Message) {
        s.mu.Lock()
        s.History = h
        s.LastSeen = time.Now()
        s.mu.Unlock()
        go s.autoSaveHistory()
}

// TryStartTask 尝试启动新任务，返回是否成功和任务ID
func (s *GlobalSession) TryStartTask() (bool, string) {
        s.mu.Lock()
        defer s.mu.Unlock()
        if s.TaskRunning {
                return false, ""
        }
        s.TaskRunning = true
        taskID := uuid.New().String()
        s.currentTaskID = taskID
        s.TaskCtx, s.TaskCancel = context.WithCancel(context.Background())
        return true, taskID
}

// SetTaskRunning 标记任务运行状态
func (s *GlobalSession) SetTaskRunning(running bool, taskID string) {
        s.mu.Lock()
        defer s.mu.Unlock()
        if s.currentTaskID != taskID {
                return
        }
        s.TaskRunning = running
        if !running {
                s.currentTaskID = ""
        }
}

// CancelTask 取消当前任务
func (s *GlobalSession) CancelTask() {
        s.mu.Lock()
        defer s.mu.Unlock()
        if s.TaskCancel != nil && s.TaskRunning {
                log.Printf("[GlobalSession] CancelTask: cancelling task (taskID=%s)", s.currentTaskID)
                s.TaskCancel()
                s.TaskCtx, s.TaskCancel = context.WithCancel(context.Background())
                s.TaskRunning = false
                s.currentTaskID = ""
        }
}

// IsTaskRunning 检查是否有任务在运行
func (s *GlobalSession) IsTaskRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.TaskRunning
}

// ProcessUserInput 处理用户输入并触发模型调用
func ProcessUserInput(session *GlobalSession, input string) {
	ok, taskID := session.TryStartTask()
	if !ok {
		session.EnqueueOutput(StreamChunk{Error: "已有任务在执行中，请使用 /stop 取消后再试"})
		return
	}
	taskCtx := session.GetTaskCtx()
	session.EnqueueOutput(StreamChunk{TaskRunning: true})
	defer func() {
		session.SetTaskRunning(false, taskID)
		session.EnqueueOutput(StreamChunk{TaskRunning: false})
	}()
	outputChannel := NewSessionChannel(session)
	history := session.GetHistory()
	// 使用全局变量
	newHistory, err := AgentLoop(taskCtx, outputChannel, history, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking)
	if err != nil && err != context.Canceled {
		session.EnqueueOutput(StreamChunk{Error: err.Error(), Done: true})
	}
	if len(newHistory) > len(history) {
		session.SetHistory(newHistory)
	}
}

// GetTaskCtx 返回当前任务的 context
func (s *GlobalSession) GetTaskCtx() context.Context {
        s.mu.RLock()
        defer s.mu.RUnlock()
        return s.TaskCtx
}

// Subscribe 注册一个输出广播订阅者
// 返回接收 chunk 的 channel 和用于通知退订的 done channel
func (s *GlobalSession) Subscribe(id string) (<-chan StreamChunk, <-chan struct{}) {
        s.subscribersMu.Lock()
        defer s.subscribersMu.Unlock()
        ch := make(chan StreamChunk, 500)
        done := make(chan struct{})
        if existing, ok := s.subscribers[id]; ok {
                close(existing.done)
        }
        s.subscribers[id] = &subscriber{ch: ch, done: done}
        log.Printf("[GlobalSession] Subscriber %s added (total: %d)", id, len(s.subscribers))
        return ch, done
}

// Unsubscribe 移除一个输出广播订阅者
func (s *GlobalSession) Unsubscribe(id string) {
        s.subscribersMu.Lock()
        defer s.subscribersMu.Unlock()
        if sub, ok := s.subscribers[id]; ok {
                close(sub.done)
                delete(s.subscribers, id)
                log.Printf("[GlobalSession] Subscriber %s removed (total: %d)", id, len(s.subscribers))
        }
}

// EnqueueOutput 广播输出到所有订阅者
func (s *GlobalSession) EnqueueOutput(chunk StreamChunk) {
        // 广播到所有订阅者
        s.subscribersMu.RLock()
        for id, sub := range s.subscribers {
                select {
                case sub.ch <- chunk:
                default:
                        log.Printf("[GlobalSession] subscriber %s queue full, dropped chunk", id)
                }
        }
        s.subscribersMu.RUnlock()

        // 同时写入 OutputQueue 保持向后兼容
        select {
        case s.OutputQueue <- chunk:
        default:
                select {
                case <-s.OutputQueue:
                default:
                }
                s.OutputQueue <- chunk
        }
}

// autoSaveHistory 自动保存当前会话
func (s *GlobalSession) autoSaveHistory() {
        s.persistMu.Lock()
        defer s.persistMu.Unlock()

        s.mu.RLock()
        historyCopy := make([]Message, len(s.History))
        copy(historyCopy, s.History)
        sessionID := s.ID
        s.mu.RUnlock()

        if len(historyCopy) == 0 {
                return
        }

        description := "会话"
        for _, msg := range historyCopy {
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

        if s.persistID == "" {
                saved, err := globalSessionPersist.SaveSession(sessionID, historyCopy, description)
                if err != nil {
                        log.Printf("[GlobalSession] Auto save failed: %v", err)
                        return
                }
                s.persistID = saved.ID
                log.Printf("[GlobalSession] Auto saved (new) with ID %s", sessionID)
        } else {
                err := globalSessionPersist.UpdateSession(s.persistID, historyCopy)
                if err != nil {
                        saved, err2 := globalSessionPersist.SaveSession(sessionID, historyCopy, description)
                        if err2 != nil {
                                log.Printf("[GlobalSession] Auto save re-create failed: %v", err2)
                                return
                        }
                        s.persistID = saved.ID
                        log.Printf("[GlobalSession] Auto saved (re-created) with ID %s", sessionID)
                } else {
                        log.Printf("[GlobalSession] Auto saved (update)")
                }
        }
}
