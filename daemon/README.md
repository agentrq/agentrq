# agentrqd

The AgentRQ machine daemon. It runs on a user's own machine, connects outbound
to the backend, and supervises agent processes (`claude-code`, `acp-gateway`)
inside pseudo-terminals it owns — so the control panel can start an agent, kill
it, and type into its terminal.

The design lives in [`docs/AGENTRQD_PLAN.md`](../docs/AGENTRQD_PLAN.md). Read
§2 (the trust model) before anything else: **this is remote code execution on
the user's machine by design**, and the plan says what that does and does not
bound.

## Status

Connected. A machine enrols, `agentrqd serve` holds a socket to the backend,
and an agent launched from the control panel runs in a pseudo-terminal on that
machine with its keystrokes and output travelling both ways.

| Package | What it is |
|---|---|
| `wire/` | The frame format, and deliberately the only definition of it |
| `internal/pty/` | The platform layer: a process in a pseudo-terminal |
| `internal/config/` | Profiles — an account, its server, its machine id |
| `internal/secret/` | Machine tokens, one 0600 file each |
| `internal/enrol/` | Trading a one-time code for a machine token |
| `internal/guard/` | The checks the daemon makes about itself before it runs |
| `internal/supervisor/` | Starting, killing and writing to sessions |
| `internal/stream/` | The headless screen, coalescing, and the output pump |
| `internal/link/` | The connection: dial, reconnect, route frames |
| `internal/metrics/` | What the machine has left: memory, CPU, disk |
| `internal/update/` | Verifying, swapping and rolling back the binary |
| `internal/restore/` | The note that survives the daemon replacing itself |
| `internal/localstatus/` | What is running here, for the person at the keyboard |
| `cmd/agentrqd/` | The CLI: enroll, serve, status, disable, rollback, version |
| `cmd/agentrqd-release/` | Signing and verifying a release |

## Running it

```sh
agentrqd serve            # every enrolled profile
agentrqd serve --profile work --verbose
```

It connects each profile and stays connected, retrying with a jittered backoff
that starts at half a second and caps at a minute. Two things it deliberately
does not do:

- **It does not exit when the backend goes away.** A daemon that stopped when
  the server restarted would need somebody to walk over to that machine, which
  is the thing this exists to avoid.
- **It does not kill agents when the connection drops.** The pumps stop, the
  sessions keep running, and the next connection's `hello` tells the backend
  what is still alive.

One profile that cannot start — an unreadable token, an unusable server URL —
is skipped with an error rather than taken as a reason to refuse the others. A
machine enrolled with two accounts should still serve the one that still works.

## Updating itself

The daemon never updates on its own initiative. It reads the release feed,
reports anything newer, and waits. An approval from the panel means *"kill
every session on this machine and restart them"*, and only a person can mean
that.

```
verify → download → test → *write the note* → kill → swap → restart
```

Everything before the note is reversible; everything after it is not. Four
things hold this together:

- **A build with no release key cannot update itself**, and says so. A
  verification step that silently passes when it has nothing to verify against
  is worse than no verification, because it looks like one. The key is set at
  build time with `-ldflags "-X …update.ReleaseKey=<hex>"`.
- **The manifest is signed as a whole** — version and platform table included,
  because those decide *which* file gets run — and every artefact carries a
  mandatory SHA-256 that is checked while downloading. There is no path to an
  artefact that skips the signature check.
- **The new binary is run before anything is replaced.** Once the swap has
  happened and the old process has gone, nothing can observe the new one
  failing, so the check has to happen while not installing it is still an
  option. That is what makes "never auto-update on a failed start"
  enforceable rather than aspirational.
- **The previous binary is retained** as `agentrqd.old` and is *not* cleaned up
  at startup — that would delete the only thing a rollback can roll back to, at
  exactly the moment the new build has proved least. It is cleared at the start
  of the next update instead, which is also when Windows will finally let go of
  it.

Replacing a running binary is two mechanisms. Unix renames over it and the
running process keeps its inode. Windows cannot delete or overwrite a running
`.exe` **but can rename one**, so the running binary goes aside first — there
is a test that holds the file open and does exactly that, including asserting
that deleting it still fails.

Who restarts depends on how it was installed: under systemd or launchd it exits
and the service manager starts the replacement, otherwise it re-executes
itself. Which one is read from the environment the supervisor sets, and an
uncertain answer is the one that leaves a daemon running.

## What "restore the sessions" means

**Intent, not state.** What comes back are new processes with new
pseudo-terminals: same kind, same folder, same arguments. The scrollback is
gone, whatever the agent was part-way through is gone, and anything half-typed
is gone. Every restored session is marked restored, in the report and therefore
in the panel, so nobody is left wondering why their terminal is empty.

The note is written to disk **before** anything is killed, because the process
holding it in memory is the process about to be replaced. It carries no
credential: the MCP URL has the token inside it, and writing that to disk to
survive a restart would turn a deliberate expiry into a file. A restored agent
reads the `.mcp.json` that was already in its folder; if that has gone, the
session fails with a reason rather than starting an agent that cannot reach its
workspace and merely looks broken.

