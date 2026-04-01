// +build xmpp

// XMPP 渠道
// 通过 XMPP（Jabber）协议连接服务器，加入聊天室收发消息
// 依赖：go-xmpp/xmpp 或 mellium/xmpp
// 使用 go build -tags xmpp 来启用
package main

import (
        "fmt"
        "log"
        "strings"
        "sync"
)

// XMPPConfig XMPP 渠道配置
type XMPPConfig struct {
        Enabled     bool     `toon:"enabled" json:"Enabled"`
        Server      string   `toon:"server" json:"Server"`           // XMPP 服务器地址（如 talk.google.com:5222）
        Username    string   `toon:"username" json:"Username"`         // 用户名/ JID（如 bot@example.com）
        Password    string   `toon:"password" json:"Password"`         // 密码
        Resource    string   `toon:"resource" json:"Resource"`         // 资源标识，默认 "garclaw"
        Rooms       []string `toon:"rooms" json:"Rooms"`               // 自动加入的 MUC 房间
        UseTLS      bool     `toon:"use_tls" json:"UseTLS"`            // 是否启用 TLS
        InsecureTLS bool     `toon:"insecure_tls" json:"InsecureTLS"`  // 是否跳过 TLS 证书验证
        GroupPolicy string   `toon:"group_policy" json:"GroupPolicy"`   // 群聊策略：silent / active
        Nick        string   `toon:"nick" json:"Nick"`                 // MUC 昵称
}

// XMPPChannel 实现 Channel 接口
type XMPPChannel struct {
        *BaseChannel
        config XMPPConfig
        mu     sync.RWMutex
        stopCh chan struct{}
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
                config.Resource = "garclaw"
        }
        if config.Nick == "" {
                config.Nick = "GarClaw"
        }
        return &XMPPChannel{
                BaseChannel: NewBaseChannel("xmpp"),
                config:      *config,
                stopCh:      make(chan struct{}),
        }, nil
}

// Start 启动 XMPP 连接
// TODO: 集成 mellium/xmpp 或 go-xmpp/xmpp 库实现实际连接
//
// 实现要点：
//   1. 使用 config.Username/Password 连接 XMPP 服务器
//   2. 自动加入 config.Rooms 中列出的 MUC 房间
//   3. 监听群组消息和私聊消息
//   4. 根据 GroupPolicy 决定是否响应
//   5. 调用 messageHandler 处理消息
func (xc *XMPPChannel) Start(messageHandler func(chatID, senderID, content string, metadata map[string]interface{})) error {
        log.Printf("[XMPP] Starting XMPP bot: %s@%s", xc.config.Username, xc.config.Server)

        // TODO: 实际的 XMPP 连接逻辑
        // 示例（使用 mellium/xmpp）:
        //
        //   addr := xc.config.Server
        //   jid := jid.MustParse(xc.config.Username)
        //   conn, err := xmpp.NewClient(addr, jid, t, xmpp.StartTLS(&tls.Config{InsecureSkipVerify: xc.config.InsecureTLS}))
        //   if err != nil { return err }
        //
        //   for _, room := range xc.config.Rooms {
        //       muc := xmpp.MUC{Address: jid.MustParse(room)}
        //       conn.JoinMUC(context.Background(), &muc, xc.config.Nick)
        //   }
        //
        //   for {
        //       stanza, err := conn.Receive()
        //       // 解析消息，调用 messageHandler
        //   }

        log.Printf("[XMPP] WARNING: XMPP channel is a stub. Full implementation requires adding an XMPP library dependency (e.g. mellium/xmpp).")

        go func() {
                <-xc.stopCh
                log.Println("[XMPP] Stopped.")
        }()

        return nil
}

// Stop 停止 XMPP 连接
func (xc *XMPPChannel) Stop() {
        close(xc.stopCh)
}

// WriteChunk 发送消息片段到 XMPP
func (xc *XMPPChannel) WriteChunk(chunk StreamChunk) error {
        // TODO: 将消息发送到对应的 MUC 房间或私聊
        log.Printf("[XMPP] WriteChunk: %s", truncateString(chunk.Content, 100))
        return nil
}

// SendToUser 发送消息给指定用户/房间（实现 MessageSender）
func (xc *XMPPChannel) SendToUser(userID string, message string) error {
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
        log.Printf("[XMPP] SendMessage to %s: %s", roomJID, truncateString(message, 100))
}

// IsConnected 返回连接状态
func (xc *XMPPChannel) IsConnected() bool {
        return false
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

