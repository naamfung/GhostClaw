# 版本历史

## v0.0.1 (2026-03-30)

### 架構重置 版本重置


## v2.8.2 (2026-03-28)

### 重要修复：延迟任务唤醒机制

**问题修复**：修复延迟任务（shell_delayed）无法正常唤醒的问题，避免模型疯狂轮询消耗大量词元。

**核心修复**：

1. **唤醒处理器实现** (`main.go`)
   - 添加 `SetWakeHandler` 设置唤醒处理函数
   - 唤醒时自动触发新的模型调用
   - 支持会话连接状态检测

2. **消息总线通知** (`messagebus.go`)
   - 新增 `EventDelayedTask` 事件类型
   - 新增 `NotifyDelayedTask` 函数

3. **SessionID 自动传递** (`channel.go`, `web_session_channel.go`)
   - 扩展 `Channel` 接口添加 `GetSessionID()` 方法
   - 自动从 Channel 获取会话ID，无需模型传入

4. **任务完成/失败立即唤醒** (`task_manager.go`)
   - 任务快速失败时立即触发唤醒通知
   - 不再等待预设的唤醒时间

**工具描述更新 - 明确禁止轮询**：

| 工具 | 更新内容 |
|------|----------|
| `shell_delayed` | `🚫 DO NOT POLL: 任务启动后不要轮询！系统会自动通知你结果。` |
| `shell_delayed_check` | `🚫 DO NOT POLL: 不要轮询！不要频繁调用此工具检查任务状态。` |
| `shell_delayed_wait` | `🚫 DO NOT POLL: 调用此工具后，不需要轮询！请继续其他工作或停止。` |

**返回消息优化**：

- 明确告知模型"不需要轮询"
- 明确告知"系统会主动通知"
- 明确告知"可以继续其他工作或停止"

**新增函数**：

- `TriggerDelayedTaskWake()` - 触发延迟任务唤醒后的模型调用

## v2.7.16 (2026-03-28)

### 重要更新：浏览器工具增强 (v2)

**新增 5 个浏览器工具**，总工具数达到 **32 个**：

| 新增工具 | 功能 |
|----------|------|
| `browser_pdf` | 将网页导出为 PDF（返回 base64） |
| `browser_pdf_from_file` | 将本地 HTML 文件导出为 PDF |
| `browser_set_headers` | 设置自定义 HTTP 请求头 |
| `browser_set_user_agent` | 设置自定义 User-Agent |
| `browser_emulate_device` | 模拟移动设备访问（iPhone/iPad/Android 等） |

**设备模拟预设**：

| 预设 | 视口 | User-Agent |
|------|------|------------|
| `iphone` | 375×812 | iPhone 17 |
| `iphone_landscape` | 812×375 | iPhone 17 |
| `ipad` | 768×1024 | iPad 17 |
| `android_phone` | 360×800 | Pixel 8 |
| `android_tablet` | 1024×768 | Pixel Tablet |
| `desktop` | 1920×1080 | Windows Chrome |
| `desktop_mac` | 1920×1080 | macOS Chrome |

**使用示例**：

```json
// 模拟 iPhone 访问
browser_emulate_device(url="https://example.com", device="iphone")

// 将本地 HTML 转 PDF（模型先生成 HTML，再转 PDF）
browser_pdf_from_file(file_path="/tmp/report.html")

// 设置自定义请求头
browser_set_headers(url="https://api.example.com", headers=["Authorization: Bearer token"])
```

**问题解决率提升**：88.9% (8/9)，新增设备模拟功能。

## v2.7.15 (2026-03-28)

### 重要更新：浏览器工具增强

**新增 27 个浏览器自动化工具**，基于 Rod 库实现完整的浏览器自动化能力：

**新增文件：**
- `browser_session.go` - 浏览器会话管理器，支持多标签页和会话持久化
- `browser_tools.go` - 基础浏览器工具（点击、输入、滚动、等待、内容提取等）
- `browser_tools_advanced.go` - 高级浏览器工具（Cookie持久化、对话框处理、文件上传、智能等待等）

**新增工具列表：**

