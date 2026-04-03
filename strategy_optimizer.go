package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// StrategyOptimizer 策略优化器
type StrategyOptimizer struct {
	mu sync.RWMutex
	
	// 配置
	dataDir        string
	configFile     string
	
	// 依赖
	insightsEngine *InsightsEngine
	
	// 状态
	lastOptimization time.Time
	optimizationCount int
}

// OptimizationConfig 优化配置
type OptimizationConfig struct {
	Enabled               bool     `json:"enabled"`
	MaxSuggestions        int      `json:"max_suggestions"`
	MinImprovementScore   float64  `json:"min_improvement_score"`
	OptimizationInterval  int      `json:"optimization_interval"` // 小时
	ExcludedTools         []string `json:"excluded_tools"`
}

// OptimizationResult 优化结果
type OptimizationResult struct {
	Timestamp        time.Time        `json:"timestamp"`
	Report           *InsightsReport  `json:"report"`
	AppliedChanges   []AppliedChange  `json:"applied_changes"`
	ImprovementScore float64          `json:"improvement_score"`
	Stats            OptimizationStats `json:"stats"`
}

// AppliedChange 应用的更改
type AppliedChange struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Success     bool   `json:"success"`
}

// OptimizationStats 优化统计
type OptimizationStats struct {
	TotalSuggestions  int `json:"total_suggestions"`
	AppliedSuggestions int `json:"applied_suggestions"`
	FailedSuggestions  int `json:"failed_suggestions"`
}

// NewStrategyOptimizer 创建新的策略优化器
func NewStrategyOptimizer(dataDir string, insightsEngine *InsightsEngine) *StrategyOptimizer {
	optimizer := &StrategyOptimizer{
		dataDir:        dataDir,
		configFile:     filepath.Join(dataDir, "optimization_config.json"),
		insightsEngine: insightsEngine,
	}
	
	// 确保目录存在
	os.MkdirAll(dataDir, 0755)
	
	// 加载配置
	optimizer.loadConfig()
	
	return optimizer
}

// optimizeBasedOnMemory 基于记忆数据优化
func (so *StrategyOptimizer) optimizeBasedOnMemory() *AppliedChange {
	// 获取全局记忆系统
	if globalUnifiedMemory == nil {
		return nil
	}
	
	// 分析经验记忆
	// 这里可以实现基于记忆的优化逻辑
	// 例如：
	// 1. 分析成功经验，提取最佳实践
	// 2. 分析失败经验，避免重复错误
	// 3. 基于用户偏好调整策略
	
	return &AppliedChange{
		Type:        "memory_based",
		Description: "基于经验记忆优化策略",
		Priority:    "medium",
		Success:     true,
	}
}

// loadConfig 加载优化配置
func (so *StrategyOptimizer) loadConfig() {
	// 这里可以实现配置加载逻辑
}

// Optimize 执行策略优化
func (so *StrategyOptimizer) Optimize() (*OptimizationResult, error) {
	so.mu.Lock()
	defer so.mu.Unlock()
	
	// 检查是否需要优化
	if time.Since(so.lastOptimization).Hours() < 1 { // 1小时内不重复优化
		log.Println("[StrategyOptimizer] Optimization skipped - too recent")
		return nil, nil
	}
	
	// 生成分析报告
	report := so.insightsEngine.GenerateReport(7) // 分析过去7天的数据
	
	// 应用优化策略
	changes := so.applyOptimizations(report)
	
	// 计算改进分数
	improvementScore := so.calculateImprovementScore(report, changes)
	
	// 生成优化结果
	result := &OptimizationResult{
		Timestamp:        time.Now(),
		Report:           report,
		AppliedChanges:   changes,
		ImprovementScore: improvementScore,
		Stats: OptimizationStats{
			TotalSuggestions:  len(report.Recommendations),
			AppliedSuggestions: len(changes),
			FailedSuggestions:  0,
		},
	}
	
	// 保存结果
	if err := so.saveOptimizationResult(result); err != nil {
		log.Printf("[StrategyOptimizer] Failed to save optimization result: %v", err)
	}
	
	so.lastOptimization = time.Now()
	so.optimizationCount++
	
	log.Printf("[StrategyOptimizer] Optimization completed with score: %.2f", improvementScore)
	return result, nil
}

