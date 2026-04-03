// +build slack

// Slack 渠道支持
// 使用 go build -tags slack 来包含此渠道
package main

import (
        "context"
        "encoding/json"
        "fmt"
        "log"
        "net/http"
        "net/url"
        "strings"
        "sync"
        "time"

        "github.com/gorilla/websocket"
)

// SlackConfig Slack 频道配置
type SlackConfig struct {
    Enabled        bool     `toon:"Enabled" json:"Enabled"`
    BotToken       string   `toon:"BotToken" json:"BotToken"`
    AppToken       string   `toon:"AppToken" json:"AppToken"`
    AllowFrom      []string `toon:"AllowFrom" json:"AllowFrom"`
    ReplyInThread  bool     `toon:"ReplyInThread" json:"ReplyInThread"`
    ReactEmoji     string   `toon:"ReactEmoji" json:"ReactEmoji"`
    DoneEmoji      string   `toon:"DoneEmoji" json:"DoneEmoji"`
    GroupPolicy    string   `toon:"GroupPolicy" json:"GroupPolicy"` // "open", "mention", "allowlist"
    GroupAllowFrom []string `toon:"GroupAllowFrom" json:"GroupAllowFrom"`
}

// SlackChannel 实现 Slack 频道
type SlackChannel struct {
        *BaseChannel
        config     *SlackConfig
        ctx        context.Context
        cancel     context.CancelFunc
        wg         sync.WaitGroup
        handler    func(chatID, senderID, content string, metadata map[string]interface{})
        ws         *websocket.Conn
        httpClient *http.Client
        allowed    map[string]bool
        groupAllowed map[string]bool
        allowAll   bool
        botUserID  string
}

// Slack Socket Mode 消息结构
type SlackSocketMessage struct {
        Type    string          `json:"type"`
        EnvelopeID string       `json:"envelope_id,omitempty"`
        Payload json.RawMessage `json:"payload,omitempty"`
}

type SlackEventPayload struct {
        Event struct {
                Type        string `json:"type"`
                User        string `json:"user,omitempty"`
                Channel     string `json:"channel,omitempty"`
                Text        string `json:"text,omitempty"`
                Ts          string `json:"ts,omitempty"`
                ThreadTs    string `json:"thread_ts,omitempty"`
                ChannelType string `json:"channel_type,omitempty"`
                Subtype     string `json:"subtype,omitempty"`
        } `json:"event"`
}

// NewSlackChannel 创建 Slack 频道
func NewSlackChannel(config *SlackConfig) (*SlackChannel, error) {
        if config == nil || !config.Enabled {
                return nil, fmt.Errorf("slack channel not enabled")
        }

        if config.BotToken == "" || config.AppToken == "" {
                return nil, fmt.Errorf("slack bot_token and app_token are required")
        }

        ctx, cancel := context.WithCancel(context.Background())

        sc := &SlackChannel{
                BaseChannel:   NewBaseChannel("slack"),
                config:        config,
                ctx:           ctx,
                cancel:        cancel,
                httpClient:    &http.Client{Timeout: 30 * time.Second},
                allowed:       make(map[string]bool),
                groupAllowed:  make(map[string]bool),
        }

        // 解析权限列表
        for _, id := range config.AllowFrom {
                if id == "*" {
                        sc.allowAll = true
                } else {
                        sc.allowed[id] = true
                }
        }

        // 解析群组权限列表
        for _, id := range config.GroupAllowFrom {
                sc.groupAllowed[id] = true
        }

        // 设置默认值
        if config.GroupPolicy == "" {
                config.GroupPolicy = "mention"
        }
        if config.ReactEmoji == "" {
                config.ReactEmoji = "eyes"
        }
        if config.DoneEmoji == "" {
                config.DoneEmoji = "white_check_mark"
        }

        return sc, nil
}

// Start 启动 Slack Socket Mode
func (sc *SlackChannel) Start(onMessage func(chatID, senderID, content string, metadata map[string]interface{})) error {
        sc.handler = onMessage

        // 获取 Bot User ID
        if err := sc.getBotUserID(); err != nil {
                log.Printf("Warning: failed to get Slack bot user ID: %v", err)
        }

        sc.wg.Add(1)
        go func() {
                defer sc.wg.Done()
                for sc.ctx.Err() == nil {
                        if err := sc.connect(); err != nil {
                                log.Printf("Slack connection error: %v", err)
                                time.Sleep(5 * time.Second)
                        }
                }
        }()

        return nil
}