## Releasing

Six targets from one runner: no cgo, so there is nothing that can only be built
on the machine it runs on. `.goreleaser.yaml` packages the archives;
`.github/workflows/daemon-release.yml` also publishes the **bare binaries and a
signed manifest**, because the self-update path downloads one file and replaces
one file rather than unpacking a tarball.

```sh
go run ./cmd/agentrqd-release keygen     # once, ever
go run ./cmd/agentrqd-release manifest --version 0.7.1 --base-url https://… update/
go run ./cmd/agentrqd-release verify   --version 0.7.1 update/
```

The private key is read from `AGENTRQD_RELEASE_KEY` and never from a flag: an
argument is visible in `ps` to every user on the build machine, and a CI log
that echoes its own command line would print it. The public half is built in
with `-ldflags`.

Three things the release job does that are worth keeping:

- **It refuses to start without both halves of the key.** A release without one
  produces daemons that can never update themselves, and that failure is silent
  until somebody presses the button months later.
- **It verifies the manifest against the public key that was compiled into the
  binaries** before publishing. A manifest signed with the wrong key is a
  release every daemon correctly refuses; catching it here costs a minute rather
  than a support thread.
- **The update binaries are built into `update/`, not `dist/`.** `goreleaser
  release --clean` empties `dist/`, which would delete them before they were
  published.

`.github/workflows/daemon.yml` rehearses the whole signing and verifying path on
every change with a throwaway key, so on release day the only new thing is the
key itself.

Install paths — the systemd **user** unit and the macOS **LaunchAgent** — are in
`packaging/`, and both are user-level on purpose: the daemon refuses to run as
root, and an install that works around that has removed the only thing limiting
an agent to what you can do. `docs/DAEMON.md` is the user-facing document and
says so in those words.

## What the heartbeat says, and what it deliberately does not

Enough to answer "can this box take another agent?" without opening a terminal
on it. Four things in it are easy to get wrong, and each has a test that fails
if it regresses:

- **CPU is a rate, not a reading.** It is measured over a window by a sampling
  loop; the heartbeat reads the last computed value. A heartbeat that waited on
  a measurement would stop being a heartbeat at exactly the moment it was most
  informative.
- **Memory is `available`, not `free`.** On Linux `free` excludes the page cache
  and reads alarmingly low on a perfectly healthy machine. The useful number is
  what a new process could actually get.
- **Disk is per mount.** One free-space figure is a lie on any machine with more
  than one filesystem: it can be 2% full and still fail to check out a
  repository. The mount each workspace sits on is included whatever kind of
  filesystem it turns out to be.
- **`loadAvg` is absent on Windows, not zero.** Three zeroes render as a
  perfectly idle machine, which is the most misleading thing this payload could
  say about a box that has no load average at all.

Nothing is sent until there is a measurement to send. An empty heartbeat would
land on the machine's row as zero memory and an idle CPU, which is a confident
lie where "we have not heard yet" is the truth. Liveness does not depend on it —
that is the connection's own ping and pong.

## Enrolling

```sh
# The control panel: Machines → Add machine gives you a short code.
agentrqd enroll --server https://app.agentrq.com --code ABCD-1234 --profile work
agentrqd status
agentrqd disable --profile work
```

Enrolment is a **local act**. Someone reads a code off the control panel and
types it at a terminal on the machine being enrolled — there is no remote
enrolment, and nothing the backend can say will cause a machine to enrol itself.

A code rather than an OAuth2 browser flow because **the obvious place to run
this is a headless build box**, and a flow needing a browser on the machine
being enrolled does not work where it is most wanted.

### TLS, and the one thing `--insecure` does not mean

TLS is required and verified unless the server is loopback. There is no network
to intercept on `localhost`, and demanding a certificate there would only push
people towards a blanket skip-verification flag — the worst of both worlds.

`--insecure` permits **plain HTTP to a remote host** and nothing else. It never
disables certificate verification for an `https` URL: *"there is no
certificate"* and *"the certificate is wrong"* are different problems, and only
one of them is ever deliberate.

A profile enrolled that way is marked, and says so on **every** `status` and
every start — not once at enrolment. A security decision that becomes invisible
after first boot has stopped being a decision.

## Where the token lives

In a `0600` file, one per profile, under a `0700` directory. The directory
permissions are not belt-and-braces with the file ones: they are what stops
another user *listing which profiles exist*, which is information on its own.

The plan says "OS keychain where there is one, else a file". The file store is
here and the keychain is not, and that order is deliberate: **a headless Linux
box has no keyring daemon**, so a keychain implementation would fall back to a
file on exactly the machines this is aimed at, while adding a dependency that
fails in ways that are miserable to debug over SSH. [`secret.Store`] is the
interface a keychain-backed store can be dropped into when there is a desktop
case that wants one.

## `wire` is imported by the backend

This is the one package here that is **not** `internal`, and that is the whole
point. The relay and the daemon are separate modules, so each could easily grow
"its own" copy of the frame format — and protocol drift between them is the
worst bug available in this design, because a daemon in the field is not
upgradable on demand. Old daemons talking to new backends is the normal case.

