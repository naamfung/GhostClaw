package main

import (
	"log"
	"math"
	"sync"
	"time"
)

// MemoryScorer 记忆评分器
type MemoryScorer struct {
	mu sync.RWMutex

	// 配置
	scoreDecayRate   float64 // 评分衰减率
	maxScore         float64 // 最高评分
	minScore         float64 // 最低评分
	accessWeight     float64 // 访问权重
	recentnessWeight float64 // 新鲜度权重
	relevanceWeight  float64 // 相关性权重
	feedbackWeight   float64 // 反馈权重
}

// NewMemoryScorer 创建新的记忆评分器
func NewMemoryScorer() *MemoryScorer {
	return &MemoryScorer{
		scoreDecayRate:   0.01, // 每天衰减 1%
		maxScore:         1.0,  // 最高评分
		minScore:         0.0,  // 最低评分
		accessWeight:     0.3,  // 访问权重
		recentnessWeight: 0.2,  // 新鲜度权重
		relevanceWeight:  0.3,  // 相关性权重
		feedbackWeight:   0.2,  // 反馈权重
	}
}

// CalculateMemoryScore 计算记忆评分
func (ms *MemoryScorer) CalculateMemoryScore(
	accessCount int,
	lastAccessTime time.Time,
	creationTime time.Time,
	relevanceScore float64,
	feedbackScore float64,
) float64 {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// 1. 访问频率评分
	accessScore := math.Min(1.0, float64(accessCount)/10.0) // 10次访问达到满分

	// 2. 新鲜度评分
	daysSinceAccess := time.Since(lastAccessTime).Hours() / 24
	recentnessScore := math.Max(0.1, 1.0-ms.scoreDecayRate*daysSinceAccess)

	// 3. 相关性评分
	relevanceScore = math.Max(0.0, math.Min(1.0, relevanceScore))

	// 4. 反馈评分
	feedbackScore = math.Max(0.0, math.Min(1.0, feedbackScore))

	// 综合评分
	totalScore := accessScore*ms.accessWeight + recentnessScore*ms.recentnessWeight + relevanceScore*ms.relevanceWeight + feedbackScore*ms.feedbackWeight

	// 归一化到 0-1 范围
	totalScore = math.Max(ms.minScore, math.Min(ms.maxScore, totalScore))

	return totalScore
}

// UpdateMemoryScore 更新记忆评分
func (ms *MemoryScorer) UpdateMemoryScore(memoryID string, relevanceScore float64, feedbackScore float64) error {
	if globalUnifiedMemory == nil || globalDB == nil {
		return nil
	}

	// 从数据库中获取记忆
	var memory Memories
	result := globalDB.First(&memory, "id = ?", memoryID)
	if result.Error != nil {
		// 尝试从 Experiences 表中查找
		var experience Experiences
		result = globalDB.First(&experience, "id = ?", memoryID)
		if result.Error != nil {
			log.Printf("[MemoryScorer] Memory not found: %s", memoryID)
			return nil
		}

		// 更新经验评分
		newScore := ms.CalculateMemoryScore(
			experience.UsedCount,
			experience.UpdatedAt,
			experience.CreatedAt,
			relevanceScore,
			feedbackScore,
		)

		// 更新数据库
		globalDB.Model(&experience).Update("score", newScore)
		log.Printf("[MemoryScorer] Updated score for experience %s to %.2f", memoryID, newScore)
		return nil
	}

	// 计算新评分
	newScore := ms.CalculateMemoryScore(
		memory.AccessCnt,
		memory.UpdatedAt,
		memory.CreatedAt,
		relevanceScore,
		feedbackScore,
	)

	// 更新数据库
	globalDB.Model(&memory).Update("score", newScore)
	log.Printf("[MemoryScorer] Updated score for memory %s to %.2f", memoryID, newScore)
	return nil
}

// BatchUpdateScores 批量更新评分
func (ms *MemoryScorer) BatchUpdateScores() error {
	if globalUnifiedMemory == nil || globalDB == nil {
		return nil
	}

	// 批量更新 Memories 表中的评分
	var memories []Memories
	result := globalDB.Find(&memories)
	if result.Error == nil {
		for _, memory := range memories {
			// 计算新评分（使用默认的相关性和反馈评分）
			newScore := ms.CalculateMemoryScore(
				memory.AccessCnt,
				memory.UpdatedAt,
				memory.CreatedAt,
				0.5, // 默认相关性评分
				0.5, // 默认反馈评分
			)

			// 更新数据库
			globalDB.Model(&memory).Update("score", newScore)
		}
		log.Printf("[MemoryScorer] Batch updated %d memories", len(memories))
	}

	// 批量更新 Experiences 表中的评分
	var experiences []Experiences
	result = globalDB.Find(&experiences)
	if result.Error == nil {
		for _, experience := range experiences {
			// 计算新评分（使用默认的相关性和反馈评分）
			newScore := ms.CalculateMemoryScore(
				experience.UsedCount,
				experience.UpdatedAt,
				experience.CreatedAt,
				0.5, // 默认相关性评分
				0.5, // 默认反馈评分
			)

			// 更新数据库
			globalDB.Model(&experience).Update("score", newScore)
		}
		log.Printf("[MemoryScorer] Batch updated %d experiences", len(experiences))
	}

	log.Println("[MemoryScorer] Batch updated memory scores")
	return nil
}

// GetScoreFactors 获取评分因素
func (ms *MemoryScorer) GetScoreFactors() map[string]float64 {
	return map[string]float64{
		"access_weight":     ms.accessWeight,
		"recentness_weight": ms.recentnessWeight,
		"relevance_weight":  ms.relevanceWeight,
		"feedback_weight":   ms.feedbackWeight,
		"decay_rate":        ms.scoreDecayRate,
	}
}

// SetScoreFactors 设置评分因素
func (ms *MemoryScorer) SetScoreFactors(factors map[string]float64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if val, ok := factors["access_weight"]; ok {
		ms.accessWeight = val
	}
	if val, ok := factors["recentness_weight"]; ok {
		ms.recentnessWeight = val
	}
	if val, ok := factors["relevance_weight"]; ok {
		ms.relevanceWeight = val
	}
	if val, ok := factors["feedback_weight"]; ok {
		ms.feedbackWeight = val
	}
	if val, ok := factors["decay_rate"]; ok {
		ms.scoreDecayRate = val
	}
}

// ========== 全局实例 ==========
var globalMemoryScorer *MemoryScorer

// InitMemoryScorer 初始化记忆评分器
func InitMemoryScorer() {
	if globalMemoryScorer == nil {
		globalMemoryScorer = NewMemoryScorer()
		log.Println("[MemoryScorer] Initialized")
	}
}

// GetMemoryScorer 获取记忆评分器
func GetMemoryScorer() *MemoryScorer {
	return globalMemoryScorer
}
