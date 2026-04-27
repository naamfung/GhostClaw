package main

import (
        "context"
        "errors"
        "fmt"
        "math/rand"
        "net"
        "net/http"
        "net/url"
        "strings"
        "sync"
        "time"
)

// ErrorType 表示 API 错误的分类类型
// 用于区分不同类型的错误，以支持差异化的重试策略和错误处理
type ErrorType int

const (
        ErrorUnknown       ErrorType = iota // 未知/未分类错误
        ErrorRateLimit                       // 429 请求频率超限
        ErrorAuthentication                  // 401/403 认证/授权失败
        ErrorContextLength                   // 上下文超出模型长度限制
        ErrorInvalidRequest                  // 400 请求格式错误（参数不合法）
        ErrorServerError                     // 500-599 服务端内部错误
        ErrorTimeout                         // 请求超时
        ErrorConnection                      // 网络/连接错误
        ErrorModelNotFound                   // 模型不存在/不支持
        ErrorContentFilter                   // 触发内容审核/过滤
)

// ErrorTypeString 返回错误类型的人类可读名称（中文）
func ErrorTypeString(t ErrorType) string {
        switch t {
        case ErrorUnknown:
                return "未知错误"
        case ErrorRateLimit:
                return "速率限制"
        case ErrorAuthentication:
                return "认证失败"
        case ErrorContextLength:
                return "上下文过长"
        case ErrorInvalidRequest:
                return "请求无效"
        case ErrorServerError:
                return "服务端错误"
        case ErrorTimeout:
                return "请求超时"
        case ErrorConnection:
                return "连接错误"
        case ErrorModelNotFound:
                return "模型未找到"
        case ErrorContentFilter:
                return "内容过滤"
        default:
                return "未知类型"
        }
}

// ClassifiedError 表示经过分类的结构化错误信息
// 包含错误类型、HTTP 状态码、原始消息以及重试策略
type ClassifiedError struct {
        Type       ErrorType     // 错误分类
        StatusCode int           // HTTP 状态码（网络错误时为 0）
        Message    string        // 错误描述信息
        Retryable  bool          // 是否可重试
        RetryAfter time.Duration // 建议重试等待时间（主要用于速率限制）
        IsFatal    bool          // 是否为致命错误（应终止 Agent 循环）
}

// Error 实现 error 接口
func (e *ClassifiedError) Error() string {
        return fmt.Sprintf("[%s] status=%d msg=%s retryable=%v fatal=%v",
                ErrorTypeString(e.Type), e.StatusCode, e.Message, e.Retryable, e.IsFatal)
}

// ErrorClassifier 错误分类器
// 负责将原始错误分类为结构化的 ClassifiedError，并提供重试策略
// 所有方法均为并发安全
type ErrorClassifier struct {
        mu          sync.RWMutex                     // 读写锁，保护 errorCounts
        errorCounts map[ErrorType]int                // 各类型错误计数器（用于监控）
        attemptMap  map[string]int                   // 各错误标识的重试次数追踪（用于指数退避）
}

// NewErrorClassifier 创建新的错误分类器实例
func NewErrorClassifier() *ErrorClassifier {
        return &ErrorClassifier{
                errorCounts: make(map[ErrorType]int),
                attemptMap:  make(map[string]int),
        }
}

// globalErrorClassifier 全局错误分类器实例
var globalErrorClassifier *ErrorClassifier

func init() {
        globalErrorClassifier = NewErrorClassifier()
}

// GetErrorClassifier 返回全局错误分类器实例
func GetErrorClassifier() *ErrorClassifier {
        return globalErrorClassifier
}

// ClassifyError 便捷函数：使用全局分类器对错误进行分类
func ClassifyError(err error) *ClassifiedError {
        return globalErrorClassifier.Classify(err)
}