For that to keep working, `wire` must import nothing from `internal/` and
nothing outside the standard library. **Adding a dependency to `wire` is how the
backend stops being able to depend on it.**

## Why `go-pty` rather than `creack/pty`

`creack/pty` is the mature Unix answer and does not cover Windows at all.
Windows needs ConPTY (`CreatePseudoConsole`, Windows 10 1809+), has no
`SIGWINCH`, and kills processes differently. `github.com/aymanbagabas/go-pty`
presents one interface over both, which is why the platform layer needs no build
tags.

## Running the tests

```sh
cd daemon && go test ./...
```

The pseudo-terminal tests use a **real** PTY and re-exec the test binary as the
child process — it is the one program that exists and behaves the same on all
three operating systems. A mocked PTY would prove nothing about ConPTY, which is
the only part anyone has reason to doubt.

CI therefore runs this suite on **ubuntu, macOS and windows** (see
`.github/workflows/daemon.yml`), and cross-compiles all six release targets from
one runner, since the daemon uses no cgo.

## Enter is CR, and the platforms disagree about it afterwards

The first thing the three-platform suite caught, and it is worth writing down
because it passes on two platforms out of three.

**Pressing Enter sends carriage return (0x0d), not line feed.** That is what a
real terminal transmits, and what `xterm.js`'s `onData` will hand the browser
side. A test that writes `"\n"` instead passes on Linux and macOS — their line
discipline accepts NL as a line ending — and **hangs on Windows**: the ConPTY
echoes the characters back but never treats the line as submitted, so the child
sits in its read until the test times out.

Then the mirror image, one layer down. The line discipline rewrites whichever
terminator it does not use: Unix maps the CR that Enter sends into NL *before
the child sees it*. So a child waiting for a specific byte waits forever on the
other platform. Read until either.

The daemon itself is correct in both directions — it is a transparent byte pipe
and translates nothing, which is the whole design. The bug was in code that had
quietly encoded one platform's convention. That is the class of thing this suite
exists to find, and it would have surfaced as "the Enter key does nothing on
Windows" months later otherwise.

## Esc, the Windows line editor, and why raw mode is the case that matters

The second Windows finding, and I got the diagnosis wrong once before getting
it right — the wrong version is recorded here because it is the more tempting
explanation.

A test that sent Esc and then Enter to a child reading a **line** failed on
Windows: the child reported a complete line containing **zero bytes**. My first
reading was that ConPTY's VT parser had seen `0x1b`, begun an escape sequence,
and swallowed the CR with it. That is wrong, and the evidence says so plainly —
**the child received a line**. If the parser had eaten both bytes there would
have been no line at all.

The CR was delivered and processed normally. Only the Esc vanished before it,
which points at the **Windows console line editor**, not the VT parser: in
cooked mode the console implements line editing, and **Esc means "clear the
current input line"**. It cleared an empty buffer; the CR submitted the empty
result. Unix canonical mode has no such key — Esc is just another byte in the
buffer — which is why two platforms out of three passed.

**So the test was asking the wrong question.** A real agent is a TUI and puts
the terminal in **raw mode**, where there is no line editor and every byte is
delivered as itself. That is the case worth testing, and it now is:
`TestEscapeByteReachesTheProcessIntact` puts the child in raw mode with
`golang.org/x/term`, writes a single `0x1b` with no terminator, and asserts the
child saw exactly that byte.

Worth carrying forward: **if an agent ever runs in cooked mode on Windows, Esc
will be eaten before it arrives**, and nothing in the daemon can fix that.

## Input is forwarded immediately, never batched

Output is batched on a timer (plan §10) because a progress bar redraws a hundred
times a second. **Input is the opposite**, for two reasons that hold regardless
of the above:

- **Latency.** 16–33ms of batching is invisible on output and *felt* on every
  keypress.
- **The Esc/Alt ambiguity is real in terminal applications**, even though it was
  not what broke the test above. TUIs distinguish a lone Escape from `ESC`-plus-key
  by timing — it is what vim's `ttimeoutlen` exists for. Batching input would
  merge a deliberate Esc with the next keystroke and hand the application
  something that reads as Alt+key.

## What ConPTY sends before anything happens

A Windows session opens with a burst of console setup that Unix does not send:

```
\x1b[?9001h   win32-input mode on      \x1b[2J     clear screen
\x1b[?1004h   focus reporting on       \x1b[H      cursor home
\x1b[?25l     hide cursor              OSC 0;…BEL  set window title
```

Nothing here needs handling at this layer — they are bytes like any others — but
the screen-state work in M3 will consume them, and anything that assumes a
session's first output is program output will be wrong on Windows.

## What the tests do not cover

`internal/pty` sits around 87%, and the gap is syscall failure paths — a
pseudo-terminal that cannot be opened, a resize the kernel refuses. Forcing
those portably means mocking the platform, at which point the test is checking
the mock rather than the code. The parts that carry meaning — that arbitrary
bytes survive, that a missing working directory is reported with its path, that
`Close` is idempotent and actually kills — are covered. `wire` is at 100%
because it is pure.
