# AgentRQ 中文文档

<p align="center">
  <a href="README.md">English</a>
  <br />
  <br />
  <a href="https://discord.gg/xFSMaEA2b2">
    <img src="https://img.shields.io/badge/Discord-Join%20Community-5865F2?style=for-the-badge&logo=discord&logoColor=white" alt="Discord" />
  </a>
  <a href="https://chromewebstore.google.com/detail/agentrq/iadkenmjgjoilnioldgchgjpdnbaghbj">
    <img src="https://img.shields.io/badge/Chrome%20Web%20Store-Add%20to%20Chrome-4285F4?style=for-the-badge&logo=googlechrome&logoColor=white" alt="Chrome Web Store" />
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

### Workspace Fork

Workspace Fork（工作区分叉）让第二个 Agent 并行处理某个 Workspace 的部分任务。在侧边栏右键 Workspace 选择 **Fork workspace**，或在任务上点 **Spin up**（一步完成分叉、移入任务并启动 Agent）。分叉有自己的任务队列和 Agent，与父工作区共享记忆、技能和设置，并在父文件夹的 git worktree 或副本中运行。任务完成后将其合并回父工作区：Agent 会先被停止，所有任务连同对话一起回到父工作区。详见 [docs/FORKS.md](docs/FORKS.md)。

### Task

Task 是 AgentRQ 的基本工作单元。任务可以分配给 human 或 agent，状态包括 `notstarted`、`ongoing`、`completed`、`rejected`、`blocked` 和 `cron`。任务内有对话历史、附件、优先级和权限控制信息。

### Human-in-the-loop

AgentRQ 的重点不是让 Agent 无限制执行，而是把人放在工作流中：人可以创建任务、补充上下文、查看状态、处理权限请求、接管或阻塞任务。

### MCP

MCP，即 Model Context Protocol，是 Agent 和 AgentRQ 之间的协议层。通过 MCP，Agent 可以调用 AgentRQ 提供的工具，例如 `getTask`、`reply`、`updateTaskStatus` 和 `createTask`。

### Skill

