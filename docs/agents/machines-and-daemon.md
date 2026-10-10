# Machines and the daemon (`daemon/`, `/machines`)

> Read before changing `daemon/`, the machines pages, terminal streaming, or the daemon release and self-update path. Full detail is in [`daemon/README.md`](../../daemon/README.md); user-facing docs are [`docs/DAEMON.md`](../DAEMON.md).

A machine is somebody's computer running `agentrqd`, enrolled against an
account. It holds a WebSocket to the backend, runs agents in real
pseudo-terminals, and streams them to the browser.

- **`daemon/` is a separate Go module**, wired in with a `replace` directive
  rather than a `go.work` (which is deliberately absent — it is not checked in,
  and builds must work without it). `daemon/wire` is the *only* definition of
  the frame format and is imported by the backend; there is no second copy to
  drift.
- **Two sockets, both on the stdlib `mux` in `app.go`, never on Fiber.** Fiber
  is mounted through an adaptor that synthesises a fasthttp context, and a
  WebSocket upgrade has to hijack a real connection, which a synthesised
  context does not have.
- **`terminalSocketUrl` and `serverOrigin` are the only absolute URLs in the
  frontend**, and they are a deliberate exception to the
  no-absolute-API-URLs rule in [`AGENTS.md`](../../AGENTS.md) rather than an
  oversight: Electron's custom-protocol handler forwards `/api` but does not
  intercept WebSockets, so a relative URL would resolve to `app://` and never
  open. Both ask the shell where the server is. Nothing else should.
- **The socket authenticates with a ticket, not the `at` cookie**, and the
  section below says why. A ticket lasts a minute, so it is fetched per
  connection attempt and never cached.
- **Terminal input is exempt from the WebMCP parity rule on purpose**, with the
  reason written into the exemption in `frontend/test/webmcpTools.test.js`:
  raw keystroke access to a remote shell is a materially different grant from
  "anything the interface can do".
- **The attach is audited; the keystrokes are not.** A test types a password
  into a terminal and asserts it appears nowhere in the log.
- **The session caps count what is running, not what is remembered.** A
  finished session stays in the supervisor's map so its exit can still be
  reported and `Forget` is never called, so counting the map refused new agents
  on a machine running none. Skip terminal states, as `Running`, `Live` and
  `Dirs` already do. The numbers themselves are `--max-per-profile` and
  `--max-per-machine`, defaulting to the constants in `serve.go`.
- **A refused launch is only ever explained by the event.** The session row is
  deleted the instant it fails, so `session.updated` carries the reason and
  nothing else does — the terminal page it navigated to has nothing left to
  read. `useStreamToasts` is what puts it on screen; drop `payload.error` there
  and the reason exists only in that machine's daemon log.
- **Merging a workspace fork kills its agent first, and waits for the row to
  say so** (`forkmerge.Merger`, shared by REST and CoreMCP). Merging past a
  live one leaves it working, token on disk, for a workspace that is gone; so an offline machine refuses the
  merge, and the unfinished-task check runs before the kill.
- Machine and session events ride the **user's global** stream
  (`bus.Publish(0, userID, …)`), not a workspace's: a machine does not belong
  to a workspace, and the person watching the machines page may have none open.

## A reconnect is not a restart, and the sessions have to be told so

The daemon reconnects rather than exiting, and two things have to survive that
gap or reconnecting achieves nothing.

- **The agent must not be bound to the connection's context.** The pty layer
  kills the process whose context is done, and the context reaching
  `Supervisor.Start` is the daemon's socket to the backend — cancelled on every
  reconnect. Passing it straight through killed every agent on the machine
  whenever the backend restarted or the network blinked. `Start` therefore
  hands the process a `context.WithoutCancel` of it: what ends a session is
  `Kill`, the process itself, or `StopAll` at shutdown, never a dropped socket.
- **The pump belongs to the session, not to the socket.** It holds the screen,
  which has to survive the gap or the next viewer gets a blank terminal it can
  never get back, and it is blocked reading a pseudo-terminal nothing has
  closed — so a second pump would not replace the first, it would race it for
  every byte. On a lost connection the viewers are forgotten and the pumps
  stay; on the new one `streams.rebind` moves them across and repaints
  whatever somebody is still watching. The browser's own socket never dropped,
  so nobody is going to ask to attach again on its behalf.
- **Anything slow runs off the frame loop, and a launch that does streams before
  it reports `running`.** Every session on the machine rides one socket, so a
  git call or a copy on the loop freezes every terminal. The backend lets a
  viewer attach on hearing `running`, and an attach with no stream is dropped.

