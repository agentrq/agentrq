# AgentRQ ── Agent-Human Collaboration Platform

<p align="center">
  <a href="README.zh-CN.md">简体中文</a>
  <br />
  <br />
  <a href="https://www.youtube.com/watch?v=GBAoSpuCzrU">Watch on YouTube in HD</a>
  <br />
  <br />
  <a href="https://discord.gg/xFSMaEA2b2">
    <img src="https://img.shields.io/badge/Discord-Join%20Community-5865F2?style=for-the-badge&logo=discord&logoColor=white" alt="Discord" />
  </a>
</p>

AgentRQ is a modern, high-performance platform designed for seamless collaboration between human operators and AI agents. It leverages the **Model Context Protocol (MCP)** to allow AI models (like Claude) to interact directly with your workspace's task management system.

## 🚀 Overview

Think of AgentRQ as a shared workspace where humans and AI agents work together seamlessly. You can break down complex goals into manageable tasks, and delegate work directly to your AI agents. 

Because agents "see" the workspace state via MCP, they can autonomously pull their assigned tasks, update statuses, request permissions for sensitive actions, and communicate with you—all synchronized instantly across the platform in real-time.

## ✨ Features

Real captures from the running app — no mockups.

<table>
<tr>
<td width="50%" valign="middle">

### Visual Task Board

Every task Claude creates appears instantly on your board. See what it's working on, what it needs, and what it just finished — all from a clean, fast dashboard you can open on any device, as a list or a Kanban.

</td>
<td width="50%"><img src="https://agentrq.com/assets/feature-task-board.gif" alt="AgentRQ active tasks dashboard and kanban board" width="320" /></td>
</tr>
<tr>
<td width="50%"><img src="https://agentrq.com/assets/feature-task-scheduling.gif" alt="AgentRQ scheduled task auto-spawning into the dashboard" width="320" /></td>
<td width="50%" valign="middle">

### Task Scheduling

Give any task a launch date, or a recurring cadence — every 15 minutes, hourly, daily, weekly, custom days. A background poller ticks every minute and spawns the task the instant it's due, no server or agent needing to stay awake and wait.

</td>
</tr>
<tr>
<td width="50%" valign="middle">

### Events

Events are named signals — `qa_passed`, `deploy_finished`, `blog_published` — that any task can fire when it completes. Wire one to a workspace and that workspace gets a new task automatically, no polling and no glue code.

</td>
<td width="50%"><img src="https://agentrq.com/assets/feature-events.gif" alt="AgentRQ events list and configured trigger" width="320" /></td>
</tr>
<tr>
<td width="50%"><img src="https://agentrq.com/assets/feature-workflows.gif" alt="AgentRQ workflow node graph" width="320" /></td>
<td width="50%" valign="middle">

### Workflows

A Workflow is Events and workspaces arranged on a graph. Drag a workspace onto an event to subscribe it; drag an event onto a workspace to emit it on completion. No decision-tree DSL, no YAML — just the shape of your release process, visible.

</td>
</tr>
<tr>
<td width="50%" valign="middle">

### Tool Call History

The task detail view's History tab lays out a lane-grouped timeline of every tool call and message in a run — Input, Agent, and Tools. Search it, click into any entry, and see exactly what ran, what it returned, and whether it was allowed or denied.

</td>
<td width="50%"><img src="https://agentrq.com/assets/feature-tool-call-history.gif" alt="AgentRQ tool call history trajectory panel" width="320" /></td>
</tr>
<tr>
<td width="50%"><img src="https://agentrq.com/assets/feature-auto-title.gif" alt="AgentRQ auto-title generation in action" width="320" /></td>
<td width="50%" valign="middle">

### Auto-Title Generation

Write your task description, click the sparkle, and a small language model — downloaded once and cached by your browser — reads it and writes the title. No API call, no server, no data leaving your machine.

</td>
</tr>
<tr>
<td width="50%" valign="middle">

### Speech-to-Text

Click the mic on any task description or reply and dictate it instead. Transcription runs on an in-browser Whisper model — your voice is processed on-device and never uploaded anywhere.

</td>
<td width="50%"><img src="https://agentrq.com/assets/feature-speech-to-text.gif" alt="AgentRQ speech-to-text mic entry point" width="320" /></td>
</tr>
</table>

