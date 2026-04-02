package main

import (
        "bufio"
        "context"
        "encoding/json"
        "fmt"
        "io"
        "log"
        "net/http"
        "os"
        "strings"
        "sync"
        "time"

        "github.com/google/uuid"
)

// ============================================================
// MCP 服务器
// 支持 stdio、SSE、HTTP 三种传输方式
// ============================================================

// MCPServer MCP 服务器
type MCPServer struct {
        name         string
        version      string
        capabilities MCPCapabilities
        tools        map[string]MCPToolHandler
        resources    map[string]MCPResourceHandler
        prompts      map[string]MCPPromptHandler
        mu           sync.RWMutex
        sessionMu    sync.RWMutex
        sessions     map[string]*SSESession
        notifier     MCPSessionNotifier
}

// MCPToolHandler 工具处理函数
type MCPToolHandler func(ctx context.Context, args map[string]interface{}) (CallToolResult, error)

// MCPResourceHandler 资源处理函数
type MCPResourceHandler func(ctx context.Context, uri string) (ReadResourceResult, error)

// MCPPromptHandler 提示处理函数
type MCPPromptHandler func(ctx context.Context, args map[string]string) (GetPromptResult, error)

// MCPSessionNotifier 会话通知器
type MCPSessionNotifier interface {
        Notify(sessionID string, method string, params interface{})
}

// SSESession SSE 会话
type SSESession struct {
        ID       string
        Writer   http.ResponseWriter
        Flusher  http.Flusher
        Done     chan struct{}
        mu       sync.Mutex
}

// NewMCPServer 创建 MCP 服务器
func NewMCPServer(name, version string) *MCPServer {
        return &MCPServer{
                name:      name,
                version:   version,
                tools:     make(map[string]MCPToolHandler),
                resources: make(map[string]MCPResourceHandler),
                prompts:   make(map[string]MCPPromptHandler),
                sessions:  make(map[string]*SSESession),
                capabilities: MCPCapabilities{
                        Tools: &ToolCapabilities{ListChanged: true},
                        Resources: &ResourceCapabilities{ListChanged: true},
                        Prompts: &PromptCapabilities{ListChanged: true},
                        Logging: &LoggingCapabilities{},
                },
        }
}

// ============================================================
// 工具注册
// ============================================================

// RegisterTool 注册工具
func (s *MCPServer) RegisterTool(tool MCPTool, handler MCPToolHandler) {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.tools[tool.Name] = handler
}

// ListTools 列出所有工具
func (s *MCPServer) ListTools() []MCPTool {
        s.mu.RLock()
        defer s.mu.RUnlock()

        tools := make([]MCPTool, 0)
        // 从 getTools 获取工具定义
        for _, t := range getMCPTools() {
                tools = append(tools, t)
        }
        return tools
}

// CallTool 调用工具
func (s *MCPServer) CallTool(ctx context.Context, name string, args map[string]interface{}) (CallToolResult, error) {
        s.mu.RLock()
        handler, ok := s.tools[name]
        s.mu.RUnlock()

        if !ok {
                return CallToolResult{
                        Content: []MCPContent{NewTextContent(fmt.Sprintf("Unknown tool: %s", name))},
                        IsError: true,
                }, nil
        }

        return handler(ctx, args)
}

// ============================================================
// 资源注册
// ============================================================

// RegisterResource 注册资源
func (s *MCPServer) RegisterResource(uri string, handler MCPResourceHandler) {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.resources[uri] = handler
}

// ListResources 列出所有资源
func (s *MCPServer) ListResources() []MCPResource {
        s.mu.RLock()
        defer s.mu.RUnlock()

        resources := make([]MCPResource, 0)
        for uri := range s.resources {
                resources = append(resources, MCPResource{
                        URI:      uri,
                        Name:     uri,
                        MimeType: "text/plain",
                })
        }
        return resources
}

