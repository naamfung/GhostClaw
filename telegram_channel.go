// +build telegram

// Telegram 渠道支持
// 使用 go build -tags telegram 来包含此渠道
package main

import (
        "context"
        "fmt"
        "log"
        "net/http"
        "net/url"
        "strconv"
        "strings"
        "sync"
        "time"

        tele "gopkg.in/telebot.v3"
)

// TelegramChannel 实现 Telegram 频道
type TelegramChannel struct {
        *BaseChannel
        config      *TelegramConfig
        bot         *tele.Bot
        ctx         context.Context
        cancel      context.CancelFunc
        wg          sync.WaitGroup
        handler     func(chatID, senderID, content string, metadata map[string]interface{})
        streamBufs  map[int64]*telegramStreamBuf
        streamMu    sync.Mutex
        allowed     map[string]bool
        allowAll    bool
        botUsername string
        botID       int64
}

type telegramStreamBuf struct {
        text      strings.Builder
        messageID int
        lastEdit  time.Time
        streamID  string
}

// NewTelegramChannel 创建 Telegram 频道
func NewTelegramChannel(config *TelegramConfig) (*TelegramChannel, error) {
        if config == nil || !config.Enabled {
                return nil, fmt.Errorf("telegram channel not enabled")
        }

        if config.Token == "" {
                return nil, fmt.Errorf("telegram token is required")
        }

        ctx, cancel := context.WithCancel(context.Background())

        tc := &TelegramChannel{
                BaseChannel: NewBaseChannel("telegram"),
                config:      config,
                ctx:         ctx,
                cancel:      cancel,
                streamBufs:  make(map[int64]*telegramStreamBuf),
                allowed:     make(map[string]bool),
        }

        // 解析权限列表
        for _, id := range config.AllowFrom {
                if id == "*" {
                        tc.allowAll = true
                } else {
                        tc.allowed[id] = true
                }
        }

        // 设置默认值
        if config.GroupPolicy == "" {
                config.GroupPolicy = "mention"
        }
        if config.ReactEmoji == "" {
                config.ReactEmoji = "👀"
        }
        if config.PollInterval == 0 {
                config.PollInterval = 1
        }

        return tc, nil
}

// Start 启动 Telegram Bot
func (tc *TelegramChannel) Start(onMessage func(chatID, senderID, content string, metadata map[string]interface{})) error {
        tc.handler = onMessage

        settings := tele.Settings{
                Token:  tc.config.Token,
                Poller: &tele.LongPoller{Timeout: time.Duration(tc.config.PollInterval) * time.Second},
        }

        // 设置代理
        if tc.config.Proxy != "" {
                proxyURL, err := url.Parse(tc.config.Proxy)
                if err != nil {
                        log.Printf("Telegram proxy URL parse error: %v", err)
                } else {
                        settings.Client = &http.Client{
                                Transport: &http.Transport{
                                        Proxy: http.ProxyURL(proxyURL),
                                },
                        }
                }
        }

        bot, err := tele.NewBot(settings)
        if err != nil {
                return fmt.Errorf("failed to create telegram bot: %w", err)
        }
        tc.bot = bot

        // 获取 Bot 身份
        me := bot.Me
        tc.botUsername = me.Username
        tc.botID = me.ID

        log.Printf("Telegram bot @%s connected", tc.botUsername)

        // 注册命令
        tc.registerCommands()

        // 注册消息处理器
        bot.Handle(tele.OnText, tc.handleTextMessage)
        bot.Handle(tele.OnPhoto, tc.handlePhotoMessage)
        bot.Handle(tele.OnDocument, tc.handleDocumentMessage)
        bot.Handle(tele.OnVoice, tc.handleVoiceMessage)
        bot.Handle(tele.OnAudio, tc.handleAudioMessage)

        // 启动 Bot
        tc.wg.Add(1)
        go func() {
                defer tc.wg.Done()
                log.Println("Telegram bot started (polling mode)")
                bot.Start()
        }()

        return nil
}

