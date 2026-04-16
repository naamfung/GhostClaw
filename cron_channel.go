package main

import (
    "log"
    "strings"
)

// LogChannel 将输出写入日志（流式按行输出）
type LogChannel struct {
    *BaseChannel
    lineBuffer strings.Builder
}

func NewLogChannel() *LogChannel {
    return &LogChannel{BaseChannel: NewBaseChannel("log")}
}

func (lc *LogChannel) WriteChunk(chunk StreamChunk) error {
    lc.mu.Lock()
    defer lc.mu.Unlock()

    // 应用流式字符串替换
    processed := lc.ProcessChunkWithReplacement(chunk)

    if processed.Error != "" {
        // 错误直接输出一行
        log.Printf("[Cron Log] Error: %s", processed.Error)
        return nil
    }

    // 处理普通内容
    if processed.Content != "" {
        lc.lineBuffer.WriteString(processed.Content)
    }
    if processed.ReasoningContent != "" {
        lc.lineBuffer.WriteString(processed.ReasoningContent)
    }

    // 检查是否包含换行符，按行输出
    fullText := lc.lineBuffer.String()
    lastNewline := strings.LastIndex(fullText, "\n")
    if lastNewline != -1 {
        // 有换行，输出到最后一个换行符之前的内容
        lines := strings.Split(fullText[:lastNewline+1], "\n")
        for _, line := range lines {
            if line != "" {
                log.Printf("[Cron Log] %s", line)
            }
        }
        // 保留未完成的部分
        remainder := fullText[lastNewline+1:]
        lc.lineBuffer.Reset()
        lc.lineBuffer.WriteString(remainder)
    }

    // 如果完成了，输出剩余内容
    if processed.Done && lc.lineBuffer.Len() > 0 {
        log.Printf("[Cron Log] %s", lc.lineBuffer.String())
        lc.lineBuffer.Reset()
    } else if processed.Done {
        log.Printf("[Cron Log] Task completed.")
    }

    return nil
}

// CompositeChannel 将输出同时发送到多个子 Channel
type CompositeChannel struct {
    *BaseChannel
    channels []Channel
}

func NewCompositeChannel(channels ...Channel) *CompositeChannel {
    return &CompositeChannel{
        BaseChannel: NewBaseChannel("composite"),
        channels:    channels,
    }
}

func (cc *CompositeChannel) WriteChunk(chunk StreamChunk) error {
    cc.mu.Lock()
    defer cc.mu.Unlock()
    for _, ch := range cc.channels {
        if err := ch.WriteChunk(chunk); err != nil {
            log.Printf("CompositeChannel: sub channel write error: %v", err)
        }
    }
    return nil
}

func (cc *CompositeChannel) Close() error {
    cc.mu.Lock()
    defer cc.mu.Unlock()
    for _, ch := range cc.channels {
        ch.Close()
    }
    return nil
}
