// +build matrix

// Matrix 渠道
// 通过 Matrix 协议（去中心化通讯）连接 Homeserver，加入房间收发消息
// 依赖：maunium.net/go/mautrix
// 使用 go build -tags matrix 来启用
package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// MatrixConfig Matrix 渠道配置
type MatrixConfig struct {
	Enabled       bool     `toon:"enabled" json:"Enabled"`
	HomeserverURL string   `toon:"homeserver_url" json:"HomeserverURL"` // Homeserver 地址（如 https://matrix.org）
	UserID        string   `toon:"user_id" json:"UserID"`               // 完整用户 ID（如 @bot:matrix.org）
	AccessToken   string   `toon:"access_token" json:"AccessToken"`     // 访问令牌
	DeviceID      string   `toon:"device_id" json:"DeviceID"`           // 设备 ID，默认 "GARCLAW"
	Rooms         []string `toon:"rooms" json:"Rooms"`                  // 自动加入的房间 ID（如 !roomid:matrix.org）
	GroupPolicy   string   `toon:"group_policy" json:"GroupPolicy"`     // 群聊策略：silent / active
	DisplayName   string   `toon:"display_name" json:"DisplayName"`     // Bot 显示名称
}

// MatrixChannel 实现 Channel 接口
type MatrixChannel struct {
	*BaseChannel
	config         MatrixConfig
	client         *mautrix.Client
	mu             sync.RWMutex
	stopCh         chan struct{}
	connected      bool
	messageHandler func(chatID, senderID, content string, metadata map[string]interface{})
}

// NewMatrixChannel 创建 Matrix 渠道
func NewMatrixChannel(config *MatrixConfig) (*MatrixChannel, error) {
	if config == nil {
		return nil, fmt.Errorf("matrix config is nil")
	}
	if config.HomeserverURL == "" {
		return nil, fmt.Errorf("matrix homeserver_url is required")
	}
	if config.UserID == "" {
		return nil, fmt.Errorf("matrix user_id is required")
	}
	if config.AccessToken == "" {
		return nil, fmt.Errorf("matrix access_token is required")
	}
	if config.DeviceID == "" {
		config.DeviceID = "GARCLAW"
	}
	return &MatrixChannel{
		BaseChannel: NewBaseChannel("matrix"),
		config:      *config,
		stopCh:      make(chan struct{}),
	}, nil
}

// Start 启动 Matrix 连接
func (mc *MatrixChannel) Start(messageHandler func(chatID, senderID, content string, metadata map[string]interface{})) error {
	mc.messageHandler = messageHandler
	log.Printf("[Matrix] Starting Matrix bot: %s on %s", mc.config.UserID, mc.config.HomeserverURL)

	// 创建客户端（UserID 直接使用字符串转换）
	userID := id.UserID(mc.config.UserID)
	client, err := mautrix.NewClient(mc.config.HomeserverURL, userID, mc.config.AccessToken)
	if err != nil {
		return fmt.Errorf("failed to create Matrix client: %w", err)
	}
	mc.client = client

	// 设置显示名称
	if mc.config.DisplayName != "" {
		if err := client.SetDisplayName(context.Background(), mc.config.DisplayName); err != nil {
			log.Printf("[Matrix] Warning: failed to set display name: %v", err)
		}
	}

	// 创建 syncer
	syncer := client.Syncer.(*mautrix.DefaultSyncer)
	syncer.OnEventType(event.EventMessage, mc.handleMatrixMessage)

	// 加入房间
	for _, roomIDStr := range mc.config.Rooms {
		roomID := id.RoomID(roomIDStr)
		log.Printf("[Matrix] Joining room: %s", roomIDStr)
		if _, err := client.JoinRoomByID(context.Background(), roomID); err != nil {
			log.Printf("[Matrix] Failed to join room %s: %v", roomIDStr, err)
		}
	}

	mc.connected = true
	log.Printf("[Matrix] Connected successfully")

	// 启动 sync 循环
	go func() {
		if err := client.Sync(); err != nil {
			if !strings.Contains(err.Error(), "context canceled") {
				log.Printf("[Matrix] Sync error: %v", err)
			}
		}
	}()

	// 监听停止信号
	go func() {
		<-mc.stopCh
		if mc.client != nil {
			mc.client.StopSync()
		}
		mc.connected = false
		log.Println("[Matrix] Stopped.")
	}()

	return nil
}

