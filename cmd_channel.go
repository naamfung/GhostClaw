package main

import (
	"fmt"
	"io"
	"os"
	"strings"
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

// isAgenticContent 检查内容是否只包含 agentic 標籤（供前端解析，不應在 CLI 顯示）
func isAgenticContent(content string) bool {
	trimmed := strings.TrimSpace(content)
	return strings.HasPrefix(trimmed, "<<<AGENTIC_") ||
		strings.HasPrefix(trimmed, "<<<TOOL_NAME:") ||
		strings.HasPrefix(trimmed, "<<<TOOL_ARGS_") ||
		trimmed == "<<<reasoning_content_start>>>" ||
		trimmed == "<<<reasoning_content_end>>>"
}

// WriteChunk 将数据块写入标准输出（经过流式替换处理）
// CLI 不顯示 agentic 標籤（<<<AGENTIC_TOOL_CALL_START>>> 等），這些是前端專用的
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
		// 跳過 agentic 標籤行（前端專用，CLI 不需要顯示）
		if isAgenticContent(processed.Content) {
			return nil
		}
		// 跳過 [AUDIT] 開頭的行（調試審計日誌，不需要在 CLI 顯示）
		if strings.HasPrefix(processed.Content, "[AUDIT]") {
			return nil
		}
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