Both were found by disabling a machine while an agent was running on it, which
closes the daemon's socket from the server's side — the cheapest way to make a
reconnect happen on demand.

## The agents go when the daemon goes

Stopping `agentrqd` stops every session it is running, explicitly, before the
connections close.

They would mostly go anyway — closing a pseudo-terminal hangs up on the process
using it — but "mostly" is not a design, and the failure it hides is total: an
agent that outlives its daemon is unreachable. Nothing lists it, nothing can
stop it, and the next daemon does not adopt it, so the panel shows an idle
machine while a process keeps working against the workspace with the
credential still sitting in its folder.

Two details in `cmdServe` are load-bearing and easy to undo:

- **The connections get their own context, not the signal's.** Sessions are
  killed and their exits reported while the sockets are still up; sharing the
  signal's context closes them first and the backend never hears what happened
  to the agents, which then linger in the panel as running.
- **The wait is bounded.** A process that ignores the hang-up must not hold the
  machine's shutdown open, so `StopAll` gives up after `shutdownGrace` and says
  how many it stopped.
- **SIGHUP is one of the signals it stops for**, because `agentrqd serve` is
  documented as something you run in a terminal: unhandled, the default
  disposition ends the process when that terminal closes and none of this runs,
  so every agent on the machine is orphaned. An *ignored* SIGHUP is left alone —
  that is `nohup`, asking for exactly the opposite.
- **The shutdown is announced before the work, with the signal that caused it.**
  Without that line a stop and a kill leave identical logs, and the absence of
  it is what identifies a SIGKILL — which is why `signal.NotifyContext`, which
  cancels without saying what did it, is not used here.

## Self-update is the one place where getting it wrong is unrecoverable

- **A build with no release key refuses to update itself**, and says so. The key
  is a build-time `-ldflags` variable; empty is the default and the correct
  behaviour for every build that is not an official release. A verification step
  that silently passes when it has nothing to verify against is worse than none.
- **The manifest is signed as a whole** — version and platform table included,
  because those decide which file gets run — and every artefact's SHA-256 is
  mandatory and checked while downloading. There is no path to an artefact that
  skips the signature check.
- **The new binary is executed before anything is replaced.** After the swap the
  old process is gone and nothing can observe the new one failing; the previous
  binary is retained for `agentrqd rollback` and is deliberately *not* tidied up
  at startup.
- **Restoration is intent, not state**: new processes, new terminals, no
  scrollback. Restored sessions are marked as such so nobody wonders why their
  terminal is empty. The note that survives the restart carries no credential.
- **A restart or update from the panel exits 75, never 0.** The shipped units
  restart on failure only; exit 0 left the machine with no daemon.
- **A handover must not end the session rows.** The stopped agents' ends are
  not reported, and `Supervisor.Running` (and the next daemon's hello, via
  `Expect`) keeps naming them until restored — or the backend deletes the rows
  and the restored agents run unlisted. Both commands are sent only to a daemon
  ≥ `wire.MinRemoteControlVersion` with the `restart`/`update` capability.

## A background without a foreground is a bug in one theme

Dark mode is a `.dark` class, and the main content area sets no colour of its
own — so text inherits the document default, which is black whatever the
theme. Any element that sets a background and no text colour is therefore
readable in exactly one of the two, and light mode is the one you are looking
at while writing it. `style.css` pairs them properly for `.md-body pre`; do the
same for anything new.

**The terminal has a palette per theme, switched in place on the `.dark` class.**
Light mode also sets xterm's `minimumContrastRatio`: agents print 24-bit
colours picked for a dark background, which a palette cannot remap. Re-theme
through `term.options`, never by rebuilding the terminal, which drops its scrollback.

`color-scheme` is the other half, and it is not a Tailwind class. A class says
nothing to the parts of a page the *browser* draws — a `<select>`'s dropdown, a
caret, the autofill background, the default scrollbar — so without
`color-scheme: dark` on `.dark` those keep the light system palette on a dark
page, and no amount of styling from the page can reach them.

## Installation is answered in three places, and they must agree

The enrol command is useless on its own — it names a binary that is not there
yet — so the "Add machine" panel walks install → enrol → run, and the steps
come from `frontend/src/composables/useDaemonInstall.js` where they are tested
rather than from the template.

The same steps appear in `docs/DAEMON.md` (for somebody who has not downloaded
anything) and `daemon/packaging/INSTALL.md` (for somebody who has the archive
and not the page). Change one, change all three.

