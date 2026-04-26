package main

import (
        "fmt"
        "regexp"
        "strings"
        "sync"
)

// Switch markers
const (
        SwitchMarkerPrefix = "[GARCLAW:"
        SwitchMarkerNext   = "[GARCLAW:NEXT:"
        SwitchMarkerEnd    = "[GARCLAW:END]"
)

// AutoSwitchMode 自动切换模式
type AutoSwitchMode string

const (
        AutoSwitchDirector   AutoSwitchMode = "director"   // 导演决策模式
        AutoSwitchRoundRobin AutoSwitchMode = "round-robin" // 轮转模式
        AutoSwitchSmart      AutoSwitchMode = "smart"      // 智能判断模式
)

// AutoSwitchConfig 自动切换配置
type AutoSwitchConfig struct {
        Enabled          bool            `json:"enabled"`
        Mode             AutoSwitchMode  `json:"mode"`
        MaxAutoTurns     int             `json:"max_auto_turns"`
        PauseOnUserInput bool            `json:"pause_on_user_input"`
        RoundOrder       []string        `json:"round_order,omitempty"` // 轮转顺序

        // 内部状态
        currentAutoTurns int `json:"-"`
        currentRoundPos  int `json:"-"`
        isPaused         bool `json:"-"`
}

// StageSetting 场景设定
type StageSetting struct {
        World            string `json:"world,omitempty"`
        Era              string `json:"era,omitempty"`
        CurrentLocation  string `json:"current_location,omitempty"`
        CurrentTime      string `json:"current_time,omitempty"`
        AdditionalContext string `json:"additional_context,omitempty"`
}

// Stage 场景管理
type Stage struct {
        mu sync.RWMutex

        // 当前演员
        CurrentActor string `json:"current_actor"`

        // 在场演员
        PresentActors []string `json:"present_actors"`

        // 场景设定
        Setting StageSetting `json:"setting"`

        // 自动切换配置
        AutoSwitch AutoSwitchConfig `json:"auto_switch"`

        // 系统提示词更新标记（角色切换时设置为 true，AgentLoop 检测后重置）
        needUpdateSystemPrompt bool `json:"-"`
}

// NewStage 创建场景
func NewStage() *Stage {
        return &Stage{
                CurrentActor:  "default",
                PresentActors: []string{"default"},
                Setting:       StageSetting{},
                AutoSwitch: AutoSwitchConfig{
                        Enabled:          false,
                        Mode:             AutoSwitchDirector,
                        MaxAutoTurns:     20,
                        PauseOnUserInput: true,
                },
        }
}

// GetCurrentActor 获取当前演员
func (s *Stage) GetCurrentActor() string {
        s.mu.RLock()
        defer s.mu.RUnlock()
        return s.CurrentActor
}

// SetCurrentActor 设置当前演员
func (s *Stage) SetCurrentActor(name string) error {
        s.mu.Lock()
        defer s.mu.Unlock()

        // 验证演员存在（由调用者验证）
        s.CurrentActor = name

        // 如果演员不在在场列表中，添加进去
        found := false
        for _, a := range s.PresentActors {
                if a == name {
                        found = true
                        break
                }
        }
        if !found {
                s.PresentActors = append(s.PresentActors, name)
        }

        // 重置自动切换计数
        s.AutoSwitch.currentAutoTurns = 0

        // 标记需要更新系统提示词
        s.needUpdateSystemPrompt = true

        return nil
}

// NeedUpdateSystemPrompt 检查是否需要更新系统提示词
func (s *Stage) NeedUpdateSystemPrompt() bool {
        s.mu.RLock()
        defer s.mu.RUnlock()
        return s.needUpdateSystemPrompt
}

// ClearUpdateSystemPrompt 清除系统提示词更新标记
func (s *Stage) ClearUpdateSystemPrompt() {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.needUpdateSystemPrompt = false
}

// SetUpdateSystemPrompt 设置系统提示词更新标记
func (s *Stage) SetUpdateSystemPrompt() {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.needUpdateSystemPrompt = true
}

// GetPresentActors 获取在场演员
func (s *Stage) GetPresentActors() []string {
        s.mu.RLock()
        defer s.mu.RUnlock()
        result := make([]string, len(s.PresentActors))
        copy(result, s.PresentActors)
        return result
}

// SetPresentActors 设置在场演员
func (s *Stage) SetPresentActors(actors []string) {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.PresentActors = actors
}

// AddPresentActor 添加在场演员
func (s *Stage) AddPresentActor(name string) {
        s.mu.Lock()
        defer s.mu.Unlock()

        for _, a := range s.PresentActors {
                if a == name {
                        return
                }
        }
        s.PresentActors = append(s.PresentActors, name)
}

// RemovePresentActor 移除在场演员
func (s *Stage) RemovePresentActor(name string) {
        s.mu.Lock()
        defer s.mu.Unlock()

        for i, a := range s.PresentActors {
                if a == name {
                        s.PresentActors = append(s.PresentActors[:i], s.PresentActors[i+1:]...)
                        return
                }
        }
}