| 类别 | 工具 | 功能 |
|------|------|------|
| 基础 | `browser_search` | 百度搜索 |
| 基础 | `browser_visit` | 访问页面提取文本 |
| 基础 | `browser_download` | 下载网页HTML |
| 交互 | `browser_click` | 点击元素 |
| 交互 | `browser_double_click` | 双击元素 |
| 交互 | `browser_right_click` | 右键点击 |
| 交互 | `browser_hover` | 鼠标悬停 |
| 交互 | `browser_drag` | 拖拽元素 |
| 交互 | `browser_type` | 输入文本 |
| 交互 | `browser_key_press` | 模拟按键 |
| 交互 | `browser_fill_form` | 填写表单 |
| 交互 | `browser_select_option` | 选择下拉选项 |
| 交互 | `browser_scroll` | 滚动页面 |
| 交互 | `browser_upload_file` | 上传文件 |
| 等待 | `browser_wait_element` | 等待元素出现 |
| 等待 | `browser_wait_smart` | 智能等待（多条件组合） |
| 导航 | `browser_navigate` | 前进/后退/刷新 |
| 提取 | `browser_extract_links` | 提取链接 |
| 提取 | `browser_extract_images` | 提取图片 |
| 提取 | `browser_extract_elements` | 提取元素内容 |
| 提取 | `browser_snapshot` | DOM快照 |
| 截图 | `browser_screenshot` | 页面截图 |
| 截图 | `browser_element_screenshot` | 元素截图 |
| 高级 | `browser_execute_js` | 执行JavaScript |
| 高级 | `browser_get_cookies` | 获取Cookies |
| 高级 | `browser_cookie_save` | 保存Cookies到TOON文件 |
| 高级 | `browser_cookie_load` | 从TOON文件加载Cookies |

**核心特性：**

1. **Cookie TOON 持久化**：
   - 使用 TOON 格式存储 Cookie，比 JSON 节省约 40% Token
   - 支持保存和恢复登录态
   - 自动生成文件名 `cookies_{domain}.toon`

2. **智能等待策略**：
   - 等待可见 (`visible`)
   - 等待可交互 (`interactable`)
   - 等待稳定 (`stable`)
   - 多条件组合，提高操作可靠性

3. **高级交互操作**：
   - 悬停触发下拉菜单
   - 双击选择文本
   - 右键打开上下文菜单
   - 拖拽排序/移动元素

4. **统一错误处理**：
   - 所有浏览器操作使用 `BrowserError` 结构
   - 捕获 panic 并转换为友好错误信息
   - 包含操作名称和超时时间

**文档更新：**
- `BROWSER_TOOLS_GUIDE.md` - 浏览器工具完整使用指南
- `BROWSER_ENHANCEMENT_ANALYSIS.md` - Rod 源码分析与实现方案

## v2.7.15 (2026-03-27)

### 重要修复

**全面统一 API 命名格式为 PascalCase**：
- v2.7.14 只修复了部分 API，v2.7.15 完成全部统一
- 所有 API 接口返回字段统一使用 PascalCase 格式

**修改的文件**：

后端：
- `api_handlers.go` - 修复 `listRoles`、`listSkills`、`listActors` 返回字段名
- `actor.go` - 更新 `Actor` 结构体 JSON 标签为 PascalCase
- `role.go` - 更新 `Role` 结构体 JSON 标签为 PascalCase
- `skill.go` - 更新 `Skill` 结构体 JSON 标签为 PascalCase

前端：
- `webui/src/lib/components/app/chat/ChatSettings/ChatSettingsActorsTab.svelte` - 更新接口定义和字段访问
- `webui/src/lib/components/app/chat/ChatSettings/ChatSettingsRolesTab.svelte` - 更新接口定义和字段访问
- `webui/src/lib/components/app/chat/ChatSettings/ChatSettingsSkillsTab.svelte` - 更新接口定义和字段访问
- `webui/src/lib/components/app/chat/ChatSettings/ChatSettingsModelTab.svelte` - 修复 `data.Models` 字段

**API 字段名变更汇总**：

| API | 旧字段名 | 新字段名 |
|-----|---------|---------|
| `/api/config` | `api_config`, `default_role`, `needs_setup` | `APIConfig`, `DefaultRole`, `NeedsSetup` |
| `/api/models` | `models`, `api_type`, `base_url`, `api_key` | `Models`, `APIType`, `BaseURL`, `APIKey` |
| `/api/actors` | `actors`, `character_name`, `is_default` | `Actors`, `CharacterName`, `IsDefault` |
| `/api/roles` | `roles`, `display_name`, `is_preset` | `Roles`, `DisplayName`, `IsPreset` |
| `/api/skills` | `skills`, `display_name`, `trigger_words` | `Skills`, `DisplayName`, `TriggerWords` |

## v2.7.14 (2026-03-27)

### 重要修复

**前端适配 PascalCase 命名格式**：
- 后端 v2.7.10 已将配置格式标准化为 PascalCase，但前端未同步更新
- 修复前端的 API 接口类型定义，统一使用 PascalCase 格式
- 修复配置 API 请求/响应的字段名映射

**修改的文件**：

后端：
- `api_handlers.go` - 修复 `getConfig`、`updateConfig`、`listModelsAPI`、`getModelAPI` 的返回字段名
- `actor.go` - 更新 `ModelConfig` 的 JSON 标签为 PascalCase