See the full list at [agentrq.com/features](https://agentrq.com/features).

## 🏛 Architecture

AgentRQ follows a decoupled service-oriented architecture:

### Backend (Go / Fiber)
- **API Server**: Fiber-based REST API for workspace and task management.
- **MCP Server**: Integrated `mcp-go` SSE server that exposes tools and resources to AI models.
- **CoreMCP (Supervisor)**: A global MCP server that allows agents to manage all workspaces, tasks, and statistics across the entire platform.
- **Data Layer**: GORM with SQLite for persistent, user-scoped storage.
- **Authentication**: Google OAuth2 integration with JWT-based session management.
- **Event Bus**: Internal pub/sub system for real-time SSE notifications.

### Frontend (Vue.js 3 / Vite)
- **Modern UI**: Tailored with Vue 3, Pinia, and Tailwind CSS.
- **Glassmorphism**: A sleek, premium design language with smooth transitions and real-time updates.
- **Reactive State**: Synchronized with the backend via SSE events.

### Desktop (Electron)
- **Same application, native shell**: the desktop app renders the *same* Vue components as the browser, so the two never diverge.
- **Native notifications**: agent activity reaches you while the window is in the background, with a dock or taskbar badge.
- **Tray, global shortcut, deep links**: `Cmd/Ctrl+Shift+N` from anywhere, and `agentrq://` URLs that open the app at a specific task.
- **Auto-updating**: checks in the background and installs on restart.

## 💻 Desktop App

AgentRQ has a desktop app for macOS, Windows and Linux. It is a client — it
connects to whichever AgentRQ server you run.

**[Download the latest release →](https://github.com/agentrq/agentrq/releases/latest)**

| Platform | Download |
|---|---|
| macOS | `.dmg` — Apple silicon and Intel |
| Windows | `.exe` installer — x64 and arm64 |
| Linux | `.AppImage` or `.deb` — x64 and arm64 |

Builds are currently **unsigned**, so macOS and Windows will warn on first
launch, and macOS cannot auto-update until signing certificates are in place.
Installation, connecting to a server, and troubleshooting are covered in the
[Desktop Guide](docs/DESKTOP.md).

To run it from source:

```bash
make install       # dependencies for the whole repo
make desktop-dev   # run the desktop app against a local server
make desktop       # build installers into desktop/release/
```

## 🛠 Getting Started

### Prerequisites
- **Go** 1.21+
- **Node.js** 18+ (with npm)
- **Google Cloud Console**: An OAuth2 Client ID and Secret.

### Configuration
1. Create a `_config/base.yaml` (or `development.yaml`) in the `backend` directory.
2. Fill in your Google OAuth2 credentials:

```yaml
auth:
  google:
    client_id: "your-google-client-id"
    client_secret: "your-google-client-secret"
```

### Running Locally
Use the provided `Makefile` to start the full stack:

```bash
# 1. Install all dependencies
make install

# 2. Start both Frontend and Backend
make dev
```

The frontend will be available at `http://localhost:5173`. For the desktop app,
run `make desktop-dev` in another terminal — see the [Desktop Guide](docs/DESKTOP.md).

### Self-Hosting (Docker)
For running the production or development stack using the pre-built Docker image, see the [Self-Hosting Setup Guide](SETUP.md).

> [!NOTE]
> **Agents / AI Assistants:** If you need to set up, configure, run, or diagnose a local self-hosted instance of AgentRQ using Docker, refer to [SETUP.md](SETUP.md) for step-by-step instructions, Docker run commands, and environment variable configurations.

## 🤖 Claude Code & AI Integration

AgentRQ is designed for seamless integration as a **Claude Channel**. This allows your AI agents to see tasks assigned to them and respond directly within your Claude session.

Each workspace has its own MCP URL and token (visible in the workspace setup modal). In production, these follow the pattern `https://WORKSPACE_ID.mcp.agentrq.com/`.

### Step 1 — `.mcp.json`

Create a `.mcp.json` file in your local project directory (the leading dot is required). Each project gets its own file so Claude instances stay isolated per workspace. Replace `YOUR_MCP_URL` below with the full URL shown in the setup modal (e.g. `https://WORKSPACE_ID.mcp.agentrq.com/?token=TOKEN`).

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

### Step 2 — `.claude/settings.local.json`

Add a `.claude/settings.local.json` file in the same project directory to pre-approve the AgentRQ tools and avoid permission prompts on every action:

```json
{
  "permissions": {
    "allow": [
      "mcp__agentrq-WORKSPACE_ID__updateTaskStatus",
      "mcp__agentrq-WORKSPACE_ID__getWorkspace",
      "mcp__agentrq-WORKSPACE_ID__reply",
      "mcp__agentrq-WORKSPACE_ID__createTask",
      "mcp__agentrq-WORKSPACE_ID__downloadAttachment",
      "mcp__agentrq-WORKSPACE_ID__getTask"
    ]
  },
  "enableAllProjectMcpServers": true,
  "enabledMcpjsonServers": ["agentrq-WORKSPACE_ID"]
}
```

### Step 3 — Start Claude

Once both files are in place, launch Claude Code from that project directory:

```bash
claude --dangerously-load-development-channels server:agentrq-WORKSPACE_ID
```

> **Tip:** The workspace ID, full MCP URL (with token), and ready-to-paste config snippets are all available in the **Setup** modal inside each AgentRQ workspace.

### Available MCP Tools
When connected, the AI agent has access to:
- `createTask`: Assign a task to the human user (supports optional `cron_schedule` for recurring tasks).
- `updateTaskStatus`: Move tasks through `notstarted`, `ongoing`, `blocked`, and `completed`.
- `reply`: Send messages back to the AgentRQ dashboard in real-time.
- `getWorkspace`: Fetch the workspace name, mission description, and task statistics.
- `getTask`: Fetch a task — with no `taskId` it dequeues the next "not started" task assigned to the agent; with a `taskId` it returns that task. Pass `includeConversation: true` to also include the chat history (cursor-based pagination).
- `downloadAttachment`: Retrieve an attachment by its ID.
- **Real-time Notifications**: Agents receive notifications via the `notifications/claude/channel` protocol whenever a human interacts with their tasks.

## 🌉 ACP Gateway (Bridge for ACP Agents)

While Claude Code has native support for `claude/notifications`, other agents like **Gemini CLI** require a bridge to receive real-time task notifications from AgentRQ. The `@agentrq/acp-gateway` bridges the [Agent Client Protocol (ACP)](https://agentclientprotocol.com) with MCP to enable this.

### Installation

```bash
npm install -g @agentrq/acp-gateway
```

### Usage

1. Ensure you have a [`.mcp.json`](#step-1--mcpjson) in your project root.
2. Run the gateway followed by your agent's ACP command:

```bash
# Using Gemini CLI
acp-gateway -- gemini --acp
```

The gateway will automatically:
- Connect to your AgentRQ workspace via the URL in `.mcp.json`.
- Spawn the agent subprocess and bridge standard I/O.
- Forward task assignments, messages, and permission requests in real-time.

## 🌌 Codex Gateway (Bridge for OpenAI Codex)

Similar to the ACP Gateway, the `@agentrq/codex-gateway` connects [OpenAI Codex](https://github.com/openai/codex) to AgentRQ workspaces by bridging the Model Context Protocol (MCP) with the Codex app-server protocol.

### Installation

```bash
npm install -g @agentrq/codex-gateway@latest
```

### Setup

**1. Configure agentrq MCP server for Codex (project-level)**

Codex reads project-level MCP server config from `.codex/config.toml`. Create this file so the Codex agent can use agentrq tools directly during task execution (replace `<WORKSPACEID>` and `<TOKEN>` with your values from the agentrq dashboard):

```bash
mkdir -p .codex
cat >> .codex/config.toml << 'EOF'

[mcp_servers.agentrq-workspace]
url = "https://<WORKSPACEID>.mcp.agentrq.com/?token=<TOKEN>"

[mcp_servers.agentrq-<ID>.tools.updateTaskStatus]
approval_mode = "approve"

[mcp_servers.agentrq-<ID>.tools.getWorkspace]
approval_mode = "approve"

[mcp_servers.agentrq-<ID>.tools.reply]
approval_mode = "approve"

[mcp_servers.agentrq-<ID>.tools.createTask]
approval_mode = "approve"

[mcp_servers.agentrq-<ID>.tools.downloadAttachment]
approval_mode = "approve"

[mcp_servers.agentrq-<ID>.tools.getTask]
approval_mode = "approve"
EOF
```

**2. Configure the gateway's agentrq connection**

Create a `.mcp.json` in your project root so `codex-gateway` can connect to the same agentrq workspace:

```json
{
  "mcpServers": {
    "agentrq": {
      "type": "http",
      "url": "https://<WORKSPACEID>.mcp.agentrq.com/mcp?token=<TOKEN>"
    }
  }
}
```

> **Note:** `.mcp.json` is used by `codex-gateway` to receive tasks. `.codex/config.toml` is used by the Codex agent itself to call agentrq tools (e.g. `reply`, `updateTaskStatus`) during execution.

### Usage

Run `codex-gateway` from your agentrq workspace root (the directory containing `.mcp.json`):

```bash
# Default: runs `codex app-server`
codex-gateway

# Custom codex command
codex-gateway -- codex app-server
```

## 👑 Supervisor (CoreMCP)

While individual workspaces provide a scoped view for specific projects, the **Supervisor (CoreMCP)** is a global MCP server that grants an agent bird's-eye view and management capabilities across your entire AgentRQ account.

The Supervisor is accessible at `https://mcp.agentrq.com/mcp`. It uses **OAuth2** for secure authentication, allowing modern AI tools (like Claude Code) to connect securely.

### Why use the Supervisor?
- **Multi-Workspace Management**: List, create, and update workspaces.
- **Global Task View**: Fetch tasks from all workspaces in a single call (`listAllTasks`).
- **Administrative Control**: Manage task assignments, status, and priorities globally.
- **Unified Statistics**: Access detailed statistics and health metrics for any workspace.

### Available Supervisor Tools
The Supervisor provides a comprehensive suite of tools for global management, requiring `workspaceId` parameters where applicable:

**Workspace Management**
- `listWorkspaces`: Overview of all active and archived workspaces.
- `createWorkspace`: Bootstrap new project environments.
- `getWorkspace`: Retrieve details of a specific workspace by ID.
- `updateWorkspace`: Modify workspace settings and metadata.
- `getWorkspaceStats`: Retrieve high-level analytics and performance data for a workspace.

**Task Management**
- `listAllTasks`: Search and filter tasks across the entire platform.
- `listTasks`: List tasks within a specific workspace.
- `createTask`: Create a new task in a specific workspace.
- `getTask`: Retrieve details of a specific task.
- `updateTaskStatus`: Change a task's status.
- `updateTaskOrder`: Reorder a task in the list.
- `updateTaskAssignee`: Change the assignee of a task.
- `updateTaskAllowAll`: Toggle `allow_all_commands` permission for a task.
- `updateScheduledTask`: Modify a scheduled/cron task.

**Communication & Files**
- `replyToTask`: Post a message to a task's chat thread.
- `respondToTask`: Submit an allow/deny verdict for a permission request.
- `getAttachment`: Retrieve data as base64 and metadata for a specific attachment.

### Connecting to Supervisor (Claude Code)
Since the Supervisor uses OAuth2, you can connect it using the following configuration in your `~/.mcp.json`:

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

When you first run Claude with this server, it will provide a link to authenticate via your browser.

## 🧩 Official Extensions

AgentRQ provides official extensions for major AI agent CLI tools to simplify setup and integration with its supervisor MCP. The sub agents MCPs should use their own workspace specific MCP server URLs.

### 🍊 Claude Code
The AgentRQ plugin for Claude Code is distributed via our official marketplace. It provides built-in skills and pre-configured MCP access.

**Installation:**
```bash
/plugin marketplace add https://github.com/agentrq/agentrq-claude-extension
/plugin install agentrq@agentrq
```

### ♊ Gemini CLI
The Gemini CLI extension allows you to manage AgentRQ workspaces and tasks directly from your terminal using Google's Gemini models.

> **Tip:** To enable real-time task notifications with Gemini, use the [ACP Gateway](#-acp-gateway-bridge-for-acp-agents).

**Installation:**
```bash
gemini extensions install https://github.com/agentrq/agentrq-gemini-extension
```

### 🐋 DeepSeek Harness
The [`@agentrq/dsh-plugin-agentrq`](plugins/deepseek-harness/README.md) bundle brings AgentRQ into [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness). It bridges the workspace's tools to the model as `mcp__agentrq__*` and holds a supervised workspace session, so tasks assigned to the agent and the human's replies arrive over the MCP channel and land in the live session — no polling, and no leaving the harness to work the queue.

**Installation:**
```bash
npx @deepseek-ai/dsh plugin --profile agentrq-<workspace> add @agentrq/dsh-plugin-agentrq
# pin this workspace's MCP URL in ~/.dsh/profiles/agentrq-<workspace>/cordis.patch.yml
npx @deepseek-ai/dsh --profile agentrq-<workspace>
```

Copy the filled-in commands and config block from **Workspace Settings → Setup → DeepSeek Harness**. A dsh profile serves one workspace and carries its own endpoint, so run one profile per workspace and switching workspaces is switching profiles. Delivery, startup catch-up, and reconnect behavior are configurable; see the [plugin README](plugins/deepseek-harness/README.md).

## 🔌 Integrations

### Slack Integration
AgentRQ supports multi-tenant Slack integration for real-time task creation, thread replies sync, and agent permission requests:
- [Slack Integration Setup & Usage Guide](integrations/slack/README.md)

## 🤝 Credits

- [AgentRQ](https://agentrq.com) — The official Agent-Human collaboration platform.
- [HasMCP](https://hasmcp.com) — Bridge the Gap Between APIs and Agents.

## 📝 License
Apache-2.0
