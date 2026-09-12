# AgentRQ CLI — Design Plan

**Status:** proposal, not yet built. **Task:** 0iF1U1qQwS1.

A command-line tool that reads `.mcp.json`, connects to the workspace it names,
and gives an agent a way to move **files** in and out of a task without the
bytes ever passing through the model's context window.

---

## 1. The problem, measured

An agent in a workspace can already create tasks, reply, and change status —
the MCP tools cover all of it, and they cover it well, because those payloads are
small text. Attachments are the exception, and they are broken in both
directions.

### Receiving a file costs more than the task

`downloadAttachment` (`backend/internal/controller/mcp/server.go:1115`) does:

```go
data, _ := ps.storage.Load(a.ID)
return &mcp.CallToolResult{
    Content: []mcp.Content{&mcp.TextContent{Text: data}}, // Return base64 data
}, nil, nil
```

`storage.Load` base64-encodes the file (`backend/internal/service/storage/storage.go:41`)
and the whole string is handed back as tool-result text — which means it lands in
the model's context and stays there for the rest of the conversation.

A 1 MB screenshot becomes ~1.4 MB of base64. Base64 tokenises badly — call it
3–4 characters per token — so that is somewhere around **350,000 to 470,000
tokens** for one image. That is not a large cost; it is larger than most context
windows. (Estimate, not a measurement: the ratio depends on the tokeniser.)

`storage.LoadRaw()` already exists one line below `Load` and returns `[]byte`.
The MCP path does not use it, and cannot: MCP tool results carry text or
base64-encoded blobs, and there is nowhere to put a file.

### Sending a file is worse

`reply` takes `attachments: [{id, filename, mimeType, data}]` where `data` is
base64 the model has to **emit, token by token**. Generating 1.4 MB of base64
without a single transcription error is not a realistic thing to ask of a
language model, and it would be ruinously slow and expensive if it were.

### And there is a 4 MB wall behind both

`backend/internal/app/app.go:740` sets Fiber's body limit:

```go
BodyLimit: 4 * 1024 * 1024, // 4 MB
```

There is no multipart endpoint anywhere in the backend — `grep FormFile` returns
nothing. Every attachment travels as base64 inside a JSON body, and base64 costs
~33%, so the real ceiling is a **~3 MB file**, shared with whatever else is in
the payload. Nothing validates the size first, so exceeding it produces a generic
body-limit rejection rather than a message anyone can act on.

**This is the finding that shapes the plan: a CLI that still posts base64 JSON
inherits the 3 MB ceiling exactly.** Making attachments genuinely cheap needs a
backend change. The client is necessary and not sufficient.

---

## 2. What is already right

Worth stating plainly, because it means less needs building than it first looks.

**The REST download endpoint is already correct.**
`GET /api/v1/workspaces/:id/tasks/:taskID/attachments/:attachmentID`
(`backend/internal/handler/api/task.go:529`) calls `storage.LoadRaw` and streams
raw bytes with the right `Content-Type` and a correctly-encoded
`Content-Disposition`. It needs no change. The CLI's entire download path is:
call this, write the body to a file, print the path.

**The notification format already points the right way.** When a human replies
with a file, `formatAttachments` (`backend/internal/handler/api/task.go:795`)
sends the agent a summary — id, name, type — and *not* the bytes. The agent is
already told "here is an attachment ID"; today the only way to act on that is the
expensive tool. The CLI slots into a gap the design already left open.

**Ownership is already enforced.** `GetAttachment`
(`backend/internal/controller/crud/task.go:754`) loads the task through
`repository.GetTask(ctx, workspaceID, taskID, uid)`, which filters by user, and
returns `ErrNotFound` if that fails. Attachment IDs are unguessable monoflakes
and are never trusted from callers — `saveAttachments`
(`backend/internal/controller/crud/task.go:801`) overwrites any caller-supplied
ID with a server-minted one, with the comment explaining exactly why. The CLI
inherits all of this for free and must not weaken it.

