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
    "os/exec"
    "strings"
    "sync"
    "time"
)

// ============================================================
// MCP 客户端
// 连接外部 MCP 服务器，调用其工具
// 支持 stdio、SSE、HTTP 三种传输方式
// ============================================================

// MCPClientConfig MCP 客户端配置
type MCPClientConfig struct {
    Name         string            `json:"name"`          // 服务器名称
    Type         string            `json:"type"`          // 传输类型: stdio, sse, http
    Command      string            `json:"command"`       // stdio 模式: 启动命令
    Args         []string          `json:"args"`          // stdio 模式: 命令参数
    Env          map[string]string `json:"env"`           // 环境变量
    URL          string            `json:"url"`           // SSE/HTTP 模式: 服务器 URL
    Headers      map[string]string `json:"headers"`       // HTTP 头
    EnabledTools []string          `json:"enabled_tools"` // 启用的工具列表（空或 ["*"] 表示全部）
    ToolTimeout  int               `json:"tool_timeout"`  // 工具调用超时（秒）
}

// MCPClient MCP 客户端
type MCPClient struct {
    config     *MCPClientConfig
    tools      map[string]*MCPClientTool
    mu         sync.RWMutex

    // stdio 模式
    cmd        *exec.Cmd
    stdin      io.WriteCloser
    stdout     io.Reader
    stderr     io.Reader

    // SSE 模式
    httpClient *http.Client

    // 会话状态
    initialized bool
    sessionID   string
}

// MCPClientTool MCP 客户端工具
type MCPClientTool struct {
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    InputSchema map[string]interface{} `json:"inputSchema"`
    ServerName  string                 `json:"server_name"`
}

// NewMCPClient 创建 MCP 客户端
func NewMCPClient(config *MCPClientConfig) *MCPClient {
    if config.ToolTimeout == 0 {
        config.ToolTimeout = 30
    }
    return &MCPClient{
        config: config,
        tools:  make(map[string]*MCPClientTool),
    }
}

// Connect 连接到 MCP 服务器
func (c *MCPClient) Connect(ctx context.Context) error {
    switch c.config.Type {
    case "stdio":
        return c.connectStdio(ctx)
    case "sse":
        return c.connectSSE(ctx)
    case "http", "streamableHttp":
        return c.connectHTTP(ctx)
    default:
        // 自动检测
        if c.config.Command != "" {
            return c.connectStdio(ctx)
        } else if c.config.URL != "" {
            if strings.HasPrefix(strings.ToLower(c.config.URL), "/sse") {
                return c.connectSSE(ctx)
            }
            return c.connectHTTP(ctx)
        }
        return fmt.Errorf("unknown MCP transport type: %s", c.config.Type)
    }
}

// Disconnect 断开连接
func (c *MCPClient) Disconnect() error {
    if c.cmd != nil && c.cmd.Process != nil {
        return c.cmd.Process.Kill()
    }
    return nil
}

// ============================================================
// stdio 传输
// ============================================================

func (c *MCPClient) connectStdio(ctx context.Context) error {
    // 准备环境变量
    env := os.Environ()
    for k, v := range c.config.Env {
        env = append(env, fmt.Sprintf("%s=%s", k, v))
    }

    // 启动进程
    c.cmd = exec.CommandContext(ctx, c.config.Command, c.config.Args...)
    c.cmd.Env = env

    // 获取 stdin/stdout
    stdin, err := c.cmd.StdinPipe()
    if err != nil {
        return fmt.Errorf("failed to get stdin: %w", err)
    }
    c.stdin = stdin

    stdout, err := c.cmd.StdoutPipe()
    if err != nil {
        return fmt.Errorf("failed to get stdout: %w", err)
    }
    c.stdout = stdout

    stderr, err := c.cmd.StderrPipe()
    if err != nil {
        return fmt.Errorf("failed to get stderr: %w", err)
    }
    c.stderr = stderr

    // 启动进程
    if err := c.cmd.Start(); err != nil {
        return fmt.Errorf("failed to start command: %w", err)
    }

    // 异步读取 stderr
    go func() {
        scanner := bufio.NewScanner(stderr)
        for scanner.Scan() {
            log.Printf("[MCP/%s/stderr] %s", c.config.Name, scanner.Text())
        }
    }()

    // 初始化连接
    if err := c.initializeStdio(); err != nil {
        return fmt.Errorf("failed to initialize: %w", err)
    }

    // 获取工具列表
    if err := c.listToolsStdio(); err != nil {
        return fmt.Errorf("failed to list tools: %w", err)
    }

    log.Printf("[MCP] Connected to %s via stdio, %d tools available", c.config.Name, len(c.tools))
    return nil
}

