// +build discord

// Discord 渠道支持
// 使用 go build -tags discord 来包含此渠道
package main

import (
        "context"
        "encoding/json"
        "fmt"
        "log"
        "net/http"
        "strings"
        "sync"
        "time"

        "github.com/gorilla/websocket"
)

// DiscordConfig Discord 频道配置
type DiscordConfig struct {
    Enabled     bool     `toon:"Enabled" json:"Enabled"`
    Token       string   `toon:"Token" json:"Token"`
    AllowFrom   []string `toon:"AllowFrom" json:"AllowFrom"`
    GatewayURL  string   `toon:"GatewayURL" json:"GatewayURL"`
    Intents     int      `toon:"Intents" json:"Intents"`
    GroupPolicy string   `toon:"GroupPolicy" json:"GroupPolicy"` // "open" or "mention"
}

// DiscordChannel 实现 Discord 频道
type DiscordChannel struct {
        *BaseChannel
        config       *DiscordConfig
        ctx          context.Context
        cancel       context.CancelFunc
        wg           sync.WaitGroup
        handler      func(chatID, senderID, content string, metadata map[string]interface{})
        ws           *websocket.Conn
        httpClient   *http.Client
        seq          int
        heartbeatMu  sync.Mutex
        heartbeatStop chan struct{}
        allowed      map[string]bool
        allowAll     bool
        botUserID    string
}

// Discord Gateway 事件结构
type DiscordGatewayMessage struct {
        Op   int             `json:"op"`
        D    json.RawMessage `json:"d,omitempty"`
        S    *int            `json:"s,omitempty"`
        T    string          `json:"t,omitempty"`
}

type DiscordHello struct {
        HeartbeatInterval int `json:"heartbeat_interval"`
}

type DiscordReady struct {
        User struct {
                ID       string `json:"id"`
                Username string `json:"username"`
        } `json:"user"`
}

type DiscordMessageCreate struct {
        ID        string `json:"id"`
        ChannelID string `json:"channel_id"`
        Author    struct {
                ID       string `json:"id"`
                Username string `json:"username"`
                Bot      bool   `json:"bot"`
        } `json:"author"`
        Content   string `json:"content"`
        GuildID   string `json:"guild_id,omitempty"`
        ReferencedMessage *struct {
                ID string `json:"id"`
        } `json:"referenced_message,omitempty"`
        Mentions []struct {
                ID string `json:"id"`
        } `json:"mentions,omitempty"`
}

// NewDiscordChannel 创建 Discord 频道
func NewDiscordChannel(config *DiscordConfig) (*DiscordChannel, error) {
        if config == nil || !config.Enabled {
                return nil, fmt.Errorf("discord channel not enabled")
        }

        if config.Token == "" {
                return nil, fmt.Errorf("discord token is required")
        }

        ctx, cancel := context.WithCancel(context.Background())

        dc := &DiscordChannel{
                BaseChannel:  NewBaseChannel("discord"),
                config:       config,
                ctx:          ctx,
                cancel:       cancel,
                httpClient:   &http.Client{Timeout: 30 * time.Second},
                allowed:      make(map[string]bool),
                heartbeatStop: make(chan struct{}),
        }

        // 解析权限列表
        for _, id := range config.AllowFrom {
                if id == "*" {
                        dc.allowAll = true
                } else {
                        dc.allowed[id] = true
                }
        }

        // 设置默认值
        if config.GatewayURL == "" {
                config.GatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"
        }
        if config.Intents == 0 {
                config.Intents = 37377 // GuildMessages + DirectMessages + MessageContent
        }
        if config.GroupPolicy == "" {
                config.GroupPolicy = "mention"
        }

        return dc, nil
}

