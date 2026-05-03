# AgentRQ ── Agent-Human Collaboration Platform

<p align="center">
  <video src="demos/demo.mp4" width="480" controls="controls"></video>
  <br />
  <a href="https://www.youtube.com/watch?v=GBAoSpuCzrU">Watch on YouTube in HD</a>
</p>

AgentRQ is a modern, high-performance platform designed for seamless collaboration between human operators and AI agents. It leverages the **Model Context Protocol (MCP)** to allow AI models (like Claude) to interact directly with your workspace's task management system.

## 🚀 Overview

The platform allows users to create workspaces, manage tasks, and delegate work to AI agents. Agents can "see" the workspace state via MCP, receive notifications when new tasks are assigned, and reply back to the human operator—all in real-time.

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

The frontend will be available at `http://localhost:5173`.

## 🤖 Claude Code & AI Integration

AgentRQ is designed for seamless integration as a **Claude Channel**. This allows your AI agents to see tasks assigned to them and respond directly within your Claude session.

Each workspace has its own MCP URL and token (visible in the workspace setup modal). In production, these follow the pattern `https://WORKSPACE_ID.mcp.agentrq.com/mcp`.

### Step 1 — `.mcp.json`

Create a `.mcp.json` file in your local project directory (the leading dot is required). Each project gets its own file so Claude instances stay isolated per workspace. Replace `YOUR_MCP_URL` below with the full URL shown in the setup modal (e.g. `https://WORKSPACE_ID.mcp.agentrq.com/mcp?token=TOKEN`).

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
      "mcp__agentrq-WORKSPACE_ID__getTaskMessages",
      "mcp__agentrq-WORKSPACE_ID__getNextTask"
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
- `getTaskMessages`: Read the chat history of a task with cursor-based pagination.
- `getNextTask`: Efficiently retrieve the next "not started" task assigned to the agent.
- `downloadAttachment`: Retrieve an attachment by its ID.
- **Real-time Notifications**: Agents receive notifications via the `notifications/claude/channel` protocol whenever a human interacts with their tasks.

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

**Installation:**
```bash
gemini extensions install https://github.com/agentrq/agentrq-gemini-extension
```

## 🤝 Credits

- [AgentRQ](https://agentrq.com) — The official Agent-Human collaboration platform.
- [HasMCP](https://hasmcp.com) — Bridge the Gap Between APIs and Agents.

## 📝 License
Apache-2.0