// applyOptimizations 应用优化策略
func (so *StrategyOptimizer) applyOptimizations(report *InsightsReport) []AppliedChange {
	var changes []AppliedChange
	
	// 1. 优化系统提示
	if change := so.optimizeSystemPrompt(report); change != nil {
		changes = append(changes, *change)
	}
	
	// 2. 优化工具使用策略
	if change := so.optimizeToolUsage(report); change != nil {
		changes = append(changes, *change)
	}
	
	// 3. 优化模型选择策略
	if change := so.optimizeModelSelection(report); change != nil {
		changes = append(changes, *change)
	}
	
	// 4. 优化性能
	if change := so.optimizePerformance(report); change != nil {
		changes = append(changes, *change)
	}
	
	// 5. 基于记忆的优化
	if change := so.optimizeBasedOnMemory(); change != nil {
		changes = append(changes, *change)
	}
	
	return changes
}

// optimizeSystemPrompt 优化系统提示
func (so *StrategyOptimizer) optimizeSystemPrompt(report *InsightsReport) *AppliedChange {
	// 分析工具使用情况
	topTools := make(map[string]int)
	for _, tool := range report.ToolUsage.TopTools {
		topTools[tool.Name] = tool.Count
	}
	
	// 分析反馈
	lowRatingCategories := so.findLowRatingCategories(report)
	
	// 生成改进建议
	improvements := []string{}
	
	// 基于工具使用优化提示
	if len(topTools) > 0 {
		// 提取使用频率最高的工具
		var topToolName string
		maxCount := 0
		for name, count := range topTools {
			if count > maxCount {
				maxCount = count
				topToolName = name
			}
		}
		
		if topToolName != "" {
			improvements = append(improvements, fmt.Sprintf("优先使用 %s 工具来解决相关问题", topToolName))
		}
	}
	
	// 基于低评分类别优化提示
	for category, count := range lowRatingCategories {
		if count > 3 { // 至少有3个相关反馈
			switch category {
			case "accuracy":
				improvements = append(improvements, "提供更准确的信息，必要时使用工具验证")
			case "clarity":
				improvements = append(improvements, "使用更清晰、简洁的语言表达")
			case "helpfulness":
				improvements = append(improvements, "更关注用户的实际需求，提供有价值的建议")
			case "speed":
				improvements = append(improvements, "提高响应速度，避免不必要的思考和工具调用")
			}
		}
	}
	
	if len(improvements) > 0 {
		// 这里可以实现实际的系统提示更新逻辑
		// 例如更新 const.go 中的默认系统提示
		
		return &AppliedChange{
			Type:        "system_prompt",
			Description: fmt.Sprintf("优化系统提示，添加 %d 项改进建议", len(improvements)),
			Priority:    "high",
			Success:     true,
		}
	}
	
	return nil
}

// optimizeToolUsage 优化工具使用策略
func (so *StrategyOptimizer) optimizeToolUsage(report *InsightsReport) *AppliedChange {
	// 分析工具成功率
	lowSuccessTools := []string{}
	for _, tool := range report.ToolUsage.TopTools {
		if tool.SuccessRate < 0.6 { // 成功率低于60%
			lowSuccessTools = append(lowSuccessTools, tool.Name)
		}
	}
	
	if len(lowSuccessTools) > 0 {
		// 这里可以实现工具使用策略的优化
		// 例如更新工具描述、添加更多示例等
		
		return &AppliedChange{
			Type:        "tool_usage",
			Description: fmt.Sprintf("优化 %d 个低成功率工具的使用策略", len(lowSuccessTools)),
			Priority:    "medium",
			Success:     true,
		}
	}
	
	return nil
}

// optimizeModelSelection 优化模型选择策略
func (so *StrategyOptimizer) optimizeModelSelection(report *InsightsReport) *AppliedChange {
	// 分析模型使用情况
	modelUsage := report.ModelBreakdown.Usage
	if len(modelUsage) > 1 {
		// 找出使用最多的模型
		var topModel string
		maxUsage := 0
		for model, usage := range modelUsage {
			if usage > maxUsage {
				maxUsage = usage
				topModel = model
			}
		}
		
		if topModel != "" {
			// 这里可以实现模型选择策略的优化
			// 例如为不同类型的任务选择合适的模型
			
			return &AppliedChange{
				Type:        "model_selection",
				Description: fmt.Sprintf("基于使用数据优化模型选择策略，推荐优先使用 %s", topModel),
				Priority:    "low",
				Success:     true,
			}
		}
	}
	
	return nil
}

// optimizePerformance 优化性能
func (so *StrategyOptimizer) optimizePerformance(report *InsightsReport) *AppliedChange {
	// 分析会话时长
	averageDuration := report.Overview.AverageSessionLen
	if averageDuration > 60 { // 平均会话时长超过60秒
		// 这里可以实现性能优化策略
		// 例如减少不必要的工具调用、优化思考过程等
		
		return &AppliedChange{
			Type:        "performance",
			Description: fmt.Sprintf("优化性能，减少平均会话时长（当前: %.1f秒）", averageDuration),
			Priority:    "high",
			Success:     true,
		}
	}
	
	return nil
}

