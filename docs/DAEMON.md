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

## Installing it

`agentrqd` is one static binary. Put it on your `PATH`, enrol it, run it.

### The script

On Linux and macOS:

```sh
curl -fsSL https://agentrq.com/install-agentrqd.sh | sh
```

It works out which archive this machine needs, **verifies it against the
SHA-256 checksums published with the release**, and installs to
`~/.local/bin`, a folder you own, so it never asks for a password. If that
folder is not on your `PATH` yet, it prints the one line that adds it, for the
shell you use. Running it again updates in place. It installs and nothing more: it does not enrol the
machine, start anything, install a service, or run as root.

The checksum is not optional and there is no flag to skip it — a download the
script cannot vouch for is never unpacked, let alone installed. That is the
part worth having: installing by hand, below, verifies nothing at all unless
you do it yourself.

**If you would rather read it first**, which is a fair thing to want on the
machine you are about to grant command access to:

```sh
curl -fsSL https://agentrq.com/install-agentrqd.sh -o install-agentrqd.sh
less install-agentrqd.sh
sh install-agentrqd.sh
```

Same install, and it is about 300 lines of POSIX `sh`.

To pass flags through the one-liner, put them after `-s --`:

```sh
curl -fsSL https://agentrq.com/install-agentrqd.sh | sh -s -- --version 0.7.0
```

`--version` pins a release, `--dir` installs somewhere else, `--force`
reinstalls, and `--help` lists them.

**Windows (PowerShell)**:

```powershell
irm https://agentrq.com/install-agentrqd.ps1 | iex
```

Same trade, same guarantee: it verifies the checksum, installs to
`%LOCALAPPDATA%\Programs`, and adds that folder to your user `PATH` if it
isn't there already — Windows has no directory that is already on `PATH` the
way `~/.local/bin` is. Running it again updates in place, and it does not
enrol, start anything, or require Administrator (it refuses to run elevated).

To read it first:

```powershell
iwr https://agentrq.com/install-agentrqd.ps1 -OutFile install-agentrqd.ps1
notepad install-agentrqd.ps1
.\install-agentrqd.ps1
```

Piped into `iex`, the script has nothing to attach command-line flags to, so
pass them through an environment variable instead:

```powershell
$env:AGENTRQD_VERSION = "0.7.0"
irm https://agentrq.com/install-agentrqd.ps1 | iex
```

`AGENTRQD_VERSION` pins a release, `AGENTRQD_INSTALL_DIR` installs somewhere
else, `AGENTRQD_FORCE` reinstalls. Run the file directly instead of piping it
and the same options are ordinary parameters: `-Version`, `-Dir`, `-Force`,
`-Help`.

### Or by hand

Releases are on the
[releases page](https://github.com/agentrq/agentrq/releases/latest) —
pick the archive for the machine you are
installing on, not the one you are reading this on.

**Linux**

```sh
tar xzf agentrqd_*_linux_*.tar.gz
mkdir -p ~/.local/bin
install -m 0755 agentrqd ~/.local/bin/
```

**macOS**

```sh
tar xzf agentrqd_*_darwin_*.tar.gz
mkdir -p ~/.local/bin
install -m 0755 agentrqd ~/.local/bin/
# put ~/.local/bin on your PATH, for the shell you use; skip if it is already
line='export PATH="$HOME/.local/bin:$PATH"'
case "$(basename "$SHELL")" in
  zsh)  echo "$line" >> "${ZDOTDIR:-$HOME}/.zshrc" ;;
  # a login bash reads only the first of these that exists, so add to that one
  bash) for rc in ~/.bash_profile ~/.bash_login ~/.profile ~/.bash_profile; do
          [ -f "$rc" ] && break
        done
        echo "$line" >> "$rc" ;;
  fish) fish -c 'fish_add_path ~/.local/bin' ;;
  *)    echo "$line" >> ~/.profile ;;
esac
```

**Windows (PowerShell)**

```powershell
$dir = "$env:LOCALAPPDATA\Programs"
New-Item -ItemType Directory -Force $dir | Out-Null
Expand-Archive agentrqd_*_windows_*.zip -DestinationPath .
Move-Item -Force agentrqd.exe "$dir\agentrqd.exe"

# Windows has no user folder already on PATH, so add this one; skip if it is.
# Read the User value: $env:Path is that and the system PATH combined.
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
[Environment]::SetEnvironmentVariable("Path", "$userPath;$dir", "User")
```

Nothing here needs `sudo`.
**Do not run the daemon itself as root or Administrator** — it refuses, because
an agent it starts would inherit those powers. If an install guide anywhere
tells you to work around that refusal, it is removing the only thing keeping an
agent to what you can already do yourself.

Nothing here verifies what you downloaded. If you install by hand, check it
against `checksums.txt` on the release page yourself:

```sh
sha256sum -c checksums.txt --ignore-missing
```

That is the one thing the script does that a manual install usually skips,
and the reason it is worth preferring.

### Enrol it

In the control panel: **Machines → Add machine** gives you a short code, and
shows these same steps. On the machine itself:

```sh
agentrqd enroll --server https://your-agentrq-server --code ABCD-EFGH
```

There is no remote enrolment. Somebody has to be at the machine, which is what
makes the code safe to display.

### Run it

```sh
agentrqd serve
```

To keep it running after you log out, use the service files. The installer
saves them beside the daemon's own config — `~/.config/agentrqd` on Linux,
`~/Library/Application Support/agentrqd` on macOS — and a manual install
leaves them in the archive you unpacked:

- **Linux** — copy `agentrqd.service` into `~/.config/systemd/user/`, then
  `systemctl --user daemon-reload && systemctl --user enable --now agentrqd`.
  Add `sudo loginctl enable-linger "$USER"` so it survives logout.
- **macOS** — copy `com.agentrq.agentrqd.plist` into `~/Library/LaunchAgents/`,
  then `launchctl load -w ~/Library/LaunchAgents/com.agentrq.agentrqd.plist`.
  It runs `~/.local/bin/agentrqd` as shipped; the script's copy names wherever
  `--dir` put it instead.

Both are **user**-level — a systemd user unit and a LaunchAgent, not a system
service and not a LaunchDaemon — for the reason above. Under either, the daemon
exits with status 75 and lets the service manager start it again when it is
restarted or updated from the panel; run by hand, it re-executes. Both work.

`INSTALL.md` in the archive is the same instructions, for when you have the
archive and not this page.

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

The daemon runs exactly two agents, and nothing else:

- `claude-code`
- `acp-gateway`

Either can be started on a model you name when you launch it — for Claude Code
an alias such as `opus` or `sonnet`, or a full model id; leave it blank for the
agent's own default. Claude Code also takes an effort level (`low`, `medium`,
`high`, `xhigh` or `max`). Choosing Claude Code's model or effort needs
agentrqd 0.9.15 or newer; an older one is refused rather than quietly starting
the defaults.