Linux and macOS install with `curl -fsSL https://agentrq.com/install-agentrqd.sh | sh`;
Windows, which has no `sh`, gets its own script instead, piped into `iex`:
`irm https://agentrq.com/install-agentrqd.ps1 | iex`. Both scripts live in the
**agentrq-landing** repository, not this one, so changing what either does is a
change over there. Added 2026-09-20 — before that, Windows had one step more
than the other two, doing by hand what the script now does.

**This used to forbid the one-liner**, on the grounds that piping unseen code
into a shell is worst on the very machine you are about to grant command
access to. That argument is still worth knowing, and it lost to two things: the
manual steps it protected verified nothing at all, and an install nobody
finishes protects nobody. What makes the trade sound is that the script's
SHA-256 check against the release's published `checksums.txt` is **mandatory
and fail-closed, with no flag to skip it** — so if that ever becomes optional,
this decision should be revisited rather than inherited. `docs/DAEMON.md`
carries the fetch-read-run form for anyone who wants to look first.

The rule that did *not* change: **never `sudo`**. The script installs to
`~/.local/bin` on macOS too, and prints the PATH line for the user's shell
rather than asking for a password. The daemon refuses to run as root,
and an install guide that works around that has removed the only thing keeping
an agent to what its user can already do. The installer refuses to run as root
for the same reason, and `daemonInstall.test.js` still enforces it here.

The LaunchAgent starts the daemon through `/bin/sh -c 'exec "$HOME/..."'`,
because launchd expands neither `~` nor `$HOME`. A literal path in its place is
right for one user and silently never starts for everyone else;
`daemon/packaging/packaging_test.go` runs it the way launchd does.

The detected platform picks which tab opens and nothing else: you are usually
setting up a machine other than the one you are browsing from.

## The terminal socket cannot use the cookie, and that is what broke the desktop app

For its whole life before this was fixed, the desktop terminal said
"Connecting" and stopped. The browser's worked. Two separate things made that
happen, and neither of them is visible anywhere near the terminal code.

**The policy refused the socket.** `buildCSP` in `desktop/src/main/protocol.js`
emitted `connect-src 'self' <model hosts>`, and `'self'` is the `app://`
origin. The socket is deliberately addressed absolutely, at the configured
server — so Chromium refused it. The way it refuses is the part worth
remembering: `new WebSocket()` **throws**, synchronously, rather than opening a
socket that then fails. So the policy now carries the configured server's
ws/wss origin, built per request because the server changes when somebody
switches profile — a policy captured at startup would work until the first
switch and then not, which is harder to diagnose than never working. Only the
socket origin goes in, never the server's `https` origin: nothing in the
renderer may address the server directly, and that is the whole point of the
`app://` proxy.

**Behind that, there was no credential.** The `at` cookie is `SameSite=Lax`, so
a browser withholds it from a cross-site request — which is what a WebSocket
from an `app://` page to the server is. Every other call escapes this by being
relative and getting forwarded by the main process, where the cookie jar lives;
this one cannot be forwarded at all. So the socket takes a **ticket**: a
one-minute JWT from `POST /api/v1/sessions/:id/terminal/ticket`, a perfectly
ordinary cookie-authenticated route that *is* forwarded like everything else.

Three things about the ticket are decisions rather than details:

- **Its audience carries the session id as well as a `terminal_ticket`
  marker.** Without the marker the `at` token itself would satisfy the check,
  and a credential that belongs in a cookie would also work in a URL — where
  proxies log it. Without the id, one ticket would open every terminal its
  holder can reach for as long as it lived.
- **A ticket that is present and bad is refused, not retried as a cookie.**
  Falling back would make every refusal above mean "try the other credential".
- **The cookie is still accepted.** It is the same authorisation either way,
  and dropping it would cost a page loaded before a deploy its terminal on the
  next reconnect, for no reason anybody could see.

The consequence for `useTerminalSession`: `connect` is **awaited**, and is
called again for every attempt. A URL computed once and kept would be refused
by every reconnect after the first minute — a terminal left open all afternoon
that silently stops recovering, which is worse than the bug this replaced.

**The general lesson, which cost more than either fix.** `open()` set the
status to `connecting` and *then* called `connect()`. When that threw, no
handler had been attached yet and nothing was left to set the status again — so
the panel sat on "Connecting" forever and the real reason went on the floor
inside an async `onMounted`. A state that is entered before the thing that
could fail, with no handler for the failure, is a hang rather than an error.
`connect` failing is now a reason on screen and a backed-off retry.

