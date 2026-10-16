# Native ACP (`controller/acpv1`)

> Read before changing `controller/acpv1`, `app/acp.go`, or anything an agent link touches. A later `acpv2` follows the same rules.

- **Versions never share code.** Only `app` imports a version package; `app/acp_isolation_test.go` fails otherwise. A new version copies what it needs, and a shared idea is fixed in each, on purpose.
- **A native connection is an agent link inside the workspace server**, not a second implementation: permissions, questions, models, commands, concurrency and usage all go through `WorkspaceServer`. Add behaviour there, not in a version package.
- **ACP never goes through MCP.** A choice for a native link calls its `AgentLink` method (`SetModel`, `SetEffort`, `SetConcurrency`, `DeliverVerdict`). A `notifications/claude/channel/*` string or `notifySession` reaches MCP sessions only, so a feature routed that way never reaches a link.
- **The notes are the gateway's words, byte for byte** ("this gateway" included): the composer matches them, and `frontend/test/acpNotesParity.test.js` reads `notes.go`.
- **Wait on the barrier before flushing a turn or asking permission.** Otherwise the chunks that raced the prompt's answer are lost from the reply, and the card says "Unknown Tool".
- **A dropped socket is not a closed connection.** The peer and its turns live through the 120 s grace window, and a resume replays what was missed. Only the window running out, the bridge's goodbye or the agent's exit ends them.
- **No fs, no terminal, ever claimed.** The server cannot reach the agent's machine; agents use their own tools.
- **The endpoint is on the stdlib mux and never also a Fiber route** (the mux shadows exact paths).
- **Off unless `AGENTRQ_ACP_V1_ENABLED=true`.** While it is off, the endpoint answers 404, so a local test needs the switch set.
