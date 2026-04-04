package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SkillMeta 技能元数据（不含完整内容）
type SkillMeta struct {
	ID           uint      `gorm:"primaryKey" json:"-"`
	Name         string    `gorm:"uniqueIndex;not null" json:"name"`
	DisplayName  string    `json:"display_name"`
	Description  string    `json:"description"` // 摘要，非完整内容
	Tags         string    `json:"tags"`        // JSON array as string
	TriggerWords string    `json:"trigger_words"` // JSON array as string
	FilePath     string    `json:"-"`
	FileSize     int64     `json:"file_size"`
	ModTime      int64     `json:"mod_time"`      // Unix timestamp
	UseCount     int       `json:"use_count"`
	LastUsed     int64     `json:"last_used"`     // Unix timestamp
	QualityScore float64   `json:"quality_score"`
	ContentHash  string    `json:"-"`
	CreatedAt    int64     `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    int64     `gorm:"autoUpdateTime" json:"updated_at"`
}

// SkillUsageEvent 技能使用事件
type SkillUsageEvent struct {
	ID           uint    `gorm:"primaryKey" json:"-"`
	SkillName    string  `gorm:"index" json:"skill_name"`
	SessionID    string  `json:"session_id"`
	Timestamp    int64   `gorm:"index" json:"timestamp"` // Unix timestamp
	ContextMatch float64 `json:"context_match"`
	UserFeedback int     `json:"user_feedback"` // 1-5星
	SuccessRate  float64 `json:"success_rate"`
	TokensSaved  int     `json:"tokens_saved"`
}

// SkillListRequest 技能列表查询请求
type SkillListRequest struct {
	Page        int      `json:"page"`        // 页码，从1开始
	PageSize    int      `json:"page_size"`   // 每页数量，默认20，最大100
	Tags        []string `json:"tags"`        // 标签过滤
	Triggers    []string `json:"triggers"`    // 触发词过滤
	Search      string   `json:"search"`      // 全文搜索
	SortBy      string   `json:"sort_by"`     // 排序字段：name, usage, quality, last_used
	SortOrder   string   `json:"sort_order"`  // 排序方向：asc, desc
	Context     string   `json:"context"`     // 当前上下文，用于智能排序
	SuggestOnly bool     `json:"suggest_only"` // 只返回推荐技能
}

// SkillListResponse 技能列表响应
type SkillListResponse struct {
	Total       int         `json:"total"`
	Page        int         `json:"page"`
	PageSize    int         `json:"page_size"`
	TotalPages  int         `json:"total_pages"`
	Skills      []SkillMeta `json:"skills"`
	Suggestions []string    `json:"suggestions,omitempty"`
}

// SkillManagerV2 新的技能管理器
type SkillManagerV2 struct {
	db        *gorm.DB
	cache     *lru.Cache[string, *Skill]
	skillsDir string
	mu        sync.RWMutex
}

// NewSkillManagerV2 创建新的技能管理器
func NewSkillManagerV2(skillsDir string, cacheSize int) (*SkillManagerV2, error) {
	// 确保目录存在
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create skills directory: %w", err)
	}

	// 创建或打开 SQLite 数据库
	dbPath := filepath.Join(skillsDir, ".skills_meta.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 自动迁移表结构
	if err := db.AutoMigrate(&SkillMeta{}, &SkillUsageEvent{}); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	// 创建 LRU 缓存
	cache, err := lru.New[string, *Skill](cacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}

	sm := &SkillManagerV2{
		db:        db,
		cache:     cache,
		skillsDir: skillsDir,
	}

	// 初始扫描和索引
	if err := sm.RebuildIndex(); err != nil {
		return nil, fmt.Errorf("failed to build initial index: %w", err)
	}

	return sm, nil
}

// RebuildIndex 重建索引
func (sm *SkillManagerV2) RebuildIndex() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 递归扫描目录，收集所有技能文件
	var skillFiles []string
	err := filepath.Walk(sm.skillsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			// 支持两种格式：
			// 1. 新格式：子目录中的 skill.md
			// 2. 旧格式：直接在 skills 目录下的 *.md 文件
			skillFiles = append(skillFiles, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk skills directory: %w", err)
	}

	// 收集当前文件
	currentFiles := make(map[string]bool)
	for _, filePath := range skillFiles {
		currentFiles[filepath.Base(filePath)] = true
	}

	// 获取数据库中已有的技能
	var existingSkills []SkillMeta
	sm.db.Find(&existingSkills)

	// 删除不存在的技能
	for _, skill := range existingSkills {
		if _, exists := currentFiles[filepath.Base(skill.FilePath)]; !exists {
			sm.db.Delete(&skill)
		}
	}

	// 更新或添加新技能
	for _, filePath := range skillFiles {
		if err := sm.indexSkillFile(filePath); err != nil {
			fmt.Printf("Warning: failed to index skill %s: %v\n", filePath, err)
		}
	}

	return nil
}

// indexSkillFile 索引单个技能文件
func (sm *SkillManagerV2) indexSkillFile(filePath string) error {
	// 读取文件
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	// 解析技能
	skill, err := parseSkillContent(string(content), filePath)
	if err != nil {
		return err
	}

	// 获取文件信息
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	// 计算内容哈希
	hash := fnv.New64a()
	hash.Write(content)
	contentHash := fmt.Sprintf("%x", hash.Sum64())

	// 检查是否需要更新
	var existing SkillMeta
	result := sm.db.Where("name = ?", skill.Name).First(&existing)
	if result.Error == nil && existing.ContentHash == contentHash {
		// 内容未变化，跳过
		return nil
	}

	// 序列化数组
	tagsJSON := "[]"
	if len(skill.Tags) > 0 {
		tagsJSON = fmt.Sprintf("[\"%s\"]", strings.Join(skill.Tags, "\",\""))
	}
	triggersJSON := "[]"
	if len(skill.TriggerWords) > 0 {
		triggersJSON = fmt.Sprintf("[\"%s\"]", strings.Join(skill.TriggerWords, "\",\""))
	}

	// 生成描述摘要（前200字符）
	descSummary := skill.Description
	if len(descSummary) > 200 {
		descSummary = descSummary[:200] + "..."
	}

	// 插入或更新
	meta := SkillMeta{
		Name:         skill.Name,
		DisplayName:  skill.DisplayName,
		Description:  descSummary,
		Tags:         tagsJSON,
		TriggerWords: triggersJSON,
		FilePath:     filePath,
		FileSize:     info.Size(),
		ModTime:      info.ModTime().Unix(),
		ContentHash:  contentHash,
	}

	if result.Error == nil {
		// 更新
		meta.ID = existing.ID
		meta.UseCount = existing.UseCount
		meta.LastUsed = existing.LastUsed
		meta.QualityScore = existing.QualityScore
	}

	return sm.db.Save(&meta).Error
}

// ListSkills 查询技能列表（支持分页、过滤、搜索）
func (sm *SkillManagerV2) ListSkills(req SkillListRequest) (*SkillListResponse, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// 设置默认值
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}
	if req.SortBy == "" {
		req.SortBy = "name"
	}
	if req.SortOrder == "" {
		req.SortOrder = "asc"
	}

	// 构建查询
	query := sm.db.Model(&SkillMeta{})

	// 标签过滤
	if len(req.Tags) > 0 {
		for _, tag := range req.Tags {
			query = query.Where("tags LIKE ?", "%\""+tag+"\"%")
		}
	}

	// 触发词过滤
	if len(req.Triggers) > 0 {
		for _, trigger := range req.Triggers {
			query = query.Where("trigger_words LIKE ?", "%\""+trigger+"\"%")
		}
	}

	// 全文搜索
	if req.Search != "" {
		searchPattern := "%" + req.Search + "%"
		query = query.Where("name LIKE ? OR display_name LIKE ? OR description LIKE ?", 
			searchPattern, searchPattern, searchPattern)
	}

	// 查询总数
	var total int64
	query.Count(&total)

	// 排序
	orderColumn := "name"
	switch req.SortBy {
	case "usage":
		orderColumn = "use_count"
	case "quality":
		orderColumn = "quality_score"
	case "last_used":
		orderColumn = "last_used"
	}
	
	orderDirection := "ASC"
	if strings.ToLower(req.SortOrder) == "desc" {
		orderDirection = "DESC"
	}

	// 查询数据
	var skills []SkillMeta
	offset := (req.Page - 1) * req.PageSize
	query.Order(orderColumn + " " + orderDirection).
		Limit(req.PageSize).
		Offset(offset).
		Find(&skills)

	totalPages := int((total + int64(req.PageSize) - 1) / int64(req.PageSize))

	return &SkillListResponse{
		Total:      int(total),
		Page:       req.Page,
		PageSize:   req.PageSize,
		TotalPages: totalPages,
		Skills:     skills,
	}, nil
}

// GetSkill 获取技能（带缓存）
func (sm *SkillManagerV2) GetSkill(name string) (*Skill, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// 先查缓存
	if skill, ok := sm.cache.Get(name); ok {
		// 更新使用统计（异步）
		go sm.recordUsage(name)
		return skill, nil
	}

	// 查询元数据
	var meta SkillMeta
	if result := sm.db.Where("name = ?", name).First(&meta); result.Error != nil {
		return nil, fmt.Errorf("skill not found: %s", name)
	}

	// 读取文件
	content, err := os.ReadFile(meta.FilePath)
	if err != nil {
		return nil, err
	}

	// 解析技能
	skill, err := parseSkillContent(string(content), meta.FilePath)
	if err != nil {
		return nil, err
	}

	// 加入缓存
	sm.cache.Add(name, skill)

	// 更新使用统计（异步）
	go sm.recordUsage(name)

	return skill, nil
}

// recordUsage 记录技能使用
func (sm *SkillManagerV2) recordUsage(name string) {
	now := time.Now().Unix()
	sm.db.Model(&SkillMeta{}).
		Where("name = ?", name).
		Updates(map[string]interface{}{
			"use_count":  gorm.Expr("use_count + 1"),
			"last_used":  now,
		})
}

// GetSkillContent 只获取技能内容（用于提示注入）
func (sm *SkillManagerV2) GetSkillContent(name string) (string, error) {
	skill, err := sm.GetSkill(name)
	if err != nil {
		return "", err
	}
	return skill.SystemPrompt, nil
}

// DeleteSkill 删除技能
func (sm *SkillManagerV2) DeleteSkill(name string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 获取文件路径
	var meta SkillMeta
	if result := sm.db.Where("name = ?", name).First(&meta); result.Error != nil {
		return fmt.Errorf("skill not found: %s", name)
	}

	// 判断是新格式（子目录中的 skill.md）还是旧格式（直接在 skills 目录下的 *.md）
	if filepath.Base(meta.FilePath) == "skill.md" {
		// 新格式：删除整个目录
		skillDir := filepath.Dir(meta.FilePath)
		if err := os.RemoveAll(skillDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete skill directory: %w", err)
		}
	} else {
		// 旧格式：删除单个文件
		if err := os.Remove(meta.FilePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete skill file: %w", err)
		}
	}

	// 删除数据库记录
	sm.db.Where("name = ?", name).Delete(&SkillMeta{})

	// 从缓存移除
	sm.cache.Remove(name)

	return nil
}

// CreateSkill 创建技能
func (sm *SkillManagerV2) CreateSkill(skill *Skill) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 检查是否已存在
	var count int64
	sm.db.Model(&SkillMeta{}).Where("name = ?", skill.Name).Count(&count)
	if count > 0 {
		return fmt.Errorf("skill already exists: %s", skill.Name)
	}

	// 构建文件内容
	content := buildSkillContent(skill)

	// 创建技能目录
	skillDir := filepath.Join(sm.skillsDir, skill.Name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("failed to create skill directory: %w", err)
	}

	// 写入文件
	filePath := filepath.Join(skillDir, "skill.md")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write skill file: %w", err)
	}

	// 索引文件
	if err := sm.indexSkillFile(filePath); err != nil {
		return fmt.Errorf("failed to index skill: %w", err)
	}

	return nil
}

// UpdateSkill 更新技能
func (sm *SkillManagerV2) UpdateSkill(name string, updates map[string]interface{}) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 获取现有技能
	var meta SkillMeta
	if result := sm.db.Where("name = ?", name).First(&meta); result.Error != nil {
		return fmt.Errorf("skill not found: %s", name)
	}

	// 读取现有内容
	contentBytes, err := os.ReadFile(meta.FilePath)
	if err != nil {
		return err
	}

	skill, err := parseSkillContent(string(contentBytes), meta.FilePath)
	if err != nil {
		return err
	}

	// 应用更新
	if displayName, ok := updates["display_name"].(string); ok {
		skill.DisplayName = displayName
	}
	if description, ok := updates["description"].(string); ok {
		skill.Description = description
	}
	if systemPrompt, ok := updates["system_prompt"].(string); ok {
		skill.SystemPrompt = systemPrompt
	}
	if tags, ok := updates["tags"].([]string); ok {
		skill.Tags = tags
	}
	if triggers, ok := updates["trigger_words"].([]string); ok {
		skill.TriggerWords = triggers
	}

	// 重新构建文件内容
	newContent := buildSkillContent(skill)

	// 写入文件
	if err := os.WriteFile(meta.FilePath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write skill file: %w", err)
	}

	// 重新索引
	if err := sm.indexSkillFile(meta.FilePath); err != nil {
		return fmt.Errorf("failed to reindex skill: %w", err)
	}

	// 更新缓存
	sm.cache.Remove(name)

	return nil
}

// Reload 重新加载所有技能
func (sm *SkillManagerV2) Reload() error {
	// 清空缓存
	sm.cache.Purge()
	
	// 重建索引
	return sm.RebuildIndex()
}

// Close 关闭管理器
func (sm *SkillManagerV2) Close() error {
	sqlDB, err := sm.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// buildSkillContent 构建技能文件内容
func buildSkillContent(skill *Skill) string {
	var b strings.Builder

	b.WriteString("# " + skill.DisplayName + "\n\n")
	
	if skill.Description != "" {
		b.WriteString("## 描述\n" + skill.Description + "\n\n")
	}

	if len(skill.Tags) > 0 {
		b.WriteString("## 标签\n")
		for _, tag := range skill.Tags {
			b.WriteString("- " + tag + "\n")
		}
		b.WriteString("\n")
	}

	if len(skill.TriggerWords) > 0 {
		b.WriteString("## 触发关键词\n")
		for _, tw := range skill.TriggerWords {
			b.WriteString("- " + tw + "\n")
		}
		b.WriteString("\n")
	}

	if skill.SystemPrompt != "" {
		b.WriteString("## 系统提示\n" + skill.SystemPrompt + "\n")
	}

	return b.String()
}

// EvolutionOptimizer 返回进化优化器
func (sm *SkillManagerV2) EvolutionOptimizer() *SkillEvolutionOptimizer {
	return &SkillEvolutionOptimizer{db: sm.db}
}