// Classify 对任意 error 进行分类
// 支持 net.Error、context.DeadlineExceeded、以及自定义的错误格式
// 如 "API returned error status: 429, body: ..." 格式的错误
func (ec *ErrorClassifier) Classify(err error) *ClassifiedError {
        if err == nil {
                return nil
        }

        errMsg := err.Error()

        // 1. 检查 context 超时
        if errors.Is(err, context.DeadlineExceeded) {
                ec.recordError(ErrorTimeout)
                return &ClassifiedError{
                        Type:      ErrorTimeout,
                        Message:   errMsg,
                        Retryable: true,
                        IsFatal:   false,
                }
        }
        if errors.Is(err, context.Canceled) {
                // 主动取消不算错误，不可重试
                return &ClassifiedError{
                        Type:      ErrorUnknown,
                        Message:   errMsg,
                        Retryable: false,
                        IsFatal:   false,
                }
        }

        // 2. 检查 net.Error（网络超时、连接拒绝、DNS 解析失败等）
        var netErr net.Error
        if errors.As(err, &netErr) {
                if netErr.Timeout() {
                        ec.recordError(ErrorTimeout)
                        return &ClassifiedError{
                                Type:      ErrorTimeout,
                                Message:   errMsg,
                                Retryable: true,
                                IsFatal:   false,
                        }
                }
                // 连接被拒绝或 DNS 解析失败等
                ec.recordError(ErrorConnection)
                return &ClassifiedError{
                        Type:      ErrorConnection,
                        Message:   errMsg,
                        Retryable: true,
                        IsFatal:   false,
                }
        }

        // 3. 检查 *url.Error（HTTP 请求产生的包装错误）
        var urlErr *url.Error
        if errors.As(err, &urlErr) {
                // 递归解包内部错误
                return ec.Classify(urlErr.Err)
        }

        // 4. 尝试从错误消息中提取 HTTP 状态码和 body
        // 兼容 CallModel.go 中 "API returned error status: %d, body: %s" 的格式
        statusCode, body := extractStatusAndBody(errMsg)
        if statusCode > 0 {
                classified := ec.ClassifyHTTPError(statusCode, body)
                return classified
        }

        // 5. 尝试通过错误消息内容进行模式匹配
        errorType, extractedMsg := classifyByBody(errMsg)
        if errorType != ErrorUnknown {
                ec.recordError(errorType)
                return ec.buildClassifiedError(errorType, 0, extractedMsg)
        }

        // 6. 未识别的错误，归为 Unknown
        ec.recordError(ErrorUnknown)
        return &ClassifiedError{
                Type:      ErrorUnknown,
                StatusCode: 0,
                Message:   errMsg,
                Retryable: false,
                IsFatal:   false,
        }
}

