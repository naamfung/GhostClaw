package main

import (
        "context"
        "encoding/json"
        "fmt"
        "log"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "time"
)

// FeedbackType 反馈类型
type FeedbackType string

const (
        FeedbackTypeImplicit FeedbackType = "implicit" // 隐式反馈（从对话行为推断）
        FeedbackTypeInferred FeedbackType = "inferred" // 推断反馈（从用户情绪信号推断）
        FeedbackTypeExplicit FeedbackType = "explicit" // 显式反馈（用户主动给出评分，保留但不再主动索求）
)

// FeedbackRecord 反馈记录
type FeedbackRecord struct {
        ID           string                 `json:"id"`
        SessionID    string                 `json:"session_id"`
        MessageID    string                 `json:"message_id"`
        FeedbackType FeedbackType           `json:"feedback_type"`
        Rating       int                    `json:"rating"`        // 1-5 评分
        Category     string                 `json:"category"`      // 反馈类别：helpfulness, accuracy, relevance, etc.
        UserMessage  string                 `json:"user_message"`  // 用户的原始消息
        BotResponse  string                 `json:"bot_response"`  // 助手的回复
        Context      string                 `json:"context"`       // 上下文摘要
        Improvement  string                 `json:"improvement"`   // 改进建议
        Timestamp    time.Time              `json:"timestamp"`
        Metadata     map[string]interface{} `json:"metadata"`
}

// TaskCompletionQuery 任务完成查询的轻量 API 配置
type TaskCompletionQuery struct {
        APIType  string
        BaseURL  string
        APIKey   string
        ModelID  string
}

// FeedbackCollector 隐式反馈收集器
type FeedbackCollector struct {
        mu sync.RWMutex

        // 配置
        dataDir      string
        feedbackFile string

        // 状态
        lastFeedbackTime time.Time
        feedbackCount    int

        // 任务完成追踪（跨轮隐式反馈的核心）
        taskCompletedAt time.Time   // 上次任务完成的时间点
        taskTopic       string      // 上次完成任务的摘要，用于判断用户是否开始新话题
        taskBotResponse string      // 上次任务完成的助手回复，用于跨轮关联

        // 隐式信号库
        implicitSignals []ImplicitSignal

        // AskModelTaskCompletion 冷却机制（防止短时间重复调用）
        lastCompletionAskTime time.Time // 上次调用 AskModelTaskCompletion 的时间
        minAskInterval       time.Duration
}

// ImplicitSignal 隐式反馈信号
type ImplicitSignal struct {
        Name        string
        Pattern     string // 支持管道符分隔的多模式
        Weight      float64
        Description string
}

// NewFeedbackCollector 创建新的反馈收集器
func NewFeedbackCollector(dataDir string) *FeedbackCollector {
        fc := &FeedbackCollector{
                dataDir:        dataDir,
                feedbackFile:   filepath.Join(dataDir, "feedback.jsonl"),
                minAskInterval: 30 * time.Second, // 两次 AskModelTaskCompletion 最小间隔
                implicitSignals: []ImplicitSignal{
                        // === 正向信号 ===
                        {
                                Name:        "gratitude",
                                Pattern:     "多谢|謝謝|感谢|感恩|有用|帮了大忙|幫了大忙|thx|thanks|thank you",
                                Weight:      0.5,
                                Description: "用户表达感谢",
                        },
                        {
                                Name:        "satisfaction",
                                Pattern:     "完美|好好|很好|不错|不錯|正是|非常好|厲害|厉害|牛|太棒了|赞",
                                Weight:      0.8,
                                Description: "用户表达满意",
                        },
                        {
                                Name:        "task_accepted",
                                Pattern:     "可以|好的|ok|没问题|沒問題|明白|了解|收到|got it",
                                Weight:      0.3,
                                Description: "用户接受结果（仅在任务完成标记后有效）",
                        },
                        // === 负向信号 ===
                        {
                                Name:        "correction",
                                Pattern:     "不对|不對|错误|錯誤|不是这样|不是這樣|应该|應該|但是不对",
                                Weight:      -0.6,
                                Description: "用户纠正，表示回答有误",
                        },
                        {
                                Name:        "dissatisfaction",
                                Pattern:     "无用|無用|没用|沒用|还是不行|還是不行|仍然|還是|无鸠用|无卵用|无屌用|无閪用|唔得|唔掂",
                                Weight:      -0.7,
                                Description: "用户表达不满",
                        },
                        {
                                Name:        "redo_request",
                                Pattern:     "重做|重新|再来一次|再試|再试|换个方式|換個方式",
                                Weight:      -0.8,
                                Description: "用户要求重做",
                        },
                        {
                                Name:        "confusion",
                                Pattern:     "不懂|看不懂|不明白|不理解|看不明白|什麼意思|什么意思",
                                Weight:      -0.4,
                                Description: "用户表示困惑",
                        },
                        // === 中性/过渡信号 ===
                        {
                                Name:        "follow_up_question",
                                Pattern:     "？|?",
                                Weight:      0.1,
                                Description: "用户追问（弱信号，需配合其他上下文判断）",
                        },
                },
        }

        // 确保目录存在
        os.MkdirAll(dataDir, 0755)

        return fc
}

