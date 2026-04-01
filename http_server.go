package main

import (
        "context"
        "encoding/json"
        "fmt"
        "io"
        "log"
        "net/http"
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
        mux.HandleFunc("/api/roles", AuthMiddleware(s.rolesHandler))
        mux.HandleFunc("/api/roles/", AuthMiddleware(s.roleDetailHandler))
        mux.HandleFunc("/api/skills", AuthMiddleware(s.skillsHandler))
        mux.HandleFunc("/api/skills/", AuthMiddleware(s.skillDetailHandler))
        mux.HandleFunc("/api/actors", AuthMiddleware(s.actorsHandler))
        mux.HandleFunc("/api/actors/", AuthMiddleware(s.actorDetailHandler))
        mux.HandleFunc("/api/hooks", AuthMiddleware(s.hooksHandler))
        mux.HandleFunc("/api/hooks/", AuthMiddleware(s.hookDetailHandler))
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
                for {
                        select {
                        case chunk := <-outputCh:
                                chunk.SessionID = session.ID
                                if err := wsChannel.WriteChunk(chunk); err != nil {
                                        log.Printf("[WS] write error: %v", err)
                                        return
                                }
                        case <-subDone:
                                return
                        }
                }
        }()

        defer func() {
                session.Unsubscribe(connID)
                <-outputFinished
                session.mu.Lock()
                session.Connected = false
                session.mu.Unlock()
                conn.Close()
                log.Printf("[WS] Connection %s disconnected", connID)
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
                                wsChannel.WriteChunk(StreamChunk{Content: resp + "\n", Done: true})
                        },
                        func() {
                                session.CancelTask()
                        },
                        func() {
                                log.Println("Received /exit, shutting down...")
                                session.autoSaveHistory()
                                os.Exit(0)
                        }) {
                        continue
                }
                session.AddToHistory("user", trimmed)
                go processUserInput(session, trimmed)
        }
}

func processUserInput(session *GlobalSession, input string) {
        ok, taskID := session.TryStartTask()
        if !ok {
                session.EnqueueOutput(StreamChunk{Error: "已有任务在执行中，请使用 /stop 取消后再试"})
                return
        }
        taskCtx := session.GetTaskCtx()
        session.EnqueueOutput(StreamChunk{TaskRunning: true})
        defer func() {
                session.SetTaskRunning(false, taskID)
                session.EnqueueOutput(StreamChunk{TaskRunning: false})
        }()
        outputChannel := NewSessionChannel(session)
        history := session.GetHistory()
        newHistory, err := AgentLoop(taskCtx, outputChannel, history, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking)
        if err != nil && err != context.Canceled {
                session.EnqueueOutput(StreamChunk{Error: err.Error(), Done: true})
        }
        if len(newHistory) > len(history) {
                session.SetHistory(newHistory)
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
                        "show_thinking": thinking,
                        "api_type":      apiType,
                        "base_url":      baseURL,
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
                                "owned_by": "garclaw",
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
