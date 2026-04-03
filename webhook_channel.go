// +build webhook

// Webhook 渠道
// 通过 HTTP POST 接收外部系统推送的消息
// 使用 go build -tags webhook 来启用
//
// 功能特性：
// 1. 同步模式：等待 AI 处理完成后直接返回结果（适合快速响应场景）
// 2. 异步模式：接收请求后立即返回，处理完成后调用回调 URL（适合长时间处理场景）
// 3. 回调 URL 支持：全局配置 或 请求级别指定（请求级别优先）
// 4. 结果查询：提供 API 查询异步任务处理结果
//
// API 端点：
// - POST /webhook      - 接收消息
// - GET  /healthz      - 健康检查
// - GET  /result/:id   - 查询异步任务结果
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// WebhookConfig Webhook 渠道配置
type WebhookConfig struct {
	Enabled       bool     `toon:"enabled" json:"Enabled"`
	Listen        string   `toon:"listen" json:"Listen"`                           // 监听地址，默认 ":10087"
	Path          string   `toon:"path" json:"Path"`                               // 接收路径，默认 "/webhook"
	AllowedTokens []string `toon:"allowed_tokens" json:"AllowedTokens"`            // 允许的 Bearer token（鉴权）
	CallbackURL   string   `toon:"callback_url" json:"CallbackURL"`                // 全局回调 URL（可选）
	Async         bool     `toon:"async" json:"Async"`                             // 默认异步模式
	SyncTimeout   int      `toon:"sync_timeout" json:"SyncTimeout"`                // 同步模式超时（秒），默认 300
	GroupPolicy   string   `toon:"group_policy" json:"GroupPolicy"`
}

// WebhookRequest 外部系统发来的请求体
type WebhookRequest struct {
	SenderID    string                 `json:"sender_id"`              // 可选，发送者标识
	ChannelID   string                 `json:"channel_id"`             // 可选，来源渠道标识
	Message     string                 `json:"message"`                // 消息内容
	CallbackURL string                 `json:"callback_url,omitempty"` // 可选，请求级别的回调 URL
	Sync        *bool                  `json:"sync,omitempty"`         // 可选，强制指定同步/异步模式
	Metadata    map[string]interface{} `json:"metadata"`               // 可选附加数据
}

// WebhookResponse 返回给调用方的响应
type WebhookResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Result    string `json:"result,omitempty"`    // 同步模式下的 AI 响应
	TaskID    string `json:"task_id,omitempty"`   // 异步模式下的任务 ID
	Timestamp int64  `json:"timestamp"`
}

// WebhookCallback 回调请求体
type WebhookCallback struct {
	TaskID    string `json:"task_id"`
	Status    string `json:"status"` // "completed", "failed", "cancelled"
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// WebhookResult 存储的异步任务结果
type WebhookResult struct {
	TaskID    string
	Status    string
	Result    string
	Error     string
	Timestamp time.Time
}

// WebhookChannel 实现 Channel 接口
type WebhookChannel struct {
	*BaseChannel
	config      WebhookConfig
	server      *http.Server
	mu          sync.RWMutex
	handler     func(chatID, senderID, content string, metadata map[string]interface{})
	httpClient  *http.Client
	// 异步任务结果存储
	results     map[string]*WebhookResult
	resultsMu   sync.RWMutex
	// 同步模式用的响应通道
	syncChans   map[string]chan *WebhookResult
	syncMu      sync.RWMutex
}

// NewWebhookChannel 创建 Webhook 渠道
func NewWebhookChannel(config *WebhookConfig) (*WebhookChannel, error) {
	if config == nil {
		return nil, fmt.Errorf("webhook config is nil")
	}
	if config.Listen == "" {
		config.Listen = ":10087"
	}
	if config.Path == "" {
		config.Path = "/webhook"
	}
	if !strings.HasPrefix(config.Path, "/") {
		config.Path = "/" + config.Path
	}
	if config.SyncTimeout <= 0 {
		config.SyncTimeout = 300 // 默认 5 分钟
	}

	return &WebhookChannel{
		BaseChannel: NewBaseChannel("webhook"),
		config:      *config,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		results:     make(map[string]*WebhookResult),
		syncChans:   make(map[string]chan *WebhookResult),
	}, nil
}

// Start 启动 Webhook HTTP 服务
func (wc *WebhookChannel) Start(messageHandler func(chatID, senderID, content string, metadata map[string]interface{})) error {
	wc.handler = messageHandler

	mux := http.NewServeMux()
	mux.HandleFunc(wc.config.Path, wc.handleWebhook)
	mux.HandleFunc("/healthz", wc.handleHealthz)
	mux.HandleFunc("/result/", wc.handleResultQuery)

	wc.server = &http.Server{Addr: wc.config.Listen, Handler: mux}

	go func() {
		log.Printf("[Webhook] Server listening on %s", wc.config.Listen)
		log.Printf("[Webhook] Endpoint: POST %s", wc.config.Path)
		log.Printf("[Webhook] Health check: GET /healthz")
		log.Printf("[Webhook] Result query: GET /result/:task_id")
		if wc.config.CallbackURL != "" {
			log.Printf("[Webhook] Global callback URL: %s", wc.config.CallbackURL)
		}
		if err := wc.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[Webhook] Server error: %v", err)
		}
	}()

	return nil
}

