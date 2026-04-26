package main

import (
        "log"
        "sync"
        "time"
)

// ============================================================
// 消息总线 - 轻量级事件分发系统
// ============================================================
//
// 设计目标：
// 1. 统一通知入口 - 心跳、子代理、定时任务都通过总线发消息
// 2. 渠道订阅机制 - 各渠道自己决定订阅哪些事件
// 3. 用户路由 - 消息可以路由到特定用户所在的渠道
// 4. 与现有频道融合 - 不改变现有 Channel 接口，只增加订阅逻辑
//
// 使用示例：
//
//      // 订阅事件
//      bus.Subscribe("heartbeat", "telegram_user_123", func(e Event) {
//          // 处理心跳事件
//      })
//
//      // 发布事件
//      bus.Publish(Event{
//          Type:    "heartbeat",
//          Topic:   "alert",
//          Payload: map[string]interface{}{"message": "磁盘空间不足"},
//      })
//
//      // 定向发送消息到用户
//      bus.SendToUser("telegram_user_123", "任务完成！")
// ============================================================

// EventType 事件类型
type EventType string

const (
        EventHeartbeat    EventType = "heartbeat"    // 心跳事件
        EventSubagent     EventType = "subagent"     // 子代理事件
        EventCron         EventType = "cron"         // 定时任务事件
        EventNotification EventType = "notification" // 通用通知
        EventSystem       EventType = "system"       // 系统事件
        EventDelayedTask  EventType = "delayed_task" // 后台延迟任务唤醒事件
)

// Event 消息总线事件
type Event struct {
        Type      EventType              `json:"type"`       // 事件类型
        Topic     string                 `json:"topic"`      // 事件主题（如 "alert", "info", "result"）
        Payload   map[string]interface{} `json:"payload"`    // 事件负载
        UserID    string                 `json:"user_id"`    // 目标用户ID（可选，用于路由）
        ChannelID string                 `json:"channel_id"` // 目标渠道ID（可选）
        Timestamp time.Time              `json:"timestamp"`  // 时间戳
}

// EventHandler 事件处理函数
type EventHandler func(event Event)

// Subscription 订阅信息
type Subscription struct {
        ID        string        // 订阅ID
        EventType EventType     // 订阅的事件类型
        UserID    string        // 订阅者用户ID
        Handler   EventHandler  // 处理函数
        CreatedAt time.Time     // 创建时间
}

// MessageBus 消息总线
type MessageBus struct {
        subscriptions map[string]*Subscription // 订阅列表
        userChannels  map[string]string        // 用户 -> 渠道映射
        channelSenders map[string]MessageSender // 渠道发送器
        mu            sync.RWMutex
}

// MessageSender 消息发送器接口
// 各渠道实现此接口来发送消息
type MessageSender interface {
        // SendToUser 发送消息给指定用户
        SendToUser(userID string, message string) error
        // GetChannelType 获取渠道类型
        GetChannelType() string
}

// 全局消息总线实例
var globalMessageBus *MessageBus

// initMessageBus 初始化消息总线
func initMessageBus() {
        if globalMessageBus == nil {
                globalMessageBus = NewMessageBus()
        }
}

// NewMessageBus 创建消息总线
func NewMessageBus() *MessageBus {
        return &MessageBus{
                subscriptions:  make(map[string]*Subscription),
                userChannels:   make(map[string]string),
                channelSenders: make(map[string]MessageSender),
        }
}

// ============================================================
// 订阅管理
// ============================================================

// Subscribe 订阅事件
// eventType: 要订阅的事件类型
// userID: 订阅者用户ID
// handler: 事件处理函数
// 返回订阅ID，可用于取消订阅
func (mb *MessageBus) Subscribe(eventType EventType, userID string, handler EventHandler) string {
        mb.mu.Lock()
        defer mb.mu.Unlock()

        subID := generateSubscriptionID(eventType, userID)

        mb.subscriptions[subID] = &Subscription{
                ID:        subID,
                EventType: eventType,
                UserID:    userID,
                Handler:   handler,
                CreatedAt: time.Now(),
        }

        log.Printf("[MessageBus] Subscribed: %s -> %s", eventType, userID)
        return subID
}

// Unsubscribe 取消订阅
func (mb *MessageBus) Unsubscribe(subscriptionID string) {
        mb.mu.Lock()
        defer mb.mu.Unlock()

        if sub, exists := mb.subscriptions[subscriptionID]; exists {
                delete(mb.subscriptions, subscriptionID)
                log.Printf("[MessageBus] Unsubscribed: %s", sub.EventType)
        }
}

