// +build !webhook

// Webhook 渠道存根
// 默认不包含 Webhook 渠道，使用 go build -tags webhook 来启用
package main

import "fmt"

// WebhookConfig Webhook 渠道配置（存根）
type WebhookConfig struct {
	Enabled       bool     `toon:"enabled" json:"Enabled"`
	Listen        string   `toon:"listen" json:"Listen"`
	Path          string   `toon:"path" json:"Path"`
	AllowedTokens []string `toon:"allowed_tokens" json:"AllowedTokens"`
	Async         bool     `toon:"async" json:"Async"`
	GroupPolicy   string   `toon:"group_policy" json:"GroupPolicy"`
}

// WebhookChannel Webhook 渠道（存根）
type WebhookChannel struct {
	*BaseChannel
}

// NewWebhookChannel 创建 Webhook 渠道（存根）
func NewWebhookChannel(config *WebhookConfig) (*WebhookChannel, error) {
	return nil, fmt.Errorf("webhook channel not compiled in, rebuild with: go build -tags webhook")
}

// Start 启动（存根）
func (wc *WebhookChannel) Start(onMessage func(chatID, senderID, content string, metadata map[string]interface{})) error {
	return fmt.Errorf("webhook channel not compiled in")
}

// Stop 停止（存根）
func (wc *WebhookChannel) Stop() {}

// WriteChunk 写入（存根）
func (wc *WebhookChannel) WriteChunk(chunk StreamChunk) error {
	return fmt.Errorf("webhook channel not compiled in")
}

// SendToUser 发送消息给用户（存根）
func (wc *WebhookChannel) SendToUser(userID string, message string) error {
	return fmt.Errorf("webhook channel not compiled in")
}

// GetChannelType 获取渠道类型（存根）
func (wc *WebhookChannel) GetChannelType() string {
	return "webhook"
}

// RegisterToBus 注册到消息总线（存根）
func (wc *WebhookChannel) RegisterToBus() {}
