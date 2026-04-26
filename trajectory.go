package main

import (
        "encoding/json"
        "errors"
        "fmt"
        "log"
        "os"
        "path/filepath"
        "sort"
        "strings"
        "sync"
        "time"
)

// Trajectory 对话轨迹
type Trajectory struct {
        ID            string    `json:"id"`
        SessionID     string    `json:"session_id"`
        Messages      []Message `json:"messages"`
        Success       bool      `json:"success"`
        UserFeedback  int       `json:"user_feedback"`  // 1-5 星评分
        ToolCalls     []ToolCall `json:"tool_calls"`
        Timestamp     time.Time `json:"timestamp"`
        Duration      int       `json:"duration"`     // 对话持续时间（秒）
        ModelUsed     string    `json:"model_used"`  // 使用的模型
        TokenUsage    TokenUsage `json:"token_usage"` // Token 使用情况
        Metadata      map[string]interface{} `json:"metadata"`
        Turns         []TurnRecord `json:"turns,omitempty"` // Per-turn token tracking (P0-2)
}

// TokenUsage Token 使用情况
type TokenUsage struct {
        PromptTokens     int `json:"prompt_tokens"`
        CompletionTokens int `json:"completion_tokens"`
        TotalTokens      int `json:"total_tokens"`
}

// ToolCall 工具调用记录
type ToolCall struct {
        FunctionName string                 `json:"function_name"`
        Arguments    map[string]interface{} `json:"arguments"`
        Result       string                 `json:"result"`
        Success      bool                   `json:"success"`
        Timestamp    time.Time              `json:"timestamp"`
}

// TurnRecord captures per-message-turn information for fine-grained analysis.
type TurnRecord struct {
        TurnIndex   int        `json:"turn_index"`
        Role        string     `json:"role"`
        TokenUsage  TokenUsage `json:"token_usage"`
        ToolName    string     `json:"tool_name,omitempty"`    // If this turn involved a tool call
        ToolSuccess bool       `json:"tool_success,omitempty"` // Whether the tool call succeeded
        DurationMs  int64      `json:"duration_ms"`            // Time taken for this turn (milliseconds)
}

// SFTSample represents a single SFT (Supervised Fine-Tuning) training sample.
type SFTSample struct {
        Messages  []SFTMessage           `json:"messages"`
        ToolCalls []SFTToolCallRecord     `json:"tool_calls,omitempty"`
        Score     float64                `json:"score"`
        Metadata  map[string]interface{} `json:"metadata"`
}

// SFTMessage is a simplified message representation for SFT training.
type SFTMessage struct {
        Role    string `json:"role"`
        Content string `json:"content"`
}

// SFTToolCallRecord is a structured tool call representation for training data export.
type SFTToolCallRecord struct {
        FunctionName string `json:"function_name"`
        Arguments    string `json:"arguments"`
        Result       string `json:"result"`
        Success      bool   `json:"success"`
}

// RLTrainingItem represents a single RL (Reinforcement Learning) training item.
type RLTrainingItem struct {
        TrajectoryID string       `json:"trajectory_id"`
        Messages     []SFTMessage `json:"messages"`
        Score        float64      `json:"score"`       // Derived from user_feedback or success
        TokenUsage   TokenUsage   `json:"token_usage"`
        ModelUsed    string       `json:"model_used"`
}

// TrajectoryManager 轨迹管理器
type TrajectoryManager struct {
        mu sync.RWMutex

        // 配置
        dataDir     string
        successFile string
        failedFile  string

        // 状态
        trajectoryCount int
        lastSaveTime    time.Time
}

// NewTrajectoryManager 创建新的轨迹管理器
func NewTrajectoryManager(dataDir string) *TrajectoryManager {
        manager := &TrajectoryManager{
                dataDir:     dataDir,
                successFile: filepath.Join(dataDir, "trajectory_samples.jsonl"),
                failedFile:  filepath.Join(dataDir, "failed_trajectories.jsonl"),
        }

        // 确保目录存在
        os.MkdirAll(dataDir, 0755)

        return manager
}

