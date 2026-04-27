package main

import (
        "fmt"
        "log"
        "runtime/debug"
        "strings"
)

// ============================================================================
// MessageList — 帶結構不變量保護的消息列表
// ============================================================================
//
// 設計目標：
//   - 替換裸 []Message 切片，所有消息操作通過方法進行
//   - 每次操作後可選驗證結構不變量
//   - 提供 Clone() 快照機制，支持從原始消息恢復
//
// 保護的不變量：
//   I1: 首條消息是 system（API 要求）
//   I2: 至少包含一條 user 消息（否則模型失去任務上下文）
//   I3: 無孤兒 tool 消息（每條 tool 消息都有前置 assistant 聲明）
//   I4: 無連續相同 role（system 除外）

// InvariantViolation 描述一條不變量違規
type InvariantViolation struct {
        Code    string // I1, I2, I3, I4
        Message string
        Index   int // 相關消息的索引（-1 表示不適用）
}

func (v InvariantViolation) String() string {
        if v.Index >= 0 {
                return fmt.Sprintf("[%s] %s (index %d)", v.Code, v.Message, v.Index)
        }
        return fmt.Sprintf("[%s] %s", v.Code, v.Message)
}

// MessageList 帶不變量保護的消息列表
type MessageList struct {
        msgs       []Message // 底層消息存儲
        origin     *MessageList // 原始快照（用於恢復）
        sourceDesc string      // 追蹤來源（調試用）
}

// NewMessageList 從 []Message 創建 MessageList
func NewMessageList(msgs []Message) *MessageList {
        copied := make([]Message, len(msgs))
        copy(copied, msgs)
        return &MessageList{
                msgs:       copied,
                sourceDesc: "raw",
        }
}

// NewMessageListWithSource 從 []Message 創建帶來源描述的 MessageList
func NewMessageListWithSource(msgs []Message, source string) *MessageList {
        copied := make([]Message, len(msgs))
        copy(copied, msgs)
        return &MessageList{
                msgs:       copied,
                sourceDesc: source,
        }
}

// FromMessages 是 NewMessageList 的別名
func FromMessages(msgs []Message) *MessageList {
        return NewMessageList(msgs)
}

// ============================================================================
// 基礎訪問
// ============================================================================

// Raw 返回底層 []Message 切片（兼容現有代碼）
// 注意：直接修改返回的切片不會觸發不變量驗證
func (ml *MessageList) Raw() []Message {
        if ml == nil {
                return nil
        }
        return ml.msgs
}

// Len 返回消息數量
func (ml *MessageList) Len() int {
        if ml == nil {
                return 0
        }
        return len(ml.msgs)
}

// IsEmpty 是否為空
func (ml *MessageList) IsEmpty() bool {
        return ml == nil || len(ml.msgs) == 0
}

// At 返回指定索引的消息（越界返回零值）
func (ml *MessageList) At(i int) Message {
        if ml == nil || i < 0 || i >= len(ml.msgs) {
                return Message{}
        }
        return ml.msgs[i]
}

// Clone 複製 MessageList，用於保存原始消息快照供恢復
// 注意：這是 Message struct 的淺層複製（shallow copy）。
// Content、ToolCalls、ReasoningContent 為 interface{} 字段，
// 原始和複製共享底層數據引用。修改 interface 內部數據（如 map 條目）
// 會同時影響兩者；賦值新值則不會。對不可變的 string Content 是安全的。
func (ml *MessageList) Clone() *MessageList {
        if ml == nil {
                return &MessageList{}
        }
        copied := make([]Message, len(ml.msgs))
        copy(copied, ml.msgs)
        return &MessageList{
                msgs:       copied,
                origin:     ml.origin, // 保留原始來源鏈
                sourceDesc: ml.sourceDesc + "→clone",
        }
}

// Snapshot 創建快照並設置為原始來源（用於後續恢復）
func (ml *MessageList) Snapshot(desc string) *MessageList {
        cloned := ml.Clone()
        cloned.origin = ml
        cloned.sourceDesc = desc
        return cloned
}

// Origin 返回原始快照（用於恢復丟失的消息）
func (ml *MessageList) Origin() *MessageList {
        if ml == nil {
                return nil
        }
        return ml.origin
}

