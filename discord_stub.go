// +build !discord

// Discord 渠道存根
// 默认不包含 Discord 渠道，使用 go build -tags discord 来启用
package main

import "fmt"

// DiscordConfig Discord 频道配置（存根）
type DiscordConfig struct {
    Enabled     bool     `toon:"Enabled" json:"Enabled"`
    Token       string   `toon:"Token" json:"Token"`
    AllowFrom   []string `toon:"AllowFrom" json:"AllowFrom"`
    GatewayURL  string   `toon:"GatewayURL" json:"GatewayURL"`
    Intents     int      `toon:"Intents" json:"Intents"`
    GroupPolicy string   `toon:"GroupPolicy" json:"GroupPolicy"`
}

// DiscordChannel Discord 渠道（存根）
type DiscordChannel struct {
        *BaseChannel
}

// NewDiscordChannel 创建 Discord 渠道（存根）
func NewDiscordChannel(config *DiscordConfig) (*DiscordChannel, error) {
        return nil, fmt.Errorf("discord channel not compiled in, rebuild with: go build -tags discord")
}

// Start 启动（存根）
func (dc *DiscordChannel) Start(onMessage func(chatID, senderID, content string, metadata map[string]interface{})) error {
        return fmt.Errorf("discord channel not compiled in")
}

// Stop 停止（存根）
func (dc *DiscordChannel) Stop() {}

// WriteChunk 写入（存根）
func (dc *DiscordChannel) WriteChunk(chunk StreamChunk) error {
        return fmt.Errorf("discord channel not compiled in")
}

// SendToUser 发送消息给用户（存根）
func (dc *DiscordChannel) SendToUser(userID string, message string) error {
        return fmt.Errorf("discord channel not compiled in")
}

// GetChannelType 获取渠道类型（存根）
func (dc *DiscordChannel) GetChannelType() string {
        return "discord"
}

// RegisterToBus 注册到消息总线（存根）
func (dc *DiscordChannel) RegisterToBus() {}
