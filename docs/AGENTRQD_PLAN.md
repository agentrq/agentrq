# agentrqd — implementation plan

A daemon that runs on a user's own machine, enrols itself against an AgentRQ
account, and spawns and supervises agent processes (`claude-code`,
`acp-gateway`) inside PTYs it owns — so the control panel can start an agent,
kill it, and type into its terminal, including control characters like `Esc`.

Status: **plan only.** Nothing here is built. **All seven open questions were
answered on 2026-09-15** — §18 records each decision and what it changed.

> **Revision note.** An earlier draft put the control plane on the supervisor
> MCP server and used a WebSocket only for terminal bytes. Per direction, this
> version uses **WebSockets for everything** and does not involve `coremcp` at
> all. §3 records what that gains and what it costs, because it is not free in
> both directions.

---

## 1. Scope

**In scope**

- One static binary, `agentrqd`, for macOS, Linux and Windows on amd64/arm64.
- Enrolment of a machine against an AgentRQ account.
- **Spawning exactly two kinds of process — `claude-code` and `acp-gateway` —
  and killing active ones.** Nothing else is spawnable, by design.
- Streaming PTY output to the UI and delivering keystrokes — arbitrary bytes,
  `Esc` included — back down.
- Machine and session lists in the existing web/desktop UI.
- **Launching an agent for a workspace** that has none, in that workspace's
  folder, with its MCP configuration written by the daemon (§11).
- **Per-machine system information**: memory, CPU and free disk per mount.
- **Approved self-update**: the daemon reports an available version, a person
  approves, it kills its sessions, updates, and re-spawns them (§7).

**Not in scope (first release)**

- Sessions surviving a daemon restart (§18 — a real decision, not an oversight).
- Multi-*user* machines — one daemon serves one OS user, though it may serve
  several accounts through profiles (§4).
- True session continuity across a daemon restart: sessions die with the daemon
  and are restored by intent (§7, §18).
- File transfer, port forwarding, or a general remote shell **as a feature**.
  See §2: a PTY you can type into *is* a shell for risk purposes regardless.

---

## 2. Trust model — read before the architecture

This is remote code execution on the user's machine, by design. That is the
product, not a flaw to be engineered away. What follows makes the blast radius
**explicit, bounded and revocable** rather than pretending it is small.

**Plain statement, which belongs in the docs and the enrolment flow verbatim:**
enrolling a machine lets anyone who can authenticate as that AgentRQ account run
commands on it as the user who started the daemon. Compromise of the account is
compromise of every enrolled machine.

1. **Enrolment is a deliberate local act.** A human runs `agentrqd enroll` at a
   terminal on that machine. The backend can never cause a machine to enrol.
2. **The daemon never runs as root/Administrator** and refuses to start if it
   is. An agent needing elevation is the user's problem to solve deliberately.
3. **Only two kinds are spawnable.** The start API takes a *kind* —
   `claude-code` or `acp-gateway` — plus validated parameters, never a command
   line. Kind → argv resolution lives in the daemon's own config. A backend that
   has been taken over cannot ask for `/bin/sh`.
   - This genuinely defends against "backend says run X". It is **no defence at
     all** against typing `sh` into a running `claude-code` session, which can
     run commands by design. Both halves must be said; claiming layered defence
     that is not there is worse than claiming none.
4. **A profile is a credential boundary, not an execution boundary.** Several
   accounts can drive one machine through profiles (§4), and it would be easy to
   read that as a sandbox because the desktop app's profiles isolate cookies.
   They do not isolate processes. Every profile's sessions run as **the same OS
   user on the same filesystem** — account A's agent can read account B's agent's
   files and see its processes. If two accounts need real isolation, that is two
   machines, or two OS users running two daemons, and nothing in this design
   substitutes for it.
5. **Disable from either end** — local `agentrqd disable`, and a per-machine
   toggle in the UI — each effective without the other end's cooperation.
6. **Audited**: every start, kill and attach is a durable record with actor,
   machine, session and timestamp. Keystrokes are **not** recorded — they carry
   secrets — but the fact of an attach is.
7. **TLS verified by default; plain HTTP is an explicit, loud choice.** Decided:
   the daemon requires and verifies TLS whenever the server is anything but
   loopback — no flag needed, and that is the case that matters. `127.0.0.1`,
   `::1` and `localhost` are allowed over plain HTTP automatically: there is no
   network there to intercept. Anything else needs `--insecure` passed
   deliberately, and when it is, the daemon **says so on every start and in
   `agentrqd status`** — not once, in a log nobody reads. A security decision
   that becomes invisible after first boot has stopped being a decision.
8. **Local visibility**: while a session is live, `agentrqd status` says so and
   each attach writes a log line. Someone at the keyboard must be able to find
   out that something is driving their machine.

---

## 3. Architecture: one WebSocket, multiplexed

Three participants. The daemon is behind NAT with no inbound connectivity, so
**every connection is outbound from the daemon** and the backend is a relay.
The browser never reaches the daemon directly.

```
  ┌──────────┐  cookie; WS per session  ┌─────────┐   ONE persistent WebSocket   ┌──────────┐
  │ Browser  │ ───────────────────────► │ Backend │ ◄─────────────────────────── │ agentrqd │
  │   (UI)   │ ◄─────────────────────── │ (relay) │  control + terminal, muxed   │  + PTYs  │
  └──────────┘                          └─────────┘ ◄──────────────────────────► └──────────┘
```

The daemon holds **one** long-lived WebSocket to the backend. Everything rides
it, distinguished by a session id in each frame: heartbeats, start, kill, status
changes, and the PTY byte streams of every attached session.