// ClassifyHTTPError 根据 HTTP 状态码和响应体对错误进行分类
// 这是最常用的分类入口，处理 API 返回的非 2xx 响应
func (ec *ErrorClassifier) ClassifyHTTPError(statusCode int, body string) *ClassifiedError {
        var errorType ErrorType
        var message string

        switch {
        case statusCode == 429:
                errorType = ErrorRateLimit
                message = "请求频率超限 (HTTP 429)"

        case statusCode == 401 || statusCode == 403:
                errorType = ErrorAuthentication
                message = fmt.Sprintf("认证/授权失败 (HTTP %d)", statusCode)

        case statusCode == 404:
                // 404 可能是模型不存在，也可能是其他资源不存在
                // 优先通过响应体内容判断
                if bodyType, bodyMsg := classifyByBody(body); bodyType == ErrorModelNotFound {
                        errorType = ErrorModelNotFound
                        message = bodyMsg
                } else {
                        errorType = ErrorInvalidRequest
                        message = fmt.Sprintf("资源未找到 (HTTP 404): %s", truncateString(body, 200))
                }

        case statusCode == 400:
                errorType = ErrorInvalidRequest
                message = fmt.Sprintf("请求格式错误 (HTTP 400): %s", truncateString(body, 200))

        case statusCode >= 500 && statusCode < 600:
                errorType = ErrorServerError
                message = fmt.Sprintf("服务端错误 (HTTP %d): %s", statusCode, truncateString(body, 200))

        case statusCode == 422:
                // 422 Unprocessable Entity，通常是内容审核或请求格式问题
                if bodyType, bodyMsg := classifyByBody(body); bodyType != ErrorUnknown {
                        errorType = bodyType
                        message = bodyMsg
                } else {
                        errorType = ErrorInvalidRequest
                        message = fmt.Sprintf("无法处理的请求 (HTTP 422): %s", truncateString(body, 200))
                }

        default:
                // 其他非 2xx 状态码
                if bodyType, bodyMsg := classifyByBody(body); bodyType != ErrorUnknown {
                        errorType = bodyType
                        message = bodyMsg
                } else {
                        errorType = ErrorUnknown
                        message = fmt.Sprintf("未知 HTTP 错误 (HTTP %d): %s", statusCode, truncateString(body, 200))
                }
        }

        // 优先使用响应体模式匹配的结果覆盖状态码推断的结果
        // 例如：某些 API 返回 400 但实际是上下文长度问题
        if bodyType, bodyMsg := classifyByBody(body); bodyType != ErrorUnknown {
                // 上下文长度错误的优先级最高
                if bodyType == ErrorContextLength {
                        errorType = ErrorContextLength
                        message = bodyMsg
                }
                // 内容过滤错误
                if bodyType == ErrorContentFilter {
                        errorType = ErrorContentFilter
                        message = bodyMsg
                }
                // 速率限制错误（某些 API 可能不返回 429）
                if bodyType == ErrorRateLimit && errorType != ErrorRateLimit {
                        errorType = ErrorRateLimit
                        message = bodyMsg
                }
        }

        ec.recordError(errorType)
        return ec.buildClassifiedError(errorType, statusCode, message)
}

// buildClassifiedError 根据错误类型构建 ClassifiedError
// 设置 Retryable、IsFatal 等策略属性
func (ec *ErrorClassifier) buildClassifiedError(errorType ErrorType, statusCode int, message string) *ClassifiedError {
        result := &ClassifiedError{
                Type:       errorType,
                StatusCode: statusCode,
                Message:    message,
        }

        switch errorType {
        case ErrorRateLimit:
                // 速率限制错误：可重试，尝试解析 Retry-After
                result.Retryable = true
                result.IsFatal = false
                result.RetryAfter = ec.GetRetryDelay(errors.New(message))

        case ErrorAuthentication:
                // 认证错误：不可重试（除非有密钥轮换机制），标记为致命
                result.Retryable = false
                result.IsFatal = true

        case ErrorContextLength:
                // 上下文长度错误：通常不可直接重试，但可通过上下文压缩后恢复
                // 这里标记为不可重试，上层逻辑可选择压缩后重试
                result.Retryable = false
                result.IsFatal = false

        case ErrorInvalidRequest:
                // 请求格式错误：不可重试，需要修正请求
                result.Retryable = false
                result.IsFatal = false

        case ErrorServerError:
                // 服务端错误：可重试
                result.Retryable = true
                result.IsFatal = false
                result.RetryAfter = ec.GetRetryDelay(errors.New(message))

        case ErrorTimeout:
                // 超时错误：可重试
                result.Retryable = true
                result.IsFatal = false
                result.RetryAfter = ec.GetRetryDelay(errors.New(message))

        case ErrorConnection:
                // 连接错误：可重试
                result.Retryable = true
                result.IsFatal = false
                result.RetryAfter = ec.GetRetryDelay(errors.New(message))

        case ErrorModelNotFound:
                // 模型不存在：不可重试，标记为致命
                result.Retryable = false
                result.IsFatal = true

        case ErrorContentFilter:
                // 内容过滤：不可重试（需要修改输入内容）
                result.Retryable = false
                result.IsFatal = false

        default:
                // 未知错误：保守策略，不可重试
                result.Retryable = false
                result.IsFatal = false
        }

        return result
}

// ShouldRetry 判断错误是否可重试
// 是 Classify + Retryable 的便捷封装
func (ec *ErrorClassifier) ShouldRetry(err error) bool {
        classified := ec.Classify(err)
        if classified == nil {
                return false
        }
        return classified.Retryable
}