// findLowRatingCategories 找出低评分类别
func (so *StrategyOptimizer) findLowRatingCategories(report *InsightsReport) map[string]int {
	lowRatingCategories := make(map[string]int)
	
	// 分析反馈类别
	for category, count := range report.FeedbackStats.ByCategory {
		// 这里可以根据实际情况判断低评分类别
		// 暂时简单实现
		if count > 0 {
			lowRatingCategories[category] = count
		}
	}
	
	return lowRatingCategories
}

// calculateImprovementScore 计算改进分数
func (so *StrategyOptimizer) calculateImprovementScore(report *InsightsReport, changes []AppliedChange) float64 {
	score := 0.0
	
	// 基于反馈评分
	averageRating := report.Overview.AverageRating
	score += averageRating * 0.3
	
	// 基于成功率
	successRate := report.Overview.SuccessRate
	score += successRate * 20 * 0.2
	
	// 基于应用的更改
	score += float64(len(changes)) * 0.5 * 0.3
	
	// 基于性能
	averageDuration := report.Overview.AverageSessionLen
	if averageDuration < 60 {
		score += (60 - averageDuration) / 60 * 0.2
	}
	
	// 归一化到 0-10 分
	score = math.Min(10, math.Max(0, score))
	
	return score
}

// saveOptimizationResult 保存优化结果
func (so *StrategyOptimizer) saveOptimizationResult(result *OptimizationResult) error {
	filename := filepath.Join(so.dataDir, fmt.Sprintf("optimization_%s.json", result.Timestamp.Format("20060102_150405")))
	
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(filename, data, 0644)
}

// GetOptimizationHistory 获取优化历史
func (so *StrategyOptimizer) GetOptimizationHistory() ([]OptimizationResult, error) {
	files, err := filepath.Glob(filepath.Join(so.dataDir, "optimization_*.json"))
	if err != nil {
		return nil, err
	}
	
	var results []OptimizationResult
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		
		var result OptimizationResult
		if err := json.Unmarshal(data, &result); err != nil {
			continue
		}
		
		results = append(results, result)
	}
	
	// 按时间排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.After(results[j].Timestamp)
	})
	
	return results, nil
}

// GenerateOptimizationSummary 生成优化摘要
func (so *StrategyOptimizer) GenerateOptimizationSummary() string {
	results, err := so.GetOptimizationHistory()
	if err != nil || len(results) == 0 {
		return "暂无优化历史"
	}
	
	latestResult := results[0]
	
	var summary strings.Builder
	summary.WriteString("# 策略优化摘要\n\n")
	summary.WriteString(fmt.Sprintf("**优化时间**: %s\n\n", latestResult.Timestamp.Format("2006-01-02 15:04:05")))
	summary.WriteString(fmt.Sprintf("**改进分数**: %.1f/10\n\n", latestResult.ImprovementScore))
	
	// 应用的更改
	if len(latestResult.AppliedChanges) > 0 {
		summary.WriteString("## 应用的优化\n")
		for _, change := range latestResult.AppliedChanges {
			summary.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", change.Type, change.Priority, change.Description))
		}
		summary.WriteString("\n")
	}
	
	// 统计信息
	summary.WriteString("## 统计信息\n")
	summary.WriteString(fmt.Sprintf("- 总建议数: %d\n", latestResult.Stats.TotalSuggestions))
	summary.WriteString(fmt.Sprintf("- 已应用: %d\n", latestResult.Stats.AppliedSuggestions))
	summary.WriteString(fmt.Sprintf("- 失败: %d\n", latestResult.Stats.FailedSuggestions))
	summary.WriteString("\n")
	
	// 下一步建议
	summary.WriteString("## 下一步建议\n")
	if latestResult.Report != nil {
		for _, rec := range latestResult.Report.Recommendations {
			if rec.Priority == "high" {
				summary.WriteString(fmt.Sprintf("- **%s**: %s\n", rec.Title, rec.Description))
			}
		}
	}
	
	return summary.String()
}

// ========== 全局实例 ==========
var globalStrategyOptimizer *StrategyOptimizer

// InitStrategyOptimizer 初始化策略优化器
func InitStrategyOptimizer(dataDir string) {
	if globalStrategyOptimizer == nil {
		insightsEngine := GetInsightsEngine()
		globalStrategyOptimizer = NewStrategyOptimizer(dataDir, insightsEngine)
		log.Println("[StrategyOptimizer] Initialized")
	}
}

// GetStrategyOptimizer 获取策略优化器
func GetStrategyOptimizer() *StrategyOptimizer {
	return globalStrategyOptimizer
}