**What this buys**

- **One connection, one auth, one reconnect path.** With MCP for control and a
  socket for data there were two liveness models, two failure modes and two
  things that could be half-up. A machine is now either connected or not.
- **Symmetric push.** The backend can tell the daemon "kill session 7" on the
  same channel the daemon is already streaming on, with no polling and no
  request/response mismatch.
- **Enrolment stops needing a browser** (§4), which matters because the obvious
  place to run this is a headless build box.
- **No new MCP tool surface** to keep in step across the several places the tool
  list is duplicated.

**What it costs, honestly**

- **We give up the existing credential path.** `coremcp` already has OAuth2,
  dynamic client registration and an authorisation UX the desktop app walks
  today. We now own a machine-token scheme instead (§4) — new code on a security
  boundary, which is exactly where new code is least welcome.
- **Agents lose programmatic access.** With MCP tools, an agent could have
  listed machines or started a session. Over a bespoke WebSocket it cannot. If
  that is wanted later it is a thin REST or MCP layer over the same state, not a
  redesign — worth knowing it is deferred rather than lost.
- **We must build what MCP gave free**: framing, request/response correlation,
  versioning and backward compatibility.

**Why the terminal could never have been MCP.** Keystrokes need tens of
milliseconds; PTY output is a continuous byte stream needing backpressure. This
repo already draws that line — SSE sits on the stdlib `mux` rather than Fiber
because streaming did not fit the request/response router.

---

## 4. Enrolment, profiles and identity

Without OAuth2, enrolment is a **one-time code**, which is a better fit for a
daemon anyway — no browser required on the machine being enrolled.

```
UI:      Machines → "Add machine" → shows  ABCD-1234   (short TTL, single use)
Machine: $ agentrqd enroll --server https://app.agentrq.com --code ABCD-1234
         → POST /api/v1/machines/enroll  {code, name, hostname, os, arch, version}
         → { machineId, machineToken }
         → token to OS keychain (Keychain / Credential Manager / libsecret),
           else 0600 file with a loud warning
```

- **The enrolment code is short-lived, single-use, and account-scoped.** It is
  the only moment a secret crosses a human's hands; make its TTL minutes.
- **The machine token is opaque, long-lived and revocable**, stored hashed
  server-side. It authorises exactly one machine and nothing else — it is not an
  account token and must not be usable against the REST API.
- **Revocation is immediate**: deleting or disabling a machine invalidates the
  token; the daemon's socket is closed server-side and it stops supervising.
- **Rotation**: the daemon may exchange its token on reconnect; plan the field
  in from day one, since retrofitting rotation is painful.

### Profiles: one machine, several accounts

The desktop app already has this concept and the daemon should use the same one
rather than invent a parallel idea. From `desktop/src/main/profiles.js`:

> A profile is an account. […] Each profile carries its own server URL […]
> because an account exists on one server: "which account" and "which server"
> are the same question.

That holds here unchanged. A daemon profile is `(server URL, machine token,
machine id)` plus its own sessions.

- **One daemon process, several profiles, one socket each.** Not one process per
  profile: a single service is easier to install, supervise and update, and the
  machine metrics of §6 are a property of the *box*, not of an account.
- **Machine identity is per profile.** The machine token is account-scoped, so
  the same physical box enrols separately in each account and appears as a
  separate machine in each. That is correct rather than a limitation — sharing
  one machine id across accounts would tell account B that account A's machine
  exists.
- **Ids stay narrow** — the same `^[a-z0-9][a-z0-9-]{0,63}$` the desktop uses,
  for the same reason: they become directory names.
- **Migration is the same shape too.** A daemon enrolled before profiles existed
  becomes a single profile, exactly as `migrateProfiles` does for a pre-profiles
  desktop config.
- **Metrics go to every profile.** The same numbers, because it is the same
  hardware. This leaks nothing new: each account already knows this machine
  exists, having enrolled it.

```
agentrqd enroll --profile work --server https://app.agentrq.com --code ABCD-1234
agentrqd enroll --profile personal --server https://my.host --code WXYZ-9876
agentrqd status                      # every profile, its machine, its sessions
agentrqd disable --profile work      # one account, without touching the other
```

Config mirrors the desktop's shape — one file with a `profiles` array — and each
profile's token is a separate keychain entry, never one blob holding both.


### Headers on every daemon request

Every request and the WebSocket upgrade carry:

```
Authorization:         Bearer <machineToken>     ← the only thing that establishes identity
X-AgentRQ-Machine-Id:  <machineId>               ← routing + correlation
X-AgentRQ-User-Id:     <userId>                  ← routing + correlation
X-AgentRQ-Version:     <agentrqd version>
```

**The mapping to a machine does not come from the headers, and must not.** The
machine token is stored hashed against the `Machine` row, and that row carries
`UserID` — so `token → machine → user` is already a lookup the backend can do,
and it is the only version of that lookup an attacker cannot influence. A header
is a value the client chose. Any code path that reads `X-AgentRQ-User-Id` to
decide *who someone is* turns "set a header" into account takeover, and it would
be the single worst bug in this design.

So the headers earn their place a different way — **state them, then verify them**:

1. The backend resolves machine and user **from the token**.
2. It compares that answer to the headers.
3. A mismatch is **rejected and logged as a security event**. It is not a
   recoverable condition and there is no reason for it to occur in normal
   operation, so it is worth alerting on rather than shrugging at.

That turns a redundant header into a detection signal, which is the only honest
reason to send identity a second time.

