package main

import (
	"fmt"
	"io"
	"os"
)

// CmdChannel 实现命令行频道
type CmdChannel struct {
	*BaseChannel
	writer io.Writer
}

// NewCmdChannel 创建命令行频道
func NewCmdChannel() *CmdChannel {
	return &CmdChannel{
		BaseChannel: NewBaseChannel("cmd"),
		writer:      os.Stdout,
	}
}

// WriteChunk 将数据块写入标准输出（经过流式替换处理）
func (c *CmdChannel) WriteChunk(chunk StreamChunk) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 应用流式字符串替换
	processed := c.ProcessChunkWithReplacement(chunk)

	if processed.Error != "" {
		fmt.Fprintf(c.writer, "Error: %s\n", processed.Error)
		return nil
	}
	if processed.Content != "" {
		fmt.Fprint(c.writer, processed.Content)
	}
	if processed.ReasoningContent != "" {
		fmt.Fprint(c.writer, processed.ReasoningContent)
	}
	if processed.Done {
		fmt.Fprintln(c.writer)
	}
	return nil
}
