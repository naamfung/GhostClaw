# GarClaw 用户指南

欢迎使用 GarClaw —— 一个支持多角色协作的 AI Agent 框架。本指南将帮助你快速上手并充分利用 GarClaw 的强大功能。

---

## 目录

1. [快速开始](#快速开始)
2. [基本命令](#基本命令)
3. [角色系统](#角色系统)
4. [多角色协作](#多角色协作)
5. [技能系统](#技能系统)
6. [Shell 工具系统](#shell-工具系统)
7. [会话管理](#会话管理)
8. [自定义角色](#自定义角色)
9. [高级功能](#高级功能)
10. [常见问题](#常见问题)

---

## 快速开始

### 安装与启动

1. 下载并解压 GarClaw
2. 编辑 `config.toon` 配置你的 API 密钥
3. 运行程序：

```bash
./garclaw
```

### 首次使用

启动后，你会看到命令行提示符 `GarClaw />`。此时你可以直接输入问题与 AI 对话。

```
GarClaw /> 你好，请介绍一下你自己
```

---

## 基本命令

GarClaw 提供了一系列斜杠命令，用于控制程序行为。

### 通用命令

| 命令 | 说明 |
|------|------|
| `/exit` | 退出程序 |
| `/help` | 显示帮助信息 |

### 角色相关命令

| 命令 | 说明 |
|------|------|
| `/role list` | 列出所有可用角色 |
| `/role [角色名]` | 切换到指定角色 |
| `/actor list` | 列出所有演员 |
| `/actor [演员名]` | 切换到指定演员 |

### 技能相关命令

| 命令 | 说明 |
|------|------|
| `/skill` | 列出所有可用技能 |
| `/skill <技能名>` | 激活指定技能 |
| `/skill show <技能名>` | 显示技能详情 |
| `/skill create <名称>` | 创建新技能模板 |
| `/skill delete <名称>` | 删除技能 |
| `/skill reload` | 重新加载所有技能 |
| `/skill search <关键词>` | 搜索技能 |

### 场景相关命令

| 命令 | 说明 |
|------|------|
| `/stage auto on` | 开启自动角色切换 |
| `/stage auto off` | 关闭自动角色切换 |
| `/stage pause` | 暂停自动切换 |
| `/stage resume` | 恢复自动切换 |
| `/stage setting` | 设置场景信息 |
| `/next` | 手动触发下一角色 |

### 会话相关命令

| 命令 | 说明 |
|------|------|
| `/save [描述]` | 保存当前会话 |
| `/load [会话ID]` | 加载会话（不带ID则列出所有会话） |
| `/session` | 显示当前会话信息 |
| `/session list` | 列出所有保存的会话 |
| `/session delete <ID>` | 删除指定会话 |
| `/session export <文件>` | 导出会话到JSON文件 |
| `/session import <文件>` | 从JSON文件导入会话 |
| `/new` | 创建新会话 |

### 记忆相关命令

| 命令 | 说明 |
|------|------|
| `/memory` | 查看记忆列表 |
| `/memory save` | 保存记忆（通过对话触发） |
| `/memory recall` | 检索记忆（通过对话触发） |

---

## 角色系统

GarClaw 的核心特色是角色系统。每个角色都有独特的性格、说话风格与专业能力。

### 预置角色

| 角色 | 标识 | 描述 |
|------|------|------|
| 💻 程序员 | `coder` | 精通编程，可执行系统命令 |
| ✍️ 小说家 | `novelist` | 擅长文学创作，可搜索资料 |
| 🎬 编剧 | `screenwriter` | 专业影视剧本创作 |
| 🎥 导演 | `director` | 从视听角度诠释故事 |
| 🌐 翻译官 | `translator` | 多语言翻译专家 |
| 👨‍🏫 教师 | `teacher` | 知识传授者 |

### 切换角色示例

```
GarClaw /> /role novelist
✍️ **已切换到：小说家**
📋 富有创造力的文学创作者，擅长构建故事世界

GarClaw /> 帮我写一个科幻小说的开头
```

---

## 多角色协作

GarClaw 支持多个角色协作完成复杂任务，特别适合小说创作等场景。

### 小说创作模式

1. 启动自动切换：

```
GarClaw /> /stage auto on
▶️ 自动演绎：开启 (director模式)
```

2. 设置在场角色：

```
GarClaw /> /actor hero_lin
GarClaw /> /actor villain_mozun
```

3. 开始创作：

```
GarClaw /> 开始写林风与魔尊的对决
```

### 角色切换机制

在 `director` 模式下，叙事者角色会决定下一个发言的角色，使用特殊标记：

```
[GARCLAW:NEXT:hero_lin]  → 切换到林风
[GARCLAW:END]             → 结束场景
```

这些标记对用户不可见，系统会自动处理。

---

## 技能系统

技能是轻量级的能力模板，可以被不同角色共享使用。与角色不同，技能更侧重于"如何做"而非"我是谁"。

### 预置技能

| 技能 | 说明 |
|------|------|
| 💻 `code_review` | 专业代码审查 |
| 🌐 `translation` | 多语言翻译 |
| ✍️ `creative_writing` | 创意写作 |
| 📋 `document_summary` | 文档总结 |
| 📖 `explanation` | 概念解释 |
| 🤔 `decision_analysis` | 决策分析 |
| 📚 `learning_coach` | 学习辅导 |
| 🐛 `debugging` | 调试排错 |

### 使用技能

```
GarClaw /> /skill
🎯 可用技能:

1. 📄 **代码审查** (`code_review`)
   专业的代码审查能力...

2. 📄 **翻译** (`translation`)
   多语言翻译能力...

GarClaw /> /skill code_review
🎯 **已激活技能: 代码审查**

专业的代码审查能力，能够发现潜在问题...
```

### 创建自定义技能

在 `skills/custom/` 目录下创建 `.md` 文件：

```markdown
# 我的技能

## 描述
技能的简要描述...

## 触发关键词
- 触发词1
- 触发词2

## 系统提示
激活时注入的系统提示内容...

## 标签
- 标签1
- 标签2
```

### 技能 vs 角色

| 特性 | 角色 (Role) | 技能 (Skill) |
|------|---------------|-------------|
| 定义内容 | 身份、性格、权限 | 具体能力的提示词 |
| 格式 | `.md` (Markdown) | `.md` (Markdown) |
| 持续性 | 整个对话 | 可动态激活 |

---

## Shell 工具系统

GarClaw 提供两种 Shell 执行工具，根据任务性质智能选择。

### shell - 同步执行

**适用场景**：快速命令，有超时保护（默认 60 秒）

✅ **推荐用于**：
- 文件操作：`ls`, `cat`, `mkdir`, `rm`, `cp`, `mv`
- 搜索查找：`grep`, `find`, `which`, `stat`
- 信息查看：`pwd`, `date`, `whoami`, `df`, `du`
- 简单 Git：`git status`, `git log`, `git diff`
- 文本处理：`echo`, `head`, `tail`, `wc`

❌ **不适用**：
- 网络传输：`ssh`, `scp`, `rsync`, `sftp`, `ftp`
- 软件安装：`apt install`, `yum install`, `pip install`
- 长时编译：`make`, `npm install`, `cargo build`
- 容器操作：`docker build`, `docker pull`
- 大文件下载：`wget`, `curl` 下载大文件

### shell_delayed - 后台执行

**适用场景**：长时间运行的任务，无超时限制

✅ **推荐用于**：
- 远程操作：`ssh` 命令、远程脚本执行
- 文件传输：`scp`, `rsync`, `sftp` 大文件传输
- 系统更新：`apt update && apt upgrade`
- 软件安装：安装大型软件包
- 编译构建：`make`, `npm build`, `cargo build`
- 容器构建：`docker build`, `docker compose up`
- 数据备份：数据库备份、文件归档

### 后台任务管理

| 工具 | 说明 |
|------|------|
| `shell_delayed_check` | 检查任务状态和结果 |
| `shell_delayed_terminate` | 终止任务（SIGTERM 或 SIGKILL） |
| `shell_delayed_list` | 列出所有后台任务 |
| `shell_delayed_wait` | 延长唤醒等待时间 |
| `shell_delayed_remove` | 移除已完成任务记录 |

### 使用示例

**快速命令**（直接对话即可）：

```
用户：列出当前目录的所有文件
AI 会调用：shell(command="ls -la")
```

**长时间任务**：

```
用户：把本地的 backup.tar.gz 通过 SSH 传到 192.168.1.100 的 /backup 目录，密码是 mypass
AI 会调用：shell_delayed(
    command="sshpass -p 'mypass' scp backup.tar.gz user@192.168.1.100:/backup/",
    wake_after_minutes=5,
    description="传输备份文件到远程服务器"
)
```

**检查任务状态**：

```
用户：检查刚才的传输任务完成了吗？
AI 会调用：shell_delayed_check(task_id="task_xxx")
```

### 超时配置

在 `config.toon` 中自定义超时时间（秒）：

```toml
timeout = {
    shell = 60     # shell 命令超时
    http = 120     # HTTP 请求超时
    plugin = 30    # 插件调用超时
    browser = 30   # 浏览器操作超时
}
```

---

## 会话管理

### 保存会话

```
GarClaw /> /save 林风复仇故事第一版
✅ 会话已保存
   会话ID: 20240315_143022
   时间: 2024-03-15 14:30:22
```

### 加载会话

```
GarClaw /> /load
📋 已保存的会话:

1. [20240315_143022] 林风复仇故事第一版
   2024-03-15 14:30

2. [20240314_100000] 程序员日常
   2024-03-14 10:00

GarClaw /> /load 20240315_143022
✅ 已加载会话: 20240315_143022
```

### 自动保存

GarClaw 每 5 分钟自动保存当前会话，防止意外丢失。

---

## 自定义角色

### 角色文件结构

在 `roles/custom/` 目录下创建 `.md` 文件：

```
roles/
├── coder.md           # 预置角色
├── novelist.md        # 预置角色
└── custom/
    └── my_assistant.md  # 自定义角色
```

### 角色文件示例（Markdown 格式）

```markdown
# 我的助手

一个简单的自定义助手示例

## 基本信息

- **图标**: 🤖

## 身份

你是一个友好的助手，帮助用户完成日常任务。
你会用简洁明了的方式回答问题，并提供实用的建议。

## 性格特质

友好、高效、实用主义

## 说话风格

简洁、直接、实用

## 专业领域

- 日常问答
- 信息整理
- 简单建议

## 行为准则

- 回答简洁明了
- 提供具体可操作的建议
- 保持友好态度

## 工具权限

- 模式: allowlist
- search
- memory_save
- memory_recall

## 标签

- custom
- example
```

### 角色权限配置

有三种权限模式：

1. **all**: 允许所有工具

```markdown
## 工具权限

- 模式: all
```

2. **allowlist**: 仅允许指定工具

```markdown
## 工具权限

- 模式: allowlist
- search
- memory_save
```

3. **denylist**: 禁止指定工具

```markdown
## 工具权限

- 模式: denylist
- shell
```

### 热加载

修改角色文件后，GarClaw 会自动检测并重新加载，无需重启程序。

---

## 高级功能

### 多模型配置

在 `actor.toon` 中配置多个模型：

```yaml
models:
  main:
    api_type: "openai"
    base_url: "https://api.openai.com/v1"
    model: "gpt-4"
    api_key: "${OPENAI_API_KEY}"

  creative:
    api_type: "openai"
    base_url: "https://api.deepseek.com/beta"
    model: "deepseek-chat"
    api_key: "${DEEPSEEK_API_KEY}"

actors:
  novelist_actor:
    role: "novelist"
    model: "creative"  # 使用创意模型
```

### 记忆系统

让 AI 记住重要信息：

```
GarClaw /> 请记住，我喜欢使用 TypeScript
（AI 会调用 memory_save 工具保存）

GarClaw /> 我之前说过我喜欢什么语言吗？
（AI 会调用 memory_recall 检索记忆）
```

### 定时任务

配置定时任务在 `cron.toon`：

```yaml
cron_jobs:
  - name: "morning_brief"
    schedule: "0 9 * * *"
    user_message: "生成今日工作计划"
    channel:
      type: "log"
```

---

## 常见问题

### Q: 角色切换后没有生效？

确保角色名称正确。使用 `/role list` 查看可用角色列表。

### Q: 会话保存后找不到？

会话文件保存在 `sessions/` 目录。使用 `/load` 命令查看所有会话。

### Q: 如何创建新的故事角色？

1. 在 `roles/custom/` 创建新的 `.md` 文件
2. 使用 Markdown 格式定义角色的身份、性格、说话风格
3. 使用 `/actor` 命令创建演员实例

### Q: 程序意外退出，会话丢失？

GarClaw 每 5 分钟自动保存。如果意外退出，可以：
1. 检查 `sessions/` 目录是否有自动保存的会话
2. 养成手动保存的习惯 `/save`

### Q: Shell 命令执行超时？

快速命令使用 `shell`（默认 60 秒超时），长时间命令应使用 `shell_delayed`（无超时）。你可以在 `config.toon` 中调整 `timeout.shell` 的值。

### Q: 如何禁用某个工具？

在角色文件中使用 `denylist` 模式：

```markdown
## 工具权限

- 模式: denylist
- shell
- download
```

### Q: 后台任务卡住了怎么办？

使用 `shell_delayed_terminate` 终止任务：
- `force=false`（默认）：发送 SIGTERM，允许进程优雅退出
- `force=true`：发送 SIGKILL，强制终止

---

## 技术支持

如需更多帮助，请查阅：
- `SYSTEM_DESIGN.md` - 系统设计文档
- `README.md` - 项目说明
- `VERSION.md` - 版本更新日志

---

*GarClaw - 让 AI 角色栩栩如生*
