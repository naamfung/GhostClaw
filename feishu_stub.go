// +build !feishu

// 飞书/Lark 渠道存根
// 默认不包含飞书渠道，使用 go build -tags feishu 来启用
package main

import "fmt"

// FeishuConfig 飞书频道配置（存根）
type FeishuConfig struct {
    Enabled           bool     `toon:"Enabled" json:"Enabled"`
    AppID             string   `toon:"AppID" json:"AppID"`
    AppSecret         string   `toon:"AppSecret" json:"AppSecret"`
    EncryptKey        string   `toon:"EncryptKey" json:"EncryptKey"`
    VerificationToken string   `toon:"VerificationToken" json:"VerificationToken"`
    AllowFrom         []string `toon:"AllowFrom" json:"AllowFrom"`
    ReactEmoji        string   `toon:"ReactEmoji" json:"ReactEmoji"`
    GroupPolicy       string   `toon:"GroupPolicy" json:"GroupPolicy"`
    ReplyToMessage    bool     `toon:"ReplyToMessage" json:"ReplyToMessage"`
}

// FeishuChannel 飞书渠道（存根）
type FeishuChannel struct {
        *BaseChannel
}

// NewFeishuChannel 创建飞书渠道（存根）
func NewFeishuChannel(config *FeishuConfig) (*FeishuChannel, error) {
        return nil, fmt.Errorf("feishu channel not compiled in, rebuild with: go build -tags feishu")
}

// Start 启动（存根）
func (fc *FeishuChannel) Start(onMessage func(chatID, senderID, content string, metadata map[string]interface{})) error {
        return fmt.Errorf("feishu channel not compiled in")
}

// Stop 停止（存根）
func (fc *FeishuChannel) Stop() {}

// WriteChunk 写入（存根）
func (fc *FeishuChannel) WriteChunk(chunk StreamChunk) error {
        return fmt.Errorf("feishu channel not compiled in")
}

// SendToUser 发送消息给用户（存根）
func (fc *FeishuChannel) SendToUser(userID string, message string) error {
        return fmt.Errorf("feishu channel not compiled in")
}

// GetChannelType 获取渠道类型（存根）
func (fc *FeishuChannel) GetChannelType() string {
        return "feishu"
}

// RegisterToBus 注册到消息总线（存根）
func (fc *FeishuChannel) RegisterToBus() {}
