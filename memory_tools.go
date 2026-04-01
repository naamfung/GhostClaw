package main

import (
    "context"
    "fmt"
    "strings"
    "time"

    "github.com/toon-format/toon-go"
)

// handleMemorySave 保存记忆
func handleMemorySave(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
    if globalUnifiedMemory == nil {
        return "Error: memory manager not initialized", false
    }

    key, ok := argsMap["key"].(string)
    if !ok || key == "" {
        return "Error: missing or invalid 'key' parameter. Example: memory_save(key=\"user_name\", value=\"张三\")", false
    }
    value, ok := argsMap["value"].(string)
    if !ok || value == "" {
        return "Error: missing or invalid 'value' parameter.", false
    }

    // 解析分类
    category := MemoryCategoryFact
    if cat, ok := argsMap["category"].(string); ok && cat != "" {
        category = MemoryCategory(cat)
        validCategories := map[MemoryCategory]bool{
            MemoryCategoryPreference: true,
            MemoryCategoryFact:       true,
            MemoryCategoryProject:    true,
            MemoryCategorySkill:      true,
            MemoryCategoryContext:    true,
        }
        if !validCategories[category] {
            return fmt.Sprintf("Error: invalid category '%s'. Valid options: preference, fact, project, skill, context", cat), false
        }
    }

    // 解析范围
    scope := MemoryScopeUser
    if s, ok := argsMap["scope"].(string); ok && s != "" {
        scope = MemoryScope(s)
        if scope != MemoryScopeUser && scope != MemoryScopeGlobal {
            return fmt.Sprintf("Error: invalid scope '%s'. Valid options: user, global", s), false
        }
    }

    // 解析标签
    var tags []string
    if tagsRaw, ok := argsMap["tags"]; ok {
        switch v := tagsRaw.(type) {
        case []interface{}:
            for _, t := range v {
                if s, ok := t.(string); ok {
                    tags = append(tags, s)
                }
            }
        case string:
            if strings.HasPrefix(v, "[") {
                var parsed []string
                if err := toon.Unmarshal([]byte(v), &parsed); err == nil {
                    tags = parsed
                }
            } else if v != "" {
                tags = strings.Split(v, ",")
                for i, t := range tags {
                    tags[i] = strings.TrimSpace(t)
                }
            }
        }
    }

    err := globalUnifiedMemory.SaveEntry(category, key, value, tags, scope)
    if err != nil {
        return fmt.Sprintf("Error saving memory: %v", err), false
    }

    return fmt.Sprintf("Memory saved: [%s] %s = %s", category, key, value), false
}

// handleMemoryRecall 检索记忆
func handleMemoryRecall(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
    if globalUnifiedMemory == nil {
        return "Error: memory manager not initialized", false
    }

    query, _ := argsMap["query"].(string)
    category := MemoryCategory("")
    if cat, ok := argsMap["category"].(string); ok && cat != "" {
        category = MemoryCategory(cat)
    }

    limit := 10
    if l, ok := argsMap["limit"].(float64); ok && l > 0 {
        limit = int(l)
    }

    entries := globalUnifiedMemory.SearchEntries(category, query, limit)
    if len(entries) == 0 {
        if query != "" {
            return fmt.Sprintf("No memories found matching '%s'.", query), false
        }
        return "No memories stored yet.", false
    }

    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("Found %d memory(ies):\n", len(entries)))
    for _, e := range entries {
        sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", e.Category, e.Key, e.Value))
        if len(e.Tags) > 0 {
            sb.WriteString(fmt.Sprintf("  Tags: %s\n", strings.Join(e.Tags, ", ")))
        }
    }

    return sb.String(), false
}

// handleMemoryGet 按键名精确获取记忆
func handleMemoryGet(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
    if globalUnifiedMemory == nil {
        return "Error: memory manager not initialized", false
    }

    key, ok := argsMap["key"].(string)
    if !ok || key == "" {
        return "Error: missing or invalid 'key' parameter. Example: memory_get(key=\"user_name\")", false
    }

    // 尝试在所有分类中查找
    categories := []MemoryCategory{
        MemoryCategoryFact,
        MemoryCategoryPreference,
        MemoryCategoryProject,
        MemoryCategorySkill,
        MemoryCategoryContext,
        MemoryCategoryExperience,
    }
    for _, cat := range categories {
        if e, found := globalUnifiedMemory.GetEntry(cat, key); found {
            data, err := toon.Marshal(e)
            if err != nil {
                return fmt.Sprintf("Error formatting memory: %v", err), false
            }
            return string(data), false
        }
    }

    return fmt.Sprintf("Memory '%s' not found.", key), false
}

// handleMemoryForget 删除记忆
func handleMemoryForget(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
    if globalUnifiedMemory == nil {
        return "Error: memory manager not initialized", false
    }

    key, ok := argsMap["key"].(string)
    if !ok || key == "" {
        return "Error: missing or invalid 'key' parameter. Example: memory_forget(key=\"old_preference\")", false
    }

    // 尝试在所有分类中删除
    categories := []MemoryCategory{
        MemoryCategoryFact,
        MemoryCategoryPreference,
        MemoryCategoryProject,
        MemoryCategorySkill,
        MemoryCategoryContext,
        MemoryCategoryExperience,
    }
    deleted := false
    for _, cat := range categories {
        if err := globalUnifiedMemory.DeleteEntry(cat, key); err == nil {
            deleted = true
            break
        }
    }

    if !deleted {
        return fmt.Sprintf("Memory '%s' not found.", key), false
    }

    return fmt.Sprintf("Memory '%s' has been forgotten.", key), false
}