// ReadResource 读取资源
func (s *MCPServer) ReadResource(ctx context.Context, uri string) (ReadResourceResult, error) {
        s.mu.RLock()
        handler, ok := s.resources[uri]
        s.mu.RUnlock()

        if !ok {
                return ReadResourceResult{}, fmt.Errorf("resource not found: %s", uri)
        }

        return handler(ctx, uri)
}

// ============================================================
// 提示注册
// ============================================================

// RegisterPrompt 注册提示
func (s *MCPServer) RegisterPrompt(name string, handler MCPPromptHandler) {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.prompts[name] = handler
}

// ListPrompts 列出所有提示
func (s *MCPServer) ListPrompts() []MCPPrompt {
        s.mu.RLock()
        defer s.mu.RUnlock()

        prompts := make([]MCPPrompt, 0)
        for name := range s.prompts {
                prompts = append(prompts, MCPPrompt{
                        Name: name,
                })
        }
        return prompts
}

// GetPrompt 获取提示
func (s *MCPServer) GetPrompt(ctx context.Context, name string, args map[string]string) (GetPromptResult, error) {
        s.mu.RLock()
        handler, ok := s.prompts[name]
        s.mu.RUnlock()

        if !ok {
                return GetPromptResult{}, fmt.Errorf("prompt not found: %s", name)
        }

        return handler(ctx, args)
}

// ============================================================
// 请求处理
// ============================================================

// HandleRequest 处理 JSON-RPC 请求
func (s *MCPServer) HandleRequest(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
        response := JSONRPCResponse{
                JSONRPC: "2.0",
                ID:      req.ID,
        }

        switch req.Method {
        case "initialize":
                result := s.handleInitialize(req.Params)
                response.Result = result

        case "notifications/initialized":
                // 通知，无响应
                return JSONRPCResponse{}

        case "ping":
                response.Result = map[string]interface{}{}

        case "tools/list":
                tools := s.ListTools()
                response.Result = ListToolsResult{Tools: tools}

        case "tools/call":
                result, err := s.handleToolCall(ctx, req.Params)
                if err != nil {
                        response.Error = &JSONRPCError{
                                Code:    InternalError,
                                Message: err.Error(),
                        }
                } else {
                        response.Result = result
                }

        case "resources/list":
                resources := s.ListResources()
                response.Result = ListResourcesResult{Resources: resources}

        case "resources/read":
                result, err := s.handleResourceRead(ctx, req.Params)
                if err != nil {
                        response.Error = &JSONRPCError{
                                Code:    InternalError,
                                Message: err.Error(),
                        }
                } else {
                        response.Result = result
                }

        case "prompts/list":
                prompts := s.ListPrompts()
                response.Result = ListPromptsResult{Prompts: prompts}

        case "prompts/get":
                result, err := s.handlePromptGet(ctx, req.Params)
                if err != nil {
                        response.Error = &JSONRPCError{
                                Code:    InternalError,
                                Message: err.Error(),
                        }
                } else {
                        response.Result = result
                }

        case "logging/setLevel":
                // 设置日志级别
                response.Result = map[string]interface{}{}

        default:
                response.Error = &JSONRPCError{
                        Code:    MethodNotFound,
                        Message: fmt.Sprintf("Method not found: %s", req.Method),
                }
        }

        return response
}

func (s *MCPServer) handleInitialize(params map[string]interface{}) InitializeResult {
        return InitializeResult{
                ProtocolVersion: MCPVersion,
                Capabilities:    s.capabilities,
                ServerInfo: MCPImplementation{
                        Name:    s.name,
                        Version: s.version,
                },
                Instructions: "GhostClaw AI Agent MCP Server. Use tools to interact with the agent.",
        }
}

func (s *MCPServer) handleToolCall(ctx context.Context, params map[string]interface{}) (CallToolResult, error) {
        name, _ := params["name"].(string)
        args, _ := params["arguments"].(map[string]interface{})
        if args == nil {
                args = make(map[string]interface{})
        }

        return s.CallTool(ctx, name, args)
}

