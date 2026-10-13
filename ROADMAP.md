# AgentRQ Roadmap

## 1. AgentRQ CLI

A command-line interface that connects to the AgentRQ backend and spawns agents for desired workspaces. Enables users to manage workspaces, trigger tasks, and attach agents directly from the terminal without the web UI.

## 2. Agent-to-Agent Collaborative Workflows ✅ Implemented

Agents can now coordinate work across workspaces, both supervisor-driven and fully decoupled:

- **Supervisor → Worker orchestration**: a supervisor agent connects to the Core MCP server (`coremcp`) over a single OAuth2 session and delegates subtasks to specialist agents in any workspace via `task` action `create(workspaceId, ...)`, monitoring progress with its `list`/`get` actions.
- **Cross-workspace events**: named event signals (`publishEvent`) let one workspace trigger tasks in others via `EventTrigger` subscriptions, with `EmitEventID` chaining for multi-stage pipelines — no supervisor required.

See [ARCHITECTURE.md](./ARCHITECTURE.md) → [Supervisor → Worker Architecture](./ARCHITECTURE.md#supervisor--worker-architecture) and [Events (Cross-Workspace Signals)](./ARCHITECTURE.md#events-cross-workspace-signals).

## 3. Skills Marketplace

A curated registry of reusable agent skills that can be discovered, installed, and shared across workspaces. Teams can publish their own skills and compose them into new workflows.

## 4. MCP Marketplace

A marketplace for Model Context Protocol (MCP) servers — enabling teams to discover, install, and configure MCP integrations directly within AgentRQ workspaces.

## 5. Workflow Marketplace

A library of pre-built end-to-end workflows described in plain English that orchestrate multiple workspaces in sequence. Each step in a workflow can be handled by a different specialized agent, enabling powerful cross-domain pipelines — for example: coding → docs → blogs → SEO indexing → social posts. Workflows can be imported, forked, and customized to fit any team's process.
