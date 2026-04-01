package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type MemoryCategory string

const (
	MemoryCategoryPreference MemoryCategory = "preference"
	MemoryCategoryFact       MemoryCategory = "fact"
	MemoryCategoryProject    MemoryCategory = "project"
	MemoryCategorySkill      MemoryCategory = "skill"
	MemoryCategoryContext    MemoryCategory = "context"
	MemoryCategoryExperience MemoryCategory = "experience"
)

type MemoryScope string

const (
	MemoryScopeUser   MemoryScope = "user"
	MemoryScopeGlobal MemoryScope = "global"
)

type MemoryEntry struct {
	ID         string
	Category   MemoryCategory
	Scope      MemoryScope
	Key        string
	Value      string
	Tags       []string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	AccessCnt  int
	Score      float64
	TaskDesc   string
	Actions    []ExperienceAction
	Result     bool
	Summary    string
	SessionID  string
	UsedCount  int
}

type ExperienceAction struct {
	ToolName string                 `json:"tool_name"`
	Input    map[string]interface{} `json:"input"`
	Output   string                 `json:"output"`
}

type SessionRecord struct {
	SessionID    string
	Channel      string
	StartTime    time.Time
	EndTime      time.Time
	MessageCount int
	Summary      string
	Tags         []string
	Experiences  []string
}

// UnifiedMemory 使用数据库存储
type UnifiedMemory struct {
	db *gorm.DB
}

func NewUnifiedMemory(workDir string) (*UnifiedMemory, error) {
	// 注意：数据库已在 InitDB 中初始化，此处直接使用全局 DB
	if globalDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	return &UnifiedMemory{db: globalDB}, nil
}

// toJSON 辅助函数
func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// fromJSON 辅助函数
func fromJSON(data string, v interface{}) error {
	return json.Unmarshal([]byte(data), v)
}

// SaveEntry 保存记忆
func (m *UnifiedMemory) SaveEntry(category MemoryCategory, key, value string, tags []string, scope MemoryScope) error {
	now := time.Now()
	var existing Memories
	result := m.db.Where("category = ? AND key = ?", category, key).First(&existing)
	if result.Error == nil {
		// 更新
		existing.Value = value
		existing.Tags = toJSON(tags)
		existing.Scope = string(scope)
		existing.UpdatedAt = now
		existing.AccessCnt++
		return m.db.Save(&existing).Error
	}
	// 新建
	mem := Memories{
		ID:        uuid.New().String(),
		Category:  string(category),
		Key:       key,
		Value:     value,
		Tags:      toJSON(tags),
		Scope:     string(scope),
		CreatedAt: now,
		UpdatedAt: now,
		AccessCnt: 1,
	}
	return m.db.Create(&mem).Error
}

// GetEntry 获取记忆
func (m *UnifiedMemory) GetEntry(category MemoryCategory, key string) (MemoryEntry, bool) {
	var mem Memories
	err := m.db.Where("category = ? AND key = ?", category, key).First(&mem).Error
	if err != nil {
		return MemoryEntry{}, false
	}
	var tags []string
	fromJSON(mem.Tags, &tags)
	return MemoryEntry{
		ID:        mem.ID,
		Category:  MemoryCategory(mem.Category),
		Scope:     MemoryScope(mem.Scope),
		Key:       mem.Key,
		Value:     mem.Value,
		Tags:      tags,
		CreatedAt: mem.CreatedAt,
		UpdatedAt: mem.UpdatedAt,
		AccessCnt: mem.AccessCnt,
	}, true
}

// DeleteEntry 删除记忆
func (m *UnifiedMemory) DeleteEntry(category MemoryCategory, key string) error {
	return m.db.Where("category = ? AND key = ?", category, key).Delete(&Memories{}).Error
}

// UpdateEntry 更新记忆
func (m *UnifiedMemory) UpdateEntry(category MemoryCategory, key, newValue string, newTags []string) error {
	return m.db.Model(&Memories{}).
		Where("category = ? AND key = ?", category, key).
		Updates(map[string]interface{}{
			"value":      newValue,
			"tags":       toJSON(newTags),
			"updated_at": time.Now(),
			"access_cnt": gorm.Expr("access_cnt + 1"),
		}).Error
}

// SearchEntries 搜索记忆
func (m *UnifiedMemory) SearchEntries(category MemoryCategory, query string, limit int) []MemoryEntry {
	db := m.db.Model(&Memories{})
	if category != "" {
		db = db.Where("category = ?", category)
	}
	if query != "" {
		db = db.Where("key LIKE ? OR value LIKE ?", "%"+query+"%", "%"+query+"%")
	}
	var mems []Memories
	db.Order("score DESC, access_cnt DESC").Limit(limit).Find(&mems)

	entries := make([]MemoryEntry, len(mems))
	for i, mem := range mems {
		var tags []string
		fromJSON(mem.Tags, &tags)
		entries[i] = MemoryEntry{
			ID:        mem.ID,
			Category:  MemoryCategory(mem.Category),
			Scope:     MemoryScope(mem.Scope),
			Key:       mem.Key,
			Value:     mem.Value,
			Tags:      tags,
			CreatedAt: mem.CreatedAt,
			UpdatedAt: mem.UpdatedAt,
			AccessCnt: mem.AccessCnt,
		}
	}
	return entries
}

