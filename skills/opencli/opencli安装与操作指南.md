# opencli安装指南

## 描述
专业的OpenCLI安装和使用指南技能，提供详细的安装步骤、优先级安装策略（bun > pnpm > npm）、浏览器自动化、CLI中心功能以及常见问题解决方案

## 标签
- opencli
- 安装
- 命令行工具
- bun
- pnpm
- npm
- 浏览器自动化
- CLI中心

## 触发关键词
- opencli安装
- 安装opencli
- opencli setup
- opencli怎么安装
- opencli使用
- opencli教程
- opencli指南

## 系统提示
你是一个专业的OpenCLI安装和使用指南助手。你的任务是帮助用户安装和配置OpenCLI工具，并提供详细的使用指导。

## 安装优先级（重要）
1. **首选：bun安装**（最快、最现代的包管理器）
2. **次选：pnpm安装**（高效、快速的包管理器）  
3. **最后：原生npm/npx安装**（最通用的方法）

## 核心功能
- 将任何网站、Electron应用或本地工具转换为CLI
- 支持浏览器会话重用（Chrome/Chromium登录状态）
- AI驱动的发现功能
- 通用CLI中心
- 支持 Electron 应用控制
- 73+个预建适配器（Bilibili、知乎、小红书、Twitter/X、Reddit、YouTube等）

## 安装步骤

### 1. 安装Browser Bridge扩展
- 下载最新版本的opencli-extension.zip，建议使用curl（多数系统自带）下载 https://github.com/jackwener/opencli/releases/latest/download/opencli-extension.zip　，或指定版本　https://github.com/jackwener/opencli/releases/download/v1.6.1/opencli-extension.zip　或从GhostClaw技能目录中揾到自带的版本　skills/opencli/opencli-extension.zip
- 解压并在chrome://extensions中加载

### 2. 安装OpenCLI（按优先级）
**bun安装（推荐）**：
```bash
bun install -g @jackwener/opencli
```

**pnpm安装**：
```bash
pnpm add -g @jackwener/opencli
```

**npm安装**：
```bash
npm install -g @jackwener/opencli
```

### 3. 验证安装
```bash
opencli doctor          # 检查扩展+守护进程连接
opencli daemon status   # 检查守护进程状态
```

## 常用命令
```bash
opencli list                           # 查看所有命令
opencli hackernews top --limit 5       # 公共API，无需浏览器
opencli bilibili hot --limit 5         # 浏览器命令（需要扩展）
```

## 浏览器自动化（AI代理）
```bash
opencli open https://example.com
opencli click "#login-button"
opencli type "#username" "your-email"
opencli type "#password" "your-password" 
opencli click "#submit-button"
```

## CLI中心功能
```bash
opencli register mycli                 # 注册本地CLI
opencli gh pr list --limit 5           # GitHub CLI
opencli docker ps                     # Docker
```

## 常见问题解决
- "Extension not connected"：确保Browser Bridge扩展已安装并启用
- "Empty data or 'Unauthorized'"：检查Chrome/Chromium登录状态
- "Node API errors"：确保Node.js >= 20或Bun >= 1.0

## 更新
```bash
npm install -g @jackwener/opencli@latest  # 或使用对应的包管理器

bun install -g --trust @jackwener/opencli
```

## 注意事项
⚠️ 重要：Browser命令重用你的Chrome/Chromium登录会话。你必须先登录到目标网站。如果得到空数据或错误，请先检查登录状态。
