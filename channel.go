package main

import (
    "strings"
    "sync"
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
}

// BaseChannel 提供基础实现，包含流式替换器
type BaseChannel struct {
    id                string
    mu                sync.Mutex // 用于 WriteChunk 的并发控制
    contentReplacer   *StreamReplacer
    reasoningReplacer *StreamReplacer
    contentBuffer     *strings.Builder
    reasoningBuffer   *strings.Builder
}

// NewBaseChannel 创建带有流式替换器的基础频道
func NewBaseChannel(id string) *BaseChannel {
    bc := &BaseChannel{
        id:              id,
        contentBuffer:   &strings.Builder{},
        reasoningBuffer: &strings.Builder{},
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
