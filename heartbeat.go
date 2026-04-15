package main

import (
        "context"
        "fmt"
        "log"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "time"
)

// HeartbeatService 心跳服务
// 定期检查 HEARTBEAT.md 中定义的任务，使用 LLM 判断是否需要执行
//
// # 启用前提
//
// 必要条件：
//   - config.Enabled = true（在配置中启用心跳服务）
//   - HEARTBEAT.md 文件存在（首次启动会自动创建）
//
// 可选条件：
//   - 有效的 LLM API 配置（没有会警告但不会阻止启动）
//   - 设置了通知器来发送结果（没有会只记录日志）
//
// # 配置示例
//
//      heartbeat:
//          enabled: true
//          interval_seconds: 1800    # 检查间隔（秒），默认 30 分钟
//          keep_recent_messages: 8   # 保留的最近消息数
//          max_concurrent_checks: 3  # 最大并发检查数
type HeartbeatService struct {
        config        HeartbeatConfig
        heartbeatFile string
        ticker        *time.Ticker
        ctx           context.Context
        cancel        context.CancelFunc
        wg            sync.WaitGroup
        notifier      HeartbeatNotifier // 通知器
        mu            sync.RWMutex
        lastCheck     time.Time
        running       bool
}

// HeartbeatNotifier 心跳通知器接口
// 实现此接口来处理心跳结果的通知
type HeartbeatNotifier interface {
        // Notify 发送通知
        Notify(task string, result string, shouldAlert bool) error
        // IsAvailable 检查通知器是否可用
        IsAvailable() bool
}

// 全局心跳通知器
var globalHeartbeatNotifier HeartbeatNotifier

// SetHeartbeatNotifier 设置全局心跳通知器
func SetHeartbeatNotifier(notifier HeartbeatNotifier) {
        globalHeartbeatNotifier = notifier
}

// NewHeartbeatService 创建心跳服务
func NewHeartbeatService(config HeartbeatConfig, workDir string) *HeartbeatService {
        ctx, cancel := context.WithCancel(context.Background())

        heartbeatFile := filepath.Join(workDir, "HEARTBEAT.md")

        return &HeartbeatService{
                config:        config,
                heartbeatFile: heartbeatFile,
                ctx:           ctx,
                cancel:        cancel,
        }
}

// Start 启动心跳服务
// 启用前提检查顺序：
//  1. 检查 config.Enabled 是否为 true，否则直接返回
//  2. 检查 LLM API 配置，未有会警告但不会阻止启动
//  3. 确保 HEARTBEAT.md 文件存在（自动创建）
//  4. 验证心跳间隔（最小 60 秒，最大 24 小时）
//  5. 检查通知器是否可用（可选）
func (s *HeartbeatService) Start() error {
        if !s.config.Enabled {
                log.Println("[Heartbeat] Service is disabled in config")
                return nil
        }

        // 检查 LLM 配置是否有效
        if apiKey == "" && baseURL == "" {
                log.Println("[Heartbeat] Warning: No LLM API configured, heartbeat may not work properly")
        }

        // 确保 HEARTBEAT.md 文件存在
        if err := s.ensureHeartbeatFile(); err != nil {
                return fmt.Errorf("failed to create heartbeat file: %w", err)
        }

        // 验证心跳间隔
        if s.config.IntervalSeconds < 60 {
                s.config.IntervalSeconds = 60 // 最小 1 分钟
        }
        if s.config.IntervalSeconds > 86400 {
                s.config.IntervalSeconds = 86400 // 最大 24 小时
        }

        // 创建定时器
        s.ticker = time.NewTicker(time.Duration(s.config.IntervalSeconds) * time.Second)
        s.running = true

        s.wg.Add(1)
        go s.run()

        log.Printf("[Heartbeat] Service started, interval: %d seconds, file: %s", 
                s.config.IntervalSeconds, s.heartbeatFile)

        // 检查通知器状态
        if globalHeartbeatNotifier == nil || !globalHeartbeatNotifier.IsAvailable() {
                log.Println("[Heartbeat] Warning: No notifier available, results will only be logged")
        } else {
                log.Println("[Heartbeat] Notifier is available for sending alerts")
        }

        return nil
}

// Stop 停止心跳服务
func (s *HeartbeatService) Stop() {
        s.mu.Lock()
        s.running = false
        s.mu.Unlock()

        s.cancel()

        if s.ticker != nil {
                s.ticker.Stop()
        }

        s.wg.Wait()
        log.Println("[Heartbeat] Service stopped")
}

// run 运行心跳循环
func (s *HeartbeatService) run() {
        defer s.wg.Done()

        // 首次检查（延迟 10 秒，等待系统初始化完成）
        select {
        case <-time.After(10 * time.Second):
        case <-s.ctx.Done():
                return
        }

        s.check()

        for {
                select {
                case <-s.ctx.Done():
                        return
                case <-s.ticker.C:
                        s.check()
                }
        }
}

// check 执行心跳检查
func (s *HeartbeatService) check() {
        s.mu.Lock()
        s.lastCheck = time.Now()
        s.mu.Unlock()

        log.Println("[Heartbeat] Checking for tasks...")

        tasks, err := s.readHeartbeatFile()
        if err != nil {
                log.Printf("[Heartbeat] Failed to read heartbeat file: %v", err)
                return
        }

        if len(tasks) == 0 {
                log.Println("[Heartbeat] No tasks defined in HEARTBEAT.md")
                return
        }

        log.Printf("[Heartbeat] Found %d task(s) to check", len(tasks))

        for _, task := range tasks {
                select {
                case <-s.ctx.Done():
                        return
                default:
                        s.processTask(task)
                }
        }
}

