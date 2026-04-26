// +build !xmpp

// XMPP 渠道存根
// 默认不包含 XMPP 渠道，使用 go build -tags xmpp 来启用
package main

import "fmt"

// XMPPConfig XMPP 渠道配置（存根）
type XMPPConfig struct {
	Enabled     bool     `toon:"enabled" json:"Enabled"`
	Server      string   `toon:"server" json:"Server"`
	Username    string   `toon:"username" json:"Username"`
	Password    string   `toon:"password" json:"Password"`
	Resource    string   `toon:"resource" json:"Resource"`
	Rooms       []string `toon:"rooms" json:"Rooms"`
	UseTLS      bool     `toon:"use_tls" json:"UseTLS"`
	InsecureTLS bool     `toon:"insecure_tls" json:"InsecureTLS"`
	GroupPolicy string   `toon:"group_policy" json:"GroupPolicy"`
	Nick        string   `toon:"nick" json:"Nick"`
}

// XMPPChannel XMPP 渠道（存根）
type XMPPChannel struct {
	*BaseChannel
}

// NewXMPPChannel 创建 XMPP 渠道（存根）
func NewXMPPChannel(config *XMPPConfig) (*XMPPChannel, error) {
	return nil, fmt.Errorf("xmpp channel not compiled in, rebuild with: go build -tags xmpp")
}

// Start 启动（存根）
func (xc *XMPPChannel) Start(onMessage func(chatID, senderID, content string, metadata map[string]interface{})) error {
	return fmt.Errorf("xmpp channel not compiled in")
}

// Stop 停止（存根）
func (xc *XMPPChannel) Stop() {}

// WriteChunk 写入（存根）
func (xc *XMPPChannel) WriteChunk(chunk StreamChunk) error {
	return fmt.Errorf("xmpp channel not compiled in")
}

// SendToUser 发送消息给用户（存根）
func (xc *XMPPChannel) SendToUser(userID string, message string) error {
	return fmt.Errorf("xmpp channel not compiled in")
}

// GetChannelType 获取渠道类型（存根）
func (xc *XMPPChannel) GetChannelType() string {
	return "xmpp"
}

// RegisterToBus 注册到消息总线（存根）
func (xc *XMPPChannel) RegisterToBus() {}