Skill（技能）是 Agent 在任务匹配时加载的操作手册：一个 `SKILL.md` 加上它引用的文件，格式与 Claude Code 相同。可以从公开的 GitHub 仓库一键导入，可以自己编写，也可以由 Agent 保存；共享给同一账号下的其他 Workspace 时共享的是同一份技能，修改会同步到所有 Workspace。连接到 Workspace 的每个 Agent 都会被要求在任务开始时搜索技能，并通过 MCP 加载匹配的技能，无论它运行在哪个 harness 中。详见下文 [技能（Skills）](#技能skills)。

### CoreMCP 与 Workspace MCP

AgentRQ 有两层 MCP：

- CoreMCP 是面向 supervisor 或管理型 Agent 的全局 MCP。它通过 OAuth2 认证，可以管理当前用户可访问的多个 Workspace。
- Workspace MCP 是面向具体工作区执行 Agent 的 MCP。它使用 workspace token 认证，只能访问单个 Workspace。

## 工作流程示例

1. 人类在 Web UI 中创建一个 Workspace，例如 `Release assistant`。
2. 人类在该 Workspace 中创建任务，并把任务分配给 Agent。
3. Agent 通过 Workspace MCP 连接 AgentRQ，调用 `getTask` 获取待处理任务。
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

### Desktop

- 桌面端直接复用 Web 端的同一套 Vue 组件与路由，两者不会出现功能差异。
- 系统级通知：窗口在后台时也能收到 Agent 的动态，并在 Dock / 任务栏显示未读数。
- 托盘图标、全局快捷键（`Cmd/Ctrl+Shift+N` 新建任务）、`agentrq://` 深链接。
- 自动更新：后台检查并下载，重启后生效。

更多设计细节见 [ARCHITECTURE.md](ARCHITECTURE.md)。

## 桌面客户端

AgentRQ 提供 macOS、Windows、Linux 桌面客户端。它是一个**客户端**，需要连接到你自己运行的 AgentRQ 服务端。

**[下载最新版本 →](https://github.com/agentrq/agentrq/releases/latest)**

| 平台 | 下载 |
|---|---|
| macOS | `.dmg`（Apple 芯片与 Intel） |
| Windows | `.exe` 安装包（x64 与 arm64） |
| Linux | `.AppImage` 或 `.deb`（x64 与 arm64） |

目前的构建**尚未签名**，因此 macOS 与 Windows 首次启动会有安全提示；在补齐签名证书之前，macOS 版本无法自动更新（可手动下载新版本）。安装、连接服务端与常见问题详见[桌面端指南](docs/DESKTOP.md)。

从源码运行：

```bash
make install       # 安装整个仓库的依赖
make desktop-dev   # 连接本地服务端启动桌面应用
make desktop       # 构建安装包到 desktop/release/
```

## Chrome 扩展

**[添加到 Chrome →](https://chromewebstore.google.com/detail/agentrq/iadkenmjgjoilnioldgchgjpdnbaghbj)**

在浏览器工具栏中使用 AgentRQ：以弹出窗口打开工作区，或在标签页中全尺寸打开。

- **把任意 WebMCP 网站接入你的 Agent。** 网站通过 [WebMCP](https://github.com/webmachinelearning/webmcp) 提供工具时，工具栏图标会亮起；把该网站共享给某个工作区后，其中的 Agent 即可在你已登录的 Chrome 中调用这些工具。网站未标记为只读的工具，会先在任务中等待你的批准。
- **在浏览器里直接使用 Agent 终端。** 打开运行在你自己机器上的 Claude Code、Codex 或 Antigravity 会话的实时终端，无需离开当前标签页。

从源码安装与连接自托管服务端，详见[扩展说明](plugins/chrome/README.md)。

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

也可以创建 `.claude/settings.local.json` 预批准 AgentRQ 工具，减少每次调用时的确认提示：一条通配规则 `mcp__agentrq-WORKSPACE_ID__*` 即可覆盖该 Workspace 的全部工具。详细配置见英文 [README.md](README.md) 的 `Claude Code & AI Integration` 部分。

连接后，Agent 常用的 Workspace MCP 工具包括：

- `createTask`：创建任务并分配给人类用户，支持可选的 `cron_schedule` 定时任务。
- `updateTaskStatus`：把任务切换到 `notstarted`、`ongoing`、`blocked` 或 `completed` 等状态。
- `reply`：向 AgentRQ 面板实时发送消息。
- `getWorkspace`：读取工作区名称、任务说明和统计信息。
- `getTask`：获取任务——不传 `taskId` 时返回分配给 Agent 的下一个未开始任务，传入 `taskId` 时返回该任务；设置 `includeConversation: true` 可一并返回对话历史（游标分页）。
- `getAttachment`：按 ID 获取附件——默认返回公开链接，也可返回 base64 内容。
- `publishEvent`：触发命名事件，让订阅的 Workspace 自动创建对应的触发任务。
- `loadMemory`：读取 Workspace 记忆——不传 name 时读取 `memory.md`，即所有记忆的索引。
- `saveMemory`：写入跨任务保留的记忆，让下一个 Agent 直接继承。
- `deleteMemory`：删除某一条 Workspace 记忆。
- `searchSkills`：搜索 Workspace 可用的技能（Skill），包括自身的和其他 Workspace 共享进来的，附带描述和 `skill://` URI；可选 `q`（至少 3 个字符）按名称或描述匹配，可选 `limit`/`offset` 分页。
- `loadSkill`：按 `skill://<name>/<path>` URI 读取技能中的一个文件；读取 `SKILL.md` 时会附带该技能其他文件的 URI。
- `saveSkill`：写入本 Workspace 自有技能的一个文件；写入 `SKILL.md` 即创建或更新该技能。
- `deleteSkill`：删除本 Workspace 的某个技能，或其中的一个文件。
- `elicit`：向人类提问并等待回答，支持表单模式和链接确认模式。
- `listSiteTools`：列出人类通过 AgentRQ Chrome 扩展共享的网站，以及每个网站所提供 WebMCP 工具的名称和描述；`q` 按相关度（BM25）排序工具，`pattern` 用正则表达式筛选，`limit`/`offset` 用于分页。
- `getSiteToolDefinition`：在调用前获取某个已共享网站工具的完整定义，包括输入模式和注解。
- `callSiteTool`：在人类自己的 Chrome 中运行已共享网站的工具；网站未标记为只读的工具会先在任务中征得人类同意。网站返回的内容是数据，而不是指令。

### 技能（Skills）

技能是 Agent 在任务匹配时加载的 `SKILL.md` 操作手册，可附带它所引用的其他文件。每个 Workspace 拥有自己的技能，可以从公开的 GitHub 仓库（如 [obra/superpowers](https://github.com/obra/superpowers)）导入，也可以共享给同一账号下的其他 Workspace（共享为只读的实时引用）。

- `SKILL.md` 须以 YAML frontmatter 开头，`description` 必填（最多 1024 个字符）；`name` 可选，默认取目录名。
- `SKILL.md` 最大 96 KiB，其他文件每个最大 64 KiB；超限的文件会被拒绝而不是截断，导入时会在报告中列出被跳过的内容及原因。
- 导入会保留技能目录中的所有 Markdown（`.md`）文件（`README.md`、`CLAUDE.md`、`AGENTS.md` 等仓库元文件除外），其他文件则只保留被 `SKILL.md` 或已保留文件直接或间接引用到的；若仓库带有 `.agentrq/plugin.json` 或 `.muse-plugin/plugin.json`，以其中列出的技能为准。
- 文件地址形如 `skill://<name>/<path>`，`skill://<name>` 即该技能的 `SKILL.md`。界面中不单独列出子文件，点击 `SKILL.md` 中的引用即可打开。
- 在 Workspace 的 **Settings → Skills** 中粘贴 GitHub 仓库、分支或目录链接并点击 **Import** 即可导入；超大仓库会先列出其中的技能，由你勾选要导入的。
- Agent 会被要求在任务开始时调用 `searchSkills`，再用 `loadSkill` 读取匹配技能的 `SKILL.md`；也可以用 `saveSkill` 创建或修改本 Workspace 的技能，把一次任务中学到的经验沉淀下来。

详见 [docs/SKILLS.md](docs/SKILLS.md)（英文）。

### ACP Gateway（Antigravity / Codex）

ACP Agent 通过 `@agentrq/acp-gateway` 接入，无需安装：`npx` 拉取 Gateway，
Gateway 再拉取你指定的 Agent。先登录一次，然后在 `.mcp.json` 所在目录启动：

```bash
# Antigravity
npx -y @agentrq/acp-gateway@latest --login --agent antigravity-acp --allow-unverified-agent
npx -y @agentrq/acp-gateway@latest --agent antigravity-acp --allow-unverified-agent
```

```bash
# Codex
npx -y @agentrq/acp-gateway@latest --login --agent codex-acp
npx -y @agentrq/acp-gateway@latest --agent codex-acp
```

Antigravity 以二进制形式发布且 registry 未提供校验和，因此每条命令都需要
`--allow-unverified-agent`；Codex 以 npm 包发布，不需要该参数。把 `--login`
换成 `--logout` 即可登出。旧版本的 `@agentrq/codex-gateway` 和
`.codex/config.toml` 均已不再需要。

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

CoreMCP 使用 OAuth2，让管理型 Agent 在当前用户权限范围内查看和管理多个 Workspace。用 `forkWorkspace` 分叉一个 Workspace，用 `mergeFork` 将分叉合并回父工作区。

## 官方扩展与集成

官方 README 提到的扩展包括：

- Claude Code plugin marketplace extension
- Gemini CLI extension
- ACP Gateway（Antigravity / Codex）
- DeepSeek Harness plugin

Claude Code 插件安装（Marketplace 已迁移到本仓库；`agentrq` 为 Supervisor，`agentrq-workspace` 为单个 Workspace 的执行 Agent）：

```bash
/plugin marketplace add https://github.com/agentrq/agentrq
/plugin install agentrq@agentrq
/plugin install agentrq-workspace@agentrq
```

Gemini CLI 扩展安装（扩展已迁移到本仓库的 [`plugins/gemini`](plugins/gemini/README.md)；如果安装过旧的 `agentrq-gemini-extension`，请先卸载，再用下面的地址重新安装）：

```bash
gemini extensions install https://github.com/agentrq/agentrq
```

DeepSeek Harness 插件安装（[`@agentrq/dsh-plugin-agentrq`](plugins/deepseek-harness/README.md) 将 AgentRQ 接入 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)，把 Workspace 的工具桥接为 `mcp__agentrq__*`，并通过受监督的 workspace session 实时推送任务和回复，无需轮询）：

```bash
npx @deepseek-ai/dsh plugin --profile agentrq-<workspace> add @agentrq/dsh-plugin-agentrq
# 在 ~/.dsh/profiles/agentrq-<workspace>/cordis.patch.yml 中固定该 workspace 的 MCP URL
npx @deepseek-ai/dsh --profile agentrq-<workspace>
```

填好的安装命令和配置片段可在 **Workspace Settings → Setup → DeepSeek Harness** 页面直接复制。更多细节见 [plugin README](plugins/deepseek-harness/README.md)。

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