## A launch sends the real terminal size, not a constant

`useLaunchTerminalSize.js` measures `App.vue`'s content box — never unmounted
by the route change a launch is about to make — and fits a hidden `Terminal`
to it before the pty is created. A bigger hardcoded default is not a fix: no
single constant is right for both a phone and an ultrawide monitor, and this
is the one place in the launch path where guessing at cols/rows *is* the bug.

## A viewer names no session, and must not

A browser holds base62 ids; the frame header wants a 64-bit number. It has no
way to produce one and does not need to — the backend decides which session an
attached socket may drive and overwrites whatever arrives, which is what stops
a browser typing into another session by changing a number. So viewer frames
carry a zero session and `wire.DecodeFromViewer` expects that.

Asking the browser for the id instead is what broke the terminal: `BigInt` of a
base62 string throws, inside a keystroke handler where nothing was watching, so
output kept arriving and the keyboard did nothing. Every unit test passed a
number; the application passes a string. Fixtures that do not match the shape
the caller actually uses are how a bug like that survives a full green suite.

## Both agent kinds read `.mcp.json`

It is easy to assume only claude-code does — the gateway takes its agent (and
optionally a model) on the command line, so it looks self-contained — and
`make remote-agy` reinforces that, because it happens to run from the
repository root, which has one. It does not: the gateway reads the workspace
from the same file, and without it prints "Could not find .mcp.json" and dies
a second after starting.

So the backend mints an MCP token for both kinds and the daemon writes a config
for both.

