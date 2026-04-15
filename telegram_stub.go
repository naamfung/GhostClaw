// +build !telegram

// Telegram 渠道存根
// 默认不包含 Telegram 渠道，使用 go build -tags telegram 来启用
package main

import "fmt"

// TelegramChannel Telegram 渠道（存根）
type TelegramChannel struct {
        *BaseChannel
}

// NewTelegramChannel 创建 Telegram 渠道（存根）
func NewTelegramChannel(config *TelegramConfig) (*TelegramChannel, error) {
        return nil, fmt.Errorf("telegram channel not compiled in, rebuild with: go build -tags telegram")
}

// Start 启动（存根）
func (tc *TelegramChannel) Start(onMessage func(chatID, senderID, content string, metadata map[string]interface{})) error {
        return fmt.Errorf("telegram channel not compiled in")
}

// Stop 停止（存根）
func (tc *TelegramChannel) Stop() {}

// WriteChunk 写入（存根）
func (tc *TelegramChannel) WriteChunk(chunk StreamChunk) error {
        return fmt.Errorf("telegram channel not compiled in")
}

// SendToUser 发送消息给用户（存根）
func (tc *TelegramChannel) SendToUser(userID string, message string) error {
        return fmt.Errorf("telegram channel not compiled in")
}

// GetChannelType 获取渠道类型（存根）
func (tc *TelegramChannel) GetChannelType() string {
        return "telegram"
}

// RegisterToBus 注册到消息总线（存根）
func (tc *TelegramChannel) RegisterToBus() {}
