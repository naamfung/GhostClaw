package main

import (
        "fmt"
        "log"
        "runtime"
        "strings"
)

// PromptSection 提示段落
type PromptSection struct {
        Name           string
        FullContent    string // 完整版本内容
        CompactContent string // 精简版本内容
        Priority       int    // 0=最高(始终包含), 5=最低(优先丢弃)
}

// AdaptivePromptBuilder 自适应提示构建器
// 根据模型的上下文窗口大小，动态调整系统提示的每个 section 的内容密度
type AdaptivePromptBuilder struct {
        ContextWindow       int
        ReservedForOutput  int
        ReservedForTools   int
        ReservedForHistory int
}

// NewAdaptivePromptBuilder 创建自适应提示构建器
func NewAdaptivePromptBuilder(contextWindow, reservedForOutput, reservedForTools, reservedForHistory int) *AdaptivePromptBuilder {
        return &AdaptivePromptBuilder{
                ContextWindow:       contextWindow,
                ReservedForOutput:  reservedForOutput,
                ReservedForTools:   reservedForTools,
                ReservedForHistory: reservedForHistory,
        }
}

// AvailablePromptBudget 计算可用于系统提示的 token 预算
func (b *AdaptivePromptBuilder) AvailablePromptBudget() int {
        budget := b.ContextWindow - b.ReservedForOutput - b.ReservedForTools - b.ReservedForHistory - 2000
        if budget < 500 {
                budget = 500 // 绝对最低保障
        }
        return budget
}

// CalculateDensity 根据可用预算计算提示密度
func (b *AdaptivePromptBuilder) CalculateDensity() PromptDensity {
        budget := b.AvailablePromptBudget()
        switch {
        case budget >= 8000:
                return PromptDensityFull
        case budget >= 4000:
                return PromptDensityStandard
        case budget >= 2000:
                return PromptDensityCompact
        default:
                return PromptDensityMinimal
        }
}

// BuildPrompt 根据密度等级构建最终提示
func (b *AdaptivePromptBuilder) BuildPrompt(sections []PromptSection, density PromptDensity) string {
        budget := b.AvailablePromptBudget()
        var result strings.Builder
        usedTokens := 0

        // 按优先级排序（升序，0 最先）
        sorted := make([]PromptSection, len(sections))
        copy(sorted, sections)
        for i := 0; i < len(sorted); i++ {
                for j := i + 1; j < len(sorted); j++ {
                        if sorted[j].Priority < sorted[i].Priority {
                                sorted[i], sorted[j] = sorted[j], sorted[i]
                        }
                }
        }

        for _, section := range sorted {
                content := ""

                switch {
                case section.Priority == 0:
                        // 最高优先级：始终包含完整版本
                        content = section.FullContent

                case density == PromptDensityMinimal && section.Priority >= 3:
                        // 最小模式下跳过低优先级 section
                        continue

                case density == PromptDensityCompact && section.Priority >= 4:
                        // 精简模式下跳过最低优先级 section
                        continue

                case density <= PromptDensityCompact && section.CompactContent != "":
                        // 精简或最小模式：使用精简版本
                        content = section.CompactContent

                default:
                        // 标准或完整模式：使用完整版本
                        content = section.FullContent
                }

                if content == "" {
                        continue
                }

                sectionTokens := ImprovedEstimateTokens(content)
                if usedTokens+sectionTokens > budget && section.Priority > 0 {
                        // 超出预算且非必需 section：跳过
                        continue
                }

                if result.Len() > 0 {
                        result.WriteString("\n\n")
                }
                result.WriteString(content)
                usedTokens += sectionTokens
        }

        return result.String()
}

// ShouldInjectConciseMode 是否注入简洁模式指令
func (b *AdaptivePromptBuilder) ShouldInjectConciseMode() bool {
        return b.CalculateDensity() <= PromptDensityCompact
}