func (s *MCPServer) handleResourceRead(ctx context.Context, params map[string]interface{}) (ReadResourceResult, error) {
        uri, _ := params["uri"].(string)
        return s.ReadResource(ctx, uri)
}

func (s *MCPServer) handlePromptGet(ctx context.Context, params map[string]interface{}) (GetPromptResult, error) {
        name, _ := params["name"].(string)
        args, _ := params["arguments"].(map[string]string)
        if args == nil {
                args = make(map[string]string)
        }
        return s.GetPrompt(ctx, name, args)
}

// ============================================================
// stdio 传输
// ============================================================

// StartStdio 启动 stdio 传输
func (s *MCPServer) StartStdio(ctx context.Context) error {
        reader := bufio.NewReader(os.Stdin)
        encoder := json.NewEncoder(os.Stdout)

        for {
                select {
                case <-ctx.Done():
                        return ctx.Err()
                default:
                }

                line, err := reader.ReadString('\n')
                if err != nil {
                        if err == io.EOF {
                                return nil
                        }
                        log.Printf("[MCP] Read error: %v", err)
                        continue
                }

                line = strings.TrimSpace(line)
                if line == "" {
                        continue
                }

                var req JSONRPCRequest
                if err := json.Unmarshal([]byte(line), &req); err != nil {
                        response := JSONRPCResponse{
                                JSONRPC: "2.0",
                                Error: &JSONRPCError{
                                        Code:    ParseError,
                                        Message: "Parse error",
                                },
                        }
                        encoder.Encode(response)
                        continue
                }

                response := s.HandleRequest(ctx, req)
                if response.ID != nil || response.Error != nil {
                        encoder.Encode(response)
                }
        }
}

// ============================================================
// SSE 传输
// ============================================================

// SSEMessage SSE 消息
type SSEMessage struct {
        Event string      `json:"event,omitempty"`
        Data  interface{} `json:"data"`
}

// HandleSSE 处理 SSE 连接
func (s *MCPServer) HandleSSE(w http.ResponseWriter, r *http.Request) {
        // 设置 SSE 头
        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")
        w.Header().Set("Access-Control-Allow-Origin", "*")

        flusher, ok := w.(http.Flusher)
        if !ok {
                http.Error(w, "SSE not supported", http.StatusInternalServerError)
                return
        }

        // 创建会话
        sessionID := uuid.New().String()
        session := &SSESession{
                ID:      sessionID,
                Writer:  w,
                Flusher: flusher,
                Done:    make(chan struct{}),
        }

        s.sessionMu.Lock()
        s.sessions[sessionID] = session
        s.sessionMu.Unlock()

        defer func() {
                s.sessionMu.Lock()
                delete(s.sessions, sessionID)
                s.sessionMu.Unlock()
                close(session.Done)
        }()

        // 发送 endpoint 事件
        endpoint := fmt.Sprintf("/mcp/message?session_id=%s", sessionID)
        fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpoint)
        flusher.Flush()

        // 保持连接
        ticker := time.NewTicker(30 * time.Second)
        defer ticker.Stop()

        for {
                select {
                case <-r.Context().Done():
                        return
                case <-ticker.C:
                        // 发送心跳
                        fmt.Fprintf(w, "event: ping\ndata: {}\n\n")
                        flusher.Flush()
                }
        }
}

