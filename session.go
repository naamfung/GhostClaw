package main

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "log"
        "os"
        "path/filepath"
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
        ID        string
        History   []Message
        CreatedAt time.Time
        LastSeen  time.Time

        TaskRunning   bool
        currentTaskID string
        TaskCtx       context.Context
        TaskCancel    context.CancelFunc

        OutputQueue   chan StreamChunk       // 用于向后兼容
        InputQueue    chan string            // 输入消息队列，用于存储待处理的消息（包括唤醒通知）
        InputMessages []string               // 输入消息列表，用于存储待处理的消息（自动增长）
        inputMu       sync.Mutex             // 输入消息列表的锁
        subscribers   map[string]*subscriber // 广播订阅者列表
        subscribersMu sync.RWMutex           // subscribers 读写锁

        persistID string
        persistMu sync.Mutex

        Connected bool // 是否至少有一个 WebSocket 连接（仅用于 WS）

        // IsNewSession 標記當前會話是否爲新會話（/new 或 idle 重置後設爲 true）
        // 首輪對話後自動清除。用於抑制首輪的記憶注入，防止舊上下文洩漏到新會話
        IsNewSession bool

        // Token 追蹤器（idle 重置 + token 統計）
        tracker *SessionTracker

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
                ID:            "default", // 可配置
                History:       make([]Message, 0),
                CreatedAt:     time.Now(),
                LastSeen:      time.Now(),
                OutputQueue:   make(chan StreamChunk, 500),
                InputQueue:    make(chan string, 100), // 保留用于向后兼容
                InputMessages: make([]string, 0),      // 输入消息列表，自动增长
                subscribers:   make(map[string]*subscriber),
                TaskCtx:       taskCtx,
                TaskCancel:    taskCancel,
                tracker:       NewSessionTracker(EffectiveSessionConfig()),
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

        // 恢復 token 追蹤統計（從 DB 加載）
        if s.tracker != nil && globalDB != nil && s.persistID != "" {
                var row SessionHistories
                if result := globalDB.Where("id = ?", s.persistID).First(&row); result.Error == nil && row.TotalTokens > 0 {
                        s.tracker.mu.Lock()
                        s.tracker.stats.InputTokens = row.InputTokens
                        s.tracker.stats.OutputTokens = row.OutputTokens
                        s.tracker.stats.TotalTokens = row.TotalTokens
                        s.tracker.stats.TurnCount = row.TurnCount
                        s.tracker.started = true
                        s.tracker.mu.Unlock()
                        log.Printf("[GlobalSession] Restored token stats: input=%d, output=%d, total=%d, turns=%d",
                                row.InputTokens, row.OutputTokens, row.TotalTokens, row.TurnCount)
                }
        }

        // 加载未处理消息队列
        if err := s.loadPendingMessages(); err != nil {
                log.Printf("Failed to load pending messages: %v", err)
        }

        return nil
}

// SavePendingMessages 保存未处理消息队列到文件
func (s *GlobalSession) SavePendingMessages() error {
        // 创建消息队列目录
        messagesDir := filepath.Join(globalExecDir, "pending_messages")
        if err := os.MkdirAll(messagesDir, 0755); err != nil {
                return fmt.Errorf("failed to create pending messages directory: %w", err)
        }

        // 生成文件路径
        filePath := filepath.Join(messagesDir, "pending_messages.json")

        // 获取未处理消息
        s.inputMu.Lock()
        messages := s.InputMessages
        s.inputMu.Unlock()

        // 序列化消息
        data, err := json.Marshal(messages)
        if err != nil {
                return fmt.Errorf("failed to marshal pending messages: %w", err)
        }

        // 写入文件
        if err := os.WriteFile(filePath, data, 0644); err != nil {
                return fmt.Errorf("failed to write pending messages to file: %w", err)
        }

        log.Printf("[GlobalSession] Saved %d pending messages to file", len(messages))
        return nil
}

