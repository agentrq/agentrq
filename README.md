# AgentRQ ── Agent-Human Collaboration Platform

AgentRQ is a modern, high-performance platform designed for seamless collaboration between human operators and AI agents. It leverages the **Model Context Protocol (MCP)** to allow AI models (like Claude) to interact directly with your workspace's task management system.

## 🚀 Overview

The platform allows users to create workspaces, manage tasks, and delegate work to AI agents. Agents can "see" the workspace state via MCP, receive notifications when new tasks are assigned, and reply back to the human operator—all in real-time.

## 🏛 Architecture

AgentRQ follows a decoupled service-oriented architecture:

### Backend (Go / Fiber)
- **API Server**: Fiber-based REST API for workspace and task management.
- **MCP Server**: Integrated `mcp-go` SSE server that exposes tools and resources to AI models.
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

Each workspace has its own MCP URL and token (visible in the workspace setup modal). Replace `YOUR_MCP_URL` below with the full URL shown there (e.g. `https://your-host/mcp/WORKSPACE_ID?token=TOKEN`).

### Step 1 — `.mcp.json`

Create a `.mcp.json` file in your local project directory (the leading dot is required). Each project gets its own file so Claude instances stay isolated per workspace.

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
      "mcp__agentrq-WORKSPACE_ID__getTaskMessages"
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
- `downloadAttachment`: Retrieve an attachment by its ID.
- **Real-time Notifications**: Agents receive notifications via the `notifications/claude/channel` protocol whenever a human interacts with their tasks.


## 📝 License
Apache-2.0