// handleMatrixMessage 处理 Matrix 消息
func (mc *MatrixChannel) handleMatrixMessage(ctx context.Context, evt *event.Event) {
	if evt.Type != event.EventMessage {
		return
	}

	// 跳过自己发的消息
	if evt.Sender.String() == mc.config.UserID {
		return
	}

	// 解析消息内容
	msgContent := evt.Content.AsMessage()
	if msgContent == nil || msgContent.Body == "" {
		return
	}

	chatID := evt.RoomID.String()
	senderID := evt.Sender.String()
	content := msgContent.Body

	// 检查是否应该响应
	isDirectMention := strings.Contains(strings.ToLower(content), strings.ToLower(mc.config.DisplayName))
	if !mc.shouldRespond(content, isDirectMention) {
		return
	}

	// 调用消息处理器
	if mc.messageHandler != nil {
		metadata := map[string]interface{}{
			"room_id":   evt.RoomID.String(),
			"event_id":  evt.ID.String(),
			"sender_id": evt.Sender.String(),
			"type":      evt.Type.String(),
			"timestamp": time.Now(),
		}
		mc.messageHandler(chatID, senderID, content, metadata)
	}
}

// Stop 停止 Matrix 连接
func (mc *MatrixChannel) Stop() {
	close(mc.stopCh)
}

// WriteChunk 发送消息片段到 Matrix 房间
func (mc *MatrixChannel) WriteChunk(chunk StreamChunk) error {
	if !mc.connected || mc.client == nil {
		return fmt.Errorf("Matrix not connected")
	}

	if chunk.Content == "" {
		return nil
	}

	// 解析房间 ID
	roomID := id.RoomID(chunk.SessionID)

	// 创建消息内容
	content := &event.MessageEventContent{
		Body:    chunk.Content,
		MsgType: event.MsgText,
	}

	// 发送消息
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := mc.client.SendMessageEvent(ctx, roomID, event.EventMessage, content); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// SendToUser 发送消息到指定房间（实现 MessageSender）
func (mc *MatrixChannel) SendToUser(roomID string, message string) error {
	if !mc.connected || mc.client == nil {
		return fmt.Errorf("Matrix not connected")
	}

	// 解析房间 ID
	rid := id.RoomID(roomID)

	// 创建消息内容
	content := &event.MessageEventContent{
		Body:    message,
		MsgType: event.MsgText,
	}

	// 发送消息
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := mc.client.SendMessageEvent(ctx, rid, event.EventMessage, content); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	log.Printf("[Matrix] SendToUser to %s: %s", roomID, truncateString(message, 100))
	return nil
}

// GetChannelType 获取渠道类型
func (mc *MatrixChannel) GetChannelType() string {
	return "matrix"
}

// RegisterToBus 注册到消息总线
func (mc *MatrixChannel) RegisterToBus() {
	if globalMessageBus != nil {
		globalMessageBus.RegisterChannelSender("matrix", mc)
		log.Println("[Matrix] Registered to message bus")
	}
}

// SendMessage 发送消息到指定房间
func (mc *MatrixChannel) SendMessage(roomID, message string) {
	mc.SendToUser(roomID, message)
}

// IsConnected 返回连接状态
func (mc *MatrixChannel) IsConnected() bool {
	return mc.connected
}

// shouldRespond 判断是否应该响应消息
func (mc *MatrixChannel) shouldRespond(content string, isDirectMention bool) bool {
	switch strings.ToLower(mc.config.GroupPolicy) {
	case "silent":
		return isDirectMention
	case "active":
		return true
	default:
		return isDirectMention
	}
}

// HealthCheck 健康检查
func (mc *MatrixChannel) HealthCheck() map[string]interface{} {
	status := "disconnected"
	if mc.connected {
		status = "connected"
	}
	return map[string]interface{}{
		"id":         mc.id,
		"status":     status,
		"homeserver": mc.config.HomeserverURL,
		"user_id":    mc.config.UserID,
		"message":    "Matrix channel health check",
	}
}

// GetSessionID 实现 Channel 接口
func (mc *MatrixChannel) GetSessionID() string {
	return ""
}
