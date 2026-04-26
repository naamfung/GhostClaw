package main

import (
	"context"
	"fmt"

	"github.com/toon-format/toon-go"
)

// cronAddUsage 返回 cron_add 的正確使用格式（出錯時附帶）
const cronAddUsage = `
=== cron_add 正確使用格式 ===
參數：
  name (string, 必填):  任務名稱，如 "每日AI論文速遞"
  schedule (string, 必填): cron 表達式（6 字段：秒 分 時 日 月 週）
    常用示例：
      "0 0 9 * * *"     — 每天 09:00
      "0 30 17 * * *"   — 每天 17:30
      "0 0 9 * * 1-5"   — 工作日 09:00
      "0 0 12 * * 1"    — 每週一 12:00
      "0 0 8 1 * *"     — 每月 1 號 08:00
  content (string, 必填): 到時執行的自然語言指令
  channel (object, 選填): 輸出目標，默認日誌
    {"type": "log"}  或  {"type": "email", "recipients": ["a@b.com"]}

調用示例：
  cron_add(name="每日AI論文速遞", schedule="0 0 17 * * *", content="去arXiv查看最新AI論文並匯總")
=== 格式結束 ===`

// handleCronAdd 添加定时任务
func handleCronAdd(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
	if globalCronManager == nil {
		return "Error: cron manager not initialized" + cronAddUsage, false
	}

	name, ok := argsMap["name"].(string)
	if !ok || name == "" {
		return "Error: missing or invalid 'name' — 必須提供任務名稱" + cronAddUsage, false
	}

	schedule, ok := argsMap["schedule"].(string)
	if !ok || schedule == "" {
		return fmt.Sprintf("Error: missing or invalid 'schedule' — 任務「%s」需要有效的 cron 表達式%s", name, cronAddUsage), false
	}

	// 兼容舊參數名 user_message 和新參數名 content
	userMsg, ok := argsMap["content"].(string)
	if !ok || userMsg == "" {
		userMsg, ok = argsMap["user_message"].(string)
	}
	if !ok || userMsg == "" {
		return fmt.Sprintf("Error: missing 'content' — 任務「%s」需要指定執行指令%s", name, cronAddUsage), false
	}

	// 解析 category 參數
	category := "scheduled"
	if cat, ok := argsMap["category"].(string); ok && cat != "" {
		if cat != "heartbeat" && cat != "scheduled" {
			return fmt.Sprintf("Error: category must be 'heartbeat' or 'scheduled', got %s%s", cat, cronAddUsage), false
		}
		category = cat
	}

	// 解析 channel 配置
	var channelConf ChannelConf
	if chConf, ok := argsMap["channel"]; ok {
		switch v := chConf.(type) {
		case map[string]interface{}:
			channelConf = parseChannelConf(v)
		case string:
			if err := toon.Unmarshal([]byte(v), &channelConf); err != nil {
				return fmt.Sprintf("Error parsing channel config: %v%s", err, cronAddUsage), false
			}
		default:
			return "Error: channel config must be object or TOON string" + cronAddUsage, false
		}
	} else {
		channelConf = ChannelConf{Type: "log"}
	}

	sessionID := ""
	if ch != nil {
		sessionID = ch.GetSessionID()
	}

	job := &CronJob{
		Name:        name,
		Schedule:    schedule,
		UserMessage: userMsg,
		Channel:     channelConf,
		SessionID:   sessionID,
		Category:    category,
	}

	if err := globalCronManager.AddJob(job); err != nil {
		return fmt.Sprintf("Error adding job: %v%s", err, cronAddUsage), false
	}
	return fmt.Sprintf("✅ 定時任務已添加\n  名稱: %s\n  排程: %s\n  類型: %s", name, schedule, category), false
}

