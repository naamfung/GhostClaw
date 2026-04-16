package main

import (
        "fmt"
        "strings"
        "time"
)

// TaskStatus 任务状态枚举
type TaskStatus string

const (
        TaskStatusPending   TaskStatus = "pending"   // 等待执行
        TaskStatusRunning   TaskStatus = "running"   // 执行中
        TaskStatusSuccess   TaskStatus = "success"   // 成功完成
        TaskStatusCancelled TaskStatus = "cancelled" // 被取消
        TaskStatusFailed    TaskStatus = "failed"    // 执行失败
        TaskStatusSkipped   TaskStatus = "skipped"   // 被跳过（因前置任务取消/失败）
)

// CancelSource 取消来源
type CancelSource string

const (
        CancelByUser   CancelSource = "user"   // 用户手动取消
        CancelBySystem CancelSource = "system" // 系统自动取消（如超时、错误）
        CancelBySignal CancelSource = "signal" // 信号中断（如 Ctrl+C）
)

// MessageMeta 消息元数据（用于状态追踪，不发送给模型）
type MessageMeta struct {
        ID           string       `json:"id"`                      // 消息唯一ID
        Timestamp    int64        `json:"timestamp"`               // 时间戳
        Status       TaskStatus   `json:"status"`                  // 任务状态
        CancelSource CancelSource `json:"cancel_source,omitempty"` // 取消来源
        CancelReason string       `json:"cancel_reason,omitempty"` // 取消原因
        ParentID     string       `json:"parent_id,omitempty"`     // 父消息ID（用于关联工具调用链）
        ToolName     string       `json:"tool_name,omitempty"`     // 工具名称
        RetryCount   int          `json:"retry_count,omitempty"`   // 重试次数
        IsHistorical bool         `json:"is_historical,omitempty"` // 是否为历史消息（已归档）
}

// EnrichedMessage 增强消息结构（内部使用）
type EnrichedMessage struct {
        Role             string      `json:"role"`
        Content          interface{} `json:"content,omitempty"`
        ToolCalls        interface{} `json:"tool_calls,omitempty"`
        ToolCallID       string      `json:"tool_call_id,omitempty"`
        ReasoningContent interface{} `json:"reasoning_content,omitempty"`
        Meta             MessageMeta `json:"_meta,omitempty"` // 元数据（下划线前缀表示内部字段）
}

// generateMessageID 生成唯一消息ID
func generateMessageID() string {
        return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}

// NewEnrichedMessage 创建增强消息
func NewEnrichedMessage(role string, content interface{}) EnrichedMessage {
        return EnrichedMessage{
                Role:    role,
                Content: content,
                Meta: MessageMeta{
                        ID:        generateMessageID(),
                        Timestamp: time.Now().Unix(),
                        Status:    TaskStatusSuccess,
                },
        }
}

// NewToolCallMessage 创建工具调用消息
func NewToolCallMessage(toolCalls interface{}) EnrichedMessage {
        return EnrichedMessage{
                Role:      "assistant",
                ToolCalls: toolCalls,
                Meta: MessageMeta{
                        ID:        generateMessageID(),
                        Timestamp: time.Now().Unix(),
                        Status:    TaskStatusPending, // 工具调用初始状态为待执行
                },
        }
}

// NewToolResultMessage 创建工具结果消息
func NewToolResultMessage(toolCallID, content string, status TaskStatus, toolName string) EnrichedMessage {
        return EnrichedMessage{
                Role:       "tool",
                ToolCallID: toolCallID,
                Content:    content,
                Meta: MessageMeta{
                        ID:        generateMessageID(),
                        Timestamp: time.Now().Unix(),
                        Status:    status,
                        ToolName:  toolName,
                },
        }
}

// CancelToolResult 创建取消的工具结果
func CancelToolResult(toolCallID string, source CancelSource, reason string, toolName string) EnrichedMessage {
        return EnrichedMessage{
                Role:       "tool",
                ToolCallID: toolCallID,
                Content:    "", // 取消的操作没有实际结果
                Meta: MessageMeta{
                        ID:           generateMessageID(),
                        Timestamp:    time.Now().Unix(),
                        Status:       TaskStatusCancelled,
                        CancelSource: source,
                        CancelReason: reason,
                        ToolName:     toolName,
                },
        }
}