func (tc *TelegramChannel) registerCommands() {
        tc.bot.Handle("/start", func(c tele.Context) error {
                if !tc.isAllowed(c.Sender()) {
                        return c.Send("Sorry, you are not authorized to use this bot.")
                }
                return c.Send(fmt.Sprintf("👋 Hi %s! I'm GhostClaw AI Agent.\n\nSend me a message and I'll respond!\nType /help to see available commands.", c.Sender().FirstName))
        })

        tc.bot.Handle("/help", func(c tele.Context) error {
                return c.Send("🤖 GhostClaw AI Agent commands:\n/new — Start a new conversation\n/stop — Stop the current task\n/help — Show available commands")
        })

        tc.bot.Handle("/new", func(c tele.Context) error {
                if !tc.isAllowed(c.Sender()) {
                        return nil
                }
                metadata := tc.buildMetadata(c)
                if tc.handler != nil {
                        tc.handler(
                                strconv.FormatInt(c.Chat().ID, 10),
                                tc.senderID(c.Sender()),
                                "/new",
                                metadata,
                        )
                }
                return c.Send("Started a new conversation.")
        })

        tc.bot.Handle("/stop", func(c tele.Context) error {
                if !tc.isAllowed(c.Sender()) {
                        return nil
                }
                metadata := tc.buildMetadata(c)
                if tc.handler != nil {
                        tc.handler(
                                strconv.FormatInt(c.Chat().ID, 10),
                                tc.senderID(c.Sender()),
                                "/stop",
                                metadata,
                        )
                }
                return c.Send("Task stopped.")
        })
}

func (tc *TelegramChannel) handleTextMessage(c tele.Context) error {
        if !tc.isAllowed(c.Sender()) {
                return nil
        }

        if !tc.shouldProcessGroupMessage(c) {
                return nil
        }

        // 发送 typing 动作
        c.Notify(tele.Typing)

        metadata := tc.buildMetadata(c)
        content := c.Text()

        if replyTo := c.Message().ReplyTo; replyTo != nil {
                replyText := replyTo.Text
                if replyText == "" {
                        replyText = replyTo.Caption
                }
                if len(replyText) > 500 {
                        replyText = replyText[:500] + "..."
                }
                if replyText != "" {
                        content = fmt.Sprintf("[Reply to: %s]\n%s", replyText, content)
                }
                metadata["reply_to_message_id"] = replyTo.ID
        }

        // 统一处理斜杠命令
        session := GetGlobalSession()
        if HandleSlashCommandWithDefaults(content,
                func(resp string) {
                        c.Send(resp)
                },
                func() {
                        session.CancelTask()
                },
                func() {
                        // 退出程序（单用户模式）
                        log.Println("[Telegram] Received /exit, exiting program...")
                        session.autoSaveHistory()
                        // 注意：这里不能直接 os.Exit，因为需要关闭连接，但程序退出是用户意愿
                        // 实际应在 main 中处理，这里仅记录并让主循环退出
                        // 为简化，直接退出
                        // os.Exit(0)
                        // 但 Telegram 渠道通常不退出程序，改为记录并返回
                        log.Println("[Telegram] /exit ignored in bot mode")
                }) {
                return nil
        }

        // 普通消息，加入历史并启动任务
        session.AddToHistory("user", content)
        go tc.processUserInput(session, c.Chat().ID, strconv.FormatInt(c.Sender().ID, 10), content, metadata)

        return nil
}