// RecordTrajectory 记录对话轨迹 (backward compatible - creates empty Turns slice)
func (tm *TrajectoryManager) RecordTrajectory(messages []Message, success bool, modelUsed string, tokenUsage TokenUsage) *Trajectory {
        return tm.RecordTrajectoryWithTurns(messages, nil, success, modelUsed, tokenUsage)
}

// RecordTrajectoryWithTurns 记录对话轨迹，包含逐轮 (per-turn) 数据
func (tm *TrajectoryManager) RecordTrajectoryWithTurns(messages []Message, turns []TurnRecord, success bool, modelUsed string, totalTokenUsage TokenUsage) *Trajectory {
        tm.mu.Lock()
        defer tm.mu.Unlock()

        // 提取工具调用记录
        toolCalls := tm.extractToolCalls(messages)

        // 提取用户反馈（如果有）
        userFeedback := tm.extractUserFeedback(messages)

        // 计算对话持续时间
        duration := tm.calculateDuration(messages)

        // Normalize turns: if nil, use empty slice
        if turns == nil {
                turns = []TurnRecord{}
        }

        trajectory := &Trajectory{
                ID:           generateTrajectoryID(),
                SessionID:    "default",
                Messages:     messages,
                Success:      success,
                UserFeedback: userFeedback,
                ToolCalls:    toolCalls,
                Timestamp:    time.Now(),
                Duration:     duration,
                ModelUsed:    modelUsed,
                TokenUsage:   totalTokenUsage,
                Turns:        turns,
                Metadata: map[string]interface{}{
                        "message_count":    len(messages),
                        "tool_call_count":  len(toolCalls),
                        "turn_count":       len(turns),
                        "has_turn_details": len(turns) > 0,
                },
        }

        // 保存轨迹
        if err := tm.saveTrajectory(trajectory); err != nil {
                log.Printf("[TrajectoryManager] Failed to save trajectory: %v", err)
        }

        tm.trajectoryCount++
        tm.lastSaveTime = time.Now()

        log.Printf("[TrajectoryManager] Recorded trajectory: %s (success: %v, messages: %d, tools: %d, turns: %d)",
                trajectory.ID, trajectory.Success, len(trajectory.Messages), len(trajectory.ToolCalls), len(trajectory.Turns))

        return trajectory
}

// ExportSFTSamples exports successful trajectories as SFT (Supervised Fine-Tuning) training samples.
// If limit <= 0, all successful trajectories are exported.
func (tm *TrajectoryManager) ExportSFTSamples(limit int) ([]SFTSample, error) {
        tm.mu.RLock()
        defer tm.mu.RUnlock()

        successTrajectories, err := tm.loadTrajectories(tm.successFile)
        if err != nil {
                return nil, fmt.Errorf("failed to load success trajectories: %w", err)
        }

        // Sort by timestamp descending (most recent first)
        sort.Slice(successTrajectories, func(i, j int) bool {
                return successTrajectories[i].Timestamp.After(successTrajectories[j].Timestamp)
        })

        // Apply limit
        if limit > 0 && len(successTrajectories) > limit {
                successTrajectories = successTrajectories[:limit]
        }

        samples := make([]SFTSample, 0, len(successTrajectories))
        for _, traj := range successTrajectories {
                sample, convertErr := tm.trajectoryToSFTSample(traj)
                if convertErr != nil {
                        log.Printf("[TrajectoryManager] Skipping trajectory %s: %v", traj.ID, convertErr)
                        continue
                }
                samples = append(samples, sample)
        }

        return samples, nil
}