前端：
- `webui/src/lib/services/config.service.ts` - 更新接口定义
- `webui/src/lib/components/app/chat/ChatSettings/ChatSettingsModelTab.svelte` - 更新字段名
- `webui/src/lib/components/app/chat/ChatSettings/ChatSettingsRolesTab.svelte` - 更新字段名

**API 字段名变更**：

| 旧字段名 (snake_case) | 新字段名 (PascalCase) |
|----------------------|----------------------|
| `api_config` | `APIConfig` |
| `api_type` | `APIType` |
| `base_url` | `BaseURL` |
| `api_key` | `APIKey` |
| `max_tokens` | `MaxTokens` |
| `default_role` | `DefaultRole` |
| `needs_setup` | `NeedsSetup` |
| `block_dangerous_commands` | `BlockDangerousCommands` |

**注意**：发送给模型 API 的参数（如 `max_tokens`、`temperature`）仍保持 snake_case，因为这是 OpenAI API 的标准格式。

## v2.7.13 (2026-03-27)

### 重要修复

**思考模式（Thinking Mode）正确实现**：
- 修复 `thinking` 参数位置错误：应放在请求体顶层，而非 `extra_body` 中
- 参考 DeepSeek 官方文档：https://api-docs.deepseek.com/zh-cn/guides/thinking_mode
- 正确格式：
  ```json
  {
    "model": "deepseek-chat",
    "messages": [...],
    "thinking": {"type": "enabled"}
  }
  ```
- 注意：`extra_body` 是 OpenAI SDK 的特性，直接 HTTP 请求不需要这个包装

**Anthropic 流式思考内容解析**：
- 修复 `parseSSEChunk` 函数未正确解析 Anthropic 流式响应中的 `thinking` 类型内容
- 新增对 `delta.thinking` 字段的解析，将思考内容输出到 `reasoning_content`
- 新增对 `content_block_start` 事件类型的处理

**配置更新采用差异合并策略**：
- 重构 `updateConfig` 函数：先读取现有配置文件 → 解析请求中存在的字段 → 差异合并 → 保存完整配置
- 解决前端部分更新时（如只发送 timeout 或 default_role）导致其他字段被覆盖为默认值的问题
- 布尔值字段（stream、thinking、block_dangerous_commands）现在只在请求中明确包含时才更新

**默认配置修正**：
- `Stream` 默认值保持 `true`（流式输出）
- `Thinking` 默认值改为 `true`（启用思考模式）
- 配置向导生成的配置现在正确设置 `Stream = true` 和 `Thinking = true`

### 修改的文件

- `CallModel.go` - 修复 thinking 参数位置，移至请求体顶层
- `StreamChunk.go` - 新增 Anthropic 流式思考内容解析
- `api_handlers.go` - 重构配置更新逻辑，采用差异合并策略
- `config.go` - 修改 Thinking 默认值为 true
- `config_wizard.go` - 添加 Thinking 默认值设置

### 技术细节

**DeepSeek 思考模式 API 调用说明**：

DeepSeek 支持两种方式开启思考模式：
1. 使用 `deepseek-reasoner` 模型（自动启用）
2. 在请求中添加 `thinking` 参数：
   - 直接 HTTP 请求：`{"thinking": {"type": "enabled"}}`
   - OpenAI SDK：`extra_body={"thinking": {"type": "enabled"}}`

**响应格式**：
```json
{
  "choices": [{
    "message": {
      "role": "assistant",
      "content": "最终回答",
      "reasoning_content": "思考过程..."
    }
  }]
}
```

**流式响应**：
- `delta.reasoning_content` - 思考内容增量
- `delta.content` - 最终回答增量

## v2.7.10 (2025-03-27)

### 重要变更

**配置格式标准化为 PascalCase**：
- 统一所有配置项键名格式为首字母大写的驼峰格式（PascalCase）
- 移除所有配置兼容代码，简化解析逻辑
- 使用 `toon.Unmarshal` 直接解析到结构体，不再手动解析 map
- 配置文件示例：`APIConfig`、`HTTPServer`、`Security` 等

### 修复

**Security 配置解析问题**：
- 修复配置文件中使用 `Security`（首字母大写）时无法正确解析的问题
- 问题原因：代码只检查小写的 `security` 键名，忽略了 `Security` 格式
- 解决方案：统一所有配置项为 PascalCase 格式

### 修改的文件

- `config.go` - 完全重写配置解析逻辑，使用结构体标签直接解析
- `cron.go` - 更新 CronJob、ChannelConf、CronFile 的 toon 标签
- `cron_tools.go` - 更新 parseChannelConf 支持新旧格式兼容
- `discord_channel.go` / `discord_stub.go` - 更新 DiscordConfig 标签
- `slack_channel.go` / `slack_stub.go` - 更新 SlackConfig 标签
- `feishu_channel.go` / `feishu_stub.go` - 更新 FeishuConfig 标签

