package main

import (
        "encoding/json"
        "fmt"
        "io"
        "log"
        "net/http"
        "net/url"
        "os"
        "path/filepath"
        "strings"
        "time"

        "github.com/google/uuid"
        "github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
        CheckOrigin: func(r *http.Request) bool { return true },
}

type HTTPServer struct {
        addr   string
        server *http.Server
}

func NewHTTPServer(addr string) *HTTPServer {
        execPath, err := os.Executable()
        if err != nil {
                log.Printf("Warning: cannot get executable path: %v", err)
                execPath = "."
        }
        execDir := filepath.Dir(execPath)
        globalUploadDir = filepath.Join(execDir, "uploads")
        if err := os.MkdirAll(globalUploadDir, 0755); err != nil {
                log.Printf("Warning: failed to create uploads directory: %v", err)
        }
        return &HTTPServer{addr: addr}
}

func (s *HTTPServer) Start() {
        if globalAuthConfig.Enabled {
                globalAuthManager = NewAuthManager(&globalAuthConfig)
                log.Printf("Authentication enabled.")
        }
        mux := http.NewServeMux()
        mux.HandleFunc("/login", HandleLoginPage)
        mux.HandleFunc("/login/submit", HandleLogin)
        mux.HandleFunc("/logout", HandleLogout)
        mux.HandleFunc("/api/login", HandleAPILogin)
        mux.HandleFunc("/", AuthMiddleware(s.indexHandler))
        mux.HandleFunc("/ws", AuthMiddleware(s.wsHandler))
        mux.HandleFunc("/props", AuthMiddleware(s.propsHandler))
        mux.HandleFunc("/v1/models", AuthMiddleware(s.modelsHandler))
        mux.HandleFunc("/upload", AuthMiddleware(s.uploadHandler))
        mux.HandleFunc("/file/", AuthMiddleware(s.fileHandler))
        mux.HandleFunc("/api/config", AuthMiddleware(s.configHandler))
        mux.HandleFunc("/api/models", AuthMiddleware(s.modelsAPIHandler))
        mux.HandleFunc("/api/models/", AuthMiddleware(s.modelDetailHandler))
        mux.HandleFunc("/api/session/new", AuthMiddleware(s.newSessionHandler))
        mux.HandleFunc("/api/roles", AuthMiddleware(s.rolesHandler))
        mux.HandleFunc("/api/roles/", AuthMiddleware(s.roleDetailHandler))
        mux.HandleFunc("/api/skills", AuthMiddleware(s.skillsHandler))
        mux.HandleFunc("/api/skills/", AuthMiddleware(s.skillDetailHandler))
        mux.HandleFunc("/api/actors", AuthMiddleware(s.actorsHandler))
        mux.HandleFunc("/api/actors/", AuthMiddleware(s.actorDetailHandler))
        mux.HandleFunc("/api/hooks", AuthMiddleware(s.hooksHandler))
        mux.HandleFunc("/api/hooks/", AuthMiddleware(s.hookDetailHandler))
        // CORS proxy endpoint
        mux.HandleFunc("/cors-proxy", AuthMiddleware(s.corsProxyHandler))
        if globalMCPServer != nil {
                mux.HandleFunc("/mcp", AuthMiddleware(globalMCPServer.HandleHTTP))
                mux.HandleFunc("/mcp/sse", AuthMiddleware(globalMCPServer.HandleSSE))
                mux.HandleFunc("/mcp/message", AuthMiddleware(globalMCPServer.HandleSSEMessage))
                log.Println("MCP endpoints enabled")
        }
        s.server = &http.Server{Addr: s.addr, Handler: mux, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second}
        log.Printf("HTTP server listening on %s", s.addr)
        if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
                log.Fatalf("HTTP server error: %v", err)
        }
}

func (s *HTTPServer) Stop() error {
        return s.server.Close()
}

