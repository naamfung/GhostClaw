// +build webhook

// Webhook 渠道
// 通过 HTTP POST 接收外部系统推送的消息
// 使用 go build -tags webhook 来启用
package main

import (
        "context"
        "encoding/json"
        "fmt"
        "log"
        "net/http"
        "strings"
        "sync"
)

// WebhookConfig Webhook 渠道配置
type WebhookConfig struct {
        Enabled      bool     `toon:"enabled" json:"Enabled"`
        Listen       string   `toon:"listen" json:"Listen"`           // 监听地址，默认 ":10087"
        Path         string   `toon:"path" json:"Path"`               // 接收路径，默认 "/webhook"
        AllowedTokens []string `toon:"allowed_tokens" json:"AllowedTokens"` // 允许的 Bearer token（鉴权）
        Async        bool     `toon:"async" json:"Async"`             // 异步模式：收到后立即 202，后台处理
        GroupPolicy  string   `toon:"group_policy" json:"GroupPolicy"`
}

// WebhookRequest 外部系统发来的请求体
type WebhookRequest struct {
        SenderID  string                 `json:"sender_id"`  // 可选，发送者标识
        ChannelID string                 `json:"channel_id"` // 可选，来源渠道标识
        Message   string                 `json:"message"`    // 消息内容
        Metadata  map[string]interface{} `json:"metadata"`   // 可选附加数据
}

// WebhookResponse 返回给调用方的响应
type WebhookResponse struct {
        Status  string `json:"status"`
        Message string `json:"message,omitempty"`
}

// WebhookChannel 实现 Channel 接口
type WebhookChannel struct {
        *BaseChannel
        config  WebhookConfig
        server  *http.Server
        mu      sync.RWMutex
        handler func(chatID, senderID, content string, metadata map[string]interface{})
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
        return &WebhookChannel{
                BaseChannel: NewBaseChannel("webhook"),
                config:      *config,
        }, nil
}

// Start 启动 Webhook HTTP 服务
func (wc *WebhookChannel) Start(messageHandler func(chatID, senderID, content string, metadata map[string]interface{})) error {
        wc.handler = messageHandler

        mux := http.NewServeMux()
        mux.HandleFunc(wc.config.Path, wc.handleWebhook)

        // 健康检查端点
        mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
                w.WriteHeader(http.StatusOK)
                fmt.Fprint(w, `{"status":"ok"}`)
        })

        wc.server = &http.Server{Addr: wc.config.Listen, Handler: mux}

        go func() {
                log.Printf("[Webhook] Server listening on %s, endpoint: %s", wc.config.Listen, wc.config.Path)
                if err := wc.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
                        log.Printf("[Webhook] Server error: %v", err)
                }
        }()

        return nil
}

// handleWebhook 处理 Webhook POST 请求
func (wc *WebhookChannel) handleWebhook(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }

        // 鉴权：检查 Bearer token
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
                        http.Error(w, `{"status":"unauthorized"}`, http.StatusUnauthorized)
                        return
                }
        }

        // 解析请求体
        var req WebhookRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                http.Error(w, `{"status":"error","message":"invalid JSON body"}`, http.StatusBadRequest)
                return
        }

        if strings.TrimSpace(req.Message) == "" {
                http.Error(w, `{"status":"error","message":"message field is required"}`, http.StatusBadRequest)
                return
        }

        // 默认值
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

        // 统一处理斜杠命令
        session := GetGlobalSession()
        if HandleSlashCommandWithDefaults(req.Message,
                func(resp string) {
                        // 异步模式：不立即发送，因为 Webhook 可能无法回复
                        // 仅记录
                        log.Printf("[Webhook] Response: %s", resp)
                },
                func() {
                        session.CancelTask()
                },
                func() {
                        log.Println("[Webhook] /exit ignored")
                }) {
                // 命令已处理，返回响应
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusOK)
                json.NewEncoder(w).Encode(WebhookResponse{
                        Status:  "command_processed",
                        Message: "Command executed",
                })
                return
        }

        // 普通消息，加入历史并启动任务
        session.AddToHistory("user", req.Message)

        // 根据配置决定同步或异步处理
        if wc.config.Async {
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusAccepted)
                json.NewEncoder(w).Encode(WebhookResponse{
                        Status:  "accepted",
                        Message: "message queued for processing",
                })
                go wc.processWebhookMessage(session, req.ChannelID, req.SenderID, req.Message, req.Metadata)
        } else {
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusOK)
                json.NewEncoder(w).Encode(WebhookResponse{
                        Status:  "received",
                        Message: "message processed",
                })
                go wc.processWebhookMessage(session, req.ChannelID, req.SenderID, req.Message, req.Metadata)
        }
}

// processWebhookMessage 处理 Webhook 消息（后台任务）
func (wc *WebhookChannel) processWebhookMessage(session *GlobalSession, channelID, senderID, input string, metadata map[string]interface{}) {
        ok, taskID := session.TryStartTask()
        if !ok {
                // 无法启动任务，记录日志
                log.Printf("[Webhook] Task already running for session %s", session.ID)
                return
        }
        taskCtx := session.GetTaskCtx()
        defer session.SetTaskRunning(false, taskID)

        // 创建会话输出通道（直接使用 WebhookChannel 自身）
        ch := wc

        // 获取当前历史
        history := session.GetHistory()

        // 执行 AgentLoop
        newHistory, err := AgentLoop(taskCtx, ch, history, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking)
        if err != nil && err != context.Canceled {
                log.Printf("[Webhook] AgentLoop error: %v", err)
        }
        if len(newHistory) > len(history) {
                session.SetHistory(newHistory)
        }
}

// Stop 停止 Webhook 服务
func (wc *WebhookChannel) Stop() {
        if wc.server != nil {
                wc.server.Close()
                log.Println("[Webhook] Server stopped.")
        }
}

// WriteChunk 写入（Webhook 是接收型渠道，通常不主动推送）
func (wc *WebhookChannel) WriteChunk(chunk StreamChunk) error {
        return nil
}

// SendToUser 向指定用户/渠道发送消息（通过回调 URL 或队列，需外部配置）
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

// GetListenAddr 返回监听地址（供外部使用）
func (wc *WebhookChannel) GetListenAddr() string {
        return wc.config.Listen
}