func (sc *SlackChannel) getBotUserID() error {
        apiURL := "https://slack.com/api/auth.test"
        req, _ := http.NewRequest("POST", apiURL, nil)
        req.Header.Set("Authorization", "Bearer "+sc.config.BotToken)
        req.Header.Set("Content-Type", "application/json")

        resp, err := sc.httpClient.Do(req)
        if err != nil {
                return err
        }
        defer resp.Body.Close()

        var result struct {
                OK     bool   `json:"ok"`
                UserID string `json:"user_id"`
                Error  string `json:"error,omitempty"`
        }

        if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
                return err
        }

        if !result.OK {
                return fmt.Errorf("slack auth.test failed: %s", result.Error)
        }

        sc.botUserID = result.UserID
        log.Printf("Slack bot connected as %s", sc.botUserID)
        return nil
}

func (sc *SlackChannel) connect() error {
        // 获取 WebSocket URL
        apiURL := "https://slack.com/api/apps.connections.open"
        req, _ := http.NewRequest("POST", apiURL, nil)
        req.Header.Set("Authorization", "Bearer "+sc.config.AppToken)
        req.Header.Set("Content-Type", "application/json")

        resp, err := sc.httpClient.Do(req)
        if err != nil {
                return fmt.Errorf("failed to get Slack WebSocket URL: %w", err)
        }
        defer resp.Body.Close()

        var wsResp struct {
                OK    bool   `json:"ok"`
                URL   string `json:"url"`
                Error string `json:"error,omitempty"`
        }

        if err := json.NewDecoder(resp.Body).Decode(&wsResp); err != nil {
                return err
        }

        if !wsResp.OK {
                return fmt.Errorf("slack apps.connections.open failed: %s", wsResp.Error)
        }

        log.Println("Connecting to Slack Socket Mode...")

        conn, _, err := websocket.DefaultDialer.Dial(wsResp.URL, nil)
        if err != nil {
                return fmt.Errorf("failed to connect to Slack WebSocket: %w", err)
        }
        sc.ws = conn
        defer conn.Close()

        log.Println("Slack Socket Mode connected")

        // 处理消息
        for {
                select {
                case <-sc.ctx.Done():
                        return nil
                default:
                        _, msg, err := conn.ReadMessage()
                        if err != nil {
                                return err
                        }

                        var socketMsg SlackSocketMessage
                        if err := json.Unmarshal(msg, &socketMsg); err != nil {
                                continue
                        }

                        sc.handleSocketMessage(&socketMsg)
                }
        }
}