func (s *HTTPServer) indexHandler(w http.ResponseWriter, r *http.Request) {
        html := GetIndexHTML()
        if html == "" {
                html = `<!DOCTYPE html>...` // fallback
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.Write([]byte(html))
}

func (s *HTTPServer) wsHandler(w http.ResponseWriter, r *http.Request) {
        conn, err := upgrader.Upgrade(w, r, nil)
        if err != nil {
                log.Printf("WebSocket upgrade error: %v", err)
                return
        }
        session := GetGlobalSession()
        connID := uuid.New().String()[:8]
        wsChannel := NewWSChannel(conn)

        session.mu.Lock()
        session.Connected = true
        session.mu.Unlock()
        log.Printf("[WS] Connection %s established for session %s", connID, session.ID)

        // 订阅会话输出（每个连接独立通道，广播到所有连接）
        outputCh, subDone := session.Subscribe(connID)

        outputFinished := make(chan struct{})
        go func() {
                defer close(outputFinished)
                chunkCount := 0
                for {
                        select {
                        case chunk, ok := <-outputCh:
                                if !ok {
                                        log.Printf("[WS] [Output-Loop] connID=%s: outputCh closed after %d chunks", connID, chunkCount)
                                        return
                                }
                                chunkCount++
                                chunk.SessionID = session.ID
                                if err := wsChannel.WriteChunk(chunk); err != nil {
                                        log.Printf("[WS] [Output-Loop] connID=%s: write error at chunk #%d (content_len=%d, done=%v): %v", connID, chunkCount, len(chunk.Content), chunk.Done, err)
                                        return
                                }
                                // 每 200 个 chunk 或 Done 时打印进度
                                if chunk.Done || chunkCount%200 == 0 {
                                        log.Printf("[WS] [Output-Loop] connID=%s: chunk #%d sent successfully (done=%v)", connID, chunkCount, chunk.Done)
                                }
                        case <-subDone:
                                log.Printf("[WS] [Output-Loop] connID=%s: received subDone signal after %d chunks", connID, chunkCount)
                                return
                        }
                }
        }()

        defer func() {
                log.Printf("[WS] connID=%s: starting cleanup (defer)", connID)
                session.Unsubscribe(connID)
                log.Printf("[WS] connID=%s: unsubscribed, waiting for outputFinished", connID)
                <-outputFinished
                session.mu.Lock()
                session.Connected = false
                session.mu.Unlock()
                conn.Close()
                log.Printf("[WS] connID=%s: connection fully closed", connID)
        }()

        wsChannel.WriteChunk(StreamChunk{SessionID: session.ID, TaskRunning: session.IsTaskRunning()})
        history := session.GetHistory()
        if len(history) > 0 {
                wsChannel.WriteChunk(StreamChunk{SessionID: session.ID, HistorySync: history, TaskRunning: session.IsTaskRunning()})
        }

        for {
                var msg struct {
                        Content string `json:"content"`
                }
                err := conn.ReadJSON(&msg)
                if err != nil {
                        break
                }
                trimmed := strings.TrimSpace(msg.Content)
                if trimmed == "" {
                        continue
                }
                if HandleSlashCommandWithDefaults(trimmed,
                        func(resp string) {
                                // 流式发送命令响应，逐行输出
                                lines := strings.Split(resp, "\n")
                                for i, line := range lines {
                                        if i > 0 {
                                                wsChannel.WriteChunk(StreamChunk{Content: "\n"})
                                        }
                                        wsChannel.WriteChunk(StreamChunk{Content: line})
                                }
                                wsChannel.WriteChunk(StreamChunk{Content: "\n", Done: true})
                        },
                        func() {
                                session.CancelTask()
                        },
                        func() {
                                // /quit: 关闭 WebSocket 连接，后台任务不受影响
                                log.Println("[WS] User issued /quit, closing connection...")
                                wsChannel.WriteChunk(StreamChunk{Content: "已断开连接，后台任务不受影响\n", Done: true})
                                conn.Close()
                        },
                        func() {
                                // /exit: 退出程序
                                log.Println("Received /exit, shutting down...")
                                session.autoSaveHistory()
                                if err := session.SavePendingMessages(); err != nil {
                                        log.Printf("Failed to save pending messages: %v", err)
                                }
                                os.Exit(0)
                        }) {
                        continue
                }
                // 将用户输入添加到输入消息列表中（自动增长，不会满）
                session.inputMu.Lock()
                session.InputMessages = append(session.InputMessages, trimmed)
                session.inputMu.Unlock()
                
                log.Printf("[HTTP Server] User input added to input messages")
                
                // 检查是否有任务在运行
                session.mu.RLock()
                taskRunning := session.TaskRunning
                session.mu.RUnlock()
                
                if !taskRunning {
                        // 模型未在处理任务，触发模型调用处理队列中的消息
                        go processInputQueue(session)
                }
        }
}



// propsHandler 返回服务器属性
func (s *HTTPServer) propsHandler(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Access-Control-Allow-Origin", "*")
        needsSetup := apiKey == ""
        props := map[string]interface{}{
                "default_generation_settings": map[string]interface{}{
                        "params": map[string]interface{}{
                                "temperature": temperature,
                                "max_tokens":  maxTokens,
                                "stream":      stream,
                        },
                },
                "model_path":  modelID,
                "needs_setup": needsSetup,
                "webui_settings": map[string]interface{}{
                        "show_thinking":     thinking,
                        "api_type":          apiType,
                        "base_url":          baseURL,
                        "plan_mode_enabled": globalPlanModeEnabled,
                },
        }
        json.NewEncoder(w).Encode(props)
}

// modelsHandler 返回模型列表
func (s *HTTPServer) modelsHandler(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Access-Control-Allow-Origin", "*")
        currentModel := modelID
        if currentModel == "" {
                currentModel = "default"
        }
        models := map[string]interface{}{
                "object": "list",
                "data": []map[string]interface{}{
                        {
                                "id":       currentModel,
                                "object":   "model",
                                "created":  time.Now().Unix(),
                                "owned_by": "ghostclaw",
                                "in_cache": true,
                                "path":     currentModel,
                                "status":   map[string]interface{}{"value": "loaded"},
                                "tags":     []string{apiType},
                        },
                },
        }
        json.NewEncoder(w).Encode(models)
}

// uploadHandler 处理文件上传
func (s *HTTPServer) uploadHandler(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Access-Control-Allow-Origin", "*")
        if r.Method != "POST" {
                http.Error(w, `{"error": "Method not allowed"}`, http.StatusMethodNotAllowed)
                return
        }
        err := r.ParseMultipartForm(100 << 20)
        if err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "Failed to parse form: %s"}`, err.Error()), http.StatusBadRequest)
                return
        }
        file, header, err := r.FormFile("file")
        if err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "Failed to get file: %s"}`, err.Error()), http.StatusBadRequest)
                return
        }
        defer file.Close()
        ext := filepath.Ext(header.Filename)
        uniqueID := uuid.New().String()
        newFilename := uniqueID + ext
        savePath := filepath.Join(globalUploadDir, newFilename)
        dst, err := os.Create(savePath)
        if err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "Failed to create file: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }
        defer dst.Close()
        written, err := io.Copy(dst, file)
        if err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "Failed to save file: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }
        log.Printf("File uploaded: %s -> %s (%d bytes)", header.Filename, savePath, written)
        response := map[string]interface{}{
                "success":  true,
                "filename": header.Filename,
                "size":     written,
                "path":     savePath,
                "url":      "/file/" + newFilename,
                "message":  fmt.Sprintf("文件已上传到: %s\n你可以告诉模型去读取这个文件: /path %s", savePath, savePath),
        }
        json.NewEncoder(w).Encode(response)
}