// handleCronRemove 刪除任務
func handleCronRemove(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
	if globalCronManager == nil {
		return "Error: cron manager not initialized", false
	}

	name, ok := argsMap["name"].(string)
	if !ok || name == "" {
		return `Error: missing or invalid 'name'
=== cron_remove 正確使用格式 ===
參數：
  name (string, 必填): 要刪除的任務名稱
提示：先用 cron_list 查看所有任務名稱
示例：cron_remove(name="每日AI論文速遞")
=== 格式結束 ===`, false
	}

	// 檢查任務是否存在
	jobs := globalCronManager.ListJobs()
	found := false
	for _, j := range jobs {
		if j.Name == name {
			found = true
			break
		}
	}
	if !found {
		return fmt.Sprintf(`Error: 未找到任務「%s」

=== 當前任務列表 ===
先用 cron_list 查看所有任務
=== 提示結束 ===`, name), false
	}

	wasRunning := globalCronManager.IsJobRunning(name)

	if err := globalCronManager.RemoveJob(name); err != nil {
		return fmt.Sprintf("Error removing job: %v", err), false
	}

	if wasRunning {
		return fmt.Sprintf("✅ 任務「%s」已刪除（正在執行的任務已終止）", name), false
	}
	return fmt.Sprintf("✅ 任務「%s」已刪除", name), false
}

// handleCronList 列出所有任務
func handleCronList(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
	if globalCronManager == nil {
		return "Error: cron manager not initialized", false
	}

	jobs := globalCronManager.ListJobs()
	if len(jobs) == 0 {
		return `📭 當前沒有定時任務

=== 添加任務 ===
使用 cron_add 創建新任務：
  cron_add(name="任務名", schedule="0 0 9 * * *", content="要執行的指令")
=== 提示結束 ===`, false
	}
	data, err := toon.Marshal(jobs)
	if err != nil {
		return fmt.Sprintf("Error marshaling jobs: %v", err), false
	}
	return string(data), false
}

// handleCronStatus 查詢任務狀態
func handleCronStatus(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
	if globalCronManager == nil {
		return "Error: cron manager not initialized", false
	}

	name, ok := argsMap["name"].(string)
	if !ok || name == "" {
		return `Error: missing or invalid 'name'
=== cron_status 正確使用格式 ===
參數：
  name (string, 必填): 要查詢的任務名稱
示例：cron_status(name="每日AI論文速遞")
=== 格式結束 ===`, false
	}
	status, err := globalCronManager.GetJobStatus(name)
	if err != nil {
		return fmt.Sprintf(`Error: %v

=== 提示 ===
先用 cron_list 確認任務名稱是否正確
=== 提示結束 ===`, err), false
	}
	data, err := toon.Marshal(status)
	if err != nil {
		return fmt.Sprintf("Error marshaling status: %v", err), false
	}
	return string(data), false
}

// parseChannelConf 從 map 解析 ChannelConf
func parseChannelConf(m map[string]interface{}) ChannelConf {
	conf := ChannelConf{}
	if typ, ok := m["Type"].(string); ok {
		conf.Type = typ
	} else if typ, ok := m["type"].(string); ok {
		conf.Type = typ
	}
	if recipients, ok := m["Recipients"].([]interface{}); ok {
		for _, r := range recipients {
			if s, ok := r.(string); ok {
				conf.Recipients = append(conf.Recipients, s)
			}
		}
	} else if recipients, ok := m["recipients"].([]interface{}); ok {
		for _, r := range recipients {
			if s, ok := r.(string); ok {
				conf.Recipients = append(conf.Recipients, s)
			}
		}
	}
	if subs, ok := m["SubChannels"].([]interface{}); ok {
		for _, sub := range subs {
			if subMap, ok := sub.(map[string]interface{}); ok {
				conf.SubChannels = append(conf.SubChannels, parseChannelConf(subMap))
			}
		}
	} else if subs, ok := m["sub_channels"].([]interface{}); ok {
		for _, sub := range subs {
			if subMap, ok := sub.(map[string]interface{}); ok {
				conf.SubChannels = append(conf.SubChannels, parseChannelConf(subMap))
			}
		}
	}
	return conf
}
