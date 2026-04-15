package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// SkillQualityReport 技能质量报告
type SkillQualityReport struct {
	SkillName         string    `json:"skill_name"`
	UsageFrequency    float64   `json:"usage_frequency"`    // 使用频率（每天）
	SuccessRate       float64   `json:"success_rate"`       // 成功率
	UserSatisfaction  float64   `json:"user_satisfaction"`  // 用户满意度
	ContextRelevance  float64   `json:"context_relevance"`  // 上下文相关性
	RedundancyScore   float64   `json:"redundancy_score"`   // 冗余度（与其他技能相似度）
	OverallScore      float64   `json:"overall_score"`      // 综合评分 0-1
	Recommendations   []string  `json:"recommendations"`    // 改进建议
}

// SkillSuggestion 技能建议
type SkillSuggestion struct {
	SkillName     string  `json:"skill_name"`
	Reason        string  `json:"reason"`
	Confidence    float64 `json:"confidence"`
	ContextMatch  float64 `json:"context_match"`
}

// CleanupSuggestion 清理建议
type CleanupSuggestion struct {
	SkillName    string  `json:"skill_name"`
	Reason       string  `json:"reason"`
	Action       string  `json:"action"` // "delete", "merge", "improve"
	TargetSkill  string  `json:"target_skill,omitempty"` // 合并目标
}

// SkillEvolutionOptimizer 技能进化优化器
type SkillEvolutionOptimizer struct {
	db *gorm.DB
	mu sync.RWMutex
}

// RecordUsageEvent 记录技能使用事件
func (seo *SkillEvolutionOptimizer) RecordUsageEvent(event SkillUsageEvent) error {
	seo.mu.Lock()
	defer seo.mu.Unlock()

	if err := seo.db.Create(&event).Error; err != nil {
		return fmt.Errorf("failed to record usage event: %w", err)
	}

	// 异步更新质量评分
	go seo.updateQualityScore(event.SkillName)

	return nil
}

// updateQualityScore 更新技能质量评分
func (seo *SkillEvolutionOptimizer) updateQualityScore(skillName string) {
	report, err := seo.EvaluateSkillQuality(skillName)
	if err != nil {
		return
	}

	seo.db.Model(&SkillMeta{}).
		Where("name = ?", skillName).
		Update("quality_score", report.OverallScore)
}

// EvaluateSkillQuality 评估技能质量
func (seo *SkillEvolutionOptimizer) EvaluateSkillQuality(skillName string) (*SkillQualityReport, error) {
	seo.mu.RLock()
	defer seo.mu.RUnlock()

	// 获取使用统计
	var meta SkillMeta
	if result := seo.db.Where("name = ?", skillName).First(&meta); result.Error != nil {
		return nil, fmt.Errorf("skill not found: %s", skillName)
	}

	// 获取使用事件统计（最近一个月）
	var stats struct {
		AvgContextMatch  float64
		AvgSuccessRate   float64
		AvgUserFeedback  float64
		EventCount       int64
	}

	oneMonthAgo := time.Now().AddDate(0, -1, 0).Unix()
	seo.db.Model(&SkillUsageEvent{}).
		Where("skill_name = ? AND timestamp > ?", skillName, oneMonthAgo).
		Select("AVG(context_match) as avg_context_match, AVG(success_rate) as avg_success_rate, AVG(user_feedback) as avg_user_feedback, COUNT(*) as event_count").
		Scan(&stats)

	// 计算使用频率（每天）
	var usageFrequency float64
	if meta.LastUsed > 0 && meta.UseCount > 0 {
		daysSinceFirstUse := float64(time.Now().Unix()-meta.LastUsed) / 86400.0
		if daysSinceFirstUse < 1 {
			daysSinceFirstUse = 1
		}
		usageFrequency = float64(meta.UseCount) / daysSinceFirstUse
	}

	// 计算冗余度（与其他技能的相似度）
	redundancyScore := seo.calculateRedundancy(skillName)

	// 归一化用户反馈到 0-1
	avgUserFeedback := stats.AvgUserFeedback / 5.0

	// 计算综合评分
	// 权重：使用频率 30%，成功率 25%，用户满意度 25%，上下文相关性 20%
	overallScore := 
		usageFrequency*0.30 +
		stats.AvgSuccessRate*0.25 +
		avgUserFeedback*0.25 +
		stats.AvgContextMatch*0.20

	// 如果事件太少，降低置信度
	if stats.EventCount < 5 {
		overallScore *= 0.5
	}

	// 生成改进建议
	recommendations := seo.generateRecommendations(
		usageFrequency, stats.AvgSuccessRate, avgUserFeedback, stats.AvgContextMatch, redundancyScore,
	)

	return &SkillQualityReport{
		SkillName:        skillName,
		UsageFrequency:   usageFrequency,
		SuccessRate:      stats.AvgSuccessRate,
		UserSatisfaction: avgUserFeedback,
		ContextRelevance: stats.AvgContextMatch,
		RedundancyScore:  redundancyScore,
		OverallScore:     math.Min(1.0, overallScore),
		Recommendations:  recommendations,
	}, nil
}