// processUserInput 处理用户输入（后台任务）
func (tc *TelegramChannel) processUserInput(session *GlobalSession, chatID int64, senderID, input string, metadata map[string]interface{}) {
        ok, taskID := session.TryStartTask()
        if !ok {
                // 已有任务运行，发送提示
                tc.bot.Send(&tele.Chat{ID: chatID}, "已有任务在执行中，请使用 /stop 取消后再试")
                return
        }
        taskCtx := session.GetTaskCtx()
        defer session.SetTaskRunning(false, taskID)

        // 创建会话输出通道（直接使用 TelegramChannel 自身）
        ch := tc

        // 获取当前历史（从全局会话中获取）
        history := session.GetHistory()

        // 执行 AgentLoop
        newHistory, err := AgentLoop(taskCtx, ch, history, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking)
        if err != nil {
                if err != context.Canceled {
                        tc.bot.Send(&tele.Chat{ID: chatID}, fmt.Sprintf("❌ 错误: %v", err))
                }
                if len(newHistory) > len(history) {
                        session.SetHistory(newHistory)
                }
                return
        }

        // 更新历史
        session.SetHistory(newHistory)
}

func (tc *TelegramChannel) handlePhotoMessage(c tele.Context) error {
        if !tc.isAllowed(c.Sender()) || !tc.shouldProcessGroupMessage(c) {
                return nil
        }

        c.Notify(tele.Typing)

        metadata := tc.buildMetadata(c)
        content := "[Photo]"
        if c.Message().Caption != "" {
                content = c.Message().Caption + "\n[Photo]"
        }

        if tc.handler != nil {
                tc.handler(
                        strconv.FormatInt(c.Chat().ID, 10),
                        tc.senderID(c.Sender()),
                        content,
                        metadata,
                )
        }

        return nil
}

func (tc *TelegramChannel) handleDocumentMessage(c tele.Context) error {
        if !tc.isAllowed(c.Sender()) || !tc.shouldProcessGroupMessage(c) {
                return nil
        }

        c.Notify(tele.Typing)

        metadata := tc.buildMetadata(c)
        doc := c.Message().Document
        content := fmt.Sprintf("[Document: %s]", doc.FileName)
        if c.Message().Caption != "" {
                content = c.Message().Caption + "\n" + content
        }

        if tc.handler != nil {
                tc.handler(
                        strconv.FormatInt(c.Chat().ID, 10),
                        tc.senderID(c.Sender()),
                        content,
                        metadata,
                )
        }

        return nil
}

func (tc *TelegramChannel) handleVoiceMessage(c tele.Context) error {
        if !tc.isAllowed(c.Sender()) || !tc.shouldProcessGroupMessage(c) {
                return nil
        }

        c.Notify(tele.Typing)

        metadata := tc.buildMetadata(c)
        content := "[Voice message]"

        if tc.handler != nil {
                tc.handler(
                        strconv.FormatInt(c.Chat().ID, 10),
                        tc.senderID(c.Sender()),
                        content,
                        metadata,
                )
        }

        return nil
}

func (tc *TelegramChannel) handleAudioMessage(c tele.Context) error {
        if !tc.isAllowed(c.Sender()) || !tc.shouldProcessGroupMessage(c) {
                return nil
        }

        c.Notify(tele.Typing)

        metadata := tc.buildMetadata(c)
        audio := c.Message().Audio
        content := fmt.Sprintf("[Audio: %s]", audio.Title)

        if tc.handler != nil {
                tc.handler(
                        strconv.FormatInt(c.Chat().ID, 10),
                        tc.senderID(c.Sender()),
                        content,
                        metadata,
                )
        }

        return nil
}

// WriteChunk 发送消息（实现 Channel 接口）
func (tc *TelegramChannel) WriteChunk(chunk StreamChunk) error {
        tc.mu.Lock()
        defer tc.mu.Unlock()

        processed := tc.ProcessChunkWithReplacement(chunk)

        if processed.Error != "" {
                log.Printf("Telegram chunk error: %s", processed.Error)
                return nil
        }

        chatID, err := strconv.ParseInt(chunk.SessionID, 10, 64)
        if err != nil {
                log.Printf("Invalid chat ID: %s", chunk.SessionID)
                return nil
        }

        chat := &tele.Chat{ID: chatID}

        // 流式模式
        if tc.config.Streaming && processed.Content != "" && !processed.Done {
                return tc.handleStreamDelta(chatID, processed.Content, processed.SessionID)
        }

        // 流式结束
        if tc.config.Streaming && processed.Done {
                return tc.handleStreamEnd(chatID, processed.SessionID)
        }

        // 非流式模式或最终消息
        if processed.Done && processed.Content != "" {
                messages := tc.splitMessage(processed.Content, 4000)
                for _, msg := range messages {
                        _, err := tc.bot.Send(chat, msg, tele.ModeHTML)
                        if err != nil {
                                log.Printf("Failed to send Telegram message: %v", err)
                                return err
                        }
                }
        }

        return nil
}

