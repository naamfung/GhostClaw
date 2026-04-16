package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// InsightsReport 分析报告
type InsightsReport struct {
	Overview        OverviewStats    `json:"overview"`
	ModelBreakdown  ModelBreakdown   `json:"model_breakdown"`
	ToolUsage       ToolUsageStats   `json:"tool_usage"`
	ActivityPattern ActivityPattern  `json:"activity_pattern"`
	FeedbackStats   FeedbackStats    `json:"feedback_stats"`
	TopSessions     []TopSession     `json:"top_sessions"`
	Recommendations []Recommendation `json:"recommendations"`
	Timestamp       time.Time        `json:"timestamp"`
}

// OverviewStats 概览统计
type OverviewStats struct {
	TotalSessions     int     `json:"total_sessions"`
	TotalMessages     int     `json:"total_messages"`
	TotalToolCalls    int     `json:"total_tool_calls"`
	TotalFeedback     int     `json:"total_feedback"`
	AverageRating     float64 `json:"average_rating"`
	SuccessRate       float64 `json:"success_rate"`
	AverageSessionLen float64 `json:"average_session_len"`
	TotalTokens       int     `json:"total_tokens"`
}

// ModelBreakdown 模型使用统计
type ModelBreakdown struct {
	Usage      map[string]int `json:"usage"`       // 模型名称 -> 使用次数
	TokenUsage map[string]int `json:"token_usage"` // 模型名称 -> Token 消耗
}

// ToolUsageStats 工具使用统计
type ToolUsageStats struct {
	TopTools     []ToolUsageItem    `json:"top_tools"`
	SuccessRates map[string]float64 `json:"success_rates"`
}

// ToolUsageItem 工具使用项
type ToolUsageItem struct {
	Name        string  `json:"name"`
	Count       int     `json:"count"`
	SuccessRate float64 `json:"success_rate"`
}

// ActivityPattern 活动模式
type ActivityPattern struct {
	ByDay     map[string]int `json:"by_day"`     // 日期 -> 会话数
	ByHour    map[int]int    `json:"by_hour"`    // 小时 -> 会话数
	PeakHours []int          `json:"peak_hours"` // 峰值小时
}

// FeedbackStats 反馈统计
type FeedbackStats struct {
	RatingDistribution map[int]int    `json:"rating_distribution"` // 评分 -> 次数
	ByCategory         map[string]int `json:"by_category"`         // 类别 -> 次数
	Trend              []RatingTrend  `json:"trend"`               // 评分趋势
}

// RatingTrend 评分趋势
type RatingTrend struct {
	Date   string  `json:"date"`
	Rating float64 `json:"rating"`
}

// TopSession 顶级会话
type TopSession struct {
	ID            string    `json:"id"`
	Timestamp     time.Time `json:"timestamp"`
	MessageCount  int       `json:"message_count"`
	ToolCallCount int       `json:"tool_call_count"`
	Duration      int       `json:"duration"`
	Rating        int       `json:"rating"`
	Model         string    `json:"model"`
}

// Recommendation 改进建议
type Recommendation struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"` // high, medium, low
}

// InsightsEngine 分析引擎
type InsightsEngine struct {
	mu sync.RWMutex

	// 配置
	dataDir    string
	reportFile string

	// 依赖
	trajectoryManager *TrajectoryManager
	feedbackCollector *FeedbackCollector
}

// NewInsightsEngine 创建新的分析引擎
func NewInsightsEngine(dataDir string, trajectoryManager *TrajectoryManager, feedbackCollector *FeedbackCollector) *InsightsEngine {
	engine := &InsightsEngine{
		dataDir:           dataDir,
		reportFile:        filepath.Join(dataDir, "insights_report.json"),
		trajectoryManager: trajectoryManager,
		feedbackCollector: feedbackCollector,
	}

	// 确保目录存在
	os.MkdirAll(dataDir, 0755)

	return engine
}

// GetMemoryStats 获取记忆统计
func (ie *InsightsEngine) GetMemoryStats() map[string]interface{} {
	memoryStats := make(map[string]interface{})

	// 获取全局记忆系统
	if globalUnifiedMemory != nil {
		// 这里可以实现记忆统计逻辑
		// 例如获取不同类别的记忆数量、使用频率等
		memoryStats["available"] = true
		memoryStats["message"] = "Memory system integrated"
	} else {
		memoryStats["available"] = false
		memoryStats["message"] = "Memory system not initialized"
	}

	return memoryStats
}