// ============================================================================
// 查詢方法
// ============================================================================

// HasUser 是否包含至少一條用戶消息
func (ml *MessageList) HasUser() bool {
        if ml == nil {
                return false
        }
        for i := len(ml.msgs) - 1; i >= 0; i-- {
                if ml.msgs[i].Role == "user" {
                        return true
                }
        }
        return false
}

// LatestUser 返回最後一條用戶消息（不存在返回零值）
func (ml *MessageList) LatestUser() Message {
        if ml == nil {
                return Message{}
        }
        for i := len(ml.msgs) - 1; i >= 0; i-- {
                if ml.msgs[i].Role == "user" {
                        return ml.msgs[i]
                }
        }
        return Message{}
}

// LatestUserIndex 返回最後一條用戶消息的索引（不存在返回 -1）
func (ml *MessageList) LatestUserIndex() int {
        if ml == nil {
                return -1
        }
        for i := len(ml.msgs) - 1; i >= 0; i-- {
                if ml.msgs[i].Role == "user" {
                        return i
                }
        }
        return -1
}

// FirstUserIndex 返回第一條用戶消息的索引（不存在返回 -1）
func (ml *MessageList) FirstUserIndex() int {
        if ml == nil {
                return -1
        }
        for i, msg := range ml.msgs {
                if msg.Role == "user" {
                        return i
                }
        }
        return -1
}

// SystemPrompt 返回系統提示消息（首條 system 消息）
func (ml *MessageList) SystemPrompt() Message {
        if ml == nil || len(ml.msgs) == 0 {
                return Message{}
        }
        for _, msg := range ml.msgs {
                if msg.Role == "system" {
                        return msg
                }
        }
        return Message{}
}

// SystemPrefixEnd 返回連續 system 消息塊的結束索引（exclusive）
func (ml *MessageList) SystemPrefixEnd() int {
        if ml == nil {
                return 0
        }
        for i, msg := range ml.msgs {
                if msg.Role != "system" {
                        return i
                }
        }
        return len(ml.msgs)
}

// HasSystem 是否以 system 消息開頭
func (ml *MessageList) HasSystem() bool {
        return ml != nil && len(ml.msgs) > 0 && ml.msgs[0].Role == "system"
}

// UserCount 返回用戶消息的數量
func (ml *MessageList) UserCount() int {
        if ml == nil {
                return 0
        }
        count := 0
        for _, msg := range ml.msgs {
                if msg.Role == "user" {
                        count++
                }
        }
        return count
}

// ============================================================================
// 不變量驗證
// ============================================================================

// Validate 檢查所有結構不變量，返回違規列表
func (ml *MessageList) Validate() []InvariantViolation {
        var violations []InvariantViolation
        if ml == nil || len(ml.msgs) == 0 {
                return violations
        }

        // I1: 首條消息是 system
        if ml.msgs[0].Role != "system" {
                violations = append(violations, InvariantViolation{
                        Code:    "I1",
                        Message: "首條消息不是 system",
                        Index:   0,
                })
        }

        // I2: 至少包含一條 user 消息
        if !ml.HasUser() {
                violations = append(violations, InvariantViolation{
                        Code:    "I2",
                        Message: "缺少用戶消息",
                        Index:   -1,
                })
        }

        // I3: 無孤兒 tool 消息
        // 收集每個位置生效的 tool_call ID 聲明
        declared := make(map[string]bool)
        for i, msg := range ml.msgs {
                switch msg.Role {
                case "assistant":
                        newDeclared := make(map[string]bool)
                        if msg.ToolCalls != nil {
                                switch v := msg.ToolCalls.(type) {
                                case []interface{}:
                                        for _, tc := range v {
                                                if tcMap, ok := tc.(map[string]interface{}); ok {
                                                        if id, ok := tcMap["id"].(string); ok && id != "" {
                                                                newDeclared[id] = true
                                                        }
                                                }
                                        }
                                case []map[string]interface{}:
                                        for _, tc := range v {
                                                if id, ok := tc["id"].(string); ok && id != "" {
                                                        newDeclared[id] = true
                                                }
                                        }
                                }
                        }
                        if len(newDeclared) > 0 {
                                declared = newDeclared
                        }
                case "tool":
                        if !declared[msg.ToolCallID] {
                                violations = append(violations, InvariantViolation{
                                        Code:    "I3",
                                        Message: fmt.Sprintf("孤兒 tool 消息 (tool_call_id: %s)", msg.ToolCallID),
                                        Index:   i,
                                })
                        }
                case "user":
                        declared = make(map[string]bool)
                }
        }

        // I4: 無連續相同 role（system 除外）
        for i := 1; i < len(ml.msgs); i++ {
                if ml.msgs[i].Role == ml.msgs[i-1].Role && ml.msgs[i].Role != "system" {
                        violations = append(violations, InvariantViolation{
                                Code:    "I4",
                                Message: fmt.Sprintf("連續相同 role '%s'", ml.msgs[i].Role),
                                Index:   i,
                        })
                }
        }

        return violations
}

