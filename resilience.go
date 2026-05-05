package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"time"
)

// resilientDo 執行帶有全面韌性策略的 HTTP 請求。
//
// 功能：
//   - 自動超時放寬（根據連續超時次數動態增加 ResponseHeaderTimeout）
//   - Provider 故障轉移（連續失敗後切換到下一個可用 provider）
//   - 無限重試 + 指數退避 + 隨機抖動（當無 failover 可用時）
//   - 尊重 context 取消（用戶可以隨時停止）
//
// 參數：
//   - data: 請求體數據（會序列化為 JSON）
//   - baseURL: API 基地址
//   - apiPath: API 路徑（如 /chat/completions）
//   - apiKey: API 密鑰
//   - apiType: API 類型（openai/anthropic/ollama）
//   - resilience: 韌性配置
//   - initialProviderName: 初始 provider 名稱（用於 failover 報告）
func resilientDo(
	ctx context.Context,
	reqBody []byte,
	baseURL, apiPath, apiKey, apiType string,
	resilience *ResilienceConfig,
	initialProviderName string,
) (*http.Response, error) {

	currentBaseURL := baseURL
	currentAPIKey := apiKey
	currentProviderName := initialProviderName
	currentTimeout := 60 * time.Second // 初始 ResponseHeaderTimeout

	consecutiveTimeouts := 0
	attempt := 0
	currentBackoff := time.Duration(resilience.InitialBackoffSeconds) * time.Second

	for {
		// 檢查 context 是否已取消
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// 構建 endpoint
		endpoint := resolveEndpoint(currentBaseURL, apiPath)

		// 為此次嘗試創建 HTTP client（支持動態 ResponseHeaderTimeout）
		client := buildResilientHTTPClient(currentTimeout)

		// 發送請求
		t0 := time.Now()
		resp, err := sendResilientRequest(ctx, client, reqBody, endpoint, currentAPIKey, apiType)

		if err == nil {
			// 成功：報告到 Provider Failover
			if globalProviderFailover != nil {
				globalProviderFailover.ReportSuccess(currentProviderName)
			}
			log.Printf("[Resilience] Request succeeded after %d attempt(s), TTFB=%v", attempt, time.Since(t0))
			return resp, nil
		}

		// ── 錯誤處理 ──

		// 1. 錯誤分類
		var classified *ClassifiedError
		if globalErrorClassifier != nil {
			classified = globalErrorClassifier.Classify(err)
			if classified != nil {
				log.Printf("[Resilience] Attempt %d failed: %s (type: %s, retryable: %v)",
					attempt+1, err.Error(), ErrorTypeString(classified.Type), classified.Retryable)
			} else {
				log.Printf("[Resilience] Attempt %d failed: %s (unclassified)", attempt+1, err.Error())
			}
		} else {
			log.Printf("[Resilience] Attempt %d failed: %s", attempt+1, err.Error())
		}

		// 2. 報告失敗到 Provider Failover
		if globalProviderFailover != nil {
			globalProviderFailover.ReportFailure(currentProviderName, err)
		}

		// 3. 檢查是否為 fatal error（不可重試）
		if classified != nil && classified.IsFatal {
			return nil, fmt.Errorf("fatal error (not retryable): %w", err)
		}

		// 4. 超時自動放寬
		if resilience.EnableTimeoutScaling && isTimeoutError(err, classified) {
			consecutiveTimeouts++
			newTimeout := time.Duration(float64(currentTimeout) * resilience.TimeoutScaleFactor)
			maxTimeout := time.Duration(resilience.MaxTimeoutSeconds) * time.Second
			if newTimeout > maxTimeout {
				newTimeout = maxTimeout
			}
			if newTimeout > currentTimeout {
				log.Printf("[Resilience] Timeout scaled: %v → %v (consecutive timeouts: %d)",
					currentTimeout, newTimeout, consecutiveTimeouts)
				currentTimeout = newTimeout
			}
		} else {
			consecutiveTimeouts = 0
		}

		// 5. Provider 故障轉移
		if resilience.EnableFailover && globalProviderFailover != nil && globalProviderFailover.ProviderCount() > 1 {
			if active, err := globalProviderFailover.GetActiveProvider(); err == nil && active != nil {
				if active.Name != currentProviderName {
					log.Printf("[Resilience] Failing over: %s → %s (BaseURL: %s)",
						currentProviderName, active.Name, active.BaseURL)
					currentBaseURL = active.BaseURL
					currentAPIKey = active.APIKey
					currentProviderName = active.Name
					// 切換 provider 後重置超時、退避和重試計數
					currentTimeout = 60 * time.Second
					consecutiveTimeouts = 0
					attempt = 0
					currentBackoff = time.Duration(resilience.InitialBackoffSeconds) * time.Second
					continue // 立即用新 provider 重試
				}
			}
		}

		// 6. 檢查重試次數上限（MaxRetries > 0 時有上限；MaxRetries == 0 時無限重試）
		if resilience.MaxRetries > 0 && attempt >= resilience.MaxRetries {
			return nil, fmt.Errorf("max retries (%d) exceeded after %d attempts: %w",
				resilience.MaxRetries, attempt+1, err)
		}

		// 7. 計算退避延遲
		waitDuration := computeBackoff(classified, currentBackoff, resilience)

		// 為下一次重試增加 backoff（指數增長）
		nextBackoff := time.Duration(float64(currentBackoff) * resilience.BackoffMultiplier)
		maxBackoff := time.Duration(resilience.MaxBackoffSeconds) * time.Second
		if nextBackoff > maxBackoff {
			nextBackoff = maxBackoff
		}
		currentBackoff = nextBackoff

		log.Printf("[Resilience] Retrying in %v (attempt %d, next backoff %v)...",
			waitDuration.Round(time.Second), attempt+1, currentBackoff.Round(time.Second))

		// 8. 等待（尊重 context 取消）
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(waitDuration):
		}

		attempt++
	}
}

