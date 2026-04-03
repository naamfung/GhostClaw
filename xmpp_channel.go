// +build xmpp

// XMPP 渠道
// 通过 XMPP（Jabber）协议连接服务器，加入聊天室收发消息
// 依赖：mellium/xmpp
// 使用 go build -tags xmpp 来启用
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"mellium.im/jid"
	"mellium.im/sasl"
	"mellium.im/xmpp"
	"mellium.im/xmpp/muc"
	"mellium.im/xmpp/stanza"
)

// XMPPConfig XMPP 渠道配置
type XMPPConfig struct {
	Enabled     bool     `toon:"enabled" json:"Enabled"`
	Server      string   `toon:"server" json:"Server"`           // XMPP 服务器地址（如 talk.google.com:5222）
	Username    string   `toon:"username" json:"Username"`         // 用户名/ JID（如 bot@example.com）
	Password    string   `toon:"password" json:"Password"`         // 密码
	Resource    string   `toon:"resource" json:"Resource"`         // 资源标识，默认 "ghostclaw"
	Rooms       []string `toon:"rooms" json:"Rooms"`               // 自动加入的 MUC 房间
	UseTLS      bool     `toon:"use_tls" json:"UseTLS"`            // 是否启用 TLS
	InsecureTLS bool     `toon:"insecure_tls" json:"InsecureTLS"`  // 是否跳过 TLS 证书验证
	GroupPolicy string   `toon:"group_policy" json:"GroupPolicy"`   // 群聊策略：silent / active
	Nick        string   `toon:"nick" json:"Nick"`                 // MUC 昵称
}

// XMPPChannel 实现 Channel 接口
type XMPPChannel struct {
	*BaseChannel
	config         XMPPConfig
	client         *xmpp.Client
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

	// 创建客户端
	client, err := xmpp.NewClient(xc.config.Server, j, 
		xmpp.StreamLogger(log.New(os.Stdout, "[XMPP] ", log.LstdFlags)),
		xmpp.StartTLS(tlsConfig),
		xmpp.SASL(sasl.Plain(xc.config.Username, xc.config.Password)),
	)
	if err != nil {
		return fmt.Errorf("failed to create XMPP client: %w", err)
	}
	xc.client = client

	// 启动连接
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 加入 MUC 房间
	for _, room := range xc.config.Rooms {
		roomJID, err := jid.Parse(room)
		if err != nil {
			log.Printf("[XMPP] Invalid room JID %s: %v", room, err)
			continue
		}
		log.Printf("[XMPP] Joining room: %s as %s", room, xc.config.Nick)
		if _, err := muc.Join(ctx, client, roomJID, xc.config.Nick); err != nil {
			log.Printf("[XMPP] Failed to join room %s: %v", room, err)
		}
	}

	xc.connected = true
	log.Printf("[XMPP] Connected successfully")

	// 启动消息处理循环
	go xc.handleMessages(ctx)

	// 监听停止信号
	go func() {
		<-xc.stopCh
		cancel()
		if xc.client != nil {
			xc.client.Close()
		}
		xc.connected = false
		log.Println("[XMPP] Stopped.")
	}()

	return nil
}

// handleMessages 处理 XMPP 消息
func (xc *XMPPChannel) handleMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := xc.client.Receive(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("[XMPP] Error receiving message: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}

			switch m := msg.(type) {
			case stanza.Message:
				xc.handleMessage(m)
			}
		}
	}
}

// handleMessage 处理单条消息
func (xc *XMPPChannel) handleMessage(msg stanza.Message) {
	if msg.Body == "" {
		return
	}

	// 跳过自己发的消息
	if msg.From.Resourcepart() == xc.config.Resource {
		return
	}

	chatID := msg.From.String()
	senderID := msg.From.String()
	content := msg.Body

	// 检查是否是 MUC 消息
	if muc.IsMUC(msg.From) {
		chatID = msg.From.Bare().String()
		senderID = msg.From.Resourcepart()
	}

	// 检查是否应该响应
	isDirectMention := strings.Contains(strings.ToLower(content), strings.ToLower(xc.config.Nick))
	if !xc.shouldRespond(content, isDirectMention) {
		return
	}

	// 调用消息处理器
	if xc.messageHandler != nil {
		metadata := map[string]interface{}{
			"from":      msg.From.String(),
			"to":        msg.To.String(),
			"type":      msg.Type,
			"timestamp": time.Now(),
		}
		xc.messageHandler(chatID, senderID, content, metadata)
	}
}

// Stop 停止 XMPP 连接
func (xc *XMPPChannel) Stop() {
	close(xc.stopCh)
}

// WriteChunk 发送消息片段到 XMPP
func (xc *XMPPChannel) WriteChunk(chunk StreamChunk) error {
	if !xc.connected || xc.client == nil {
		return fmt.Errorf("XMPP not connected")
	}

	if chunk.Content == "" {
		return nil
	}

	// 解析目标 JID
	toJID, err := jid.Parse(chunk.SessionID)
	if err != nil {
		return fmt.Errorf("invalid session ID (JID): %w", err)
	}

	// 创建消息
	msg := stanza.Message{
		To:   toJID,
		Type: stanza.ChatMessage,
		Body: chunk.Content,
	}

	// 发送消息
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := xc.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// SendToUser 发送消息给指定用户/房间（实现 MessageSender）
func (xc *XMPPChannel) SendToUser(userID string, message string) error {
	if !xc.connected || xc.client == nil {
		return fmt.Errorf("XMPP not connected")
	}

	// 解析目标 JID
	toJID, err := jid.Parse(userID)
	if err != nil {
		return fmt.Errorf("invalid user ID (JID): %w", err)
	}

	// 创建消息
	msg := stanza.Message{
		To:   toJID,
		Type: stanza.ChatMessage,
		Body: message,
	}

	// 发送消息
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := xc.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	log.Printf("[XMPP] SendToUser to %s: %s", userID, truncateString(message, 100))
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

// shouldRespond 判断是否应该响应消息
func (xc *XMPPChannel) shouldRespond(content string, isDirectMention bool) bool {
	switch strings.ToLower(xc.config.GroupPolicy) {
	case "silent":
		return isDirectMention
	case "active":
		return true
	default:
		return isDirectMention
	}
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
		"server":  xc.config.Server,
		"username": xc.config.Username,
		"message": "XMPP channel health check",
	}
}

// GetSessionID 实现 Channel 接口
func (xc *XMPPChannel) GetSessionID() string {
	return ""
}