// GenerateReport 生成分析报告
func (ie *InsightsEngine) GenerateReport(days int) *InsightsReport {
	ie.mu.Lock()
	defer ie.mu.Unlock()

	startTime := time.Now().AddDate(0, 0, -days)

	report := &InsightsReport{
		Overview:        ie.computeOverview(startTime),
		ModelBreakdown:  ie.computeModelBreakdown(startTime),
		ToolUsage:       ie.computeToolUsage(startTime),
		ActivityPattern: ie.computeActivityPattern(startTime),
		FeedbackStats:   ie.computeFeedbackStats(startTime),
		TopSessions:     ie.computeTopSessions(startTime),
		Recommendations: ie.generateRecommendations(startTime),
		Timestamp:       time.Now(),
	}

	// 保存报告
	if err := ie.saveReport(report); err != nil {
		log.Printf("[InsightsEngine] Failed to save report: %v", err)
	}

	log.Printf("[InsightsEngine] Generated report for last %d days", days)
	return report
}

// computeOverview 计算概览统计
func (ie *InsightsEngine) computeOverview(startTime time.Time) OverviewStats {
	if ie.trajectoryManager == nil {
		return OverviewStats{}
	}

	stats := ie.trajectoryManager.GetTrajectoryStats()

	totalSessions := 0
	totalMessages := 0
	totalToolCalls := 0
	totalTokens := 0
	successCount := 0

	if val, ok := stats["total_trajectories"].(int); ok {
		totalSessions = val
	}
	if val, ok := stats["success_count"].(int); ok {
		successCount = val
	}
	if val, ok := stats["average_messages"].(float64); ok {
		totalMessages = int(val * float64(totalSessions))
	}
	if val, ok := stats["average_tool_calls"].(float64); ok {
		totalToolCalls = int(val * float64(totalSessions))
	}
	if val, ok := stats["average_tokens"].(float64); ok {
		totalTokens = int(val * float64(totalSessions))
	}

	averageSessionLen := 0.0
	if val, ok := stats["average_duration"].(float64); ok {
		averageSessionLen = val
	}

	successRate := 0.0
	if totalSessions > 0 {
		successRate = float64(successCount) / float64(totalSessions)
	}

	totalFeedback := 0
	averageRating := 0.0
	if ie.feedbackCollector != nil {
		feedbackStats := ie.feedbackCollector.GetFeedbackStats()
		if val, ok := feedbackStats["total_feedback"].(int); ok {
			totalFeedback = val
		}
		if val, ok := feedbackStats["average_rating"].(float64); ok {
			averageRating = val
		}
	}

	return OverviewStats{
		TotalSessions:     totalSessions,
		TotalMessages:     totalMessages,
		TotalToolCalls:    totalToolCalls,
		TotalFeedback:     totalFeedback,
		AverageRating:     averageRating,
		SuccessRate:       successRate,
		AverageSessionLen: averageSessionLen,
		TotalTokens:       totalTokens,
	}
}

// computeModelBreakdown 计算模型使用统计
func (ie *InsightsEngine) computeModelBreakdown(startTime time.Time) ModelBreakdown {
	if ie.trajectoryManager == nil {
		return ModelBreakdown{}
	}

	stats := ie.trajectoryManager.GetTrajectoryStats()

	modelUsage := make(map[string]int)
	if val, ok := stats["model_usage"].(map[string]int); ok {
		modelUsage = val
	}

	tokenUsage := make(map[string]int)
	// 初始化 Token 使用统计
	for model := range modelUsage {
		tokenUsage[model] = 0
	}

	return ModelBreakdown{
		Usage:      modelUsage,
		TokenUsage: tokenUsage,
	}
}

// computeToolUsage 计算工具使用统计
func (ie *InsightsEngine) computeToolUsage(startTime time.Time) ToolUsageStats {
	if ie.trajectoryManager == nil {
		return ToolUsageStats{}
	}

	stats := ie.trajectoryManager.GetTrajectoryStats()

	toolUsage := make(map[string]int)
	if val, ok := stats["tool_usage"].(map[string]int); ok {
		toolUsage = val
	}

	// 获取工具成功率数据
	toolSuccessRates := ie.trajectoryManager.GetToolSuccessRates()

	// 转换为排序的工具使用项
	var toolItems []ToolUsageItem
	for name, count := range toolUsage {
		successRate := 0.0
		if rate, ok := toolSuccessRates[name]; ok {
			successRate = rate
		}
		toolItems = append(toolItems, ToolUsageItem{
			Name:        name,
			Count:       count,
			SuccessRate: successRate,
		})
	}

	// 按使用次数排序
	sort.Slice(toolItems, func(i, j int) bool {
		return toolItems[i].Count > toolItems[j].Count
	})

	// 只取前10个
	if len(toolItems) > 10 {
		toolItems = toolItems[:10]
	}

	return ToolUsageStats{
		TopTools:     toolItems,
		SuccessRates: toolSuccessRates,
	}
}

