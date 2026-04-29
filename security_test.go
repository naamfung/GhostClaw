package main

import (
	"strings"
	"testing"
)

// ============================================================================
// IsPrivateIP
// ============================================================================

func TestIsPrivateIP_PrivateIPv4(t *testing.T) {
	tests := []struct {
		ip   string
		desc string
	}{
		{"10.0.0.1", "RFC 1918 - 10.0.0.0/8"},
		{"10.255.255.255", "RFC 1918 - 10.0.0.0/8 upper"},
		{"172.16.0.1", "RFC 1918 - 172.16.0.0/12"},
		{"172.31.255.255", "RFC 1918 - 172.16.0.0/12 upper"},
		{"192.168.0.1", "RFC 1918 - 192.168.0.0/16"},
		{"192.168.255.255", "RFC 1918 - 192.168.0.0/16 upper"},
		{"127.0.0.1", "Loopback"},
		{"127.255.255.255", "Loopback upper"},
		{"169.254.1.1", "Link-local"},
		{"169.254.255.255", "Link-local upper"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if !IsPrivateIP(tt.ip) {
				t.Errorf("IsPrivateIP(%q) = false, want true (%s)", tt.ip, tt.desc)
			}
		})
	}
}

func TestIsPrivateIP_BlockedIPs(t *testing.T) {
	tests := []string{
		"169.254.169.254", // AWS/GCP/Azure metadata
		"100.100.100.200", // Alibaba Cloud metadata
	}

	for _, ip := range tests {
		t.Run(ip, func(t *testing.T) {
			if !IsPrivateIP(ip) {
				t.Errorf("IsPrivateIP(%q) = false, want true (blocked IP)", ip)
			}
		})
	}
}

func TestIsPrivateIP_IPv6Private(t *testing.T) {
	tests := []struct {
		ip   string
		desc string
	}{
		{"::1", "IPv6 loopback"},
		{"fe80::1", "IPv6 link-local"},
		{"fe80::abcd:ef01:2345:6789", "IPv6 link-local random"},
		{"fc00::1", "IPv6 unique local"},
		{"fd00::1", "IPv6 unique local (fd00)"},
		{"fd12:3456:789a:1::1", "IPv6 unique local random"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if !IsPrivateIP(tt.ip) {
				t.Errorf("IsPrivateIP(%q) = false, want true (%s)", tt.ip, tt.desc)
			}
		})
	}
}

func TestIsPrivateIP_PublicIPs(t *testing.T) {
	tests := []string{
		"8.8.8.8",
		"1.1.1.1",
		"93.184.216.34", // example.com
		"2001:4860:4860::8888",
		"2606:2800:220:1:248:1893:25c8:1946",
	}

	for _, ip := range tests {
		t.Run(ip, func(t *testing.T) {
			if IsPrivateIP(ip) {
				t.Errorf("IsPrivateIP(%q) = true, want false", ip)
			}
		})
	}
}

func TestIsPrivateIP_InvalidIP(t *testing.T) {
	tests := []string{
		"",
		"not an ip",
		"256.256.256.256",
		"abc.def.ghi.jkl",
		"192.168.1",
	}

	for _, ip := range tests {
		t.Run(ip, func(t *testing.T) {
			if IsPrivateIP(ip) {
				t.Errorf("IsPrivateIP(%q) = true, want false (invalid IP)", ip)
			}
		})
	}
}

// ============================================================================
// IsBlockedHost
// ============================================================================

func TestIsBlockedHost_ExactMatch(t *testing.T) {
	tests := []string{
		"localhost",
		"localhost.localdomain",
		"ip6-localhost",
		"ip6-loopback",
		"metadata.google.internal",
		"instance-data",
		"kubernetes",
		"kubernetes.default",
		"kubernetes.default.svc",
	}

	for _, host := range tests {
		t.Run(host, func(t *testing.T) {
			if !IsBlockedHost(host) {
				t.Errorf("IsBlockedHost(%q) = false, want true", host)
			}
		})
	}
}