// ========== 冷却与过滤机制 ==========

// CanAskCompletion 检查是否可以调用 AskModelTaskCompletion（冷却期内不允许）
func (fc *FeedbackCollector) CanAskCompletion() bool {
        fc.mu.RLock()
        defer fc.mu.RUnlock()
        if fc.lastCompletionAskTime.IsZero() {
                return true
        }
        return time.Since(fc.lastCompletionAskTime) >= fc.minAskInterval
}

// RecordCompletionAsk 记录一次 AskModelTaskCompletion 调用时间
func (fc *FeedbackCollector) RecordCompletionAsk() {
        fc.mu.Lock()
        defer fc.mu.Unlock()
        fc.lastCompletionAskTime = time.Now()
}

// IsWakeNotification 检测输入消息是否为异步任务唤醒通知（系统生成，非真实用户任务）
func IsWakeNotification(input string) bool {
        return strings.Contains(input, "任务唤醒通知") ||
                strings.Contains(input, "Task Wake") ||
                strings.Contains(input, "task wake") ||
                strings.Contains(input, "Wake notification")
}

// ========== 任务完成判定（轻量模型调用） ==========

// AskModelTaskCompletion 私下询问模型用户任务是否已完成
// 发送最小化请求：最后一条用户消息 + 模型回复 + 系统提示
// 返回 true 表示任务已完成，false 表示未完成或调用失败
func (fc *FeedbackCollector) AskModelTaskCompletion(ctx context.Context, lastUserMsg, lastAssistantMsg string, apiConfig TaskCompletionQuery) bool {
        if apiConfig.ModelID == "" || apiConfig.APIKey == "" {
                log.Printf("[FeedbackCollector] AskModelTaskCompletion skipped: missing API config")
                return false
        }

        // 截断过长的内容，避免浪费 token
        if len(lastUserMsg) > 500 {
                lastUserMsg = lastUserMsg[:500] + "..."
        }
        if len(lastAssistantMsg) > 500 {
                lastAssistantMsg = lastAssistantMsg[:500] + "..."
        }

        messages := []Message{
                {Role: "system", Content: "你是一个任务完成度判定器。根据用户请求和助手回复，判断用户的请求是否已被完整完成。\n\n规则：\n- 助手已给出最终结论或代码/修复/方案，且没有遗留待办事项 → YES\n- 助手仍在调查、分析、执行中，或明确表示需要进一步操作 → NO\n- 助手给出了结果但提到需要测试/验证/后续步骤 → NO\n\n只回答 YES 或 NO，不要输出任何其他内容。"},
                {Role: "user", Content: fmt.Sprintf("用户请求：\n%s\n\n助手回复：\n%s", lastUserMsg, lastAssistantMsg)},
        }

        resp, err := CallModelSync(ctx, messages, apiConfig.APIType, apiConfig.BaseURL, apiConfig.APIKey, apiConfig.ModelID, 0.0, 50, false, false)
        if err != nil {
                log.Printf("[FeedbackCollector] AskModelTaskCompletion error: %v", err)
                return false
        }

        answer := ""
        if content, ok := resp.Content.(string); ok {
                answer = strings.TrimSpace(strings.ToUpper(content))
        }

        completed := strings.HasPrefix(answer, "YES")
        log.Printf("[FeedbackCollector] Task completion check: %s (answer: %s)", map[bool]string{true: "YES", false: "NO"}[completed], answer)

        return completed
}

