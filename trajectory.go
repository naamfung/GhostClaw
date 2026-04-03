package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

// TrajectoryManager 轨迹管理器
type TrajectoryManager struct {
	mu sync.RWMutex
	
	// 配置
	dataDir           string
	successFile       string
	failedFile        string
	
	// 状态
	trajectoryCount   int
	lastSaveTime      time.Time
}

// NewTrajectoryManager 创建新的轨迹管理器
func NewTrajectoryManager(dataDir string) *TrajectoryManager {
	manager := &TrajectoryManager{
		dataDir:      dataDir,
		successFile:  filepath.Join(dataDir, "trajectory_samples.jsonl"),
		failedFile:   filepath.Join(dataDir, "failed_trajectories.jsonl"),
	}
	
	// 确保目录存在
	os.MkdirAll(dataDir, 0755)
	
	return manager
}

// RecordTrajectory 记录对话轨迹
func (tm *TrajectoryManager) RecordTrajectory(messages []Message, success bool, modelUsed string, tokenUsage TokenUsage) *Trajectory {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	
	// 提取工具调用记录
	toolCalls := tm.extractToolCalls(messages)
	
	// 提取用户反馈（如果有）
	userFeedback := tm.extractUserFeedback(messages)
	
	// 计算对话持续时间
	duration := tm.calculateDuration(messages)
	
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
		TokenUsage:   tokenUsage,
		Metadata: map[string]interface{}{
			"message_count": len(messages),
			"tool_call_count": len(toolCalls),
		},
	}
	
	// 保存轨迹
	if err := tm.saveTrajectory(trajectory); err != nil {
		log.Printf("[TrajectoryManager] Failed to save trajectory: %v", err)
	}
	
	tm.trajectoryCount++
	tm.lastSaveTime = time.Now()
	
	log.Printf("[TrajectoryManager] Recorded trajectory: %s (success: %v, messages: %d, tools: %d)",
		trajectory.ID, trajectory.Success, len(trajectory.Messages), len(trajectory.ToolCalls))
	
	return trajectory
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
	
	return map[string]interface{}{
		"total_trajectories":   totalTrajectories,
		"success_count":        successCount,
		"failed_count":         failedCount,
		"success_rate":         float64(successCount) / float64(totalTrajectories),
		"average_messages":     averageMessages,
		"average_tool_calls":   averageToolCalls,
		"average_duration":     averageDuration,
		"average_tokens":       averageTokens,
		"model_usage":          modelUsage,
		"tool_usage":           toolUsage,
		"last_save_time":       tm.lastSaveTime,
	}
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

// GetTrajectoryManager 获取轨迹管理器
func GetTrajectoryManager() *TrajectoryManager {
	return globalTrajectoryManager
}
