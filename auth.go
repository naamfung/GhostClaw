package main

import (
        "crypto/rand"
        "encoding/hex"
        "encoding/json"
        "fmt"
        "log"
        "net/http"
        "os"
        "strings"
        "path/filepath"
        "sync"
        "time"
)

// ============================================================
// 认证模块
// 提供网页登录密码验证功能
// ============================================================

// AuthManager 认证管理器
type AuthManager struct {
        config      *AuthConfig
        sessions    map[string]*AuthSession
        mu          sync.RWMutex
        persistFile string
}

// AuthSession 认证会话
type AuthSession struct {
        Token     string
        CreatedAt time.Time
        ExpiresAt time.Time
        UserAgent string
        IP        string
}

// NewAuthManager 创建认证管理器
func NewAuthManager(config *AuthConfig) *AuthManager {
        // 认证会话文件放在数据目录
        persistFile := filepath.Join(globalDataDir, "auth_sessions.json")

        am := &AuthManager{
                config:      config,
                sessions:    make(map[string]*AuthSession),
                persistFile: persistFile,
        }
        // 加载已有会话
        am.loadSessions()
        if config.SessionToken != "" {
                am.sessions[config.SessionToken] = &AuthSession{
                        Token:     config.SessionToken,
                        CreatedAt: time.Now(),
                        ExpiresAt: time.Now().Add(time.Duration(config.TokenExpiry) * time.Hour),
                }
        }
        go am.cleanupExpiredSessions()
        return am
}

func (am *AuthManager) saveSessions() {
        data, err := json.Marshal(am.sessions)
        if err != nil {
                log.Printf("Failed to marshal auth sessions: %v", err)
                return
        }
        os.WriteFile(am.persistFile, data, 0600)
}

func (am *AuthManager) loadSessions() {
        data, err := os.ReadFile(am.persistFile)
        if err != nil {
                return
        }
        var sessions map[string]*AuthSession
        if err := json.Unmarshal(data, &sessions); err != nil {
                log.Printf("Failed to unmarshal auth sessions: %v", err)
                return
        }
        // 过滤过期的
        now := time.Now()
        for k, v := range sessions {
                if now.After(v.ExpiresAt) {
                        delete(sessions, k)
                }
        }
        am.sessions = sessions
        am.saveSessions()
}

// generateToken 生成随机令牌
func generateToken() string {
        b := make([]byte, 32)
        if _, err := rand.Read(b); err != nil {
                log.Fatalf("[Auth] Failed to generate random token: %v", err)
        }
        return hex.EncodeToString(b)
}

// ValidatePassword 验证密码
func (am *AuthManager) ValidatePassword(password string) bool {
        return password == am.config.Password
}

// CreateSession 创建会话
func (am *AuthManager) CreateSession(userAgent, ip string) *AuthSession {
        token := generateToken()
        expiry := time.Duration(am.config.TokenExpiry) * time.Hour
        if expiry == 0 {
                expiry = 24 * time.Hour
        }

        session := &AuthSession{
                Token:     token,
                CreatedAt: time.Now(),
                ExpiresAt: time.Now().Add(expiry),
                UserAgent: userAgent,
                IP:        ip,
        }

        am.mu.Lock()
        am.sessions[token] = session
        am.mu.Unlock()

        return session
}

// ValidateToken 验证令牌
func (am *AuthManager) ValidateToken(token string) bool {
        if token == "" {
                return false
        }

        am.mu.Lock()
        defer am.mu.Unlock()

        session, exists := am.sessions[token]
        if !exists {
                return false
        }

        // 检查是否过期
        if time.Now().After(session.ExpiresAt) {
                delete(am.sessions, token)
                return false
        }

        return true
}

// Logout 登出
func (am *AuthManager) Logout(token string) {
        am.mu.Lock()
        delete(am.sessions, token)
        am.mu.Unlock()
}

// cleanupExpiredSessions 清理过期会话
func (am *AuthManager) cleanupExpiredSessions() {
        ticker := time.NewTicker(1 * time.Hour)
        for range ticker.C {
                am.mu.Lock()
                now := time.Now()
                for token, session := range am.sessions {
                        if now.After(session.ExpiresAt) {
                                delete(am.sessions, token)
                        }
                }
                am.mu.Unlock()
        }
}

// GetSessionCount 获取会话数量
func (am *AuthManager) GetSessionCount() int {
        am.mu.RLock()
        defer am.mu.RUnlock()
        return len(am.sessions)
}

// ============================================================
// HTTP 处理函数
// ============================================================

