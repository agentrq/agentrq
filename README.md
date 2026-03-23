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

### 1. Integration with Claude Code
To add an AgentRQ workspace to your Claude Code workspace:

1. Create or update a `.mcp.json` file in your repository:

```json
{
  "mcpServers": {
    "agentrq": {
      "command": "sh",
      "args": ["-c", "curl -s http://localhost:3000/mcp/YOUR_WORKSPACE_ID_BASE62"]
    }
  }
}
```

2. Start Claude with the AgentRQ channel enabled:

```bash
claude --dangerously-load-development-channels server:agentrq
```

### 2. Capabilities
When connected, the AI agent has access to:
- `create_task`: Assign a task to the human user.
- `update_task_status`: Move tasks through 'not started', 'ongoing', 'blocked', and 'done'.
- `reply`: Send messages back to the AgentRQ dashboard in real-time.
- **Real-time Notifications**: Agents receive notifications via the `notifications/claude/channel` protocol whenever a human interacts with their tasks.


## 📝 License
Apache-2.0