func (sc *SlackChannel) handleSocketMessage(msg *SlackSocketMessage) {
        // 立即确认
        if msg.EnvelopeID != "" {
                ack := map[string]string{
                        "envelope_id": msg.EnvelopeID,
                }
                sc.ws.WriteJSON(ack)
        }

        if msg.Type != "events_api" {
                return
        }

        var payload SlackEventPayload
        if err := json.Unmarshal(msg.Payload, &payload); err != nil {
                return
        }

        event := payload.Event

        // 只处理消息事件
        if event.Type != "message" && event.Type != "app_mention" {
                return
        }

        // 忽略子类型消息（bot消息、编辑等）
        if event.Subtype != "" {
                return
        }

        // 忽略自己的消息
        if event.User == sc.botUserID {
                return
        }

        // 避免重复处理：Slack 会同时发送 message 和 app_mention
        if event.Type == "message" && strings.Contains(event.Text, fmt.Sprintf("<@%s>", sc.botUserID)) {
                return
        }

        // 权限检查
        if !sc.isAllowed(event.User, event.Channel, event.ChannelType) {
                return
        }

        // 群组消息策略
        if event.ChannelType != "im" && !sc.shouldRespondInChannel(event.Type, event.Text, event.Channel) {
                return
        }

        // 移除 Bot 提及
        text := sc.stripBotMention(event.Text)

        // 添加反应
        sc.addReaction(event.Channel, event.Ts, sc.config.ReactEmoji)

        // 确定线程
        threadTs := event.ThreadTs
        if sc.config.ReplyInThread && threadTs == "" {
                threadTs = event.Ts
        }

        metadata := map[string]interface{}{
                "message_id":    event.Ts,
                "user_id":       event.User,
                "channel_id":    event.Channel,
                "channel_type":  event.ChannelType,
                "thread_ts":     threadTs,
        }

        // 统一处理斜杠命令
        session := GetGlobalSession()
        if HandleSlashCommandWithDefaults(text,
                func(resp string) {
                        // 流式輸出命令回應
                        lines := strings.Split(resp, "\n")
                        for i, line := range lines {
                                if i > 0 {
                                        sc.sendMessage(event.Channel, "\n")
                                }
                                sc.sendMessage(event.Channel, line)
                        }
                },
                func() {
                        session.CancelTask()
                },
                func() {
                        log.Println("[Slack] /exit ignored in bot mode")
                }) {
                return
        }

        // 普通消息，加入历史并启动任务
        session.AddToHistory("user", text)
        go sc.processUserInput(session, event.Channel, event.User, text, metadata)

        // 更新反应
        if sc.config.DoneEmoji != "" {
                sc.removeReaction(event.Channel, event.Ts, sc.config.ReactEmoji)
                sc.addReaction(event.Channel, event.Ts, sc.config.DoneEmoji)
        }
}

// processUserInput 处理用户输入（后台任务）
func (sc *SlackChannel) processUserInput(session *GlobalSession, channelID, userID, input string, metadata map[string]interface{}) {
        ok, taskID := session.TryStartTask()
        if !ok {
                sc.sendMessage(channelID, "已有任务在执行中，请使用 /stop 取消后再试")
                return
        }
        taskCtx := session.GetTaskCtx()
        defer session.SetTaskRunning(false, taskID)

        // 创建会话输出通道（直接使用 SlackChannel 自身）
        ch := sc

        // 获取当前历史
        history := session.GetHistory()

        // 执行 AgentLoop
        newHistory, err := AgentLoop(taskCtx, ch, history, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking)
        if err != nil && err != context.Canceled {
                sc.sendMessage(channelID, fmt.Sprintf("❌ 错误: %v", err))
        }
        if len(newHistory) > len(history) {
                session.SetHistory(newHistory)
        }
}

func (sc *SlackChannel) isAllowed(userID, channelID, channelType string) bool {
        if channelType == "im" {
                if sc.allowAll {
                        return true
                }
                return sc.allowed[userID]
        }

        // 群组权限检查
        if sc.config.GroupPolicy == "allowlist" {
                return sc.groupAllowed[channelID]
        }

        return true
}

func (sc *SlackChannel) shouldRespondInChannel(eventType, text, channelID string) bool {
        // 使用全局群聊策略
        if globalGroupChatConfig != nil {
                return ShouldRespondInGroup(globalGroupChatConfig, channelID, text, sc.botUserID)
        }
        // 回退到原有逻辑
        switch sc.config.GroupPolicy {
        case "open":
                return true
        case "mention":
                if eventType == "app_mention" {
                        return true
                }
                return strings.Contains(text, fmt.Sprintf("<@%s>", sc.botUserID))
        case "allowlist":
                return sc.groupAllowed[channelID]
        }
        return false
}

func (sc *SlackChannel) stripBotMention(text string) string {
        if sc.botUserID == "" {
                return text
        }
        mention := fmt.Sprintf("<@%s>", sc.botUserID)
        text = strings.ReplaceAll(text, mention, "")
        return strings.TrimSpace(text)
}

func (sc *SlackChannel) addReaction(channel, ts, emoji string) {
        if emoji == "" {
                return
        }

        apiURL := "https://slack.com/api/reactions.add"
        data := url.Values{}
        data.Set("channel", channel)
        data.Set("timestamp", ts)
        data.Set("name", emoji)

        req, _ := http.NewRequest("POST", apiURL, strings.NewReader(data.Encode()))
        req.Header.Set("Authorization", "Bearer "+sc.config.BotToken)
        req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

        sc.httpClient.Do(req)
}

