package main

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "log"
        "os"
        "path/filepath"
        "strings"
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

        // 中斷（pause）機制：取消 CallModel 但不取消任務
        //   - interruptCancel 對應當前 CallModel 的 cancel func
        //   - /pause 命令會同時設置 interruptMsg 並調用 interruptCancel
        //   - AgentLoop 檢測到中斷後注入 interruptMsg，創建新的 CallModel cancel
        interruptCancel context.CancelFunc
        interruptMsg    string
        interruptMu     sync.Mutex

        OutputQueue   chan StreamChunk       // 用于向后兼容
        InputQueue    chan string            // 输入消息队列，用于存储待处理的消息（包括唤醒通知）
        InputMessages []string               // 输入消息列表，用于存储待处理的消息（自动增长）
        inputMu       sync.Mutex             // 输入消息列表的锁
        subscribers   map[string]*subscriber // 广播订阅者列表
        subscribersMu sync.RWMutex           // subscribers 读写锁

        persistID string
        persistMu sync.Mutex

        Connected bool // 是否至少有一个 WebSocket 连接（仅用于 WS，由 Subscribe/Unsubscribe 自動維護）

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
        messagesDir := filepath.Join(globalDataDir, "pending_messages")
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
        filePath := filepath.Join(globalDataDir, "pending_messages", "pending_messages.json")
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

// IsTaskCancelled 檢查任務上下文是否已被取消（用於抑制 /stop 之後的殘留工具輸出）
func (s *GlobalSession) IsTaskCancelled() bool {
        s.mu.RLock()
        defer s.mu.RUnlock()
        if s.TaskCtx == nil {
                return true // 沒有活躍任務視為已取消
        }
        select {
        case <-s.TaskCtx.Done():
                return true
        default:
                return false
        }
}

// InterruptTask 中斷當前 CallModel 串流但不取消任務。
// 模型會在下一輪迭代中接收 msg 作為用戶輸入並繼續任務。
// 若無活躍任務則為空操作。
func (s *GlobalSession) InterruptTask(msg string) {
        s.interruptMu.Lock()
        s.interruptMsg = msg
        cancel := s.interruptCancel
        s.interruptMu.Unlock()
        if cancel != nil {
                log.Printf("[GlobalSession] InterruptTask: interrupting current LLM call (msg=%q)", msg)
                cancel()
        }
}

// getInterruptCancel 返回當前中斷 cancel func（由 AgentLoop 在每次 CallModel 前設置）
func (s *GlobalSession) getInterruptCancel() context.CancelFunc {
        s.interruptMu.Lock()
        defer s.interruptMu.Unlock()
        return s.interruptCancel
}

// setInterruptCancel 設置當前 CallModel 的中斷 cancel（由 AgentLoop 在每次 CallModel 前調用）
func (s *GlobalSession) setInterruptCancel(cancel context.CancelFunc) {
        s.interruptMu.Lock()
        defer s.interruptMu.Unlock()
        s.interruptCancel = cancel
}

// takeInterruptMsg 取出並清空中斷訊息（由 AgentLoop 在每次迭代頂部調用）
func (s *GlobalSession) takeInterruptMsg() string {
        s.interruptMu.Lock()
        defer s.interruptMu.Unlock()
        msg := s.interruptMsg
        s.interruptMsg = ""
        return msg
}