---

## 3. Shape of the thing

```
.mcp.json ──read──► arq ──HTTPS──► AgentRQ backend
                     │              /api/v1/…  (REST — carries bytes)
                     └──────────────► local disk (files land here)
```

The CLI reads its connection from `.mcp.json`, as the task asked, and talks to
the **REST API** rather than to MCP. That is the point: MCP is a protocol for
moving text into a model, and attachments are the one workspace operation that
must not do that. `.mcp.json` is the credential and workspace source; REST is the
transport.

### Why not just extend the MCP tools

Considered and rejected. MCP could return a *path* instead of base64 — but the
MCP server is frequently not on the same machine as the agent (the production
URL is `https://<workspace>.mcp.agentrq.com`), so a path it returns means nothing
locally. Only something running next to the agent can put a file on the agent's
disk.

---

## 4. Config discovery

Resolution order, first hit wins:

1. `--config <path>` / `--workspace <id>` flags
2. `AGENTRQ_MCP_URL` environment variable (matches the DeepSeek Harness plugin's
   existing `AGENTRQ_WORKSPACE_MCP_URL` convention)
3. `.mcp.json` in the working directory, then each parent up to the git root
4. `~/.agentrq/config.json`

Within `.mcp.json`, an AgentRQ server is one whose key matches `agentrq-*` **or**
whose URL host matches `*.mcp.agentrq.com` **or** whose path matches `/mcp/<id>`.
The repo's own file shows why the key alone is not enough to go on — it also
holds an unrelated `chrome-devtools` entry:

```json
{
  "mcpServers": {
    "agentrq-0ZzhYQG2qtl": { "type": "http", "url": "https://….mcp.agentrq.com?token=…" },
    "chrome-devtools":     { "command": "npx", "args": ["-y", "chrome-devtools-mcp@latest"] }
  }
}
```

If more than one AgentRQ server matches, the CLI **errors and lists them** rather
than picking. Silently guessing a workspace is how a file ends up on the wrong
task.

The workspace ID comes from the server key suffix, the URL, or the token's
`aud[0]` — used only as a *hint*. The CLI cannot verify a JWT signature and must
never behave as though it can; the server remains the only authority on what the
token permits.

### The one genuinely unresolved piece: the API base URL

Two deployment shapes, and they derive differently:

| MCP URL | API base |
|---|---|
| `https://<ws>.mcp.agentrq.com/?token=…` | **unverified** — see below |
| `http://localhost:3000/mcp/<ws>?token=…` | `http://localhost:3000/api/v1` |

Self-hosted is unambiguous: same origin, swap the path. Production is not. The
Fiber app serves `/api/v1` regardless of `Host`, so *if* the per-workspace
subdomain routes to the same backend process, the API is reachable there — but
that routing is infrastructure, and there is no proxy config in this repo to
confirm it from. I could not settle it empirically either; probing production
with the workspace token was blocked by this environment's command classifier,
correctly, since it means sending a live credential to a remote host.

**This must be verified before implementation starts.** One `curl` against
`https://<ws>.mcp.agentrq.com/api/v1/workspaces` with a valid cookie answers it.

Three ways to handle the outcome, in order of preference:

1. **If the subdomain does route to the backend** — derive same-origin. Nothing
   more to build.
2. **Otherwise, add a discovery document.** A tiny unauthenticated
   `GET /.well-known/agentrq` returning `{"apiBaseUrl": "...", "version": "..."}`,
   served from the MCP host. Costs one handler and removes the guesswork
   permanently, including for future clients.
3. **Always allow the override.** `AGENTRQ_API_URL` / `--api-url`, regardless of
   which of the above lands, plus an `arq doctor` command that prints what it
   resolved and why. Config discovery that fails silently is worse than config
   that must be stated.

---

## 5. Authentication — and an asymmetry to fix first