// GetRetryDelay 获取建议的重试延迟时间
// 基于错误类型的基础延迟，加上抖动（jittered exponential backoff）
// 对于速率限制错误，优先使用 Retry-After 头的值
// 注意：此方法會調用 Classify 進行錯誤計數；若已有已分類的錯誤，請使用 GetRetryDelayForClassified 避免重複計數
func (ec *ErrorClassifier) GetRetryDelay(err error) time.Duration {
        classified := ec.Classify(err)
        if classified == nil {
                return 0
        }

        return ec.GetRetryDelayForClassified(classified)
}

// GetRetryDelayForClassified 為已分類的錯誤計算重試延遲（避免雙重計數）
func (ec *ErrorClassifier) GetRetryDelayForClassified(classified *ClassifiedError) time.Duration {
        if classified == nil {
                return 0
        }

        // 如果已经设置了 RetryAfter（例如从 HTTP 429 的 Retry-After 头解析），直接使用
        if classified.RetryAfter > 0 && classified.Type == ErrorRateLimit {
                return classified.RetryAfter
        }

        // 获取当前重试次数
        errKey := fmt.Sprintf("%s:%d", classified.Message, classified.Type)
        ec.mu.Lock()
        attempt := ec.attemptMap[errKey]
        ec.attemptMap[errKey] = attempt + 1
        ec.mu.Unlock()

        // 根据错误类型确定基础延迟
        var baseDelay time.Duration
        switch classified.Type {
        case ErrorRateLimit:
                baseDelay = 1 * time.Second
        case ErrorServerError:
                baseDelay = 2 * time.Second
        case ErrorTimeout:
                baseDelay = 5 * time.Second
        case ErrorConnection:
                baseDelay = 3 * time.Second
        default:
                baseDelay = 2 * time.Second
        }

        // 指数退避：base * 2^attempt，最多 5 次重试
        const maxAttempts = 5
        if attempt >= maxAttempts {
                attempt = maxAttempts
        }
        delay := baseDelay * time.Duration(1<<uint(attempt))

        // 添加随机抖动：random(0, base_delay * 0.5)
        // 防止多个并发请求同时重试导致惊群效应
        jitterMax := baseDelay / 2
        if jitterMax > 0 {
                jitter := time.Duration(rand.Int63n(int64(jitterMax)))
                delay += jitter
        }

        return delay
}

// GetRetryDelayWithAttempt 获取指定重试次数的延迟时间
// attempt 从 0 开始（第一次重试）
func (ec *ErrorClassifier) GetRetryDelayWithAttempt(classified *ClassifiedError, attempt int) time.Duration {
        if classified == nil {
                return 0
        }

        // 如果已经设置了 RetryAfter（例如从 HTTP 429 的 Retry-After 头解析），直接使用
        if classified.RetryAfter > 0 && classified.Type == ErrorRateLimit {
                return classified.RetryAfter
        }

        // 根据错误类型确定基础延迟
        var baseDelay time.Duration
        switch classified.Type {
        case ErrorRateLimit:
                baseDelay = 1 * time.Second
        case ErrorServerError:
                baseDelay = 2 * time.Second
        case ErrorTimeout:
                baseDelay = 5 * time.Second
        case ErrorConnection:
                baseDelay = 3 * time.Second
        default:
                baseDelay = 2 * time.Second
        }

        // 指数退避：base * 2^attempt，最多 5 次重试
        const maxAttempts = 5
        if attempt >= maxAttempts {
                attempt = maxAttempts
        }
        delay := baseDelay * time.Duration(1<<uint(attempt))

        // 添加随机抖动：random(0, base_delay * 0.5)
        jitterMax := baseDelay / 2
        if jitterMax > 0 {
                jitter := time.Duration(rand.Int63n(int64(jitterMax)))
                delay += jitter
        }

        return delay
}

// GetErrorCounts 返回各类型错误的计数快照
// 用于监控和日志记录
func (ec *ErrorClassifier) GetErrorCounts() map[ErrorType]int {
        ec.mu.RLock()
        defer ec.mu.RUnlock()

        // 返回副本，避免外部修改影响内部状态
        counts := make(map[ErrorType]int, len(ec.errorCounts))
        for k, v := range ec.errorCounts {
                counts[k] = v
        }
        return counts
}

