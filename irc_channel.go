// +build irc

package main

import (
        "fmt"
        "log"
        "strings"
        "sync"
        "time"

        "github.com/lrstanley/girc"
)

// IRCConfig holds IRC connection configuration.
type IRCConfig struct {
        Enabled     bool     `toon:"enabled" json:"Enabled"`
        Server      string   `toon:"server" json:"Server"`
        Port        int      `toon:"port" json:"Port"`
        Nick        string   `toon:"nick" json:"Nick"`
        Password    string   `toon:"password" json:"Password"`
        Channels    []string `toon:"channels" json:"Channels"`
        UseTLS      bool     `toon:"use_tls" json:"UseTLS"`
        GroupPolicy string   `toon:"group_policy" json:"GroupPolicy"`
}

// IRCChannel implements the Channel interface for IRC.
type IRCChannel struct {
        *BaseChannel
        config         IRCConfig
        client         *girc.Client
        mu             sync.RWMutex
        stopCh         chan struct{}
        connected      bool
        messageHandler func(chatID, senderID, content string, metadata map[string]interface{})
}

// NewIRCChannel creates a new IRC channel instance.
func NewIRCChannel(config *IRCConfig) (*IRCChannel, error) {
        if config == nil {
                return nil, fmt.Errorf("IRC config is nil")
        }
        if config.Nick == "" {
                return nil, fmt.Errorf("IRC nick is required")
        }
        if config.Server == "" {
                return nil, fmt.Errorf("IRC server is required")
        }
        if config.Port == 0 {
                config.Port = 6667
                if config.UseTLS {
                        config.Port = 6697
                }
        }
        return &IRCChannel{
                BaseChannel: NewBaseChannel("irc"),
                config:      *config,
                stopCh:      make(chan struct{}),
        }, nil
}

// Start starts the IRC bot connection.
func (irc *IRCChannel) Start(messageHandler func(chatID, senderID, content string, metadata map[string]interface{})) error {
        irc.messageHandler = messageHandler
        log.Printf("[IRC] Starting IRC bot: %s@%s:%d", irc.config.Nick, irc.config.Server, irc.config.Port)

        // 创建 IRC 客户端
        client := girc.New(girc.Config{
                Server:      irc.config.Server,
                Port:        irc.config.Port,
                Nick:        irc.config.Nick,
                User:        irc.config.Nick,
                Name:        "GhostClaw AI Agent",
                SSL:         irc.config.UseTLS,
        })
        
        // 设置服务器密码（如果需要）
        if irc.config.Password != "" {
                client.Config.ServerPass = irc.config.Password
        }

        // 注册消息处理
        client.Handlers.Add(girc.PRIVMSG, irc.handleIRCMessage)
        client.Handlers.Add(girc.JOIN, func(c *girc.Client, e girc.Event) {
                log.Printf("[IRC] Joined channel: %s", e.Params[0])
        })
        client.Handlers.Add(girc.CONNECTED, func(c *girc.Client, e girc.Event) {
                irc.connected = true
                log.Printf("[IRC] Connected to %s", irc.config.Server)
                
                // 加入频道
                for _, channel := range irc.config.Channels {
                        log.Printf("[IRC] Joining channel: %s", channel)
                        c.Cmd.Join(channel)
                }
        })
        client.Handlers.Add(girc.DISCONNECTED, func(c *girc.Client, e girc.Event) {
                irc.connected = false
                log.Printf("[IRC] Disconnected from %s", irc.config.Server)
        })

        irc.client = client

        // 启动连接
        go func() {
                if err := client.Connect(); err != nil {
                        log.Printf("[IRC] Connection error: %v", err)
                }
        }()

        // 监听停止信号
        go func() {
                <-irc.stopCh
                if irc.client != nil {
                        irc.client.Quit("GhostClaw shutting down")
                }
                irc.connected = false
                log.Println("[IRC] Stopped.")
        }()

        return nil
}