The MCP handler checks that the token's audience matches the workspace being
addressed (`backend/internal/handler/mcp/mcp.go:387-416`). The REST middleware
does not:

```go
// backend/internal/handler/api/api.go:322
func (h *handler) authMiddleware() fiber.Handler {
    return func(c *fiber.Ctx) error {
        tokenStr := c.Cookies("at")
        …
        claims, err := h.tokenSvc.ValidateToken(tokenStr)
```

`ValidateToken` (`backend/internal/service/auth/jwt.go:274`) verifies the
signature and expiry and **never looks at `aud`**. Both token kinds are signed
with the same secret, so the 365-day workspace token from `.mcp.json`
(`CreateMCPToken`, `jwt.go:142`) is accepted as a full user session cookie —
across every workspace that user owns.

That means the CLI *could* ship tomorrow by sending `Cookie: at=<mcp token>`, and
this plan deliberately does not do that. It would silently promote a
workspace-scoped credential to an account-wide one, and bake that promotion into
a tool we then ask people to install.

**Proposed instead:** accept `Authorization: Bearer <token>` on the API group,
validating that `aud` contains both the workspace ID being addressed and
`"access"`. The cookie path stays exactly as it is for browser and desktop
clients. The CLI's reach then matches the token it was handed.

*Not currently a live vulnerability:* `.mcp.json` is gitignored
(`.gitignore:5`), the token is the user's own, and nothing exposes it. This is
about not building on the weakness — and the audience check is worth adding on
its own merits regardless of whether the CLI ships.

---

## 6. Backend changes

Ordered by whether the CLI can exist without them.

### 6.1 Upload: required

A multipart endpoint, because the whole point is not to base64 anything.

```
POST /api/v1/workspaces/:id/tasks/:taskID/reply     (multipart/form-data)
  text: string
  file: one or more file parts
→ 200 { task: {...} }
```

**Attach-on-send rather than upload-then-reference.** A detached
`POST /attachments` returning an ID to quote later reads cleaner, but it breaks
the invariant `saveAttachments` exists to hold — that attachment IDs are always
server-minted and never accepted from a caller. Honouring a caller-supplied ID
means a new ownership table, a claim/expiry model for orphans, and a check on
every reference. Attaching at send time needs none of that. If detached upload is
wanted later it can be added deliberately, with that table designed on purpose
rather than as a side effect.

**The body limit is the hard part.** Fiber's `BodyLimit` is app-wide; there is no
per-route override. Three options:

| Option | Trade-off |
|---|---|
| Raise the global limit | Widens the DoS surface of every route to buy one. No. |
| `StreamRequestBody` + manual multipart | Keeps it in Fiber, but the streaming path has to be hand-rolled and the limit is still global. |
| **Mount the upload on the stdlib mux** | `r.MultipartReader()` streams parts straight to `storage`, never buffering the file in RAM, with an explicit per-file cap. |

The third is recommended. The mux already exists in `app.go` and already carries
`/api/v1/…` routes (`app.go:885`). **This contradicts `AGENTS.md`**, which says
that mux is only for SSE and pub-stats routes — so the note must be amended in
the same PR, with the reason: streaming upload is the second thing that cannot go
through Fiber, and the rule should say what it is protecting rather than list
exceptions.

### 6.2 An explicit size cap: required

There is none today. Add one — 25 MB per file is a reasonable start — enforced
during the stream so an oversized upload is cut off rather than absorbed, and
returned as `413` with a JSON body naming the limit. The CLI turns that into one
readable line. Make it configurable; self-hosted deployments have different
appetites than the hosted one.

### 6.3 Bearer auth with an audience check: required

Section 5.

### 6.4 Teaching `downloadAttachment` to stop: recommended, decide explicitly

Once the CLI exists, MCP `downloadAttachment` returning a 400 KB base64 string is
a trap left armed. The cheapest fix that breaks nothing: above a threshold
(~64 KB), return a short text saying the file is too large to inline and naming
the CLI command that fetches it. Small attachments keep working exactly as they
do now.

