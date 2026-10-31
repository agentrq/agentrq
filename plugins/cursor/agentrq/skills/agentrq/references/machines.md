# The machines tool

A machine is a computer running agentrqd; it is where a workspace's agent runs.

- `createEnrolmentCode` — Mint a one-time code for enrolling a new machine with agentrqd. Shown once and expires shortly — hand it to the human right away
- `list` — List the account's machines: enabled, online, agentrqd version, how many agents each runs, and `agents`: each one's workspace and state, as in `workspace.get`
- `launchAgent` — Start a workspace's agent (`claude-code` or `acp-gateway`, with an optional `model` and, for `claude-code`, `effort` and `autocompact` — 100k to 900k, default 400k; for `acp-gateway`, `acpVersion`, the ACP version it speaks, `"1"` when left out) on a machine from `machines.list`. It connects a few seconds later and picks up its tasks. Refused while the workspace already has one, has no working directory, or the machine is off, offline or too old
- `stopAgent` — Stop the agent a workspace is running on a machine, ending what it is in the middle of

## Giving idle workspaces an agent

A task assigned to `agent` waits until the workspace has one running. `machines.list` shows which machines are online; `machines.launchAgent` starts one there, and `machines.stopAgent` stops it. Ask the human before stopping an agent that is mid-task.
