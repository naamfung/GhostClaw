# GhostClaw WebUI

GhostClaw 的 Web 前端界面，基于 SvelteKit 构建，编译后嵌入 Go 后端二进制文件中。

## 技术栈

| 层级 | 技术 | 说明 |
|------|------|------|
| 框架 | SvelteKit + Svelte 5 | 使用 Runes 响应式系统（`$state`、`$derived`、`$effect`） |
| UI 组件 | shadcn-svelte + bits-ui | 可访问的组件库 |
| 样式 | TailwindCSS 4 | 工具类优先的 CSS |
| 数据库 | IndexedDB (Dexie) | 客户端持久化存储对话和消息 |
| 构建 | Vite | 快速打包，使用静态适配器 |
| 测试 | Vitest | 单元测试 |
| Markdown | marked + rehype/remark | Markdown 渲染、KaTeX 公式、代码高亮 |

## 项目结构

```
webui/
├── src/
│   ├── lib/
│   │   ├── components/
│   │   │   ├── app/          # 应用组件（chat、dialogs、forms、navigation 等）
│   │   │   └── ui/           # shadcn-svelte 基础组件
│   │   ├── hooks/            # Svelte Hooks
│   │   ├── services/         # API 服务层（chat、config、models、roles、skills 等）
│   │   ├── stores/           # Svelte 状态管理（chat、conversations、models、settings 等）
│   │   ├── types/            # TypeScript 类型定义
│   │   └── utils/            # 工具函数
│   ├── routes/
│   │   ├── +layout.svelte    # 全局布局（侧边栏、导航）
│   │   ├── +page.svelte      # 首页
│   │   └── chat/[id]/        # 聊天页面
│   └── styles/               # 全局样式
├── static/                   # 静态资源
└── docs/                     # 架构流程文档
```

## 编译

```bash
cd webui
npm install
npm run build
```

构建产物输出到 `embed/index.html`，由 Go 后端通过 `embed.FS` 嵌入二进制文件。生产构建会生成 GZIP 压缩的 `index.html.gz`（内联所有 CSS、JS、字体和 favicon），通过 GhostClaw 的 HTTP 服务器直接提供服务。

## 开发

```bash
npm run dev          # 启动 Vite 开发服务器（默认 http://localhost:5173）
npm run preview      # 预览生产构建
npm run test         # 运行 Vitest 测试
```

开发模式下 Vite 代理 API 请求到 GhostClaw 后端（默认 `http://localhost:10086`）。