// ValidateOrLog 驗證不變量，有違規時記錄日誌並返回 false
func (ml *MessageList) ValidateOrLog(prefix string) bool {
        violations := ml.Validate()
        if len(violations) == 0 {
                return true
        }
        for _, v := range violations {
                log.Printf("[MessageList] %s invariant violation: %s", prefix, v)
        }
        return false
}

// ============================================================================
// 自動修復
// ============================================================================

// EnsureUser 確保至少包含一條用戶消息
// 優先從 origin（快照）恢復，其次從自身尾部搜索
// 返回修復後的新 MessageList（不修改原對象）
func (ml *MessageList) EnsureUser() *MessageList {
        if ml.HasUser() {
                return ml
        }

        result := ml.Clone()

        // 優先從原始快照恢復
        if ml.origin != nil && ml.origin.HasUser() {
                userMsg := ml.origin.LatestUser()
                if userMsg.Role != "" {
                        log.Printf("[MessageList] EnsureUser: 從原始快照恢復用戶消息 (%s)", ml.sourceDesc)
                        insertPos := result.SystemPrefixEnd()
                        if insertPos >= len(result.msgs) {
                                insertPos = len(result.msgs)
                        }
                        newMsgs := make([]Message, 0, len(result.msgs)+1)
                        newMsgs = append(newMsgs, result.msgs[:insertPos]...)
                        newMsgs = append(newMsgs, userMsg)
                        newMsgs = append(newMsgs, result.msgs[insertPos:]...)
                        result.msgs = newMsgs
                        return result
                }
        }

        log.Printf("[MessageList] EnsureUser: 無法恢復用戶消息 (source: %s)", ml.sourceDesc)
        return result
}

// ============================================================================
// 變換操作（返回新的 MessageList，不修改原對象）
// ============================================================================

// RepairOrphans 修復孤兒 tool 消息和孤立的 tool_calls
// 組合 findLegalStart + removeOrphanedToolCalls + mergeConsecutiveSameRole
func (ml *MessageList) RepairOrphans() *MessageList {
        if ml == nil || len(ml.msgs) <= 1 {
                return ml
        }

        // Step 1: findLegalStart — 前向掃描移除開頭的孤兒 tool 結果
        repaired := findLegalStart(ml.msgs)

        // Step 2: removeOrphanedToolCalls — 移除沒有對應 tool 結果的 tool_calls
        repaired = removeOrphanedToolCalls(repaired)

        // Step 3: mergeConsecutiveSameRole — 合併連續相同角色
        repaired = mergeConsecutiveSameRole(repaired)

        return &MessageList{
                msgs:       repaired,
                origin:     ml.origin,
                sourceDesc: ml.sourceDesc + "→repair",
        }
}

// Deduplicate 合併連續相同角色的消息
func (ml *MessageList) Deduplicate() *MessageList {
        if ml == nil || len(ml.msgs) <= 1 {
                return ml
        }
        return &MessageList{
                msgs:       mergeConsecutiveSameRole(ml.msgs),
                origin:     ml.origin,
                sourceDesc: ml.sourceDesc + "→dedup",
        }
}