## v2.7.9 (2025-03-27)

### 修复

**定时任务移除时终止运行中的实例**：
- 修复移除定时任务时，正在运行的任务实例不会被终止的问题
- 新增 `runningJobs` map 跟踪正在运行的任务及其取消函数
- `RemoveJob` 现在会检查任务是否正在运行，如果是则调用 cancel 函数终止
- 新增 `IsJobRunning` 方法用于检查任务是否正在运行
- `handleCronRemove` 返回消息现在会告知用户任务是否被终止

### 修改的文件

- `cron.go` - 新增 `runningJobs` 字段，修改 `executeJob` 和 `RemoveJob`，新增 `IsJobRunning`
- `cron_tools.go` - 更新 `handleCronRemove` 返回更友好的消息

## v2.7.8 (2025-03-27)

### 修复

**浏览器工具问题修复**：
1. **选择器语法错误**：修复 `BrowserExtractElements` 工具在选择器包含双引号时导致的 JavaScript 语法错误
   - 问题：选择器如 `a[href*="news"]` 会破坏 JavaScript 字符串
   - 解决：对选择器中的特殊字符进行转义
2. **超时时间优化**：将浏览器默认超时从 30 秒增加到 60 秒
3. **错误信息改进**：BrowserError 现在包含超时时间信息，便于诊断问题

### 修改的文件

- `browser_tools.go` - 修复选择器转义问题
- `services.go` - 改进 BrowserError，增加超时时间显示
- `const.go` - 增加浏览器默认超时时间，修复常量命名

## v2.7.7 (2025-03-27)

### 修复

**浏览器操作 panic 问题**：
- 修复 `browser_search` 工具在超时时 panic 导致程序崩溃的问题
- 问题原因：rod 库的 `Must*` 系列方法在 context 超时时会 panic
- 解决方案：使用 Go 的 `recover` 机制捕获 panic 并转换为 error
- 新增 `browserSafeOp` 包装函数，统一处理浏览器操作的 panic 恢复
- 新增 `BrowserError` 错误类型，包含操作名称和原始错误

### 修改的文件

- `services.go` - 为 Search、Visit、Download 函数添加 recover 机制
- `browser_tools.go` - 重写所有浏览器工具函数，添加统一的 panic 恢复

## v2.7.6 (2025-03-27)

### 改进

**Hook 事件命名优化**：
- 将 `BeforeLLMCall` 改名为 `BeforeModelCall`，更准确地描述调用模型的行为
- 相关 API 变更：
  - `HookEventBeforeLLMCall` → `HookEventBeforeModelCall`
  - `HookPayloadBeforeLLM` → `HookPayloadBeforeModel`
  - `RunBeforeLLM()` → `RunBeforeModel()`

### 修改的文件

- `hooks.go` - 重命名 Hook 事件和相关类型
- `AgentLoop.go` - 更新 Hook 调用

## v2.7.5 (2025-03-27)

### 改进

**Hook 系统重构**：
- 移除外部脚本依赖，改为 Go 程序内部回调机制
- 移除 YAML 依赖（不再使用 `gopkg.in/yaml.v3`）
- 新增 `HookFunc` 类型，Hook 作为 Go 函数实现
- 内置两个 Hook：
  - `dangerous-command-check` - 检测危险命令（rm -rf、dd 等）并发出警告
  - `audit-log` - 记录所有工具调用到审计日志
- API 变更：`RunBeforeLLM`、`RunBeforeTool`、`RunAfterTool` 只返回 `*HookResult`，不再返回 `error`

### 修改的文件

- `hooks.go` - 完全重写，改为内部回调机制
- `AgentLoop.go` - 适配新的 Hook API
- `api_handlers.go` - 适配新的 Hook API
- 删除 `hooks/` 目录下的脚本文件

## v2.7.4 (2025-03-27)

### 改进

**移动端设置模态弹窗优化**：
- 修复模型管理、角色管理、演员管理、技能管理标签页在移动端屏幕的显示问题
- 之前：左右分栏布局在移动端无法正常显示，横排内容溢出
- 现在：
  - 移动端使用垂直堆叠布局，列表和详情分开显示
  - 选中项目后显示详情页，添加返回按钮返回列表
  - 桌面端保持原有的左右分栏布局

### 修改的文件

- `ChatSettingsModelTab.svelte` - 移动端响应式布局
- `ChatSettingsRolesTab.svelte` - 移动端响应式布局
- `ChatSettingsActorsTab.svelte` - 移动端响应式布局
- `ChatSettingsSkillsTab.svelte` - 移动端响应式布局