// handleHealthz 健康检查
func (wc *WebhookChannel) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"status":"error","message":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleResultQuery 查询异步任务结果
func (wc *WebhookChannel) handleResultQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"status":"error","message":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// 从路径中提取 task_id
	path := strings.TrimPrefix(r.URL.Path, "/result/")
	taskID := strings.TrimSpace(path)
	if taskID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(WebhookResponse{
			Status:  "error",
			Message: "task_id is required",
		})
		return
	}

	wc.resultsMu.RLock()
	result, exists := wc.results[taskID]
	wc.resultsMu.RUnlock()

	if !exists {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(WebhookResponse{
			Status:  "not_found",
			Message: "task not found or expired",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(WebhookCallback{
		TaskID:    result.TaskID,
		Status:    result.Status,
		Result:    result.Result,
		Error:     result.Error,
		Timestamp: result.Timestamp.Unix(),
	})
}

// handleWebhook 处理 Webhook POST 请求
func (wc *WebhookChannel) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"status":"error","message":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// 鉴权
	if len(wc.config.AllowedTokens) > 0 {
		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		valid := false
		for _, allowed := range wc.config.AllowedTokens {
			if token == allowed {
				valid = true
				break
			}
		}
		if !valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(WebhookResponse{Status: "unauthorized"})
			return
		}
	}

	// 解析请求体
	var req WebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(WebhookResponse{
			Status:  "error",
			Message: "invalid JSON body",
		})
		return
	}

	if strings.TrimSpace(req.Message) == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(WebhookResponse{
			Status:  "error",
			Message: "message field is required",
		})
		return
	}

	// 设置默认值
	if req.SenderID == "" {
		req.SenderID = "webhook"
	}
	if req.ChannelID == "" {
		req.ChannelID = "default"
	}
	if req.Metadata == nil {
		req.Metadata = make(map[string]interface{})
	}

	log.Printf("[Webhook] Received message from %s (channel: %s): %s",
		req.SenderID, req.ChannelID, truncateString(req.Message, 80))

	// 确定是否同步模式
	isSync := wc.config.Async
	if req.Sync != nil {
		isSync = !*req.Sync // Sync=true 表示同步，Async=false
	} else {
		isSync = !isSync // 默认配置 Async=true 表示异步，所以同步模式取反
	}

	// 确定回调 URL（请求级别优先）
	callbackURL := req.CallbackURL
	if callbackURL == "" {
		callbackURL = wc.config.CallbackURL
	}

	session := GetGlobalSession()

	// 统一处理斜杠命令
	if HandleSlashCommandWithDefaults(req.Message,
		func(resp string) {
			// 命令回應
			if isSync {
				// 同步模式：直接返回
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(WebhookResponse{
					Status:    "command_processed",
					Result:    resp,
					Timestamp: time.Now().Unix(),
				})
			} else if callbackURL != "" {
				// 异步模式：发送回调
				go wc.sendCallback(callbackURL, &WebhookCallback{
					TaskID:    fmt.Sprintf("cmd_%d", time.Now().UnixNano()),
					Status:    "completed",
					Result:    resp,
					Timestamp: time.Now().Unix(),
				})
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(WebhookResponse{
					Status:    "command_processed",
					Message:   "Command executed, callback will be sent",
					Timestamp: time.Now().Unix(),
				})
			} else {
				// 无回调 URL，直接返回（异步但无回调）
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(WebhookResponse{
					Status:    "command_processed",
					Result:    resp,
					Timestamp: time.Now().Unix(),
				})
			}
		},
		func() {
			session.CancelTask()
		},
		func() {
			log.Println("[Webhook] /exit ignored")
		}) {
		return
	}

	// 普通消息，加入历史并启动任务
	session.AddToHistory("user", req.Message)

	if isSync {
		// 同步模式
		wc.handleSyncRequest(w, session, req, callbackURL)
	} else {
		// 异步模式
		wc.handleAsyncRequest(w, session, req, callbackURL)
	}
}

