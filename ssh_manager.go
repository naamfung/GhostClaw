package main

import (
    //"crypto/ssh"
    "golang.org/x/crypto/ssh" // FOR GHOSTBSD/FREEBSD
    "fmt"
    "os"
    "log"
    "sync"
    "time"
)

// SSHSession 代表一个到远程主机的持久化 SSH 连接
type SSHSession struct {
    ID         string          // 会话唯一标识，如 "user@host:port"
    Client     *ssh.Client     // SSH 客户端
    Username   string
    Host       string
    Port       int
    CreatedAt  time.Time
    LastUsedAt time.Time
    mu         sync.RWMutex
}

// SSHSessionManager 管理所有活跃的 SSH 连接
type SSHSessionManager struct {
    sessions map[string]*SSHSession
    mu       sync.RWMutex
}

var globalSSHManager *SSHSessionManager

func init() {
    globalSSHManager = &SSHSessionManager{
        sessions: make(map[string]*SSHSession),
    }
}

// Connect 建立一个新的 SSH 连接
func (m *SSHSessionManager) Connect(user, host, password, privateKeyPath string, port int) (string, error) {
    sessionID := fmt.Sprintf("%s@%s:%d", user, host, port)

    m.mu.Lock()
    defer m.mu.Unlock()

    // 如果连接已存在且有效，直接返回
    if sess, exists := m.sessions[sessionID]; exists {
        if _, err := sess.Client.NewSession(); err == nil {
            sess.LastUsedAt = time.Now()
            return sessionID, nil
        }
        // 连接已失效，删除并重建
        delete(m.sessions, sessionID)
    }

    // 配置认证方法
    var authMethods []ssh.AuthMethod
    if privateKeyPath != "" {
        key, err := os.ReadFile(privateKeyPath)
        if err != nil {
            return "", fmt.Errorf("unable to read private key: %w", err)
        }
        signer, err := ssh.ParsePrivateKey(key)
        if err != nil {
            return "", fmt.Errorf("unable to parse private key: %w", err)
        }
        authMethods = []ssh.AuthMethod{ssh.PublicKeys(signer)}
    } else if password != "" {
        authMethods = []ssh.AuthMethod{ssh.Password(password)}
    } else {
        return "", fmt.Errorf("no authentication method provided")
    }

    config := &ssh.ClientConfig{
        User:            user,
        Auth:            authMethods,
        HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: 生产环境替换为更安全的验证方式
        Timeout:         30 * time.Second,
    }

    addr := fmt.Sprintf("%s:%d", host, port)
    client, err := ssh.Dial("tcp", addr, config)
    if err != nil {
        return "", fmt.Errorf("failed to dial: %w", err)
    }

    sess := &SSHSession{
        ID:         sessionID,
        Client:     client,
        Username:   user,
        Host:       host,
        Port:       port,
        CreatedAt:  time.Now(),
        LastUsedAt: time.Now(),
    }

    m.sessions[sessionID] = sess
    log.Printf("[SSH] New connection established: %s", sessionID)
    return sessionID, nil
}

// GetSession 获取一个已存在的 SSH 会话
func (m *SSHSessionManager) GetSession(sessionID string) (*SSHSession, bool) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    sess, ok := m.sessions[sessionID]
    if ok {
        sess.LastUsedAt = time.Now()
    }
    return sess, ok
}

// Close 关闭一个指定的 SSH 会话
func (m *SSHSessionManager) Close(sessionID string) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    if sess, ok := m.sessions[sessionID]; ok {
        err := sess.Client.Close()
        delete(m.sessions, sessionID)
        log.Printf("[SSH] Connection closed: %s", sessionID)
        return err
    }
    return fmt.Errorf("session %s not found", sessionID)
}

// ListSessions 列出所有活跃会话
func (m *SSHSessionManager) ListSessions() []string {
    m.mu.RLock()
    defer m.mu.RUnlock()
    ids := make([]string, 0, len(m.sessions))
    for id := range m.sessions {
        ids = append(ids, id)
    }
    return ids
}
