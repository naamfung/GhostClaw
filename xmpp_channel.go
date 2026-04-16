// +build xmpp

// XMPP 渠道简化实现
// 通过 XMPP（Jabber）协议连接服务器
// 依赖：mellium/xmpp
// 使用 go build -tags xmpp 来启用
package main

import (
        "context"
        "crypto/tls"
        "encoding/xml"
        "fmt"
        "log"
        "strings"
        "sync"
        "time"

        "mellium.im/sasl"
        "mellium.im/xmpp"
        "mellium.im/xmpp/jid"
        "mellium.im/xmlstream"
)

// XMPPConfig XMPP 渠道配置
type XMPPConfig struct {
        Enabled     bool     `toon:"enabled" json:"Enabled"`
        Server      string   `toon:"server" json:"Server"`           // XMPP 服务器地址
        Username    string   `toon:"username" json:"Username"`         // 用户名/JID
        Password    string   `toon:"password" json:"Password"`         // 密码
        Resource    string   `toon:"resource" json:"Resource"`         // 资源标识
        Rooms       []string `toon:"rooms" json:"Rooms"`               // 自动加入的房间
        InsecureTLS bool     `toon:"insecure_tls" json:"InsecureTLS"`  // 是否跳过 TLS 证书验证
        GroupPolicy string   `toon:"group_policy" json:"GroupPolicy"`   // 群聊策略
        Nick        string   `toon:"nick" json:"Nick"`                 // MUC 昵称
}

// XMPPChannel 实现 Channel 接口
type XMPPChannel struct {
        *BaseChannel
        config         XMPPConfig
        session        *xmpp.Session
        mu             sync.RWMutex
        stopCh         chan struct{}
        connected      bool
        messageHandler func(chatID, senderID, content string, metadata map[string]interface{})
}

// NewXMPPChannel 创建 XMPP 渠道
func NewXMPPChannel(config *XMPPConfig) (*XMPPChannel, error) {
        if config == nil {
                return nil, fmt.Errorf("xmpp config is nil")
        }
        if config.Username == "" {
                return nil, fmt.Errorf("xmpp username is required")
        }
        if config.Resource == "" {
                config.Resource = "ghostclaw"
        }
        if config.Nick == "" {
                config.Nick = "GhostClaw"
        }
        return &XMPPChannel{
                BaseChannel: NewBaseChannel("xmpp"),
                config:      *config,
                stopCh:      make(chan struct{}),
        }, nil
}

// Start 启动 XMPP 连接
func (xc *XMPPChannel) Start(messageHandler func(chatID, senderID, content string, metadata map[string]interface{})) error {
        xc.messageHandler = messageHandler
        log.Printf("[XMPP] Starting XMPP bot: %s@%s", xc.config.Username, xc.config.Server)

        // 解析 JID
        j, err := jid.Parse(xc.config.Username)
        if err != nil {
                return fmt.Errorf("invalid username/JID: %w", err)
        }

        // 配置 TLS
        tlsConfig := &tls.Config{
                InsecureSkipVerify: xc.config.InsecureTLS,
        }

        // 创建可取消的 context
        ctx, cancel := context.WithCancel(context.Background())

        // 创建 XMPP 会话
        session, err := xmpp.DialClientSession(ctx, j,
                xmpp.StartTLS(tlsConfig),
                xmpp.SASL("", xc.config.Password, sasl.Plain),
                xmpp.BindResource(),
        )
        if err != nil {
                return fmt.Errorf("failed to create XMPP session: %w", err)
        }
        xc.session = session
        xc.connected = true

        log.Printf("[XMPP] Connected successfully")

        // 启动消息处理循环
        go xc.handleMessages(ctx)

        // 监听停止信号
        go func() {
                <-xc.stopCh
                cancel()
                if xc.session != nil {
                        xc.session.Close()
                }
                xc.connected = false
                log.Println("[XMPP] Stopped.")
        }()

        return nil
}

// handleMessages 处理 XMPP 消息
func (xc *XMPPChannel) handleMessages(ctx context.Context) {
        // 使用 Serve 方法处理入站 XML
        err := xc.session.Serve(xmpp.HandlerFunc(func(t xmlstream.TokenReadEncoder, start *xml.StartElement) error {
                select {
                case <-ctx.Done():
                        return ctx.Err()
                default:
                }
                // 简化处理，实际应用需要解析 XML token
                return nil
        }))

        if err != nil && ctx.Err() == nil {
                log.Printf("[XMPP] Serve error: %v", err)
        }
}