// RecordExperience 记录经验
func (m *UnifiedMemory) RecordExperience(taskDesc string, actions []ExperienceAction, result bool, sessionID string) error {
	now := time.Now()
	exp := Experiences{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		TaskDesc:  taskDesc,
		Actions:   toJSON(actions),
		Result:    result,
		Summary:   fmt.Sprintf("%s → %s", taskDesc, mapResult(result)),
		Score:     0.5,
		UsedCount: 0,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return m.db.Create(&exp).Error
}

// RetrieveExperiences 检索经验
func (m *UnifiedMemory) RetrieveExperiences(taskDesc string, limit int) []MemoryEntry {
	var exps []Experiences
	query := m.db.Model(&Experiences{})
	if taskDesc != "" {
		query = query.Where("task_desc LIKE ?", "%"+taskDesc+"%")
	}
	query.Order("score DESC, used_count DESC").Limit(limit).Find(&exps)

	entries := make([]MemoryEntry, len(exps))
	for i, exp := range exps {
		var actions []ExperienceAction
		fromJSON(exp.Actions, &actions)
		entries[i] = MemoryEntry{
			ID:        exp.ID,
			Category:  MemoryCategoryExperience,
			TaskDesc:  exp.TaskDesc,
			Actions:   actions,
			Result:    exp.Result,
			Summary:   exp.Summary,
			Score:     exp.Score,
			UsedCount: exp.UsedCount,
			CreatedAt: exp.CreatedAt,
			UpdatedAt: exp.UpdatedAt,
		}
	}
	return entries
}

// UpdateExperienceRating 更新经验评分
func (m *UnifiedMemory) UpdateExperienceRating(expID string, success bool) {
	var exp Experiences
	if err := m.db.First(&exp, "id = ?", expID).Error; err != nil {
		return
	}
	delta := 0.1
	if success {
		exp.Score += delta
		if exp.Score > 1.0 {
			exp.Score = 1.0
		}
	} else {
		exp.Score -= delta
		if exp.Score < 0.0 {
			exp.Score = 0.0
		}
	}
	exp.UsedCount++
	exp.UpdatedAt = time.Now()
	m.db.Save(&exp)
}

// RecordSession 记录会话摘要
func (m *UnifiedMemory) RecordSession(sessionID, channel, summary string, messageCount int, tags []string) {
	now := time.Now()
	var existing Sessions
	err := m.db.Where("session_key = ?", sessionID).First(&existing).Error
	if err == nil {
		existing.EndTime = now
		existing.MessageCount = messageCount
		existing.Summary = summary
		existing.Tags = toJSON(tags)
		m.db.Save(&existing)
		return
	}
	sess := Sessions{
		ID:           uuid.New().String(),
		SessionKey:   sessionID,
		StartTime:    now,
		EndTime:      now,
		MessageCount: messageCount,
		Summary:      summary,
		Tags:         toJSON(tags),
		Channel:      channel,
	}
	m.db.Create(&sess)
}

// GetRecentSessions 获取最近会话
func (m *UnifiedMemory) GetRecentSessions(limit int) []SessionRecord {
	var sessions []Sessions
	m.db.Order("start_time DESC").Limit(limit).Find(&sessions)
	records := make([]SessionRecord, len(sessions))
	for i, s := range sessions {
		var tags []string
		fromJSON(s.Tags, &tags)
		records[i] = SessionRecord{
			SessionID:    s.SessionKey,
			Channel:      s.Channel,
			StartTime:    s.StartTime,
			EndTime:      s.EndTime,
			MessageCount: s.MessageCount,
			Summary:      s.Summary,
			Tags:         tags,
		}
	}
	return records
}

// GetContextForPrompt 获取提示上下文（用于注入系统提示）
func (m *UnifiedMemory) GetContextForPrompt(taskDesc string) string {
	var sb strings.Builder

	// 获取事实、偏好、项目、技能
	facts := m.SearchEntries(MemoryCategoryFact, "", 5)
	prefs := m.SearchEntries(MemoryCategoryPreference, "", 3)
	projects := m.SearchEntries(MemoryCategoryProject, "", 3)
	skills := m.SearchEntries(MemoryCategorySkill, "", 3)

	if len(facts) > 0 || len(prefs) > 0 || len(projects) > 0 || len(skills) > 0 {
		sb.WriteString("## 关于用户的记忆\n\n")
		for _, f := range facts {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", f.Key, f.Value))
		}
		for _, p := range prefs {
			sb.WriteString(fmt.Sprintf("- 偏好: %s: %s\n", p.Key, p.Value))
		}
		for _, pr := range projects {
			sb.WriteString(fmt.Sprintf("- 项目: %s: %s\n", pr.Key, pr.Value))
		}
		for _, s := range skills {
			sb.WriteString(fmt.Sprintf("- 技能: %s: %s\n", s.Key, s.Value))
		}
		sb.WriteString("\n")
	}

	exps := m.RetrieveExperiences(taskDesc, 3)
	if len(exps) > 0 {
		sb.WriteString("## 历史经验参考\n\n")
		for i, exp := range exps {
			status := "✅ 成功"
			if !exp.Result {
				status = "❌ 失败"
			}
			sb.WriteString(fmt.Sprintf("%d. %s (评分: %.2f)\n", i+1, exp.Summary, exp.Score))
			if len(exp.Actions) > 0 {
				sb.WriteString(fmt.Sprintf("   行动: %s\n", formatActions(exp.Actions)))
			}
			sb.WriteString(fmt.Sprintf("   结果: %s\n\n", status))
		}
	}
	return sb.String()
}

func mapResult(success bool) string {
	if success {
		return "成功"
	}
	return "失败"
}

func formatActions(actions []ExperienceAction) string {
	if len(actions) == 0 {
		return "无"
	}
	var sb strings.Builder
	for i, a := range actions {
		if i > 0 {
			sb.WriteString(" → ")
		}
		sb.WriteString(a.ToolName)
	}
	return sb.String()
}

