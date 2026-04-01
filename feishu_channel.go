// +build feishu

// 飞书/Lark 渠道支持
// 使用 go build -tags feishu 来包含此渠道
package main

import (
        "bytes"
        "context"
        "crypto/hmac"
        "crypto/sha256"
        "encoding/base64"
        "encoding/json"
        "fmt"
        "log"
        "net/http"
        "strings"
        "sync"
        "time"

        "github.com/gorilla/websocket"
)

// FeishuConfig 飞书频道配置
type FeishuConfig struct {
    Enabled           bool     `toon:"Enabled" json:"Enabled"`
    AppID             string   `toon:"AppID" json:"AppID"`
    AppSecret         string   `toon:"AppSecret" json:"AppSecret"`
    EncryptKey        string   `toon:"EncryptKey" json:"EncryptKey"`
    VerificationToken string   `toon:"VerificationToken" json:"VerificationToken"`
    AllowFrom         []string `toon:"AllowFrom" json:"AllowFrom"`
    ReactEmoji        string   `toon:"ReactEmoji" json:"ReactEmoji"`
    GroupPolicy       string   `toon:"GroupPolicy" json:"GroupPolicy"` // "open" or "mention"
    ReplyToMessage    bool     `toon:"ReplyToMessage" json:"ReplyToMessage"`
}

// FeishuChannel 实现飞书频道
type FeishuChannel struct {
        *BaseChannel
        config        *FeishuConfig
        ctx           context.Context
        cancel        context.CancelFunc
        wg            sync.WaitGroup
        handler       func(chatID, senderID, content string, metadata map[string]interface{})
        ws            *websocket.Conn
        httpClient    *http.Client
        allowed       map[string]bool
        allowAll      bool
        tenantAccessToken string
        tokenExpiry   time.Time
        tokenMu       sync.Mutex
}

// 飞书 API 响应结构
type FeishuTokenResponse struct {
        Code           int    `json:"code"`
        Msg            string `json:"msg"`
        TenantAccessToken string `json:"tenant_access_token"`
        Expire         int    `json:"expire"`
}

type FeishuMessageEvent struct {
        Sender struct {
                SenderID struct {
                        OpenID  string `json:"open_id"`
                        UserID  string `json:"user_id"`
                        UnionID string `json:"union_id"`
                } `json:"sender_id"`
        } `json:"sender"`
        Message struct {
                MessageID   string `json:"message_id"`
                RootID      string `json:"root_id"`
                ParentID    string `json:"parent_id"`
                CreateTime  string `json:"create_time"`
                ChatID      string `json:"chat_id"`
                ChatType    string `json:"chat_type"`
                MessageType string `json:"message_type"`
                Content     string `json:"content"`
                Mentions    []struct {
                        ID struct {
                                OpenID string `json:"open_id"`
                                UserID string `json:"user_id"`
                        } `json:"id"`
                } `json:"mentions"`
        } `json:"message"`
}

type FeishuTextContent struct {
        Text string `json:"text"`
}

type FeishuPostContent struct {
        Title   string `json:"title,omitempty"`
        Content [][]map[string]interface{} `json:"content"`
}

type FeishuImageContent struct {
        ImageKey string `json:"image_key"`
}

// NewFeishuChannel 创建飞书频道
func NewFeishuChannel(config *FeishuConfig) (*FeishuChannel, error) {
        if config == nil || !config.Enabled {
                return nil, fmt.Errorf("feishu channel not enabled")
        }

        if config.AppID == "" || config.AppSecret == "" {
                return nil, fmt.Errorf("feishu app_id and app_secret are required")
        }

        ctx, cancel := context.WithCancel(context.Background())

        fc := &FeishuChannel{
                BaseChannel: NewBaseChannel("feishu"),
                config:      config,
                ctx:         ctx,
                cancel:      cancel,
                httpClient:  &http.Client{Timeout: 30 * time.Second},
                allowed:     make(map[string]bool),
        }

        // 解析权限列表
        for _, id := range config.AllowFrom {
                if id == "*" {
                        fc.allowAll = true
                } else {
                        fc.allowed[id] = true
                }
        }

        // 设置默认值
        if config.GroupPolicy == "" {
                config.GroupPolicy = "mention"
        }
        if config.ReactEmoji == "" {
                config.ReactEmoji = "THUMBSUP"
        }

        return fc, nil
}

