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
| `cmd/agentrqd/` | The CLI: enroll, serve, status, disable, version |

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