// Start 启动 Discord Gateway 连接
func (dc *DiscordChannel) Start(onMessage func(chatID, senderID, content string, metadata map[string]interface{})) error {
        dc.handler = onMessage

        dc.wg.Add(1)
        go func() {
                defer dc.wg.Done()
                for dc.ctx.Err() == nil {
                        if err := dc.connect(); err != nil {
                                log.Printf("Discord connection error: %v", err)
                                time.Sleep(5 * time.Second)
                        }
                }
        }()

        return nil
}

func (dc *DiscordChannel) connect() error {
        log.Println("Connecting to Discord gateway...")

        conn, _, err := websocket.DefaultDialer.Dial(dc.config.GatewayURL, nil)
        if err != nil {
                return fmt.Errorf("failed to connect to Discord gateway: %w", err)
        }
        dc.ws = conn
        defer conn.Close()

        // 读取 Hello 消息
        _, msg, err := conn.ReadMessage()
        if err != nil {
                return err
        }

        var gwMsg DiscordGatewayMessage
        if err := json.Unmarshal(msg, &gwMsg); err != nil {
                return err
        }

        if gwMsg.Op != 10 {
                return fmt.Errorf("expected Hello opcode 10, got %d", gwMsg.Op)
        }

        var hello DiscordHello
        if err := json.Unmarshal(gwMsg.D, &hello); err != nil {
                return err
        }

        // 启动心跳
        heartbeatInterval := time.Duration(hello.HeartbeatInterval) * time.Millisecond
        dc.startHeartbeat(heartbeatInterval)

        // 发送 Identify
        identify := map[string]interface{}{
                "op": 2,
                "d": map[string]interface{}{
                        "token":   dc.config.Token,
                        "intents": dc.config.Intents,
                        "properties": map[string]string{
                                "os":      "ghostclaw",
                                "browser": "ghostclaw",
                                "device":  "ghostclaw",
                        },
                },
        }
        if err := conn.WriteJSON(identify); err != nil {
                return err
        }

        log.Println("Discord gateway connected")

        // 处理消息
        for {
                select {
                case <-dc.ctx.Done():
                        return nil
                default:
                        _, msg, err := conn.ReadMessage()
                        if err != nil {
                                return err
                        }

                        var gwMsg DiscordGatewayMessage
                        if err := json.Unmarshal(msg, &gwMsg); err != nil {
                                continue
                        }

                        if gwMsg.S != nil {
                                dc.seq = *gwMsg.S
                        }

                        switch gwMsg.Op {
                        case 0: // Dispatch
                                dc.handleDispatch(gwMsg.T, gwMsg.D)
                        case 7: // Reconnect
                                return fmt.Errorf("gateway requested reconnect")
                        case 9: // Invalid Session
                                return fmt.Errorf("invalid session")
                        case 11: // Heartbeat ACK
                                // OK
                        }
                }
        }
}

func (dc *DiscordChannel) startHeartbeat(interval time.Duration) {
        dc.heartbeatMu.Lock()
        defer dc.heartbeatMu.Unlock()

        select {
        case <-dc.heartbeatStop:
                // Already stopped
        default:
                close(dc.heartbeatStop)
        }
        dc.heartbeatStop = make(chan struct{})

        go func() {
                ticker := time.NewTicker(interval)
                defer ticker.Stop()

                for {
                        select {
                        case <-dc.heartbeatStop:
                                return
                        case <-ticker.C:
                                if dc.ws != nil {
                                        heartbeat := map[string]interface{}{
                                                "op": 1,
                                                "d":  dc.seq,
                                        }
                                        dc.ws.WriteJSON(heartbeat)
                                }
                        }
                }
        }()
}

