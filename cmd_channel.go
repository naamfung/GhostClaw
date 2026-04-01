package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// CmdChannel 实现命令行频道
type CmdChannel struct {
	*BaseChannel
	writer io.Writer
	// 行级緩衝：用於處理跨 chunk 的 [AUDIT] / agentic 標籤過濾
	lineBuf string
}

// NewCmdChannel 创建命令行频道
func NewCmdChannel() *CmdChannel {
	return &CmdChannel{
		BaseChannel: NewBaseChannel("cmd"),
		writer:      os.Stdout,
	}
}

// isAgenticLine 检查单行是否为 agentic 标签（前端专用，CLI 不显示）
func isAgenticLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "<<<AGENTIC_") ||
		strings.HasPrefix(trimmed, "<<<TOOL_NAME:") ||
		strings.HasPrefix(trimmed, "<<<TOOL_ARGS_") ||
		strings.HasPrefix(trimmed, "<<<reasoning_") ||
		trimmed == "<<<reasoning_content_start>>>" ||
		trimmed == "<<<reasoning_content_end>>>"
}

// isAuditLine 检查单行是否为审计日志（调试用，CLI 不显示）
func isAuditLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "[AUDIT]")
}

// shouldFilterLine 检查单行是否应该被过滤（CLI 不显示）
func shouldFilterLine(line string) bool {
	return isAgenticLine(line) || isAuditLine(line)
}

// filterContent 逐行过滤内容，移除 agentic 标签和审计日志行
// 使用缓冲区处理跨 chunk 的行分割
func (c *CmdChannel) filterContent(content string) string {
	c.lineBuf += content

	var result strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(c.lineBuf))

	for scanner.Scan() {
		line := scanner.Text()
		if !shouldFilterLine(line) {
			result.WriteString(line)
			result.WriteByte('\n')
		}
		// 被过滤的行直接丢弃
	}

	// 获取未完成的尾部（最后一行可能不完整）
	remaining := c.lineBuf
	if idx := strings.LastIndex(c.lineBuf, "\n"); idx >= 0 {
		remaining = c.lineBuf[idx+1:]
	}
	c.lineBuf = remaining

	return result.String()
}

// WriteChunk 将数据块写入标准输出（经过流式替换处理）
// CLI 不顯示 agentic 標籤（<<<AGENTIC_TOOL_CALL_START>>> 等），這些是前端專用的
// CLI 不顯示 [AUDIT] 審計日誌
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
		// 逐行過濾：移除 agentic 標籤行和 [AUDIT] 審計日誌行
		filtered := c.filterContent(processed.Content)
		if filtered != "" {
			fmt.Fprint(c.writer, filtered)
		}
	}
	if processed.ReasoningContent != "" {
		fmt.Fprint(c.writer, processed.ReasoningContent)
	}
	if processed.Done {
		// 處理緩衝區中剩餘的內容（可能是不完整的最後一行）
		if c.lineBuf != "" {
			if !shouldFilterLine(c.lineBuf) {
				fmt.Fprint(c.writer, c.lineBuf)
			}
			c.lineBuf = ""
		}
		fmt.Fprintln(c.writer)
	}
	return nil
}