// GetConciseModeInstructions 返回简洁模式指令
// 灵感来自 cc-mini 的 "Go straight to the point" 系列约束
func GetConciseModeInstructions() string {
        return `[效率模式 - 请严格遵守以下规则]
- 直奔主题，不要不必要的解释。先给出答案或行动，而不是推理过程。
- 优先使用最简单的工具和方法。不要过度设计。
- 不要添加未被要求的功能。Bug 修复不需要重构周围代码。
- 三行相似代码优于一个过早的抽象。
- 输出保持简短直接。如果可以用一个工具完成，就不要用三个。
- 遇到工具调用失败时，尝试最简单的替代方案，不要反复重试相同操作。
- 需要更多工具时，使用 menu 工具：menu(action="list") 查看可用分类，menu(action="load", target="<分类名>") 加载。`
}

// GetCompactBehaviorRules 返回精简版行为规则（~200 tokens vs 原版 ~1500 tokens）
func GetCompactBehaviorRules() string {
        return `核心行为规则：
1. 始终优先处理最新的用户请求，忽略过时的上下文。
2. 调用工具前先检查历史记录，避免重复调用相同的工具和参数。
3. 遇到 [OPERATION CANCELLED BY USER] 标记时立即停止当前操作。
4. 完成任务后简要汇报结果即可，不要长篇总结。
5. 如果连续两次工具调用返回相同错误，尝试不同的方法而不是重试。`
}

// NewIdentitySection 创建角色身份 section（Priority 0，始终包含）
func NewIdentitySection(role *Role) *PromptSection {
        if role == nil {
                return &PromptSection{
                        Name:        "Identity",
                        FullContent: "你是一个 AI 助手。",
                        Priority:    0,
                }
        }

        full := fmt.Sprintf("你是 %s。%s", role.DisplayName, role.Identity)
        if role.Personality != "" {
                full += fmt.Sprintf("\n性格：%s", role.Personality)
        }
        if role.SpeakingStyle != "" {
                full += fmt.Sprintf("\n说话风格：%s", role.SpeakingStyle)
        }

        compact := fmt.Sprintf("你是%s。%s", role.DisplayName, role.Identity)

        return &PromptSection{
                Name:           "Identity",
                FullContent:    full,
                CompactContent: compact,
                Priority:       0,
        }
}

// NewBehaviorSection 创建行为规则 section（Priority 1）
func NewBehaviorSection() *PromptSection {
        return &PromptSection{
                Name:           "BehaviorRules",
                FullContent:    SYSTEM_PROMPT, // 使用现有的完整行为规则
                CompactContent: GetCompactBehaviorRules(),
                Priority:       1,
        }
}

// NewToolGuideSection 创建工具指南 section（Priority 2）
func NewToolGuideSection(toolSection string) *PromptSection {
        if toolSection == "" {
                return &PromptSection{
                        Name:           "ToolGuide",
                        FullContent:    "",
                        CompactContent: "",
                        Priority:       2,
                }
        }

        // 精简版本：只保留工具名称列表
        lines := strings.Split(toolSection, "\n")
        var compactLines []string
        for _, line := range lines {
                line = strings.TrimSpace(line)
                if line == "" {
                        continue
                }
                // 只保留包含工具名称的行（通常是简短的行）
                if len(line) < 100 {
                        compactLines = append(compactLines, line)
                }
        }
        compact := strings.Join(compactLines, "\n")

        return &PromptSection{
                Name:           "ToolGuide",
                FullContent:    toolSection,
                CompactContent: compact,
                Priority:       2,
        }
}

// NewUserInfoSection 创建用户信息 section（Priority 3）
func NewUserInfoSection(userInfo string) *PromptSection {
        return &PromptSection{
                Name:           "UserInfo",
                FullContent:    userInfo,
                CompactContent: "", // 精简模式下跳过
                Priority:       3,
        }
}

// NewSkillSection 创建技能 section（Priority 3）
func NewSkillSection(skillContent string) *PromptSection {
        return &PromptSection{
                Name:           "Skills",
                FullContent:    skillContent,
                CompactContent: "", // 精简模式下跳过
                Priority:       3,
        }
}