// computeActivityPattern 计算活动模式
func (ie *InsightsEngine) computeActivityPattern(startTime time.Time) ActivityPattern {
	if ie.trajectoryManager == nil {
		return ActivityPattern{}
	}

	// 从轨迹数据中提取活动模式
	trajectories, err := ie.trajectoryManager.GetTrajectories()
	if err != nil {
		trajectories = []Trajectory{}
	}

	byDay := make(map[string]int)
	byHour := make(map[int]int)

	// 统计每天的会话数
	for _, t := range trajectories {
		if t.Timestamp.After(startTime) {
			date := t.Timestamp.Format("2006-01-02")
			byDay[date]++

			hour := t.Timestamp.Hour()
			byHour[hour]++
		}
	}

	// 如果没有数据，使用默认值
	if len(byDay) == 0 {
		// 生成过去7天的数据
		for i := 0; i < 7; i++ {
			date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
			byDay[date] = 0
		}

		// 生成24小时的数据
		for i := 0; i < 24; i++ {
			byHour[i] = 0
		}
	}

	// 找出峰值小时
	var peakHours []int
	maxCount := 0
	for hour, count := range byHour {
		if count > maxCount {
			maxCount = count
			peakHours = []int{hour}
		} else if count == maxCount {
			peakHours = append(peakHours, hour)
		}
	}

	return ActivityPattern{
		ByDay:     byDay,
		ByHour:    byHour,
		PeakHours: peakHours,
	}
}

// computeFeedbackStats 计算反馈统计
func (ie *InsightsEngine) computeFeedbackStats(startTime time.Time) FeedbackStats {
	if ie.feedbackCollector == nil {
		return FeedbackStats{}
	}

	stats := ie.feedbackCollector.GetFeedbackStats()

	// 从反馈收集器获取真实的评分分布
	ratingDistribution := ie.feedbackCollector.GetRatingDistribution()
	// 初始化评分分布
	for i := 1; i <= 5; i++ {
		if _, ok := ratingDistribution[i]; !ok {
			ratingDistribution[i] = 0
		}
	}

	byCategory := make(map[string]int)
	if val, ok := stats["by_category"].(map[string]int); ok {
		byCategory = val
	} else {
		// 初始化类别分布
		byCategory["helpfulness"] = 0
		byCategory["accuracy"] = 0
		byCategory["clarity"] = 0
		byCategory["speed"] = 0
		byCategory["general"] = 0
	}

	// 生成评分趋势
	var trend []RatingTrend
	// 从反馈收集器获取真实数据
	dailyRatings := ie.feedbackCollector.GetDailyRatings()
	if len(dailyRatings) > 0 {
		// 按日期排序
		dates := make([]string, 0, len(dailyRatings))
		for date := range dailyRatings {
			dates = append(dates, date)
		}
		sort.Strings(dates)

		for _, date := range dates {
			ratings := dailyRatings[date]
			if len(ratings) > 0 {
				total := 0
				for _, r := range ratings {
					total += r
				}
				average := float64(total) / float64(len(ratings))
				trend = append(trend, RatingTrend{Date: date, Rating: average})
			}
		}
	}

	// 如果没有数据，生成默认趋势
	if len(trend) == 0 {
		for i := 6; i >= 0; i-- {
			date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
			trend = append(trend, RatingTrend{Date: date, Rating: 0})
		}
	}

	return FeedbackStats{
		RatingDistribution: ratingDistribution,
		ByCategory:         byCategory,
		Trend:              trend,
	}
}