## v2.7.3 (2025-03-27)

### 修复

**记忆系统死锁问题**：
- 修复 `memory.go` 中 `Save` 方法调用 `save()` 时的 RWMutex 死锁问题
- 问题原因：`Save` 持有写锁后调用 `save()`，而 `save()` 内部又尝试获取读锁，Go RWMutex 不支持递归锁导致死锁
- 解决方案：重构 `save()` 方法不再获取锁（假设调用者已持有锁），新增 `saveWithLock()` 方法用于需要获取锁的场景
- 影响工具：`memory_save`、`memory_forget` 等记忆相关工具调用时会永久阻塞

**全局管理器空指针检查**：
- 为 `memory_tools.go` 中 6 个工具处理函数添加 `globalMemoryManager` nil 检查
- 为 `cron_tools.go` 中 4 个工具处理函数添加 `globalCronManager` nil 检查
- 为 `task_tools.go` 中 6 个工具处理函数添加 `globalTaskManager` nil 检查
- 防止初始化失败时程序 panic

### 修改的文件

- `memory.go` - 修复死锁问题，重构 `save()` 方法
- `memory_tools.go` - 添加 `globalMemoryManager` nil 检查
- `cron_tools.go` - 添加 `globalCronManager` nil 检查
- `task_tools.go` - 添加 `globalTaskManager` nil 检查

## v2.6.2 (2025-03-26)

### 新增功能

**多渠道聊天应用支持**：
- 新增按需加载机制，通过 Build Tags 控制编译哪些渠道
- 支持以下聊天平台（需单独启用）：
  - **Telegram** - 使用 telebot.v3 SDK，支持流式响应、群组策略
  - **Discord** - 使用 Gateway WebSocket，支持心跳、群组策略
  - **Slack** - 使用 Socket Mode，支持线程回复、表情反应
  - **飞书/Lark** - 使用 HTTP API，支持消息卡片、表情反应

**按需加载机制**：
- 默认构建只包含核心渠道（CLI、HTTP、WebSocket、Email）
- 扩展渠道通过环境变量启用，减少二进制体积
- 构建命令：
  ```bash
  ENABLE_TELEGRAM=1 ./build.sh     # 启用 Telegram
  ENABLE_DISCORD=1 ./build.sh      # 启用 Discord
  ENABLE_SLACK=1 ./build.sh        # 启用 Slack
  ENABLE_FEISHU=1 ./build.sh       # 启用飞书
  ENABLE_ALL_CHANNELS=1 ./build.sh # 启用所有渠道
  ```

### 配置示例

```toml
# Telegram 配置
telegram_config:
  enabled: true
  token: "YOUR_BOT_TOKEN"
  allow_from: ["*"]
  group_policy: "mention"
  streaming: true

# Discord 配置
discord_config:
  enabled: true
  token: "YOUR_BOT_TOKEN"
  allow_from: ["*"]
  group_policy: "mention"

# Slack 配置
slack_config:
  enabled: true
  bot_token: "xoxb-xxx"
  app_token: "xapp-xxx"
  reply_in_thread: true
  react_emoji: "eyes"

# 飞书配置
feishu_config:
  enabled: true
  app_id: "cli_xxx"
  app_secret: "xxx"
  group_policy: "mention"
  reply_to_message: true
```

### 技术细节

- 使用 Build Tags 实现按需加载（`// +build telegram` 等）
- 每个渠道独立文件，包含实现和存根两个版本
- 新增依赖：`gopkg.in/telebot.v3`（仅 Telegram 渠道需要）

### 修改的文件

- `config.go` - 添加多渠道配置结构
- `main.go` - 添加渠道启动逻辑
- `build.sh` - 支持多渠道构建选项
- `go.mod` / `go.sum` - 添加 telebot 依赖
- `telegram_channel.go` / `telegram_stub.go` - Telegram 渠道
- `discord_channel.go` / `discord_stub.go` - Discord 渠道
- `slack_channel.go` / `slack_stub.go` - Slack 渠道
- `feishu_channel.go` / `feishu_stub.go` - 飞书渠道
- `embed/index.html` - WebUI 嵌入页面占位

## v2.6.0 (2025-03-26)

### 新增功能

**Shell 工具系统重构**：
- 重命名 `delayed_exec` → `shell_delayed`，使命名更加清晰明确
- 重命名相关工具：
  - `task_check` → `shell_delayed_check`
  - `task_terminate` → `shell_delayed_terminate`
  - `task_list` → `shell_delayed_list`
  - `task_wait` → `shell_delayed_wait`
  - `task_remove` → `shell_delayed_remove`
- 改进工具描述，明确区分 `shell` 与 `shell_delayed` 的使用场景
- 添加详细的使用示例和适用场景说明

