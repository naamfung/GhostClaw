package main

import (
	"log"

	"github.com/gorilla/websocket"
)

// WSChannel 实现 WebSocket 频道
type WSChannel struct {
	*BaseChannel
	conn *websocket.Conn
}

// NewWSChannel 创建 WebSocket 频道
func NewWSChannel(conn *websocket.Conn) *WSChannel {
	return &WSChannel{
		BaseChannel: NewBaseChannel(conn.RemoteAddr().String()),
		conn:        conn,
	}
}

// WriteChunk 将数据块通过 WebSocket 发送 JSON（经过流式替换处理）
// 返回错误以便上层停止发送
func (wsc *WSChannel) WriteChunk(chunk StreamChunk) error {
	wsc.mu.Lock()
	defer wsc.mu.Unlock()

	// 应用流式字符串替换
	processed := wsc.ProcessChunkWithReplacement(chunk)

	err := wsc.Retry(func() error {
		err := wsc.conn.WriteJSON(processed)
		if err != nil {
			log.Printf("WebSocket write error (retrying): %v", err)
		}
		return err
	})

	if err != nil {
		log.Printf("WebSocket write failed after retries: %v", err)
	}
	return err
}

// Close 关闭 WebSocket 连接
func (wsc *WSChannel) Close() error {
	wsc.mu.Lock()
	defer wsc.mu.Unlock()
	return wsc.conn.Close()
}

// HealthCheck 健康检查
func (wsc *WSChannel) HealthCheck() map[string]interface{} {
	status := "disconnected"
	if wsc.conn != nil {
		status = "connected"
	}
	return map[string]interface{}{
		"id":      wsc.id,
		"status":  status,
		"message": "WebSocket channel health check",
	}
}

// GetSessionID 实现 Channel 接口
func (wsc *WSChannel) GetSessionID() string {
	return ""
}