This is a **behaviour change to an existing tool** and agents in the field depend
on it, so it is called out as a decision rather than folded in. It should ship
after the CLI is real, never before.

---

## 7. Command surface

Binary name: **`arq`**.

Not `agentrq` — the desktop installer already puts a binary at
`~/.local/bin/agentrq` on Linux (`docs/DESKTOP.md:26`), and a CLI of the same
name would collide on exactly the machines most likely to have both. `arq` is
also three characters, which matters for something an agent types on every file
operation. (Arq Backup ships on macOS under the same name but installs no `arq`
executable; low risk, worth a check before publishing.)

```
arq workspace show                          # name, mission, task counts
arq task list [--status …] [--limit N]
arq task get <taskId> [--messages]
arq task create --title T --body B [--file F …] [--cron C] [--event E]
arq task status <taskId> <status>
arq reply <taskId> --text T [--file F …]

arq attachment list <taskId>                # id, filename, mimeType, size
arq attachment get <attachmentId> --task <taskId> [-o DIR|-]
arq event publish <name> --payload P [--faq q=a …]

arq doctor                                  # what config resolved to, and whether it works
```

Conventions that matter specifically because the caller is an agent:

- **`--json` on every command.** Machine-parseable and small.
- **Human output is one line per result.** The default output is also going into
  a context window; verbosity is a real cost, not a style preference.
- **`attachment get` prints a path, never bytes.** Writing to stdout requires an
  explicit `-o -`. The single most important rule here: the tool exists to keep
  bytes out of the conversation, and a tool that dumps a file on stdout when
  someone forgets a flag has defeated itself.
- **Distinct exit codes** — `0` ok, `1` usage, `2` auth, `3` not found,
  `4` too large, `5` network — so an agent can branch without parsing prose.
- **Errors go to stderr as one line**, with `--json` putting them on stdout as an
  object instead.

---

## 8. Packaging

**Recommendation: Node, published as `@agentrq/cli`, exposing `arq`.**

Consistency is the argument. `@agentrq/acp-gateway` and `@agentrq/codex-gateway`
are already npm packages that already read `.mcp.json`, so the discovery logic
and the user's mental model both carry over, and `npx @agentrq/cli` installs
nothing. Three AgentRQ command-line tools installed three different ways is a
worse outcome than a slightly less elegant implementation. The attachment work is
I/O, not computation, so Go's advantages do not pay for the new distribution
channel.

**If Go is chosen instead**, the plan changes in these places only: §4 discovery
reuses `monoflake` directly rather than reimplementing base62; §7 ships as a
static binary per platform through GitHub releases and the existing
`install.sh`; and the package name in §8 becomes a release artifact. Everything
in §5 and §6 — the audience check, the multipart endpoint, the size cap — is
backend work and is unaffected by the choice.

---

## 9. Phasing

Each phase is independently shippable and useful on its own.

**Phase 1 — download only.** `arq doctor`, `arq attachment list`,
`arq attachment get`. No backend change at all: the REST endpoint already
streams bytes. Requires only that §4's base-URL question is answered. *This phase
alone removes the expensive half of the problem* and is the fastest thing here to
get real feedback on.

**Phase 2 — the audience check.** §5, backend only, no CLI change. Lands before
Phase 3 so upload is never built against the cookie path.

**Phase 3 — upload.** §6.1 and §6.2 (multipart endpoint, streaming, size cap),
then `arq reply --file` and `arq task create --file`. This is the phase with real
design risk in it, and it benefits from Phase 1 having settled the config and
auth plumbing first.

**Phase 4 — the rest of the surface.** `task list/get/create/status`,
`workspace show`, `event publish`. Straightforward REST wrapping, valuable for
shell use, and not the reason to build any of this.

**Phase 5 — `downloadAttachment` threshold.** §6.4, only once the CLI is
available to point people at.

---

## 10. Testing

