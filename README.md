# GhostClaw

多渠道 AI Agent。Go 语言，CLI / Web / 聊天应用均可交互，所有渠道共享同一会话。

## 安装

```bash
git clone https://github.com/naamfung/GhostClaw.git --depth=1
cd GhostClaw && ./build.sh && ./ghostclaw
```

## 首次配置

首次运行自动进入配置向导（选 API、填 Key、选模型），完成后生成 `config.toon`。也可手动编辑：

```yaml
Models:
  "deepseek-chat":
    ModelBase:
      APIType: "openai"
      BaseURL: "https://api.deepseek.com/v1"
      APIKey: "${DEEPSEEK_API_KEY}"
      Model: deepseek-chat
      IsDefault: true
```

API Key 支持环境变量 `${VAR_NAME}` 引用。内置 20+ 主流 LLM 提供商自动识别。

## 启动

```bash
./ghostclaw              # Log 模式（后台，终端显日志）
./ghostclaw --repl        # 直接对话
./ghostclaw -p "你好"     # 单次提问
```

Log 模式下按 `/` 切换到对话；对话中 `/quit` 切回 Log，`/exit` 退出。

WebUI：`http://localhost:10086`

## 常用命令

| 命令 | 说明 |
|------|------|
| `/help` | 帮助 |
| `/exit` | 退出 |
| `/stop` | 取消当前任务 |
| `/role <名称>` | 切换角色 |
| `/skill <名称>` | 激活技能 |
| `/stage auto on` | 开启多角色自动演绎 |
| `/actor <名称>` | 切换到指定演员 |
| `/next` | 手动推进下一演员发言 |
| `/model [名称]` | 查看或切换模型 |
| `/save [描述]` | 保存会话 |
| `/load [ID]` | 加载会话（不带 ID 列出全部） |
| `/new` | 开始新会话 |
| `/session` | 查看当前会话信息 |
| `/context` | 查看上下文用量 |

角色、技能、会话、模型等详细信息可分别通过对应命令查看（如 `/role list`、`/skill list`、`/session list`）。

## 许可证

Apache License Version 2.0