// calculateRedundancy 计算技能冗余度
func (seo *SkillEvolutionOptimizer) calculateRedundancy(skillName string) float64 {
	// 获取当前技能的触发词和标签
	var meta SkillMeta
	if result := seo.db.Where("name = ?", skillName).First(&meta); result.Error != nil {
		return 0
	}

	tags := parseJSONArray(meta.Tags)
	triggers := parseJSONArray(meta.TriggerWords)

	if len(tags) == 0 && len(triggers) == 0 {
		return 0
	}

	// 查找相似技能
	var otherSkills []SkillMeta
	seo.db.Where("name != ?", skillName).Find(&otherSkills)

	var maxSimilarity float64
	for _, other := range otherSkills {
		otherTags := parseJSONArray(other.Tags)
		otherTriggers := parseJSONArray(other.TriggerWords)

		// 计算 Jaccard 相似度
		tagSim := jaccardSimilarity(tags, otherTags)
		triggerSim := jaccardSimilarity(triggers, otherTriggers)

		// 加权平均
		similarity := tagSim*0.6 + triggerSim*0.4
		if similarity > maxSimilarity {
			maxSimilarity = similarity
		}
	}

	return maxSimilarity
}

// jaccardSimilarity 计算 Jaccard 相似度
func jaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	setA := make(map[string]bool)
	for _, item := range a {
		setA[strings.ToLower(item)] = true
	}

	intersection := 0
	for _, item := range b {
		if setA[strings.ToLower(item)] {
			intersection++
		}
	}

	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// generateRecommendations 生成改进建议
func (seo *SkillEvolutionOptimizer) generateRecommendations(
	usageFreq, successRate, userSatisfaction, contextRelevance, redundancy float64,
) []string {
	var recommendations []string

	if usageFreq < 0.1 {
		recommendations = append(recommendations, "使用频率过低，考虑删除或改进触发关键词")
	}
	if successRate < 0.5 {
		recommendations = append(recommendations, "执行成功率较低，需要改进技能逻辑")
	}
	if userSatisfaction < 0.6 {
		recommendations = append(recommendations, "用户满意度不高，考虑优化用户体验")
	}
	if contextRelevance < 0.5 {
		recommendations = append(recommendations, "上下文匹配度低，需要更精确的触发条件")
	}
	if redundancy > 0.7 {
		recommendations = append(recommendations, "与其他技能高度相似，建议合并")
	}

	return recommendations
}

// SuggestSkills 根据上下文推荐技能
func (seo *SkillEvolutionOptimizer) SuggestSkills(context string, topK int) ([]SkillSuggestion, error) {
	seo.mu.RLock()
	defer seo.mu.RUnlock()

	// 获取所有技能
	var skills []SkillMeta
	seo.db.Order("quality_score DESC, use_count DESC").Find(&skills)

	var suggestions []SkillSuggestion
	contextLower := strings.ToLower(context)

	for _, skill := range skills {
		tags := parseJSONArray(skill.Tags)
		triggers := parseJSONArray(skill.TriggerWords)

		// 计算上下文匹配度
		contextMatch := calculateContextMatch(contextLower, skill.Name, skill.DisplayName, skill.Description, tags, triggers)

		// 综合评分
		score := contextMatch*0.4 + skill.QualityScore*0.4 + math.Min(1.0, float64(skill.UseCount)/100.0)*0.2

		if score > 0.3 { // 阈值
			suggestions = append(suggestions, SkillSuggestion{
				SkillName:    skill.Name,
				Reason:       fmt.Sprintf("匹配度: %.0f%%, 质量: %.0f%%", contextMatch*100, skill.QualityScore*100),
				Confidence:   score,
				ContextMatch: contextMatch,
			})
		}
	}

	// 排序并取前 K 个
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Confidence > suggestions[j].Confidence
	})

	if len(suggestions) > topK {
		suggestions = suggestions[:topK]
	}

	return suggestions, nil
}