// Truncate 截斷消息到指定最大數量，保留 system 前綴和用戶消息
func (ml *MessageList) Truncate(max int) *MessageList {
        if ml == nil || len(ml.msgs) <= max {
                return ml
        }

        hasSystem := ml.HasSystem()
        budget := max
        if hasSystem {
                budget = max - 1
        }

        latestUserIdx := ml.LatestUserIndex()
        if latestUserIdx < 0 {
                latestUserIdx = len(ml.msgs) - 1
        }

        idealStart := len(ml.msgs) - budget
        if idealStart < 0 {
                idealStart = 0
        }
        if latestUserIdx > 0 && idealStart > latestUserIdx {
                idealStart = latestUserIdx
        }

        // 搜索邊界附近的 user 消息起始位置
        boundaryStart := idealStart
        searchWindow := 20
        if idealStart > searchWindow {
                for i := idealStart; i >= idealStart-searchWindow && i > 0; i-- {
                        if ml.msgs[i].Role == "user" && (i == 0 || ml.msgs[i-1].Role != "user") {
                                boundaryStart = i
                                break
                        }
                }
        }
        if latestUserIdx > 0 && boundaryStart > latestUserIdx {
                boundaryStart = latestUserIdx
        }

        // 構建截斷後的消息
        var newMsgs []Message
        if hasSystem {
                newMsgs = make([]Message, 0, 1+len(ml.msgs)-boundaryStart)
                newMsgs = append(newMsgs, ml.msgs[0])
                newMsgs = append(newMsgs, ml.msgs[boundaryStart:]...)
        } else {
                newMsgs = ml.msgs[boundaryStart:]
        }

        return &MessageList{
                msgs:       newMsgs,
                origin:     ml.origin,
                sourceDesc: ml.sourceDesc + fmt.Sprintf("→truncate(%d)", max),
        }
}

// Prepend 在消息列表前面插入消息
func (ml *MessageList) Prepend(msgs ...Message) *MessageList {
        if ml == nil {
                return NewMessageList(msgs)
        }
        newMsgs := make([]Message, 0, len(msgs)+len(ml.msgs))
        newMsgs = append(newMsgs, msgs...)
        newMsgs = append(newMsgs, ml.msgs...)
        return &MessageList{
                msgs:       newMsgs,
                origin:     ml.origin,
                sourceDesc: ml.sourceDesc + "→prepend",
        }
}

// Append 在消息列表末尾追加消息
func (ml *MessageList) Append(msgs ...Message) *MessageList {
        if ml == nil {
                return NewMessageList(msgs)
        }
        newMsgs := make([]Message, 0, len(ml.msgs)+len(msgs))
        newMsgs = append(newMsgs, ml.msgs...)
        newMsgs = append(newMsgs, msgs...)
        return &MessageList{
                msgs:       newMsgs,
                origin:     ml.origin,
                sourceDesc: ml.sourceDesc + "→append",
        }
}

// InsertAt 在指定位置插入消息
func (ml *MessageList) InsertAt(pos int, msgs ...Message) *MessageList {
        if ml == nil {
                return NewMessageList(msgs)
        }
        if pos < 0 {
                pos = 0
        }
        if pos > len(ml.msgs) {
                pos = len(ml.msgs)
        }
        newMsgs := make([]Message, 0, len(ml.msgs)+len(msgs))
        newMsgs = append(newMsgs, ml.msgs[:pos]...)
        newMsgs = append(newMsgs, msgs...)
        newMsgs = append(newMsgs, ml.msgs[pos:]...)
        return &MessageList{
                msgs:       newMsgs,
                origin:     ml.origin,
                sourceDesc: ml.sourceDesc + fmt.Sprintf("→insert(%d)", pos),
        }
}

// SetMsgs 直接設置底層消息（⚠️ 破壞不可變性保證，僅用於內部遷移）
func (ml *MessageList) SetMsgs(msgs []Message) *MessageList {
        if ml == nil {
                return NewMessageList(msgs)
        }
        ml.msgs = msgs
        return ml
}

// TokenEstimate 預估消息列表的 token 數
func (ml *MessageList) TokenEstimate() int {
        if ml == nil {
                return 0
        }
        return estimateMessagesTokens(ml.msgs)
}

// ============================================================================
// TransformPipeline — 管線式消息變換
// ============================================================================

// StageLog 記錄單個管線階段的執行結果
type StageLog struct {
        Name      string
        InputLen  int
        OutputLen int
        Warnings  []string
}

