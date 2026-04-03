package main

import (
    "fmt"
    "log"
    "strings"
    "sync"
    "time"
)

// Channel 是所有前端频道的统一接口
type Channel interface {
    // WriteChunk 向客户端发送一个流式数据块
    WriteChunk(chunk StreamChunk) error
    // ID 返回频道的唯一标识
    ID() string
    // Close 关闭频道，释放资源
    Close() error
    // GetSessionID 返回关联的会话ID（可选，如果没有返回空字符串）
    GetSessionID() string
    // HealthCheck 健康检查，返回频道状态
    HealthCheck() map[string]interface{}
}

// ChannelError 统一的频道错误结构
type ChannelError struct {
	Code    string // 错误代码
	Message string // 错误消息
	Err     error  // 原始错误
}

// Error 实现 error 接口
func (e *ChannelError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap 实现 errors.Unwrap 接口
func (e *ChannelError) Unwrap() error {
	return e.Err
}

// NewChannelError 创建新的频道错误
func NewChannelError(code, message string, err error) *ChannelError {
	return &ChannelError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// RateLimiter 速率限制器
type RateLimiter struct {
	mu          sync.Mutex
	tokens      map[string][]time.Time // 用户ID -> 消息时间戳列表
	maxTokens   int                   // 最大消息数
	timeWindow  time.Duration         // 时间窗口
}

// NewRateLimiter 创建速率限制器
func NewRateLimiter(maxTokens int, timeWindow time.Duration) *RateLimiter {
	return &RateLimiter{
		tokens:     make(map[string][]time.Time),
		maxTokens:  maxTokens,
		timeWindow: timeWindow,
	}
}

// Allow 检查是否允许请求
func (rl *RateLimiter) Allow(userID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	
	// 清理过期的时间戳
	timestamps := rl.tokens[userID]
	var validTimestamps []time.Time
	for _, ts := range timestamps {
		if now.Sub(ts) < rl.timeWindow {
			validTimestamps = append(validTimestamps, ts)
		}
	}
	rl.tokens[userID] = validTimestamps

	// 检查是否超过限制
	if len(validTimestamps) >= rl.maxTokens {
		return false
	}

	// 添加当前时间戳
	rl.tokens[userID] = append(validTimestamps, now)
	return true
}

// BaseChannel 提供基础实现，包含流式替换器
type BaseChannel struct {
    id                string
    mu                sync.Mutex // 用于 WriteChunk 的并发控制
    contentReplacer   *StreamReplacer
    reasoningReplacer *StreamReplacer
    contentBuffer     *strings.Builder
    reasoningBuffer   *strings.Builder
    maxRetries        int           // 最大重试次数
    retryInterval     time.Duration // 重试间隔
    rateLimiter       *RateLimiter  // 速率限制器
}

// NewBaseChannel 创建带有流式替换器的基础频道
func NewBaseChannel(id string) *BaseChannel {
    bc := &BaseChannel{
        id:              id,
        contentBuffer:   &strings.Builder{},
        reasoningBuffer: &strings.Builder{},
        maxRetries:      3,      // 默认最大重试次数
        retryInterval:   1 * time.Second, // 默认重试间隔
        rateLimiter:     NewRateLimiter(10, 60*time.Second), // 默认：60秒内最多10条消息
    }
    // 创建 Content 的流式替换器
    bc.contentReplacer = NewStreamReplacer(func(r rune) {
        bc.contentBuffer.WriteRune(r)
    })
    // 创建 ReasoningContent 的流式替换器
    bc.reasoningReplacer = NewStreamReplacer(func(r rune) {
        bc.reasoningBuffer.WriteRune(r)
    })
    return bc
}

func (bc *BaseChannel) ID() string { return bc.id }

func (bc *BaseChannel) Close() error { return nil }

// GetSessionID 默认实现，返回空字符串
func (bc *BaseChannel) GetSessionID() string { return "" }

// HealthCheck 默认实现，返回基础状态
func (bc *BaseChannel) HealthCheck() map[string]interface{} {
    return map[string]interface{}{
        "id":      bc.id,
        "status":  "unknown",
        "message": "Base channel health check",
    }
}

// ProcessChunkWithReplacement 对 chunk 应用流式字符串替换（最长匹配）
// 返回处理后的新 chunk，不会修改原始 chunk
func (bc *BaseChannel) ProcessChunkWithReplacement(chunk StreamChunk) StreamChunk {
    result := StreamChunk{
        Done:         chunk.Done,
        Error:        chunk.Error,
        FinishReason: chunk.FinishReason,
        ToolCalls:    chunk.ToolCalls,
        SessionID:    chunk.SessionID,
        TaskRunning:  chunk.TaskRunning,
    }

    // 处理 Content
    if chunk.Content != "" {
        bc.contentReplacer.Write(chunk.Content)
        result.Content = bc.contentBuffer.String()
        bc.contentBuffer.Reset()
    }

    // 处理 ReasoningContent
    if chunk.ReasoningContent != "" {
        bc.reasoningReplacer.Write(chunk.ReasoningContent)
        result.ReasoningContent = bc.reasoningBuffer.String()
        bc.reasoningBuffer.Reset()
    }

    // 如果结束，刷新缓冲区
    if chunk.Done {
        bc.contentReplacer.Flush()
        if bc.contentBuffer.Len() > 0 {
            result.Content += bc.contentBuffer.String()
            bc.contentBuffer.Reset()
        }

        bc.reasoningReplacer.Flush()
        if bc.reasoningBuffer.Len() > 0 {
            result.ReasoningContent += bc.reasoningBuffer.String()
            bc.reasoningBuffer.Reset()
        }
    }

    return result
}

// ResetReplacers 重置替换器状态（用于新会话）
func (bc *BaseChannel) ResetReplacers() {
    bc.contentBuffer.Reset()
    bc.reasoningBuffer.Reset()
    // 重新创建替换器以清除缓冲区
    bc.contentReplacer = NewStreamReplacer(func(r rune) {
        bc.contentBuffer.WriteRune(r)
    })
    bc.reasoningReplacer = NewStreamReplacer(func(r rune) {
        bc.reasoningBuffer.WriteRune(r)
    })
}

// Retry 重试执行函数，直到成功或达到最大重试次数
func (bc *BaseChannel) Retry(f func() error) error {
    var err error
    for i := 0; i <= bc.maxRetries; i++ {
        if i > 0 {
            time.Sleep(bc.retryInterval)
        }
        if err = f(); err == nil {
            return nil
        }
    }
    return err
}

// SetRetryConfig 设置重试配置
func (bc *BaseChannel) SetRetryConfig(maxRetries int, retryInterval time.Duration) {
    bc.maxRetries = maxRetries
    bc.retryInterval = retryInterval
}

// SetRateLimitConfig 设置速率限制配置
func (bc *BaseChannel) SetRateLimitConfig(maxMessages int, timeWindow time.Duration) {
    bc.rateLimiter = NewRateLimiter(maxMessages, timeWindow)
}

// CheckRateLimit 检查速率限制
func (bc *BaseChannel) CheckRateLimit(userID string) bool {
    if bc.rateLimiter == nil {
        return true
    }
    return bc.rateLimiter.Allow(userID)
}

// NewError 创建频道错误
func (bc *BaseChannel) NewError(code, message string, err error) *ChannelError {
    return NewChannelError(code, message, err)
}

// HandleError 处理错误并记录日志
func (bc *BaseChannel) HandleError(err error, operation string) error {
    if err == nil {
        return nil
    }
    log.Printf("[%s] Error in %s: %v", bc.id, operation, err)
    return err
}

// LogInfo 记录信息级别的日志
func (bc *BaseChannel) LogInfo(format string, v ...interface{}) {
    log.Printf("[%s] INFO: "+format, append([]interface{}{bc.id}, v...)...)
}

// LogWarning 记录警告级别的日志
func (bc *BaseChannel) LogWarning(format string, v ...interface{}) {
    log.Printf("[%s] WARNING: "+format, append([]interface{}{bc.id}, v...)...)
}

// LogError 记录错误级别的日志
func (bc *BaseChannel) LogError(format string, v ...interface{}) {
    log.Printf("[%s] ERROR: "+format, append([]interface{}{bc.id}, v...)...)
}

// LogDebug 记录调试级别的日志
func (bc *BaseChannel) LogDebug(format string, v ...interface{}) {
    // 可以根据需要启用调试日志
    // log.Printf("[%s] DEBUG: "+format, append([]interface{}{bc.id}, v...)...)
}