// ToAPIMessage 转换为API消息格式（发送给模型）
// 关键：将被取消的操作明确告知模型
func (em EnrichedMessage) ToAPIMessage() Message {
        msg := Message{
                Role:             em.Role,
                Content:          em.Content,
                ToolCalls:        em.ToolCalls,
                ToolCallID:       em.ToolCallID,
                ReasoningContent: em.ReasoningContent,
                Timestamp:        em.Meta.Timestamp,
        }

        // 特殊处理：工具结果消息
        if em.Role == "tool" {
                contentStr, _ := em.Content.(string)
                switch em.Meta.Status {
                case TaskStatusCancelled:
                        // 被取消的操作：明确告知模型此操作被取消，不应重试
                        var cancelNote strings.Builder
                        cancelNote.WriteString(fmt.Sprintf("[OPERATION CANCELLED BY %s", strings.ToUpper(string(em.Meta.CancelSource))))
                        if em.Meta.ToolName != "" {
                                cancelNote.WriteString(fmt.Sprintf(" | Tool: %s", em.Meta.ToolName))
                        }
                        cancelNote.WriteString("]")
                        if em.Meta.CancelReason != "" {
                                cancelNote.WriteString(fmt.Sprintf(" Reason: %s", em.Meta.CancelReason))
                        }
                        cancelNote.WriteString(" DO NOT RETRY THIS OPERATION. The user intentionally stopped this task.")
                        msg.Content = cancelNote.String()

                case TaskStatusFailed:
                        // 失败的操作：标记为失败
                        if contentStr == "" {
                                msg.Content = "[OPERATION FAILED] No result returned. DO NOT RETRY without user confirmation."
                        } else {
                                // 检查是否已有 Error 前缀
                                if !strings.HasPrefix(contentStr, "Error:") && !strings.HasPrefix(contentStr, "error:") {
                                        msg.Content = "[OPERATION FAILED] " + contentStr
                                } else {
                                        msg.Content = contentStr
                                }
                        }

                case TaskStatusSuccess:
                        // 成功的操作：历史消息添加标记
                        if em.Meta.IsHistorical {
                                if em.Meta.ToolName != "" {
                                        msg.Content = fmt.Sprintf("[COMPLETED | Tool: %s] %s", em.Meta.ToolName, contentStr)
                                } else {
                                        msg.Content = "[COMPLETED] " + contentStr
                                }
                        }
                        // 非历史消息保持原样

                case TaskStatusSkipped:
                        // 被跳过的操作
                        msg.Content = fmt.Sprintf("[OPERATION SKIPPED] Dependency was cancelled or failed. Tool: %s", em.Meta.ToolName)
                }
        }

        return msg
}

// MessageHistory 消息历史管理器
type MessageHistory struct {
        messages []EnrichedMessage
}

// NewMessageHistory 创建消息历史
func NewMessageHistory() *MessageHistory {
        return &MessageHistory{
                messages: make([]EnrichedMessage, 0),
        }
}

// AddMessage 添加消息
func (mh *MessageHistory) AddMessage(msg EnrichedMessage) {
        mh.messages = append(mh.messages, msg)
}

// AddUserMessage 添加用户消息
func (mh *MessageHistory) AddUserMessage(content string) EnrichedMessage {
        msg := NewEnrichedMessage("user", content)
        mh.messages = append(mh.messages, msg)
        return msg
}

// AddAssistantMessage 添加助手消息
func (mh *MessageHistory) AddAssistantMessage(content interface{}, reasoning string) EnrichedMessage {
        msg := EnrichedMessage{
                Role:             "assistant",
                Content:          content,
                ReasoningContent: reasoning,
                Meta: MessageMeta{
                        ID:        generateMessageID(),
                        Timestamp: time.Now().Unix(),
                        Status:    TaskStatusSuccess,
                },
        }
        mh.messages = append(mh.messages, msg)
        return msg
}

// AddToolCallMessage 添加工具调用消息
func (mh *MessageHistory) AddToolCallMessage(toolCalls interface{}) EnrichedMessage {
        msg := NewToolCallMessage(toolCalls)
        mh.messages = append(mh.messages, msg)
        return msg
}

// AddToolResultMessage 添加工具结果消息
func (mh *MessageHistory) AddToolResultMessage(toolCallID, content string, status TaskStatus, toolName string) EnrichedMessage {
        msg := NewToolResultMessage(toolCallID, content, status, toolName)
        mh.messages = append(mh.messages, msg)
        return msg
}

// GetMessages 获取所有消息
func (mh *MessageHistory) GetMessages() []EnrichedMessage {
        return mh.messages
}

// GetAPIMessages 转换为API消息格式（发送给模型）
func (mh *MessageHistory) GetAPIMessages() []Message {
        apiMsgs := make([]Message, 0, len(mh.messages))
        for _, em := range mh.messages {
                apiMsgs = append(apiMsgs, em.ToAPIMessage())
        }
        return apiMsgs
}

// CancelLastToolCall 取消最后一个工具调用（用户中断时调用）
func (mh *MessageHistory) CancelLastToolCall(source CancelSource, reason string) bool {
        // 从后往前找最后一个 pending 或 running 状态的工具调用
        for i := len(mh.messages) - 1; i >= 0; i-- {
                msg := &mh.messages[i]
                if msg.Role == "tool" && (msg.Meta.Status == TaskStatusPending || msg.Meta.Status == TaskStatusRunning) {
                        msg.Meta.Status = TaskStatusCancelled
                        msg.Meta.CancelSource = source
                        msg.Meta.CancelReason = reason
                        msg.Content = "" // 清空结果
                        return true
                }
        }
        return false
}

