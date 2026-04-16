package main

import (
        "log"
        "time"

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
// 注意：WebSocket 是全雙工長連接，寫入失敗通常意味著連接已斷開，
// 重試無意義且會引入 1-3 秒的額外延遲（每次重試間隔 1 秒），
// 因此這裡直接寫入，不使用 Retry 機制。
func (wsc *WSChannel) WriteChunk(chunk StreamChunk) error {
        wsc.mu.Lock()
        defer wsc.mu.Unlock()

        // 应用流式字符串替换
        processed := wsc.ProcessChunkWithReplacement(chunk)

        // 设置写超時，避免寫入操作無限阻塞（WS 連接可能已半開）
        wsc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
        err := wsc.conn.WriteJSON(processed)
        // 重置寫超時
        wsc.conn.SetWriteDeadline(time.Time{})
        if err != nil {
                log.Printf("[WS-Write] conn %s write failed (content_len=%d, done=%v): %v", wsc.id, len(chunk.Content), chunk.Done, err)
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