**超时配置系统**：
- 新增统一的超时配置机制
- 配置文件支持 `timeout` 字段自定义超时值
- 可配置项：
  - `shell`: Shell 命令超时（默认 60 秒）
  - `http`: HTTP 请求超时（默认 120 秒）
  - `plugin`: 插件调用超时（默认 30 秒）
  - `browser`: 浏览器操作超时（默认 30 秒）

### 改进

- `shell` 工具添加默认超时保护，防止 ssh/scp/rsync 等阻塞命令无限等待
- `text_search` 工具添加 toon 标签支持，修复序列化错误
- 工具描述添加清晰的使用指引（✅ 推荐场景 / ❌ 不推荐场景）

### 修复

- 修复 `text_search` 工具返回 "Failed to marshal search results" 错误的问题

### 修改的文件

- `getTools.go` - 重命名工具，更新描述
- `AgentLoop.go` - 更新 case 语句
- `shell.go` - 添加超时保护
- `const.go` - 添加默认超时常量
- `config.go` - 添加 TimeoutConfig 结构和解析逻辑
- `main.go` - 添加全局超时配置变量
- `file.go` - 为 TextSearchResult 添加 toon 标签
- `task_tools.go` - 工具处理函数

## v2.5.21 (2025-03-26)

### 修复
- 修复模型管理页面无法编辑主模型的问题
  - 之前编辑 `main` 模型时返回错误「请通过配置 API 更新主模型」
  - 现在可以通过 `/api/models/main` 接口更新主模型配置
  - 更新会同步保存到主配置文件和内存缓存
- 修复配置向导生成的配置文件中 API 类型为空的问题
  - 用户直接按回车保留默认值时，APIType 未被正确设置
- 修复配置向导生成的配置文件中默认角色为空的问题
  - 新增默认角色设置为 `coder`

### 修改的文件
- `api_handlers.go` - 修改 `updateModelAPI` 函数，支持更新 main 模型
- `actor.go` - 新增 `UpdateMainModel` 方法
- `config_wizard.go` - 修复 APIType 默认值设置，新增 DefaultRole 默认值

## v2.5.19 (2025-03-26)

### 新增
- **首次启动配置向导**：改善第一次启动时的配置设置体验
  - 终端：不再直接生成配置并退出，而是进入配置模型的交互式循环
  - 逐步引导用户配置：API 类型、Base URL、API 密钥、模型标识
  - 支持 Ctrl+C 随时退出，已输入的配置会保存
  - Ollama 模式自动跳过 API Key 配置
- **模型管理 API**：新增 `/api/models` 系列接口
  - `GET /api/models` - 列出所有模型
  - `POST /api/models` - 创建新模型
  - `GET /api/models/:name` - 获取模型详情
  - `PUT /api/models/:name` - 更新模型
  - `DELETE /api/models/:name` - 删除模型

### 修复
- 修复配置向导完成后 `PluginsDir` 为空的问题
- 修复插件目录路径为空时的错误提示
- 修复模型管理页面「没有找到模型」的问题

### 修改的文件
- `config.go` - 新增 ModelConfig 结构和加载逻辑
- `main.go` - 添加配置向导检查逻辑和全局变量
- `http_server.go` - 新增 /api/models 路由
- `api_handlers.go` - 新增模型管理 API
- `plugin.go` - 空路径检查
- `config_wizard.go` - 新增配置向导实现

## v2.5.17 (2025-03-25)

### 新增
- **macOS 编译支持**（通过 Go 原生交叉编译，无需虚拟机）：
  - `darwin-amd64` - macOS Intel (x86_64)
  - `darwin-arm64` - macOS Apple Silicon (M1/M2/M3)
- 现已支持 **11 个目标平台**

### 支持的平台
| 目标 | 说明 |
|------|------|
| `linux-amd64` | Linux x86_64 |
| `linux-arm64` | Linux ARM64 |
| `alpine-amd64` | Alpine Linux (静态链接) |
| `alpine-arm64` | Alpine ARM64 |
| `loong64` | 龙芯处理器 |
| `darwin-amd64` | macOS Intel |
| `darwin-arm64` | macOS Apple Silicon |
| `windows-amd64` | Windows |
| `freebsd-amd64` | FreeBSD |
| `ghostbsd-amd64` | GhostBSD |

### 用法
```bash
# macOS Intel
./docker-build.sh darwin-amd64 --cn

# macOS Apple Silicon (M1/M2/M3)
./docker-build.sh darwin-arm64 --cn

# 构建所有 11 个平台
./docker-build.sh all --cn
```

## v2.5.16 (2025-03-25)

### 修复
- 修复 Dockerfile Go 版本问题：改用 `golang:alpine` (latest) 以支持 go.mod 要求的 Go 1.25+