// trajectoryToSFTSample converts a single Trajectory into an SFTSample.
func (tm *TrajectoryManager) trajectoryToSFTSample(traj Trajectory) (SFTSample, error) {
        // Convert messages to SFT format
        sftMessages := make([]SFTMessage, 0, len(traj.Messages))
        for _, msg := range traj.Messages {
                contentStr := tm.messageContentToString(msg.Content)
                // Skip empty content messages
                if contentStr == "" {
                        continue
                }
                sftMessages = append(sftMessages, SFTMessage{
                        Role:    msg.Role,
                        Content: contentStr,
                })
        }

        if len(sftMessages) == 0 {
                return SFTSample{}, errors.New("no convertible messages in trajectory")
        }

        // Convert tool calls to export format
        toolCallRecords := make([]SFTToolCallRecord, 0, len(traj.ToolCalls))
        for _, tc := range traj.ToolCalls {
                argsJSON := "{}"
                if tc.Arguments != nil {
                        if argsBytes, err := json.Marshal(tc.Arguments); err == nil {
                                argsJSON = string(argsBytes)
                        }
                }
                toolCallRecords = append(toolCallRecords, SFTToolCallRecord{
                        FunctionName: tc.FunctionName,
                        Arguments:    argsJSON,
                        Result:       tc.Result,
                        Success:      tc.Success,
                })
        }

        // Compute score: use user_feedback if available (normalize 1-5 to 0.0-1.0), otherwise 1.0 for successful trajectories
        score := 1.0
        if traj.UserFeedback > 0 {
                score = float64(traj.UserFeedback) / 5.0
        }

        metadata := map[string]interface{}{
                "trajectory_id": traj.ID,
                "session_id":    traj.SessionID,
                "model_used":    traj.ModelUsed,
                "duration":      traj.Duration,
                "message_count": len(traj.Messages),
                "tool_count":    len(traj.ToolCalls),
        }
        if len(traj.Turns) > 0 {
                metadata["turn_count"] = len(traj.Turns)
        }

        return SFTSample{
                Messages:  sftMessages,
                ToolCalls: toolCallRecords,
                Score:     score,
                Metadata:  metadata,
        }, nil
}

// ExportRLData exports trajectory data in a format compatible with RL training pipelines.
// Both successful and failed trajectories are included, with scores derived from
// user feedback or success/failure status.
// If limit <= 0, all trajectories are exported.
func (tm *TrajectoryManager) ExportRLData(limit int) ([]RLTrainingItem, error) {
        tm.mu.RLock()
        defer tm.mu.RUnlock()

        successTrajectories, _ := tm.loadTrajectories(tm.successFile)
        failedTrajectories, _ := tm.loadTrajectories(tm.failedFile)

        allTrajectories := append(successTrajectories, failedTrajectories...)

        // Sort by timestamp descending (most recent first)
        sort.Slice(allTrajectories, func(i, j int) bool {
                return allTrajectories[i].Timestamp.After(allTrajectories[j].Timestamp)
        })

        // Apply limit
        if limit > 0 && len(allTrajectories) > limit {
                allTrajectories = allTrajectories[:limit]
        }

        items := make([]RLTrainingItem, 0, len(allTrajectories))
        for _, traj := range allTrajectories {
                item, convertErr := tm.trajectoryToRLItem(traj)
                if convertErr != nil {
                        log.Printf("[TrajectoryManager] Skipping trajectory %s for RL export: %v", traj.ID, convertErr)
                        continue
                }
                items = append(items, item)
        }

        return items, nil
}

