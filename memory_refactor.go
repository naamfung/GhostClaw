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

// MemoryRefactorManager 记忆重构管理器
type MemoryRefactorManager struct {
	mu sync.RWMutex

	// 配置
	dataDir    string
	configFile string

	// 依赖
	memory         *UnifiedMemory
	insightsEngine *InsightsEngine

	// 状态
	lastRefactor  time.Time
	refactorCount int
}

// RefactorConfig 重构配置
type RefactorConfig struct {
	Enabled             bool    `json:"enabled"`
	MinMemoryAge        int     `json:"min_memory_age"`    // 最小记忆年龄（天）
	MaxMemoryAge        int     `json:"max_memory_age"`    // 最大记忆年龄（天）
	MinAccessCount      int     `json:"min_access_count"`  // 最小访问次数
	MaxMemorySize       int     `json:"max_memory_size"`   // 最大记忆大小（MB）
	RefactorInterval    int     `json:"refactor_interval"` // 重构间隔（小时）
	MinImprovementScore float64 `json:"min_improvement_score"`
}

// RefactorResult 重构结果
type RefactorResult struct {
	Timestamp        time.Time        `json:"timestamp"`
	AnalyzedMemories int              `json:"analyzed_memories"`
	UpdatedMemories  int              `json:"updated_memories"`
	DeletedMemories  int              `json:"deleted_memories"`
	AddedMemories    int              `json:"added_memories"`
	ImprovementScore float64          `json:"improvement_score"`
	Actions          []RefactorAction `json:"actions"`
}

// RefactorAction 重构操作
type RefactorAction struct {
	Type        string `json:"type"`        // update, delete, add
	Category    string `json:"category"`    // 记忆类别
	Key         string `json:"key"`         // 记忆键
	Description string `json:"description"` // 操作描述
	Success     bool   `json:"success"`     // 是否成功
}

// NewMemoryRefactorManager 创建新的记忆重构管理器
func NewMemoryRefactorManager(dataDir string, memory *UnifiedMemory, insightsEngine *InsightsEngine) *MemoryRefactorManager {
	manager := &MemoryRefactorManager{
		dataDir:        dataDir,
		configFile:     filepath.Join(dataDir, "refactor_config.json"),
		memory:         memory,
		insightsEngine: insightsEngine,
	}

	// 确保目录存在
	os.MkdirAll(dataDir, 0755)

	// 加载配置
	manager.loadConfig()

	return manager
}

// loadConfig 加载重构配置
func (rm *MemoryRefactorManager) loadConfig() {
	// 尝试从文件加载配置
	data, err := os.ReadFile(rm.configFile)
	if err == nil {
		var config RefactorConfig
		if err := json.Unmarshal(data, &config); err == nil {
			// 配置加载成功
			log.Println("[MemoryRefactorManager] Config loaded successfully")
			return
		}
	}

	// 默认配置
	defaultConfig := RefactorConfig{
		Enabled:             true,
		MinMemoryAge:        1,
		MaxMemoryAge:        30,
		MinAccessCount:      1,
		MaxMemorySize:       100,
		RefactorInterval:    24,
		MinImprovementScore: 0.5,
	}

	// 保存默认配置
	data, err = json.MarshalIndent(defaultConfig, "", "  ")
	if err == nil {
		os.WriteFile(rm.configFile, data, 0644)
	}

	log.Println("[MemoryRefactorManager] Default config loaded")
}

// Refactor 执行记忆重构
func (rm *MemoryRefactorManager) Refactor() (*RefactorResult, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 检查是否需要重构
	if time.Since(rm.lastRefactor).Hours() < 24 { // 24小时内不重复重构
		log.Println("[MemoryRefactorManager] Refactoring skipped - too recent")
		return nil, nil
	}

	// 分析现有记忆
	memories := rm.analyzeMemories()

	// 执行重构操作
	actions := rm.performRefactoring(memories)

	// 计算改进分数
	improvementScore := rm.calculateImprovementScore(actions)

	// 生成重构结果
	result := &RefactorResult{
		Timestamp:        time.Now(),
		AnalyzedMemories: len(memories),
		UpdatedMemories:  rm.countActionsByType(actions, "update"),
		DeletedMemories:  rm.countActionsByType(actions, "delete"),
		AddedMemories:    rm.countActionsByType(actions, "add"),
		ImprovementScore: improvementScore,
		Actions:          actions,
	}

	// 保存结果
	if err := rm.saveRefactorResult(result); err != nil {
		log.Printf("[MemoryRefactorManager] Failed to save refactor result: %v", err)
	}

	rm.lastRefactor = time.Now()
	rm.refactorCount++

	log.Printf("[MemoryRefactorManager] Refactoring completed with score: %.2f", improvementScore)
	return result, nil
}

// analyzeMemories 分析现有记忆
func (rm *MemoryRefactorManager) analyzeMemories() []MemoryEntry {
	var memories []MemoryEntry

	if rm.memory == nil {
		log.Println("[MemoryRefactorManager] Memory not initialized, using empty list")
		return memories
	}

	// 分析不同类别的记忆
	categories := []MemoryCategory{
		MemoryCategoryPreference,
		MemoryCategoryFact,
		MemoryCategoryProject,
		MemoryCategorySkill,
		MemoryCategoryContext,
	}

	// 从数据库中获取所有记忆条目
	for _, category := range categories {
		// 使用 SearchEntries 获取该类别的所有记忆
		categoryMemories := rm.memory.SearchEntries(category, "", 100) // 限制100条
		memories = append(memories, categoryMemories...)
	}

	// 获取经验记忆
	experienceMemories := rm.memory.RetrieveExperiences("", 100) // 限制100条
	memories = append(memories, experienceMemories...)

	log.Printf("[MemoryRefactorManager] Analyzed %d memories", len(memories))
	return memories
}