What the headers are genuinely *for*:

- **Routing.** If the backend runs more than one instance (§18), a load balancer
  needs a stable key to pin a daemon's socket and the matching browser attach to
  the same instance. `X-AgentRQ-Machine-Id` is the better key than user id —
  the socket is per-machine, and one user may have many machines.
- **Correlation.** Logs, traces and audit records join on these without parsing
  a token.
- **Version skew.** `X-AgentRQ-Version` lets the backend answer an old daemon
  differently, and is how we find out what is actually deployed in the field.

---

## 5. Process supervision

A **session** is one agent process and the PTY it runs in.

| Kind | Launched as | Baseline |
|---|---|---|
| `claude-code` | `claude --name <n> server:<workspace>` | the `remote-claude` Make target |
| `acp-gateway` | `npx @agentrq/acp-gateway@latest --model <m> --agent <a>` | the `remote-agy` Make target |

Those two Make targets are the v1 specification: the daemon automates exactly
what a person does by hand today, which keeps the first version honest and
testable against a known-good baseline.

- **Kind → argv resolution lives in the daemon**, from its own config. The
  backend supplies only validated parameters (workspace id, model, agent name),
  each pattern-checked before reaching an argv slot.
- **Lifecycle**: `starting → running → exited(code) | killed | failed`.
- **Kill** is SIGTERM then SIGKILL after a grace period (Windows:
  `TerminateProcess` on the job object), applied to the whole **process group** —
  agents spawn children and leaking them is how a machine fills up.
- **No automatic restart** in v1. A crashed agent stays dead and visible;
  restart-on-crash hides the failures a person most needs to see early.
- **Two concurrency caps, and the global one is the real protection.** A
  per-profile cap stops one account running away; a **whole-machine cap** is what
  stops two accounts collectively exhausting a box that neither can see the other
  using. CPU and memory are physical and shared, so the limit that matters is the
  one on the hardware.

---

## 6. Machine telemetry

The UI shows, per machine: **total and free memory, and CPU usage**, alongside
the session list. Enough to answer "can this box take another agent?" without
opening a terminal on it.

- **Source**: `github.com/shirou/gopsutil/v4` — one API over `/proc`, `sysctl`
  and the Windows performance counters. It is the boring, maintained answer;
  hand-rolling three platform paths for this is not a good use of the effort.
- **Sampled continuously, reported on the heartbeat.** CPU percentage is a
  *rate*, not a reading: it needs two samples separated by an interval. The
  daemon keeps a sampling loop and reports the last computed value — the
  heartbeat must never block on a measurement.
- **Payload** on each heartbeat (every 10–30 s):

```
{ "memTotal": 16777216000, "memAvailable": 4203741184,
  "cpuPercent": 37.4, "loadAvg": [1.2, 0.9, 0.7], "uptimeSec": 918273,
  "disks": [ { "mount": "/",     "total": 494384795648, "free": 91203829760 },
             { "mount": "/data", "total": 1998678822912, "free": 1502398476288 } ] }
```

**Disk is reported per mount, not as one number.** A single "free space" figure is
a lie on any machine with more than one filesystem — the box can be 2 % full and
still fail to check out a repository because `/` is the full one. Report the
mount the workspace folder is actually on (§11) plus the root filesystem, and let
the UI show which is which.

Two platform details, so nobody has to rediscover them: on Linux the interesting
mounts have to be filtered out of a list that includes dozens of `tmpfs`,
`overlay` and `squashfs` entries; on Windows the same call returns drive letters.
`gopsutil` normalises both, which is a second reason to use it rather than
reading `/proc` directly.

  `loadAvg` is Unix-only and simply absent on Windows; the UI must render a
  machine that does not report it, rather than showing a zero that looks like an
  idle box.

- **Storage: latest snapshot on the `Machine` row, nothing historical.** Writing
  every heartbeat to a table is a table that grows forever to power a widget
  nobody has asked for. If a sparkline is wanted later, keep a bounded in-memory
  ring per machine in the backend and accept that it empties on restart — that
  is the right trade for a liveness graph.
- **Free vs available memory**: report `available`, not `free`. On Linux `free`
  excludes cache and reads alarmingly low on a perfectly healthy machine; the
  number a person wants is what a new process could actually get.

---

## 7. Auto-update and session restoration

The daemon updates itself, but **never on its own initiative**: it reports that a
version is available, and a person approves from the panel. The approval means
*"kill the sessions and update"* — that is the deal, and the UI must say so in
those words, because it is destructive.

### The sequence

```
1. daemon polls the release feed  →  reports {available: "0.7.1"} on heartbeat
2. UI shows "Update available" on the machine, with what will be lost
3. user approves  →  control op  updateNow
4. daemon: record running sessions → kill them → swap binary → restart
5. on start: re-spawn the recorded sessions
```

### What "restore the sessions" honestly means

The restored sessions are **new processes with new PTYs**. What is restored is
the *intent* — same kind, same workspace, same model and agent arguments — not
the state. Scrollback does not survive, the agent's in-flight work does not
survive, and anything half-typed at a prompt is gone.

That is worth stating plainly because "spin up the same sessions back" can be
read as continuity, and it is not. If genuine continuity is wanted, that is the
tmux-shaped problem in §18 and a different piece of work. **The restoration
design here is the cheap 90%, deliberately.**

- A restored session is **marked as restored** in the UI and in its audit record,
  so a person is not left wondering why their terminal is empty.
- **Restore intent is persisted to disk** before the kill, not held in memory —
  the whole point is that the process is about to be replaced.
