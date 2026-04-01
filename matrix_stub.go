// +build !matrix

// Matrix 渠道存根
// 默认不包含 Matrix 渠道，使用 go build -tags matrix 来启用
package main

import "fmt"

// MatrixConfig Matrix 渠道配置（存根）
type MatrixConfig struct {
	Enabled       bool     `toon:"enabled" json:"Enabled"`
	HomeserverURL string   `toon:"homeserver_url" json:"HomeserverURL"`
	UserID        string   `toon:"user_id" json:"UserID"`
	AccessToken   string   `toon:"access_token" json:"AccessToken"`
	DeviceID      string   `toon:"device_id" json:"DeviceID"`
	Rooms         []string `toon:"rooms" json:"Rooms"`
	GroupPolicy   string   `toon:"group_policy" json:"GroupPolicy"`
	DisplayName   string   `toon:"display_name" json:"DisplayName"`
}

// MatrixChannel Matrix 渠道（存根）
type MatrixChannel struct {
	*BaseChannel
}

// NewMatrixChannel 创建 Matrix 渠道（存根）
func NewMatrixChannel(config *MatrixConfig) (*MatrixChannel, error) {
	return nil, fmt.Errorf("matrix channel not compiled in, rebuild with: go build -tags matrix")
}

// Start 启动（存根）
func (mc *MatrixChannel) Start(onMessage func(chatID, senderID, content string, metadata map[string]interface{})) error {
	return fmt.Errorf("matrix channel not compiled in")
}

// Stop 停止（存根）
func (mc *MatrixChannel) Stop() {}

// WriteChunk 写入（存根）
func (mc *MatrixChannel) WriteChunk(chunk StreamChunk) error {
	return fmt.Errorf("matrix channel not compiled in")
}

// SendToUser 发送消息给用户（存根）
func (mc *MatrixChannel) SendToUser(userID string, message string) error {
	return fmt.Errorf("matrix channel not compiled in")
}

// GetChannelType 获取渠道类型（存根）
func (mc *MatrixChannel) GetChannelType() string {
	return "matrix"
}

// RegisterToBus 注册到消息总线（存根）
func (mc *MatrixChannel) RegisterToBus() {}
