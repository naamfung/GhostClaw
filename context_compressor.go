package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// ContextCompressor 上下文压缩器
type ContextCompressor struct {
	thresholdPercent float64  // 触发压缩的阈值（默认 50%）
	protectFirstN    int      // 保护前 N 条消息
	tailTokenBudget  int      // 尾部 token 预算
	previousSummary  string   // 上次摘要，用于迭代更新
	compressionCount int      // 压缩次数
}

// NewContextCompressor 创建新的上下文压缩器
func NewContextCompressor() *ContextCompressor {
	return &ContextCompressor{
		thresholdPercent: 0.5,  // 50% 阈值
		protectFirstN:    3,     // 保护前 3 条消息
		tailTokenBudget:  20000, // 尾部 20K token 预算
		compressionCount: 0,
	}
}

// Compress 压缩消息列表
// 策略：保护头部 + 保护尾部（按 token 预算）+ 摘要中间
func (cc *ContextCompressor) Compress(messages []Message) []Message {
	if len(messages) <= MaxHistoryMessages {
		return messages
	}

	// 1. 保护头部（system + 前 N 条）
	head := make([]Message, 0)
	systemIndex := -1
	for i, msg := range messages {
		if msg.Role == "system" {
			systemIndex = i
			head = append(head, msg)
		} else if i > systemIndex && i <= systemIndex+cc.protectFirstN {
			head = append(head, msg)
		} else if i > systemIndex+cc.protectFirstN {
			break
		}
	}

	// 2. 保护尾部（按 token 预算，确保至少保留最新用户消息）
	tail := make([]Message, 0)
	latestUserIndex := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			latestUserIndex = i
			break
		}
	}

	// 确保最新用户消息在尾部
	if latestUserIndex > 0 {
		tail = append(tail, messages[latestUserIndex:]...)
	} else {
		tail = append(tail, messages[len(messages)-5:]...)
	}

	// 3. 中间部分生成结构化摘要
	middle := make([]Message, 0)
	if len(messages) > len(head)+len(tail) {
		middle = messages[len(head) : len(messages)-len(tail)]
	}

	// 4. 生成结构化摘要
	summary := cc.generateSummary(middle)
	
	// 5. 构建压缩后的消息列表
	compressed := make([]Message, 0)
	compressed = append(compressed, head...)
	
	// 添加摘要消息
		if summary != "" {
			summaryMsg := Message{
				Role:      "system",
				Content:   summary,
				Timestamp: time.Now().Unix(),
				ToolCalls: nil,
			}
			compressed = append(compressed, summaryMsg)
		}
	
	compressed = append(compressed, tail...)
	cc.compressionCount++
	
	log.Printf("[ContextCompressor] Compressed from %d to %d messages (count: %d)", 
		len(messages), len(compressed), cc.compressionCount)
	
	return compressed
}

// generateSummary 生成结构化摘要
func (cc *ContextCompressor) generateSummary(messages []Message) string {
	if len(messages) == 0 {
		return ""
	}

	var summary strings.Builder
	summary.WriteString("=== 对话历史摘要 ===\n")
	summary.WriteString(fmt.Sprintf("压缩时间: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	summary.WriteString(fmt.Sprintf("压缩消息数: %d\n", len(messages)))
	summary.WriteString("\n")

	// 提取用户目标
	goals := []string{}
	for _, msg := range messages {
		if msg.Role == "user" {
			content := msg.Content
			if strContent, ok := content.(string); ok && strContent != "" {
				goals = append(goals, strContent)
			}
		}
	}

	if len(goals) > 0 {
		summary.WriteString("## 用户目标\n")
		for i, goal := range goals[:min(len(goals), 3)] {
			summary.WriteString(fmt.Sprintf("%d. %s\n", i+1, goal))
		}
		summary.WriteString("\n")
	}

	// 提取代理响应
	responses := []string{}
	for _, msg := range messages {
		if msg.Role == "assistant" {
			content := msg.Content
			if strContent, ok := content.(string); ok && strContent != "" {
				responses = append(responses, strContent)
			}
		}
	}

	if len(responses) > 0 {
		summary.WriteString("## 代理响应\n")
		for i, response := range responses[:min(len(responses), 3)] {
			if len(response) > 100 {
				response = response[:100] + "..."
			}
			summary.WriteString(fmt.Sprintf("%d. %s\n", i+1, response))
		}
		summary.WriteString("\n")
	}

	// 提取工具调用
	toolCalls := []string{}
	for _, msg := range messages {
		if msg.ToolCalls != nil {
			// 尝试类型断言：[]interface{}
			if tcSlice, ok := msg.ToolCalls.([]interface{}); ok && len(tcSlice) > 0 {
				for _, tc := range tcSlice {
					if tcMap, ok := tc.(map[string]interface{}); ok {
						if function, ok := tcMap["function"].(map[string]interface{}); ok {
							if name, ok := function["name"].(string); ok {
								toolCalls = append(toolCalls, name)
							}
						}
					}
				}
			}
			// 尝试类型断言：[]map[string]interface{}
			if tcMapSlice, ok := msg.ToolCalls.([]map[string]interface{}); ok && len(tcMapSlice) > 0 {
				for _, tcMap := range tcMapSlice {
					if function, ok := tcMap["function"].(map[string]interface{}); ok {
						if name, ok := function["name"].(string); ok {
							toolCalls = append(toolCalls, name)
						}
					}
				}
			}
		}
	}

	if len(toolCalls) > 0 {
		summary.WriteString("## 工具调用\n")
		toolMap := make(map[string]int)
		for _, tool := range toolCalls {
			toolMap[tool]++
		}
		for tool, count := range toolMap {
			summary.WriteString(fmt.Sprintf("- %s: %d次\n", tool, count))
		}
		summary.WriteString("\n")
	}

	summary.WriteString("## 关键信息\n")
	summary.WriteString("- 此摘要包含了被压缩的对话历史\n")
	summary.WriteString("- 请优先响应最新的用户消息\n")
	summary.WriteString("- 如有指令冲突，以最新用户消息为准\n")
	summary.WriteString("=== 摘要结束 ===")

	return summary.String()
}