// CancelPendingToolCalls 取消所有待执行的工具调用
func (mh *MessageHistory) CancelPendingToolCalls(source CancelSource, reason string) int {
        count := 0
        for i := range mh.messages {
                if mh.messages[i].Role == "tool" && (mh.messages[i].Meta.Status == TaskStatusPending || mh.messages[i].Meta.Status == TaskStatusRunning) {
                        mh.messages[i].Meta.Status = TaskStatusCancelled
                        mh.messages[i].Meta.CancelSource = source
                        mh.messages[i].Meta.CancelReason = reason
                        mh.messages[i].Content = ""
                        count++
                }
        }
        return count
}

// MarkToolSuccess 标记工具调用成功
func (mh *MessageHistory) MarkToolSuccess(toolCallID string, result string) {
        for i := range mh.messages {
                if mh.messages[i].ToolCallID == toolCallID {
                        mh.messages[i].Meta.Status = TaskStatusSuccess
                        mh.messages[i].Content = result
                        return
                }
        }
}

// MarkToolFailed 标记工具调用失败
func (mh *MessageHistory) MarkToolFailed(toolCallID string, errMsg string) {
        for i := range mh.messages {
                if mh.messages[i].ToolCallID == toolCallID {
                        mh.messages[i].Meta.Status = TaskStatusFailed
                        mh.messages[i].Content = errMsg
                        return
                }
        }
}

// MarkToolRunning 标记工具调用正在执行
func (mh *MessageHistory) MarkToolRunning(toolCallID string) {
        for i := range mh.messages {
                if mh.messages[i].ToolCallID == toolCallID {
                        mh.messages[i].Meta.Status = TaskStatusRunning
                        return
                }
        }
}

// GetLastToolCallID 获取最后一个工具调用的ID
func (mh *MessageHistory) GetLastToolCallID() string {
        for i := len(mh.messages) - 1; i >= 0; i-- {
                if mh.messages[i].Role == "assistant" && mh.messages[i].ToolCalls != nil {
                        // 从 ToolCalls 中提取 ID
                        if tcSlice, ok := mh.messages[i].ToolCalls.([]interface{}); ok && len(tcSlice) > 0 {
                                if tc, ok := tcSlice[len(tcSlice)-1].(map[string]interface{}); ok {
                                        if id, ok := tc["id"].(string); ok {
                                                return id
                                        }
                                }
                        }
                        if tcMap, ok := mh.messages[i].ToolCalls.([]map[string]interface{}); ok && len(tcMap) > 0 {
                                if id, ok := tcMap[len(tcMap)-1]["id"].(string); ok {
                                        return id
                                }
                        }
                }
        }
        return ""
}

// GetHistoricalSummary 获取历史摘要（用于长对话压缩，包含时间戳）
func (mh *MessageHistory) GetHistoricalSummary() string {
        var summary strings.Builder
        for _, msg := range mh.messages {
                if msg.Meta.IsHistorical {
                        ts := ""
                        if msg.Meta.Timestamp > 0 {
                                ts = time.Unix(msg.Meta.Timestamp, 0).Format("15:04:05")
                        }
                        switch msg.Role {
                        case "user":
                                if ts != "" {
                                        summary.WriteString(fmt.Sprintf("[%s] User: %s\n", ts, TruncateAny(msg.Content, 100)))
                                } else {
                                        summary.WriteString(fmt.Sprintf("User: %s\n", TruncateAny(msg.Content, 100)))
                                }
                        case "assistant":
                                if msg.ToolCalls != nil {
                                        summary.WriteString(fmt.Sprintf("Assistant: [Tool Call - %s]\n", msg.Meta.Status))
                                } else {
                                        summary.WriteString(fmt.Sprintf("Assistant: %s\n", TruncateAny(msg.Content, 100)))
                                }
                        case "tool":
                                summary.WriteString(fmt.Sprintf("Tool Result (%s): [%s]\n", msg.Meta.ToolName, msg.Meta.Status))
                        }
                }
        }
        return summary.String()
}

// CompactHistory 压缩历史消息（将旧消息标记为历史并生成摘要）
func (mh *MessageHistory) CompactHistory(keepLast int) string {
        if len(mh.messages) <= keepLast {
                return ""
        }

        // 标记旧消息为历史
        for i := 0; i < len(mh.messages)-keepLast; i++ {
                mh.messages[i].Meta.IsHistorical = true
        }

        return mh.GetHistoricalSummary()
}

// Count 获取消息数量
func (mh *MessageHistory) Count() int {
        return len(mh.messages)
}

// Clear 清空消息历史
func (mh *MessageHistory) Clear() {
        mh.messages = make([]EnrichedMessage, 0)
}