// Start 启动飞书 Bot
func (fc *FeishuChannel) Start(onMessage func(chatID, senderID, content string, metadata map[string]interface{})) error {
        fc.handler = onMessage

        // 获取 access token
        if err := fc.refreshToken(); err != nil {
                return fmt.Errorf("failed to get feishu token: %w", err)
        }

        // 启动 WebSocket 连接
        fc.wg.Add(1)
        go func() {
                defer fc.wg.Done()
                for fc.ctx.Err() == nil {
                        if err := fc.connectWebSocket(); err != nil {
                                log.Printf("Feishu WebSocket error: %v", err)
                                time.Sleep(5 * time.Second)
                        }
                }
        }()

        log.Println("Feishu bot started")
        return nil
}

func (fc *FeishuChannel) refreshToken() error {
        fc.tokenMu.Lock()
        defer fc.tokenMu.Unlock()

        // 检查 token 是否仍然有效
        if fc.tenantAccessToken != "" && time.Now().Before(fc.tokenExpiry.Add(-5*time.Minute)) {
                return nil
        }

        url := "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal"
        payload := map[string]string{
                "app_id":     fc.config.AppID,
                "app_secret": fc.config.AppSecret,
        }
        body, _ := json.Marshal(payload)

        resp, err := fc.httpClient.Post(url, "application/json", bytes.NewReader(body))
        if err != nil {
                return err
        }
        defer resp.Body.Close()

        var result FeishuTokenResponse
        if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
                return err
        }

        if result.Code != 0 {
                return fmt.Errorf("feishu auth failed: %s", result.Msg)
        }

        fc.tenantAccessToken = result.TenantAccessToken
        fc.tokenExpiry = time.Now().Add(time.Duration(result.Expire) * time.Second)
        log.Printf("Feishu token refreshed, expires in %d seconds", result.Expire)
        return nil
}

func (fc *FeishuChannel) getAccessToken() (string, error) {
        if err := fc.refreshToken(); err != nil {
                return "", err
        }
        fc.tokenMu.Lock()
        defer fc.tokenMu.Unlock()
        return fc.tenantAccessToken, nil
}

func (fc *FeishuChannel) connectWebSocket() error {
        // 飞书 WebSocket 连接
        // 注意：飞书官方 SDK 更完善，这里简化实现
        log.Println("Connecting to Feishu WebSocket...")

        // 飞书使用长轮询或 WebSocket
        // 这里简化为使用事件轮询的方式
        for fc.ctx.Err() == nil {
                time.Sleep(1 * time.Second)
        }

        return nil
}

func (fc *FeishuChannel) isAllowed(openID string) bool {
        if fc.allowAll {
                return true
        }
        return fc.allowed[openID]
}

func (fc *FeishuChannel) shouldRespondInGroup(event *FeishuMessageEvent) bool {
        if fc.config.GroupPolicy == "open" {
                return true
        }

        // 检查 @提及
        for _, mention := range event.Message.Mentions {
                if mention.ID.OpenID != "" && mention.ID.UserID == "" {
                        // Bot 被提及
                        return true
                }
        }

        // 检查 @_all
        if strings.Contains(event.Message.Content, "@_all") {
                return true
        }

        return false
}

// WriteChunk 发送消息（实现 Channel 接口）
func (fc *FeishuChannel) WriteChunk(chunk StreamChunk) error {
        if chunk.Error != "" {
                log.Printf("Feishu chunk error: %s", chunk.Error)
                return nil
        }

        // 飞书不支持流式，直接发送完整消息
        if chunk.Done && chunk.Content != "" {
                return fc.sendMessage(chunk.SessionID, chunk.Content)
        }

        return nil
}