// calculateContextMatch 计算上下文匹配度
func calculateContextMatch(context string, name, displayName, description string, tags, triggers []string) float64 {
	var matches int
	var totalChecks int

	// 检查名称匹配
	if strings.Contains(context, strings.ToLower(name)) {
		matches += 3
	}
	totalChecks += 3

	// 检查显示名称匹配
	if strings.Contains(context, strings.ToLower(displayName)) {
		matches += 2
	}
	totalChecks += 2

	// 检查描述匹配
	if strings.Contains(context, strings.ToLower(description)) {
		matches += 2
	}
	totalChecks += 2

	// 检查标签匹配
	for _, tag := range tags {
		if strings.Contains(context, strings.ToLower(tag)) {
			matches += 2
			totalChecks += 2
		}
	}

	// 检查触发词匹配
	for _, trigger := range triggers {
		if strings.Contains(context, strings.ToLower(trigger)) {
			matches += 3
			totalChecks += 3
		}
	}

	if totalChecks == 0 {
		return 0
	}

	return float64(matches) / float64(totalChecks)
}

// GenerateCleanupSuggestions 生成清理建议
func (seo *SkillEvolutionOptimizer) GenerateCleanupSuggestions() ([]CleanupSuggestion, error) {
	seo.mu.RLock()
	defer seo.mu.RUnlock()

	var suggestions []CleanupSuggestion

	// 获取所有技能
	var skills []SkillMeta
	seo.db.Find(&skills)

	now := time.Now().Unix()

	for _, skill := range skills {
		// 检查是否需要清理
		daysSinceLastUse := float64(now-skill.LastUsed) / 86400.0

		if skill.UseCount == 0 && daysSinceLastUse > 30 {
			suggestions = append(suggestions, CleanupSuggestion{
				SkillName: skill.Name,
				Reason:    "从未使用且创建超过30天",
				Action:    "delete",
			})
		} else if skill.QualityScore < 0.2 && skill.UseCount > 0 {
			suggestions = append(suggestions, CleanupSuggestion{
				SkillName: skill.Name,
				Reason:    "质量评分过低",
				Action:    "improve",
			})
		} else if daysSinceLastUse > 90 && skill.UseCount < 5 {
			suggestions = append(suggestions, CleanupSuggestion{
				SkillName: skill.Name,
				Reason:    "长期未使用且使用次数极少",
				Action:    "delete",
			})
		}
	}

	return suggestions, nil
}

// AutoTagSkill 自动为技能生成标签
func (seo *SkillEvolutionOptimizer) AutoTagSkill(skillName string) ([]string, error) {
	// 获取技能内容
	var meta SkillMeta
	if result := seo.db.Where("name = ?", skillName).First(&meta); result.Error != nil {
		return nil, result.Error
	}

	// 简单的关键词提取
	suggestedTags := extractKeywords(meta.Description)

	return suggestedTags, nil
}

// extractKeywords 提取关键词
func extractKeywords(text string) []string {
	// 简单的关键词提取规则
	keywords := make(map[string]bool)
	
	// 常见技术关键词
	techKeywords := []string{
		"api", "web", "database", "sql", "http", "json", "xml",
		"python", "go", "javascript", "java", "cpp", "rust",
		"docker", "kubernetes", "aws", "azure", "gcp",
		"linux", "windows", "macos", "bash", "shell",
		"git", "github", "ci/cd", "devops", "testing",
		"frontend", "backend", "fullstack", "mobile", "desktop",
	}

	textLower := strings.ToLower(text)
	for _, keyword := range techKeywords {
		if strings.Contains(textLower, keyword) {
			keywords[keyword] = true
		}
	}

	// 转换为切片
	var result []string
	for keyword := range keywords {
		result = append(result, keyword)
	}

	return result
}

// GetSkillStats 获取技能统计信息
func (seo *SkillEvolutionOptimizer) GetSkillStats() (map[string]interface{}, error) {
	seo.mu.RLock()
	defer seo.mu.RUnlock()

	stats := make(map[string]interface{})

	// 总技能数
	var totalSkills int64
	seo.db.Model(&SkillMeta{}).Count(&totalSkills)
	stats["total_skills"] = totalSkills

	// 总使用次数
	var totalUsage int64
	seo.db.Model(&SkillMeta{}).Select("SUM(use_count)").Scan(&totalUsage)
	stats["total_usage"] = totalUsage

	// 平均质量评分
	var avgQuality float64
	seo.db.Model(&SkillMeta{}).Select("AVG(quality_score)").Scan(&avgQuality)
	stats["average_quality"] = avgQuality

	// 热门技能（使用次数最多）
	var topSkills []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	seo.db.Model(&SkillMeta{}).
		Select("name, use_count as count").
		Order("use_count DESC").
		Limit(10).
		Scan(&topSkills)
	stats["top_skills"] = topSkills

	// 最近7天使用事件数
	var recentEvents int64
	sevenDaysAgo := time.Now().AddDate(0, 0, -7).Unix()
	seo.db.Model(&SkillUsageEvent{}).Where("timestamp > ?", sevenDaysAgo).Count(&recentEvents)
	stats["recent_events_7d"] = recentEvents

	return stats, nil
}