func (c *MCPClient) initializeStdio() error {
    // 发送 initialize 请求
    req := JSONRPCRequest{
        JSONRPC: "2.0",
        ID:      1,
        Method:  "initialize",
        Params: map[string]interface{}{
            "protocolVersion": MCPVersion,
            "capabilities": MCPCapabilities{
                Tools: &ToolCapabilities{},
            },
            "clientInfo": MCPImplementation{
                Name:    "GhostClaw",
                Version: "1.0.0",
            },
        },
    }

    resp, err := c.sendRequestStdio(req)
    if err != nil {
        return err
    }

    if resp.Error != nil {
        return fmt.Errorf("initialize error: %s", resp.Error.Message)
    }

    c.initialized = true
    return nil
}

func (c *MCPClient) listToolsStdio() error {
    req := JSONRPCRequest{
        JSONRPC: "2.0",
        ID:      2,
        Method:  "tools/list",
        Params:  map[string]interface{}{},
    }

    resp, err := c.sendRequestStdio(req)
    if err != nil {
        return err
    }

    if resp.Error != nil {
        return fmt.Errorf("list tools error: %s", resp.Error.Message)
    }

    // 解析工具列表
    resultBytes, err := json.Marshal(resp.Result)
    if err != nil {
        return err
    }

    var listResult ListToolsResult
    if err := json.Unmarshal(resultBytes, &listResult); err != nil {
        return err
    }

    // 注册工具
    enabledSet := make(map[string]bool)
    allowAll := len(c.config.EnabledTools) == 0
    for _, t := range c.config.EnabledTools {
        if t == "*" {
            allowAll = true
            break
        }
        enabledSet[t] = true
    }

    for _, tool := range listResult.Tools {
        wrappedName := fmt.Sprintf("mcp_%s_%s", c.config.Name, tool.Name)
        if !allowAll && !enabledSet[tool.Name] && !enabledSet[wrappedName] {
            continue
        }

        c.tools[wrappedName] = &MCPClientTool{
            Name:        tool.Name,
            Description: tool.Description,
            InputSchema: tool.InputSchema,
            ServerName:  c.config.Name,
        }
    }

    return nil
}

func (c *MCPClient) sendRequestStdio(req JSONRPCRequest) (*JSONRPCResponse, error) {
        // 发送请求
        reqBytes, err := json.Marshal(req)
        if err != nil {
                return nil, err
        }
        c.stdin.Write(append(reqBytes, '\n'))

        // 读取响应（可能跨多行）
        reader := bufio.NewReader(c.stdout)
        var buffer strings.Builder
        for {
                line, err := reader.ReadString('\n')
                if err != nil {
                        return nil, err
                }
                buffer.WriteString(line)
                // 尝试解析累积的数据
                var resp JSONRPCResponse
                if err := json.Unmarshal([]byte(buffer.String()), &resp); err == nil {
                        return &resp, nil
                }
                // 如果还没收到完整的 JSON，继续读取
                // 简单判断：如果当前行是空行或已包含完整的括号，但解析失败则继续
        }
}

// ============================================================
// SSE 传输
// ============================================================