// computeTopSessions 计算顶级会话
func (ie *InsightsEngine) computeTopSessions(startTime time.Time) []TopSession {
	var topSessions []TopSession

	if ie.trajectoryManager == nil {
		return topSessions
	}

	// 从轨迹数据中提取顶级会话
	trajectories, err := ie.trajectoryManager.GetTrajectories()
	if err != nil {
		return topSessions
	}

	// 过滤出 startTime 之后的轨迹
	var filteredTrajectories []Trajectory
	for _, t := range trajectories {
		if t.Timestamp.After(startTime) {
			filteredTrajectories = append(filteredTrajectories, t)
		}
	}

	// 按消息数和工具调用数排序
	sort.Slice(filteredTrajectories, func(i, j int) bool {
		// 首先按消息数排序
		if len(filteredTrajectories[i].Messages) != len(filteredTrajectories[j].Messages) {
			return len(filteredTrajectories[i].Messages) > len(filteredTrajectories[j].Messages)
		}
		// 消息数相同则按工具调用数排序
		return len(filteredTrajectories[i].ToolCalls) > len(filteredTrajectories[j].ToolCalls)
	})

	// 只取前5个
	limit := 5
	if len(filteredTrajectories) < limit {
		limit = len(filteredTrajectories)
	}

	for i := 0; i < limit; i++ {
		t := filteredTrajectories[i]
		topSessions = append(topSessions, TopSession{
			ID:            t.ID,
			Timestamp:     t.Timestamp,
			MessageCount:  len(t.Messages),
			ToolCallCount: len(t.ToolCalls),
			Duration:      t.Duration,
			Rating:        t.UserFeedback,
			Model:         t.ModelUsed,
		})
	}

	return topSessions
}

// generateRecommendations 生成改进建议
func (ie *InsightsEngine) generateRecommendations(startTime time.Time) []Recommendation {
	recommendations := []Recommendation{}

	// 基于工具使用情况生成建议
	if ie.trajectoryManager != nil {
		toolSuccessRates := ie.trajectoryManager.GetToolSuccessRates()
		lowSuccessTools := []string{}
		for tool, rate := range toolSuccessRates {
			if rate < 0.5 {
				lowSuccessTools = append(lowSuccessTools, tool)
			}
		}

		if len(lowSuccessTools) > 0 {
			recommendations = append(recommendations, Recommendation{
				Type:        "tool_usage",
				Title:       "优化低成功率工具",
				Description: "分析显示以下工具的成功率较低：" + strings.Join(lowSuccessTools, ", ") + "。建议优化这些工具的实现或使用方式。",
				Priority:    "high",
			})
		}

		// 基于轨迹数据生成建议
		trajectories, err := ie.trajectoryManager.GetTrajectories()
		if err == nil && len(trajectories) > 0 {
			// 分析平均会话时长
			totalDuration := 0
			for _, t := range trajectories {
				totalDuration += t.Duration
			}
			averageDuration := float64(totalDuration) / float64(len(trajectories))
			if averageDuration > 600 { // 超过10分钟
				recommendations = append(recommendations, Recommendation{
					Type:        "performance",
					Title:       "改善响应速度",
					Description: "分析显示平均会话时长较长（超过10分钟），建议优化工具调用和模型响应时间，提高用户体验。",
					Priority:    "high",
				})
			}

			// 分析模型使用情况
			modelUsage := make(map[string]int)
			for _, t := range trajectories {
				if t.ModelUsed != "" {
					modelUsage[t.ModelUsed]++
				}
			}
			if len(modelUsage) > 1 {
				recommendations = append(recommendations, Recommendation{
					Type:        "model",
					Title:       "优化模型选择策略",
					Description: "系统使用了多种模型，建议为不同类型的任务选择合适的模型，以提高效率和准确性。",
					Priority:    "medium",
				})
			}
		}
	}

	// 基于反馈数据生成建议
	if ie.feedbackCollector != nil {
		ratingDistribution := ie.feedbackCollector.GetRatingDistribution()
		lowRatings := 0
		totalRatings := 0
		for rating, count := range ratingDistribution {
			totalRatings += count
			if rating <= 2 {
				lowRatings += count
			}
		}

		if totalRatings > 0 && float64(lowRatings)/float64(totalRatings) > 0.2 {
			recommendations = append(recommendations, Recommendation{
				Type:        "feedback",
				Title:       "提高服务质量",
				Description: "分析显示低评分比例较高，建议关注用户反馈，提高服务质量。",
				Priority:    "high",
			})
		}

		// 检查反馈收集率
		dailyRatings := ie.feedbackCollector.GetDailyRatings()
		if len(dailyRatings) < 7 {
			recommendations = append(recommendations, Recommendation{
				Type:        "feedback",
				Title:       "增加反馈收集频率",
				Description: "当前反馈收集率较低，建议在更多任务完成时询问用户反馈，以获取更多改进数据。",
				Priority:    "medium",
			})
		}
	}

	// 添加默认建议（如果没有其他建议）
	if len(recommendations) == 0 {
		recommendations = append(recommendations, Recommendation{
			Type:        "general",
			Title:       "持续优化系统性能",
			Description: "建议定期分析系统使用数据，持续优化系统性能和用户体验。",
			Priority:    "low",
		})
	}

	return recommendations
}

