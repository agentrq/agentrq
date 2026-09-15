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