## v2.5.15 (2025-03-25)

### 改进
- Docker 构建优化：
  - 移除不必要的依赖（git, musl-dev），减少构建时间
  - 新增 `--cn` 参数支持国内镜像加速（Alpine/Go/pnpm）
  - 新增 `--no-cache` 参数禁用 Docker 缓存
  - 支持构建缓存，避免重复下载
- 新增 `webui/.npmrc` 配置，消除 pnpm 构建脚本警告

## v2.5.14 (2025-03-25)

### 新增
- **Docker 跨平台编译支持**：
  - `Dockerfile` - 多阶段构建，支持交叉编译
  - `docker-compose.yml` - 便捷的多平台构建配置
  - `docker-build.sh` - 统一的构建入口脚本
  - `.dockerignore` - 优化构建上下文
- **支持的目标平台**：
  - Linux AMD64 (glibc)
  - Linux ARM64 (glibc, 树莓派等)
  - Alpine Linux AMD64 (musl, 静态链接)
  - Alpine Linux ARM64 (musl, 静态链接)
  - LoongArch 64位 (龙芯处理器)
  - Windows AMD64
  - FreeBSD AMD64
  - GhostBSD AMD64 (独立目标，输出文件明确标识)

### 用法
```bash
# 国内用户推荐使用 --cn 加速
./docker-build.sh linux-amd64 --cn
./docker-build.sh alpine-amd64 --cn
./docker-build.sh ghostbsd-amd64 --cn
./docker-build.sh all --cn

# 构建所有平台
./docker-build.sh all
```

## v2.5.13 (2025-03-25)

### 优化
- 配置代码分割（code splitting），将大型依赖库分离为独立 chunk：
  - `katex` - 数学公式渲染
  - `highlight` - 代码高亮
  - `marked` - Markdown 解析
  - `pdfjs` - PDF 预览
  - `shiki` - 代码语法高亮
- 提高 chunk size 警告阈值至 5000 kB，消除构建警告

## v2.5.12 (2025-03-25)

### 修复
- 修复构建脚本中颜色代码不显示的问题：将 `echo` 改为 `printf`
- 改进依赖检查输出，使用 ✓/✗ 符号清晰显示状态
- 添加 `--check-deps` 参数单独检查依赖并显示安装指南

## v2.5.11 (2025-03-25)

### 改进
- 强化构建脚本对 GhostBSD/FreeBSD 的支持：
  - 自动检测操作系统类型
  - 新增 `--check-deps` 参数，可单独检查依赖并提供安装指南
  - 针对 GhostBSD/FreeBSD 提示 npm 需要单独安装（pkg install npm-node24）
  - 构建失败时自动回退到 npm 重试
  - 彩色输出增强可读性
  - 检查 package.json 中是否有 sass-embedded 残留
  - 构建完成后显示运行提示

## v2.5.10 (2025-03-25)

### 修复
- 移除已弃用的 `hast` 包（类型定义已在 `@types/hast` 中提供）
- 改用 `katex/dist/katex.min.css` 替代自定义 SCSS，消除 Sass `@import` 弃用警告
- 删除不再使用的 `katex-custom.scss` 文件

## v2.5.9 (2025-03-25)

### 修复
- 将 `sass-embedded` 替换为 `sass`（纯 JavaScript 实现），解决在某些 Unix 系统上的兼容性问题

## v2.5.8 (2025-03-25)

### 改进
- 构建脚本增加 pnpm 支持，优先级调整为：pnpm > npm > bun
- 简化脚本逻辑，更清晰易读

## v2.5.7 (2025-03-25)

### 改进
- 构建脚本优先使用 npm（更稳定），仅在 npm 不可用时才使用 bun
- 添加更详细的错误提示信息

## v2.5.6 (2025-03-25)

### 改进
- 构建脚本增加自动安装依赖：如果 node_modules 不存在，自动运行 bun install 或 npm install

## v2.5.5 (2025-03-25)

### 改进
- 重写 build.sh 构建脚本，使用 POSIX shell 兼容语法，兼容所有 Unix 系统
  - 使用 `#!/bin/sh` 替代 `#!/bin/bash`
  - 使用 POSIX 兼容的重定向语法 `> /dev/null 2>&1`
  - 添加 npm 回退检测

## v2.5.4 (2025-03-25)

### 改进
- 将「删除所有对话」按钮的文字颜色改为白色，提升可读性

## v2.5.3 (2025-03-25)

### 改进
- 调整设置模态窗口标签页顺序，使之更合理：
  1. 模型管理（核心配置）
  2. 采样（模型参数）
  3. 惩罚（模型参数）
  4. 角色管理
  5. 演员管理
  6. 技能管理
  7. 常规（界面偏好）
  8. 显示
  9. 导入/导出
  10. 开发者
