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
	phase  int // 0=未開始, 1=thinking, 2=content
}

const (
	phaseNone     = 0
	phaseThinking = 1
	phaseContent  = 2
)

// NewCmdChannel 创建命令行频道（输出到 os.Stdout）
func NewCmdChannel() *CmdChannel {
	return &CmdChannel{
		BaseChannel: NewBaseChannel("cmd"),
		writer:      os.Stdout,
	}
}

// NewCmdChannelWithWriter 创建命令行频道，使用指定的 writer
// 用於 REPL 模式下 os.Stdout 已被重定向到日誌檔案時，仍能輸出到終端
func NewCmdChannelWithWriter(w io.Writer) *CmdChannel {
	return &CmdChannel{
		BaseChannel: NewBaseChannel("cmd"),
		writer:      w,
	}
}

// agenticTagMarkers 需要从输出中移除的 agentic 标记列表
var agenticTagMarkers = []string{
	"<<<AGENTIC_TOOL_CALL_START>>>",
	"<<<AGENTIC_TOOL_CALL_END>>>",
	"<<<TOOL_NAME:",
	"<<<TOOL_ARGS_START>>>",
	"<<<TOOL_ARGS_END>>>",
	"<<<reasoning_content_start>>>",
	"<<<reasoning_content_end>>>",
}

// stripAgenticTags 从文本中移除所有 agentic 标记
func stripAgenticTags(text string) string {
	result := text
	for _, marker := range agenticTagMarkers {
		result = strings.ReplaceAll(result, marker, "")
	}
	if idx := strings.Index(result, "<<<TOOL_NAME:"); idx >= 0 {
		if endIdx := strings.Index(result[idx:], ">>>"); endIdx >= 0 {
			result = result[:idx] + result[idx+endIdx+3:]
		}
	}
	return result
}

// WriteChunk 将数据块写入标准输出
//
// 直接使用 ProcessChunkWithReplacement 輸出，不作額外積累或 \n 剝離。
// 僅在思考/正文切換時加入 [思考]/[正文] 標記同 \r\n 分隔。
func (c *CmdChannel) WriteChunk(chunk StreamChunk) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	processed := c.ProcessChunkWithReplacement(chunk)

	if processed.Error != "" {
		fmt.Fprintf(c.writer, "\r\nError: %s\r\n", processed.Error)
		c.phase = phaseNone
		return nil
	}

	var output strings.Builder

	// 思考 → 直接流式輸出
	if processed.ReasoningContent != "" {
		if c.phase == phaseContent {
			output.WriteString("\r\n") // 結束正文區塊
		}
		if c.phase != phaseThinking {
			output.WriteString("[思考] ")
			c.phase = phaseThinking
		}
		output.WriteString(stripAgenticTags(processed.ReasoningContent))
	}

	// 正文 → 直接流式輸出
	if processed.Content != "" {
		if c.phase == phaseThinking {
			output.WriteString("\r\n") // 結束思考區塊
		}
		if c.phase != phaseContent {
			output.WriteString("[正文] ")
			c.phase = phaseContent
		}
		output.WriteString(stripAgenticTags(processed.Content))
	}

	if processed.Done {
		output.WriteString("\r\n")
		c.phase = phaseNone
	}

	if output.Len() > 0 {
		fmt.Fprint(c.writer, output.String())
	}
	return nil
}