// ResetErrorCounts 重置所有错误计数器
func (ec *ErrorClassifier) ResetErrorCounts() {
        ec.mu.Lock()
        defer ec.mu.Unlock()
        ec.errorCounts = make(map[ErrorType]int)
        ec.attemptMap = make(map[string]int)
}

// recordError 记录一次错误（内部方法，必须持有写锁或自行加锁）
func (ec *ErrorClassifier) recordError(errorType ErrorType) {
        ec.mu.Lock()
        defer ec.mu.Unlock()
        ec.errorCounts[errorType]++
}

// extractStatusAndBody 从错误消息中提取 HTTP 状态码和响应体
// 兼容格式：
//   - "API returned error status: 429, body: ..."
//   - "context_length_exceeded: ..."
func extractStatusAndBody(errMsg string) (statusCode int, body string) {
        // 尝试匹配 "status: %d, body: ..." 格式（CallModel.go 的格式）
        if idx := strings.Index(errMsg, "status: "); idx != -1 {
                remaining := errMsg[idx+len("status: "):]
                // 提取状态码数字
                var codeStr string
                for i, ch := range remaining {
                        if ch >= '0' && ch <= '9' {
                                codeStr += string(ch)
                        } else {
                                if i > 0 {
                                        break
                                }
                        }
                }
                if codeStr != "" {
                        fmt.Sscanf(codeStr, "%d", &statusCode)
                        // 提取 body
                        if bodyIdx := strings.Index(remaining, "body: "); bodyIdx != -1 {
                                body = remaining[bodyIdx+len("body: "):]
                        }
                        return
                }
        }

        // 尝试匹配 "context_length_exceeded: ..." 格式
        if strings.Contains(errMsg, "context_length_exceeded") {
                return 0, errMsg
        }

        return 0, ""
}