- 默认打开设置时定位到「模型管理」而非「常规」

## v2.5.2 (2025-03-25)

### 修复
- 修复 ChatSettings.svelte 中 `settingSections` 数组重复 `MODEL` 项导致的 `{#each}` key 重复错误
- 修复 text_replace_tools.go 中的语法错误：类型断言语法 `argsMap[key).(` 改为 `argsMap[key].(`

## v2.5.1 (2025-03-25)

### 修复
- 修复 text_replace_tools.go 中的语法错误：类型断言语法 `argsMap[key).(` 改为 `argsMap[key].(`

## v2.5.0 (2025-03-25)

### 新增功能
- **文本替换工具**：新增功能强大的文本处理工具套件，参考 sed 命令设计
  - `text_replace`：强大的文本替换工具，支持：
    - 字符串替换和正则表达式替换
    - 行范围限制（起始行、结束行）
    - 行模式过滤（只处理/排除匹配的行）
    - 操作类型：replace(替换)、delete(删除行)、print(打印匹配行)、count(计数)
    - 全局替换或单次替换
    - 大小写敏感/忽略
    - 原地修改文件、备份文件、模拟运行
  - `text_grep`：类 grep 的文件行搜索工具，支持正则表达式和上下文行显示
  - `text_transform`：文本转换工具，支持大小写转换、排序、去重、反转、添加行号、移除空行

### 技术细节
- 新增 `text_replace_tools.go` 实现文本处理核心逻辑
- 在 `getTools.go` 中添加 OpenAI 和 Anthropic 格式的工具定义
- 在 `AgentLoop.go` 中添加工具调用处理

## v2.4.5 (2025-03-25)

### 修复
- 重新构建前端静态文件，修复模态设置侧栏导航顺序
- 确认顺序正确：角色管理 → 演员管理 → 技能管理

## v2.4.4 (2025-03-25)

### 修复
- 彻底清理代码中所有 persona 变量名残留，统一改为 role：
  - `currentPersona` → `currentRole` (AgentLoop.go)
  - `persona` 参数 → `role` 参数 (getTools.go, CallModel.go, const.go)
  - `BuildToolSectionForPersona` → `BuildToolSectionForRole` (const.go)
- 修复环境变量残留：`DEFAULT_PERSONA` → `DEFAULT_ROLE` (config.go)
- 修复 JSON 配置键名残留：`default_persona` → `default_role` (config.go)
- 修复帮助示例：`persona_coder` → `role_coder` (role_tools.go)

## v2.4.3 (2025-03-25)

### 修复
- 修复终端日志中残留的 persona 名称：Persona manager → Role manager
- 修复配置文件 JSON 标签：default_persona → default_role
- 修复角色热加载日志中的 persona 残留
- 修复角色配置文件路径：persona.toon → role.toon

## v2.4.2 (2025-03-25)

### 修复
- 确认模态设置侧栏导航顺序正确：角色 → 演员 → 技能

## v1.0.12 (2025-03-25)

### 修复
- 修复清除默认角色功能：之前无法将默认角色设置为空（清除），现已修正
- 改进配置更新逻辑，正确检测请求中是否包含 `default_role` 字段

## v1.0.9 (2025-03-24)

### 新增功能
- **角色持久化功能**：在角色管理页面添加「设为默认」按钮，可将当前选中的角色保存为默认角色
- 程序重启后会自动加载配置中保存的默认角色，不再每次恢复为程序员角色
- 默认角色显示星标标识，支持一键清除默认设置

### 修改
- 配置文件新增 `default_role` 字段用于持久化存储默认角色
- 配置 API 新增 `default_role` 字段的读取与保存支持

## v1.0.8 (2025-03-24)

### 修复
- 清理打包文件，排除 node_modules，大幅减小压缩包体积（从 100M+ 降至 20M）

## v1.0.7 (2025-03-24)

### 新增功能
- **设置系统重构**：彻底改造设置模态弹窗，与角色系统、技能系统、配置管理深度整合
- **后端 API 接口**：
  - `GET/PUT /api/config` - 配置管理
  - `GET/POST /api/roles` - 角色列表/创建
  - `GET/PUT/DELETE /api/roles/:name` - 角色详情/更新/删除
  - `GET/POST /api/skills` - 技能列表/创建
  - `GET/PUT/DELETE /api/skills/:name` - 技能详情/更新/删除
- **前端新标签页**：
  - 「模型配置」- API 类型、Base URL、API 密钥、模型选择等
  - 「角色管理」- 角色列表、创建/编辑/删除角色
  - 「技能管理」- 技能列表、创建/编辑/删除技能

