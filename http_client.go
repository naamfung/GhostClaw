package main

import (
        "context"
        "fmt"
        "io"
        "net/http"
        "net/url"
        "strings"
        "time"
)

// SecureHTTPClient 安全的 HTTP 客户端
// 统一进行 SSRF 检查，一处配置，处处生效
type SecureHTTPClient struct {
        client         *http.Client
        ssrfEnabled    bool
        allowPrivateIP bool
        allowedHosts   []string
        blockedHosts   []string
}

// 全局安全 HTTP 客户端
var globalSecureHTTPClient *SecureHTTPClient

// 全局安全配置（供查询使用）
var globalSecurityConfig SecurityConfig

// SetSecurityConfig 设置安全配置并初始化安全 HTTP 客户端
// 这是 SSRF 防护的主入口，应在配置加载后调用
func SetSecurityConfig(config SecurityConfig) {
        globalSecurityConfig = config
        // 使用 HTTP 超时配置，默认 120 秒
        timeout := globalTimeoutConfig.HTTP
        if timeout <= 0 {
                timeout = 120
        }
        InitSecureHTTPClient(config, timeout)
}

// GetSecurityConfig 获取当前安全配置
func GetSecurityConfig() SecurityConfig {
        return globalSecurityConfig
}

// IsSSRFProtectionEnabled 返回 SSRF 防护是否启用
func IsSSRFProtectionEnabled() bool {
        return globalSecurityConfig.EnableSSRFProtection
}

// InitSecureHTTPClient 初始化全局安全 HTTP 客户端
// 应在配置加载后调用
func InitSecureHTTPClient(config SecurityConfig, timeoutSeconds int) {
        if timeoutSeconds <= 0 {
                timeoutSeconds = 120
        }

        globalSecureHTTPClient = &SecureHTTPClient{
                client: &http.Client{
                        Timeout: time.Duration(timeoutSeconds) * time.Second,
                        CheckRedirect: func(req *http.Request, via []*http.Request) error {
                                // 重定向时也检查
                                if globalSecureHTTPClient != nil && globalSecureHTTPClient.ssrfEnabled {
                                        if err := globalSecureHTTPClient.validateURL(req.URL.String()); err != nil {
                                                return fmt.Errorf("重定向被阻止: %w", err)
                                        }
                                }
                                return nil
                        },
                },
                ssrfEnabled:    config.EnableSSRFProtection,
                allowPrivateIP: config.AllowPrivateIPs,
                allowedHosts:   config.AllowedHosts,
                blockedHosts:   config.BlockedHosts,
        }
}

// GetSecureHTTPClient 获取安全 HTTP 客户端
func GetSecureHTTPClient() *SecureHTTPClient {
        if globalSecureHTTPClient == nil {
                // 使用默认安全配置初始化
                InitSecureHTTPClient(SecurityConfig{EnableSSRFProtection: true}, 120)
        }
        return globalSecureHTTPClient
}

// validateURL 内部 URL 验证（核心逻辑）
func (c *SecureHTTPClient) validateURL(rawURL string) error {
        parsedURL, err := url.Parse(rawURL)
        if err != nil {
                return fmt.Errorf("无效的 URL: %w", err)
        }

        // 检查协议
        scheme := strings.ToLower(parsedURL.Scheme)
        if scheme != "http" && scheme != "https" {
                return fmt.Errorf("仅支持 HTTP/HTTPS 协议")
        }

        host := parsedURL.Hostname()

        // 检查白名单（如果配置了白名单，只允许白名单中的主机）
        if len(c.allowedHosts) > 0 {
                allowed := false
                for _, ah := range c.allowedHosts {
                        if host == ah || strings.HasSuffix(host, "."+ah) {
                                allowed = true
                                break
                        }
                }
                if !allowed {
                        return fmt.Errorf("主机不在白名单中: %s", host)
                }
        }

        // 检查配置的黑名单
        for _, bh := range c.blockedHosts {
                if host == bh || strings.HasSuffix(host, "."+bh) {
                        return fmt.Errorf("禁止访问主机: %s", host)
                }
        }

        // 如果允许私有 IP，跳过 SSRF 检查
        if c.allowPrivateIP {
                return nil
        }

        // SSRF 核心检查
        result := CheckURLSSRF(rawURL)
        if !result.Safe {
                return fmt.Errorf("%s", result.Reason)
        }

        return nil
}

// ========== 公开 API ==========

// CheckURL 检查 URL 是否安全（不发送请求）
func (c *SecureHTTPClient) CheckURL(rawURL string) error {
        if !c.ssrfEnabled {
                return nil
        }
        return c.validateURL(rawURL)
}

// IsSSRFEnabled 返回 SSRF 防护是否启用
func (c *SecureHTTPClient) IsSSRFEnabled() bool {
        return c.ssrfEnabled
}

// Get 发送 GET 请求（自动 SSRF 检查）
func (c *SecureHTTPClient) Get(ctx context.Context, rawURL string) (*http.Response, error) {
        if err := c.CheckURL(rawURL); err != nil {
                return nil, err
        }
        req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
        if err != nil {
                return nil, err
        }
        return c.client.Do(req)
}

// Post 发送 POST 请求（自动 SSRF 检查）
func (c *SecureHTTPClient) Post(ctx context.Context, rawURL string, body io.Reader, contentType string) (*http.Response, error) {
        if err := c.CheckURL(rawURL); err != nil {
                return nil, err
        }
        req, err := http.NewRequestWithContext(ctx, "POST", rawURL, body)
        if err != nil {
                return nil, err
        }
        if contentType != "" {
                req.Header.Set("Content-Type", contentType)
        }
        return c.client.Do(req)
}

// Do 发送自定义请求（自动 SSRF 检查）
func (c *SecureHTTPClient) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
        if err := c.CheckURL(req.URL.String()); err != nil {
                return nil, err
        }
        return c.client.Do(req.WithContext(ctx))
}

// ========== 全局便捷函数 ==========

// ValidateURLForFetch 统一的 URL 验证入口
// 所有需要访问外部 URL 的地方都应调用此函数
func ValidateURLForFetch(rawURL string) error {
        return GetSecureHTTPClient().CheckURL(rawURL)
}

// SafeHTTPGet 安全的 HTTP GET 请求
func SafeHTTPGet(ctx context.Context, url string) (*http.Response, error) {
        return GetSecureHTTPClient().Get(ctx, url)
}

// SafeHTTPPost 安全的 HTTP POST 请求
func SafeHTTPPost(ctx context.Context, url string, body io.Reader, contentType string) (*http.Response, error) {
        return GetSecureHTTPClient().Post(ctx, url, body, contentType)
}