// UnsubscribeByUser 取消用户的所有订阅
func (mb *MessageBus) UnsubscribeByUser(userID string) int {
        mb.mu.Lock()
        defer mb.mu.Unlock()

        count := 0
        for id, sub := range mb.subscriptions {
                if sub.UserID == userID {
                        delete(mb.subscriptions, id)
                        count++
                }
        }

        if count > 0 {
                log.Printf("[MessageBus] Unsubscribed %d subscriptions for user: %s", count, userID)
        }
        return count
}

// ============================================================
// 用户路由
// ============================================================

// RegisterUserChannel 注册用户所在的渠道
// 当需要向用户发送消息时，会使用此映射找到对应渠道
func (mb *MessageBus) RegisterUserChannel(userID, channelID string) {
        mb.mu.Lock()
        defer mb.mu.Unlock()

        mb.userChannels[userID] = channelID
        log.Printf("[MessageBus] User %s registered to channel %s", userID, channelID)
}

// UnregisterUserChannel 取消用户渠道注册
func (mb *MessageBus) UnregisterUserChannel(userID string) {
        mb.mu.Lock()
        defer mb.mu.Unlock()

        delete(mb.userChannels, userID)
        log.Printf("[MessageBus] User %s unregistered", userID)
}

// GetUserChannel 获取用户所在的渠道
func (mb *MessageBus) GetUserChannel(userID string) (string, bool) {
        mb.mu.RLock()
        defer mb.mu.RUnlock()

        channelID, exists := mb.userChannels[userID]
        return channelID, exists
}

// ============================================================
// 渠道发送器注册
// ============================================================

// RegisterChannelSender 注册渠道消息发送器
func (mb *MessageBus) RegisterChannelSender(channelID string, sender MessageSender) {
        mb.mu.Lock()
        defer mb.mu.Unlock()

        mb.channelSenders[channelID] = sender
        log.Printf("[MessageBus] Channel sender registered: %s (%s)", channelID, sender.GetChannelType())
}

// UnregisterChannelSender 取消渠道发送器注册
func (mb *MessageBus) UnregisterChannelSender(channelID string) {
        mb.mu.Lock()
        defer mb.mu.Unlock()

        delete(mb.channelSenders, channelID)
        log.Printf("[MessageBus] Channel sender unregistered: %s", channelID)
}

// ============================================================
// 事件发布
// ============================================================

// Publish 发布事件
// 事件会被分发给所有订阅了该事件类型的处理器
func (mb *MessageBus) Publish(event Event) {
        event.Timestamp = time.Now()

        mb.mu.RLock()
        subscriptions := make([]*Subscription, 0)
        for _, sub := range mb.subscriptions {
                if sub.EventType == event.Type || sub.EventType == "*" {
                        subscriptions = append(subscriptions, sub)
                }
        }
        mb.mu.RUnlock()

        // 异步分发事件
        for _, sub := range subscriptions {
                go func(handler EventHandler, e Event) {
                        defer func() {
                                if r := recover(); r != nil {
                                        log.Printf("[MessageBus] Handler panic: %v", r)
                                }
                        }()
                        handler(e)
                }(sub.Handler, event)
        }

        log.Printf("[MessageBus] Event published: %s/%s (subscribers: %d)",
                event.Type, event.Topic, len(subscriptions))
}

// PublishToUser 发布事件给特定用户
func (mb *MessageBus) PublishToUser(event Event, userID string) {
        event.UserID = userID
        event.Timestamp = time.Now()

        mb.mu.RLock()
        defer mb.mu.RUnlock()

        // 找到订阅了该事件类型的用户处理器
        for _, sub := range mb.subscriptions {
                if (sub.EventType == event.Type || sub.EventType == "*") && sub.UserID == userID {
                        go func(handler EventHandler, e Event) {
                                defer func() {
                                        if r := recover(); r != nil {
                                                log.Printf("[MessageBus] Handler panic: %v", r)
                                        }
                                }()
                                handler(e)
                        }(sub.Handler, event)
                }
        }
}

// ============================================================
// 便捷发送方法
// ============================================================

// SendToUser 直接发送消息给用户
// 会自动路由到用户所在的渠道
func (mb *MessageBus) SendToUser(userID string, message string) error {
        mb.mu.RLock()
        defer mb.mu.RUnlock()

        channelID, exists := mb.userChannels[userID]
        if !exists {
                log.Printf("[MessageBus] User %s not found in any channel", userID)
                return nil // 不报错，只是没有注册
        }

        sender, exists := mb.channelSenders[channelID]
        if !exists {
                log.Printf("[MessageBus] No sender for channel %s", channelID)
                return nil
        }

        log.Printf("[MessageBus] Sending to user %s via channel %s", userID, channelID)
        return sender.SendToUser(userID, message)
}