// loadPendingMessages 从文件加载未处理消息队列
func (s *GlobalSession) loadPendingMessages() error {
        // 检查消息队列文件是否存在
        filePath := filepath.Join(globalExecDir, "pending_messages", "pending_messages.json")
        if _, err := os.Stat(filePath); os.IsNotExist(err) {
                return nil
        }

        // 读取文件内容
        data, err := os.ReadFile(filePath)
        if err != nil {
                return fmt.Errorf("failed to read pending messages file: %w", err)
        }

        // 反序列化消息
        var messages []string
        if err := json.Unmarshal(data, &messages); err != nil {
                return fmt.Errorf("failed to unmarshal pending messages: %w", err)
        }

        // 添加到未处理消息队列
        s.inputMu.Lock()
        s.InputMessages = append(s.InputMessages, messages...)
        s.inputMu.Unlock()

        // 删除已加载的消息文件
        if err := os.Remove(filePath); err != nil {
                log.Printf("Failed to remove pending messages file: %v", err)
        }

        log.Printf("[GlobalSession] Loaded %d pending messages from file", len(messages))
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
        // === Idle 重置 + Token 上限檢查 ===
        if resetNotice := session.CheckIdleReset(); resetNotice != "" {
                session.EnqueueOutput(StreamChunk{Content: "\n" + resetNotice + "\n"})
        }

        // 記錄對話輪次
        if tracker := session.GetTracker(); tracker != nil {
                tracker.RecordTurn()
        }
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

                // 处理输入队列中的下一条消息
                go processInputQueue(session)
        }()

        // 将当前输入添加到历史记录
        session.AddToHistory("user", input)

        // 跨轮隐式反馈采集：检查上一轮是否有已完成的任务
        // 跳过唤醒通知 — 系统生成的消息不触发用户反馈采集
        if globalFeedbackCollector != nil && !IsWakeNotification(input) {
            history := session.GetHistory()
            if feedback := globalFeedbackCollector.CollectImplicitFeedback(input, history); feedback != nil {
                log.Printf("[FeedbackCollector] Cross-turn implicit feedback collected: rating=%d", feedback.Rating)
            }
        }

        outputChannel := NewSessionChannel(session)
        history := session.GetHistory()

        // 获取当前模型配置
        effectiveAPIType := apiType
        effectiveBaseURL := baseURL
        effectiveAPIKey := apiKey
        effectiveModelID := modelID
        effectiveTemperature := temperature
        effectiveMaxTokens := maxTokens
        effectiveStream := stream
        effectiveThinking := thinking

        // 优先使用从ConfigManager获取的模型配置
        if globalConfigManager != nil {
                // 首先尝试获取当前actor的模型配置（如果有）
                if globalStage != nil {
                        currentActor := globalStage.GetCurrentActor()
                        if modelConfig := getActorModelConfig(currentActor); modelConfig != nil {
                                log.Printf("[Session] Using actor model config: %s (API: %s, BaseURL: %s)", modelConfig.Model, modelConfig.APIType, modelConfig.BaseURL)
                                if modelConfig.APIType != "" {
                                        effectiveAPIType = modelConfig.APIType
                                }
                                if modelConfig.BaseURL != "" {
                                        effectiveBaseURL = modelConfig.BaseURL
                                }
                                if modelConfig.APIKey != "" {
                                        effectiveAPIKey = modelConfig.ResolveAPIKey()
                                }
                                if modelConfig.Model != "" {
                                        effectiveModelID = modelConfig.Model
                                }
                                if modelConfig.Temperature > 0 {
                                        effectiveTemperature = modelConfig.Temperature
                                }
                                if modelConfig.MaxTokens > 0 {
                                        effectiveMaxTokens = modelConfig.MaxTokens
                                }
                        }
                }
        } else {
                log.Printf("[Session] No config manager found, using default model config: %s (API: %s, BaseURL: %s)", effectiveModelID, effectiveAPIType, effectiveBaseURL)
        }

        newHistory, err := AgentLoop(taskCtx, outputChannel, history, effectiveAPIType, effectiveBaseURL, effectiveAPIKey, effectiveModelID, effectiveTemperature, effectiveMaxTokens, effectiveStream, effectiveThinking)
        if err != nil && err != context.Canceled {
                session.EnqueueOutput(StreamChunk{Error: err.Error(), Done: true})
        }
        if len(newHistory) > len(history) {
                session.SetHistory(newHistory)
        }
}

// processInputQueue 处理输入消息列表中的消息
func processInputQueue(session *GlobalSession) {
        // 检查是否有任务在运行
        session.mu.RLock()
        taskRunning := session.TaskRunning
        session.mu.RUnlock()

        if taskRunning {
                // 有任务在运行，2 秒後重試，避免 wake 通知永久丟失
                go func() {
                        time.Sleep(2 * time.Second)
                        processInputQueue(session)
                }()
                return
        }

        // 检查输入消息列表是否有消息
        session.inputMu.Lock()
        if len(session.InputMessages) == 0 {
                session.inputMu.Unlock()
                // 列表空，不需要处理
                return
        }

        // 获取第一条消息并从列表中移除
        nextInput := session.InputMessages[0]
        session.InputMessages = session.InputMessages[1:]
        session.inputMu.Unlock()

        log.Printf("[Session] Processing next message from input messages")
        // 处理列表中的下一条消息
        ProcessUserInput(session, nextInput)
}

// GetTaskCtx 返回当前任务的 context
func (s *GlobalSession) GetTaskCtx() context.Context {
        s.mu.RLock()
        defer s.mu.RUnlock()
        return s.TaskCtx
}

// GetTracker 返回會話追蹤器
func (s *GlobalSession) GetTracker() *SessionTracker {
        return s.tracker
}

