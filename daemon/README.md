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

Milestone **M0** — the spike. Two packages exist and nothing is wired together
yet.

| Package | What it is |
|---|---|
| `wire/` | The frame format, and deliberately the only definition of it |
| `internal/pty/` | The platform layer: a process in a pseudo-terminal |

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
