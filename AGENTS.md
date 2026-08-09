# GhostClaw Agent Guide

## Project Overview

GhostClaw is an AI Agent framework built with Go backend and SvelteKit WebUI. The frontend is compiled and embedded into the Go binary using Go's `embed.FS`. The system supports agentic loops, MCP (Model Context Protocol) clients/servers, memory management, skill evolution, role presets, and multi-channel integrations (Telegram, Discord, Slack, Feishu, IRC, XMPP, Matrix, Email, Webhook).

## Technology Stack

### Backend (Go)
- **Language**: Go 1.25.0
- **Database**: SQLite (via `gorm.io/gorm` and `github.com/glebarez/sqlite`)
- **MCP SDK**: `@modelcontextprotocol/sdk` (TypeScript/Go integration)
- **WebSockets**: `github.com/gorilla/websocket`
- **Browser Automation**: `github.com/go-rod/rod`
- **Cron Jobs**: `github.com/robfig/cron/v3`
- **Lisp Evaluation**: `github.com/jig/lisp`, `github.com/yuin/gopher-lua`

### Frontend (WebUI)
- **Framework**: SvelteKit + Svelte 5 (using Runes: `$state`, `$derived`, `$effect`)
- **UI Components**: shadcn-svelte + bits-ui
- **Styling**: TailwindCSS 4
- **Database**: IndexedDB via Dexie (`dexie`)
- **Build Tool**: Vite (static adapter)
- **Testing**: Vitest
- **Markdown Processing**: marked + rehype/remark, KaTeX, Shiki syntax highlighting

## Project Structure

```
GhostClaw/
├── webui/                      # SvelteKit WebUI frontend
│   ├── src/
│   │   ├── lib/
│   │   │   ├── components/     # Svelte components (app/, ui/)
│   │   │   ├── services/       # API services (chat, config, mcp, models, etc.)
│   │   │   ├── stores/         # Svelte state stores (chat, conversations, models, settings, mcp, agentic)
│   │   │   ├── types/          # TypeScript type definitions
│   │   │   └── utils/          # Utility functions
│   │   └── routes/             # SvelteKit routes (+layout.svelte, +page.svelte, chat/[id]/)
│   ├── package.json
│   └── build.sh                # WebUI build script (bunx vite build)
├── builder/                    # Go builder utility
├── main.go                     # Application entry point
├── api_handlers.go             # HTTP API endpoints
├── http_server.go              # HTTP server setup
├── config.go, config_manager.go# Configuration management
├── agent_loop.go, loop_*.go    # Agentic loop modules
├── mcp_client.go, mcp_server.go, mcp_tools.go # MCP implementation
├── tool_registry.go, tool_safety.go, tool_tier.go # Tool registration and safety
├── memory_consolidator.go, unified_memory.go # Memory management
├── skill.go, skill_manager_v2.go, skill_evolution.go # Skill management
├── role.go, role_presets.go    # Role management
├── prompt_engine.go, prompt_cache.go # Prompt engineering and caching
├── context_manager.go, context_compressor.go # Context management
├── session.go, session_persist.go # Session management
└── build.sh                    # Overall build script (Go + WebUI embedding)
```

## Build & Run Commands

### Overall Build (Go + Embedded WebUI)
```bash
./build.sh
```
This script:
1. Checks dependencies (Go, Node.js, npm/pnpm/bun)
2. Builds the WebUI using `bunx vite build` (or configured package manager)
3. Compiles the Go binary with embedded static assets

### WebUI Development
```bash
cd webui
npm install                    # or pnpm install, bun install
npm run dev                    # Start Vite dev server (http://localhost:5173)
npm run build                  # Build for production
npm run preview                # Preview production build
npm run test                   # Run Vitest tests
```

### Go Development & Testing
```bash
go mod tidy
go build -o ghostclaw .
go test ./...
go test -v -run <TestName>
```

### Docker Build
```bash
./docker-build.sh
docker-compose up -d
```

## Architecture & Data Flow

### Backend Architecture
1. **Initialization (`bootstrap.go`, `main.go`)**: Sets up global managers (ConfigManager, RoleManager, ActorManager, SkillManager, SkillManagerV2, MCPServer, UnifiedMemory, ProfileLoader, PluginManager, MCPClientManager, SessionPersistManager, SubagentManager, AuthManager).
2. **API Layer (`api_handlers.go`, `http_server.go`)**: Exposes REST/WebSocket endpoints for chat, models, MCP servers, skills, roles, and configuration.
3. **Agent Loop (`agent_loop.go`, `loop_*.go`)**: Manages agentic execution cycles including planning, tool calling, history management, safety checks, and escalation.
4. **MCP Integration (`mcp_client.go`, `mcp_server.go`, `mcp_tools.go`)**: Supports both MCP client (connecting to external MCP servers) and MCP server (exposing GhostClaw tools to external clients).
5. **Memory & Skills (`unified_memory.go`, `skill_manager_v2.go`)**: Consolidates conversation memory and manages skill lifecycle including evolution.