// buildResilientHTTPClient 創建帶有動態 ResponseHeaderTimeout 的 HTTP client。
// 複製全局 httpClient 的 TLS/Dial 設定，但使用自定義的超時值。
func buildResilientHTTPClient(responseHeaderTimeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: 0, // 無整體超時，由 Context 和 ResponseHeaderTimeout 控制
		Transport: &http.Transport{
			// 禁用 HTTP/2：SSE 兼容性
			TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),

			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: responseHeaderTimeout,
		},
	}
}

// sendResilientRequest 發送 HTTP 請求並返回 response（與 sendRequest 類似，但使用自定義 client）
func sendResilientRequest(ctx context.Context, client *http.Client, reqBody []byte, endpoint, apiKey, apiType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if apiType == "anthropic" && globalPromptCacheConfig.Enabled {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	if apiKey != "" {
		if apiType == "openai" || apiType == "ollama" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		} else if apiType == "anthropic" {
			req.Header.Set("x-api-key", apiKey)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errorBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		errorBodyStr := string(errorBody)
		// 檢測上下文長度錯誤
		if isContextLengthError(errorBodyStr) {
			return nil, fmt.Errorf("context_length_exceeded: %s", errorBodyStr)
		}
		return nil, fmt.Errorf("API returned error status: %d, body: %s", resp.StatusCode, errorBodyStr)
	}

	return resp, nil
}

// isTimeoutError 判斷錯誤是否為超時類型
func isTimeoutError(err error, classified *ClassifiedError) bool {
	if classified != nil && classified.Type == ErrorTimeout {
		return true
	}
	// 檢查原始錯誤是否為 net.Error 且超時
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return true
	}
	return false
}

// computeBackoff 計算退避延遲（指數退避 + 隨機抖動）
func computeBackoff(classified *ClassifiedError, currentBackoff time.Duration, resilience *ResilienceConfig) time.Duration {
	baseDelay := currentBackoff

	// 如果有已分類的錯誤，使用分類器的基礎延遲作為最小值
	if classified != nil {
		var classifiedBase time.Duration
		switch classified.Type {
		case ErrorRateLimit:
			classifiedBase = 1 * time.Second
		case ErrorServerError:
			classifiedBase = 2 * time.Second
		case ErrorTimeout:
			classifiedBase = 5 * time.Second
		case ErrorConnection:
			classifiedBase = 3 * time.Second
		default:
			classifiedBase = 2 * time.Second
		}
		if classifiedBase > baseDelay {
			baseDelay = classifiedBase
		}
		// 速率限制：優先使用 Retry-After
		if classified.RetryAfter > 0 && classified.Type == ErrorRateLimit {
			baseDelay = classified.RetryAfter
		}
	}

	// 添加隨機抖動（±25%）
	jitterRange := float64(baseDelay) * 0.25
	jitter := time.Duration(jitterRange * (rand.Float64()*2 - 1))
	waitDuration := baseDelay + jitter

	// 限制最大退避時間
	maxBackoff := time.Duration(resilience.MaxBackoffSeconds) * time.Second
	if waitDuration > maxBackoff {
		waitDuration = maxBackoff
	}

	// 確保至少為 0
	if waitDuration < 0 {
		waitDuration = 0
	}

	return waitDuration
}