- **Restore is best-effort and bounded**: if a session fails to come back, it is
  reported as failed rather than retried forever.

### Replacing a running binary

- **Unix**: write the new binary beside the old, `rename(2)` over it — atomic,
  and the running process keeps its open inode until it exits — then re-exec.
- **Windows**: you cannot delete a running `.exe`, **but you can rename it**. So:
  rename the running binary aside, write the new one into place, restart, and
  clean up the stale file on next start. This is the same class of platform trap
  as ConPTY in §8 and deserves the same treatment — prove it in the spike rather
  than assume it.
- **Who restarts the process** depends on how it was installed: under systemd or
  launchd, exit and let the supervisor restart it; run by hand in a terminal, the
  daemon must re-exec itself. Both paths need to exist and be tested.
- **Verify before swapping.** The downloaded binary's signature or checksum is
  checked against the release manifest before anything is replaced. An update
  channel that does not verify is a remote code execution path that bypasses
  every boundary in §2 — the one place in this design where getting it wrong is
  unrecoverable.
- **Never auto-update on a failed start.** If the new binary fails to come up,
  roll back to the retained old one and report. An update loop that crashes on
  boot takes the machine offline with no way in.

---

## 8. The PTY layer

**Go.** The backend is already Go 1.27, the wire types can be shared rather than
duplicated, and it is one toolchain for the team. Rust would win only if we
needed something Go cannot do here; we do not.

**Cross-platform PTY is the hardest portability problem in this plan.**

- Unix (macOS, Linux): `github.com/creack/pty` — `openpty`/`forkpty`,
  `TIOCSWINSZ` to resize.
- Windows: needs **ConPTY** (`CreatePseudoConsole`, Windows 10 1809+), which
  `creack/pty` does not cover. `github.com/aymanbagabas/go-pty` offers one
  interface over both.

**Windows is first-class in v1** — decided — which makes this the highest-risk
item in the plan rather than a formality. **This needs a spike before the plan is
trusted** (M0, §17). I have not run the Windows path on real hardware. Windows PTY differs in ways that bite late: no
`SIGWINCH`, different signal semantics, CRLF handling. Treat any estimate that
assumes Windows "just works" as unfounded.

Per session the daemon holds: the PTY master, the child handle, a **headless VT
maintaining screen state and bounded scrollback** (§10 — not a byte buffer, which
is the wrong structure for output that redraws in place), and the current
`cols`/`rows`.

---

## 9. Wire protocol (daemon ↔ backend)

**Binary frames**, because PTY traffic is arbitrary bytes at volume; JSON with
base64 costs a third more per frame and invites questions about how `0x1b` or
invalid UTF-8 survives. As bytes, they simply do.

```
[ type:1 ][ sessionId:8 ][ payload... ]        sessionId 0 == control
```

| Type | Direction | Payload |
|---|---|---|
| `0x00` CONTROL | both | JSON (see below), `sessionId` 0 |
| `0x01` INPUT | backend → daemon | raw bytes for the PTY (`Esc` is just `0x1b`) |
| `0x02` OUTPUT | daemon → backend | raw bytes from the PTY |
| `0x03` RESIZE | backend → daemon | `{"cols":120,"rows":40}` |
| `0x04` EXIT | daemon → backend | `{"code":0}` |
| `0x05` REPLAY | daemon → backend | synthesised redraw of current screen state on attach (§10) |

Control messages (`type 0x00`) are JSON with `{id, op, ...}`, `id` correlating
replies:

| Op | Direction | Carries |
|---|---|---|
| `hello` | daemon → backend | protocol version, agentrqd version, os/arch |
| `heartbeat` | daemon → backend | liveness **and the machine metrics of §6** |
| `startSession` / `killSession` | backend → daemon | kind + validated params; session id |
| `sessionState` | daemon → backend | lifecycle transitions, exit codes, `restored` flag |
| `attach` / `detach` | backend → daemon | begin/end streaming a session |
| `updateAvailable` | daemon → backend | version the daemon has found and verified |
| `updateNow` | backend → daemon | the user's approval to kill sessions and update (§7) |
| `error` | both | correlated failure, never a silent drop |

Points that matter more than the table:

- **`Esc` needs no special case, and that is the point.** The UI sends the byte
  the key produced. Any design enumerating "special keys" is wrong for the next
  one; a transparent byte pipe is right for all of them. `xterm.js`'s `onData`
  already yields exactly these bytes.
- **Replay on attach** is a redraw of the current *screen state*, not a replay of
  byte history (§10). It lives in the **daemon**, so the backend accumulates no
  per-session buffer it would then have to bound and evict.
- **Backpressure and redraws** are their own problem, and bigger than they look —
  see §10. Short version: coalesce on a timer, never drop bytes from the middle of
  a stateful stream, and resync from screen state when a client falls behind.
- **The relay never parses the stream.** The backend authorises the attach, then
  copies bytes. It must not interpret terminal output, ever.
- **Version the protocol in `hello`** from the first commit. A daemon in the
  field is not upgradable on demand, so old daemons talking to new backends is
  the normal case, not the exception.

---

## 10. Screen state, redraws and backpressure

A progress bar is not a lot of output. It is **one line, rewritten a hundred
times a second** — and it breaks two things an earlier draft of this plan got
wrong. `claude-code` does exactly this, so it is the common case, not an edge
case.

### Why it breaks the naive design

An agent drawing a spinner or a progress bar emits carriage returns and cursor
movement — `\r`, `ESC[2K`, `ESC[1A` — to overwrite what it already drew. So:

