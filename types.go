package main

// Message 对话消息结构
type Message struct {
    Role             string      `json:"role"`
    Content          interface{} `json:"content,omitempty"`
    ToolCalls        interface{} `json:"tool_calls,omitempty"`
    ToolCallID       string      `json:"tool_call_id,omitempty"`
    ReasoningContent interface{} `json:"reasoning_content,omitempty"`
    ThinkingSignature string     `json:"thinking_signature,omitempty"`
    Timestamp        int64       `json:"timestamp,omitempty"` // Unix时间戳（秒），omitempty确保零值不序列化，不影响API兼容性
}

// Response 模型响应结构
type Response struct {
    Content          interface{} `json:"content"`
    StopReason       string      `json:"stop_reason"`
    ReasoningContent interface{} `json:"reasoning_content,omitempty"`
    ThinkingSignature string     `json:"thinking_signature,omitempty"`
    ToolCalls        interface{} `json:"tool_calls,omitempty"`
}

// ToolUse 工具调用结构（用于 Anthropic 等）
type ToolUse struct {
    Type  string                 `json:"type"`
    ID    string                 `json:"id"`
    Name  string                 `json:"name"`
    Input map[string]interface{} `json:"input"`
}