// NewSystemInfoSection 创建系统信息 section（Priority 4，最先丢弃）
func NewSystemInfoSection(sysInfo string) *PromptSection {
        return &PromptSection{
                Name:           "SystemInfo",
                FullContent:    sysInfo,
                CompactContent: "", // 精简模式下跳过
                Priority:       4,
        }
}

// BuildAdaptiveSystemPrompt 构建自适应系统提示
// 这是对现有 BuildSystemPromptForActor 的轻量包装，在小模型下自动降级
func BuildAdaptiveSystemPrompt(
        actorName string,
        am *ActorManager,
        pm *RoleManager,
        stage *Stage,
        contextWindow int,
        toolTokens int,
        historyTokens int,
        maxOutputTokens int,
) string {
        builder := NewAdaptivePromptBuilder(contextWindow, maxOutputTokens, toolTokens, historyTokens)
        density := builder.CalculateDensity()

        // 对于大上下文模型，使用原有的完整构建方式
        if density == PromptDensityFull {
                prompt := BuildSystemPromptForActor(actorName, am, pm, stage)
                // Plan Mode 系统提示注入
                if globalPlanMode != nil && globalPlanMode.IsActive() {
                        prompt += "\n\n" + GetPlanModeSystemPrompt()
                }
                return prompt
        }

        // 小模型：使用自适应构建
        var sections []PromptSection

        // 1. 身份 section（始终包含）
        actor, _ := am.GetActor(actorName)
        var role *Role
        if actor != nil {
                role, _ = pm.GetRole(actor.Role)
        }
        sections = append(sections, *NewIdentitySection(role))

        // 2. 行为规则 section
        if density <= PromptDensityStandard {
                sections = append(sections, *NewBehaviorSection())
        }

        // 3. 简洁模式指令
        if builder.ShouldInjectConciseMode() {
                sections = append(sections, PromptSection{
                        Name:        "ConciseMode",
                        FullContent: GetConciseModeInstructions(),
                        Priority:    1,
                })
        }

        // 4. 工具指南（仅在 Standard 及以上包含）
        if density <= PromptDensityStandard && role != nil {
                toolSection := BuildToolSectionForRole(role)
                sections = append(sections, *NewToolGuideSection(toolSection))
        }

        // 5. 用户信息（仅在 Full 模式包含，此处 density < Full 所以跳过）
        // 6. 技能信息（同上）

        // 7. 系统环境信息（仅在 Standard 包含精简版）
        if density <= PromptDensityStandard {
                sysInfo := getSystemInfoString(density)
                if sysInfo != "" {
                        sections = append(sections, *NewSystemInfoSection(sysInfo))
                }
        }

        prompt := builder.BuildPrompt(sections, density)

        // Plan Mode 系统提示注入（最高优先级，追加到系统提示末尾）
        if globalPlanMode != nil && globalPlanMode.IsActive() {
                prompt += "\n\n" + GetPlanModeSystemPrompt()
        }

        log.Printf("[AdaptivePrompt] density=%v, budget=%d, sections=%d, prompt_len=%d",
                density, builder.AvailablePromptBudget(), len(sections), len(prompt))

        return prompt
}

// getSystemInfoString 获取系统信息字符串
func getSystemInfoString(density PromptDensity) string {
        if density <= PromptDensityCompact {
                // 精简模式：仅 OS 信息
                return fmt.Sprintf("系统：%s", getOSName())
        }
        // 标准/完整模式
        return getFullSystemInfo()
}

// getOSName 获取简化的 OS 名称
func getOSName() string {
        if globalConfig.SystemInfo.Enabled {
                return runtime.GOOS
        }
        return runtime.GOOS
}

// getFullSystemInfo 获取完整系统信息
func getFullSystemInfo() string {
        return fmt.Sprintf("OS: %s, Arch: %s", runtime.GOOS, runtime.GOARCH)
}