// fileHandler 提供文件访问
func (s *HTTPServer) fileHandler(w http.ResponseWriter, r *http.Request) {
        filename := strings.TrimPrefix(r.URL.Path, "/file/")
        if filename == "" || strings.Contains(filename, "..") || strings.ContainsAny(filename, "/\\") {
                http.Error(w, "Invalid filename", http.StatusBadRequest)
                return
        }
        filePath := filepath.Join(globalUploadDir, filename)
        info, err := os.Stat(filePath)
        if err != nil || info.IsDir() {
                http.Error(w, "File not found", http.StatusNotFound)
                return
        }
        w.Header().Set("Access-Control-Allow-Origin", "*")
        http.ServeFile(w, r, filePath)
}

// corsProxyHandler 处理 CORS 代理请求
func (s *HTTPServer) corsProxyHandler(w http.ResponseWriter, r *http.Request) {
        // 处理 OPTIONS 预检请求
        if r.Method == "OPTIONS" {
                w.Header().Set("Access-Control-Allow-Origin", "*")
                w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
                w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
                w.WriteHeader(http.StatusOK)
                return
        }

        // 处理 HEAD 请求（用于探测代理是否可用）
        if r.Method == "HEAD" {
                w.Header().Set("Access-Control-Allow-Origin", "*")
                w.WriteHeader(http.StatusOK)
                return
        }

        // 处理 GET/POST 请求
        targetURL := r.URL.Query().Get("url")
        if targetURL == "" {
                http.Error(w, "Missing 'url' parameter", http.StatusBadRequest)
                return
        }

        // 验证 URL 安全性
        parsedURL, err := url.Parse(targetURL)
        if err != nil {
                http.Error(w, "Invalid URL", http.StatusBadRequest)
                return
        }

        // 只允许 HTTP 与 HTTPS 协议
        if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
                http.Error(w, "Invalid URL scheme", http.StatusBadRequest)
                return
        }

        // 创建代理请求
        proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
        if err != nil {
                http.Error(w, "Failed to create proxy request", http.StatusInternalServerError)
                return
        }

        // 复制请求头
        for key, values := range r.Header {
                if key != "Host" {
                        proxyReq.Header[key] = values
                }
        }

        // 发送代理请求
        client := &http.Client{}
        resp, err := client.Do(proxyReq)
        if err != nil {
                http.Error(w, "Failed to proxy request", http.StatusInternalServerError)
                return
        }
        defer resp.Body.Close()

        // 复制响应头
        for key, values := range resp.Header {
                w.Header()[key] = values
        }

        // 设置 CORS 头
        w.Header().Set("Access-Control-Allow-Origin", "*")

        // 复制响应状态码与体
        w.WriteHeader(resp.StatusCode)
        io.Copy(w, resp.Body)
}