// handleSyncRequest 处理同步请求
func (wc *WebhookChannel) handleSyncRequest(w http.ResponseWriter, session *GlobalSession, req WebhookRequest, callbackURL string) {
	taskID := fmt.Sprintf("sync_%d", time.Now().UnixNano())

	// 创建同步通道
	resultChan := make(chan *WebhookResult, 1)
	wc.syncMu.Lock()
	wc.syncChans[taskID] = resultChan
	wc.syncMu.Unlock()

	// 启动处理
	go wc.processWebhookMessage(session, taskID, req.ChannelID, req.SenderID, req.Message, req.Metadata, callbackURL)

	// 等待结果或超时
	select {
	case result := <-resultChan:
		wc.syncMu.Lock()
		delete(wc.syncChans, taskID)
		wc.syncMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if result.Status == "failed" {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		json.NewEncoder(w).Encode(WebhookResponse{
			Status:    result.Status,
			Result:    result.Result,
			Message:   result.Error,
			Timestamp: time.Now().Unix(),
		})

	case <-time.After(time.Duration(wc.config.SyncTimeout) * time.Second):
		wc.syncMu.Lock()
		delete(wc.syncChans, taskID)
		wc.syncMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGatewayTimeout)
		json.NewEncoder(w).Encode(WebhookResponse{
			Status:    "timeout",
			Message:   fmt.Sprintf("Request timeout after %d seconds", wc.config.SyncTimeout),
			Timestamp: time.Now().Unix(),
		})
	}
}

// handleAsyncRequest 处理异步请求
func (wc *WebhookChannel) handleAsyncRequest(w http.ResponseWriter, session *GlobalSession, req WebhookRequest, callbackURL string) {
	taskID := fmt.Sprintf("async_%d", time.Now().UnixNano())

	// 初始化结果存储
	wc.resultsMu.Lock()
	wc.results[taskID] = &WebhookResult{
		TaskID:    taskID,
		Status:    "processing",
		Timestamp: time.Now(),
	}
	wc.resultsMu.Unlock()

	// 启动后台处理
	go wc.processWebhookMessage(session, taskID, req.ChannelID, req.SenderID, req.Message, req.Metadata, callbackURL)

	// 立即返回任务 ID
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(WebhookResponse{
		Status:    "accepted",
		TaskID:    taskID,
		Message:   fmt.Sprintf("Processing started. Query result at /result/%s", taskID),
		Timestamp: time.Now().Unix(),
	})
}

// processWebhookMessage 处理 Webhook 消息
func (wc *WebhookChannel) processWebhookMessage(session *GlobalSession, taskID, channelID, senderID, input string, metadata map[string]interface{}, callbackURL string) {
	ok, realTaskID := session.TryStartTask()
	if !ok {
		result := &WebhookResult{
			TaskID:    taskID,
			Status:    "failed",
			Error:     "Task already running",
			Timestamp: time.Now(),
		}
		wc.storeResult(taskID, result)
		wc.notifyResult(taskID, result, callbackURL)
		return
	}

	taskCtx := session.GetTaskCtx()
	defer session.SetTaskRunning(false, realTaskID)

	// 创建缓冲收集输出
	var outputBuf bytes.Buffer
	ch := &webhookOutputCollector{buf: &outputBuf}

	// 获取当前历史
	history := session.GetHistory()

	// 执行 AgentLoop
	newHistory, err := AgentLoop(taskCtx, ch, history, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking)

	var result *WebhookResult
	if err != nil {
		if err == context.Canceled {
			result = &WebhookResult{
				TaskID:    taskID,
				Status:    "cancelled",
				Result:    outputBuf.String(),
				Timestamp: time.Now(),
			}
		} else {
			result = &WebhookResult{
				TaskID:    taskID,
				Status:    "failed",
				Result:    outputBuf.String(),
				Error:     err.Error(),
				Timestamp: time.Now(),
			}
		}
	} else {
		result = &WebhookResult{
			TaskID:    taskID,
			Status:    "completed",
			Result:    outputBuf.String(),
			Timestamp: time.Now(),
		}
	}

	if len(newHistory) > len(history) {
		session.SetHistory(newHistory)
	}

	// 存储结果
	wc.storeResult(taskID, result)

	// 通知结果
	wc.notifyResult(taskID, result, callbackURL)
}

