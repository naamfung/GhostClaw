package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// SSRF 安全防护模块
// 阻止访问私有 IP 地址和内部网络资源

// 私有 IP 地址段
var privateIPBlocks = []string{
	"10.0.0.0/8",      // RFC 1918
	"172.16.0.0/12",   // RFC 1918
	"192.168.0.0/16",  // RFC 1918
	"127.0.0.0/8",     // Loopback
	"169.254.0.0/16",  // Link-local
	"::1/128",         // IPv6 Loopback
	"fe80::/10",       // IPv6 Link-local
	"fc00::/7",        // IPv6 Unique Local
}

// 特殊域名黑名单
var blockedHosts = []string{
	"localhost",
	"localhost.localdomain",
	"ip6-localhost",
	"ip6-loopback",
	"metadata.google.internal",  // GCP metadata
	"instance-data",              // AWS metadata
	"kubernetes",                 // K8s internal
	"kubernetes.default",         // K8s internal
	"kubernetes.default.svc",     // K8s internal
}

// 特殊 IP 黑名单（云服务元数据端点）
var blockedIPs = map[string]bool{
	"169.254.169.254": true, // AWS/GCP/Azure metadata
	"100.100.100.200": true, // Alibaba Cloud metadata
}

// 内部域名后缀
var internalSuffixes = []string{".internal", ".local", ".localhost", ".localdomain"}

// SSRFCheckResult SSRF 检查结果
type SSRFCheckResult struct {
	Safe    bool
	Reason  string
	Blocked bool
}

// IsPrivateIP 检查 IP 是否为私有地址
func IsPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	// 检查是否在黑名单中
	if blockedIPs[ipStr] {
		return true
	}

	// 检查是否在私有 IP 段中
	for _, block := range privateIPBlocks {
		_, cidr, err := net.ParseCIDR(block)
		if err != nil {
			continue
		}
		if cidr.Contains(ip) {
			return true
		}
	}

	return false
}

// IsBlockedHost 检查主机名是否被阻止
func IsBlockedHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))

	// 检查域名黑名单
	for _, blocked := range blockedHosts {
		if host == blocked || strings.HasSuffix(host, "."+blocked) {
			return true
		}
	}

	// 检查内部域名后缀
	for _, suffix := range internalSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}

	return false
}

// CheckURLSSRF 检查 URL 是否安全（核心 SSRF 检查逻辑）
// 此函数不考虑配置开关，仅做纯检查
func CheckURLSSRF(rawURL string) SSRFCheckResult {
	// 解析 URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return SSRFCheckResult{
			Safe:    false,
			Reason:  fmt.Sprintf("无效的 URL: %v", err),
			Blocked: true,
		}
	}

	// 检查协议
	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return SSRFCheckResult{
			Safe:    false,
			Reason:  fmt.Sprintf("仅支持 HTTP/HTTPS 协议，不支持: %s", scheme),
			Blocked: true,
		}
	}

	host := parsedURL.Hostname()

	// 检查主机名黑名单
	if IsBlockedHost(host) {
		return SSRFCheckResult{
			Safe:    false,
			Reason:  fmt.Sprintf("禁止访问内部主机: %s", host),
			Blocked: true,
		}
	}

	// 解析 IP 地址
	ips, err := net.LookupIP(host)
	if err != nil {
		// DNS 解析失败，检查是否为内部域名
		for _, suffix := range internalSuffixes {
			if strings.HasSuffix(host, suffix) {
				return SSRFCheckResult{
					Safe:    false,
					Reason:  fmt.Sprintf("禁止访问内部域名: %s", host),
					Blocked: true,
				}
			}
		}
		// 公网域名 DNS 解析失败，允许继续（后续请求会失败）
		return SSRFCheckResult{
			Safe:    true,
			Reason:  "",
			Blocked: false,
		}
	}

	// 检查所有解析出的 IP
	for _, ip := range ips {
		ipStr := ip.String()
		if IsPrivateIP(ipStr) {
			return SSRFCheckResult{
				Safe:    false,
				Reason:  fmt.Sprintf("禁止访问私有 IP 地址: %s (%s)", host, ipStr),
				Blocked: true,
			}
		}
	}

	return SSRFCheckResult{
		Safe:    true,
		Reason:  "",
		Blocked: false,
	}
}

// IsInternalCommand 检查命令字符串中是否包含内部 URL
// 用于 shell 命令的安全检查
func IsInternalCommand(command string) bool {
	lowerCmd := strings.ToLower(command)

	internalPatterns := []string{
		"localhost",
		"127.0.0.1",
		"169.254.169.254",
		"100.100.100.200",
		"metadata.google.internal",
		".internal",
		".local",
		"0.0.0.0",
		"::1",
	}

	for _, pattern := range internalPatterns {
		if strings.Contains(lowerCmd, pattern) {
			return true
		}
	}

	return false
}

// SanitizeURL 清理 URL，移除敏感信息
func SanitizeURL(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	// 移除 URL 中的密码部分
	if parsedURL.User != nil {
		if _, hasPassword := parsedURL.User.Password(); hasPassword {
			parsedURL.User = url.User(parsedURL.User.Username())
		}
	}

	return parsedURL.String()
}
