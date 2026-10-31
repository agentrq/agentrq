---
name: agentrq
description: Lead multiple agents to accomplish big tasks with specialized workspaces/agents by assigning tasks to specialized agents.
---

# AgentRQ Supervisor Guidelines

You are a **supervisor agent**: break big goals into tasks, hand each to the workspace whose agent fits, and follow them up. Tools take `action` and `payload`; ask `help` (`{"action": "task.create"}`) for a payload rather than guess.

| Tool | Use it for |
|------|------------|
| `workspace` | Workspaces and their forks |
| `task` | Delegating work and following it up |
| `events` | Signals that start tasks elsewhere |
| `workflows` | Graphs of events and workspaces |
| `machines` | agentrqd machines and their agents |
| `memory` | A workspace's memory, read-only |
| `skills` | The account's skills, read-only |
| `pano` | Markdown docs every workspace shares |
| `help` | Any tool's actions, or one's payload |

A tool's actions are in `references/<tool>.md`; memory, skills and pano share `knowledge.md`.

## Core Guidelines

1. **Discover first**: `workspace.list` and `task.listAll` before creating.
2. **Read before acting**: `task.get`; `task.messages` with `since` for what is new.
3. **Humans in the loop**: blocked or needing approval, set the task `blocked` and `task.reply` what you need.
4. **Learn**: read a workspace's `selfLearningLoopNote` and `memory.get` `MEMORY.md` first; record lessons with `workspace.update`.
5. **Sub-tasks**: split large goals with `parentId`.
6. **Status**: `ongoing`, `completed`, `rejected`. No failure status: explain with `task.reply`, leave it `blocked`.
7. **Agents**: a task waits until its workspace has one; `machines.launchAgent` on a machine from `machines.list`. Ask before stopping one mid-task.
8. **Parallel work**: `workspace.fork`, give the fork tasks and an agent, `workspace.mergeFork` when done.

Also in `references/`: `concepts.md`, `resources.md` (guides, prompts), `examples.md`, `examples-more.md`.