func TestIsBlockedHost_SubdomainMatch(t *testing.T) {
	tests := []struct {
		host   string
		reason string
	}{
		{"foo.localhost", "subdomain of localhost"},
		{"bar.localhost.localdomain", "subdomain of localhost.localdomain"},
		{"api.metadata.google.internal", "subdomain of metadata.google.internal"},
		{"svc.kubernetes.default.svc", "sub-subdomain of kubernetes.default.svc"},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			if !IsBlockedHost(tt.host) {
				t.Errorf("IsBlockedHost(%q) = false, want true (%s)", tt.host, tt.reason)
			}
		})
	}
}

func TestIsBlockedHost_InternalSuffixes(t *testing.T) {
	tests := []string{
		"something.internal",
		"test.local",
		"host.localhost",
		"server.localdomain",
	}

	for _, host := range tests {
		t.Run(host, func(t *testing.T) {
			if !IsBlockedHost(host) {
				t.Errorf("IsBlockedHost(%q) = false, want true (internal suffix)", host)
			}
		})
	}
}

func TestIsBlockedHost_CaseInsensitive(t *testing.T) {
	tests := []string{
		"LOCALHOST",
		"LocalHost",
		"METADATA.GOOGLE.INTERNAL",
		"KUBERNETES.DEFAULT.SVC",
	}

	for _, host := range tests {
		t.Run(host, func(t *testing.T) {
			if !IsBlockedHost(host) {
				t.Errorf("IsBlockedHost(%q) = false, want true (case insensitive)", host)
			}
		})
	}
}

func TestIsBlockedHost_SafeHosts(t *testing.T) {
	tests := []string{
		"google.com",
		"github.com",
		"example.com",
		"api.openai.com",
		"myinternal.com", // .internal as suffix, but host is myinternal.com not *.internal
	}

	for _, host := range tests {
		t.Run(host, func(t *testing.T) {
			if IsBlockedHost(host) {
				t.Errorf("IsBlockedHost(%q) = true, want false", host)
			}
		})
	}
}

func TestIsBlockedHost_WhitespaceHandling(t *testing.T) {
	if !IsBlockedHost("  localhost  ") {
		t.Error("IsBlockedHost with surrounding whitespace should match")
	}
}

// ============================================================================
// CheckURLSSRF
// ============================================================================

func TestCheckURLSSRF_InvalidURL(t *testing.T) {
	tests := []string{
		"",
		"not-a-url",
		"http://[::1]:namedport", // malformed IPv6 URL
	}

	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			result := CheckURLSSRF(rawURL)
			if result.Safe {
				t.Errorf("CheckURLSSRF(%q).Safe = true, want false", rawURL)
			}
			if !result.Blocked {
				t.Errorf("CheckURLSSRF(%q).Blocked = false, want true", rawURL)
			}
			if result.Reason == "" {
				t.Errorf("CheckURLSSRF(%q).Reason should not be empty", rawURL)
			}
		})
	}
}

func TestCheckURLSSRF_UnsupportedScheme(t *testing.T) {
	tests := []struct {
		url    string
		scheme string
	}{
		{"ftp://example.com/file", "ftp"},
		{"file:///etc/passwd", "file"},
		{"gopher://localhost/", "gopher"},
		{"dict://localhost:11211/stats", "dict"},
	}

	for _, tt := range tests {
		t.Run(tt.scheme, func(t *testing.T) {
			result := CheckURLSSRF(tt.url)
			if result.Safe {
				t.Errorf("CheckURLSSRF(%q).Safe = true, want false", tt.url)
			}
			if !result.Blocked {
				t.Errorf("CheckURLSSRF(%q).Blocked = false, want true", tt.url)
			}
			if !strings.Contains(result.Reason, tt.scheme) {
				t.Errorf("CheckURLSSRF(%q).Reason should mention scheme %q, got: %s", tt.url, tt.scheme, result.Reason)
			}
		})
	}
}

func TestCheckURLSSRF_BlockedHost(t *testing.T) {
	tests := []string{
		"http://localhost:8080/path",
		"https://localhost.localdomain/api",
		"http://metadata.google.internal/",
		"https://kubernetes.default.svc/healthz",
	}

	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			result := CheckURLSSRF(rawURL)
			if result.Safe {
				t.Errorf("CheckURLSSRF(%q).Safe = true, want false", rawURL)
			}
			if !result.Blocked {
				t.Errorf("CheckURLSSRF(%q).Blocked = false, want true", rawURL)
			}
		})
	}
}