1. **A byte ring buffer fills with frames nobody wants.** 256 KiB of scrollback
   becomes 256 KiB of the *same line* redrawn, and the useful history before it
   has been pushed out. Replay on attach then shows a few seconds of spinner and
   nothing else.
2. **"Drop the oldest bytes" is actively destructive.** Terminal output is a
   stateful stream, not independent messages. Drop a chunk and you may drop the
   `ESC[0m` that resets colour — everything after renders wrong, permanently.
   Worse, dropping *part* of an escape sequence leaves the emulator parsing
   garbage. The elision marker an earlier draft proposed would land in the middle
   of a broken sequence and make things worse, not clearer.

### Three fixes, in order of how much they buy

**1. Coalesce on a timer — this alone handles most of it.** Do not forward every
PTY read. Batch whatever arrives in a ~16–33 ms window (30–60 fps) and send it as
one frame. A spinner redrawing 100×/s collapses to ~30 frames/s.

The important property: **concatenation is lossless.** The bytes are unchanged
and still contiguous, so the emulator sees exactly the stream it would have seen;
the intermediate redraws simply land within one paint. Visually identical, a
fraction of the frames. This is what tmux, ttyd and VS Code's terminal do.

**2. Keep a terminal, not a buffer.** For replay-on-attach the right structure is
not a byte ring — it is **screen state**. The daemon runs a headless VT per
session (`github.com/hinshun/vt10x`, or Charm's `x/vt`) that consumes the PTY
output and maintains a cell grid plus bounded scrollback. On attach it
**synthesises a redraw of the current screen** rather than replaying history.

That is precisely how tmux behaves when a second client attaches, and it fixes
the progress-bar case completely: you get the bar as it looks *now*, not a
thousand frames of how it got there.

It costs a dependency and real work, so worth saying plainly: the byte ring is
fine for linear output and wrong for anything that redraws. Since `claude-code`
redraws, **the VT is in scope from M3, not deferred**.

**3. On overrun, resync instead of dropping.** If a session still outruns the
socket after coalescing:

- Never drop bytes from the middle of the stream.
- Keep feeding the daemon-side VT — bounded, cheap, and what keeps the screen
  correct.
- Mark the client desynced and send a **fresh full-screen redraw** from the VT.

The viewer loses intermediate frames and keeps a correct screen. That is the
right trade, and strictly better than a corrupted stream with a marker in it.

### Input is the exception: never coalesce it

Everything above is about **output**. Input is the opposite, and the distinction
is not a nicety — it was found by the M0 spike failing on Windows.

A terminal cannot tell a lone **Escape key** from the **start of an escape
sequence** except by timing: `0x1b` alone is Esc; `0x1b` followed closely by
more bytes is a sequence. Writing `{0x1b, '\r'}` in a single write passed on
Unix and failed on ConPTY, which began a sequence and swallowed the CR — the
receiving process saw a complete line containing zero bytes.

So **input frames are forwarded as they arrive and never batched**. Applying the
output coalescer to input would merge a deliberate Esc with the next keystroke
and synthesise an escape sequence nobody typed. It is also the cheaper
direction: a person types a few bytes a second, not megabytes.

Latency runs the same way. Coalescing output by 16–33 ms is invisible; doing it
to input is 33 ms added to every keypress, which is felt.

### Caps worth having

- **Per-session byte-rate cap on the relay**, so one runaway process cannot
  saturate the backend for everyone.
- **Sustained saturation is a signal, not something to absorb quietly** — a
  process emitting megabytes a second is usually a bug on that machine, and the
  UI should say so rather than throttling silently forever.
- **Bounded scrollback** in the VT (a few thousand lines). Older content is gone,
  exactly as in a real terminal.

---

## 11. Launching an agent for a workspace

The flow a person actually uses: open a workspace that has no agent attached,
press a button, and have one running on a chosen machine.

### The precondition, and why it is a precondition

**A workspace gets at most one agent connection.** The backend already knows
whether one is live — `Manager.IsAgentConnected(workspaceID)` in
`backend/internal/controller/mcp/manager.go` — and that check is the gate. If an
agent is already connected, the UI offers no launch; the button is absent rather
than present-and-failing.

This must be checked **twice, and the second time is the one that counts**: once
in the UI so the affordance is honest, and again in the backend at the moment of
launch, because two people can press the button at the same moment. Only the
server-side check can make that race come out with one agent.

### The folder

`Workspace.WorkingDirectory` already exists, is validated, and is already
described in the backend as *"the working directory a workspace hands to its
agent"* — with a comment noting the agent usually runs on a different machine, so
the server cannot check the path exists. That field is the folder, and there is
no need for a new one.

- **Empty means the user is asked first.** "If the workspace has no folder set up,
  the user has to provide it" maps exactly onto `WorkingDirectory == ""`, so the
  launch flow sends them to set it rather than guessing or defaulting to the
  daemon's home directory.
- **The daemon verifies what the server cannot.** The server can only refuse a
  path that could not be a directory anywhere. The daemon knows: it checks the
  directory exists and is writable *before* declaring the session started, and
  fails with a message naming the path — not a generic "failed to start".
- **One path for all machines — decided, with a consequence to handle.**
  `WorkingDirectory` stays a single value; there is no per-machine override. Every
  machine a workspace runs on must therefore use the same path — a convention a
  fleet can keep and a laptop often cannot. **So the failure has to be a good
  one**: when the directory is missing on the chosen machine, say exactly that —
  the path, the machine, and that the workspace expects it there — rather than a
  generic "failed to start". That message is the whole mitigation.

### Handing the agent its MCP configuration

The backend sends the MCP config with the start command; the daemon writes it and
launches the agent in the folder. That mirrors what a person does by hand today —
an `.mcp.json` in the project directory pointing at the workspace's MCP endpoint
with a token.

```
startSession { kind: "claude-code", workspaceId, cwd,
               mcp: { url, token, serverName } }
   → daemon: verify cwd exists and is writable
   → daemon: write .mcp.json (0600), owned by the daemon user
   → daemon: spawn `claude … server:<workspace>` with cwd set
```

Three things this design has to be careful about, because a credential is landing
on a disk:

- **`0600`, always.** The token in that file is the workspace's. On a machine with
  other users, a default-permission file hands it to all of them.
- **Never overwrite a person's own `.mcp.json` silently.** If one already exists
  and differs, that is a decision the user has to make — merge the AgentRQ server
  entry rather than replacing the file, and say what happened.
- **Scope the token to the workspace and keep it short-lived.** It is now at rest
  on a machine the account does not fully control. This is the one place in the
  design where the daemon holds a credential it did not mint, and it should hold
  the smallest, shortest-lived one that works.

### What the UI shows

On a workspace with no agent: a machine picker (only machines that are online and
enabled), the folder — prefilled from `WorkingDirectory`, or a prompt to set it
when blank — and a launch button. Afterwards the workspace shows which machine
its agent is on, with a link to that session's terminal.

---

## 12. Backend changes

**Models** (monoflake ids, GORM, camelCase in the API per the coding standards):

```
Machine  { ID, UserID, Name, Hostname, OS, Arch, Version,
           TokenHash, Enabled, LastSeenAt, CreatedAt,
           AvailableVersion?,                       // §7, null when current
           MemTotal, MemAvailable, CPUPercent, LoadAvg?, UptimeSec,
           MetricsAt }                              // latest only, never history
Session  { ID, MachineID, UserID, Kind, WorkspaceID?, Status,
           ExitCode?, Cols, Rows, StartedAt, EndedAt? }
```

`Enabled` is the §2.4 kill switch. Online/offline is **derived** from
`LastSeenAt` against a threshold, never stored — a stored flag goes stale
exactly when the daemon dies unexpectedly, which is when it matters most.

**WebSocket endpoints** (stdlib `mux`, beside the existing SSE routes, using
`gorilla/websocket` — already an indirect dependency, would become direct).

> **Why not `gofiber/contrib/websocket`?** It was asked, it is the natural
> question given the app is a Fiber app, and the answer is that **Fiber is not
> the server here.** `backend/internal/service/server` runs a `net/http.Server`
> whose handler is the stdlib `mux`; Fiber is mounted *inside* it via
> `adaptor.FiberApp(fiberApp)`. That adaptor builds a **synthetic fasthttp
> context** from a net/http request — `app.go` says so in as many words when
> explaining why Fiber's logger is missing there.
>
> A WebSocket upgrade needs to hijack the real connection, and a synthesised
> context has none to hand over. `gofiber/contrib/websocket` is fasthttp-based,
> so it cannot work through the adaptor no matter how the route is declared.
>
> This is the same structural reason SSE already lives on the mux rather than on
> Fiber, so the WebSocket sitting beside it is consistent with the codebase, not
> an exception to it. Using Fiber's websocket package would mean making Fiber the
> top-level server — a far larger change than this feature should carry.

The endpoints:

- `/api/v1/daemon/connect` — the daemon's single multiplexed socket, authorised
  by machine token.
- `/api/v1/sessions/{id}/stream` — one per browser attach, authorised by session
  cookie; the backend pairs it to the owning machine's socket.

**REST** (Fiber, `/api/v1`, for the browser — per `AGENTS.md` these belong on
Fiber, *not* the stdlib mux): list/get/rename/enable/disable/delete machines,
create enrolment code, list sessions, start session, kill session,
**approve update**.

**In-memory registry** of connected machines, mapping machineId → socket, in the
shape of the existing MCP `Manager`. It is per-process — which is why the
pairing below is stored.

### Several backend instances, and how a browser finds the right one

**Decided: the backend may run more than one instance**, and the pairing is
stored — `(machineId, instanceId)` — so any instance can find out which of its
peers holds a given daemon's socket.

```
ServerInstance    { ID, Address, StartedAt, LastSeenAt }   // internal address
Machine.InstanceID, Machine.ConnectedAt                     // who holds the socket
```

A `Machine` row is per profile and therefore holds exactly one socket, so the
owning instance can live on the machine row itself rather than in a join table.

**The attach path:**

```
browser → load balancer → instance B → /api/v1/sessions/{id}/stream
   1. look up session → machine → machine.instanceId
   2. instanceId == mine?      → serve locally
   3. otherwise                → dial that instance's internal address,
                                 open an internal socket, relay bytes through
   4. peer unreachable?        → clear the stale row, report the machine offline
```

**Clear the pairing on disconnect** — when the socket closes for any reason, the
instance that held it deletes the pair. A row that outlives its socket sends the
next attach to an instance that will not answer.

**And still treat it as a lease, because the clean path is not the only path.**
An instance that is killed outright never gets to delete anything, so the row
survives it and nothing about that is self-correcting. Belt and braces:

- deleted on clean disconnect, which covers the normal case;
- `LastSeenAt` refreshed on each daemon heartbeat, so a row whose lease has
  expired is ignored even though it is still there;
- the daemon's own reconnect — to whichever instance the balancer hands it —
  rewrites the pair and repairs the record;
- an attach that finds a dead peer clears the row itself and reports the machine
  offline. It must never hang.

**What this requires from the deployment, and it is not free:**

- **Every instance needs a stable id and an address its peers can reach.** In
  Kubernetes that is the pod name and the pod IP; elsewhere it has to come from
  configuration. If instances cannot reach each other, this design does not work
  and the only alternative is sticky routing at the balancer.
- **Instance-to-instance calls are a new trust boundary.** That hop carries
  terminal bytes for someone's session, so it needs its own authentication — a
  shared secret or mTLS — and must not be reachable from outside the cluster.
- **One extra hop of latency on keystrokes.** Within a datacentre this is
  irrelevant; across regions it would not be.

**The simpler alternative, if your deployment allows it:** give each instance a
publicly addressable hostname and have the browser connect straight to the
instance holding the socket. That removes the relay hop and the internal trust
boundary entirely — but it requires per-instance public addressability, which
most load-balanced deployments deliberately do not have.



**SSE**: machine and session state changes publish onto the existing pub/sub so
the UI updates without polling, reusing the forwarder in `app.go`.

---

## 13. Frontend changes

- **Routes go in `frontend/src/app.js` and nowhere else.** The desktop renderer
  mounts the same table; a second copy is how the two builds silently drift.
  New: `/machines`, `/machines/:id`.
- **Machine detail** shows memory and CPU from §6 beside the session list, and an
  **Update available** affordance whose confirm copy names what it destroys —
  "this kills N running sessions and restarts them" — not a bare "Update?".
- **Terminal**: `xterm.js` + `@xterm/addon-fit`. A new runtime dependency that
  lands in the desktop renderer too — check bundle size before committing.
- **The 100% coverage gate covers only an explicit include list.** Per the
  repo's established pattern, logic goes in a composable
  (`useTerminalSession.js`: framing, reconnect, resize debounce, replay) which
  is tested directly; the `.vue` stays thin. Decisions must not live in the SFC.
- **WebMCP**: `frontend/test/webmcpTools.test.js` enforces one tool per `api.js`
  function. Machine/session **read** functions get tools. **Terminal input does
  not — decided.** It goes in that test's exemption list with the reason written
  out: giving a browser agent raw keystroke access to a remote shell is a
  materially different grant from "anything the UI can do". Writing the reason
  down is the point — an unexplained exemption is indistinguishable from an
  oversight, and the next person tidying the list would delete it.
- **Telemetry**: any new client-reported action needs all four places updated
  (constant + `String()`, allowlist, append-only `model.ActionID*`, controller
  mapping) or it reads as zero.

---

## 14. Where the code lives

A top-level `daemon/` directory, as its own Go module:

```
daemon/go.mod                  module github.com/agentrq/agentrq/daemon
daemon/cmd/agentrqd/           main, CLI (enroll, run, status, disable)
daemon/internal/supervisor/    sessions, lifecycle, restore
daemon/internal/pty/           the platform layer (§8)
daemon/internal/vt/            screen state (§10)
daemon/internal/enrol/         profiles, tokens, config (§4)
daemon/wire/                   frame format + control ops — EXPORTED, because
                               the backend imports this exact package
go.work                        ties backend/ and daemon/ together for local dev
```

An earlier draft put this inside the backend module, on the grounds that the
wire protocol must have exactly one definition — **protocol drift between relay
and daemon is the worst bug available in this design.** A top-level folder is
the call, and it does not have to cost that: the rule is not "one module", it is
**one definition**. So `daemon/wire` is a deliberately exported package, and the
backend depends on it (`require` plus a `replace ../daemon`, with `go.work` for
day-to-day work) rather than declaring its own copy of the frame format.

What to watch, given the boundary is now a module edge rather than a package
one: `daemon/wire` must import nothing else from `daemon/internal`, or the
backend cannot depend on it. Keeping it dependency-free is what keeps this
arrangement honest — and a test in the backend that round-trips a frame through
that package is what proves the two sides still agree.

Cross-compiled for six targets with GoReleaser. No cgo (the PTY libraries use
syscalls directly), so this stays a clean matrix build.

---

## 15. Failure modes to design for, not discover

| Failure | Required behaviour |
|---|---|
| Backend unreachable | Sessions keep running; reconnect with backoff + jitter. Never kill work because the control plane blinked |
| Daemon killed / machine sleeps | Sessions die with it (v1). UI shows machine offline and sessions `unknown` — never a stale `running` |
| Socket drops mid-session | Session survives; on reconnect the UI re-attaches and gets a replay |
| Two browsers attach to one session | Both see output, both can type. Allowed, but surfaced — the UI names the other viewer |
| Agent outruns the UI | Coalesce, then resync from screen state — never drop bytes mid-stream (§10) |
| Agent redraws one line 100×/s (progress bar) | Time-coalesced frames; replay is a screen redraw, not byte history (§10) |
| Machine token revoked | Server closes the socket; daemon stops supervising and reports. It must not keep running blind |
| Backend runs multiple instances | Attach must reach the instance holding that daemon's socket (§12, §18) |
| Update binary fails to start | Roll back to the retained old binary and report. An update loop that crashes on boot takes the machine offline with no way back in |
| Session fails to restore after update | Reported as failed, not retried forever; the machine still comes back |
| Metrics unavailable (no `loadAvg` on Windows) | Field absent; UI renders the machine without it — never a zero that reads as idle |
| Clock skew | Online/offline derived from server time only |

---

## 16. Testing

- **Daemon (Go)**: table tests over framing, argv resolution and the lifecycle
  state machine — all pure. The PTY sits behind an interface so the supervisor
  is testable without a terminal, plus a few **real-PTY integration tests** gated
  by OS, because a mocked PTY proves nothing about ConPTY.
- **Backend**: handler tests with `mockgen` as elsewhere, plus a relay test
  pushing bytes through a real `httptest` WebSocket in both directions and
  asserting `0x1b` **and invalid UTF-8 survive byte-for-byte**. That single
  assertion protects the actual feature.
- **Frontend**: composable tests against a fake socket — and they must be added
  to the coverage include list or they do not count toward the gate.
- **Cross-platform CI**: daemon tests run on all three OSes. Note from recent
  work in this repo: the Windows runner checks text files out with CRLF, so
  anything hashing or byte-comparing a text fixture must normalise first.

---

## 17. Phasing

| Milestone | Deliverable | Proves |
|---|---|---|
| **M0 Spike** | PTY open/read/write/resize/kill on macOS, Linux **and Windows**; WS echo through the relay | The one genuinely uncertain thing, before anything is built on it |
| **M1** | Enrolment code, machine token, connect + heartbeat, machines list | The credential scheme we now own |
| **M1b** | `ServerInstance` + `Machine.InstanceID`, peer relay, pair cleared on disconnect (§12) | Multi-instance, decided in §18 — build it with the relay, not after |
| **M2** | Start/kill the two kinds; session list; audit records | Supervision |
| **M2.5** | Launch an agent for a workspace: the connected-agent gate, the folder, the MCP config (§11) | The flow people actually press a button for |
| **M3** | Output streaming, read-only terminal, **headless VT + coalescing** | Data plane, and the redraw problem of §10 — which is the common case, not a refinement |
| **M4** | Input, `Esc`, resize | The feature as asked |
| **M5** | Machine metrics on the heartbeat; memory/CPU/disk in the UI | Cheap, independent, useful on its own |
| **M6** | Approved self-update + session restoration, incl. the Windows rename dance | The most dangerous code in the plan, done last and deliberately |
| **M7** | Packaging, signing, docs, kill switch, audit surfacing | Shippable |

M0 is not a formality. If Windows ConPTY needs real work, that is far better
known in week one — and it is the likeliest place for these estimates to be wrong.

---

## 18. Decisions, and who made them

All seven open questions were answered on 2026-09-15. Recorded here with what
each one changed, so the reasoning survives the conversation it happened in.

| # | Question | Answer |
|---|---|---|
| 1 | More than one backend instance? | **Yes** — store the `(machineId, instanceId)` pair, delete it on disconnect |
| 2 | Sessions survive a daemon restart? | **No** — keep it simple for v1 |
| 3 | Plain HTTP for self-hosted? | **Optional** — TLS unless loopback, `--insecure` to override |
| 4 | Windows in v1? | **First-class** |
| 5 | Agent access to machines? | **No** — agents keep using the workspace MCP |
| 6 | Folder per machine? | **No** — one path per workspace |
| 7 | Terminal-input WebMCP tool? | **No** |

**1 — Multiple instances, with the pair stored and cleared.** The largest change,
and it is designed in §12 rather than deferred: `ServerInstance` plus
`Machine.InstanceID`, an attach that relays to the owning peer, and the pair
deleted when the socket closes. Two things this asks of the deployment, and they
are not free: instances must be able to reach each other at a stable address, and
that hop carries someone's terminal bytes so it needs its own authentication.
Both are written up in §12; neither should be discovered during implementation.

**2 — Simple, and here is what "simple" means.** Sessions die with the daemon.
No PTY re-parenting, no tmux. The update flow restores sessions by *intent* (§7)
— same kind, same workspace, same arguments, **new process and empty scrollback**.
Taking the decision as delegated: this is the right call for v1 because the
alternative adds a hard dependency or platform-specific process surgery to get a
property nobody has yet asked for. It is reversible later; the restore records
already carry everything a continuity implementation would need.

**3 — Optional, and loud when used.** TLS required and verified unless the server
is loopback, where it is relaxed automatically. `--insecure` overrides, and the
daemon reports that it is running insecurely on every start and in `agentrqd
status` (§2). A warning printed once at first boot is not a safeguard.

**4 — Windows is first-class**, which promotes the M0 spike from prudence to the
critical path. ConPTY, no `SIGWINCH`, and the rename-the-running-exe dance for
self-update (§7) are all now must-work rather than nice-to-have. **If the plan's
estimates are wrong anywhere, it is here.**

**5 — Agents use the workspace MCP, not the core.** This confirms that dropping
`coremcp` costs nothing: the surface agents actually use is unchanged. Machine
and session control stays out of both MCP surfaces in v1 — the daemon's control
plane is the WebSocket, and the UI is the way people drive it. *Flagging my
reading in case it is wrong: I have taken this as "do not add machine tools to
any MCP surface", not as "add them to the workspace server instead".*

**6 — One folder path per workspace.** `Workspace.WorkingDirectory` as it already
exists; no schema change. The cost is that every machine must use the same path,
so the error when it is missing has to name the path and the machine rather than
saying "failed to start" (§11).

**7 — No terminal-input WebMCP tool.** It goes in the parity test's exemption
list with the reason recorded (§13).

### Still genuinely open

Nothing blocking. Two things deliberately left for later, listed so they are not
mistaken for oversights:

- **True session continuity** across a daemon restart (see 2 above) — reversible,
  and the restore records already carry what it would need.
- **A per-(machine, workspace) folder override** (see 6) — a schema change plus
  UI, worth doing only once a workspace genuinely runs on machines with different
  layouts.