func (c *MCPClient) connectSSE(ctx context.Context) error {
    c.httpClient = &http.Client{
        Timeout: time.Duration(c.config.ToolTimeout) * time.Second,
    }

    // SSE 连接需要先获取 endpoint
    // 这里简化处理，直接使用 POST 请求
    return c.connectHTTP(ctx)
}

// ============================================================
// HTTP 传输
// ============================================================

func (c *MCPClient) connectHTTP(ctx context.Context) error {
    c.httpClient = &http.Client{
        Timeout: time.Duration(c.config.ToolTimeout) * time.Second,
    }

    // 发送 initialize 请求
    req := JSONRPCRequest{
        JSONRPC: "2.0",
        ID:      1,
        Method:  "initialize",
        Params: map[string]interface{}{
            "protocolVersion": MCPVersion,
            "capabilities": MCPCapabilities{
                Tools: &ToolCapabilities{},
            },
            "clientInfo": MCPImplementation{
                Name:    "GhostClaw",
                Version: "1.0.0",
            },
        },
    }

    resp, err := c.sendRequestHTTP(req)
    if err != nil {
        return fmt.Errorf("failed to initialize: %w", err)
    }

    if resp.Error != nil {
        return fmt.Errorf("initialize error: %s", resp.Error.Message)
    }

    c.initialized = true

    // 获取工具列表
    if err := c.listToolsHTTP(); err != nil {
        return fmt.Errorf("failed to list tools: %w", err)
    }

    log.Printf("[MCP] Connected to %s via HTTP, %d tools available", c.config.Name, len(c.tools))
    return nil
}

func (c *MCPClient) listToolsHTTP() error {
    req := JSONRPCRequest{
        JSONRPC: "2.0",
        ID:      2,
        Method:  "tools/list",
        Params:  map[string]interface{}{},
    }

    resp, err := c.sendRequestHTTP(req)
    if err != nil {
        return err
    }

    if resp.Error != nil {
        return fmt.Errorf("list tools error: %s", resp.Error.Message)
    }

    // 解析工具列表
    resultBytes, err := json.Marshal(resp.Result)
    if err != nil {
        return err
    }

    var listResult ListToolsResult
    if err := json.Unmarshal(resultBytes, &listResult); err != nil {
        return err
    }

    // 注册工具
    enabledSet := make(map[string]bool)
    allowAll := len(c.config.EnabledTools) == 0
    for _, t := range c.config.EnabledTools {
        if t == "*" {
            allowAll = true
            break
        }
        enabledSet[t] = true
    }

    for _, tool := range listResult.Tools {
        wrappedName := fmt.Sprintf("mcp_%s_%s", c.config.Name, tool.Name)
        if !allowAll && !enabledSet[tool.Name] && !enabledSet[wrappedName] {
            continue
        }

        c.tools[wrappedName] = &MCPClientTool{
            Name:        tool.Name,
            Description: tool.Description,
            InputSchema: tool.InputSchema,
            ServerName:  c.config.Name,
        }
    }

    return nil
}

func (c *MCPClient) sendRequestHTTP(req JSONRPCRequest) (*JSONRPCResponse, error) {
    reqBytes, err := json.Marshal(req)
    if err != nil {
        return nil, err
    }

    httpReq, err := http.NewRequest("POST", c.config.URL, strings.NewReader(string(reqBytes)))
    if err != nil {
        return nil, err
    }

    httpReq.Header.Set("Content-Type", "application/json")
    for k, v := range c.config.Headers {
        httpReq.Header.Set(k, v)
    }

    httpResp, err := c.httpClient.Do(httpReq)
    if err != nil {
        return nil, err
    }
    defer httpResp.Body.Close()

    var resp JSONRPCResponse
    if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
        return nil, err
    }

    return &resp, nil
}

// ============================================================
// 工具调用
// ============================================================