### Frontend Architecture
1. **Stores (`src/lib/stores/*.svelte.ts`)**: Svelte 5 stores for state management:
   - `chat.svelte.ts`: Chat loading, streaming, processing states, message operations, regeneration, editing
   - `conversations.svelte.ts`: Conversation CRUD, message management, navigation, import/export
   - `models.svelte.ts`: Model listing, selection, loading/unloading, modalities
   - `server.svelte.ts`: Server props, context size, router/model mode detection
   - `settings.svelte.ts`: Configuration, theme, server sync
   - `mcp.svelte.ts`: MCP server management, health checks, tool execution, prompts
   - `mcpResource.svelte.ts`: MCP resource discovery, caching, subscriptions, attachments
   - `agentic.svelte.ts`: Agentic session management, loop execution, tool call normalization
2. **Services (`src/lib/services/*.service.ts`)**: API wrappers for backend endpoints (chat, config, database, mcp, models, roles, skills, upload).
3. **Components**: Organized in `src/lib/components/app/` (chat UI, dialogs, forms, navigation, MCP UI, server status) and `src/lib/components/ui/` (shadcn-svelte base components).

## Code Patterns & Conventions

### Go Backend
- **Global Managers**: Most managers are stored as global variables (e.g., `globalConfigManager`, `globalMCPServer`, `globalSkillManagerV2`) initialized in `bootstrap.go` or `main.go`.
- **Tool Registration**: Tools are registered via `tool_registry.go` with safety tiers (`tool_tier.go`) and safety checks (`tool_safety.go`).
- **Loop Modules**: Agentic loop is modularized into `loop_*.go` files (e.g., `loop_call.go`, `loop_plan.go`, `loop_history.go`, `loop_safety.go`, `loop_escalate.go`).
- **Context Management**: `context_manager.go` handles context compression and token limits.

### Frontend (SvelteKit)
- **Svelte 5 Runes**: Use `$state`, `$derived`, `$effect` for reactive state.
- **Store Exports**: Stores expose reactive getters (e.g., `isLoading()`, `currentResponse()`, `selectedModelId()`).
- **Type Definitions**: Types are in `src/lib/types/` (agentic.d.ts, api.d.ts, chat.d.ts, mcp.d.ts, models.d.ts, settings.d.ts, etc.).
- **API Fetch**: `src/lib/utils/api-fetch.ts` and `src/lib/services/*.service.ts` handle backend communication.

## Important Gotchas

1. **WebUI Embedding**: Production builds generate `embed/index.html.gz` which is embedded into the Go binary via `embed.FS`. The Go HTTP server serves this directly.
2. **Development Proxy**: In dev mode, Vite proxies API requests to GhostClaw backend (default `http://localhost:10086`).
3. **Global State**: Go backend relies heavily on global manager variables. Ensure proper initialization order in `bootstrap.go`/`main.go`.
4. **MCP Server/Client Dual Mode**: GhostClaw acts as both MCP client (connecting to external MCP servers) and MCP server (exposing its own tools).
5. **Agentic Loop State**: Agentic session state is managed in `agenticStore` with `sessions` Map and `isAnyRunning` flag. Tool calls are normalized and streamed via `agenticIsRunning()`, `agenticCurrentTurn()`, `agenticTotalToolCalls()`.

## Testing

### Go Tests
Run all tests:
```bash
go test ./...
```

Specific test files include:
- `agent_loop_test.go`, `api_handlers_test.go`, `auth_test.go`, `callmodel_test.go`
- `config_test.go`, `context_compressor_test.go`, `context_manager_test.go`
- `cron_test.go`, `loop_branch_none_test.go`, `loop_history_test.go`, `loop_post_test.go`
- `memory_yaml_backup_test.go`, `naming_convention_test.go`, `scheduler_test.go`
- `security_test.go`, `self_evolver_test.go`, `session_persist_test.go`, `skill_test.go`
- `task_tools_test.go`, `task_tracker_test.go`, `text_replace_tools_test.go`
- `todo_test.go`, `tool_handlers_test.go`, `tool_result_budget_test.go`, `tool_safety_test.go`

### Frontend Tests
```bash
cd webui
npm run test
```

Tests are in `webui/tests/`:
- `tests/client/`: Svelte component tests
- `tests/e2e/`: Playwright E2E tests (`demo.test.ts`)
- `tests/unit/`: Unit tests (agentic-strip.test.ts, clipboard.test.ts, latex-protection.test.ts, model-id-parser.test.ts, model-names.test.ts, settings-prompt-cache.test.ts, uri-template.test.ts)
- `tests/stories/`: Storybook stories for components

## MCP (Model Context Protocol)

GhostClaw implements both MCP client and MCP server:
- **MCP Client**: `mcp_client.go`, `mcp_client_config.go` - Connects to external MCP servers, fetches tools/prompts/resources.
- **MCP Server**: `mcp_server.go`, `mcp_types.go`, `mcp_tools.go` - Exposes GhostClaw tools and resources to MCP clients.
- **Frontend MCP UI**: `src/lib/components/app/mcp/` - MCP servers settings, resource browser, resource preview, server cards.

## Channels & Integrations

GhostClaw supports multiple communication channels:
- **Messaging**: Telegram (`telegram_channel.go`), Discord (`discord_channel.go`), Slack (`slack_channel.go`), Feishu (`feishu_channel.go`), IRC (`irc_channel.go`), XMPP (`xmpp_channel.go`), Matrix (`matrix_channel.go`), Email (`email_channel.go`), Webhook (`webhook_channel.go`), WebSocket (`ws_channel.go`).
- **Stub files**: `*_stub.go` files provide interface definitions or mock implementations for channels.
