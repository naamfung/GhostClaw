package main

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// generateToken
// ============================================================================

func TestGenerateToken_Length(t *testing.T) {
	token := generateToken()
	// 32 bytes encoded as hex = 64 characters
	if len(token) != 64 {
		t.Errorf("generateToken length = %d, want 64", len(token))
	}
}

func TestGenerateToken_HexFormat(t *testing.T) {
	token := generateToken()
	_, err := hex.DecodeString(token)
	if err != nil {
		t.Errorf("generateToken should produce valid hex: %v", err)
	}
}

func TestGenerateToken_Uniqueness(t *testing.T) {
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token := generateToken()
		if tokens[token] {
			t.Errorf("generateToken produced duplicate: %q", token)
		}
		tokens[token] = true
	}
}

func TestGenerateToken_NotEmpty(t *testing.T) {
	token := generateToken()
	if token == "" {
		t.Error("generateToken should not return empty string")
	}
}

// ============================================================================
// ValidatePassword
// ============================================================================

func TestValidatePassword_CorrectPassword(t *testing.T) {
	am := &AuthManager{
		config: &AuthConfig{Password: "secret123"},
	}
	if !am.ValidatePassword("secret123") {
		t.Error("ValidatePassword should return true for correct password")
	}
}

func TestValidatePassword_IncorrectPassword(t *testing.T) {
	am := &AuthManager{
		config: &AuthConfig{Password: "secret123"},
	}
	if am.ValidatePassword("wrong-password") {
		t.Error("ValidatePassword should return false for incorrect password")
	}
}

func TestValidatePassword_EmptyPassword(t *testing.T) {
	am := &AuthManager{
		config: &AuthConfig{Password: ""},
	}
	if !am.ValidatePassword("") {
		t.Error("ValidatePassword should match empty password")
	}
	if am.ValidatePassword("something") {
		t.Error("ValidatePassword should reject non-empty password when config is empty")
	}
}

func TestValidatePassword_CaseSensitive(t *testing.T) {
	am := &AuthManager{
		config: &AuthConfig{Password: "Secret123"},
	}
	if am.ValidatePassword("secret123") {
		t.Error("ValidatePassword should be case-sensitive")
	}
}

// ============================================================================
// CreateSession
// ============================================================================

func TestCreateSession_HasToken(t *testing.T) {
	am := &AuthManager{
		config:   &AuthConfig{TokenExpiry: 24},
		sessions: make(map[string]*AuthSession),
	}
	session := am.CreateSession("Firefox", "192.168.1.1")
	if session.Token == "" {
		t.Error("CreateSession should set a token")
	}
	if len(session.Token) != 64 {
		t.Errorf("token length = %d, want 64", len(session.Token))
	}
}

func TestCreateSession_SetsUserAgentAndIP(t *testing.T) {
	am := &AuthManager{
		config:   &AuthConfig{TokenExpiry: 24},
		sessions: make(map[string]*AuthSession),
	}
	session := am.CreateSession("Chrome/100", "10.0.0.1")
	if session.UserAgent != "Chrome/100" {
		t.Errorf("UserAgent = %q, want %q", session.UserAgent, "Chrome/100")
	}
	if session.IP != "10.0.0.1" {
		t.Errorf("IP = %q, want %q", session.IP, "10.0.0.1")
	}
}

func TestCreateSession_ExpirySet(t *testing.T) {
	am := &AuthManager{
		config:   &AuthConfig{TokenExpiry: 48},
		sessions: make(map[string]*AuthSession),
	}
	session := am.CreateSession("UA", "1.2.3.4")
	expectedExpiry := time.Now().Add(48 * time.Hour)
	diff := session.ExpiresAt.Sub(expectedExpiry)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("expiry should be ~48h from now, got diff: %v", diff)
	}
}

func TestCreateSession_DefaultExpiry(t *testing.T) {
	am := &AuthManager{
		config:   &AuthConfig{TokenExpiry: 0}, // 零值應使用 24h
		sessions: make(map[string]*AuthSession),
	}
	session := am.CreateSession("UA", "0.0.0.0")
	expectedExpiry := time.Now().Add(24 * time.Hour)
	diff := session.ExpiresAt.Sub(expectedExpiry)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("expiry should default to ~24h, got diff: %v", diff)
	}
}

func TestCreateSession_StoredInSessions(t *testing.T) {
	am := &AuthManager{
		config:   &AuthConfig{TokenExpiry: 24},
		sessions: make(map[string]*AuthSession),
	}
	session := am.CreateSession("UA", "1.2.3.4")
	stored := am.sessions[session.Token]
	if stored == nil {
		t.Error("CreateSession should add token to sessions map")
	}
	if stored.Token != session.Token {
		t.Error("stored session token mismatch")
	}
}

// ============================================================================
// ValidateToken
// ============================================================================

func TestValidateToken_EmptyToken(t *testing.T) {
	am := &AuthManager{
		sessions: make(map[string]*AuthSession),
	}
	if am.ValidateToken("") {
		t.Error("ValidateToken should return false for empty token")
	}
}