// GetLoginPageHTML 获取登录页面 HTML
func GetLoginPageHTML(errorMsg string) string {
        errorHTML := ""
        if errorMsg != "" {
                errorHTML = fmt.Sprintf(`<div class="error">%s</div>`, errorMsg)
        }

        return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>GhostClaw - 登录</title>
    <link rel="icon" href="data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMjU2IiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIGhlaWdodD0iMjU2IiBpZD0ic2NyZWVuc2hvdC1lZjk0ZmJiMC1kYmFiLTgwZWQtODAwNi04OTQyOTkwMGVkYmYiIHZpZXdCb3g9IjAgMCAyNTYgMjU2IiB4bWxuczp4bGluaz0iaHR0cDovL3d3dy53My5vcmcvMTk5OS94bGluayIgZmlsbD0ibm9uZSIgdmVyc2lvbj0iMS4xIj48ZyBpZD0ic2hhcGUtZWY5NGZiYjAtZGJhYi04MGVkLTgwMDYtODk0Mjk5MDBlZGJmIiByeD0iMCIgcnk9IjAiPjxnIGlkPSJzaGFwZS1lZjk0ZmJiMC1kYmFiLTgwZWQtODAwNi04OTQyMTU3NTVjM2EiPjxnIGNsYXNzPSJmaWxscyIgaWQ9ImZpbGxzLWVmOTRmYmIwLWRiYWItODBlZC04MDA2LTg5NDIxNTc1NWMzYSI+PHJlY3Qgcng9IjAiIHJ5PSIwIiB4PSIwIiB5PSIwIiB0cmFuc2Zvcm09Im1hdHJpeCgxLjAwMDAwMCwgMC4wMDAwMDAsIDAuMDAwMDAwLCAxLjAwMDAwMCwgMC4wMDAwMDAsIDAuMDAwMDAwKSIgd2lkdGg9IjI1NiIgaGVpZ2h0PSIyNTYiIHN0eWxlPSJmaWxsOiByZ2IoMjcsIDMxLCAzMik7IGZpbGwtb3BhY2l0eTogMTsiLz48L2c+PC9nPjxnIGlkPSJzaGFwZS1lZjk0ZmJiMC1kYmFiLTgwZWQtODAwNi04OTQyMjM2M2VmM2YiIHJ4PSIwIiByeT0iMCI+PGcgaWQ9InNoYXBlLWVmOTRmYmIwLWRiYWItODBlZC04MDA2LTg5NDIyMzYzZWY0MCI+PGcgY2xhc3M9ImZpbGxzIiBpZD0iZmlsbHMtZWY5NGZiYjAtZGJhYi04MGVkLTgwMDYtODk0MjIzNjNlZjQwIj48cGF0aCBkPSJNMTcxLjY2NTAwODU0NDkyMTg4LDk5LjUzMDI1MDU0OTMxNjRMMTU5Ljc5OTUzMDAyOTI5Njg4LDEyMC42MjQ2ODcxOTQ4MjQyMkMxNDQuMTU0NTEwNDk4MDQ2ODgsMTA4LjU4MzI5MDEwMDA5NzY2LDEyMC45NTA0MTY1NjQ5NDE0LDEwNi44MjU0MTY1NjQ5NDE0LDEwNS4zMDUzOTcwMzM2OTE0LDExOS43NDU3NTA0MjcyNDYxQzgwLjA3OTgxMTA5NjE5MTQsMTQwLjU3NjUyMjgyNzE0ODQ0LDgxLjgzNzYyMzU5NjE5MTQsMTg4Ljc0MjI2Mzc5Mzk0NTMsMTIxLjEyNjE5NzgxNDk0MTQsMTg5LjAwNTg3NDYzMzc4OTA2QzEzMi4xMTMwMDY1OTE3OTY4OCwxODkuMDA1ODc0NjMzNzg5MDYsMTQxLjQyOTY1Njk4MjQyMTg4LDE4My44MjAxMTQxMzU3NDIyLDE1MS40NDk2NzY1MTM2NzE4OCwxODAuMzkyMzQ5MjQzMTY0MDZMMTU2LjcyMzM1ODE1NDI5Njg4LDIwMS4zOTg4NDk0ODczMDQ3QzE0Ny44NDU5MTY3NDgwNDY4OCwyMDUuNTI5ODkxOTY3NzczNDQsMTM4Ljc5MjkzODIzMjQyMTg4LDIwOS43NDg3MzM1MjA1MDc4LDEyOS4wMzY4MzQ3MTY3OTY4OCwyMTEuMDY3MTIzNDEzMDg1OTRDNDAuMDg4MzUyMjAzMzY5MTQsMjIzLjE5NjQ1NjkwOTE3OTcsNDUuMTg2MDA4NDUzMzY5MTQsOTQuNzg0MDA0MjExNDI1NzgsMTI1LjYwODg2MzgzMDU2NjQsODguMTA0MDcyNTcwODAwNzhDMTQyLjQ4NDM0NDQ4MjQyMTg4LDg2LjY5NzgyMjU3MDgwMDc4LDE1Ny4zMzgzNDgzODg2NzE4OCw5MS4wOTI0NzU4OTExMTMyOCwxNzEuNzUzMTQzMzEwNTQ2ODgsOTkuNTMwMjUwNTQ5MzE2NFoiIGNsYXNzPSJzdDAiIHN0eWxlPSJmaWxsOiByZ2IoMjU1LCAxMzAsIDU0KTsgZmlsbC1vcGFjaXR5OiAxOyIvPjwvZz48L2c+PC9nPjwvZz48L3N2Zz4=" />
    <style>
        :root {
            --bg-primary: #1b1f20;
            --bg-secondary: #242829;
            --bg-tertiary: #2d3132;
            --text-primary: #e8eaeb;
            --text-secondary: #9ca3af;
            --text-muted: #6b7280;
            --accent: #ff8236;
            --accent-hover: #ff9a57;
            --border: #3a3f40;
            --error: #ef4444;
        }
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            background-color: var(--bg-primary);
            color: var(--text-primary);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .login-container {
            width: 100%%;
            max-width: 400px;
            padding: 20px;
        }
        .login-box {
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 16px;
            padding: 40px;
            text-align: center;
        }
        .logo {
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 12px;
            margin-bottom: 32px;
        }
        .logo svg {
            width: 48px;
            height: 48px;
        }
        .logo h1 {
            font-size: 2rem;
            font-weight: 600;
            color: var(--text-primary);
            letter-spacing: -0.02em;
        }
        .logo span {
            color: var(--accent);
        }
        .error {
            background: rgba(239, 68, 68, 0.15);
            border: 1px solid rgba(239, 68, 68, 0.3);
            color: var(--error);
            padding: 12px;
            border-radius: 8px;
            margin-bottom: 20px;
            font-size: 0.9rem;
        }
        .form-group {
            margin-bottom: 20px;
            text-align: left;
        }
        .form-group label {
            display: block;
            margin-bottom: 8px;
            color: var(--text-secondary);
            font-size: 0.9rem;
        }
        .form-group input {
            width: 100%%;
            padding: 14px 18px;
            border: 1px solid var(--border);
            border-radius: 12px;
            background: var(--bg-tertiary);
            color: var(--text-primary);
            font-size: 1rem;
            outline: none;
            transition: all 0.2s ease;
        }
        .form-group input:focus {
            border-color: var(--accent);
            box-shadow: 0 0 0 3px rgba(255, 130, 54, 0.15);
        }
        .login-btn {
            width: 100%%;
            padding: 14px;
            border-radius: 12px;
            background: var(--accent);
            border: none;
            color: white;
            font-size: 1rem;
            font-weight: 500;
            cursor: pointer;
            transition: all 0.2s ease;
        }
        .login-btn:hover {
            background: var(--accent-hover);
            transform: translateY(-1px);
        }
        .login-btn:active {
            transform: translateY(0);
        }
        .footer {
            margin-top: 24px;
            color: var(--text-muted);
            font-size: 0.8rem;
        }
        @media (max-width: 480px) {
            .login-box {
                padding: 30px 24px;
            }
            .logo h1 {
                font-size: 1.5rem;
            }
        }
    </style>
</head>
<body>
    <div class="login-container">
        <div class="login-box">
            <div class="logo">
                <h1>Ghost<span>Claw</span></h1>
            </div>
            %s
            <form method="POST" action="/login">
                <div class="form-group">
                    <label for="password">访问密码</label>
                    <input type="password" id="password" name="password" placeholder="请输入密码" required autofocus>
                </div>
                <button type="submit" class="login-btn">登 录</button>
            </form>
            <p class="footer">GhostClaw AI Agent · 安全访问</p>
        </div>
    </div>
</body>
</html>`, errorHTML)
}

// HandleLoginPage 处理登录页面
func HandleLoginPage(w http.ResponseWriter, r *http.Request) {
        // 检查是否已登录
        cookie, err := r.Cookie("ghostclaw_token")
        if err == nil && globalAuthManager.ValidateToken(cookie.Value) {
                // 已登录，重定向到主页
                http.Redirect(w, r, "/", http.StatusFound)
                return
        }

        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.Write([]byte(GetLoginPageHTML("")))
}

// HandleLogin 处理登录请求
func HandleLogin(w http.ResponseWriter, r *http.Request) {
        if r.Method != "POST" {
                http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }

        password := r.FormValue("password")
        if password == "" {
                w.Header().Set("Content-Type", "text/html; charset=utf-8")
                w.Write([]byte(GetLoginPageHTML("请输入密码")))
                return
        }

        if !globalAuthManager.ValidatePassword(password) {
                w.Header().Set("Content-Type", "text/html; charset=utf-8")
                w.Write([]byte(GetLoginPageHTML("密码错误，请重试")))
                log.Printf("[Auth] Failed login attempt from %s", r.RemoteAddr)
                return
        }

        // 创建会话
        session := globalAuthManager.CreateSession(r.UserAgent(), r.RemoteAddr)

        // 设置 cookie
        expiry := time.Duration(globalAuthManager.config.TokenExpiry) * time.Hour
        if expiry == 0 {
                expiry = 24 * time.Hour
        }

        http.SetCookie(w, &http.Cookie{
                Name:     "ghostclaw_token",
                Value:    session.Token,
                Path:     "/",
                HttpOnly: true,
                Secure:   false, // 如果使用 HTTPS，设为 true
                MaxAge:   int(expiry.Seconds()),
        })

        log.Printf("[Auth] Successful login from %s", r.RemoteAddr)

        // 重定向到主页
        http.Redirect(w, r, "/", http.StatusFound)
}

// HandleLogout 处理登出请求
func HandleLogout(w http.ResponseWriter, r *http.Request) {
        cookie, err := r.Cookie("ghostclaw_token")
        if err == nil {
                globalAuthManager.Logout(cookie.Value)
        }

        // 清除 cookie
        http.SetCookie(w, &http.Cookie{
                Name:     "ghostclaw_token",
                Value:    "",
                Path:     "/",
                HttpOnly: true,
                MaxAge:   -1,
        })

        http.Redirect(w, r, "/login", http.StatusFound)
}

// HandleAPILogin 处理 API 登录请求
func HandleAPILogin(w http.ResponseWriter, r *http.Request) {
        if r.Method != "POST" {
                http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }

        var req struct {
                Password string `json:"password"`
        }

        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                http.Error(w, "Invalid request", http.StatusBadRequest)
                return
        }

        if !globalAuthManager.ValidatePassword(req.Password) {
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusUnauthorized)
                json.NewEncoder(w).Encode(map[string]string{
                        "error": "Invalid password",
                })
                return
        }

        // 创建会话
        session := globalAuthManager.CreateSession(r.UserAgent(), r.RemoteAddr)

        log.Printf("[Auth] API login from %s", r.RemoteAddr)

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]string{
                "token": session.Token,
        })
}

// AuthMiddleware 认证中间件
func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                // 如果未启用认证，直接放行
                if globalAuthManager == nil || !globalAuthManager.config.Enabled {
                        next(w, r)
                        return
                }

                // 检查 cookie
                cookie, err := r.Cookie("ghostclaw_token")
                if err != nil || !globalAuthManager.ValidateToken(cookie.Value) {
                        // 检查是否是 WebSocket 请求（通过 URL 参数传递 token）
                        token := r.URL.Query().Get("token")
                        if token != "" && globalAuthManager.ValidateToken(token) {
                                next(w, r)
                                return
                        }

                        // 检查 Authorization header
                        authHeader := r.Header.Get("Authorization")
                        if authHeader != "" && len(authHeader) > 7 && authHeader[:7] == "Bearer " {
                                token = authHeader[7:]
                                if globalAuthManager.ValidateToken(token) {
                                        next(w, r)
                                        return
                                }
                        }

                        // 未认证，重定向到登录页或返回 401
                        if r.Header.Get("Accept") == "application/json" || strings.HasPrefix(r.URL.Path, "/api") {
                                w.Header().Set("Content-Type", "application/json")
                                w.WriteHeader(http.StatusUnauthorized)
                                json.NewEncoder(w).Encode(map[string]string{
                                        "error": "Unauthorized",
                                })
                        } else {
                                http.Redirect(w, r, "/login", http.StatusFound)
                        }
                        return
                }

                next(w, r)
        }
}

// IsAuthEnabled 检查是否启用了认证
func IsAuthEnabled() bool {
        return globalAuthManager != nil && globalAuthManager.config.Enabled
}

