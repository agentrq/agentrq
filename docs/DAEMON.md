# Running agents on your own machines

`agentrqd` is a small program you install on a computer so that AgentRQ can run
coding agents on it. You enrol the machine once, and after that you can start an
agent for a workspace from the control panel, watch its terminal, and type into
it.

This document is about what that means, what it can and cannot protect you
from, and how to turn it off.

---

## Read this before you enrol a machine

> **Enrolling a machine lets anyone who can authenticate as that AgentRQ
> account run commands on it as the user who started the daemon. Compromise of
> the account is compromise of every enrolled machine.**

That is the whole trust model in one sentence, and it is not a caveat. An agent
started on your machine can read every file you can read, write every file you
can write, reach every network your machine can reach, and use every credential
sitting in your home directory — because it *is* you, as far as the operating
system is concerned.

So:

- Enrol machines you would be comfortable giving that account a shell on.
- Protect the AgentRQ account accordingly. Your machines are only as safe as it
  is.
- Do not enrol a machine that holds something the account's owner should not
  have.

The daemon refuses to run as root or Administrator, which limits the damage an
agent can do to what *you* can do. That is a meaningful limit and it is not
isolation.

---

## A profile is a credential boundary, not an execution boundary

One machine can be enrolled with several AgentRQ accounts. Each is a "profile",
with its own token and its own machine identity, and the panel shows them
separately.

**They are not isolated from each other.** All of them run as the same
operating-system user, on the same filesystem, in the same home directory.
Account A's agent can read the files account B's agent is working on. It can
read account B's tokens if they are on disk. Nothing in the daemon prevents
that, and nothing in the daemon could: they are the same user.

This is worth saying plainly because the desktop app's profiles *do* isolate —
separate cookie jars, separate sessions — and it would be reasonable to assume
these work the same way. They do not.

**If two accounts need real isolation, that is two machines, or two operating
system user accounts with their own home directories and their own copies of the
daemon.** Anything else is a convention, not a boundary.

---

## What it can do, exactly

The daemon runs exactly two programs, and nothing else:

- `claude-code`
- `acp-gateway`

It will not run a shell, and it will not run a command the server sends it. The
control panel asks for a *kind* — one of those two names — and the daemon
decides what that means from its own configuration. A backend that has been
taken over cannot ask this machine to run `/bin/sh`, because there is no
message that says "run this".

What it *can* do, once one of those two is running, is send it keystrokes. That
is the feature. An agent at a terminal can do whatever you could do at that
terminal.

---

## Turning it off

```sh
agentrqd disable --profile <id>
```

This deletes that profile's token from the machine. **It works without the
server's cooperation and takes effect immediately**: the machine can no longer
authenticate, so no future connection succeeds, whatever the server thinks. It
is the thing to reach for if you believe the account has been compromised.

Also remove the machine in the control panel, which revokes it server-side and
closes any connection it currently holds. The two are independent on purpose —
either one alone is enough to stop that machine working, and you do not need
access to the panel to do the local one.

Stopping the daemon (`systemctl --user stop agentrqd`, or closing the terminal
it runs in) stops new work but leaves the credential on disk.

---

## Seeing what is happening on your own machine

```sh
agentrqd status
```

reports what is running **right now**: each agent, the folder it is working in,
which profile asked for it, and — the part that matters most — whether anybody
is attached to its terminal at this moment.

That question is answerable from the machine, by the person sitting at it,
without asking the account that might be doing the watching. A machine whose
owner cannot find out what is driving it is not a machine anyone should enrol.

The daemon also writes a log line when a viewer attaches to a terminal on this
machine and when they leave. Under systemd: `journalctl --user -u agentrqd -f`.

---

## The audit trail, and what is deliberately not in it

Recorded, on the server, for every machine:

- an agent being started, and by whom
- an agent being killed
- a browser **attaching** to a terminal, and detaching
- an update being approved, and to which version

**Keystrokes are not recorded.** Not on the server, not on the machine, not
anywhere. What somebody types into a terminal is passwords, tokens, and the
contents of files; recording it would build the most sensitive log in the
system to answer a question nobody asked. That an attach happened, and when, is
the fact worth keeping.

Terminal *output* is not stored either. It is relayed to whoever is watching
and forgotten.

---

## Updates

The daemon checks for new releases and tells the control panel when there is
one. **It never updates itself on its own initiative.** Somebody has to approve
it, because approving means:

> stop every agent running on this machine, replace the daemon, and start them
> again

Those agents come back as **new terminals**. Same agent, same folder, same
settings — and no scrollback, no in-flight work, nothing half-typed. The panel
marks them as restored so it is clear why the terminal is empty. "Restore the
sessions" means restoring the *intent*, not the state, and there is no version
of this that could mean otherwise.

Before anything is replaced, the daemon:

1. checks the release manifest's signature against a key built into it,
2. checks the downloaded file against the checksum in that manifest,
3. runs the new binary and confirms it works on this machine,
4. writes down what was running, *then* stops it.

A build with no release key **refuses to update itself** and says so; update it
by hand. If an update turns out to be bad, `agentrqd rollback` puts the previous
binary back — it is kept for exactly that reason.

---

## Where things are

| What | Where |
|---|---|
| Config | `~/.config/agentrqd/config.json` (`%AppData%\agentrqd` on Windows) |
| Tokens | one `0600` file per profile in `.../agentrqd/tokens/` |
| Local status | `.../agentrqd/status.json`, what `agentrqd status` reads |

The config file is **not** a credential and can safely be copied, backed up or
pasted into a bug report. The tokens directory is.

---

## Commands

```sh
agentrqd enroll --server <url> --code <code> [--profile <id>]
agentrqd serve  [--profile <id>] [--verbose]
agentrqd status
agentrqd disable --profile <id>
agentrqd rollback
agentrqd version
```

Installation, including the systemd unit and the macOS LaunchAgent, is in
`INSTALL.md` in the release archive.