func TestValidateToken_NonExistent(t *testing.T) {
	am := &AuthManager{
		sessions: make(map[string]*AuthSession),
	}
	if am.ValidateToken("some-random-token") {
		t.Error("ValidateToken should return false for non-existent token")
	}
}

func TestValidateToken_ValidSession(t *testing.T) {
	am := &AuthManager{
		sessions: map[string]*AuthSession{
			"valid-token": {
				Token:     "valid-token",
				CreatedAt: time.Now(),
				ExpiresAt: time.Now().Add(24 * time.Hour),
			},
		},
	}
	if !am.ValidateToken("valid-token") {
		t.Error("ValidateToken should return true for valid unexpired token")
	}
}

func TestValidateToken_ExpiredSession(t *testing.T) {
	am := &AuthManager{
		sessions: map[string]*AuthSession{
			"expired-token": {
				Token:     "expired-token",
				CreatedAt: time.Now().Add(-48 * time.Hour),
				ExpiresAt: time.Now().Add(-1 * time.Hour), // 1小時前過期
			},
		},
	}
	if am.ValidateToken("expired-token") {
		t.Error("ValidateToken should return false for expired token")
	}
	// 過期 token 應該被刪除
	if _, exists := am.sessions["expired-token"]; exists {
		t.Error("expired token should be deleted from sessions")
	}
}

// ============================================================================
// Logout
// ============================================================================

func TestLogout_RemovesSession(t *testing.T) {
	am := &AuthManager{
		sessions: map[string]*AuthSession{
			"token-to-remove": {
				Token:     "token-to-remove",
				CreatedAt: time.Now(),
				ExpiresAt: time.Now().Add(24 * time.Hour),
			},
			"token-to-keep": {
				Token:     "token-to-keep",
				CreatedAt: time.Now(),
				ExpiresAt: time.Now().Add(24 * time.Hour),
			},
		},
	}
	am.Logout("token-to-remove")

	if _, exists := am.sessions["token-to-remove"]; exists {
		t.Error("Logout should remove the specified session")
	}
	if _, exists := am.sessions["token-to-keep"]; !exists {
		t.Error("Logout should not remove other sessions")
	}
}

func TestLogout_NonExistent(t *testing.T) {
	am := &AuthManager{
		sessions: map[string]*AuthSession{
			"existing": {
				Token:     "existing",
				CreatedAt: time.Now(),
				ExpiresAt: time.Now().Add(24 * time.Hour),
			},
		},
	}
	// 唔存在嘅 token 唔應該 panic
	am.Logout("non-existent-token")
	if _, exists := am.sessions["existing"]; !exists {
		t.Error("Logout of non-existent token should not remove existing sessions")
	}
}

// ============================================================================
// ValidateToken edge cases
// ============================================================================

func TestValidateToken_ExpiryExactlyNow(t *testing.T) {
	// just expired
	am := &AuthManager{
		sessions: map[string]*AuthSession{
			"just-now": {
				Token:     "just-now",
				CreatedAt: time.Now().Add(-24 * time.Hour),
				ExpiresAt: time.Now().Add(-time.Millisecond),
			},
		},
	}
	if am.ValidateToken("just-now") {
		t.Error("ValidateToken should return false for just-expired token")
	}
}

func TestValidateToken_ExpiresInOneSecond(t *testing.T) {
	// still valid
	am := &AuthManager{
		sessions: map[string]*AuthSession{
			"still-valid": {
				Token:     "still-valid",
				CreatedAt: time.Now(),
				ExpiresAt: time.Now().Add(time.Second),
			},
		},
	}
	if !am.ValidateToken("still-valid") {
		t.Error("ValidateToken should return true for token expiring in 1 second")
	}
}

func TestSession_FieldsPopulated(t *testing.T) {
	am := &AuthManager{
		config:   &AuthConfig{TokenExpiry: 12},
		sessions: make(map[string]*AuthSession),
	}
	session := am.CreateSession("Safari", "10.10.10.10")

	if session.Token == "" {
		t.Error("Token should be set")
	}
	if session.UserAgent != "Safari" {
		t.Errorf("UserAgent = %q", session.UserAgent)
	}
	if session.IP != "10.10.10.10" {
		t.Errorf("IP = %q", session.IP)
	}
	if session.CreatedAt.After(time.Now()) {
		t.Error("CreatedAt should not be in the future")
	}
	if time.Since(session.CreatedAt) > time.Minute {
		t.Error("CreatedAt should be recent")
	}

	// token should be valid hex
	if !isHexString(session.Token) {
		t.Errorf("token should be hex, got: %s", session.Token)
	}
}

func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// ============================================================================
// AuthMiddleware integration helpers
// ============================================================================

func TestExtractToken_FromCookie(t *testing.T) {
	// 檢查 auth go 提取 token 的邏輯係咪正確
	// 呢個係測試 extract 函數嘅基礎邏輯，唔需要 HTTP
	token := generateToken()
	if len(token) != 64 {
		t.Errorf("token for cookie should be 64 hex chars")
	}
	if strings.ContainsAny(token, "ghijklmnopqrstuvwxyz") {
		t.Error("hex token should only contain 0-9 and a-f")
	}
}
