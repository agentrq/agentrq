# AgentRQ Workspace plugin for Claude Code

The **workspace** plugin: it connects to one workspace's own MCP server and works the
queue there — picking up tasks, reporting progress, and asking the human when it needs
something.

Installed from this repository's marketplace:

```bash
/plugin marketplace add https://github.com/agentrq/agentrq
/plugin install agentrq-workspace@agentrq
```

Each workspace has its own MCP URL and token, both shown in the workspace's **Setup** tab.

To supervise several workspaces from one session, see [`agentrq`](../agentrq/README.md).

## Tools


| Tool | Description |
| --- | --- |
| `createTask` | Create a task for the human user. Returns the task ID. |
| `updateTaskStatus` | Update the status of a task. Useful for moving tasks to ongoing or completed. |
| `reply` | Send a message to the current ongoing task. You can optionally include attachments. |
| `downloadAttachment` | Download the content of an attachment by its ID |
| `getWorkspace` | Returns the workspace title and mission description. |
| `getTask` | Fetch a task. With no taskId, returns the next available "not started" task assigned to the agent (dequeues the work queue). With a taskId, returns that specific task. Set `includeConversation=true` to also include the task's chat history. |
| `publishEvent` | Publish a named event so that subscriber workspaces are notified and their trigger tasks are created automatically. |
| `loadMemory` | Read what the workspace remembers. With no name it reads `memory.md`, the index of everything remembered there. |
| `saveMemory` | Write something worth remembering, so the next task starts with it. Replaces the named memory entirely. |
| `deleteMemory` | Delete one of the workspace's memories. |
| `elicit` | Ask the human a question and wait for the answer — a form, or a link for them to confirm. |

The tables here are checked against the server in CI — see
`backend/internal/handler/coremcp/plugin_docs_test.go`. A tool added to the server
without a row here fails the build.
