# GhostClaw

GhostClaw 是一个基于 LLM 的多渠道 AI Agent，Go 语言开发。通过命令行、Web、聊天应用与模型交互，所有渠道共享同一会话。

## 安装

```bash
git clone https://github.com/naamfung/GhostClaw.git --depth=1
cd GhostClaw && ./build.sh && ./ghostclaw
```

Docker 构建：
```bash
./docker-build.sh linux-amd64     # 单平台
./docker-build.sh all --cn        # 全平台（国内加速）
docker compose up runtime         # Docker Compose 运行
```

## 首次配置

首次运行自动进入交互式配置向导（选 API、填 Key、选模型），完成后自动生成 `config.toon`。也可手动编辑：

```yaml
Models:
  "deepseek-chat":
    ModelBase:
      Name: deepseek-chat
      APIType: "openai"
      BaseURL: "https://api.deepseek.com/v1"
      APIKey: "${DEEPSEEK_API_KEY}"
      Model: deepseek-chat
      IsDefault: true
```

内置 20+ 主流 LLM 提供商自动识别（OpenAI、Anthropic、DeepSeek、Gemini、通义千问、智谱、Groq、Ollama 等）。

## 启动

```bash
./ghostclaw              # Log 模式（后台运行）
./ghostclaw --repl        # REPL 模式（对话）
./ghostclaw -p "你好"     # 单次提问
```

Log 模式下按 `/` 切换到 REPL；REPL 中 `/quit` 切回 Log，`/exit` 退出。

## WebUI

启动后打开 `http://localhost:10086`，完整对话、切换模型/角色/技能、MCP 管理等功能。

## 核心功能

| 功能 | 说明 |
|------|------|
| **多渠道会话** | CLI、Web、Telegram、Discord、Slack、飞书等 12 种渠道共享上下文 |
| **角色系统** | 预置 9 个角色 + 40+ 自定义角色，切换人设及工具权限 |
| **技能系统** | 轻量能力模板，跨角色共享 |
| **多角色协作** | 自动轮流对话，适用于创作、群像 RP |
| **Tasks 模式** | 结构化任务分解：探索 → 设计 → 执行 |
| **子代理（Spawn）** | 后台并行独立任务，结果自动回传 |
| **记忆系统** | 自动整合长对话，重要信息持久化 |
| **Shell 工具** | 同步/异步/智能三种模式，后台任务自动唤醒 |
| **浏览器自动化** | 网页搜索、访问、截图等 |
| **SSH 远程** | 远程执行命令 |
| **定时任务** | 模型自主安排 cron |
| **插件系统** | Lua 动态插件 |

## 角色系统

```
/role list          # 列出所有角色
/role coder         # 切换到程序员
```

预置角色：`coder`（程序员）、`novelist`（小说家）、`screenwriter`（编剧）、`translator`（翻译）、`teacher`（教师）等。`roles/custom/` 下添加 `.md` 文件即可创建自定义角色，热加载生效。

## 技能系统

```
/skill              # 列出技能
/skill code_review  # 激活代码审查
```

技能文件在 `skills/` 目录，包含 `SKILL.md` 描述及可选脚本、模板。

## 多角色协作

```
/stage auto on            # 开启自动演绎
/actor hero_lin           # 添加角色
/actor villain_mozun      # 添加角色
开始写林风与魔尊的对决      # 系统自动切换角色视角
```

## Tasks 模式（结构化任务分解）

复杂任务先探索再执行，一个 `Tasks` 工具统一管理：

```
Tasks(PlanPhase="explore")                           # 探索：只讀工具分析代碼
Tasks(PlanPhase="design", PlanContent="...", Tasks=[...])  # 設計：方案 + 任務列表
Tasks(PlanPhase="execute")                           # 執行：退出計劃，按 Tasks 執行
```

每個 Task 用 `Todos(list_id="task_<id>")` 管理子任務，Task 之間無依賴，模型自行決定順序。

## Shell 工具

| 工具 | 场景 |
|------|------|
| `Shell` | 快速同步命令（ls、cat、grep） |
| `ShellDelayed` | 长时后台任务（编译、部署、下载），完成自动唤醒 |
| `SmartShell` | 自动判断执行方式 |

## 浏览器 / SSH / 定时任务

- **浏览器**：27 个工具，网页搜索、访问、截图等
- **SSH**：`ssh_connect` / `ssh_exec` / `ssh_close`，同步/异步执行
- **Cron**：`cron_add` / `cron_list` / `cron_remove`，模型自主安排

## 扩展渠道

```bash
ENABLE_TELEGRAM=1 ./build.sh   # 启用 Telegram
ENABLE_DISCORD=1 ./build.sh    # 启用 Discord
ENABLE_ALL_CHANNELS=1 ./build.sh  # 全部
```

支持 Telegram、Discord、Slack、飞书、IRC、Webhook、XMPP、Matrix、Email 等 12 种渠道。群组策略推荐 `mention`（仅响应 @提及）。

## 常用命令

| 命令 | 说明 |
|------|------|
| `/exit` | 退出程序 |
| `/help` | 帮助 |
| `/role <名称>` | 切换角色 |
| `/skill <名称>` | 激活技能 |
| `/save [描述]` | 保存会话 |
| `/load [ID]` | 加载会话 |
| `/new` | 新会话 |
| `/session` | 当前会话信息 |
| `/context` | 上下文使用情况 |
| `/model` | 查看/切换模型 |
| `/stage auto on` | 开启自动演绎 |
| `/next` | 下一角色发言 |
| `/stop` | 取消当前任务 |

## 许可证

Apache License Version 2.0