// GetSetting 获取场景设定
func (s *Stage) GetSetting() StageSetting {
        s.mu.RLock()
        defer s.mu.RUnlock()
        return s.Setting
}

// SetSetting 设置场景设定
func (s *Stage) SetSetting(setting StageSetting) {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.Setting = setting
}

// AutoSwitchEnabled 检查自动切换是否启用
func (s *Stage) AutoSwitchEnabled() bool {
        s.mu.RLock()
        defer s.mu.RUnlock()
        return s.AutoSwitch.Enabled && !s.AutoSwitch.isPaused
}

// EnableAutoSwitch 启用自动切换
func (s *Stage) EnableAutoSwitch(mode AutoSwitchMode) {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.AutoSwitch.Enabled = true
        s.AutoSwitch.Mode = mode
        s.AutoSwitch.currentAutoTurns = 0
        s.AutoSwitch.isPaused = false
}

// DisableAutoSwitch 禁用自动切换
func (s *Stage) DisableAutoSwitch() {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.AutoSwitch.Enabled = false
        s.AutoSwitch.currentAutoTurns = 0
}

// PauseAutoSwitch 暂停自动切换
func (s *Stage) PauseAutoSwitch() {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.AutoSwitch.isPaused = true
}

// ResumeAutoSwitch 恢复自动切换
func (s *Stage) ResumeAutoSwitch() {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.AutoSwitch.isPaused = false
        s.AutoSwitch.currentAutoTurns = 0
}

// CanAutoSwitch 检查是否可以自动切换
func (s *Stage) CanAutoSwitch() bool {
        s.mu.RLock()
        defer s.mu.RUnlock()

        if !s.AutoSwitch.Enabled || s.AutoSwitch.isPaused {
                return false
        }

        if s.AutoSwitch.currentAutoTurns >= s.AutoSwitch.MaxAutoTurns {
                return false
        }

        return true
}

// IncrementAutoTurns 增加自动切换轮数
func (s *Stage) IncrementAutoTurns() int {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.AutoSwitch.currentAutoTurns++
        return s.AutoSwitch.currentAutoTurns
}

// ResetAutoTurns 重置自动切换轮数
func (s *Stage) ResetAutoTurns() {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.AutoSwitch.currentAutoTurns = 0
}

// GetAutoSwitchState 获取自动切换状态
func (s *Stage) GetAutoSwitchState() (enabled bool, paused bool, turns int, maxTurns int, mode AutoSwitchMode) {
        s.mu.RLock()
        defer s.mu.RUnlock()
        return s.AutoSwitch.Enabled, s.AutoSwitch.isPaused, s.AutoSwitch.currentAutoTurns, s.AutoSwitch.MaxAutoTurns, s.AutoSwitch.Mode
}

// ParseSwitchMarker 解析切换标记
func ParseSwitchMarker(content string) (hasMarker bool, targetActor string, isEnd bool) {
        // 匹配 [GARCLAW:NEXT:actor_name]
        nextRegex := regexp.MustCompile(`\[GARCLAW:NEXT:([a-zA-Z_][a-zA-Z0-9_]*)\]`)
        if matches := nextRegex.FindStringSubmatch(content); len(matches) > 1 {
                return true, matches[1], false
        }

        // 匹配 [GARCLAW:END]
        if strings.Contains(content, SwitchMarkerEnd) {
                return true, "", true
        }

        return false, "", false
}

// StripSwitchMarker 移除切换标记
func StripSwitchMarker(content string) string {
        // 移除 [GARCLAW:NEXT:xxx]
        content = regexp.MustCompile(`\[GARCLAW:NEXT:[a-zA-Z_][a-zA-Z0-9_]*\]`).ReplaceAllString(content, "")
        // 移除 [GARCLAW:END]
        content = strings.ReplaceAll(content, SwitchMarkerEnd, "")
        return strings.TrimSpace(content)
}

// GetNextActorForRoundRobin 获取轮转模式下的下一个演员
func (s *Stage) GetNextActorForRoundRobin() string {
        s.mu.RLock()
        defer s.mu.RUnlock()

        if len(s.AutoSwitch.RoundOrder) > 0 {
                // 使用自定义轮转顺序
                nextIdx := (s.AutoSwitch.currentRoundPos + 1) % len(s.AutoSwitch.RoundOrder)
                return s.AutoSwitch.RoundOrder[nextIdx]
        }

        // 使用在场演员列表轮转
        if len(s.PresentActors) == 0 {
                return s.CurrentActor
        }

        for i, a := range s.PresentActors {
                if a == s.CurrentActor {
                        nextIdx := (i + 1) % len(s.PresentActors)
                        return s.PresentActors[nextIdx]
                }
        }

        return s.PresentActors[0]
}

// AdvanceRoundRobin 推进轮转位置
func (s *Stage) AdvanceRoundRobin() {
        s.mu.Lock()
        defer s.mu.Unlock()

        if len(s.AutoSwitch.RoundOrder) > 0 {
                s.AutoSwitch.currentRoundPos = (s.AutoSwitch.currentRoundPos + 1) % len(s.AutoSwitch.RoundOrder)
        }
}