func (sl StageLog) String() string {
        if len(sl.Warnings) > 0 {
                return fmt.Sprintf("%s: %d→%d (%d warnings)", sl.Name, sl.InputLen, sl.OutputLen, len(sl.Warnings))
        }
        return fmt.Sprintf("%s: %d→%d", sl.Name, sl.InputLen, sl.OutputLen)
}

// PipelineStage 管線中的一個變換階段
type PipelineStage struct {
        Name       string
        Transform func(*MessageList) *MessageList
        Validate  bool // 是否在階段後驗證不變量
}

// PipelineResult 管線執行結果
type PipelineResult struct {
        Messages *MessageList
        Stages   []StageLog
        Errors   []string
}

// TransformPipeline 消息變換管線
type TransformPipeline struct {
        stages []PipelineStage
        ml     *MessageList
}

// NewPipeline 創建新的變換管線
func NewPipeline(ml *MessageList) *TransformPipeline {
        return &TransformPipeline{
                ml:     ml,
                stages: make([]PipelineStage, 0),
        }
}

// Stage 添加一個變換階段
func (p *TransformPipeline) Stage(name string, transform func(*MessageList) *MessageList) *TransformPipeline {
        p.stages = append(p.stages, PipelineStage{
                Name:       name,
                Transform:  transform,
                Validate:   false,
        })
        return p
}

// StageWithValidation 添加一個帶不變量驗證的變換階段
func (p *TransformPipeline) StageWithValidation(name string, transform func(*MessageList) *MessageList) *TransformPipeline {
        p.stages = append(p.stages, PipelineStage{
                Name:       name,
                Transform:  transform,
                Validate:   true,
        })
        return p
}

// Execute 執行管線
func (p *TransformPipeline) Execute() *PipelineResult {
        result := &PipelineResult{
                Stages: make([]StageLog, 0, len(p.stages)),
        }

        current := p.ml
        for _, stage := range p.stages {
                inputLen := current.Len()

                // panic 恢復：防止單個階段崩潰導致整個 pipeline 失敗
                transformed := func() (result *MessageList) {
                        defer func() {
                                if r := recover(); r != nil {
                                        log.Printf("[Pipeline] Stage '%s' panicked: %v\n%s", stage.Name, r, debug.Stack())
                                }
                        }()
                        return stage.Transform(current)
                }()

                if transformed == nil {
                        log.Printf("[Pipeline] Stage '%s' returned nil, keeping previous result", stage.Name)
                        result.Stages = append(result.Stages, StageLog{Name: stage.Name, InputLen: inputLen, OutputLen: current.Len()})
                        result.Errors = append(result.Errors, fmt.Sprintf("stage '%s' returned nil", stage.Name))
                        continue
                }
                current = transformed
                outputLen := current.Len()

                stageLog := StageLog{
                        Name:      stage.Name,
                        InputLen:  inputLen,
                        OutputLen: outputLen,
                }

                // 階段級驗證
                if stage.Validate {
                        violations := current.Validate()
                        for _, v := range violations {
                                stageLog.Warnings = append(stageLog.Warnings, v.String())
                        }
                }

                result.Stages = append(result.Stages, stageLog)
                log.Printf("[Pipeline] %s", stageLog)
        }

        // 最終驗證 + 自動修復
        violations := current.Validate()
        if len(violations) > 0 {
                var violStrs []string
                for _, v := range violations {
                        violStrs = append(violStrs, v.String())
                        result.Errors = append(result.Errors, v.String())
                }
                log.Printf("[Pipeline] Final validate: %d violations: %s", len(violations), strings.Join(violStrs, "; "))

                // 自動修復：確保有用戶消息
                if !current.HasUser() {
                        current = current.EnsureUser()
                        log.Printf("[Pipeline] Auto-repair: EnsureUser applied, result has %d messages, hasUser=%v",
                                current.Len(), current.HasUser())
                }

                // 再次驗證
                remaining := current.Validate()
                if len(remaining) > 0 {
                        log.Printf("[Pipeline] WARNING: %d violations remain after repair", len(remaining))
                } else {
                        log.Printf("[Pipeline] All invariants satisfied after repair")
                }
        } else {
                log.Printf("[Pipeline] Final validate: OK (all invariants satisfied)")
        }

        result.Messages = current
        return result
}
