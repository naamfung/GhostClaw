package main

import (
        "context"
        "fmt"
        "log"
        "sync"
        "time"

        "github.com/go-rod/rod"
        "github.com/go-rod/rod/lib/proto"
)

// ============================================================
// 浏览器会话管理器
// 支持持久浏览器实例与多标签页管理
// ============================================================

// BrowserSession 浏览器会话
type BrowserSession struct {
        ID         string
        Browser    *rod.Browser
        Pages      map[string]*rod.Page
        ActivePage string
        CreatedAt  time.Time
        LastUsed   time.Time
        mu         sync.Mutex
}

// BrowserSessionManager 浏览器会话管理器
type BrowserSessionManager struct {
        sessions    map[string]*BrowserSession
        mu          sync.RWMutex
        closeMu     sync.Mutex // 用于避免重复关闭
}

var (
        globalBrowserSessionManager *BrowserSessionManager
        browserSessionOnce          sync.Once
)

// GetBrowserSessionManager 获取全局浏览器会话管理器
func GetBrowserSessionManager() *BrowserSessionManager {
        browserSessionOnce.Do(func() {
                globalBrowserSessionManager = &BrowserSessionManager{
                        sessions: make(map[string]*BrowserSession),
                }
                // 启动空闲会话清理协程
                go globalBrowserSessionManager.cleanupIdleSessions()
        })
        return globalBrowserSessionManager
}

// CreateSession 创建新的浏览器会话
// 如果会话已存在，直接返回现有会话并更新 LastUsed
func (m *BrowserSessionManager) CreateSession(sessionID string) (*BrowserSession, error) {
        m.mu.Lock()
        defer m.mu.Unlock()

        // 如果会话已存在，直接返回（在 sess.mu 下更新 LastUsed）
        if sess, ok := m.sessions[sessionID]; ok {
                sess.mu.Lock()
                sess.LastUsed = time.Now()
                sess.mu.Unlock()
                return sess, nil
        }

        // 启动浏览器
        browser, err := launchBrowserRod()
        if err != nil {
                return nil, fmt.Errorf("启动浏览器失败: %w", err)
        }

        now := time.Now()
        sess := &BrowserSession{
                ID:        sessionID,
                Browser:   browser,
                Pages:     make(map[string]*rod.Page),
                CreatedAt: now,
                LastUsed:  now,
        }

        m.sessions[sessionID] = sess
        log.Printf("[BrowserSessionManager] Created session %s", sessionID)
        return sess, nil
}

// GetSession 获取会话（不创建）
func (m *BrowserSessionManager) GetSession(sessionID string) (*BrowserSession, bool) {
        m.mu.RLock()
        sess, ok := m.sessions[sessionID]
        m.mu.RUnlock()
        if ok {
                // 更新 LastUsed 必須在 sess.mu 下進行，避免與 cleanupIdleSessions 等讀取競爭
                sess.mu.Lock()
                sess.LastUsed = time.Now()
                sess.mu.Unlock()
        }
        return sess, ok
}

// CloseSession 关闭并移除指定的浏览器会话
// 这是防止资源泄漏的关键方法，调用方应在 GlobalSession 停止时调用此方法
func (m *BrowserSessionManager) CloseSession(sessionID string) error {
    m.closeMu.Lock()
    defer m.closeMu.Unlock()

    m.mu.Lock()
    sess, ok := m.sessions[sessionID]
    if !ok {
        m.mu.Unlock()
        return nil
    }
    delete(m.sessions, sessionID)
    m.mu.Unlock()

    if sess.Browser != nil {
        log.Printf("[BrowserSessionManager] Closing browser session %s", sessionID)
        sess.Browser.Close()
    }
    return nil
}

// CloseAllSessions 关闭所有浏览器会话（用于程序退出时清理）
func (m *BrowserSessionManager) CloseAllSessions() {
        // 使用寫鎖拍快照並清空 map，防止 CloseAllSessions 和 CreateSession 之間的窗口泄漏
        m.mu.Lock()
        sessions := make([]*BrowserSession, 0, len(m.sessions))
        for _, sess := range m.sessions {
                sessions = append(sessions, sess)
        }
        m.sessions = make(map[string]*BrowserSession)
        m.mu.Unlock()

        for _, sess := range sessions {
                if sess.Browser != nil {
                        sess.Browser.Close()
                }
        }
        log.Printf("[BrowserSessionManager] All browser sessions closed")
}