func (tc *TelegramChannel) handleStreamDelta(chatID int64, delta string, sessionID string) error {
        tc.streamMu.Lock()
        defer tc.streamMu.Unlock()

        buf, exists := tc.streamBufs[chatID]
        if !exists {
                buf = &telegramStreamBuf{streamID: sessionID}
                tc.streamBufs[chatID] = buf
        }

        buf.text.WriteString(delta)

        if strings.TrimSpace(buf.text.String()) == "" {
                return nil
        }

        chat := &tele.Chat{ID: chatID}

        if buf.messageID == 0 {
                msg, err := tc.bot.Send(chat, buf.text.String())
                if err != nil {
                        return err
                }
                buf.messageID = msg.ID
                buf.lastEdit = time.Now()
                return nil
        }

        if time.Since(buf.lastEdit) >= 600*time.Millisecond {
                _, err := tc.bot.Edit(&tele.Message{ID: buf.messageID, Chat: chat}, buf.text.String())
                if err != nil && !strings.Contains(err.Error(), "message is not modified") {
                        return err
                }
                buf.lastEdit = time.Now()
        }

        return nil
}

func (tc *TelegramChannel) handleStreamEnd(chatID int64, sessionID string) error {
        tc.streamMu.Lock()
        defer tc.streamMu.Unlock()

        buf, exists := tc.streamBufs[chatID]
        if !exists {
                return nil
        }

        defer delete(tc.streamBufs, chatID)

        if buf.messageID == 0 || buf.text.Len() == 0 {
                return nil
        }

        chat := &tele.Chat{ID: chatID}
        htmlText := markdownToTelegramHTML(buf.text.String())

        _, err := tc.bot.Edit(&tele.Message{ID: buf.messageID, Chat: chat}, htmlText, tele.ModeHTML)
        if err != nil && !strings.Contains(err.Error(), "message is not modified") {
                return err
        }

        return nil
}

// Stop 停止 Bot
func (tc *TelegramChannel) Stop() {
        if tc.bot != nil {
                tc.bot.Stop()
        }
        tc.cancel()
        tc.wg.Wait()
}

// Close 实现 Channel 接口
func (tc *TelegramChannel) Close() error {
        tc.Stop()
        return tc.BaseChannel.Close()
}

func (tc *TelegramChannel) isAllowed(user *tele.User) bool {
        if tc.allowAll {
                return true
        }
        if user == nil {
                return false
        }

        userID := strconv.FormatInt(user.ID, 10)
        if tc.allowed[userID] {
                return true
        }
        if user.Username != "" && tc.allowed[user.Username] {
                return true
        }
        if user.Username != "" {
                combined := fmt.Sprintf("%d|%s", user.ID, user.Username)
                if tc.allowed[combined] {
                        return true
                }
        }

        log.Printf("Access denied for user %d (@%s)", user.ID, user.Username)
        return false
}