// MarkTaskCompleted 标记一个任务已完成（由 AgentLoop 在模型确认后调用）
func (fc *FeedbackCollector) MarkTaskCompleted(topic string, botResponse string) {
        fc.mu.Lock()
        defer fc.mu.Unlock()

        fc.taskCompletedAt = time.Now()
        fc.taskTopic = topic
        if len(fc.taskTopic) > 100 {
                fc.taskTopic = fc.taskTopic[:100]
        }
        fc.taskBotResponse = botResponse
        if len(fc.taskBotResponse) > 300 {
                fc.taskBotResponse = fc.taskBotResponse[:300]
        }
}

// IsTaskJustCompleted 检查是否有刚完成的任务可用于跨轮隐式反馈
// 返回上次的 botResponse（用于关联评分），如果太久了则返回空
func (fc *FeedbackCollector) IsTaskJustCompleted() string {
        fc.mu.RLock()
        defer fc.mu.RUnlock()

        // 超过 10 分钟未收到用户消息，认为窗口期已过
        if fc.taskCompletedAt.IsZero() || time.Since(fc.taskCompletedAt) > 10*time.Minute {
                return ""
        }

        return fc.taskBotResponse
}

// ClearTaskCompleted 清除任务完成标记（用户已发送新消息并完成反馈采集）
func (fc *FeedbackCollector) ClearTaskCompleted() {
        fc.mu.Lock()
        defer fc.mu.Unlock()

        fc.taskCompletedAt = time.Time{}
        fc.taskTopic = ""
        fc.taskBotResponse = ""
}

// IsNewTopic 检测用户是否开启了新话题（而非对上次任务的延续反馈）
func (fc *FeedbackCollector) IsNewTopic(userMessage string) bool {
        fc.mu.RLock()
        defer fc.mu.RUnlock()

        if fc.taskTopic == "" {
                return true
        }

        // 如果用户的上一轮任务刚完成，且新消息与任务主题完全无关，视为新话题
        // 使用简单的关键词重叠检测
        topicLower := strings.ToLower(fc.taskTopic)
        msgLower := strings.ToLower(userMessage)

        // 新消息很短且是命令式，大概率是新任务
        fields := strings.Fields(msgLower)
        if len(fields) <= 5 && !strings.Contains(msgLower, topicLower[:min(20, len(topicLower))]) {
                return true
        }

        return false
}

// ========== 隐式反馈采集 ==========

// CollectImplicitFeedback 跨轮隐式反馈采集
// 在用户发送新消息时调用：检查上一轮是否有完成的任务，从新消息中提取隐式信号
func (fc *FeedbackCollector) CollectImplicitFeedback(userMessage string, messages []Message) *FeedbackRecord {
        prevBotResponse := fc.IsTaskJustCompleted()
        if prevBotResponse == "" {
                return nil
        }

        // 新话题 → 上一轮任务被静默接受，记录正向反馈
        if fc.IsNewTopic(userMessage) {
                fc.ClearTaskCompleted()
                record := &FeedbackRecord{
                        ID:           generateFeedbackID(),
                        FeedbackType: FeedbackTypeInferred,
                        Rating:       4, // 默认正面（用户未抱怨直接开始新任务）
                        Category:     "acceptance",
                        UserMessage:  userMessage,
                        BotResponse:  prevBotResponse,
                        Context:      "用户直接开启新话题，隐式接受了上次结果",
                        Timestamp:    time.Now(),
                        Metadata: map[string]interface{}{
                                "signal":        "topic_change",
                                "implicit_score": 0.4,
                        },
                }
                _ = fc.SaveFeedback(record)
                return record
        }

        content := strings.ToLower(userMessage)

        // 检测隐式信号
        implicitScore := 0.0
        matchedSignals := []string{}

        for _, signal := range fc.implicitSignals {
                // Pattern 支持管道符分隔的多模式
                patterns := strings.Split(signal.Pattern, "|")
                for _, pattern := range patterns {
                        if strings.Contains(content, strings.ToLower(pattern)) {
                                implicitScore += signal.Weight
                                matchedSignals = append(matchedSignals, signal.Name)
                                break // 同一信号只计一次
                        }
                }
        }

        // 没有匹配到任何信号 → 中性接受，不记录
        if len(matchedSignals) == 0 {
                return nil
        }

        // 有匹配信号 → 清除标记并记录
        fc.ClearTaskCompleted()

        rating := fc.scoreToRating(implicitScore)
        improvement := fc.extractImprovement(userMessage)

        record := &FeedbackRecord{
                ID:           generateFeedbackID(),
                FeedbackType: FeedbackTypeImplicit,
                Rating:       rating,
                Category:     fc.categorizeFeedback(userMessage),
                UserMessage:  userMessage,
                BotResponse:  prevBotResponse,
                Context:      fc.summarizeContext(messages),
                Improvement:  improvement,
                Timestamp:    time.Now(),
                Metadata: map[string]interface{}{
                        "signal":         "cross_turn",
                        "implicit_score": implicitScore,
                        "matched_signals": matchedSignals,
                },
        }

        _ = fc.SaveFeedback(record)
        return record
}