// trajectoryToRLItem converts a single Trajectory into an RLTrainingItem.
func (tm *TrajectoryManager) trajectoryToRLItem(traj Trajectory) (RLTrainingItem, error) {
        // Convert messages to SFT format
        sftMessages := make([]SFTMessage, 0, len(traj.Messages))
        for _, msg := range traj.Messages {
                contentStr := tm.messageContentToString(msg.Content)
                if contentStr == "" {
                        continue
                }
                sftMessages = append(sftMessages, SFTMessage{
                        Role:    msg.Role,
                        Content: contentStr,
                })
        }

        if len(sftMessages) == 0 {
                return RLTrainingItem{}, errors.New("no convertible messages in trajectory")
        }

        // Derive score: user_feedback takes priority, then success/failure
        score := 0.0
        if traj.UserFeedback > 0 {
                score = float64(traj.UserFeedback) / 5.0
        } else if traj.Success {
                score = 1.0
        } else {
                score = 0.0
        }

        return RLTrainingItem{
                TrajectoryID: traj.ID,
                Messages:     sftMessages,
                Score:        score,
                TokenUsage:   traj.TokenUsage,
                ModelUsed:    traj.ModelUsed,
        }, nil
}

// GetTurnStats returns per-turn statistics across all loaded trajectories.
// Works correctly even when trajectories have no turn data (backward compatible).
func (tm *TrajectoryManager) GetTurnStats() map[string]interface{} {
        tm.mu.RLock()
        defer tm.mu.RUnlock()

        successTrajectories, _ := tm.loadTrajectories(tm.successFile)
        failedTrajectories, _ := tm.loadTrajectories(tm.failedFile)
        allTrajectories := append(successTrajectories, failedTrajectories...)

        stats := map[string]interface{}{
                "total_trajectories_with_turns": 0,
                "total_turns":                   0,
                "avg_tokens_per_turn":           0.0,
                "avg_prompt_tokens_per_turn":    0.0,
                "avg_completion_tokens_per_turn": 0.0,
                "avg_duration_ms_per_turn":      0.0,
                "tool_success_rate":             0.0,
                "total_tool_turns":              0,
                "successful_tool_turns":         0,
                "role_distribution":             map[string]int{},
                "per_turn_token_avg":            []interface{}{},
        }

        // Filter trajectories that have turn data
        trajectoriesWithTurns := make([]Trajectory, 0)
        for _, t := range allTrajectories {
                if len(t.Turns) > 0 {
                        trajectoriesWithTurns = append(trajectoriesWithTurns, t)
                }
        }

        stats["total_trajectories_with_turns"] = len(trajectoriesWithTurns)

        if len(trajectoriesWithTurns) == 0 {
                return stats
        }

        // Collect all turns
        var allTurns []TurnRecord
        for _, t := range trajectoriesWithTurns {
                allTurns = append(allTurns, t.Turns...)
        }

        totalTurns := len(allTurns)
        stats["total_turns"] = totalTurns

        if totalTurns == 0 {
                return stats
        }

        // Aggregate totals
        var totalTokens, totalPromptTokens, totalCompletionTokens, totalDurationMs int64
        var totalToolTurns, successfulToolTurns int
        roleDistribution := make(map[string]int)

        // Per-turn index aggregation: collect tokens by turn index
        turnIndexTokens := make(map[int][]int)
        turnIndexCount := make(map[int]int)

        for _, turn := range allTurns {
                totalTokens += int64(turn.TokenUsage.TotalTokens)
                totalPromptTokens += int64(turn.TokenUsage.PromptTokens)
                totalCompletionTokens += int64(turn.TokenUsage.CompletionTokens)
                totalDurationMs += turn.DurationMs
                roleDistribution[turn.Role]++

                turnIndexCount[turn.TurnIndex]++

                if turn.ToolName != "" {
                        totalToolTurns++
                        if turn.ToolSuccess {
                                successfulToolTurns++
                        }
                }

                turnIndexTokens[turn.TurnIndex] = append(turnIndexTokens[turn.TurnIndex], turn.TokenUsage.TotalTokens)
        }

        stats["avg_tokens_per_turn"] = float64(totalTokens) / float64(totalTurns)
        stats["avg_prompt_tokens_per_turn"] = float64(totalPromptTokens) / float64(totalTurns)
        stats["avg_completion_tokens_per_turn"] = float64(totalCompletionTokens) / float64(totalTurns)
        stats["avg_duration_ms_per_turn"] = float64(totalDurationMs) / float64(totalTurns)
        stats["total_tool_turns"] = totalToolTurns
        stats["successful_tool_turns"] = successfulToolTurns
        stats["role_distribution"] = roleDistribution

        if totalToolTurns > 0 {
                stats["tool_success_rate"] = float64(successfulToolTurns) / float64(totalToolTurns)
        }

        // Build per-turn average tokens array
        maxTurnIdx := 0
        for idx := range turnIndexCount {
                if idx > maxTurnIdx {
                        maxTurnIdx = idx
                }
        }

        perTurnAvg := make([]interface{}, 0, maxTurnIdx+1)
        for i := 0; i <= maxTurnIdx; i++ {
                count := turnIndexCount[i]
                if count == 0 {
                        perTurnAvg = append(perTurnAvg, map[string]interface{}{
                                "turn_index":       i,
                                "avg_total_tokens": 0.0,
                                "turn_count":       0,
                        })
                        continue
                }
                tokens := turnIndexTokens[i]
                sum := 0
                for _, t := range tokens {
                        sum += t
                }
                perTurnAvg = append(perTurnAvg, map[string]interface{}{
                        "turn_index":       i,
                        "avg_total_tokens": float64(sum) / float64(count),
                        "turn_count":       count,
                })
        }
        stats["per_turn_token_avg"] = perTurnAvg

        return stats
}

