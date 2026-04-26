package main

import (
	"testing"
	"time"
)

// TestRateLimiter 测试速率限制器
func TestRateLimiter(t *testing.T) {
	// 创建一个速率限制器：1秒内最多2条消息
	limiter := NewRateLimiter(2, 1*time.Second)
	userID := "testuser"

	// 第一条消息应该允许
	if !limiter.Allow(userID) {
		t.Error("First message should be allowed")
	}

	// 第二条消息应该允许
	if !limiter.Allow(userID) {
		t.Error("Second message should be allowed")
	}

	// 第三条消息应该被拒绝
	if limiter.Allow(userID) {
		t.Error("Third message should be rejected")
	}

	// 等待1秒后，应该可以再发送消息
	time.Sleep(1 * time.Second)
	if !limiter.Allow(userID) {
		t.Error("Message should be allowed after time window")
	}
}

// TestChannelError 测试频道错误结构
func TestChannelError(t *testing.T) {
	err := NewChannelError("TEST_ERROR", "Test error message", nil)
	if err.Error() != "[TEST_ERROR] Test error message" {
		t.Errorf("Expected error message '[TEST_ERROR] Test error message', got '%s'", err.Error())
	}

	// 测试带原始错误的情况
	originalErr := &ChannelError{Code: "ORIGINAL", Message: "Original error"}
	errWithOriginal := NewChannelError("WRAPPED", "Wrapped error", originalErr)
	if errWithOriginal.Error() != "[WRAPPED] Wrapped error: [ORIGINAL] Original error" {
		t.Errorf("Expected wrapped error message, got '%s'", errWithOriginal.Error())
	}
}

// TestBaseChannel 测试基础频道功能
func TestBaseChannel(t *testing.T) {
	bc := NewBaseChannel("test-channel")

	// 测试健康检查
	health := bc.HealthCheck()
	if health["id"] != "test-channel" {
		t.Errorf("Expected health check id 'test-channel', got '%v'", health["id"])
	}

	// 测试速率限制配置
	bc.SetRateLimitConfig(5, 30*time.Second)
	if !bc.CheckRateLimit("testuser") {
		t.Error("Rate limit should allow first message")
	}

	// 测试重试配置
	bc.SetRetryConfig(5, 500*time.Millisecond)

	// 测试错误创建
	err := bc.NewError("TEST", "Test error", nil)
	if err.Code != "TEST" {
		t.Errorf("Expected error code 'TEST', got '%s'", err.Code)
	}
}

// TestRetry 测试重试机制
func TestRetry(t *testing.T) {
	bc := NewBaseChannel("test-channel")
	bc.SetRetryConfig(3, 10*time.Millisecond)

	// 测试成功的情况
	successCount := 0
	err := bc.Retry(func() error {
		successCount++
		return nil
	})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if successCount != 1 {
		t.Errorf("Expected 1 attempt, got %d", successCount)
	}

	// 测试失败的情况（超过最大重试次数）
	attemptCount := 0
	maxRetries := 3
	err = bc.Retry(func() error {
		attemptCount++
		return NewChannelError("TEMP_ERROR", "Temporary error", nil)
	})
	if err == nil {
		t.Error("Expected error after max retries")
	}
	if attemptCount != maxRetries+1 {
		t.Errorf("Expected %d attempts, got %d", maxRetries+1, attemptCount)
	}
}