// ParseFeedbackFromMessage 从用户消息中解析显式评分（保留兼容性）
func (fc *FeedbackCollector) ParseFeedbackFromMessage(userMessage, botResponse string, messages []Message) *FeedbackRecord {
        content := strings.ToLower(userMessage)

        // 只提取显式评分（用户主动给出）
        rating := fc.extractRating(content)
        if rating == 0 {
                return nil
        }

        improvement := fc.extractImprovement(userMessage)

        return &FeedbackRecord{
                ID:           generateFeedbackID(),
                FeedbackType: FeedbackTypeExplicit,
                Rating:       rating,
                Category:     fc.categorizeFeedback(userMessage),
                UserMessage:  userMessage,
                BotResponse:  botResponse,
                Context:      fc.summarizeContext(messages),
                Improvement:  improvement,
                Timestamp:    time.Now(),
        }
}

// ========== 内部辅助方法 ==========

// extractRating 从消息中提取显式评分
func (fc *FeedbackCollector) extractRating(content string) int {
        // 匹配 "5分"、"4分" 等格式
        if idx := strings.Index(content, "分"); idx > 0 && idx < len(content) {
                for i := idx - 1; i >= 0 && i >= idx-3; i++ {
                        c := content[i]
                        if c >= '1' && c <= '5' {
                                return int(c - '0')
                        }
                }
        }

        // 检测文字描述
        if strings.Contains(content, "完美") || strings.Contains(content, "非常好") {
                return 5
        }
        if strings.Contains(content, "很好") || strings.Contains(content, "很有帮助") {
                return 4
        }
        if strings.Contains(content, "还可以") || strings.Contains(content, "一般") {
                return 3
        }
        if strings.Contains(content, "不太好") || strings.Contains(content, "不太对") {
                return 2
        }
        if strings.Contains(content, "没用") || strings.Contains(content, "完全不对") {
                return 1
        }

        return 0
}

// scoreToRating 将隐式分数转换为评分
func (fc *FeedbackCollector) scoreToRating(score float64) int {
        switch {
        case score >= 0.5:
                return 5
        case score >= 0.3:
                return 4
        case score >= 0:
                return 3
        case score >= -0.3:
                return 2
        default:
                return 1
        }
}

// categorizeFeedback 分类反馈
func (fc *FeedbackCollector) categorizeFeedback(message string) string {
        content := strings.ToLower(message)

        if strings.Contains(content, "准确") || strings.Contains(content, "正确") || strings.Contains(content, "错误") {
                return "accuracy"
        }
        if strings.Contains(content, "帮助") || strings.Contains(content, "有用") || strings.Contains(content, "解决问题") {
                return "helpfulness"
        }
        if strings.Contains(content, "理解") || strings.Contains(content, "明白") || strings.Contains(content, "清楚") {
                return "clarity"
        }
        if strings.Contains(content, "速度") || strings.Contains(content, "慢") || strings.Contains(content, "快") {
                return "speed"
        }

        return "general"
}

// extractImprovement 提取改进建议
func (fc *FeedbackCollector) extractImprovement(message string) string {
        markers := []string{"不过", "但是", "建议", "如果", "希望", "可以改进", "不過", "建議", "希望"}

        for _, marker := range markers {
                if idx := strings.Index(message, marker); idx != -1 {
                        suggestion := strings.TrimSpace(message[idx:])
                        if len(suggestion) > 10 {
                                return suggestion
                        }
                }
        }

        return ""
}

// summarizeContext 总结上下文
func (fc *FeedbackCollector) summarizeContext(messages []Message) string {
        if len(messages) == 0 {
                return ""
        }

        var topics []string
        for i := len(messages) - 1; i >= 0 && i >= len(messages)-6; i-- {
                msg := messages[i]
                if msg.Role == "user" {
                        content := fmt.Sprintf("%v", msg.Content)
                        if len(content) > 50 {
                                content = content[:50] + "..."
                        }
                        topics = append([]string{content}, topics...)
                }
        }

        return strings.Join(topics, " → ")
}