func (dc *DiscordChannel) handleDispatch(eventType string, data json.RawMessage) {
        switch eventType {
        case "READY":
                var ready DiscordReady
                if err := json.Unmarshal(data, &ready); err != nil {
                        return
                }
                dc.botUserID = ready.User.ID
                log.Printf("Discord bot connected as %s (%s)", ready.User.Username, ready.User.ID)

        case "MESSAGE_CREATE":
                var msg DiscordMessageCreate
                if err := json.Unmarshal(data, &msg); err != nil {
                        return
                }

                // 忽略机器人消息
                if msg.Author.Bot {
                        return
                }

                // 权限检查
                if !dc.isAllowed(msg.Author.ID) {
                        return
                }

                // 群组消息策略
                if msg.GuildID != "" && !dc.shouldRespondInGroup(&msg) {
                        return
                }

                // 发送 typing
                dc.sendTyping(msg.ChannelID)

                // 回复消息
                var replyTo string
                if msg.ReferencedMessage != nil {
                        replyTo = msg.ReferencedMessage.ID
                }

                metadata := map[string]interface{}{
                        "message_id": msg.ID,
                        "user_id":    msg.Author.ID,
                        "username":   msg.Author.Username,
                        "guild_id":   msg.GuildID,
                        "reply_to":   replyTo,
                }

                // 统一处理斜杠命令
                session := GetGlobalSession()
                if HandleSlashCommandWithDefaults(msg.Content,
                        func(resp string) {
                                // 流式輸出命令回應
                                lines := strings.Split(resp, "\n")
                                for i, line := range lines {
                                        if i > 0 {
                                                dc.sendMessage(msg.ChannelID, "\n")
                                        }
                                        dc.sendMessage(msg.ChannelID, line)
                                }
                        },
                        func() {
                                session.CancelTask()
                        },
                        func() {
                                log.Println("[Discord] /exit ignored in bot mode")
                        }) {
                        return
                }

                // 普通消息，加入历史并启动任务
                session.AddToHistory("user", msg.Content)
                go dc.processUserInput(session, msg.ChannelID, msg.Author.ID, msg.Content, metadata)
        }
}

// processUserInput 处理用户输入（后台任务）
func (dc *DiscordChannel) processUserInput(session *GlobalSession, channelID, userID, input string, metadata map[string]interface{}) {
        ok, taskID := session.TryStartTask()
        if !ok {
                dc.sendMessage(channelID, "已有任务在执行中，请使用 /stop 取消后再试")
                return
        }
        taskCtx := session.GetTaskCtx()
        defer session.SetTaskRunning(false, taskID)

        // 创建会话输出通道（直接使用 DiscordChannel 自身）
        ch := dc

        // 获取当前历史
        history := session.GetHistory()

        // 执行 AgentLoop
        newHistory, err := AgentLoop(taskCtx, ch, history, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking)
        if err != nil && err != context.Canceled {
                dc.sendMessage(channelID, fmt.Sprintf("❌ 错误: %v", err))
        }
        if len(newHistory) > len(history) {
                session.SetHistory(newHistory)
        }
}

func (dc *DiscordChannel) isAllowed(userID string) bool {
        if dc.allowAll {
                return true
        }
        return dc.allowed[userID]
}

func (dc *DiscordChannel) shouldRespondInGroup(msg *DiscordMessageCreate) bool {
        // 使用全局群聊策略
        if globalGroupChatConfig != nil {
                return ShouldRespondInGroup(globalGroupChatConfig,
                        msg.GuildID,
                        msg.Content,
                        dc.botUserID)
        }
        // 回退
        if dc.config.GroupPolicy == "open" {
                return true
        }
        for _, mention := range msg.Mentions {
                if mention.ID == dc.botUserID {
                        return true
                }
        }
        mention1 := fmt.Sprintf("<@%s>", dc.botUserID)
        mention2 := fmt.Sprintf("<@!%s>", dc.botUserID)
        return strings.Contains(msg.Content, mention1) || strings.Contains(msg.Content, mention2)
}

func (dc *DiscordChannel) sendTyping(channelID string) {
        url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/typing", channelID)
        req, _ := http.NewRequest("POST", url, nil)
        req.Header.Set("Authorization", "Bot "+dc.config.Token)
        dc.httpClient.Do(req)
}