// classifyByBody 根据错误响应体内容进行模式匹配分类
// 检查已知的错误模式，返回匹配到的错误类型和描述
// 注意：此函数包含独立的上下文长度检测逻辑，与 CallModel.go 中的 isContextLengthError 互补
// 不调用 isContextLengthError（避免跨文件重复定义），而是使用自己的关键词匹配
func classifyByBody(body string) (ErrorType, string) {
        if body == "" {
                return ErrorUnknown, ""
        }

        lowerBody := strings.ToLower(body)

        // ---- 内容过滤/审核 ----
        contentFilterPatterns := []string{
                "content_filter",
                "content moderation",
                "moderation",
                "blocked",
                "flagged content",
                "content policy",
                "safety",
                "nsfw",
                "harmful content",
        }
        for _, pattern := range contentFilterPatterns {
                if strings.Contains(lowerBody, pattern) {
                        return ErrorContentFilter, fmt.Sprintf("内容过滤触发: %s", truncateString(body, 200))
                }
        }

        // ---- 模型不存在 ----
        modelNotFoundPatterns := []string{
                "model_not_found",
                "does not exist",
                "invalid model",
                "model not found",
                "model not available",
                "model_not_available",
                "no such model",
                "unknown model",
                "model id",
        }
        for _, pattern := range modelNotFoundPatterns {
                if strings.Contains(lowerBody, pattern) {
                        return ErrorModelNotFound, fmt.Sprintf("模型未找到: %s", truncateString(body, 200))
                }
        }

        // ---- 速率限制 ----
        rateLimitPatterns := []string{
                "rate_limit",
                "rate limit",
                "too many requests",
                "quota",
                "throttl",
                "ratelimit",
                "request limit",
        }
        for _, pattern := range rateLimitPatterns {
                if strings.Contains(lowerBody, pattern) {
                        return ErrorRateLimit, fmt.Sprintf("速率限制: %s", truncateString(body, 200))
                }
        }

        // ---- 上下文长度 ----
        // 这里使用独立的检测逻辑，不调用 isContextLengthError（该函数在 CallModel.go 中定义）
        contextLengthPatterns := []string{
                "context_length",
                "max_tokens",
                "too long",
                "token limit",
                "maximum context",
                "context window",
                "exceeds the model",
                "input too large",
                "reduce the length",
        }
        for _, pattern := range contextLengthPatterns {
                if strings.Contains(lowerBody, pattern) {
                        return ErrorContextLength, fmt.Sprintf("上下文过长: %s", truncateString(body, 200))
                }
        }

        // ---- 认证错误 ----
        authPatterns := []string{
                "invalid api key",
                "incorrect api key",
                "api key expired",
                "unauthorized",
                "authentication",
                "authorization",
                "forbidden",
                "invalid token",
                "token expired",
        }
        for _, pattern := range authPatterns {
                if strings.Contains(lowerBody, pattern) {
                        return ErrorAuthentication, fmt.Sprintf("认证失败: %s", truncateString(body, 200))
                }
        }

        // ---- 服务端错误 ----
        serverErrorPatterns := []string{
                "internal server error",
                "service unavailable",
                "bad gateway",
                "gateway timeout",
                "overloaded",
                "capacity",
                "temporarily unavailable",
                "server error",
        }
        for _, pattern := range serverErrorPatterns {
                if strings.Contains(lowerBody, pattern) {
                        return ErrorServerError, fmt.Sprintf("服务端错误: %s", truncateString(body, 200))
                }
        }

        // ---- 连接错误 ----
        connectionPatterns := []string{
                "connection refused",
                "connection reset",
                "connection timed out",
                "no such host",
                "network error",
                "dns",
                "eof",
        }
        for _, pattern := range connectionPatterns {
                if strings.Contains(lowerBody, pattern) {
                        return ErrorConnection, fmt.Sprintf("连接错误: %s", truncateString(body, 200))
                }
        }

        // ---- 超时 ----
        timeoutPatterns := []string{
                "timeout",
                "timed out",
                "deadline exceeded",
                "request timeout",
        }
        for _, pattern := range timeoutPatterns {
                if strings.Contains(lowerBody, pattern) {
                        return ErrorTimeout, fmt.Sprintf("请求超时: %s", truncateString(body, 200))
                }
        }

        return ErrorUnknown, ""
}

// ParseRetryAfter 从 HTTP 响应头或响应体中解析 Retry-After 值
// 支持两种格式：
//   - 秒数（如 "30"）
//   - HTTP 日期格式（如 "Wed, 21 Oct 2015 07:28:00 GMT"）
func ParseRetryAfter(retryAfter string) time.Duration {
        if retryAfter == "" {
                return 0
        }

        // 尝试解析为秒数
        var seconds int
        if _, err := fmt.Sscanf(retryAfter, "%d", &seconds); err == nil && seconds > 0 {
                return time.Duration(seconds) * time.Second
        }

        // 尝试解析为 HTTP 日期格式
        if t, err := http.ParseTime(retryAfter); err == nil {
                delay := time.Until(t)
                if delay > 0 {
                        return delay
                }
        }

        return 0
}

// ClassifyHTTPResponse 从完整的 HTTP Response 中分类错误
// 自动解析 Retry-After 头
// 注意：调用者负责关闭 resp.Body
func (ec *ErrorClassifier) ClassifyHTTPResponse(resp *http.Response) *ClassifiedError {
        if resp == nil {
                ec.recordError(ErrorConnection)
                return &ClassifiedError{
                        Type:      ErrorConnection,
                        Message:   "收到空响应",
                        Retryable: true,
                        IsFatal:   false,
                }
        }

        classified := ec.ClassifyHTTPError(resp.StatusCode, "")

        // 尝试解析 Retry-After 头
        if resp.StatusCode == 429 {
                retryAfterStr := resp.Header.Get("Retry-After")
                if retryAfterStr != "" {
                        retryAfter := ParseRetryAfter(retryAfterStr)
                        if retryAfter > 0 {
                                classified.RetryAfter = retryAfter
                        }
                }
        }

        return classified
}