// CallTool 调用 MCP 工具
func (c *MCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
    // 查找工具
    c.mu.RLock()
    tool, ok := c.tools[name]
    if !ok {
        c.mu.RUnlock()
        return "", fmt.Errorf("tool not found: %s", name)
    }
    c.mu.RUnlock()

    // 根据传输类型调用
    switch c.config.Type {
    case "stdio":
        return c.callToolStdio(ctx, tool.Name, args)
    default:
        return c.callToolHTTP(ctx, tool.Name, args)
    }
}

func (c *MCPClient) callToolStdio(ctx context.Context, name string, args map[string]interface{}) (string, error) {
    req := JSONRPCRequest{
        JSONRPC: "2.0",
        ID:      time.Now().UnixNano(),
        Method:  "tools/call",
        Params: map[string]interface{}{
            "name":      name,
            "arguments": args,
        },
    }

    resp, err := c.sendRequestStdio(req)
    if err != nil {
        return "", err
    }

    if resp.Error != nil {
        return "", fmt.Errorf("tool call error: %s", resp.Error.Message)
    }

    // 解析结果
    resultBytes, err := json.Marshal(resp.Result)
    if err != nil {
        return "", err
    }

    var callResult CallToolResult
    if err := json.Unmarshal(resultBytes, &callResult); err != nil {
        return string(resultBytes), nil
    }

    // 提取文本内容
    var parts []string
    for _, content := range callResult.Content {
        if text, ok := content["text"].(string); ok {
            parts = append(parts, text)
        }
    }

    if len(parts) == 0 {
        return "(no output)", nil
    }
    return strings.Join(parts, "\n"), nil
}

func (c *MCPClient) callToolHTTP(ctx context.Context, name string, args map[string]interface{}) (string, error) {
    req := JSONRPCRequest{
        JSONRPC: "2.0",
        ID:      time.Now().UnixNano(),
        Method:  "tools/call",
        Params: map[string]interface{}{
            "name":      name,
            "arguments": args,
        },
    }

    resp, err := c.sendRequestHTTP(req)
    if err != nil {
        return "", err
    }

    if resp.Error != nil {
        return "", fmt.Errorf("tool call error: %s", resp.Error.Message)
    }

    // 解析结果
    resultBytes, err := json.Marshal(resp.Result)
    if err != nil {
        return "", err
    }

    var callResult CallToolResult
    if err := json.Unmarshal(resultBytes, &callResult); err != nil {
        return string(resultBytes), nil
    }

    // 提取文本内容
    var parts []string
    for _, content := range callResult.Content {
        if text, ok := content["text"].(string); ok {
            parts = append(parts, text)
        }
    }

    if len(parts) == 0 {
        return "(no output)", nil
    }
    return strings.Join(parts, "\n"), nil
}

// ============================================================
// 工具列表
// ============================================================

// GetTools 获取所有工具
func (c *MCPClient) GetTools() map[string]*MCPClientTool {
    c.mu.RLock()
    defer c.mu.RUnlock()

    result := make(map[string]*MCPClientTool)
    for k, v := range c.tools {
        result[k] = v
    }
    return result
}

// GetToolDefinitions 获取工具定义（OpenAI 格式）
func (c *MCPClient) GetToolDefinitions() []map[string]interface{} {
    c.mu.RLock()
    defer c.mu.RUnlock()

    var definitions []map[string]interface{}
    for name, tool := range c.tools {
        def := map[string]interface{}{
            "type": "function",
            "function": map[string]interface{}{
                "name":        name,
                "description": tool.Description,
                "parameters":  normalizeSchema(tool.InputSchema),
            },
        }
        definitions = append(definitions, def)
    }
    return definitions
}

