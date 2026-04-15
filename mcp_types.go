package main

// ============================================================
// MCP (Model Context Protocol) 类型定义
// https://spec.modelcontextprotocol.io/
// ============================================================

// MCP 协议版本
const MCPVersion = "2024-11-05"

// JSON-RPC 2.0 基础类型

// JSONRPCRequest JSON-RPC 请求
type JSONRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      interface{}    `json:"id,omitempty"`      // string | number | null
	Method  string         `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

// JSONRPCResponse JSON-RPC 响应
type JSONRPCResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      interface{}    `json:"id"`
	Result  interface{}    `json:"result,omitempty"`
	Error   *JSONRPCError  `json:"error,omitempty"`
}

// JSONRPCError JSON-RPC 错误
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// JSON-RPC 错误码
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// ============================================================
// MCP 能力 (Capabilities)
// ============================================================

// MCPCapabilities 服务器能力
type MCPCapabilities struct {
	Tools     *ToolCapabilities     `json:"tools,omitempty"`
	Resources *ResourceCapabilities `json:"resources,omitempty"`
	Prompts   *PromptCapabilities   `json:"prompts,omitempty"`
	Logging   *LoggingCapabilities  `json:"logging,omitempty"`
}

// ToolCapabilities 工具能力
type ToolCapabilities struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourceCapabilities 资源能力
type ResourceCapabilities struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptCapabilities 提示能力
type PromptCapabilities struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// LoggingCapabilities 日志能力
type LoggingCapabilities struct{}

// ============================================================
// MCP 实现信息
// ============================================================

// MCPImplementation 实现信息
type MCPImplementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ============================================================
// MCP 初始化
// ============================================================

// InitializeParams 初始化参数
type InitializeParams struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    MCPCapabilities   `json:"capabilities"`
	ClientInfo      MCPImplementation `json:"clientInfo"`
}

// InitializeResult 初始化结果
type InitializeResult struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    MCPCapabilities   `json:"capabilities"`
	ServerInfo      MCPImplementation `json:"serverInfo"`
	Instructions    string            `json:"instructions,omitempty"`
}

// ============================================================
// MCP 工具 (Tools)
// ============================================================

// MCPTool 工具定义
type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// ListToolsResult 工具列表结果
type ListToolsResult struct {
	Tools      []MCPTool `json:"tools"`
	NextCursor string    `json:"nextCursor,omitempty"`
}

// CallToolParams 调用工具参数
type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// CallToolResult 调用工具结果
type CallToolResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// ============================================================
// MCP 内容类型
// ============================================================

// MCPContent 内容接口
type MCPContent map[string]interface{}

// NewTextContent 创建文本内容
func NewTextContent(text string) MCPContent {
	return MCPContent{
		"type": "text",
		"text": text,
	}
}

// NewImageContent 创建图片内容
func NewImageContent(data, mimeType string) MCPContent {
	return MCPContent{
		"type":     "image",
		"data":     data,
		"mimeType": mimeType,
	}
}

// NewResourceContent 创建资源内容
func NewResourceContent(uri, mimeType string, text string) MCPContent {
	return MCPContent{
		"type":     "resource",
		"resource": map[string]interface{}{
			"uri":      uri,
			"mimeType": mimeType,
			"text":     text,
		},
	}
}

// ============================================================
// MCP 资源 (Resources)
// ============================================================

// MCPResource 资源定义
type MCPResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ListResourcesResult 资源列表结果
type ListResourcesResult struct {
	Resources  []MCPResource `json:"resources"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

// ReadResourceParams 读取资源参数
type ReadResourceParams struct {
	URI string `json:"uri"`
}

// ReadResourceResult 读取资源结果
type ReadResourceResult struct {
	Contents []ResourceContents `json:"contents"`
}

// ResourceContents 资源内容
type ResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// ============================================================
// MCP 提示 (Prompts)
// ============================================================

// MCPPrompt 提示定义
type MCPPrompt struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description,omitempty"`
	Arguments   []PromptArgument        `json:"arguments,omitempty"`
}

// PromptArgument 提示参数
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// ListPromptsResult 提示列表结果
type ListPromptsResult struct {
	Prompts    []MCPPrompt `json:"prompts"`
	NextCursor string      `json:"nextCursor,omitempty"`
}

// GetPromptParams 获取提示参数
type GetPromptParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]string      `json:"arguments,omitempty"`
}

// GetPromptResult 获取提示结果
type GetPromptResult struct {
	Description string            `json:"description,omitempty"`
	Messages    []PromptMessage   `json:"messages"`
}

// PromptMessage 提示消息
type PromptMessage struct {
	Role    string      `json:"role"`
	Content MCPContent  `json:"content"`
}

// ============================================================
// MCP 日志
// ============================================================

// LoggingLevel 日志级别
type LoggingLevel string

const (
	LoggingLevelDebug     LoggingLevel = "debug"
	LoggingLevelInfo      LoggingLevel = "info"
	LoggingLevelNotice    LoggingLevel = "notice"
	LoggingLevelWarning   LoggingLevel = "warning"
	LoggingLevelError     LoggingLevel = "error"
	LoggingLevelCritical  LoggingLevel = "critical"
	LoggingLevelAlert     LoggingLevel = "alert"
	LoggingLevelEmergency LoggingLevel = "emergency"
)

// SetLevelParams 设置日志级别参数
type SetLevelParams struct {
	Level LoggingLevel `json:"level"`
}