// handleMessage 处理单条消息
func (xc *XMPPChannel) handleMessage(from, to, body string) {
        if body == "" {
                return
        }

        chatID := from
        senderID := from
        content := body

        // 检查是否应该响应
        isDirectMention := strings.Contains(strings.ToLower(content), strings.ToLower(xc.config.Nick))
        if !xc.shouldRespondToMessage(content, isDirectMention) {
                return
        }

        // 调用消息处理器
        if xc.messageHandler != nil {
                metadata := map[string]interface{}{
                        "from":      from,
                        "to":        to,
                        "timestamp": time.Now(),
                }
                xc.messageHandler(chatID, senderID, content, metadata)
        }
}

// shouldRespondToMessage 检查是否应该响应消息
func (xc *XMPPChannel) shouldRespondToMessage(content string, isDirectMention bool) bool {
        // 如果消息包含昵称，或者是私聊，则响应
        return isDirectMention || xc.config.GroupPolicy == "active"
}

// Stop 停止 XMPP 连接
func (xc *XMPPChannel) Stop() {
        close(xc.stopCh)
}

// WriteChunk 发送消息片段到 XMPP
func (xc *XMPPChannel) WriteChunk(chunk StreamChunk) error {
        if !xc.connected || xc.session == nil {
                return fmt.Errorf("XMPP not connected")
        }

        if chunk.Content == "" {
                return nil
        }

        // 解析目标 JID
        _, err := jid.Parse(chunk.SessionID)
        if err != nil {
                return fmt.Errorf("invalid session ID (JID): %w", err)
        }

        // 发送消息
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()

        // 创建 body payload
        bodyPayload := &struct {
                Body string `xml:"body"`
        }{Body: chunk.Content}

        _, err = xc.session.EncodeIQ(ctx, bodyPayload)
        if err != nil {
                return fmt.Errorf("failed to send message: %w", err)
        }

        return nil
}

// SendToUser 发送消息给指定用户/房间（实现 MessageSender）
func (xc *XMPPChannel) SendToUser(userID string, message string) error {
        if !xc.connected || xc.session == nil {
                return fmt.Errorf("XMPP not connected")
        }

        // 解析目标 JID
        _, err := jid.Parse(userID)
        if err != nil {
                return fmt.Errorf("invalid user ID (JID): %w", err)
        }

        // 发送消息
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()

        // 创建 body payload
        bodyPayload := &struct {
                Body string `xml:"body"`
        }{Body: message}

        _, err = xc.session.EncodeIQ(ctx, bodyPayload)
        if err != nil {
                return fmt.Errorf("failed to send message: %w", err)
        }

        log.Printf("[XMPP] SendToUser to %s: %s", userID, TruncateString(message, 100))
        return nil
}

// GetChannelType 获取渠道类型
func (xc *XMPPChannel) GetChannelType() string {
        return "xmpp"
}

// RegisterToBus 注册到消息总线
func (xc *XMPPChannel) RegisterToBus() {
        if globalMessageBus != nil {
                globalMessageBus.RegisterChannelSender("xmpp", xc)
                log.Println("[XMPP] Registered to message bus")
        }
}

// SendMessage 发送消息到指定 MUC 房间
func (xc *XMPPChannel) SendMessage(roomJID, message string) {
        xc.SendToUser(roomJID, message)
}

// IsConnected 返回连接状态
func (xc *XMPPChannel) IsConnected() bool {
        return xc.connected
}

// HealthCheck 健康检查
func (xc *XMPPChannel) HealthCheck() map[string]interface{} {
        status := "disconnected"
        if xc.connected {
                status = "connected"
        }
        return map[string]interface{}{
                "id":      xc.id,
                "status":  status,
                "message": "XMPP channel health check",
        }
}

// GetSessionID 实现 Channel 接口
func (xc *XMPPChannel) GetSessionID() string {
        return ""
}

func init() {
        log.Println("XMPP channel support enabled")
}
