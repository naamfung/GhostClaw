// +build !slack

// Slack 渠道存根
// 默认不包含 Slack 渠道，使用 go build -tags slack 来启用
package main

import "fmt"

// SlackConfig Slack 频道配置（存根）
type SlackConfig struct {
    Enabled        bool     `toon:"Enabled" json:"Enabled"`
    BotToken       string   `toon:"BotToken" json:"BotToken"`
    AppToken       string   `toon:"AppToken" json:"AppToken"`
    AllowFrom      []string `toon:"AllowFrom" json:"AllowFrom"`
    ReplyInThread  bool     `toon:"ReplyInThread" json:"ReplyInThread"`
    ReactEmoji     string   `toon:"ReactEmoji" json:"ReactEmoji"`
    DoneEmoji      string   `toon:"DoneEmoji" json:"DoneEmoji"`
    GroupPolicy    string   `toon:"GroupPolicy" json:"GroupPolicy"`
    GroupAllowFrom []string `toon:"GroupAllowFrom" json:"GroupAllowFrom"`
}

// SlackChannel Slack 渠道（存根）
type SlackChannel struct {
        *BaseChannel
}

// NewSlackChannel 创建 Slack 渠道（存根）
func NewSlackChannel(config *SlackConfig) (*SlackChannel, error) {
        return nil, fmt.Errorf("slack channel not compiled in, rebuild with: go build -tags slack")
}

// Start 启动（存根）
func (sc *SlackChannel) Start(onMessage func(chatID, senderID, content string, metadata map[string]interface{})) error {
        return fmt.Errorf("slack channel not compiled in")
}

// Stop 停止（存根）
func (sc *SlackChannel) Stop() {}

// WriteChunk 写入（存根）
func (sc *SlackChannel) WriteChunk(chunk StreamChunk) error {
        return fmt.Errorf("slack channel not compiled in")
}

// SendToUser 发送消息给用户（存根）
func (sc *SlackChannel) SendToUser(userID string, message string) error {
        return fmt.Errorf("slack channel not compiled in")
}

// GetChannelType 获取渠道类型（存根）
func (sc *SlackChannel) GetChannelType() string {
        return "slack"
}

// RegisterToBus 注册到消息总线（存根）
func (sc *SlackChannel) RegisterToBus() {}