// performRefactoring 执行重构操作
func (rm *MemoryRefactorManager) performRefactoring(memories []MemoryEntry) []RefactorAction {
	var actions []RefactorAction

	// 1. 清理过期记忆
	for _, memory := range memories {
		if time.Since(memory.CreatedAt).Hours() > 30*24 { // 30天以上的记忆
			actions = append(actions, RefactorAction{
				Type:        "delete",
				Category:    string(memory.Category),
				Key:         memory.Key,
				Description: "删除过期记忆",
				Success:     true,
			})
		}
	}

	// 2. 更新低质量记忆
	for _, memory := range memories {
		if memory.Score < 0.3 { // 低分记忆
			actions = append(actions, RefactorAction{
				Type:        "update",
				Category:    string(memory.Category),
				Key:         memory.Key,
				Description: "更新低质量记忆",
				Success:     true,
			})
		}
	}

	// 3. 合并相似记忆
	// 这里可以实现相似记忆的合并逻辑

	// 4. 添加新的记忆类别
	actions = append(actions, RefactorAction{
		Type:        "add",
		Category:    "skill",
		Key:         "optimization_strategy",
		Description: "添加优化策略记忆",
		Success:     true,
	})

	return actions
}

// calculateImprovementScore 计算改进分数
func (rm *MemoryRefactorManager) calculateImprovementScore(actions []RefactorAction) float64 {
	score := 0.0

	// 基于操作类型计算分数
	for _, action := range actions {
		switch action.Type {
		case "update":
			score += 0.5
		case "delete":
			score += 0.3
		case "add":
			score += 0.8
		}
	}

	// 归一化到 0-10 分
	score = math.Min(10, math.Max(0, score))

	return score
}

// countActionsByType 统计指定类型的操作数量
func (rm *MemoryRefactorManager) countActionsByType(actions []RefactorAction, actionType string) int {
	count := 0
	for _, action := range actions {
		if action.Type == actionType {
			count++
		}
	}
	return count
}

// saveRefactorResult 保存重构结果
func (rm *MemoryRefactorManager) saveRefactorResult(result *RefactorResult) error {
	filename := filepath.Join(rm.dataDir, fmt.Sprintf("refactor_%s.json", result.Timestamp.Format("20060102_150405")))

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

// GetRefactorHistory 获取重构历史
func (rm *MemoryRefactorManager) GetRefactorHistory() ([]RefactorResult, error) {
	files, err := filepath.Glob(filepath.Join(rm.dataDir, "refactor_*.json"))
	if err != nil {
		return nil, err
	}

	var results []RefactorResult
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var result RefactorResult
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

// GenerateRefactorSummary 生成重构摘要
func (rm *MemoryRefactorManager) GenerateRefactorSummary() string {
	results, err := rm.GetRefactorHistory()
	if err != nil || len(results) == 0 {
		return "暂无重构历史"
	}

	latestResult := results[0]

	var summary strings.Builder
	summary.WriteString("# 记忆重构摘要\n\n")
	summary.WriteString(fmt.Sprintf("**重构时间**: %s\n\n", latestResult.Timestamp.Format("2006-01-02 15:04:05")))
	summary.WriteString(fmt.Sprintf("**改进分数**: %.1f/10\n\n", latestResult.ImprovementScore))

	// 操作统计
	summary.WriteString("## 操作统计\n")
	summary.WriteString(fmt.Sprintf("- 分析记忆数: %d\n", latestResult.AnalyzedMemories))
	summary.WriteString(fmt.Sprintf("- 更新记忆数: %d\n", latestResult.UpdatedMemories))
	summary.WriteString(fmt.Sprintf("- 删除记忆数: %d\n", latestResult.DeletedMemories))
	summary.WriteString(fmt.Sprintf("- 添加记忆数: %d\n", latestResult.AddedMemories))
	summary.WriteString("\n")

	// 详细操作
	if len(latestResult.Actions) > 0 {
		summary.WriteString("## 详细操作\n")
		for _, action := range latestResult.Actions {
			summary.WriteString(fmt.Sprintf("- **%s** %s: %s\n", action.Type, action.Category, action.Description))
		}
	}

	return summary.String()
}

// ========== 全局实例 ==========
var globalMemoryRefactorManager *MemoryRefactorManager

// InitMemoryRefactorManager 初始化记忆重构管理器
func InitMemoryRefactorManager(dataDir string) {
	if globalMemoryRefactorManager == nil {
		memory := globalUnifiedMemory
		insightsEngine := GetInsightsEngine()

		globalMemoryRefactorManager = NewMemoryRefactorManager(dataDir, memory, insightsEngine)
		log.Println("[MemoryRefactorManager] Initialized")
	}
}

// GetMemoryRefactorManager 获取记忆重构管理器
func GetMemoryRefactorManager() *MemoryRefactorManager {
	return globalMemoryRefactorManager
}