// NotifyHeartbeat 发送心跳通知
func (mb *MessageBus) NotifyHeartbeat(task, result string, isAlert bool) {
        topic := "info"
        if isAlert {
                topic = "alert"
        }

        mb.Publish(Event{
                Type:  EventHeartbeat,
                Topic: topic,
                Payload: map[string]interface{}{
                        "task":    task,
                        "result":  result,
                        "is_alert": isAlert,
                },
        })
}

// NotifySubagent 发送子代理通知
func (mb *MessageBus) NotifySubagent(taskID, status, result string, userID string) {
        mb.Publish(Event{
                Type:    EventSubagent,
                Topic:   status,
                UserID:  userID,
                Payload: map[string]interface{}{
                        "task_id": taskID,
                        "status":  status,
                        "result":  result,
                },
        })
}

// NotifyCron 发送定时任务通知
func (mb *MessageBus) NotifyCron(name, status, output string) {
        mb.Publish(Event{
                Type:  EventCron,
                Topic: status,
                Payload: map[string]interface{}{
                        "name":   name,
                        "status": status,
                        "output": output,
                },
        })
}

// NotifyDelayedTask 发送后台延迟任务唤醒通知
func (mb *MessageBus) NotifyDelayedTask(taskID, command, status, output string, sessionID string) {
        mb.Publish(Event{
                Type:    EventDelayedTask,
                Topic:   status,
                UserID:  sessionID,
                Payload: map[string]interface{}{
                        "task_id":    taskID,
                        "command":    command,
                        "status":     status,
                        "output":     output,
                        "session_id": sessionID,
                },
        })
}

// Broadcast 广播消息给所有订阅者
func (mb *MessageBus) Broadcast(eventType EventType, topic string, payload map[string]interface{}) {
        mb.Publish(Event{
                Type:    eventType,
                Topic:   topic,
                Payload: payload,
        })
}

// ============================================================
// 统计与状态
// ============================================================

// GetStats 获取消息总线统计信息
func (mb *MessageBus) GetStats() map[string]interface{} {
        mb.mu.RLock()
        defer mb.mu.RUnlock()

        // 按类型统计订阅数
        subCountByType := make(map[EventType]int)
        for _, sub := range mb.subscriptions {
                subCountByType[sub.EventType]++
        }

        return map[string]interface{}{
                "subscriptions":     len(mb.subscriptions),
                "by_type":          subCountByType,
                "registered_users": len(mb.userChannels),
                "channel_senders":  len(mb.channelSenders),
        }
}

// ============================================================
// 辅助函数
// ============================================================

func generateSubscriptionID(eventType EventType, userID string) string {
        return string(eventType) + "_" + userID + "_" + time.Now().Format("20060102150405")
}

// ============================================================
// 全局便捷函数
// ============================================================

// GetBus 获取全局消息总线
func GetBus() *MessageBus {
        if globalMessageBus == nil {
                initMessageBus()
        }
        return globalMessageBus
}

// BusSubscribe 全局订阅
func BusSubscribe(eventType EventType, userID string, handler EventHandler) string {
        return GetBus().Subscribe(eventType, userID, handler)
}

// BusPublish 全局发布
func BusPublish(event Event) {
        GetBus().Publish(event)
}

// BusSendToUser 全局发送给用户
func BusSendToUser(userID string, message string) error {
        return GetBus().SendToUser(userID, message)
}

// ============================================================
// BusHeartbeatNotifier - 心跳服务通知器实现
// ============================================================

// BusHeartbeatNotifier 使用消息总线发送心跳通知
type BusHeartbeatNotifier struct{}

// NewBusHeartbeatNotifier 创建基于消息总线的心跳通知器
func NewBusHeartbeatNotifier() *BusHeartbeatNotifier {
        return &BusHeartbeatNotifier{}
}

// Notify 发送心跳通知（实现 HeartbeatNotifier 接口）
func (n *BusHeartbeatNotifier) Notify(task string, result string, shouldAlert bool) error {
        GetBus().NotifyHeartbeat(task, result, shouldAlert)
        return nil
}

// IsAvailable 检查通知器是否可用
func (n *BusHeartbeatNotifier) IsAvailable() bool {
        return globalMessageBus != nil
}