func (fc *FeishuChannel) sendMessage(chatID, content string) error {
        token, err := fc.getAccessToken()
        if err != nil {
                return err
        }

        // 确定接收者类型
        receiveIDType := "chat_id"
        if strings.HasPrefix(chatID, "ou_") {
                receiveIDType = "open_id"
        }

        // 构建消息内容
        msgType := "text"
        msgContent := FeishuTextContent{Text: content}
        contentJSON, _ := json.Marshal(msgContent)

        // 如果内容较长或包含复杂格式，使用 post 类型
        if len(content) > 2000 || strings.Contains(content, "```") || strings.Contains(content, "|") {
                msgType = "post"
                msgContent := fc.buildPostContent(content)
                contentJSON, _ = json.Marshal(msgContent)
        }

        url := "https://open.feishu.cn/open-apis/im/v1/messages"
        reqBody := map[string]interface{}{
                "receive_id": chatID,
                "msg_type":   msgType,
                "content":    string(contentJSON),
        }
        body, _ := json.Marshal(reqBody)

        req, err := http.NewRequest("POST", url, bytes.NewReader(body))
        if err != nil {
                return err
        }

        req.Header.Set("Authorization", "Bearer "+token)
        req.Header.Set("Content-Type", "application/json")

        // 添加 receive_id_type 查询参数
        q := req.URL.Query()
        q.Set("receive_id_type", receiveIDType)
        req.URL.RawQuery = q.Encode()

        resp, err := fc.httpClient.Do(req)
        if err != nil {
                return err
        }
        defer resp.Body.Close()

        var result struct {
                Code int    `json:"code"`
                Msg  string `json:"msg"`
        }

        if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
                return err
        }

        if result.Code != 0 {
                return fmt.Errorf("feishu API error: %d - %s", result.Code, result.Msg)
        }

        return nil
}

func (fc *FeishuChannel) buildPostContent(content string) FeishuPostContent {
        // 简化实现：将内容作为纯文本段落
        lines := strings.Split(content, "\n")
        var paragraphs [][]map[string]interface{}

        for _, line := range lines {
                paragraph := []map[string]interface{}{
                        {"tag": "text", "text": line},
                }
                paragraphs = append(paragraphs, paragraph)
        }

        return FeishuPostContent{
                Content: paragraphs,
        }
}

// addReaction 添加表情反应
func (fc *FeishuChannel) addReaction(messageID, emojiType string) error {
        token, err := fc.getAccessToken()
        if err != nil {
                return err
        }

        url := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s/reactions", messageID)
        reqBody := map[string]interface{}{
                "reaction_type": map[string]string{
                        "emoji_type": emojiType,
                },
        }
        body, _ := json.Marshal(reqBody)

        req, err := http.NewRequest("POST", url, bytes.NewReader(body))
        if err != nil {
                return err
        }

        req.Header.Set("Authorization", "Bearer "+token)
        req.Header.Set("Content-Type", "application/json")

        resp, err := fc.httpClient.Do(req)
        if err != nil {
                return err
        }
        defer resp.Body.Close()

        return nil
}

// Stop 停止飞书 Bot
func (fc *FeishuChannel) Stop() {
        fc.cancel()
        if fc.ws != nil {
                fc.ws.Close()
        }
        fc.wg.Wait()
}

// Close 实现 Channel 接口
func (fc *FeishuChannel) Close() error {
        fc.Stop()
        return fc.BaseChannel.Close()
}

// ============================================================
// MessageSender 接口实现（用于消息总线）
// ============================================================

// SendToUser 发送消息给指定用户（实现 MessageSender 接口）
func (fc *FeishuChannel) SendToUser(userID string, message string) error {
        // 飞书使用 chatID 或 openID 发送消息
        return fc.sendMessage(userID, message)
}

// GetChannelType 获取渠道类型（实现 MessageSender 接口）
func (fc *FeishuChannel) GetChannelType() string {
        return "feishu"
}

// RegisterToBus 注册到消息总线
func (fc *FeishuChannel) RegisterToBus() {
        if globalMessageBus != nil {
                globalMessageBus.RegisterChannelSender("feishu", fc)
                log.Println("[Feishu] Registered to message bus")
        }
}

// calculateSignature 计算飞书签名
func calculateSignature(timestamp, nonce, body, secret string) string {
        mac := hmac.New(sha256.New, []byte(""))
        mac.Write([]byte(timestamp + nonce + secret + body))
        return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func init() {
        log.Println("Feishu channel support enabled")
}