// BuildStageContext 构建场景上下文（用于系统提示）
func (s *Stage) BuildStageContext(am *ActorManager, rm *RoleManager) string {
        s.mu.RLock()
        defer s.mu.RUnlock()

        var sb strings.Builder

        // 场景设定
        if s.Setting.World != "" || s.Setting.CurrentLocation != "" {
                sb.WriteString("## 场景设定\n\n")
                if s.Setting.World != "" {
                        sb.WriteString(fmt.Sprintf("- **世界**：%s\n", s.Setting.World))
                }
                if s.Setting.Era != "" {
                        sb.WriteString(fmt.Sprintf("- **时代**：%s\n", s.Setting.Era))
                }
                if s.Setting.CurrentLocation != "" {
                        sb.WriteString(fmt.Sprintf("- **当前地点**：%s\n", s.Setting.CurrentLocation))
                }
                if s.Setting.CurrentTime != "" {
                        sb.WriteString(fmt.Sprintf("- **当前时间**：%s\n", s.Setting.CurrentTime))
                }
                sb.WriteString("\n")
        }

        // 在场角色
        if len(s.PresentActors) > 1 {
                sb.WriteString("## 在场角色\n\n")
                for _, actorName := range s.PresentActors {
                        actor, _ := am.GetActor(actorName)
                        if actor != nil {
                                role, _ := rm.GetRole(actor.Role)
                                icon := ""
                                if role != nil {
                                        icon = role.Icon + " "
                                }
                                sb.WriteString(fmt.Sprintf("- %s**%s** (%s)\n", icon, actor.CharacterName, actorName))
                        }
                }
                sb.WriteString("\n")
        }

        // 自动切换提示（导演模式）
        if s.AutoSwitch.Enabled && s.AutoSwitch.Mode == AutoSwitchDirector {
                sb.WriteString(`## 角色切换指令

当场景需要切换角色视角时，在回复末尾使用以下格式：

` + "- `[GARCLAW:NEXT:actor_name]` → 切换到指定角色\n" +
                        "- `[GARCLAW:END]` → 场景结束，等待用户\n\n" +
                        `示例：
` + "```" + `
[GARCLAW:NEXT:hero_lin]
[GARCLAW:END]
` + "```" + `

**规则**：
1. 一次只标注一个角色
2. 确保角色在在场角色列表中
3. 对话场景要让角色交替发言，避免独角戏
4. 重要剧情转折点可以保留叙事者视角

`)
        }

        return sb.String()
}

// BuildWelcomeMessage 构建角色切换欢迎语
func (s *Stage) BuildWelcomeMessage(am *ActorManager, rm *RoleManager) string {
        s.mu.RLock()
        actorName := s.CurrentActor
        s.mu.RUnlock()

        actor, ok := am.GetActor(actorName)
        if !ok {
                return "🎭 已切换到默认角色"
        }

        role, ok := rm.GetRole(actor.Role)
        icon := "🎭"
        displayName := actor.Role
        if ok {
                icon = role.Icon
                displayName = role.DisplayName
        }

        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("%s **已切换到：%s**\n", icon, displayName))

        if actor.CharacterName != "" && actor.CharacterName != displayName {
                sb.WriteString(fmt.Sprintf("📝 角色名：%s\n", actor.CharacterName))
        }

        if role != nil && role.Description != "" {
                sb.WriteString(fmt.Sprintf("📋 %s\n", role.Description))
        }

        // 显示自动切换状态
        if s.AutoSwitch.Enabled {
                _, paused, turns, maxTurns, mode := s.GetAutoSwitchState()
                if paused {
                        sb.WriteString(fmt.Sprintf("⏸️ 自动演绎：已暂停 (%d/%d轮)\n", turns, maxTurns))
                } else {
                        sb.WriteString(fmt.Sprintf("▶️ 自动演绎：开启 (%s模式, %d/%d轮)\n", mode, turns, maxTurns))
                }
        }

        return sb.String()
}

// RestoreFromState 从保存的状态恢复场景
func (s *Stage) RestoreFromState(state StageState) {
        s.mu.Lock()
        defer s.mu.Unlock()

        s.CurrentActor = state.CurrentActor
        s.PresentActors = state.PresentActors
        s.Setting = state.Setting
        s.AutoSwitch = state.AutoSwitch
}

// ToState 导出当前状态
func (s *Stage) ToState() StageState {
        s.mu.RLock()
        defer s.mu.RUnlock()

        presentActors := make([]string, len(s.PresentActors))
        copy(presentActors, s.PresentActors)

        return StageState{
                CurrentActor:  s.CurrentActor,
                PresentActors: presentActors,
                Setting:        s.Setting,
                AutoSwitch:     s.AutoSwitch,
        }
}

// StageState 可序列化的场景状态（用于持久化）
type StageState struct {
        CurrentActor   string           `json:"current_actor"`
        PresentActors  []string         `json:"present_actors"`
        Setting        StageSetting     `json:"setting"`
        AutoSwitch     AutoSwitchConfig `json:"auto_switch"`
}
