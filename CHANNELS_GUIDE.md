# GhostClaw 多渠道配置指南

本文档详细介绍如何配置 GhostClaw 连接各种聊天平台。GhostClaw 支持 12 种交互渠道，所有渠道共享全局会话（GlobalSession）。

---

## 目录

1. [Telegram 配置](#telegram-配置)
2. [Discord 配置](#discord-配置)
3. [Slack 配置](#slack-配置)
4. [飞书/Lark 配置](#飞书lark-配置)
5. [XMPP 配置](#xmpp-配置)
6. [Matrix 配置](#matrix-配置)
7. [IRC 配置](#irc-配置)
8. [通用配置说明](#通用配置说明)
9. [常见问题](#常见问题)

---

## Telegram 配置

### 第一步：创建 Telegram Bot

1. 在 Telegram 中搜索 **@BotFather**
2. 发送 `/newbot` 命令
3. 按提示输入 Bot 名称（显示名）
4. 按提示输入 Bot 用户名（必须以 `bot` 结尾，如 `MyAssistantBot`）
5. BotFather 会返回 **Bot Token**，格式类似：
   ```
   123456789:ABCdefGHIjklMNOpqrsTUVwxyz
   ```

### 第二步：获取 Chat ID（可选，用于权限控制）

1. 在 Telegram 中搜索 **@userinfobot**
2. 发送任意消息，它会返回你的 Chat ID
3. 群组 Chat ID 需要将 Bot 加入群组后，通过 API 获取

### 第三步：配置 config.toon

在 `config.toon` 文件中添加以下配置：

```toml
telegram_config = {
    enabled = true
    token = "YOUR-BOT-TOKEN-HERE"
    allow_from = ["*"]
    group_policy = "mention"
    streaming = true
    reply_to_message = true
    react_emoji = "👀"
    poll_interval = 0
}
```

### 配置字段说明

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `enabled` | bool | 是 | false | 是否启用 Telegram 渠道 |
| `token` | string | 是 | - | BotFather 返回的 Bot Token |
| `allow_from` | []string | 否 | ["*"] | 允许的用户/群组 ID 列表，`["*"]` 表示允许所有 |
| `group_policy` | string | 否 | "mention" | 群组响应策略：`open`=响应所有消息，`mention`=仅响应@提及 |
| `streaming` | bool | 否 | true | 是否启用流式响应（逐字显示） |
| `reply_to_message` | bool | 否 | true | 是否以回复方式响应消息 |
| `react_emoji` | string | 否 | "👀" | 开始处理时的表情反应 |
| `poll_interval` | int | 否 | 0 | 轮询间隔（秒），0 表示使用 Webhook |
| `proxy` | string | 否 | - | 代理地址，如 `http://127.0.0.1:7890` |

### 权限控制示例

```toml
# 允许所有用户
allow_from = ["*"]

# 仅允许特定用户
allow_from = ["123456789", "987654321"]

# 允许特定用户和群组
allow_from = ["123456789", "-1001234567890"]
```

---

## Discord 配置

### 第一步：创建 Discord 应用

1. 访问 [Discord Developer Portal](https://discord.com/developers/applications)
2. 点击 **New Application**
3. 输入应用名称
4. 在左侧菜单选择 **Bot**
5. 点击 **Add Bot**
6. 点击 **Reset Token** 获取 Bot Token
7. 在 **Privileged Gateway Intents** 部分，启用：
   - Message Content Intent
   - Server Members Intent（可选）

### 第二步：邀请 Bot 到服务器

1. 在 Developer Portal 左侧选择 **OAuth2** → **URL Generator**
2. 在 Scopes 中勾选 `bot`
3. 在 Bot Permissions 中勾选：
   - Send Messages
   - Send Messages in Threads
   - Read Message History
   - Add Reactions
4. 复制生成的 URL，在浏览器中打开并选择服务器授权

### 第三步：配置 config.toon

```toml
discord_config = {
    enabled = true
    token = "YOUR-DISCORD-BOT-TOKEN-HERE"
    allow_from = ["*"]
    group_policy = "mention"
    gateway_url = ""
    intents = 0
}
```

### 配置字段说明

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `enabled` | bool | 是 | false | 是否启用 Discord 渠道 |
| `token` | string | 是 | - | Discord Bot Token |
| `allow_from` | []string | 否 | ["*"] | 允许的用户/服务器 ID 列表 |
| `group_policy` | string | 否 | "mention" | 群组响应策略 |
| `gateway_url` | string | 否 | Discord默认 | Discord Gateway WebSocket 地址 |
| `intents` | int | 否 | 0 | Gateway Intents 位掩码，0 表示使用默认 |

---

## Slack 配置

### 第一步：创建 Slack App

1. 访问 [Slack API: Applications](https://api.slack.com/apps)
2. 点击 **Create New App**
3. 选择 **From scratch**
4. 输入 App 名称并选择工作空间

### 第二步：配置 Socket Mode

1. 在 App 设置页面，选择 **Socket Mode**
2. 启用 Socket Mode
3. 点击 **Generate Token** 生成 **App-Level Token**（`xapp-` 开头）
4. 确保添加 `connections:write` scope

### 第三步：配置 OAuth & Permissions

1. 选择 **OAuth & Permissions**
2. 添加以下 Bot Token Scopes：
   - `chat:write` - 发送消息
   - `channels:history` - 读取频道历史
   - `groups:history` - 读取私有频道历史
   - `im:history` - 读取私信历史
   - `mpim:history` - 读取多人群组历史
   - `reactions:write` - 添加表情反应
   - `reactions:read` - 读取表情反应
3. 点击 **Install to Workspace**
4. 获取 **Bot User OAuth Token**（`xoxb-` 开头）

### 第四步：启用 Events

1. 选择 **Event Subscriptions**
2. 启用 Events
3. Subscribe to bot events：
   - `message.channels`
   - `message.groups`
   - `message.im`
   - `message.mpim`

### 第五步：配置 config.toon

```toml
slack_config = {
    enabled = true
    bot_token = "xoxb-YOUR-BOT-TOKEN-HERE"
    app_token = "xapp-YOUR-APP-TOKEN-HERE"
    allow_from = ["*"]
    group_allow_from = ["*"]
    group_policy = "mention"
    reply_in_thread = true
    react_emoji = "eyes"
    done_emoji = "white_check_mark"
}
```

### 配置字段说明

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `enabled` | bool | 是 | false | 是否启用 Slack 渠道 |
| `bot_token` | string | 是 | - | Bot User OAuth Token（`xoxb-` 开头） |
| `app_token` | string | 是 | - | App-Level Token（`xapp-` 开头） |
| `allow_from` | []string | 否 | ["*"] | 允许的用户 ID 列表 |
| `group_allow_from` | []string | 否 | ["*"] | 允许的频道 ID 列表 |
| `group_policy` | string | 否 | "mention" | 频道响应策略 |
| `reply_in_thread` | bool | 否 | true | 是否在线程中回复 |
| `react_emoji` | string | 否 | "eyes" | 开始处理时的表情 |
| `done_emoji` | string | 否 | "white_check_mark" | 处理完成时的表情 |

---

## 飞书/Lark 配置

### 第一步：创建飞书企业自建应用

1. 访问 [飞书开放平台](https://open.feishu.cn/)
2. 登录并进入开发者后台
3. 点击 **创建企业自建应用**
4. 填写应用名称和描述

### 第二步：配置应用权限

在应用管理页面，选择 **权限管理**，开通以下权限：

**消息相关**：
- `im:message` - 获取与发送消息
- `im:message:send_as_bot` - 以应用身份发消息
- `im:message.group_msg` - 获取群组消息
- `im:message.p2p_msg` - 获取用户私信

**用户相关**：
- `contact:user.base:readonly` - 获取用户基本信息

### 第三步：获取凭证

在 **凭证与基础信息** 页面获取：
- **App ID**（`cli_` 开头）
- **App Secret**

### 第四步：配置事件订阅

1. 选择 **事件订阅**
2. 配置请求网址（需要公网可访问的地址）
3. 添加事件：
   - `im.message.receive_v1` - 接收消息

**注意**：如果使用内网环境，可以使用飞书的 **长连接** 模式，无需配置 Webhook 地址。

### 第五步：发布应用

1. 在 **版本管理与发布** 中创建版本
2. 提交审核
3. 审核通过后发布
4. 在企业后台启用应用

### 第六步：配置 config.toon

```toml
feishu_config = {
    enabled = true
    app_id = "cli_YOUR-APP-ID-HERE"
    app_secret = "YOUR-APP-SECRET-HERE"
    encrypt_key = ""
    verification_token = ""
    allow_from = ["*"]
    group_policy = "mention"
    reply_to_message = true
    react_emoji = "👀"
}
```

### 配置字段说明

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `enabled` | bool | 是 | false | 是否启用飞书渠道 |
| `app_id` | string | 是 | - | 应用 App ID（`cli_` 开头） |
| `app_secret` | string | 是 | - | 应用 App Secret |
| `encrypt_key` | string | 否 | - | 消息加密 Key（如启用加密） |
| `verification_token` | string | 否 | - | 事件订阅验证 Token |
| `allow_from` | []string | 否 | ["*"] | 允许的用户 Open ID 列表 |
| `group_policy` | string | 否 | "mention" | 群组响应策略 |
| `reply_to_message` | bool | 否 | true | 是否回复原消息 |
| `react_emoji` | string | 否 | "👀" | 表情反应（仅私聊生效） |

---

## XMPP 配置

### 第一步：获取 XMPP 账号

1. 注册 XMPP 账号（可使用任意 XMPP 服务器）
   - 公共服务器：[jabber.org](https://jabber.org)、[xmpp.jp](https://xmpp.jp) 等
   - 自建服务器：Openfire、Prosody、ejabberd 等
2. 记录你的 JID（如 `user@example.com`）和密码

### 第二步：配置 config.toon

```toml
xmpp_config = {
    enabled = true
    server = "example.com"
    username = "user@example.com"
    password = "YOUR-PASSWORD-HERE"
    resource = "ghostclaw"
    rooms = ["room@conference.example.com"]
    insecure_tls = false
    group_policy = "mention"
    nick = "GhostClaw"
}
```

### 配置字段说明

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `enabled` | bool | 是 | false | 是否启用 XMPP 渠道 |
| `server` | string | 是 | - | XMPP 服务器地址 |
| `username` | string | 是 | - | XMPP JID（完整格式） |
| `password` | string | 是 | - | XMPP 密码 |
| `resource` | string | 否 | "ghostclaw" | 资源标识 |
| `rooms` | []string | 否 | [] | 自动加入的 MUC 房间列表 |
| `insecure_tls` | bool | 否 | false | 是否跳过 TLS 证书验证 |
| `group_policy` | string | 否 | "mention" | 群组响应策略 |
| `nick` | string | 否 | "GhostClaw" | MUC 房间昵称 |

### 注意事项

- **MUC 房间支持**：当前版本 MUC 功能正在完善中
- **TLS 验证**：如果使用自签名证书，需要设置 `insecure_tls = true`
- **资源标识**：建议保持默认或使用有意义的标识

---

## Matrix 配置

### 第一步：创建 Matrix 账号

1. 注册 Matrix 账号（可使用任意 Homeserver）
   - 公共服务器：[matrix.org](https://matrix.org)
   - 自建服务器：Synapse、Dendrite、Conduit 等
2. 获取 Access Token
   - 登录 Element Web
   - 设置 → 帮助与关于 → 访问令牌

### 第二步：配置 config.toon

```toml
matrix_config = {
    enabled = true
    homeserver_url = "https://matrix.org"
    user_id = "@user:matrix.org"
    access_token = "YOUR-ACCESS-TOKEN-HERE"
    rooms = ["!roomid:matrix.org"]
    group_policy = "mention"
}
```

### 配置字段说明

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `enabled` | bool | 是 | false | 是否启用 Matrix 渠道 |
| `homeserver_url` | string | 是 | - | Matrix Homeserver URL |
| `user_id` | string | 是 | - | Matrix User ID（@user:server 格式） |
| `access_token` | string | 是 | - | Matrix Access Token |
| `rooms` | []string | 否 | [] | 自动加入的房间 ID 列表 |
| `group_policy` | string | 否 | "mention" | 群组响应策略 |

### 注意事项

- **房间 ID 格式**：使用完整的房间 ID，如 `!roomid:matrix.org`
- **Access Token**：请妥善保管，不要泄露
- **加密房间**：当前版本暂不支持端到端加密（E2EE）

---

## IRC 配置

### 第一步：获取 IRC 服务器信息

1. 选择 IRC 服务器
   - 公共网络：Libera.Chat、OFTC、Freenode 等
   - 自建服务器：InspIRCd、UnrealIRCd 等
2. 记录服务器地址、端口和频道信息

### 第二步：配置 config.toon

```toml
irc_config = {
    enabled = true
    server = "irc.libera.chat"
    port = 6697
    nick = "GhostClaw"
    password = ""
    channels = ["#ghostclaw"]
    use_tls = true
    group_policy = "mention"
}
```

### 配置字段说明

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `enabled` | bool | 是 | false | 是否启用 IRC 渠道 |
| `server` | string | 是 | - | IRC 服务器地址 |
| `port` | int | 是 | - | IRC 服务器端口 |
| `nick` | string | 是 | - | IRC 昵称 |
| `password` | string | 否 | - | 服务器密码（如需要） |
| `channels` | []string | 否 | [] | 自动加入的频道列表 |
| `use_tls` | bool | 否 | true | 是否使用 TLS/SSL |
| `group_policy` | string | 否 | "mention" | 频道响应策略 |

### 注意事项

- **TLS 端口**：通常 TLS 端口为 6697，非 TLS 端口为 6667
- **昵称注册**：某些网络需要注册昵称（NickServ）
- **频道格式**：使用 `#` 开头的频道名

---

## 通用配置说明

### 群组策略

| 值 | 说明 |
|----|------|
| `open` | 响应群组/频道中的所有消息 |
| `mention` | 仅响应 @提及 Bot 的消息 |

**推荐**：在群组中使用 `mention` 策略，避免 Bot 响应所有消息造成干扰。

### 权限控制

- `["*"]` - 允许所有用户
- `["123456789"]` - 仅允许特定用户 ID
- 对于群组/频道，通常使用负数 ID 或特殊格式

### 构建选项

```bash
# 默认构建（不包含这些渠道）
./build.sh

# 启用单个渠道
ENABLE_TELEGRAM=1 ./build.sh
ENABLE_DISCORD=1 ./build.sh
ENABLE_SLACK=1 ./build.sh
ENABLE_FEISHU=1 ./build.sh
ENABLE_XMPP=1 ./build.sh
ENABLE_MATRIX=1 ./build.sh
ENABLE_IRC=1 ./build.sh

# 启用多个渠道
ENABLE_TELEGRAM=1 ENABLE_DISCORD=1 ./build.sh

# 启用所有渠道
ENABLE_ALL_CHANNELS=1 ./build.sh
```

### Go 构建标签

如果你直接使用 `go build`，可以使用以下标签：

```bash
# 启用 XMPP 渠道
go build -tags xmpp -o ghostclaw .

# 启用 Matrix 渠道
go build -tags matrix -o ghostclaw .

# 启用 IRC 渠道
go build -tags irc -o ghostclaw .

# 启用多个渠道
go build -tags "xmpp matrix irc" -o ghostclaw .

# 启用所有渠道
go build -tags "telegram discord slack feishu xmpp matrix irc webhook" -o ghostclaw .
```

---

## 常见问题

### Q: Telegram Bot 不响应消息？

1. 检查 Token 是否正确
2. 确认 `enabled = true`
3. 检查 `allow_from` 是否包含你的 Chat ID
4. 如果是群组，确认 Bot 有管理员权限或 `group_policy` 设置正确
5. 检查网络连接，如在国内可能需要配置 `proxy`

### Q: Discord Bot 无法读取消息内容？

1. 在 Developer Portal 确认已启用 **Message Content Intent**
2. 重新邀请 Bot 到服务器
3. 重启 GhostClaw

### Q: Slack Bot 无法连接？

1. 确认 Socket Mode 已启用
2. 检查 `app_token` 是否正确（`xapp-` 开头）
3. 检查 `bot_token` 是否正确（`xoxb-` 开头）
4. 确认已安装应用到工作空间

### Q: 飞书 Bot 收不到消息？

1. 确认应用已发布并启用
2. 检查权限配置是否完整
3. 如果使用事件订阅，确认 Webhook 地址可访问
4. 检查事件订阅是否添加了 `im.message.receive_v1`

### Q: 流式响应不工作？

- **Telegram**：确保 `streaming = true`
- **Discord/Slack**：流式响应通过消息编辑实现，确保 Bot 有编辑消息权限
- **飞书**：流式响应通过消息更新实现

### Q: 如何同时启用多个渠道？

```bash
ENABLE_ALL_CHANNELS=1 ./build.sh
```

然后在 `config.toon` 中配置所有需要的渠道。

### Q: XMPP 连接失败？

1. 检查 JID 格式是否正确（应为 `user@server.com`）
2. 确认服务器地址和端口正确
3. 如果使用自签名证书，设置 `insecure_tls = true`
4. 检查防火墙是否允许 XMPP 端口（通常为 5222）

### Q: Matrix 无法同步消息？

1. 确认 Access Token 有效（可在 Element 中重新获取）
2. 检查 Homeserver URL 是否正确
3. 确认 User ID 格式正确（`@user:server.com`）
4. 如果使用自建服务器，确保 Synapse 配置正确

### Q: IRC 连接被拒绝？

1. 确认服务器地址和端口正确
2. 检查是否需要 TLS（通常 6697 为 TLS 端口，6667 为非 TLS）
3. 某些网络需要注册昵称，使用 NickServ 注册
4. 检查是否被服务器封禁（尝试更换昵称）

### Q: 如何查看调试日志？

设置环境变量启用调试：

```bash
DEBUG=1 ./ghostclaw
```

---

## 安全建议

1. **保护凭证**：不要将 `config.toon` 提交到版本控制
2. **限制权限**：使用 `allow_from` 限制可访问的用户
3. **最小权限原则**：只申请必要的 Bot 权限
4. **定期轮换**：定期重新生成 Token 和 Secret
5. **监控日志**：关注异常访问和错误日志

---

*GhostClaw 多渠道配置指南 - 让 AI 助手无处不在*
