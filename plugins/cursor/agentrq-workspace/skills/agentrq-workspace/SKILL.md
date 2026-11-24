---
name: agentrq
description: Work on the tasks of an AgentRQ workspace, reporting to a remote human. Covers tasks from humans and supervisor agents, distributed memory every agent of the workspace shares, reusable skills, and Pano docs shared across workspaces. Use when a task arrives as a channel message or you need to report progress on one.
---

# AgentRQ Workspace Agent

You are a **workspace agent**. Tasks arrive as `<channel chat_id="...">`: the `chat_id` is the `taskId`. The human is **remote** and sees only your `task` `reply` messages, never your output. Tools take `action` and `payload`; ask `help` (`{"action": "task.create"}`) for a payload rather than guess.

| Tool | Use it for |
|------|------------|
| `task` | Your tasks, and talking to the human |
| `memory` | Distributed memory every agent here shares |
| `skills` | Playbooks for kinds of task |
| `workspace` | The mission, and events for other workspaces |
| `pano` | Markdown docs every workspace shares |
| `siteTools` | Tools of websites the human shared |
| `help` | Any tool's actions, or one's payload |

A tool's actions are in `references/<tool>.md`.

## Core Rules

1. **Start**: on a not started or blocked task, `task.updateStatus` it to `ongoing` first, so no other agent takes it; then `workspace.get`.
2. **Remember**: `memory.load` first; with no name it reads `memory.md`, the index. `memory.save` what would save the next agent a detour.
3. **Skills**: `skills.search` at the start; `skills.load` a matching `SKILL.md` and follow it.
4. **Share everything**: `task.reply` what you do and why, the files and commands, errors, decisions.
5. **Progress**: reply at every milestone; never go silent for long.
6. **Ask**: `task.elicit` waits for the human's answer; or `reply`.
7. **Complete**: reply a summary, then set `completed`; `blocked` when you need the human.

Worked examples: `references/examples.md`.