It will not run a shell, and it will not run a command the server sends it. The
control panel asks for a *kind* — one of those two names — and the daemon
decides what that means from its own configuration. A backend that has been
taken over cannot ask this machine to run `/bin/sh`, because there is no
message that says "run this".

What it *can* do, once one of those two is running, is send it keystrokes. That
is the feature. An agent at a terminal can do whatever you could do at that
terminal.

The one other program it starts is `git`, and only to make a workspace fork's
folder: `git worktree add` with arguments it builds itself, never a shell.

### A workspace fork gets a folder of its own

A fork runs in `~/.agentrq/forks/<fork id>`, made from its parent's working
directory on the fork's first launch and reused after that:

- **Parent folder inside a git repository** → a worktree of that repository on
  a new branch, `agentrq/fork-<fork id>`, checked out at `HEAD`. The fork starts
  in the same subfolder its parent does. Uncommitted changes in the parent are
  not in it.
- **Anywhere else** → a copy of the folder, without its `.mcp.json` (which
  points at the parent) and without `.agentrq/`.

Merging a fork back moves its tasks, never its files: the folder and the
branch stay where they are, and the fork's working directory shows the path.
A repository that commits a `.mcp.json` with an `agentrq-workspace` entry
cannot be forked this way — the launch is refused, naming the file — because
that entry would connect the fork's agent as the parent. A machine running an
agentrqd from before forks is refused too: update it first.

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
- a restart being asked for

**Keystrokes are not recorded.** Not on the server, not on the machine, not
anywhere. What somebody types into a terminal is passwords, tokens, and the
contents of files; recording it would build the most sensitive log in the
system to answer a question nobody asked. That an attach happened, and when, is
the fact worth keeping.

Terminal *output* is not stored either. It is relayed to whoever is watching
and forgotten.

---

## Restarts and updates

A machine running agentrqd **0.9.3 or newer** can be restarted from its page
in the control panel. It is one command: once the daemon has found a newer
release, the button reads **Update daemon** and installs that release on the
way; otherwise it reads **Restart daemon**. Older daemons have to be updated by
hand once; after that, the panel can do it.

The daemon checks for new releases and tells the control panel when there is
one. **It never updates itself on its own initiative.** Somebody has to approve
it, because approving means:

> stop every agent running on this machine, replace the daemon, and start them
> again

A restart is the same without replacing the daemon.

Those agents come back as **the same sessions in new terminals**: same agent,
same folder, same settings, still listed on the machine's page. A claude-code
agent resumes its conversation where it stopped. An ACP Gateway agent starts
fresh. Either way the scrollback and anything half-typed are gone, and the
panel marks the sessions as restored so it is clear why the terminal starts
over. Like every launch, the new terminal opens with the command the agent was
started with.

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
| Workspace forks | `~/.agentrq/forks/<fork id>`, one folder per fork |

The config file is **not** a credential and can safely be copied, backed up or
pasted into a bug report. The tokens directory is.

---

## Commands

```sh
agentrqd enroll --server <url> --code <code> [--profile <id>]
agentrqd serve  [--profile <id>] [--verbose]
                [--max-per-profile <n>] [--max-per-machine <n>]
agentrqd status
agentrqd disable --profile <id>
agentrqd rollback
agentrqd version
```

`--max-per-profile` and `--max-per-machine` bound how many agents may run here
at once, and default to 8 and 16. Pass `0` for no limit. A refused launch says
why in the panel and in this daemon's log.

Installation is at the top of this page; the archive carries the same steps as
`INSTALL.md`, plus the systemd unit and the macOS LaunchAgent themselves.
