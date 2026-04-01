package main

import (
        "context"
        "fmt"

        "github.com/toon-format/toon-go"
)

// handleCronAdd 添加定时任务
func handleCronAdd(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
    if globalCronManager == nil {
        return "Error: cron manager not initialized", false
    }

    name, ok := argsMap["name"].(string)
    if !ok || name == "" {
        return "Error: missing or invalid 'name'", false
    }
    schedule, ok := argsMap["schedule"].(string)
    if !ok || schedule == "" {
        return "Error: missing or invalid 'schedule'", false
    }
    userMsg, ok := argsMap["user_message"].(string)
    if !ok || userMsg == "" {
        return "Error: missing or invalid 'user_message'", false
    }

    // 解析 category 参数
    category := "scheduled"
    if cat, ok := argsMap["category"].(string); ok && cat != "" {
        if cat != "heartbeat" && cat != "scheduled" {
            return fmt.Sprintf("Error: category must be 'heartbeat' or 'scheduled', got %s", cat), false
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
                return fmt.Sprintf("Error parsing channel config: %v", err), false
            }
        default:
            return "Error: channel config must be object or TOON string", false
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
        return fmt.Sprintf("Error adding job: %v", err), false
    }
    return fmt.Sprintf("Job '%s' added successfully. Schedule: %s\nCategory: %s", name, schedule, category), false
}

// handleCronRemove 删除任务
func handleCronRemove(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        // 检查定时任务管理器是否已初始化
        if globalCronManager == nil {
                return "Error: cron manager not initialized", false
        }

        name, ok := argsMap["name"].(string)
        if !ok || name == "" {
                return "Error: missing or invalid 'name'", false
        }

        // 检查任务是否正在运行
        wasRunning := globalCronManager.IsJobRunning(name)

        if err := globalCronManager.RemoveJob(name); err != nil {
                return fmt.Sprintf("Error removing job: %v", err), false
        }

        if wasRunning {
                return fmt.Sprintf("Job '%s' removed. The running task has been terminated.", name), false
        }
        return fmt.Sprintf("Job '%s' removed.", name), false
}

// handleCronList 列出所有任务
func handleCronList(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        // 检查定时任务管理器是否已初始化
        if globalCronManager == nil {
                return "Error: cron manager not initialized", false
        }

        jobs := globalCronManager.ListJobs()
        if len(jobs) == 0 {
                return "No cron jobs configured.", false
        }
        data, err := toon.Marshal(jobs)
        if err != nil {
                return fmt.Sprintf("Error marshaling jobs: %v", err), false
        }
        return string(data), false
}

// handleCronStatus 查询任务状态
func handleCronStatus(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        // 检查定时任务管理器是否已初始化
        if globalCronManager == nil {
                return "Error: cron manager not initialized", false
        }

        name, ok := argsMap["name"].(string)
        if !ok || name == "" {
                return "Error: missing or invalid 'name'", false
        }
        status, err := globalCronManager.GetJobStatus(name)
        if err != nil {
                return fmt.Sprintf("Error: %v", err), false
        }
        data, err := toon.Marshal(status)
        if err != nil {
                return fmt.Sprintf("Error marshaling status: %v", err), false
        }
        return string(data), false
}

// parseChannelConf 从 map 解析 ChannelConf
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