// ProcessUserInput 处理用户输入并触发模型调用
func ProcessUserInput(session *GlobalSession, input string) {
        // === Idle 任務接續檢查 ===
        idleResult := session.CheckTaskOnIdle()

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
                wasRunning := session.IsTaskRunning()
                session.SetTaskRunning(false, taskID)
                // 只有任務原本喺執行中先發送 TaskRunning: false，
                // 避免 /stop 已由 command handler 處理後重複輸出
                if wasRunning {
                        session.EnqueueOutput(StreamChunk{TaskRunning: false})
                }

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
        historyLenBeforeInject := len(history)

        // IdleInjectResume：喺 AgentLoop 嘅 messages slice 注入「繼續任務」喚醒模型
        // 只喺 AgentLoop 嘅 messages，唔入 global history
        if idleResult != nil && *idleResult == IdleInjectResume {
                resumeMsg := Message{Role: "user", Content: "[SYSTEM_RESUME] 繼續任務。"}
                // 注入喺用戶最後一條消息之前
                history = append(history[:len(history)-1], resumeMsg, history[len(history)-1])
        }

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

        // 移除隱藏嘅 resume 消息，確保佢唔會入 global history
        if idleResult != nil && *idleResult == IdleInjectResume {
                newHistory = stripResumeMessage(newHistory)
        }

        if len(newHistory) > historyLenBeforeInject {
                session.SetHistory(newHistory)
        }

        // Post-AgentLoop idle 結果處理
        if idleResult != nil {
                if *idleResult == IdlePauseCheck {
                        session.GetTracker().PauseIdleCheck()
                }
                // InjectResume：任務完成檢查由現有嘅 AskModelTaskCompletion
                // 喺 RunPostLoop → MarkTaskCompleted 入面處理
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
// reason: 重置原因（"idle"、"new_command"）
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

// ============================================================================
// Idle 任務接續檢查
// ============================================================================

// IdleCheckResult idle 任務接續檢查的結果
type IdleCheckResult int

const (
        IdleNoAction     IdleCheckResult = iota // 無需處理（chat mode / blocked）
        IdleInjectResume                        // 注入「繼續任務」喚醒模型
        IdlePauseCheck                          // 標記完成，暫停後續 idle check
)

// CheckTaskOnIdle 檢查是否需要因 idle 超時進行任務接續檢查
// 返回 nil 表示不需要檢查
func (s *GlobalSession) CheckTaskOnIdle() *IdleCheckResult {
        s.mu.RLock()
        lastSeen := s.LastSeen
        s.mu.RUnlock()

        tracker := s.GetTracker()
        if tracker == nil {
                return nil
        }

        if !tracker.ShouldCheckTaskOnIdle(lastSeen) {
                return nil
        }

        s.mu.Lock()
        s.LastSeen = time.Now() // 更新防止重覆觸發
        s.mu.Unlock()

        // Chat mode → return nil
        if globalTaskTracker == nil || !globalTaskTracker.IsWorkMode() {
                return nil
        }

        // Work mode：三元分類
        history := s.GetHistory()
        result := classifyTaskIdleStatus(history)

        switch result {
        case "COMPLETE":
                r := IdlePauseCheck
                return &r
        case "BLOCKED":
                r := IdleNoAction
                return &r
        default: // CONTINUE (or parse error safe default)
                r := IdleInjectResume
                return &r
        }
}

// classifyTaskIdleStatus 使用強制模型做三元分類（COMPLETE / BLOCKED / CONTINUE）
// 只判斷最後一個任務，不理會歷史中其他未完成的舊任務
func classifyTaskIdleStatus(history []Message) string {
        messages := []Message{
                {
                        Role: "system",
                        Content: `你是一個任務狀態分類器。根據對話歷史，判斷用戶**最後一個請求的任務**當前所處的狀態。

重要：只判斷最後一個任務，不要理會歷史中其他未完成的舊任務。
只回覆一個詞：

COMPLETE — 最後的任務已完成，所有步驟已執行完畢，無遺留工作
BLOCKED  — 最後的任務受阻，需要用戶的決定或輸入才能繼續
CONTINUE — 最後的任務可繼續執行，之前可能因異常中斷等原因暫停

當前任務狀態：`,
                },
        }
        messages = append(messages, history...)

        // 解析當前有效的模型配置
        effectiveAPIType := apiType
        effectiveBaseURL := baseURL
        effectiveAPIKey := apiKey
        effectiveModelID := modelID

        if globalConfigManager != nil && globalStage != nil {
                currentActor := globalStage.GetCurrentActor()
                if modelConfig := getActorModelConfig(currentActor); modelConfig != nil {
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
                }
        }

        ctx := context.Background()
        resp, err := CallModelSync(ctx, messages, effectiveAPIType, effectiveBaseURL, effectiveAPIKey, effectiveModelID, 0.0, 10, false, false)
        if err != nil {
                log.Printf("[classifyTaskIdleStatus] CallModelSync error: %v, defaulting to CONTINUE", err)
                return "CONTINUE"
        }

        content := strings.TrimSpace(extractContentString(resp.Content))
        upper := strings.ToUpper(content)

        if strings.HasPrefix(upper, "COMPLETE") {
                return "COMPLETE"
        }
        if strings.HasPrefix(upper, "BLOCKED") {
                return "BLOCKED"
        }
        // safe default: CONTINUE
        log.Printf("[classifyTaskIdleStatus] Unrecognized response: %q, defaulting to CONTINUE", content)
        return "CONTINUE"
}

// stripResumeMessage 從歷史中移除隱藏的 resume 消息
func stripResumeMessage(history []Message) []Message {
        var filtered []Message
        for _, msg := range history {
                if content, ok := msg.Content.(string); ok && content == "[SYSTEM_RESUME] 繼續任務。" {
                        continue
                }
                filtered = append(filtered, msg)
        }
        return filtered
}

// Subscribe 注册一个输出广播订阅者
// 返回接收 chunk 的 channel 和用于通知退订的 done channel
func (s *GlobalSession) Subscribe(id string) (<-chan StreamChunk, <-chan struct{}) {
        s.subscribersMu.Lock()
        ch := make(chan StreamChunk, 500)
        done := make(chan struct{})
        if existing, ok := s.subscribers[id]; ok {
                close(existing.done)
        }
        s.subscribers[id] = &subscriber{ch: ch, done: done}
        isFirst := len(s.subscribers) == 1
        total := len(s.subscribers)
        s.subscribersMu.Unlock()

        // 自動維護 Connected：第一個訂閱者加入時設為 true
        if isFirst {
                s.mu.Lock()
                s.Connected = true
                s.mu.Unlock()
        }
        log.Printf("[GlobalSession] Subscriber %s added (total: %d)", id, total)
        return ch, done
}

// Unsubscribe 移除一个输出广播订阅者
func (s *GlobalSession) Unsubscribe(id string) {
        s.subscribersMu.Lock()
        if sub, ok := s.subscribers[id]; ok {
                close(sub.done)
                delete(s.subscribers, id)
        }
        isEmpty := len(s.subscribers) == 0
        total := len(s.subscribers)
        s.subscribersMu.Unlock()

        if isEmpty {
                s.mu.Lock()
                s.Connected = false
                s.mu.Unlock()
        }
        log.Printf("[GlobalSession] Subscriber %s removed (total: %d)", id, total)
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
