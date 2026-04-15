package main

import (
        "strings"
)

// Required bootstrap memory keys that must exist for the agent to be fully operational.
var requiredBootstrapKeys = []string{
        "user.name",
        "user.birth_year",
        "user.gender",
        "assistant.name",
}

// bootstrapCategories 是引導檢查需要覆蓋的記憶類別
var bootstrapCategories = []MemoryCategory{
        MemoryCategoryFact,
        MemoryCategoryPreference,
        MemoryCategoryContext,
}

// batchCheckBootstrapKeys 一次查詢所有需要的 keys × categories，
// 返回一個 set[string] 包含已存在的 key（不區分 category）。
// 將 12~15 次 SELECT 減少為 1 次，消除 GORM "record not found" 日誌刷屏。
func batchCheckBootstrapKeys(um *UnifiedMemory) map[string]bool {
        found := make(map[string]bool)
        if um == nil {
                return found
        }
        gdb := um.getDB()
        if gdb == nil {
                return found
        }
        var results []Memories
        gdb.Where("key IN ? AND category IN ?", requiredBootstrapKeys, bootstrapCategories).
                Select("DISTINCT key").
                Find(&results)
        for _, r := range results {
                found[r.Key] = true
        }
        return found
}

// IsBootstrapNeeded checks if any required bootstrap keys are missing from memory.
func IsBootstrapNeeded(um *UnifiedMemory) bool {
        found := batchCheckBootstrapKeys(um)
        for _, key := range requiredBootstrapKeys {
                if !found[key] {
                        return true
                }
        }
        return false
}

// GetMissingBootstrapKeys returns the list of required keys that are missing.
func GetMissingBootstrapKeys(um *UnifiedMemory) []string {
        var missing []string
        if um == nil {
                return requiredBootstrapKeys
        }
        found := batchCheckBootstrapKeys(um)
        for _, key := range requiredBootstrapKeys {
                if !found[key] {
                        missing = append(missing, key)
                }
        }
        return missing
}

// GetBootstrapPrompt returns the hardcoded bootstrap prompt in Chinese.
// This prompt instructs the model to query the user for missing information
// and save it using memory_save.
func GetBootstrapPrompt() string {
        return `# 初始化引导

这是你与雇主的第一次对话。你需要收集以下基本信息以完成初始化：

- **user.name**：雇主的姓名/称呼
- **user.birth_year**：雇主的出生年份
- **user.gender**：雇主的性别
- **assistant.name**：雇主希望如何称呼你

**请按照以下步骤操作：**

1. 自然地与雇主打招呼，说明你需要了解一些基本信息。
2. 通过对话收集上述信息。不要一次列出所有问题，而系似自然对话一样逐个了解。
3. 每获取到一个信息后，立即使用 ` + "`" + `memory_save` + "`" + ` 工具保存到记忆中：
   - ` + "`" + `memory_save(key="user.name", value="张三", category="fact")` + "`" + `
   - ` + "`" + `memory_save(key="user.birth_year", value="1990", category="fact")` + "`" + `
   - ` + "`" + `memory_save(key="user.gender", value="男", category="fact")` + "`" + `
   - ` + "`" + `memory_save(key="assistant.name", value="小助", category="preference")` + "`" + `
4. 所有信息收集完毕后，确认已保存，并告知雇主初始化完成。

**重要规则：**
- 保持自然、友好的语气，不要似填写表单一样机械。
- 根据雇主的回应灵活调整对话节奏。
- 如果雇主不想提供某些信息，跳过即可，不要强求。
- 完成初始化后，立即开始正常工作。`
}

// GetBootstrapMissingKeysPrompt returns a prompt listing which keys are missing.
func GetBootstrapMissingKeysPrompt(um *UnifiedMemory) string {
        missing := GetMissingBootstrapKeys(um)
        if len(missing) == 0 {
                return ""
        }
        return `# 初始化引导

以下信息尚未收集，请在对话中自然地了解并保存：

` + formatKeyList(missing) + `

使用 memory_save 工具保存收集到的信息。收集完毕后即可正常工作。`
}

// formatKeyList formats a list of keys into a readable bullet list.
func formatKeyList(keys []string) string {
        var sb strings.Builder
        for _, key := range keys {
                switch key {
                case "user.name":
                        sb.WriteString("- **user.name**：雇主的姓名/称呼\n")
                case "user.birth_year":
                        sb.WriteString("- **user.birth_year**：雇主的出生年份\n")
                case "user.gender":
                        sb.WriteString("- **user.gender**：雇主的性别\n")
                case "assistant.name":
                        sb.WriteString("- **assistant.name**：雇主希望怎么称呼你\n")
                default:
                        sb.WriteString("- **" + key + "**\n")
                }
        }
        return sb.String()
}
