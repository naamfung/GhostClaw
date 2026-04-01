// +build matrix

// Matrix 渠道
// 通过 Matrix 协议（去中心化通讯）连接 Homeserver，加入房间收发消息
// 依赖：maunium.net/go/mautrix
// 使用 go build -tags matrix 来启用
package main

import (
        "fmt"
        "log"
        "strings"
        "sync"
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
        config MatrixConfig
        mu     sync.RWMutex
        stopCh chan struct{}
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
// TODO: 集成 mautrix/mautrix 库实现实际连接
//
// 实现要点：
//   1. 使用 mautrix.NewClient(homeserverURL, userID, accessToken) 创建客户端
//   2. 设置 DisplayName（如果配置了）
//   3. 加入 config.Rooms 中列出的房间
//   4. 启动 Syncer 监听事件
//   5. 对 m.event.EventMessage 类型事件调用 messageHandler
//   6. 根据 GroupPolicy 决定是否响应（检查是否被 @mention）
func (mc *MatrixChannel) Start(messageHandler func(chatID, senderID, content string, metadata map[string]interface{})) error {
        log.Printf("[Matrix] Starting Matrix bot: %s on %s", mc.config.UserID, mc.config.HomeserverURL)

        // TODO: 实际的 Matrix 连接逻辑
        // 示例（使用 mautrix）:
        //
        //   client, err := mautrix.NewClient(mc.config.HomeserverURL, mc.config.UserID, mc.config.AccessToken)
        //   if err != nil { return err }
        //
        //   if mc.config.DisplayName != "" {
        //       client.SetDisplayName(mc.config.DisplayName)
        //   }
        //
        //   syncer := mautrix.NewDefaultSyncer()
        //   syncer.OnEventType(mautrix.EventMessage, func(evt mautrix.Event) {
        //       // 跳过自己发的消息
        //       if evt.Sender == mc.config.UserID { return }
        //       content := evt.Content.AsMessage().Body
        //       chatID := evt.RoomID.String()
        //       senderID := evt.Sender.String()
        //       messageHandler(chatID, senderID, content, nil)
        //   })
        //   client.Syncer = syncer
        //
        //   for _, room := range mc.config.Rooms {
        //       _, _ = client.JoinRoomByID(room)
        //   }
        //
        //   go func() {
        //       err = client.Sync()
        //       if err != nil { log.Printf("[Matrix] Sync error: %v", err) }
        //   }()

        log.Printf("[Matrix] WARNING: Matrix channel is a stub. Full implementation requires adding mautrix library: go get maunium.net/go/mautrix")

        go func() {
                <-mc.stopCh
                log.Println("[Matrix] Stopped.")
        }()

        return nil
}

// Stop 停止 Matrix 连接
func (mc *MatrixChannel) Stop() {
        close(mc.stopCh)
}

// WriteChunk 发送消息片段到 Matrix 房间
func (mc *MatrixChannel) WriteChunk(chunk StreamChunk) error {
        // TODO: 将消息发送到对应的 Matrix 房间
        log.Printf("[Matrix] WriteChunk: %s", truncateString(chunk.Content, 100))
        return nil
}

// SendToUser 发送消息到指定房间（实现 MessageSender）
func (mc *MatrixChannel) SendToUser(roomID string, message string) error {
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
        log.Printf("[Matrix] SendMessage to %s: %s", roomID, truncateString(message, 100))
}

// IsConnected 返回连接状态
func (mc *MatrixChannel) IsConnected() bool {
        return false
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