// saveReport 保存报告到文件
func (ie *InsightsEngine) saveReport(report *InsightsReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(ie.reportFile, data, 0644)
}

// GetReport 获取最新报告
func (ie *InsightsEngine) GetReport() (*InsightsReport, error) {
	data, err := os.ReadFile(ie.reportFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var report InsightsReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}

	return &report, nil
}

// GenerateSummary 生成摘要
func (ie *InsightsEngine) GenerateSummary(days int) string {
	report := ie.GenerateReport(days)

	var summary strings.Builder
	summary.WriteString("# 系统使用分析报告\n\n")
	summary.WriteString(fmt.Sprintf("**生成时间**: %s\n\n", report.Timestamp.Format("2006-01-02 15:04:05")))

	// 概览
	summary.WriteString("## 概览\n")
	summary.WriteString(fmt.Sprintf("- 总会话数: %d\n", report.Overview.TotalSessions))
	summary.WriteString(fmt.Sprintf("- 总消息数: %d\n", report.Overview.TotalMessages))
	summary.WriteString(fmt.Sprintf("- 总工具调用: %d\n", report.Overview.TotalToolCalls))
	summary.WriteString(fmt.Sprintf("- 总反馈数: %d\n", report.Overview.TotalFeedback))
	summary.WriteString(fmt.Sprintf("- 平均评分: %.1f/5\n", report.Overview.AverageRating))
	summary.WriteString(fmt.Sprintf("- 成功率: %.1f%%\n", report.Overview.SuccessRate*100))
	summary.WriteString(fmt.Sprintf("- 平均会话时长: %.1f 秒\n", report.Overview.AverageSessionLen))
	summary.WriteString(fmt.Sprintf("- 总Token消耗: %d\n\n", report.Overview.TotalTokens))

	// 模型使用
	if len(report.ModelBreakdown.Usage) > 0 {
		summary.WriteString("## 模型使用\n")
		for model, count := range report.ModelBreakdown.Usage {
			summary.WriteString(fmt.Sprintf("- %s: %d 次\n", model, count))
		}
		summary.WriteString("\n")
	}

	// 工具使用
	if len(report.ToolUsage.TopTools) > 0 {
		summary.WriteString("## 工具使用\n")
		for _, tool := range report.ToolUsage.TopTools {
			summary.WriteString(fmt.Sprintf("- %s: %d 次 (成功率: %.1f%%)\n",
				tool.Name, tool.Count, tool.SuccessRate*100))
		}
		summary.WriteString("\n")
	}

	// 反馈统计
	if len(report.FeedbackStats.RatingDistribution) > 0 {
		summary.WriteString("## 反馈统计\n")
		summary.WriteString("### 评分分布\n")
		for rating, count := range report.FeedbackStats.RatingDistribution {
			summary.WriteString(fmt.Sprintf("- %d分: %d 次\n", rating, count))
		}
		summary.WriteString("\n")
	}

	// 改进建议
	if len(report.Recommendations) > 0 {
		summary.WriteString("## 改进建议\n")
		for _, rec := range report.Recommendations {
			summary.WriteString(fmt.Sprintf("**%s** (%s)\n", rec.Title, rec.Priority))
			summary.WriteString(fmt.Sprintf("%s\n\n", rec.Description))
		}
	}

	return summary.String()
}

// ========== 全局实例 ==========
var globalInsightsEngine *InsightsEngine

// InitInsightsEngine 初始化分析引擎
func InitInsightsEngine(dataDir string) {
	if globalInsightsEngine == nil {
		trajectoryManager := GetTrajectoryManager()
		feedbackCollector := GetFeedbackCollector()

		globalInsightsEngine = NewInsightsEngine(dataDir, trajectoryManager, feedbackCollector)
		log.Println("[InsightsEngine] Initialized")
	}
}

// GetInsightsEngine 获取分析引擎
func GetInsightsEngine() *InsightsEngine {
	return globalInsightsEngine
}