func TestCheckURLSSRF_BlockedIPDirectly(t *testing.T) {
	tests := []string{
		"http://169.254.169.254/latest/meta-data",
		"http://100.100.100.200/meta-data",
	}

	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			result := CheckURLSSRF(rawURL)
			// These IPs resolve to themselves via DNS or are blocked as private IPs
			if result.Safe {
				t.Errorf("CheckURLSSRF(%q).Safe = true, want false", rawURL)
			}
			if !result.Blocked {
				t.Errorf("CheckURLSSRF(%q).Blocked = false, want true", rawURL)
			}
		})
	}
}

func TestCheckURLSSRF_PrivateIPRange(t *testing.T) {
	tests := []string{
		"http://127.0.0.1:8080/",
		"http://10.0.0.1/api",
		"http://192.168.1.1/",
	}

	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			result := CheckURLSSRF(rawURL)
			if result.Safe {
				t.Errorf("CheckURLSSRF(%q).Safe = true, want false", rawURL)
			}
			if !result.Blocked {
				t.Errorf("CheckURLSSRF(%q).Blocked = false, want true", rawURL)
			}
		})
	}
}

func TestCheckURLSSRF_PublicURL(t *testing.T) {
	tests := []string{
		"https://google.com",
		"https://api.github.com/repos",
		"https://example.com/path?query=1",
	}

	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			result := CheckURLSSRF(rawURL)
			if !result.Safe {
				t.Errorf("CheckURLSSRF(%q).Safe = false, want true. Reason: %s", rawURL, result.Reason)
			}
			if result.Blocked {
				t.Errorf("CheckURLSSRF(%q).Blocked = true, want false", rawURL)
			}
		})
	}
}

func TestCheckURLSSRF_ResultStructValues(t *testing.T) {
	t.Run("safe result", func(t *testing.T) {
		result := CheckURLSSRF("https://example.com")
		if !result.Safe {
			t.Error("Safe should be true")
		}
		if result.Blocked {
			t.Error("Blocked should be false")
		}
		if result.Reason != "" {
			t.Errorf("Reason should be empty, got: %s", result.Reason)
		}
	})

	t.Run("blocked result", func(t *testing.T) {
		result := CheckURLSSRF("http://localhost")
		if result.Safe {
			t.Error("Safe should be false")
		}
		if !result.Blocked {
			t.Error("Blocked should be true")
		}
		if result.Reason == "" {
			t.Error("Reason should not be empty")
		}
	})
}

// ============================================================================
// IsInternalCommand
// ============================================================================

func TestIsInternalCommand_MatchingPatterns(t *testing.T) {
	tests := []struct {
		cmd  string
		desc string
	}{
		{"curl http://localhost:8080", "contains localhost"},
		{"wget 127.0.0.1/api", "contains 127.0.0.1"},
		{"ping 169.254.169.254", "contains metadata IP"},
		{"dig 100.100.100.200", "contains alibaba metadata IP"},
		{"curl metadata.google.internal", "contains GCP metadata"},
		{"ssh admin@server.internal", "contains .internal suffix"},
		{"nslookup test.local", "contains .local suffix"},
		{"telnet 0.0.0.0 80", "contains 0.0.0.0"},
		{"curl [::1]:8080", "contains IPv6 loopback"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if !IsInternalCommand(tt.cmd) {
				t.Errorf("IsInternalCommand(%q) = false, want true (%s)", tt.cmd, tt.desc)
			}
		})
	}
}

func TestIsInternalCommand_CaseInsensitive(t *testing.T) {
	tests := []string{
		"curl LOCALHOST:8080",
		"wget Metadata.Google.Internal",
		"ping 169.254.169.254",
	}

	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			if !IsInternalCommand(cmd) {
				t.Errorf("IsInternalCommand(%q) = false, want true", cmd)
			}
		})
	}
}

func TestIsInternalCommand_SafeCommands(t *testing.T) {
	tests := []string{
		"ls -la",
		"git status",
		"go build ./...",
		"curl https://api.github.com",
		"cat README.md",
		"echo hello world",
	}

	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			if IsInternalCommand(cmd) {
				t.Errorf("IsInternalCommand(%q) = true, want false", cmd)
			}
		})
	}
}

