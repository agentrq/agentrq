# AgentRQ 中文文档

<p align="center">
  <a href="README.md">English</a>
  <br />
  <br />
  <a href="https://www.youtube.com/watch?v=GBAoSpuCzrU">Watch on YouTube in HD</a>
  <br />
  <br />
  <a href="https://discord.gg/xFSMaEA2b2">
    <img src="https://img.shields.io/badge/Discord-Join%20Community-5865F2?style=for-the-badge&logo=discord&logoColor=white" alt="Discord" />
  </a>
</p>

> 本文是面向中文开发者的导读，帮助快速理解 AgentRQ 的定位、架构和本地运行方式。
>
> 注意：中文版本可能滞后于英文文档；如需获取最新信息，请尽可能优先参考英文 [README.md](README.md)。英文 [README.md](README.md)、[SETUP.md](SETUP.md)、[DOCKER.md](DOCKER.md) 和 [ARCHITECTURE.md](ARCHITECTURE.md) 仍是维护时的权威资料；如果两者不一致，请以英文文档和当前代码为准。
>
> Note: This Simplified Chinese version may lag behind the English documentation. For the most up-to-date information, please refer to the English [README.md](README.md). The English README, SETUP, DOCKER, and ARCHITECTURE docs remain the source of truth.

## AgentRQ 是什么

AgentRQ 是一个面向“人类操作者 + AI Agent”的协作平台。它不是模型本身，也不是某个 Agent CLI 的替代品，而是提供一个可视化工作区，让人和 Agent 围绕任务、状态、消息、权限请求、附件和通知协同。

AI Agent 通过 MCP 连接到工作区：读取任务、更新状态、回复消息、下载附件或创建新任务；人类则通过 Web UI 观察进度、补充信息或处理需要批准的操作。

官方 README 没有说明 “RQ” 的展开含义，本文不做额外定义。可以把它理解为围绕任务队列或请求队列组织 Agent 工作流的产品名。

## 适合谁使用

- 想把 Claude Code、Codex、Gemini CLI 等 Agent 接入统一任务面板的开发者。
- 想把复杂目标拆成可追踪任务，并让 Agent 和人类来回协作的团队。
- 想自托管一个 MCP 驱动的 Agent-Human 协作平台的人。
- 想研究 Go + Vue + MCP + 实时通知架构的贡献者。

## 核心概念

### Workspace

Workspace 是一个独立的任务空间，通常对应一个项目、仓库或目标。它包含名称、任务说明、Agent 可读取的上下文、MCP 连接地址、令牌和权限设置。每个 Workspace 都有自己的 Workspace MCP endpoint，Agent 连接后只能看到该工作区内的信息。

### Task

Task 是 AgentRQ 的基本工作单元。任务可以分配给 human 或 agent，状态包括 `notstarted`、`ongoing`、`completed`、`rejected`、`blocked` 和 `cron`。任务内有对话历史、附件、优先级和权限控制信息。

### Human-in-the-loop

AgentRQ 的重点不是让 Agent 无限制执行，而是把人放在工作流中：人可以创建任务、补充上下文、查看状态、处理权限请求、接管或阻塞任务。

### MCP

MCP，即 Model Context Protocol，是 Agent 和 AgentRQ 之间的协议层。通过 MCP，Agent 可以调用 AgentRQ 提供的工具，例如 `getNextTask`、`reply`、`updateTaskStatus` 和 `createTask`。

### CoreMCP 与 Workspace MCP

AgentRQ 有两层 MCP：

- CoreMCP 是面向 supervisor 或管理型 Agent 的全局 MCP。它通过 OAuth2 认证，可以管理当前用户可访问的多个 Workspace。
- Workspace MCP 是面向具体工作区执行 Agent 的 MCP。它使用 workspace token 认证，只能访问单个 Workspace。

## 工作流程示例

1. 人类在 Web UI 中创建一个 Workspace，例如 `Release assistant`。
2. 人类在该 Workspace 中创建任务，并把任务分配给 Agent。
3. Agent 通过 Workspace MCP 连接 AgentRQ，调用 `getNextTask` 获取待处理任务。
4. Agent 开始执行，调用 `updateTaskStatus` 把任务改为 `ongoing`。
5. Agent 遇到问题时调用 `reply`，把进度、疑问或权限请求同步到 AgentRQ。
6. 人类在 UI 中回复或批准操作。
7. Agent 完成后调用 `updateTaskStatus` 把任务改为 `completed`。

对于多工作区场景，也可以让 supervisor Agent 通过 CoreMCP 发现所有 Workspace，并把子任务分发给不同 Workspace 中的 specialist Agent。

## 技术架构

AgentRQ 采用前后端分离和 MCP 服务层组合的架构。

### Backend

