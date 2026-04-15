// +build !irc

// IRC 渠道存根
// 默认不包含 IRC 渠道，使用 go build -tags irc 来启用
package main

import "fmt"

// IRCConfig IRC 频道配置（存根）
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

// IRCChannel IRC 渠道（存根）
type IRCChannel struct {
	*BaseChannel
}

// NewIRCChannel 创建 IRC 渠道（存根）
func NewIRCChannel(config *IRCConfig) (*IRCChannel, error) {
	return nil, fmt.Errorf("irc channel not compiled in, rebuild with: go build -tags irc")
}

// Start 启动（存根）
func (irc *IRCChannel) Start(onMessage func(chatID, senderID, content string, metadata map[string]interface{})) error {
	return fmt.Errorf("irc channel not compiled in")
}

// Stop 停止（存根）
func (irc *IRCChannel) Stop() {}

// WriteChunk 写入（存根）
func (irc *IRCChannel) WriteChunk(chunk StreamChunk) error {
	return fmt.Errorf("irc channel not compiled in")
}

// SendToUser 发送消息给用户（存根）
func (irc *IRCChannel) SendToUser(userID string, message string) error {
	return fmt.Errorf("irc channel not compiled in")
}

// GetChannelType 获取渠道类型（存根）
func (irc *IRCChannel) GetChannelType() string {
	return "irc"
}

// RegisterToBus 注册到消息总线（存根）
func (irc *IRCChannel) RegisterToBus() {}