// messageContentToString converts a Message.Content (interface{}) to a plain string.
func (tm *TrajectoryManager) messageContentToString(content interface{}) string {
        if content == nil {
                return ""
        }
        switch v := content.(type) {
        case string:
                return v
        case []byte:
                return string(v)
        case fmt.Stringer:
                return v.String()
        default:
                // Fall back to JSON serialization for complex types
                if b, err := json.Marshal(content); err == nil {
                        return string(b)
                }
                return fmt.Sprintf("%v", content)
        }
}

// extractToolCalls 提取工具调用记录
func (tm *TrajectoryManager) extractToolCalls(messages []Message) []ToolCall {
        var toolCalls []ToolCall

        for _, msg := range messages {
                if msg.ToolCalls != nil {
                        // 尝试不同的类型断言
                        if toolCallsSlice, ok := msg.ToolCalls.([]ToolCall); ok {
                                for _, tc := range toolCallsSlice {
                                        toolCalls = append(toolCalls, tc)
                                }
                        } else if tcSlice, ok := msg.ToolCalls.([]interface{}); ok {
                                for _, tc := range tcSlice {
                                        if tcMap, ok := tc.(map[string]interface{}); ok {
                                                functionName := ""
                                                arguments := make(map[string]interface{})

                                                if function, ok := tcMap["function"].(map[string]interface{}); ok {
                                                        if name, ok := function["name"].(string); ok {
                                                                functionName = name
                                                        }
                                                        if args, ok := function["arguments"].(map[string]interface{}); ok {
                                                                arguments = args
                                                        }
                                                }

                                                if functionName != "" {
                                                        toolCalls = append(toolCalls, ToolCall{
                                                                FunctionName: functionName,
                                                                Arguments:    arguments,
                                                                Timestamp:    time.Now(),
                                                        })
                                                }
                                        }
                                }
                        }
                }
        }

        return toolCalls
}

// extractUserFeedback 提取用户反馈
func (tm *TrajectoryManager) extractUserFeedback(messages []Message) int {
        // 查找最近的反馈消息
        for i := len(messages) - 1; i >= 0; i-- {
                if messages[i].Role == "user" {
                        if content, ok := messages[i].Content.(string); ok {
                                // 尝试解析评分
                                rating := tm.parseRating(content)
                                if rating > 0 {
                                        return rating
                                }
                        }
                }
        }

        return 0
}