// ========== 持久化 ==========

// SaveFeedback 保存反馈记录
func (fc *FeedbackCollector) SaveFeedback(record *FeedbackRecord) error {
        fc.mu.Lock()
        defer fc.mu.Unlock()

        data, err := json.Marshal(record)
        if err != nil {
                return err
        }

        f, err := os.OpenFile(fc.feedbackFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
        if err != nil {
                return err
        }
        defer f.Close()

        if _, err := f.WriteString(string(data) + "\n"); err != nil {
                return err
        }

        fc.feedbackCount++
        fc.lastFeedbackTime = time.Now()

        log.Printf("[FeedbackCollector] Saved implicit feedback: rating=%d, type=%s, category=%s", record.Rating, record.FeedbackType, record.Category)

        return nil
}

// GetFeedbackStats 获取反馈统计
func (fc *FeedbackCollector) GetFeedbackStats() map[string]interface{} {
        fc.mu.RLock()
        defer fc.mu.RUnlock()

        records, err := fc.loadAllFeedback()
        if err != nil {
                return map[string]interface{}{
                        "error": err.Error(),
                }
        }

        var totalRating int
        typeCount := make(map[FeedbackType]int)
        categoryCount := make(map[string]int)
        dailyCount := make(map[string]int)

        for _, r := range records {
                totalRating += r.Rating
                typeCount[r.FeedbackType]++
                categoryCount[r.Category]++

                day := r.Timestamp.Format("2006-01-02")
                dailyCount[day]++
        }

        avgRating := 0.0
        if len(records) > 0 {
                avgRating = float64(totalRating) / float64(len(records))
        }

        return map[string]interface{}{
                "total_feedback": len(records),
                "average_rating": avgRating,
                "by_type":        typeCount,
                "by_category":    categoryCount,
                "daily_trend":    dailyCount,
                "last_feedback":  fc.lastFeedbackTime,
        }
}

// loadAllFeedback 加载所有反馈记录
func (fc *FeedbackCollector) loadAllFeedback() ([]FeedbackRecord, error) {
        data, err := os.ReadFile(fc.feedbackFile)
        if err != nil {
                if os.IsNotExist(err) {
                        return []FeedbackRecord{}, nil
                }
                return nil, err
        }

        var records []FeedbackRecord
        lines := strings.Split(string(data), "\n")

        for _, line := range lines {
                line = strings.TrimSpace(line)
                if line == "" {
                        continue
                }

                var record FeedbackRecord
                if err := json.Unmarshal([]byte(line), &record); err != nil {
                        continue
                }
                records = append(records, record)
        }

        return records, nil
}

// generateFeedbackID 生成反馈 ID
func generateFeedbackID() string {
        return fmt.Sprintf("fb_%d", time.Now().UnixNano())
}

// ========== 全局实例 ==========

var globalFeedbackCollector *FeedbackCollector

// InitFeedbackCollector 初始化反馈收集器
func InitFeedbackCollector(dataDir string) {
        if globalFeedbackCollector == nil {
                globalFeedbackCollector = NewFeedbackCollector(dataDir)
                log.Println("[FeedbackCollector] Initialized (implicit-only mode)")
        }
}

// GetFeedbackCollector 获取反馈收集器
func GetFeedbackCollector() *FeedbackCollector {
        return globalFeedbackCollector
}

// GetRatingDistribution 获取评分分布数据
func (fc *FeedbackCollector) GetRatingDistribution() map[int]int {
        fc.mu.RLock()
        defer fc.mu.RUnlock()

        records, err := fc.loadAllFeedback()
        if err != nil {
                return make(map[int]int)
        }

        ratingDistribution := make(map[int]int)
        for _, record := range records {
                if record.Rating >= 1 && record.Rating <= 5 {
                        ratingDistribution[record.Rating]++
                }
        }

        return ratingDistribution
}

// GetDailyRatings 获取每日评分数据
func (fc *FeedbackCollector) GetDailyRatings() map[string][]int {
        fc.mu.RLock()
        defer fc.mu.RUnlock()

        records, err := fc.loadAllFeedback()
        if err != nil {
                return make(map[string][]int)
        }

        dailyRatings := make(map[string][]int)
        for _, record := range records {
                date := record.Timestamp.Format("2006-01-02")
                if record.Rating >= 1 && record.Rating <= 5 {
                        dailyRatings[date] = append(dailyRatings[date], record.Rating)
                }
        }

        return dailyRatings
}