// handleIRCMessage 处理 IRC 消息
func (irc *IRCChannel) handleIRCMessage(c *girc.Client, e girc.Event) {
        // 跳过自己发的消息
        if e.Source.Name == irc.config.Nick {
                return
        }

        chatID := e.Params[0]
        senderID := e.Source.Name
        content := e.Last() // 获取消息内容（Params 的最后一个元素）

        // 检查是否是私聊
        if !strings.HasPrefix(chatID, "#") {
                chatID = senderID // 私聊时 chatID 设为发送者昵称
        }

        // 检查是否应该响应
        isDirectMention := strings.Contains(strings.ToLower(content), strings.ToLower(irc.config.Nick))
        if !irc.shouldRespond(content, isDirectMention) {
                return
        }

        // 调用消息处理器
        if irc.messageHandler != nil {
                metadata := map[string]interface{}{
                        "channel":   e.Params[0],
                        "sender":    e.Source.Name,
                        "timestamp": time.Now(),
                }
                irc.messageHandler(chatID, senderID, content, metadata)
        }
}

// Stop stops the IRC bot.
func (irc *IRCChannel) Stop() {
        close(irc.stopCh)
}

// WriteChunk sends a response chunk to IRC.
func (irc *IRCChannel) WriteChunk(chunk StreamChunk) error {
        if !irc.connected || irc.client == nil {
                return fmt.Errorf("IRC not connected")
        }

        if chunk.Content == "" {
                return nil
        }

        // 发送消息
        irc.client.Cmd.Message(chunk.SessionID, chunk.Content)
        return nil
}

// RegisterToBus registers the IRC channel with the message bus.
func (irc *IRCChannel) RegisterToBus() {
        if globalMessageBus != nil {
                globalMessageBus.RegisterChannelSender("irc", irc)
                log.Println("[IRC] Registered to message bus")
        }
}

// SendToUser sends a message to a specific IRC user/channel (implements MessageSender).
func (irc *IRCChannel) SendToUser(userID string, message string) error {
        if !irc.connected || irc.client == nil {
                return fmt.Errorf("IRC not connected")
        }

        // 发送消息
        irc.client.Cmd.Message(userID, message)
        log.Printf("[IRC] SendToUser to %s: %s", userID, TruncateString(message, 100))
        return nil
}

// GetChannelType returns the channel type (implements MessageSender).
func (irc *IRCChannel) GetChannelType() string {
        return "irc"
}

// SendMessage sends a message to a specific IRC channel.
func (irc *IRCChannel) SendMessage(target, message string) {
        irc.SendToUser(target, message)
}

// IsConnected returns whether the IRC connection is active.
func (irc *IRCChannel) IsConnected() bool {
        return irc.connected
}

// GetNick returns the configured IRC nickname.
func (irc *IRCChannel) GetNick() string {
        return irc.config.Nick
}

// shouldRespond determines if the bot should respond to a message.
func (irc *IRCChannel) shouldRespond(content string, isDirectMention bool) bool {
        switch strings.ToLower(irc.config.GroupPolicy) {
        case "silent":
                return isDirectMention
        case "active":
                return true
        default:
                return isDirectMention
        }
}

func (ic *IRCChannel) shouldRespondInGroup(channel, message string) bool {
        // 使用全局群聊策略
        if globalGroupChatConfig != nil {
                return ShouldRespondInGroup(globalGroupChatConfig, channel, message, ic.config.Nick)
        }
        // 回退
        policy := ic.config.GroupPolicy
        if policy == "" {
                return strings.Contains(message, "@"+ic.config.Nick)
        }
        switch policy {
        case "open":
                return true
        case "mention":
                return strings.Contains(message, "@"+ic.config.Nick)
        default:
                return false
        }
}

// HealthCheck 健康检查
func (irc *IRCChannel) HealthCheck() map[string]interface{} {
        status := "disconnected"
        if irc.connected {
                status = "connected"
        }
        return map[string]interface{}{
                "id":      irc.id,
                "status":  status,
                "server":  irc.config.Server,
                "nick":    irc.config.Nick,
                "message": "IRC channel health check",
        }
}

// GetSessionID 实现 Channel 接口
func (irc *IRCChannel) GetSessionID() string {
        return ""
}