func (tc *TelegramChannel) shouldProcessGroupMessage(c tele.Context) bool {
        // 私聊总是响应
        if c.Chat().Type == tele.ChatPrivate {
                return true
        }

        // 使用全局群聊策略（如果配置了）
        if globalGroupChatConfig != nil {
                return ShouldRespondInGroup(globalGroupChatConfig,
                        strconv.FormatInt(c.Chat().ID, 10),
                        c.Text(),
                        tc.botUsername)
        }

        // 回退到原有逻辑（保持兼容）
        if tc.config.GroupPolicy == "open" {
                return true
        }

        text := c.Text()
        if text != "" {
                mention := fmt.Sprintf("@%s", tc.botUsername)
                if strings.Contains(text, mention) {
                        return true
                }
        }

        if replyTo := c.Message().ReplyTo; replyTo != nil {
                if replyTo.Sender != nil && replyTo.Sender.ID == tc.botID {
                        return true
                }
        }

        return false
}

func (tc *TelegramChannel) senderID(user *tele.User) string {
        if user.Username != "" {
                return fmt.Sprintf("%d|%s", user.ID, user.Username)
        }
        return strconv.FormatInt(user.ID, 10)
}

func (tc *TelegramChannel) buildMetadata(c tele.Context) map[string]interface{} {
        metadata := map[string]interface{}{
                "message_id": c.Message().ID,
                "user_id":    c.Sender().ID,
                "username":   c.Sender().Username,
                "first_name": c.Sender().FirstName,
                "chat_type":  string(c.Chat().Type),
                "is_group":   c.Chat().Type != tele.ChatPrivate,
        }

        if threadID := c.Message().ThreadID; threadID != 0 {
                metadata["message_thread_id"] = threadID
        }

        return metadata
}

func (tc *TelegramChannel) splitMessage(text string, maxLen int) []string {
        if len(text) <= maxLen {
                return []string{text}
        }

        var messages []string
        var current strings.Builder

        lines := strings.Split(text, "\n")
        for _, line := range lines {
                if current.Len()+len(line)+1 > maxLen {
                        if current.Len() > 0 {
                                messages = append(messages, current.String())
                                current.Reset()
                        }
                }
                current.WriteString(line)
                current.WriteString("\n")
        }

        if current.Len() > 0 {
                messages = append(messages, current.String())
        }

        return messages
}

// markdownToTelegramHTML 将 Markdown 转换为 Telegram HTML
func markdownToTelegramHTML(text string) string {
        // 处理代码块
        text = strings.ReplaceAll(text, "```", "")
        // 处理行内代码
        text = strings.ReplaceAll(text, "`", "")
        // 处理粗体
        text = strings.ReplaceAll(text, "**", "<b>")
        text = strings.ReplaceAll(text, "__", "<b>")
        // 处理斜体
        text = strings.ReplaceAll(text, "*", "<i>")
        text = strings.ReplaceAll(text, "_", "<i>")
        return text
}

// ============================================================
// MessageSender 接口实现（用于消息总线）
// ============================================================

// SendToUser 发送消息给指定用户（实现 MessageSender 接口）
// userID 格式: "chatID" 或 "chatID|username"
func (tc *TelegramChannel) SendToUser(userID string, message string) error {
        if tc.bot == nil {
                return fmt.Errorf("telegram bot not initialized")
        }

        // 解析 userID，格式可能是 "chatID" 或 "chatID|username"
        chatIDStr := userID
        if idx := strings.Index(userID, "|"); idx > 0 {
                chatIDStr = userID[:idx]
        }

        chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
        if err != nil {
                return fmt.Errorf("invalid user ID: %s", userID)
        }

        chat := &tele.Chat{ID: chatID}

        // 分割长消息
        messages := tc.splitMessage(message, 4000)
        for _, msg := range messages {
                _, err := tc.bot.Send(chat, msg, tele.ModeHTML)
                if err != nil {
                        return fmt.Errorf("failed to send message: %w", err)
                }
        }

        return nil
}

// GetChannelType 获取渠道类型（实现 MessageSender 接口）
func (tc *TelegramChannel) GetChannelType() string {
        return "telegram"
}

// RegisterToBus 注册到消息总线
func (tc *TelegramChannel) RegisterToBus() {
        if globalMessageBus != nil {
                globalMessageBus.RegisterChannelSender("telegram", tc)
                log.Println("[Telegram] Registered to message bus")
        }
}

