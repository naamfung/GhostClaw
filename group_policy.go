package main

import (
    "strings"
)

// ShouldRespondInGroup 根据全局配置判断是否应在群聊中响应
// cfg: 全局群聊配置（来自 config.GroupChat）
// channelID: 群聊标识（如 Telegram 群 ID）
// messageText: 消息内容
// botID: 机器人的用户标识（用于检测 @ 提及）
func ShouldRespondInGroup(cfg *GroupChatConfig, channelID, messageText, botID string) bool {
    if cfg == nil {
        // 默认策略：仅当被 @ 时响应
        return strings.Contains(messageText, "@"+botID)
    }
    switch cfg.DefaultPolicy {
    case "open":
        return true
    case "mention":
        // 检查是否被 @ 提及
        if botID != "" && strings.Contains(messageText, "@"+botID) {
            return true
        }
        // 也检查消息中是否包含机器人的名字（简单实现）
        return false
    case "allowlist":
        for _, id := range cfg.AllowList {
            if id == channelID {
                return true
            }
        }
        return false
    default:
        return false
    }
}