// ConsumeIsNewSession 消費 IsNewSession 標記（原子操作）。
// 返回 true 表示這是新會話的首輪對話，調用後自動清除標記。
// 用於 AgentLoop 中決定是否抑制記憶注入。
func (s *GlobalSession) ConsumeIsNewSession() bool {
        s.mu.Lock()
        defer s.mu.Unlock()
        if s.IsNewSession {
                s.IsNewSession = false
                return true
        }
        return false
}

// fullSessionReset 完整重置會話的所有子系統狀態。
// 調用方必須在持有 session.mu 的情況下調用（inputMu 在內部嵌套獲取）。
// reason: 重置原因（"idle"、"token_limit"、"new_command"）
func (s *GlobalSession) fullSessionReset(reason string) {
        // === 核心會話狀態 ===
        s.History = make([]Message, 0)
        s.LastSeen = time.Now()
        s.CreatedAt = time.Now()
        s.IsNewSession = true
        s.persistID = "" // D11 修復：清除持久化 ID，防止新會話覆寫舊 DB 記錄

        // === 輸入隊列（嵌套鎖，mu > inputMu）===
        s.inputMu.Lock()
        s.InputMessages = make([]string, 0)
        s.inputMu.Unlock()

        // === SessionTracker ===
        if s.tracker != nil {
                s.tracker.Reset(reason, true)
        }

        // === MemoryConsolidator 會話級緩存 ===
        if globalMemoryConsolidator != nil {
                globalMemoryConsolidator.ClearSession("default")
        }

        // === FeedbackCollector 任務完成狀態 ===
        if globalFeedbackCollector != nil {
                globalFeedbackCollector.ClearTaskCompleted()
        }

        // === Stage auto-switch 輪次計數器（C9 修復）===
        if globalStage != nil {
                globalStage.ResetAutoTurns()
        }

        // === InputQueue 防禦性排空（C10 修復）===
        for {
                select {
                case <-s.InputQueue:
                default:
                        goto drained
                }
        }
drained:
}

// persistEmptySession 將空的會話顯式持久化到 DB。
// 確保 idle/token 重置後重啟加載的是空會話而非舊數據（B6 修復）。
func (s *GlobalSession) persistEmptySession(reason string) {
        if globalSessionPersist == nil {
                return
        }
        s.persistMu.Lock()
        defer s.persistMu.Unlock()
        s.mu.RLock()
        sessionID := s.ID
        s.mu.RUnlock()

        saved, err := globalSessionPersist.SaveSession(sessionID, []Message{}, fmt.Sprintf("auto_reset_%s", reason))
        if err != nil {
                log.Printf("[GlobalSession] Failed to persist empty session after %s reset: %v", reason, err)
                return
        }
        s.mu.Lock()
        s.persistID = saved.ID
        s.mu.Unlock()
        log.Printf("[GlobalSession] Persisted empty session after %s reset: %s", reason, saved.ID)
}

// CheckIdleReset 檢查是否需要 idle 重置，如果需要則重置會話並返回通知消息
// 返回空字符串表示不需要重置
func (s *GlobalSession) CheckIdleReset() string {
        s.mu.RLock()
        lastSeen := s.LastSeen
        s.mu.RUnlock()

        tracker := s.GetTracker()
        if tracker == nil {
                return ""
        }

        if tracker.ShouldIdleReset(lastSeen) {
                cfg := EffectiveSessionConfig()
                s.mu.RLock()
                hadActivity := len(s.History) > 0
                s.mu.RUnlock()

                s.mu.Lock()
                s.fullSessionReset("idle")
                s.mu.Unlock()

                go s.persistEmptySession("idle")

                log.Printf("[GlobalSession] Idle reset triggered: idle_timeout=%d mins, had_activity=%v, full_state_cleared",
                        cfg.IdleTimeoutMins, hadActivity)
                return BuildIdleResetNotice(cfg.IdleTimeoutMins, hadActivity)
        }

        // 檢查 token 上限重置
        if tracker.IsTokenBudgetExceeded() {
                cfg := EffectiveSessionConfig()
                s.mu.RLock()
                hadActivity := len(s.History) > 0
                s.mu.RUnlock()

                s.mu.Lock()
                s.fullSessionReset("token_limit")
                s.mu.Unlock()

                go s.persistEmptySession("token_limit")

                log.Printf("[GlobalSession] Token limit reset triggered: limit=%d tokens, had_activity=%v, full_state_cleared",
                        cfg.SessionTokenLimit, hadActivity)
                return BuildTokenLimitNotice(cfg.SessionTokenLimit)
        }

        return ""
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

        if len(historyCopy) == 0 {
                s.mu.RUnlock()
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
                s.mu.RUnlock()
                if err != nil {
                        log.Printf("[GlobalSession] Auto save failed: %v", err)
                        return
                }
                s.persistID = saved.ID
                log.Printf("[GlobalSession] Auto saved (new) with ID %s", sessionID)
        } else {
                err := globalSessionPersist.UpdateSession(s.persistID, historyCopy)
                s.mu.RUnlock()
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
