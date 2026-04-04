package main

import (
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
	FeedbackTypeImplicit   FeedbackType = "implicit"   // 隐式反馈（从对话中推断）
	FeedbackTypeExplicit   FeedbackType = "explicit"   // 显式反馈（直接询问）
	FeedbackTypeInferred   FeedbackType = "inferred"   // 推断反馈（从行为推断）
)

// FeedbackRecord 反馈记录
type FeedbackRecord struct {
	ID            string       `json:"id"`
	SessionID     string       `json:"session_id"`
	MessageID     string       `json:"message_id"`
	FeedbackType  FeedbackType `json:"feedback_type"`
	Rating        int          `json:"rating"`        // 1-5 评分
	Category      string       `json:"category"`      // 反馈类别：helpfulness, accuracy, relevance, etc.
	UserMessage   string       `json:"user_message"`  // 用户的原始消息
	BotResponse   string       `json:"bot_response"`  // 助手的回复
	Context       string       `json:"context"`       // 上下文摘要
	Improvement   string       `json:"improvement"`   // 改进建议
	Timestamp     time.Time    `json:"timestamp"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// FeedbackCollector 反馈收集器
type FeedbackCollector struct {
	mu sync.RWMutex
	
	// 配置
	dataDir           string
	feedbackFile      string
	
	// 状态
	lastFeedbackTime  time.Time
	feedbackCount     int
	
	// 收集策略
	minMessagesBeforeAsk int     // 最少消息数后才询问
	askInterval         int      // 每隔多少轮询问一次
	implicitSignals     []ImplicitSignal
}

// ImplicitSignal 隐式反馈信号
type ImplicitSignal struct {
	Name        string
	Pattern     string
	Weight      float64
	Description string
}

// NewFeedbackCollector 创建新的反馈收集器
func NewFeedbackCollector(dataDir string) *FeedbackCollector {
	fc := &FeedbackCollector{
		dataDir:              dataDir,
		feedbackFile:         filepath.Join(dataDir, "feedback.jsonl"),
		minMessagesBeforeAsk: 5,
		askInterval:          10,
		implicitSignals: []ImplicitSignal{
			{
				Name:        "follow_up_question",
				Pattern:     "?",
				Weight:      0.3,
				Description: "用户追问，可能表示之前的回答不够清晰",
			},
			{
				Name:        "correction",
				Pattern:     "不对|错误|不是|应该|但是",
				Weight:      -0.5,
				Description: "用户纠正，表示回答有误",
			},
			{
				Name:        "gratitude",
				Pattern:     "多谢|谢谢|感谢|有用|帮了大忙",
				Weight:      0.5,
				Description: "用户表达感谢，表示回答有帮助",
			},
			{
				Name:        "satisfaction",
				Pattern:     "完美|好好|很好|不错|正是|非常好",
				Weight:      0.8,
				Description: "用户表达满意",
			},
			{
				Name:        "dissatisfaction",
				Pattern:     "不对|不成|不行|无用|无鸠用|无卵用|无屌用|无閪用|没用|还是|仍然",
				Weight:      -0.6,
				Description: "用户表达不满",
			},
		},
	}
	
	// 确保目录存在
	os.MkdirAll(dataDir, 0755)
	
	return fc
}

// ShouldAskForFeedback 判断是否应该询问反馈
func (fc *FeedbackCollector) ShouldAskForFeedback(messages []Message, turnCount int) bool {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	
	// 检查是否完成了一个任务
	if !fc.isTaskCompleted(messages) {
		return false
	}
	
	// 检查是否已经为当前任务询问过反馈
	if fc.hasAskedForCurrentTask(messages) {
		return false
	}
	
	// 消息数不够，不询问
	if turnCount < 3 {
		return false
	}
	
	return true
}

// isTaskCompleted 检测是否完成了一个任务
func (fc *FeedbackCollector) isTaskCompleted(messages []Message) bool {
	if len(messages) < 3 {
		return false
	}
	
	// 查找最近的助手回复
	lastAssistantMsg := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			if content, ok := messages[i].Content.(string); ok {
				lastAssistantMsg = content
				break
			}
		}
	}
	
	// 检查助手回复是否包含任务完成的信号
	completionSignals := []string{
		"完成", "完成了", "已完成","经已完成", "已经完成",
		"搞定", "解决", "解决了", "经已解决","已经解决",
		"好的", "明白了", "知道了", "了解",
		"可以了", "没问题", "冇问题", "完成任务", "任务完成",
	}
	
	lastAssistantMsg = strings.ToLower(lastAssistantMsg)
	for _, signal := range completionSignals {
		if strings.Contains(lastAssistantMsg, strings.ToLower(signal)) {
			return true
		}
	}
	
	// 检查是否有明确的总结或结论
	conclusionSignals := []string{
		"总结", "综上所述", "总之", "总的来说", "总体而言",
		"最终", "最后", "结果", "答案",
	}
	
	for _, signal := range conclusionSignals {
		if strings.Contains(lastAssistantMsg, strings.ToLower(signal)) {
			return true
		}
	}
	
	return false
}

// hasAskedForCurrentTask 检查是否已经为当前任务询问过反馈
func (fc *FeedbackCollector) hasAskedForCurrentTask(messages []Message) bool {
	if len(messages) < 2 {
		return false
	}
	
	// 查找最近的用户消息
	lastUserMsg := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			if content, ok := messages[i].Content.(string); ok {
				lastUserMsg = content
				break
			}
		}
	}
	
	// 检查最近的助手消息是否是反馈询问
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			if content, ok := messages[i].Content.(string); ok {
				// 检查是否包含反馈询问的标记
				if strings.Contains(content, "快速自检") || 
				   strings.Contains(content, "持续改进") ||
				   strings.Contains(content, "评分") {
					// 检查这个反馈询问是否在最近的用户消息之后
					if i > 0 && messages[i-1].Role == "user" {
						if userContent, ok := messages[i-1].Content.(string); ok {
							// 如果反馈询问是在最近的用户消息之后，说明已经询问过了
							if lastUserMsg == userContent {
								return true
							}
						}
					}
				}
			}
		}
	}
	
	return false
}

// GenerateFeedbackPrompt 生成反馈收集提示
func (fc *FeedbackCollector) GenerateFeedbackPrompt(context string) string {
	var prompt strings.Builder
	
	prompt.WriteString("\n\n---\n")
	prompt.WriteString("💭 **快速自检**\n\n")
	prompt.WriteString("作为你的助手，我希望持续改进。请告诉我：\n\n")
	prompt.WriteString("1. 刚才的回答对你有帮助吗？（1-5分，5分最有帮助）\n")
	prompt.WriteString("2. 如果有可以改进的地方，是什么？\n\n")
	prompt.WriteString("你可以直接回复评分（如\"4分\"），或者详细说明。你的反馈将帮助我变得更好。\n")
	prompt.WriteString("---\n")
	
	return prompt.String()
}

// ParseFeedbackFromMessage 从用户消息中解析反馈
func (fc *FeedbackCollector) ParseFeedbackFromMessage(userMessage, botResponse string, messages []Message) *FeedbackRecord {
	content := strings.ToLower(userMessage)
	
	// 尝试解析显式评分
	rating := fc.extractRating(content)
	
	// 检测隐式信号
	implicitScore := 0.0
	matchedSignals := []string{}
	
	for _, signal := range fc.implicitSignals {
		if strings.Contains(content, strings.ToLower(signal.Pattern)) {
			implicitScore += signal.Weight
			matchedSignals = append(matchedSignals, signal.Name)
		}
	}
	
	// 如果没有显式评分，但有隐式信号，根据信号计算评分
	if rating == 0 && implicitScore != 0 {
		rating = fc.scoreToRating(implicitScore)
	}
	
	// 如果没有检测到任何反馈信号，返回 nil
	if rating == 0 && len(matchedSignals) == 0 {
		return nil
	}
	
	// 提取改进建议
	improvement := fc.extractImprovement(userMessage)
	
	return &FeedbackRecord{
		ID:           generateFeedbackID(),
		FeedbackType: fc.determineFeedbackType(rating, matchedSignals),
		Rating:       rating,
		Category:     fc.categorizeFeedback(userMessage),
		UserMessage:  userMessage,
		BotResponse:  botResponse,
		Context:      fc.summarizeContext(messages),
		Improvement:  improvement,
		Timestamp:    time.Now(),
		Metadata: map[string]interface{}{
			"implicit_score":  implicitScore,
			"matched_signals": matchedSignals,
		},
	}
}

// extractRating 从消息中提取评分
func (fc *FeedbackCollector) extractRating(content string) int {
	// 匹配 "5分"、"4分"、"评分：3" 等格式
	if idx := strings.Index(content, "分"); idx > 0 && idx < len(content) {
		// 查找数字
		for i := idx - 1; i >= 0 && i >= idx-3; i-- {
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

// determineFeedbackType 确定反馈类型
func (fc *FeedbackCollector) determineFeedbackType(rating int, signals []string) FeedbackType {
	if rating > 0 && len(signals) == 0 {
		return FeedbackTypeExplicit
	}
	if rating > 0 && len(signals) > 0 {
		return FeedbackTypeImplicit
	}
	return FeedbackTypeInferred
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
	// 查找 "不过"、"但是"、"建议" 等引导词后的内容
	markers := []string{"不过", "但是", "建议", "如果", "希望", "可以改进"}
	
	for _, marker := range markers {
		if idx := strings.Index(message, marker); idx != -1 {
			// 提取标记后的内容
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
	
	// 提取最近的几轮对话主题
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

// SaveFeedback 保存反馈记录
func (fc *FeedbackCollector) SaveFeedback(record *FeedbackRecord) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	
	// 序列化为 JSON
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	
	// 追加写入文件
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
	
	log.Printf("[FeedbackCollector] Saved feedback: rating=%d, type=%s", record.Rating, record.FeedbackType)
	
	return nil
}

// GetFeedbackStats 获取反馈统计
func (fc *FeedbackCollector) GetFeedbackStats() map[string]interface{} {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	
	// 读取所有反馈记录
	records, err := fc.loadAllFeedback()
	if err != nil {
		return map[string]interface{}{
			"error": err.Error(),
		}
	}
	
	// 计算统计
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
		"total_feedback":   len(records),
		"average_rating":   avgRating,
		"by_type":          typeCount,
		"by_category":      categoryCount,
		"daily_trend":      dailyCount,
		"last_feedback":    fc.lastFeedbackTime,
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
			continue // 跳过无效记录
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
		log.Println("[FeedbackCollector] Initialized")
	}
}

// GetFeedbackCollector 获取反馈收集器
func GetFeedbackCollector() *FeedbackCollector {
	return globalFeedbackCollector
}