func (sc *SlackChannel) removeReaction(channel, ts, emoji string) {
        if emoji == "" {
                return
        }

        apiURL := "https://slack.com/api/reactions.remove"
        data := url.Values{}
        data.Set("channel", channel)
        data.Set("timestamp", ts)
        data.Set("name", emoji)

        req, _ := http.NewRequest("POST", apiURL, strings.NewReader(data.Encode()))
        req.Header.Set("Authorization", "Bearer "+sc.config.BotToken)
        req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

        sc.httpClient.Do(req)
}

// WriteChunk 发送消息（实现 Channel 接口）
func (sc *SlackChannel) WriteChunk(chunk StreamChunk) error {
        if chunk.Error != "" {
                log.Printf("Slack chunk error: %s", chunk.Error)
                return nil
        }

        // Slack 不支持流式，直接发送完整消息
        if chunk.Done && chunk.Content != "" {
                return sc.sendMessage(chunk.SessionID, chunk.Content)
        }

        return nil
}

func (sc *SlackChannel) sendMessage(channelID, content string) error {
        apiURL := "https://slack.com/api/chat.postMessage"

        // 转换 Markdown 到 Slack mrkdwn
        mrkdwn := markdownToSlackMrkdwn(content)

        data := url.Values{}
        data.Set("channel", channelID)
        data.Set("text", mrkdwn)

        req, _ := http.NewRequest("POST", apiURL, strings.NewReader(data.Encode()))
        req.Header.Set("Authorization", "Bearer "+sc.config.BotToken)
        req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

        resp, err := sc.httpClient.Do(req)
        if err != nil {
                return err
        }
        defer resp.Body.Close()

        var result struct {
                OK    bool   `json:"ok"`
                Error string `json:"error,omitempty"`
        }

        if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
                return err
        }

        if !result.OK {
                return fmt.Errorf("slack chat.postMessage failed: %s", result.Error)
        }

        return nil
}

// markdownToSlackMrkdwn 将 Markdown 转换为 Slack mrkdwn
func markdownToSlackMrkdwn(text string) string {
        // 处理粗体 **text** -> *text*
        text = strings.ReplaceAll(text, "**", "*")
        
        // 处理代码块（保留）
        // Slack 使用 ```code```
        
        // 处理链接 [text](url) -> <url|text>
        // 简化处理：保留原样

        return text
}

// Stop 停止 Slack Socket Mode
func (sc *SlackChannel) Stop() {
        sc.cancel()
        if sc.ws != nil {
                sc.ws.Close()
        }
        sc.wg.Wait()
}

// Close 实现 Channel 接口
func (sc *SlackChannel) Close() error {
        sc.Stop()
        return sc.BaseChannel.Close()
}

// ============================================================
// MessageSender 接口实现（用于消息总线）
// ============================================================

// SendToUser 发送消息给指定用户（实现 MessageSender 接口）
func (sc *SlackChannel) SendToUser(userID string, message string) error {
        // Slack 使用 channelID 发送消息
        return sc.sendMessage(userID, message)
}

// GetChannelType 获取渠道类型（实现 MessageSender 接口）
func (sc *SlackChannel) GetChannelType() string {
        return "slack"
}

// RegisterToBus 注册到消息总线
func (sc *SlackChannel) RegisterToBus() {
	if globalMessageBus != nil {
		globalMessageBus.RegisterChannelSender("slack", sc)
		log.Println("[Slack] Registered to message bus")
	}
}

// HealthCheck 健康检查
func (sc *SlackChannel) HealthCheck() map[string]interface{} {
	status := "disconnected"
	if sc.client != nil {
		status = "connected"
	}
	return map[string]interface{}{
		"id":      sc.id,
		"status":  status,
		"message": "Slack channel health check",
	}
}

// GetSessionID 实现 Channel 接口
func (sc *SlackChannel) GetSessionID() string {
	return ""
}

// IsConnected 检查 Slack 连接状态
func (sc *SlackChannel) IsConnected() bool {
	return sc.client != nil
}

func init() {
        log.Println("Slack channel support enabled")
}