// ============================================================================
// SanitizeURL
// ============================================================================

func TestSanitizeURL_RemovesPassword(t *testing.T) {
	result := SanitizeURL("https://user:password@example.com/path")
	if strings.Contains(result, "password") {
		t.Errorf("SanitizeURL should remove password, got: %s", result)
	}
	if !strings.Contains(result, "user@") {
		t.Errorf("SanitizeURL should keep username, got: %s", result)
	}
	expected := "https://user@example.com/path"
	if result != expected {
		t.Errorf("SanitizeURL = %q, want %q", result, expected)
	}
}

func TestSanitizeURL_UserOnlyNoPassword(t *testing.T) {
	result := SanitizeURL("https://user@example.com/path")
	if result != "https://user@example.com/path" {
		t.Errorf("SanitizeURL = %q, want %q", result, "https://user@example.com/path")
	}
}

func TestSanitizeURL_NoUserInfo(t *testing.T) {
	result := SanitizeURL("https://example.com/path?query=1")
	if result != "https://example.com/path?query=1" {
		t.Errorf("SanitizeURL = %q, want %q", result, "https://example.com/path?query=1")
	}
}

func TestSanitizeURL_InvalidURL(t *testing.T) {
	result := SanitizeURL("not-a-url")
	if result != "not-a-url" {
		t.Errorf("SanitizeURL should return original for invalid URL, got: %s", result)
	}
}

func TestSanitizeURL_EmptyPassword(t *testing.T) {
	result := SanitizeURL("https://user:@example.com/path")
	if result != "https://user@example.com/path" {
		t.Errorf("SanitizeURL should clean empty password, got: %s", result)
	}
}

func TestSanitizeURL_ComplexURL(t *testing.T) {
	result := SanitizeURL("https://admin:secret123@api.internal.example.com:8443/v1/data?key=val#section")
	if strings.Contains(result, "secret123") {
		t.Errorf("SanitizeURL should remove password, got: %s", result)
	}
	if !strings.Contains(result, "admin@") {
		t.Errorf("SanitizeURL should keep username, got: %s", result)
	}
}

// ============================================================================
// Edge cases / regression
// ============================================================================

func TestIsPrivateIP_BoundaryCIDR(t *testing.T) {
	// 172.16.0.0/12 covers 172.16.0.0 - 172.31.255.255
	// Boundary just outside should be public

	t.Run("0.0.0.0 is not private", func(t *testing.T) {
		if IsPrivateIP("0.0.0.0") {
			t.Error("0.0.0.0 should not be considered private")
		}
	})
	t.Run("172.15.255.255 is public (just before 172.16.0.0/12)", func(t *testing.T) {
		if IsPrivateIP("172.15.255.255") {
			t.Error("172.15.255.255 should be public (outside 172.16.0.0/12)")
		}
	})

	t.Run("172.32.0.0 is public (just after 172.31.255.255)", func(t *testing.T) {
		if IsPrivateIP("172.32.0.0") {
			t.Error("172.32.0.0 should be public (outside 172.16.0.0/12)")
		}
	})

	t.Run("172.16.0.0 is private (start of 172.16.0.0/12)", func(t *testing.T) {
		if !IsPrivateIP("172.16.0.0") {
			t.Error("172.16.0.0 should be private (start of 172.16.0.0/12)")
		}
	})

	t.Run("172.31.255.255 is private (end of 172.16.0.0/12)", func(t *testing.T) {
		if !IsPrivateIP("172.31.255.255") {
			t.Error("172.31.255.255 should be private (end of 172.16.0.0/12)")
		}
	})
}

func TestIsBlockedHost_NotConfusedByPartialMatch(t *testing.T) {
	// ".internal" suffix should NOT match "myinternal.com" (the dot matters)
	// Actually looking at the code: strings.HasSuffix(host, ".internal") vs strings.HasSuffix(host, suffix)
	// suffix = ".internal", host = "myinternal.com" -> HasSuffix("myinternal.com", ".internal") = false ✓
	t.Run("myinternal.com should NOT be blocked", func(t *testing.T) {
		if IsBlockedHost("myinternal.com") {
			t.Error("myinternal.com should not be blocked (no dot before 'internal')")
		}
	})
}