// HandleSSEMessage 处理 SSE 消息
func (s *MCPServer) HandleSSEMessage(w http.ResponseWriter, r *http.Request) {
        sessionID := r.URL.Query().Get("session_id")
        if sessionID == "" {
                http.Error(w, "Missing session_id", http.StatusBadRequest)
                return
        }

        s.sessionMu.RLock()
        session, ok := s.sessions[sessionID]
        s.sessionMu.RUnlock()

        if !ok {
                http.Error(w, "Session not found", http.StatusNotFound)
                return
        }

        var req JSONRPCRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                http.Error(w, "Invalid JSON", http.StatusBadRequest)
                return
        }

        response := s.HandleRequest(r.Context(), req)

        // 通过 SSE 发送响应
        session.mu.Lock()
        if response.ID != nil || response.Error != nil {
                data, _ := json.Marshal(response)
                fmt.Fprintf(session.Writer, "event: message\ndata: %s\n\n", data)
                session.Flusher.Flush()
        }
        session.mu.Unlock()

        w.WriteHeader(http.StatusAccepted)
}

// ============================================================
// HTTP 传输
// ============================================================

// HandleHTTP 处理 HTTP 请求
func (s *MCPServer) HandleHTTP(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

        if r.Method == "OPTIONS" {
                w.WriteHeader(http.StatusOK)
                return
        }

        if r.Method != "POST" {
                http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }

        var req JSONRPCRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                response := JSONRPCResponse{
                        JSONRPC: "2.0",
                        Error: &JSONRPCError{
                                Code:    ParseError,
                                Message: "Parse error",
                        },
                }
                json.NewEncoder(w).Encode(response)
                return
        }

        response := s.HandleRequest(r.Context(), req)
        json.NewEncoder(w).Encode(response)
}

// ============================================================
// 工具定义获取
// ============================================================

// getMCPTools 获取 MCP 工具定义
func getMCPTools() []MCPTool {
        tools := []MCPTool{
                {
                        Name:        "execute_shell",
                        Description: "Execute a shell command",
                        InputSchema: map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "command": map[string]interface{}{
                                                "type":        "string",
                                                "description": "The shell command to execute",
                                        },
                                        "timeout": map[string]interface{}{
                                                "type":        "integer",
                                                "description": "Timeout in seconds",
                                        },
                                },
                                "required": []string{"command"},
                        },
                },
                {
                        Name:        "read_file",
                        Description: "Read a file from the filesystem",
                        InputSchema: map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "path": map[string]interface{}{
                                                "type":        "string",
                                                "description": "The file path to read",
                                        },
                                },
                                "required": []string{"path"},
                        },
                },
                {
                        Name:        "write_file",
                        Description: "Write content to a file",
                        InputSchema: map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "path": map[string]interface{}{
                                                "type":        "string",
                                                "description": "The file path to write",
                                        },
                                        "content": map[string]interface{}{
                                                "type":        "string",
                                                "description": "The content to write",
                                        },
                                },
                                "required": []string{"path", "content"},
                        },
                },
                {
                        Name:        "search_web",
                        Description: "Search the web for information",
                        InputSchema: map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "query": map[string]interface{}{
                                                "type":        "string",
                                                "description": "The search query",
                                        },
                                        "num": map[string]interface{}{
                                                "type":        "integer",
                                                "description": "Number of results",
                                        },
                                },
                                "required": []string{"query"},
                        },
                },
                {
                        Name:        "memory_save",
                        Description: "Save a memory for later recall",
                        InputSchema: map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "key": map[string]interface{}{
                                                "type":        "string",
                                                "description": "Memory key",
                                        },
                                        "value": map[string]interface{}{
                                                "type":        "string",
                                                "description": "Memory value",
                                        },
                                        "category": map[string]interface{}{
                                                "type":        "string",
                                                "description": "Memory category (preference, fact, project, skill, context)",
                                        },
                                },
                                "required": []string{"key", "value"},
                        },
                },
                {
                        Name:        "memory_recall",
                        Description: "Recall memories matching a query",
                        InputSchema: map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "query": map[string]interface{}{
                                                "type":        "string",
                                                "description": "Search query",
                                        },
                                        "limit": map[string]interface{}{
                                                "type":        "integer",
                                                "description": "Maximum results",
                                        },
                                },
                                "required": []string{"query"},
                        },
                },
        }
        return tools
}