// parseRating 解析用户反馈评分
func (tm *TrajectoryManager) parseRating(content string) int {
        content = strings.ToLower(content)

        // 数字评分
        if strings.Contains(content, "分") {
                for _, c := range content {
                        if c >= '1' && c <= '5' {
                                return int(c - '0')
                        }
                }
        }

        // 文字评分
        if strings.Contains(content, "完美") || strings.Contains(content, "非常好") {
                return 5
        }
        if strings.Contains(content, "很好") || strings.Contains(content, "很有帮助") {
                return 4
        }
        if strings.Contains(content, "还可以") || strings.Contains(content, "一般") {
                return 3
        }
        if strings.Contains(content, "不太好") || strings.Contains(content, "不太对") {
                return 2
        }
        if strings.Contains(content, "没用") || strings.Contains(content, "完全不对") {
                return 1
        }

        return 0
}

// calculateDuration 计算对话持续时间
func (tm *TrajectoryManager) calculateDuration(messages []Message) int {
        if len(messages) < 2 {
                return 0
        }

        firstTime := time.Now()
        lastTime := time.Now()

        // 查找第一条消息的时间
        for _, msg := range messages {
                if msg.Timestamp > 0 {
                        t := time.Unix(msg.Timestamp, 0)
                        if t.Before(firstTime) {
                                firstTime = t
                        }
                        if t.After(lastTime) {
                                lastTime = t
                        }
                }
        }

        return int(lastTime.Sub(firstTime).Seconds())
}

// saveTrajectory 保存轨迹到文件
func (tm *TrajectoryManager) saveTrajectory(trajectory *Trajectory) error {
        // 序列化为 JSON
        data, err := json.Marshal(trajectory)
        if err != nil {
                return err
        }

        // 选择保存文件
        filename := tm.successFile
        if !trajectory.Success {
                filename = tm.failedFile
        }

        // 追加写入文件
        f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
        if err != nil {
                return err
        }
        defer f.Close()

        if _, err := f.WriteString(string(data) + "\n"); err != nil {
                return err
        }

        return nil
}

// GetTrajectoryStats 获取轨迹统计
func (tm *TrajectoryManager) GetTrajectoryStats() map[string]interface{} {
        tm.mu.RLock()
        defer tm.mu.RUnlock()

        // 读取所有轨迹
        successTrajectories, _ := tm.loadTrajectories(tm.successFile)
        failedTrajectories, _ := tm.loadTrajectories(tm.failedFile)

        allTrajectories := append(successTrajectories, failedTrajectories...)

        // 计算统计
        totalTrajectories := len(allTrajectories)
        successCount := len(successTrajectories)
        failedCount := len(failedTrajectories)

        var totalMessages, totalToolCalls, totalDuration int
        var totalTokens int

        for _, t := range allTrajectories {
                totalMessages += len(t.Messages)
                totalToolCalls += len(t.ToolCalls)
                totalDuration += t.Duration
                totalTokens += t.TokenUsage.TotalTokens
        }

        averageMessages := 0.0
        averageToolCalls := 0.0
        averageDuration := 0.0
        averageTokens := 0.0

        if totalTrajectories > 0 {
                averageMessages = float64(totalMessages) / float64(totalTrajectories)
                averageToolCalls = float64(totalToolCalls) / float64(totalTrajectories)
                averageDuration = float64(totalDuration) / float64(totalTrajectories)
                averageTokens = float64(totalTokens) / float64(totalTrajectories)
        }

        // 模型使用统计
        modelUsage := make(map[string]int)
        for _, t := range allTrajectories {
                if t.ModelUsed != "" {
                        modelUsage[t.ModelUsed]++
                }
        }

        // 工具使用统计
        toolUsage := make(map[string]int)
        for _, t := range allTrajectories {
                for _, tc := range t.ToolCalls {
                        toolUsage[tc.FunctionName]++
                }
        }

        // Turn tracking stats
        trajectoriesWithTurns := 0
        totalTurns := 0
        for _, t := range allTrajectories {
                if len(t.Turns) > 0 {
                        trajectoriesWithTurns++
                        totalTurns += len(t.Turns)
                }
        }

        result := map[string]interface{}{
                "total_trajectories":            totalTrajectories,
                "success_count":                 successCount,
                "failed_count":                  failedCount,
                "success_rate":                  float64(successCount) / float64(totalTrajectories),
                "average_messages":              averageMessages,
                "average_tool_calls":            averageToolCalls,
                "average_duration":              averageDuration,
                "average_tokens":                averageTokens,
                "model_usage":                   modelUsage,
                "tool_usage":                    toolUsage,
                "last_save_time":                tm.lastSaveTime,
                "trajectories_with_turn_data":   trajectoriesWithTurns,
                "total_turns_recorded":          totalTurns,
        }

        // Avoid division by zero for success_rate
        if totalTrajectories == 0 {
                result["success_rate"] = 0.0
        }

        return result
}