// WriteChunk 发送消息（实现 Channel 接口）
func (dc *DiscordChannel) WriteChunk(chunk StreamChunk) error {
        if chunk.Error != "" {
                log.Printf("Discord chunk error: %s", chunk.Error)
                return nil
        }

        // Discord 不支持流式，直接发送完整消息
        if chunk.Done && chunk.Content != "" {
                messages := dc.splitMessage(chunk.Content, 2000)
                for _, msg := range messages {
                        if err := dc.sendMessage(chunk.SessionID, msg); err != nil {
                                return err
                        }
                }
        }

        return nil
}

func (dc *DiscordChannel) sendMessage(channelID, content string) error {
        url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channelID)

        payload := map[string]string{"content": content}
        body, _ := json.Marshal(payload)

        req, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
        if err != nil {
                return err
        }

        req.Header.Set("Authorization", "Bot "+dc.config.Token)
        req.Header.Set("Content-Type", "application/json")

        resp, err := dc.httpClient.Do(req)
        if err != nil {
                return err
        }
        defer resp.Body.Close()

        if resp.StatusCode == 429 {
                // Rate limited
                var rateLimit struct {
                        RetryAfter float64 `json:"retry_after"`
                }
                json.NewDecoder(resp.Body).Decode(&rateLimit)
                time.Sleep(time.Duration(rateLimit.RetryAfter * float64(time.Second)))
                return dc.sendMessage(channelID, content)
        }

        if resp.StatusCode >= 400 {
                return fmt.Errorf("discord API error: %d", resp.StatusCode)
        }

        return nil
}

func (dc *DiscordChannel) splitMessage(text string, maxLen int) []string {
        if len(text) <= maxLen {
                return []string{text}
        }

        var messages []string
        var current strings.Builder

        lines := strings.Split(text, "\n")
        for _, line := range lines {
                if current.Len()+len(line)+1 > maxLen {
                        if current.Len() > 0 {
                                messages = append(messages, current.String())
                                current.Reset()
                        }
                }
                current.WriteString(line)
                current.WriteString("\n")
        }

        if current.Len() > 0 {
                messages = append(messages, current.String())
        }

        return messages
}

// Stop 停止 Discord Gateway 连接
func (dc *DiscordChannel) Stop() {
        dc.cancel()
        close(dc.heartbeatStop)
        if dc.ws != nil {
                dc.ws.Close()
        }
        dc.wg.Wait()
}

// Close 实现 Channel 接口
func (dc *DiscordChannel) Close() error {
        dc.Stop()
        return dc.BaseChannel.Close()
}

// ============================================================
// MessageSender 接口实现（用于消息总线）
// ============================================================

// SendToUser 发送消息给指定用户（实现 MessageSender 接口）
func (dc *DiscordChannel) SendToUser(userID string, message string) error {
        // Discord 使用 channelID 发送消息
        // 如果 userID 是 channelID，直接发送
        return dc.sendMessage(userID, message)
}

// GetChannelType 获取渠道类型（实现 MessageSender 接口）
func (dc *DiscordChannel) GetChannelType() string {
        return "discord"
}

// RegisterToBus 注册到消息总线
func (dc *DiscordChannel) RegisterToBus() {
	if globalMessageBus != nil {
		globalMessageBus.RegisterChannelSender("discord", dc)
		log.Println("[Discord] Registered to message bus")
	}
}

// HealthCheck 健康检查
func (dc *DiscordChannel) HealthCheck() map[string]interface{} {
	status := "disconnected"
	if dc.ws != nil {
		status = "connected"
	}
	return map[string]interface{}{
		"id":      dc.id,
		"status":  status,
		"message": "Discord channel health check",
	}
}

// GetSessionID 实现 Channel 接口
func (dc *DiscordChannel) GetSessionID() string {
	return ""
}

// IsConnected 检查 Discord 连接状态
func (dc *DiscordChannel) IsConnected() bool {
	return dc.ws != nil
}

func init() {
        log.Println("Discord channel support enabled")
}