// cleanupIdleSessions 定期清理空闲超时的会话
// 空闲超时时间默认 30 分钟
func (m *BrowserSessionManager) cleanupIdleSessions() {
        ticker := time.NewTicker(5 * time.Minute)
        defer ticker.Stop()

        for range ticker.C {
                m.cleanupIdleSessionsOnce()
        }
}

func (m *BrowserSessionManager) cleanupIdleSessionsOnce() {
        idleThreshold := 30 * time.Minute
        now := time.Now()
        toClose := make([]string, 0)

        m.mu.RLock()
        for id, sess := range m.sessions {
                sess.mu.Lock()
                lastUsed := sess.LastUsed
                sess.mu.Unlock()
                if now.Sub(lastUsed) > idleThreshold {
                        toClose = append(toClose, id)
                }
        }
        m.mu.RUnlock()

        for _, id := range toClose {
                if err := m.CloseSession(id); err != nil {
                        log.Printf("[BrowserSessionManager] Failed to close idle session %s: %v", id, err)
                } else {
                        log.Printf("[BrowserSessionManager] Closed idle session %s", id)
                }
        }
}

// CreatePage 在会话中创建新页面
func (s *BrowserSession) CreatePage(pageID string, url string) (*rod.Page, error) {
        s.mu.Lock()
        defer s.mu.Unlock()

        // 创建新页面
        page, err := s.Browser.Page(proto.TargetCreateTarget{URL: url})
        if err != nil {
                return nil, fmt.Errorf("创建页面失败: %w", err)
        }

        s.Pages[pageID] = page
        s.ActivePage = pageID
        s.LastUsed = time.Now()

        return page, nil
}

// GetPage 获取页面
func (s *BrowserSession) GetPage(pageID string) (*rod.Page, bool) {
        s.mu.Lock()
        defer s.mu.Unlock()

        page, ok := s.Pages[pageID]
        if ok {
                s.LastUsed = time.Now()
        }
        return page, ok
}

// GetActivePage 获取当前活动页面
func (s *BrowserSession) GetActivePage() (*rod.Page, bool) {
        s.mu.Lock()
        defer s.mu.Unlock()

        if s.ActivePage == "" {
                return nil, false
        }
        page, ok := s.Pages[s.ActivePage]
        s.LastUsed = time.Now()
        return page, ok
}

// SetActivePage 设置活动页面
func (s *BrowserSession) SetActivePage(pageID string) error {
        s.mu.Lock()
        defer s.mu.Unlock()

        if _, ok := s.Pages[pageID]; !ok {
                return fmt.Errorf("页面 %s 不存在", pageID)
        }
        s.ActivePage = pageID
        s.LastUsed = time.Now()
        return nil
}

// ClosePage 关闭页面
func (s *BrowserSession) ClosePage(pageID string) error {
        s.mu.Lock()
        defer s.mu.Unlock()

        page, ok := s.Pages[pageID]
        if !ok {
                return nil
        }

        if page != nil {
                page.Close()
        }
        delete(s.Pages, pageID)

        // 如果关闭的是活动页面，切换到其他页面
        if s.ActivePage == pageID {
                s.ActivePage = ""
                for id := range s.Pages {
                        s.ActivePage = id
                        break
                }
        }
        s.LastUsed = time.Now()
        return nil
}

// ListPages 列出所有页面
func (s *BrowserSession) ListPages() []PageInfo {
        s.mu.Lock()
        defer s.mu.Unlock()

        var pages []PageInfo
        for id, page := range s.Pages {
                info, _ := page.Info()
                pi := PageInfo{
                        ID:     id,
                        URL:    info.URL,
                        Title:  info.Title,
                        Active: id == s.ActivePage,
                }
                pages = append(pages, pi)
        }
        return pages
}

// PageInfo 页面信息
type PageInfo struct {
        ID     string `json:"id"`
        URL    string `json:"url"`
        Title  string `json:"title"`
        Active bool   `json:"active"`
}

// ============================================================
// 浏览器会话工具函数
// ============================================================