// normalizeSchema 标准化 JSON Schema
func normalizeSchema(schema map[string]interface{}) map[string]interface{} {
    if schema == nil {
        return map[string]interface{}{
            "type":       "object",
            "properties": map[string]interface{}{},
        }
    }

    // 处理 nullable
    if rawType, ok := schema["type"].([]interface{}); ok {
        var nonNull []string
        for _, t := range rawType {
            if s, ok := t.(string); ok && s != "null" {
                nonNull = append(nonNull, s)
            }
        }
        if len(nonNull) == 1 {
            schema["type"] = nonNull[0]
            schema["nullable"] = true
        }
    }

    // 递归处理 properties
    if props, ok := schema["properties"].(map[string]interface{}); ok {
        for k, v := range props {
            if m, ok := v.(map[string]interface{}); ok {
                schema["properties"].(map[string]interface{})[k] = normalizeSchema(m)
            }
        }
    }

    // 递归处理 items
    if items, ok := schema["items"].(map[string]interface{}); ok {
        schema["items"] = normalizeSchema(items)
    }

    // 确保 type 是 object 时有 properties
    if t, ok := schema["type"].(string); ok && t == "object" {
        if _, ok := schema["properties"]; !ok {
            schema["properties"] = map[string]interface{}{}
        }
        if _, ok := schema["required"]; !ok {
            schema["required"] = []string{}
        }
    }

    return schema
}

// ============================================================
// MCP 客户端管理器
// ============================================================

// MCPClientManager MCP 客户端管理器
type MCPClientManager struct {
    clients map[string]*MCPClient
    mu      sync.RWMutex
}

// NewMCPClientManager 创建 MCP 客户端管理器
func NewMCPClientManager() *MCPClientManager {
    return &MCPClientManager{
        clients: make(map[string]*MCPClient),
    }
}

// AddClient 添加客户端
func (m *MCPClientManager) AddClient(name string, config *MCPClientConfig) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    client := NewMCPClient(config)
    m.clients[name] = client
    return nil
}

// ConnectAll 连接所有客户端
func (m *MCPClientManager) ConnectAll(ctx context.Context) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    var errors []string
    for name, client := range m.clients {
        if err := client.Connect(ctx); err != nil {
            errors = append(errors, fmt.Sprintf("%s: %v", name, err))
        }
    }

    if len(errors) > 0 {
        return fmt.Errorf("failed to connect some clients: %s", strings.Join(errors, "; "))
    }
    return nil
}

// DisconnectAll 断开所有客户端
func (m *MCPClientManager) DisconnectAll() {
    m.mu.Lock()
    defer m.mu.Unlock()

    for _, client := range m.clients {
        client.Disconnect()
    }
}

// GetClient 获取客户端
func (m *MCPClientManager) GetClient(name string) (*MCPClient, bool) {
    m.mu.RLock()
    defer m.mu.RUnlock()

    client, ok := m.clients[name]
    return client, ok
}

// Count 获取客户端数量
func (m *MCPClientManager) Count() int {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return len(m.clients)
}

// GetAllTools 获取所有工具定义
func (m *MCPClientManager) GetAllTools() []map[string]interface{} {
    m.mu.RLock()
    defer m.mu.RUnlock()

    var tools []map[string]interface{}
    for _, client := range m.clients {
        tools = append(tools, client.GetToolDefinitions()...)
    }
    return tools
}

// CallTool 调用工具（根据工具名自动路由到对应客户端）
func (m *MCPClientManager) CallTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()

    // 工具名格式: mcp_{server}_{tool}
    parts := strings.SplitN(name, "_", 3)
    if len(parts) < 3 || parts[0] != "mcp" {
        return "", fmt.Errorf("invalid MCP tool name format: %s", name)
    }

    serverName := parts[1]
    client, ok := m.clients[serverName]
    if !ok {
        return "", fmt.Errorf("MCP server not found: %s", serverName)
    }

    return client.CallTool(ctx, name, args)
}

// 全局 MCP 客户端管理器
var initMCPClientManagerOnce sync.Once

// initMCPClientManager 初始化 MCP 客户端管理器（线程安全）
func initMCPClientManager() {
    initMCPClientManagerOnce.Do(func() {
        if globalMCPClientManager == nil {
            globalMCPClientManager = NewMCPClientManager()
        }
    })
}