// loadTrajectories 加载轨迹文件
func (tm *TrajectoryManager) loadTrajectories(filename string) ([]Trajectory, error) {
        data, err := os.ReadFile(filename)
        if err != nil {
                if os.IsNotExist(err) {
                        return []Trajectory{}, nil
                }
                return nil, err
        }

        var trajectories []Trajectory
        lines := strings.Split(string(data), "\n")

        for _, line := range lines {
                line = strings.TrimSpace(line)
                if line == "" {
                        continue
                }

                var trajectory Trajectory
                if err := json.Unmarshal([]byte(line), &trajectory); err != nil {
                        continue // 跳过无效记录
                }
                trajectories = append(trajectories, trajectory)
        }

        return trajectories, nil
}

// generateTrajectoryID 生成轨迹 ID
func generateTrajectoryID() string {
        return fmt.Sprintf("traj_%d", time.Now().UnixNano())
}

// ========== 全局实例 ==========
var globalTrajectoryManager *TrajectoryManager

// InitTrajectoryManager 初始化轨迹管理器
func InitTrajectoryManager(dataDir string) {
        if globalTrajectoryManager == nil {
                globalTrajectoryManager = NewTrajectoryManager(dataDir)
                log.Println("[TrajectoryManager] Initialized")
        }
}

// GetTrajectories 获取轨迹数据
func (tm *TrajectoryManager) GetTrajectories() ([]Trajectory, error) {
        tm.mu.RLock()
        defer tm.mu.RUnlock()

        successTrajectories, _ := tm.loadTrajectories(tm.successFile)
        failedTrajectories, _ := tm.loadTrajectories(tm.failedFile)
        allTrajectories := append(successTrajectories, failedTrajectories...)

        return allTrajectories, nil
}

// GetToolSuccessRates 获取工具成功率数据
func (tm *TrajectoryManager) GetToolSuccessRates() map[string]float64 {
        tm.mu.RLock()
        defer tm.mu.RUnlock()

        trajectories, err := tm.GetTrajectories()
        if err != nil {
                return make(map[string]float64)
        }

        toolSuccessCount := make(map[string]int)
        toolTotalCount := make(map[string]int)

        for _, t := range trajectories {
                for _, tc := range t.ToolCalls {
                        toolTotalCount[tc.FunctionName]++
                        if tc.Success {
                                toolSuccessCount[tc.FunctionName]++
                        }
                }
        }

        toolSuccessRates := make(map[string]float64)
        for tool, total := range toolTotalCount {
                success := toolSuccessCount[tool]
                if total > 0 {
                        toolSuccessRates[tool] = float64(success) / float64(total)
                } else {
                        toolSuccessRates[tool] = 0.0
                }
        }

        return toolSuccessRates
}

// GetTrajectoryManager 获取轨迹管理器
func GetTrajectoryManager() *TrajectoryManager {
        return globalTrajectoryManager
}