// handleMemoryList 列出记忆
func handleMemoryList(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
    if globalUnifiedMemory == nil {
        return "Error: memory manager not initialized", false
    }

    category := MemoryCategory("")
    if cat, ok := argsMap["category"].(string); ok && cat != "" {
        category = MemoryCategory(cat)
    }

    scope := MemoryScope("")
    if s, ok := argsMap["scope"].(string); ok && s != "" {
        scope = MemoryScope(s)
    }

    entries := globalUnifiedMemory.SearchEntries(category, "", 0)
    if len(entries) == 0 {
        return "No memories found.", false
    }

    // 按 scope 过滤
    filtered := make([]MemoryEntry, 0)
    for _, e := range entries {
        if scope != "" && e.Scope != scope {
            continue
        }
        filtered = append(filtered, e)
    }

    if len(filtered) == 0 {
        return "No memories found for given scope.", false
    }

    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("Total %d memory(ies):\n\n", len(filtered)))

    // 按分类分组输出
    categoriesMap := map[MemoryCategory][]MemoryEntry{
        MemoryCategoryFact:       {},
        MemoryCategoryPreference: {},
        MemoryCategoryProject:    {},
        MemoryCategorySkill:      {},
        MemoryCategoryContext:    {},
        MemoryCategoryExperience: {},
    }
    for _, e := range filtered {
        categoriesMap[e.Category] = append(categoriesMap[e.Category], e)
    }

    categoryOrder := []MemoryCategory{
        MemoryCategoryFact,
        MemoryCategoryPreference,
        MemoryCategoryProject,
        MemoryCategorySkill,
        MemoryCategoryContext,
        MemoryCategoryExperience,
    }

    for _, cat := range categoryOrder {
        if mems, ok := categoriesMap[cat]; ok && len(mems) > 0 {
            sb.WriteString(fmt.Sprintf("## %s\n", cat))
            for _, e := range mems {
                sb.WriteString(fmt.Sprintf("- %s: %s\n", e.Key, e.Value))
            }
            sb.WriteString("\n")
        }
    }

    return sb.String(), false
}

// handleMemorySummarize 生成记忆摘要
func handleMemorySummarize(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
    if globalUnifiedMemory == nil {
        return "Error: memory manager not initialized", false
    }

    category := MemoryCategory("")
    if cat, ok := argsMap["category"].(string); ok && cat != "" {
        category = MemoryCategory(cat)
    }

    recentDays := 7
    if d, ok := argsMap["recent_days"].(float64); ok && d > 0 {
        recentDays = int(d)
    }

    // 获取所有记忆（无查询）
    entries := globalUnifiedMemory.SearchEntries(category, "", 0)
    if len(entries) == 0 {
        return "No memories to summarize.", false
    }

    // 按更新时间过滤
    cutoff := time.Now().AddDate(0, 0, -recentDays)
    var filtered []MemoryEntry
    for _, e := range entries {
        if e.UpdatedAt.After(cutoff) {
            filtered = append(filtered, e)
        }
    }

    if len(filtered) == 0 {
        return "No recent memories to summarize.", false
    }

    var sb strings.Builder
    sb.WriteString("# 关于用户的记忆\n\n")

    // 按分类组织
    categoriesMap := map[MemoryCategory][]MemoryEntry{
        MemoryCategoryFact:       {},
        MemoryCategoryPreference: {},
        MemoryCategoryProject:    {},
        MemoryCategorySkill:      {},
        MemoryCategoryContext:    {},
        MemoryCategoryExperience: {},
    }
    for _, e := range filtered {
        categoriesMap[e.Category] = append(categoriesMap[e.Category], e)
    }

    if len(categoriesMap[MemoryCategoryFact]) > 0 {
        sb.WriteString("## 基本信息\n")
        for _, e := range categoriesMap[MemoryCategoryFact] {
            sb.WriteString(fmt.Sprintf("- %s: %s\n", e.Key, e.Value))
        }
        sb.WriteString("\n")
    }

    if len(categoriesMap[MemoryCategoryPreference]) > 0 {
        sb.WriteString("## 用户偏好\n")
        for _, e := range categoriesMap[MemoryCategoryPreference] {
            sb.WriteString(fmt.Sprintf("- %s: %s\n", e.Key, e.Value))
        }
        sb.WriteString("\n")
    }

    if len(categoriesMap[MemoryCategoryProject]) > 0 {
        sb.WriteString("## 当前项目\n")
        for _, e := range categoriesMap[MemoryCategoryProject] {
            sb.WriteString(fmt.Sprintf("- %s: %s\n", e.Key, e.Value))
        }
        sb.WriteString("\n")
    }

    if len(categoriesMap[MemoryCategorySkill]) > 0 {
        sb.WriteString("## 技能/能力\n")
        for _, e := range categoriesMap[MemoryCategorySkill] {
            sb.WriteString(fmt.Sprintf("- %s: %s\n", e.Key, e.Value))
        }
        sb.WriteString("\n")
    }

    if len(categoriesMap[MemoryCategoryContext]) > 0 {
        sb.WriteString("## 上下文信息\n")
        for _, e := range categoriesMap[MemoryCategoryContext] {
            sb.WriteString(fmt.Sprintf("- %s: %s\n", e.Key, e.Value))
        }
        sb.WriteString("\n")
    }

    result := sb.String()
    if result == "# 关于用户的记忆\n\n" {
        return "No memories to summarize.", false
    }
    return result, false
}