// processTask 处理单个心跳任务
func (s *HeartbeatService) processTask(task string) {
        taskPreview := TruncateString(task, 50)
        log.Printf("[Heartbeat] Processing: %s", taskPreview)

        // 执行检查
        result, err := s.executeCheck(task)
        if err != nil {
                log.Printf("[Heartbeat] Check failed: %v", err)
                return
        }

        // 判断是否需要告警
        shouldAlert := s.shouldAlert(result)

        if shouldAlert {
                log.Printf("[Heartbeat] Alert triggered for: %s", taskPreview)

                // 尝试发送通知
                if globalHeartbeatNotifier != nil && globalHeartbeatNotifier.IsAvailable() {
                        if err := globalHeartbeatNotifier.Notify(task, result, true); err != nil {
                                log.Printf("[Heartbeat] Failed to send notification: %v", err)
                        }
                } else {
                        // 没有通知器，打印到日志
                        log.Printf("[Heartbeat] Alert (no notifier): %s -> %s", taskPreview, TruncateString(result, 100))
                }
        } else {
                log.Printf("[Heartbeat] Check passed: %s", taskPreview)
        }
}

// executeCheck 执行检查任务
func (s *HeartbeatService) executeCheck(task string) (string, error) {
        ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
        defer cancel()

        systemPrompt := `你是一个后台检查任务执行助手。执行检查任务并返回简洁结果。

规则：
1. 执行检查，返回简洁结果
2. 如果发现问题，明确说明问题和建议
3. 如果一切正常，返回"正常"
4. 不要请求用户输入`

        history := []Message{
                {Role: "system", Content: systemPrompt},
                {Role: "user", Content: fmt.Sprintf("请执行以下检查任务：\n\n%s", task)},
        }

        response, err := CallModelSync(ctx, history, apiType, baseURL, apiKey, modelID, temperature, maxTokens, false, false)
        if err != nil {
                return "", fmt.Errorf("model call failed: %w", err)
        }

        result, ok := response.Content.(string)
        if !ok {
                return "", fmt.Errorf("unexpected response type")
        }

        return result, nil
}

// shouldAlert 判断是否需要告警
func (s *HeartbeatService) shouldAlert(result string) bool {
        lowerResult := strings.ToLower(result)

        // 问题关键词
        alertKeywords := []string{
                "错误", "error", "失败", "failed", "异常", "exception",
                "问题", "problem", "警告", "warning", "需要处理",
                "过期", "expired", "超时", "timeout", "异常", "不正常",
                "空间不足", "disk full", "内存不足", "out of memory",
        }

        for _, kw := range alertKeywords {
                if strings.Contains(lowerResult, strings.ToLower(kw)) {
                        return true
                }
        }

        // 检查是否包含正常标记
        normalMarkers := []string{"正常", "normal", "ok", "good", "健康", "healthy"}
        for _, marker := range normalMarkers {
                if strings.Contains(lowerResult, strings.ToLower(marker)) {
                        return false
                }
        }

        // 如果结果不包含正常标记，可能需要关注
        return len(result) > 20 // 有实质性内容则告警
}

// ensureHeartbeatFile 确保 HEARTBEAT.md 文件存在
func (s *HeartbeatService) ensureHeartbeatFile() error {
        if _, err := os.Stat(s.heartbeatFile); os.IsNotExist(err) {
                content := `# 心跳任务列表

这个文件定义了心跳服务需要定期检查的任务。
心跳服务会在后台自动执行这些检查，发现问题时通知你。

## 使用方法

每行一个任务，以 "- " 开头：

- 检查系统磁盘空间是否充足
- 检查是否有未读的重要邮件
- 检查服务器运行状态

## 配置

在 config.toon 中启用心跳：

heartbeat:
    enabled: true
    interval_seconds: 1800    # 检查间隔（秒），默认 30 分钟

## 注意事项

1. 任务描述应清晰明确
2. 避免需要用户交互的任务
3. 任务执行时间不宜过长（建议 < 60 秒）

---
`
                return os.WriteFile(s.heartbeatFile, []byte(content), 0644)
        }
        return nil
}

// readHeartbeatFile 读取心跳任务列表
func (s *HeartbeatService) readHeartbeatFile() ([]string, error) {
        data, err := os.ReadFile(s.heartbeatFile)
        if err != nil {
                return nil, err
        }

        var tasks []string
        lines := strings.Split(string(data), "\n")

        for _, line := range lines {
                line = strings.TrimSpace(line)
                // 跳过空行、注释和标题
                if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "---") {
                        continue
                }
                // 解析任务项
                if strings.HasPrefix(line, "- ") {
                        task := strings.TrimPrefix(line, "- ")
                        task = strings.TrimSpace(task)
                        if task != "" {
                                tasks = append(tasks, task)
                        }
                }
        }

        return tasks, nil
}

// GetStatus 获取心跳服务状态
func (s *HeartbeatService) GetStatus() map[string]interface{} {
        s.mu.RLock()
        defer s.mu.RUnlock()

        status := "stopped"
        if s.running {
                status = "running"
        }

        return map[string]interface{}{
                "status":      status,
                "enabled":     s.config.Enabled,
                "interval":    s.config.IntervalSeconds,
                "last_check":  s.lastCheck.Format(time.RFC3339),
                "file":        s.heartbeatFile,
                "has_notifier": globalHeartbeatNotifier != nil && globalHeartbeatNotifier.IsAvailable(),
        }
}