// storeResult 存储任务结果
func (wc *WebhookChannel) storeResult(taskID string, result *WebhookResult) {
	wc.resultsMu.Lock()
	wc.results[taskID] = result
	wc.resultsMu.Unlock()

	// 清理旧结果（保留最近 1000 个）
	go wc.cleanupOldResults()
}

// cleanupOldResults 清理旧的结果
func (wc *WebhookChannel) cleanupOldResults() {
	wc.resultsMu.Lock()
	defer wc.resultsMu.Unlock()

	if len(wc.results) <= 1000 {
		return
	}

	// 找出最旧的条目
	type item struct {
		id        string
		timestamp time.Time
	}
	var items []item
	for id, result := range wc.results {
		items = append(items, item{id, result.Timestamp})
	}

	// 按时间排序并删除旧的
	// 简化处理：直接清空一半
	count := 0
	for id := range wc.results {
		delete(wc.results, id)
		count++
		if count >= len(wc.results)/2 {
			break
		}
	}
}

// notifyResult 通知结果（同步通道或回调）
func (wc *WebhookChannel) notifyResult(taskID string, result *WebhookResult, callbackURL string) {
	// 尝试发送到同步通道
	wc.syncMu.RLock()
	ch, exists := wc.syncChans[taskID]
	wc.syncMu.RUnlock()

	if exists {
		select {
		case ch <- result:
		default:
		}
	}

	// 发送回调
	if callbackURL != "" {
		wc.sendCallback(callbackURL, &WebhookCallback{
			TaskID:    result.TaskID,
			Status:    result.Status,
			Result:    result.Result,
			Error:     result.Error,
			Timestamp: result.Timestamp.Unix(),
		})
	}
}

// sendCallback 发送回调请求
func (wc *WebhookChannel) sendCallback(url string, callback *WebhookCallback) {
	payload, err := json.Marshal(callback)
	if err != nil {
		log.Printf("[Webhook] Failed to marshal callback: %v", err)
		return
	}

	resp, err := wc.httpClient.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("[Webhook] Callback failed to %s: %v", url, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Printf("[Webhook] Callback sent successfully to %s (task: %s)", url, callback.TaskID)
	} else {
		log.Printf("[Webhook] Callback returned status %d from %s", resp.StatusCode, url)
	}
}

// Stop 停止 Webhook 服务
func (wc *WebhookChannel) Stop() {
	if wc.server != nil {
		wc.server.Close()
		log.Println("[Webhook] Server stopped.")
	}
}

// WriteChunk 实现 Channel 接口 - 实际由 webhookOutputCollector 处理
func (wc *WebhookChannel) WriteChunk(chunk StreamChunk) error {
	// WebhookChannel 本身不直接处理输出，由 processWebhookMessage 中的 collector 处理
	return nil
}

// SendToUser 向指定用户/渠道发送消息
func (wc *WebhookChannel) SendToUser(userID string, message string) error {
	log.Printf("[Webhook] SendToUser to %s: %s", userID, truncateString(message, 100))
	return nil
}

// GetChannelType 获取渠道类型
func (wc *WebhookChannel) GetChannelType() string {
	return "webhook"
}

// RegisterToBus 注册到消息总线
func (wc *WebhookChannel) RegisterToBus() {
	if globalMessageBus != nil {
		globalMessageBus.RegisterChannelSender("webhook", wc)
		log.Println("[Webhook] Registered to message bus")
	}
}

// GetListenAddr 返回监听地址
func (wc *WebhookChannel) GetListenAddr() string {
	return wc.config.Listen
}

// webhookOutputCollector 用于收集 AgentLoop 的输出
type webhookOutputCollector struct {
	buf *bytes.Buffer
}

func (woc *webhookOutputCollector) WriteChunk(chunk StreamChunk) error {
	if chunk.Error != "" {
		return nil
	}
	woc.buf.WriteString(chunk.Content)
	return nil
}