- Go + Fiber 提供 REST API。
- 集成 MCP server，暴露 Workspace MCP 和 CoreMCP。
- GORM 管理数据访问，默认可使用 SQLite，自托管生产环境建议使用 PostgreSQL。
- Google OAuth2 和 JWT 负责用户认证。
- 内部 Pub/Sub 与 SSE 负责实时事件同步。
- 可选集成 Slack、SMTP、Web Push 等通知能力。

### Frontend

- Vue 3 + Vite 构建前端应用。
- Pinia 管理状态。
- Tailwind CSS 提供样式。
- 通过 SSE 接收后端实时事件，保持任务和消息同步。

更多设计细节见 [ARCHITECTURE.md](ARCHITECTURE.md)。

## 快速开始

如果只是想先体验完整产品，优先看 Docker 自托管路径；如果想参与代码开发，再走源码开发路径。

### Docker 本地体验

Docker 是最直接的本地体验方式，完整步骤见 [SETUP.md](SETUP.md) 和 [DOCKER.md](DOCKER.md)。

核心流程如下：

```bash
docker pull agentrq/agentrq:latest
mkdir -p _storage
docker run -d \
  --name agentrq \
  --restart unless-stopped \
  -p 2026:2026 \
  --env-file .env \
  -v ./_storage:/_storage \
  agentrq/agentrq:latest
```

启动后访问：

```text
http://localhost:2026
```

你需要准备 `.env`。本地体验常用配置包括：

```env
ENV=production
PORT=2026
AGENTRQ_BASE_URL=http://localhost:2026
AGENTRQ_DOMAIN=localhost

AGENTRQ_SQLITE_ENABLED=true
AGENTRQ_SQLITE_DSN=./_storage/agentrq.db

AGENTRQ_AUTH_JWT_SECRET=CHANGE-ME-TO-A-LONG-RANDOM-SECRET-32-CHARS-MIN
AGENTRQ_AUTH_WORKSPACE_TOKEN_KEY=CHANGE-ME-EXACTLY-32-BYTES-LONG!
AGENTRQ_AUTH_ROOT_LOGIN_ENABLED=true
AGENTRQ_AUTH_ROOT_ACCESS_TOKEN=CHANGE-ME-ROOT-TOKEN

AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_SECRET=your-client-secret
```

注意：

- `AGENTRQ_AUTH_WORKSPACE_TOKEN_KEY` 必须正好是 32 bytes。修改它会导致已有 Workspace MCP token 无法解密。
- 本地可以临时开启 root login，生产环境应关闭。
- Google OAuth 回调地址本地通常是 `http://localhost:2026/api/v1/auth/google/callback`。

### 源码开发

源码开发需要：

- Go 1.21+
- Node.js 18+ 和 npm
- Google OAuth2 Client ID / Client Secret

安装依赖：

```bash
make install
```

启动完整开发环境：

```bash
make dev
```

前端开发服务器默认是：

```text
http://localhost:5173
```

后端默认监听 `http://localhost:3000`，Vite 会把 `/api` 和 `/mcp` 代理到后端。

Windows 提示：当前 `Makefile` 使用了 `lsof`、`xargs`、`kill` 等 Unix 工具。如果你在 Windows PowerShell 中没有这些命令，可以分别在两个终端中启动后端和前端：

```powershell
cd backend/cmd/server
New-Item -ItemType Directory -Force _storage
go build -o agentrq_binary.exe main.go
.\agentrq_binary.exe
```

```powershell
cd frontend
npm install
npm run dev
```

后端配置默认从 `backend/cmd/server/_config/base.yaml` 读取，并支持 `AGENTRQ_*` 环境变量覆盖。源码开发时如果登录或 MCP token 相关功能异常，优先核对 OAuth、JWT secret、workspace token key 和 base URL。

## 认证与配置

常见配置项：

| 变量 | 作用 |
| --- | --- |
| `AGENTRQ_BASE_URL` | 当前 AgentRQ 对外访问地址，例如 `http://localhost:2026` |
| `AGENTRQ_DOMAIN` | 域名，不带协议，例如 `localhost` |
| `AGENTRQ_AUTH_JWT_SECRET` | 签发会话 JWT 的密钥，建议 32 字符以上随机值 |
| `AGENTRQ_AUTH_WORKSPACE_TOKEN_KEY` | MCP workspace token 加密密钥，必须正好 32 bytes |
| `AGENTRQ_AUTH_ROOT_LOGIN_ENABLED` | 是否启用 root login，建议仅本地初始化使用 |
| `AGENTRQ_AUTH_ROOT_ACCESS_TOKEN` | root login 使用的访问令牌 |
| `AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_ID` | Google OAuth2 Client ID |
| `AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_SECRET` | Google OAuth2 Client Secret |
| `AGENTRQ_SQLITE_ENABLED` | 是否使用 SQLite |
| `AGENTRQ_POSTGRES_ENABLED` | 是否使用 PostgreSQL |