// BrowserSessionCreateResult 创建会话结果
type BrowserSessionCreateResult struct {
        Success   bool   `json:"success"`
        SessionID string `json:"session_id"`
        Message   string `json:"message"`
}

// BrowserSessionCreate 创建新的浏览器会话
func BrowserSessionCreate(sessionID string) (*BrowserSessionCreateResult, error) {
        mgr := GetBrowserSessionManager()
        _, err := mgr.CreateSession(sessionID)
        if err != nil {
                return &BrowserSessionCreateResult{
                        Success: false,
                        Message: err.Error(),
                }, nil
        }

        return &BrowserSessionCreateResult{
                Success:   true,
                SessionID: sessionID,
                Message:   "浏览器会话创建成功",
        }, nil
}

// BrowserSessionCloseResult 关闭会话结果
type BrowserSessionCloseResult struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
}

// BrowserSessionClose 关闭浏览器会话（应在 GlobalSession 停止时调用）
func BrowserSessionClose(sessionID string) (*BrowserSessionCloseResult, error) {
        mgr := GetBrowserSessionManager()
        err := mgr.CloseSession(sessionID)
        if err != nil {
                return &BrowserSessionCloseResult{
                        Success: false,
                        Message: err.Error(),
                }, nil
        }

        return &BrowserSessionCloseResult{
                Success: true,
                Message: "浏览器会话已关闭",
        }, nil
}

// BrowserPageCreateResult 创建页面结果
type BrowserPageCreateResult struct {
        Success bool       `json:"success"`
        PageID  string     `json:"page_id"`
        URL     string     `json:"url"`
        Title   string     `json:"title"`
        Pages   []PageInfo `json:"pages"`
}

// BrowserPageCreate 在会话中创建新页面
func BrowserPageCreate(sessionID, pageID, url string) (*BrowserPageCreateResult, error) {
        mgr := GetBrowserSessionManager()
        sess, ok := mgr.GetSession(sessionID)
        if !ok {
                // 自动创建会话
                var err error
                sess, err = mgr.CreateSession(sessionID)
                if err != nil {
                        return nil, err
                }
        }

        page, err := sess.CreatePage(pageID, url)
        if err != nil {
                return nil, err
        }

        // 等待页面加载
        timeout := globalTimeoutConfig.Browser
        if timeout <= 0 {
                timeout = DefaultBrowserTimeout
        }
        ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
        defer cancel()
        page = page.Context(ctx)

        if err := page.WaitLoad(); err != nil {
                log.Printf("页面加载警告: %v", err)
        }

        info, _ := page.Info()

        return &BrowserPageCreateResult{
                Success: true,
                PageID:  pageID,
                URL:     info.URL,
                Title:   info.Title,
                Pages:   sess.ListPages(),
        }, nil
}

// BrowserPageListResult 列出页面结果
type BrowserPageListResult struct {
        Success bool       `json:"success"`
        Pages   []PageInfo `json:"pages"`
}

// BrowserPageList 列出会话中的所有页面
func BrowserPageList(sessionID string) (*BrowserPageListResult, error) {
        mgr := GetBrowserSessionManager()
        sess, ok := mgr.GetSession(sessionID)
        if !ok {
                return &BrowserPageListResult{
                        Success: false,
                        Pages:   []PageInfo{},
                }, nil
        }

        return &BrowserPageListResult{
                Success: true,
                Pages:   sess.ListPages(),
        }, nil
}

// BrowserPageSwitch 切换活动页面
func BrowserPageSwitch(sessionID, pageID string) error {
        mgr := GetBrowserSessionManager()
        sess, ok := mgr.GetSession(sessionID)
        if !ok {
                return fmt.Errorf("会话 %s 不存在", sessionID)
        }

        return sess.SetActivePage(pageID)
}

// BrowserPageClose 关闭页面
func BrowserPageClose(sessionID, pageID string) error {
        mgr := GetBrowserSessionManager()
        sess, ok := mgr.GetSession(sessionID)
        if !ok {
                return fmt.Errorf("会话 %s 不存在", sessionID)
        }

        return sess.ClosePage(pageID)
}

// 注意：获取或创建页面的辅助函数已统一到 browser_tools.go 中的 getOrCreatePage()