The one exception: `acp-gateway --list-agents` needs no workspace at all — it
answers from the public registry before the gateway ever looks for
`.mcp.json` — but `--list-models --agent <a>` opens a real (if throwaway)
session and hits the same check, so `supervisor.ListAcpModels` (the daemon
side of the launch form's model autocomplete) takes a working directory and
`ListAcpAgents` does not. Confirmed against the gateway's own source, not
assumed from the pattern above.

## A launch writes three files, and never overwrites an entry it finds

`.mcp.json`, `.claude/settings.local.json` (claude-code only — the gateway asks
over ACP and never reads it), and a `.gitignore` line for **the config alone**
when the folder is in a checkout, written *before* it because it holds a token.
That line goes in the agent's own folder's `.gitignore`, never the repository's
top: in a monorepo every agent would otherwise edit one shared file. A rule in
any `.gitignore` from the top down to the folder already counts.
The permissions file is deliberately not excluded: no secret in it, so whether
it is checked in belongs to the repository.

**An MCP entry that is already there is kept, whatever it points at**, and said
twice — a daemon log line, and a notice fed into the session's terminal, since
the log is on the machine and the person is in a browser. Feed it into the
stream, never write it to the pty: writing is typing at the agent. The cost is
that a relaunch keeps the old entry's token rather than the one it just minted;
a workspace token lasts a year, so it works, and the way to move an entry is to
delete it.

**The permissions file allows `mcp__<server>__*`, not a list of tool names.**
A list here would be a sixth copy of the server's catalogue, and a stale copy
stalls an agent on the one call nobody is watching.

Only the **supervisor** workspace gets the second, account-wide `agentrq`
entry, and only the backend decides that — by sending `CoreMCPURL`. The daemon
knows nothing about which workspace is which.

The folder itself is checked in `workspaceDir` before any of this: absolute
only, cleaned, and it must already exist. The pty layer checks it too and still
should — but by the time it does, three files have been written, so a relative
or mistyped path has to be refused here or it becomes a new empty directory
with a token in it.

## A fork runs in a folder the daemon makes, and only a daemon that says so

A fork's folder is `~/.agentrq/forks/<id>`, a worktree when the parent's folder is in a repository and a copy (no `.mcp.json`) otherwise — never the parent's own folder, or its `.mcp.json` entry would connect the fork as the parent. An agentrqd that predates this ignores `StartSession.Fork` and does exactly that, so the backend refuses a fork launch unless the hello listed `wire.CapabilityFork`, and the daemon refuses one whose folder already names the workspace server.

## The terminal must never size itself

The fit addon reads the host element's box and sets the terminal's rows to
match. If that box is content-sized, fitting makes it taller, which makes the
box taller, which fits again — the terminal grows until it has pushed the page
off the bottom of the screen. That is what this page did.

So the host is a flex child with `min-h-0` all the way up, which gives it a
height that does not depend on its content, and the resize handler refuses to
act on a measurement that has not changed. Either alone is enough on a good
day; both is what makes it hard to reintroduce.

xterm's theme is also set explicitly, all sixteen colours. Agent output assumes
a dark background, and leaving the palette to a default that has never seen
this surface is where unreadable output comes from.

The terminal's host element must carry **no padding** — the fit addon measures
its `border-box` height, so padding there is counted but undrawable and the
bottom row gets clipped. Put it on a wrapper.

## `lineHeight` is 1 because a terminal is drawn, not typeset

It reads like a comfort setting, and it is the one value in `TERMINAL_OPTIONS`
that cannot be raised. Above 1 there is space between rows, and a vertical
box-drawing character cannot cross that space to reach the row above — so `│`,
`├`, `└` and `╰` hang unattached and an agent's framed interface renders as
loose fragments rather than a box. It was 1.2 for exactly that reason: 1.2 is
right for prose, and most of what arrives here is *drawn*.

Verified by rendering the same agent output at both values and looking at it,
which is the only way this shows up — nothing throws, nothing is measured
wrongly, and the sizing maths is identical either way.

Six related facts about how this surface renders, worth knowing before
debugging something that looks like a font problem:

- **The WebGL addon draws the box characters, the DOM renderer asks the font.**
  That is why it is loaded: `customGlyphs` does not work with the DOM renderer,
  so without it a file tree's verticals come out as detached bars. It falls back
  to the DOM renderer where WebGL is refused, which fails *inside* `activate()`
  rather than in the constructor.
- **A lost GPU context throws nothing** — the addon simply stops drawing and the
  terminal is blank for good, so `useTerminalRenderer` rebuilds it on
  `onContextLoss`, bounded so a machine that cannot keep a context stops asking.
- **Test a renderer at a real device scale factor**, not an emulated one.
  Playwright's `deviceScaleFactor` (CDP emulation) makes the canvas renderers
  draw several times too large here; `--force-device-scale-factor` with
  `viewport: null` renders correctly. The emulated path is a convincing false
  alarm.
- **Name no font in `fontFamily` that the app does not ship.** One that is
  named and never loaded is used by whoever happens to have it installed and by
  nobody else, which is how a rendering report stops being reproducible.
- **A webfont has to be re-measured, not just re-fitted.** xterm keeps the cell
  it measured at open and re-measures only for a *changed* `fontFamily` or
  `fontSize` — the same value assigned back is dropped — so `fit()` after the
  font lands returns the columns it already had. That is what `remeasureCell`
  is for.
- **Google's subsets stop short of box drawing (U+2500-257F)**, so those glyphs
  still come from the system fallback. Shipping a font fixes the cell, not the
  box characters.

## ...and a fit that did nothing looks exactly like one that worked

The opposite hazard: `FitAddon.fit()` is a **no-op that reports nothing** until
the renderer has measured a character cell. It asks `proposeDimensions()`
first, which answers `undefined` while `dimensions.css.cell.width === 0`, and
returns. Read `term.cols` back afterwards and you get 80×24, xterm's default,
with nothing to say it is not a measurement.

`useTerminalFit` therefore treats a fit as an attempt that can fail: it asks
`proposeDimensions()` *before* fitting, because that is the only thing that
distinguishes "not ready" from a real size, and retries on the following frames
instead of recording a default as a measurement. Three things about it are
decisions rather than details:

- **The retry is bounded** (`FIT_ATTEMPTS`). A terminal in a panel nobody has
  opened measures zero every frame, and spinning a render-frame loop forever
  to discover that is worse than waiting — when it is shown, the observer
  fires.
- **The opening attempt is synchronous**, so the first size handed to the
  session is a measurement rather than the default.
- **Both rules above survive it.** A size that has not changed is still never
  reported, or the fit → taller box → fit loop comes straight back.

**Read this part before you cite the above as the cause of anything.** The
no-op path is quoted from the addon's source and is real. It was *not*
demonstrated to happen here: measured in headless Chromium against this app's
own `TERMINAL_OPTIONS`, `proposeDimensions()` answered a real size
synchronously in the same tick as `open()`, every time — with a normal box,
with a zero-height box, and with `xterm.css` applied late. The ordering change
is likewise insurance rather than a traced fix, because `sendResize` keeps the
last size in `pendingSize` and re-sends it when the socket opens, which is
always more than a frame after mount.

So the terminal-sizing path is now hardened against a documented hazard, and
the report it was written for — *text mis-shaped on first open, better after
the sidebar is collapsed* — **is not explained by it**. The shape of the real
cause is a measurement that is wrong but plausible, since every guard here
passes such a number straight through. Do not let this section persuade the
next person that the question is closed.

## A launch ends at the terminal, from either end

Both launch sites — the workspace's `StartAgentPanel` and the machine page's
own form — navigate to `/sessions/<id>` as soon as the daemon has been asked,
and that navigation is the feature rather than a convenience.

An agent's first minute is when it asks the questions that stop it dead: trust
this folder, allow this tool, paste a key. A pseudo-terminal is the only place
those appear — no event, no toast and no session row carries them — so a launch
that leaves somebody on the page they launched from shows them a row saying
`starting` while the agent waits for an answer to a question nobody can see it
asking. The machine page did exactly that until it didn't.

The path is `terminalPath` in `useTerminalView`, and it is shared rather than
written out at each call site so the two endings cannot drift. It answers `''`
for a session with no id, and the callers stay put on that: `/sessions/undefined`
is a route that resolves, finds no session and reports that the agent has
ended — a lie, and a worse outcome than not moving.

**No kind is excluded, and that is the rule rather than an oversight.**
`isWatchable` in `useWorkspaceTerminal` listed `claude-code` alone until
somebody hit the case it misses. The argument for leaving the gateway out was
that it is already driveable from the page — its turns arrive in the task
composer, with a Stop button, because it speaks ACP a turn at a time. That
holds for the gateway's *turns* and for nothing else. The gateway is also a
process in a pseudo-terminal, and a process asks its own questions on the way
up: `install this version (y/n)`, trust this folder, paste a key. None of those
are ACP, none of them reach the composer, and the agent is stopped until one is
answered. So the question the code asks is "is there a session", not "which
kind" — which also means the next kind the daemon learns to run is reachable
because it is a terminal, rather than because somebody remembered a list.

One creation path deliberately does *not* navigate: the WebMCP `launchAgent`
tool. An agent calling a tool must not move the person's browser out from under
them, and the workspace page grows its "Agent terminal" button on its own when
the session appears.

## A session is named by its workspace, and the name is filled in one place

A machine runs agents for several workspaces at once and most of them are the
same kind, so a session identified only by its kind is a row nobody can read —
three lines saying "claude-code" answer none of the questions somebody opened
the page with. `SessionView` therefore carries `WorkspaceName` beside the id,
and `nameWorkspaces` in the session controller fills it for every endpoint that
returns one. Add another and call it, or the page silently loses the name with
no error anywhere.

It is one query for the whole set, not one per row, and it is deliberately
best-effort: a lookup that fails leaves the sessions unnamed rather than
turning "what is running here" into an error page. The interface falls back to
the kind, which is what it showed before.

## Sessions are operational state, not history

A session row answers "what is running on this machine". When one finishes the
row is deleted — by the state report, by the reconcile on a daemon's hello, and
once at startup for rows written before that was true. The record of what
happened is the audit log; the rows are not it, and keeping them turns the
machine page into a list of everything that has ever run and the table into one
that grows forever.

The consequence to keep in mind: a failure is visible in the moment, over the
event stream, and not afterwards. If that needs to change, add a retention
window rather than keeping every row indefinitely.

## `ws: true` is what makes the terminal work in dev

`vite.config.js` proxies `/api` to the backend, and a proxy without `ws: true`
does not proxy upgrades — so every ordinary API call works and the terminal
socket is never proxied at all. The browser sits on "connecting" forever with
no error, because nothing ever answers the handshake.

## "Does this workspace already have an agent?" is two questions

The launch gate checks both, and needs both. `IsAgentConnected` answers "is an
agent talking to this workspace right now" — it says nothing about one that has
been started and has not finished connecting, and that window is seconds long,
easily enough to press the button twice and end up with two agents sharing one
`.mcp.json` and racing for the same tasks. `ActiveSessionForWorkspace` answers
that half from the database, and survives a backend restart into the bargain.
It was written for this and went uncalled until the interface gained a button.

The interface pre-empts what it can — `useAgentLaunch` works out every reason a
launch would be refused *before* anything is sent, because a form that fired
and reported whichever of the five refusals it hit would make somebody press
the button to find out whether they could press the button. It is never the
authority: two people can press at once, and only the server sees both.

**Every launch gate lives in `controller/agentlaunch`**, which the REST route
and the supervisor's `launchAgent`/`stopAgent` both call. A gate added to the
handler instead is one the supervisor walks straight past.

The plan, with the decisions and who made them, is `docs/AGENTRQD_PLAN.md`.