生产部署通常应使用 HTTPS、关闭 root login，并优先考虑 PostgreSQL。完整生产部署说明见 [SETUP.md](SETUP.md)。

## AI Agent 接入

AgentRQ 可以接入多种 Agent CLI 或 MCP 客户端。每个 Workspace 的 MCP URL 和 token 可在 AgentRQ workspace 的 Setup modal 中找到。

### Claude Code

在项目根目录创建 `.mcp.json`，填入 Workspace MCP URL：

```json
{
  "mcpServers": {
    "agentrq-WORKSPACE_ID": {
      "type": "http",
      "url": "YOUR_MCP_URL"
    }
  }
}
```

也可以创建 `.claude/settings.local.json` 预批准 AgentRQ 工具，减少每次调用时的确认提示。详细配置见英文 [README.md](README.md) 的 `Claude Code & AI Integration` 部分。

连接后，Agent 常用的 Workspace MCP 工具包括：

- `createTask`：创建任务并分配给人类用户，支持可选的 `cron_schedule` 定时任务。
- `updateTaskStatus`：把任务切换到 `notstarted`、`ongoing`、`blocked` 或 `completed` 等状态。
- `reply`：向 AgentRQ 面板实时发送消息。
- `getWorkspace`：读取工作区名称、任务说明和统计信息。
- `getTaskMessages`：分页读取任务对话历史。
- `getNextTask`：获取下一个分配给 Agent 的未开始任务。
- `downloadAttachment`：按 ID 下载附件。

### Codex Gateway

Codex 可通过 `@agentrq/codex-gateway` 连接 AgentRQ：

```bash
npm install -g @agentrq/codex-gateway@latest
codex-gateway
```

它通常需要同时配置：

- `.mcp.json`：供 `codex-gateway` 接收 AgentRQ 任务。
- `.codex/config.toml`：供 Codex agent 在执行任务时直接调用 AgentRQ MCP tools。

详细配置见英文 [README.md](README.md) 的 `Codex Gateway` 部分。

### ACP / Gemini

ACP Agent 可通过 `@agentrq/acp-gateway` 接入，例如 Gemini CLI：

```bash
npm install -g @agentrq/acp-gateway
acp-gateway -- gemini --acp
```

详细说明见英文 [README.md](README.md) 的 `ACP Gateway` 部分。

### Supervisor / CoreMCP

Supervisor Agent 可连接全局 CoreMCP：

```json
{
  "mcpServers": {
    "agentrq": {
      "type": "http",
      "url": "https://mcp.agentrq.com/mcp"
    }
  }
}
```

CoreMCP 使用 OAuth2，让管理型 Agent 在当前用户权限范围内查看和管理多个 Workspace。

## 官方扩展与集成

官方 README 提到的扩展包括：

- Claude Code plugin marketplace extension
- Gemini CLI extension
- ACP Gateway
- Codex Gateway

Claude Code 扩展安装：

```bash
/plugin marketplace add https://github.com/agentrq/agentrq-claude-extension
/plugin install agentrq@agentrq
```

Gemini CLI 扩展安装：

```bash
gemini extensions install https://github.com/agentrq/agentrq-gemini-extension
```

集成能力包括：

- [Slack Integration](integrations/slack/README.md)
- SMTP 邮件通知
- Web Push / PWA 原生推送

## 常见问题

### AgentRQ 是一个 Agent 吗？

不是。AgentRQ 更像一个 Agent-Human 协作平台或任务编排平台。它让外部 Agent 通过 MCP 接入任务系统，但它本身不是大语言模型，也不是 Claude Code、Codex 或 Gemini 的替代品。

### 我应该先用 Docker 还是源码启动？

只想体验产品，先用 Docker。想贡献代码或调试前后端，再用源码开发环境。

### 英文 README 和中文 README 不一致怎么办？

以英文 README、SETUP.md、ARCHITECTURE.md 和当前代码为准。中文文档主要作为导读，帮助中文开发者更快进入项目。

### 本地一定要配置 Google OAuth 吗？

正常用户登录依赖 Google OAuth2。自托管本地初始化可以临时启用 root login，但生产环境应关闭。

### RQ 是什么意思？

仓库当前文档没有给出官方展开。不要在文档或 PR 描述中擅自定义它；可以把 AgentRQ 当作产品名理解。

## 贡献指南

如果你想从中文文档开始贡献，建议保持低风险、小范围：

1. 先阅读 [README.md](README.md)、[SETUP.md](SETUP.md)、[DOCKER.md](DOCKER.md) 和 [ARCHITECTURE.md](ARCHITECTURE.md)。
2. 不确定的产品表述不要自行扩展，优先引用英文文档和代码事实。
3. 文档 PR 可以从翻译、补充快速开始、修复链接、澄清概念开始。
4. 提交前检查 Markdown 链接和格式。
5. PR 标题可以使用 `docs: add Simplified Chinese README`。

许可证见英文 [README.md](README.md) 的 License 部分。