- **Backend**: table tests beside the existing ones in
  `backend/internal/controller/crud/task_test.go`, which already mocks
  `storage.LoadRaw` and is the natural home for upload coverage. New paths worth
  covering explicitly: oversized upload cut off mid-stream, wrong-workspace
  token rejected by the audience check, multipart with no file part, and a
  filename needing header escaping (the `contentDisposition` helper at
  `task.go:583` exists because of a real crash — see its comment).
- **CLI**: config discovery against fixture `.mcp.json` files (missing, multiple
  AgentRQ servers, unrelated servers alongside, both URL shapes); transport
  against a stub HTTP server. No network in tests.
- **End to end**: one script that uploads a file, downloads it back, and compares
  checksums. It is the only test that proves the actual claim.
- Coverage target for new lines is 100%, per the repo's PR convention.

---

## 11. Non-goals

Stated so the scope does not drift into them:

- **Not an MCP replacement.** `createTask`, `reply`, `updateTaskStatus` over MCP
  stay exactly as they are. The CLI is additive.
- **No interactive TUI.** The caller is an agent; a prompt is a hang.
- **No local state or cache.** `.mcp.json` and flags are the whole configuration.
  Nothing to invalidate, nothing to go stale.
- **No credential management.** The CLI reads a token someone else provisioned;
  it does not log in, refresh, or store one.
- **Not a supervisor client.** CoreMCP's cross-workspace tools
  (`listWorkspaces`, `listAllTasks`) are a different surface with different
  auth. Out of scope.

---

## 12. Decisions needed before implementation

1. **Does `https://<ws>.mcp.agentrq.com/api/v1/…` reach the backend?** (§4) —
   blocks Phase 1. One curl.
2. **Node or Go?** (§8) — recommendation is Node/`@agentrq/cli`.
3. **`arq` as the binary name?** (§7) — confirms the collision is avoided.
4. **Per-file size cap** (§6.2) — 25 MB proposed.
5. **Mux exception for streaming upload, and the `AGENTS.md` amendment** (§6.1).
6. **Threshold behaviour for `downloadAttachment`** (§6.4) — and whether that
   change is acceptable at all.

---

## Appendix: source references

| Claim | Location |
|---|---|
| 4 MB body limit | `backend/internal/app/app.go:740` |
| No multipart anywhere | `grep -rn "FormFile\|multipart" backend --include='*.go'` → no hits |
| MCP returns base64 as tool text | `backend/internal/controller/mcp/server.go:1115` |
| `LoadRaw` exists and is unused by MCP | `backend/internal/service/storage/storage.go:53` |
| REST streams raw bytes already | `backend/internal/handler/api/task.go:529` |
| Attachment route path | `backend/internal/handler/api/task.go:37` |
| Ownership check on download | `backend/internal/controller/crud/task.go:754` |
| Server-minted attachment IDs | `backend/internal/controller/crud/task.go:801` |
| REST auth ignores `aud` | `backend/internal/handler/api/api.go:322`, `backend/internal/service/auth/jwt.go:274` |
| MCP auth checks `aud` | `backend/internal/handler/mcp/mcp.go:387-416` |
| Workspace token TTL is 365 days | `backend/internal/service/auth/jwt.go:142` |
| `.mcp.json` is gitignored | `.gitignore:5` |
| Desktop installs `agentrq` on Linux | `docs/DESKTOP.md:26` |
| Agent is told attachment metadata, not bytes | `backend/internal/handler/api/task.go:795` |

**Note on `backend/openapi.yaml`:** it is stale and should not be used to
generate a client. It documents the attachment route as
`/workspaces/{id}/attachments/{attachmentID}`, omitting the `/tasks/{taskID}`
segment the real route has, and its schemas use `snake_case` (`created_at`)
where the live API and `AGENTS.md` both require `camelCase` (`createdAt`,
`backend/internal/data/view/api/view.go:13`). Worth a separate fix.
