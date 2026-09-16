# Installing agentrqd

`agentrqd` is one static binary. Put it somewhere on your `PATH`, enrol it, and
run it.

**Read [`DAEMON.md`](DAEMON.md) before you enrol a machine.** Enrolling grants
the AgentRQ account the ability to run commands on this computer as you, and
that is not something to find out afterwards.

## 1. Install the binary

**Linux and macOS**

Pick the archive for your platform from the
[releases page](https://github.com/agentrq/agentrq/releases?q=agentrqd&expanded=true),
then:

```sh
# Linux
tar xzf agentrqd_*_linux_*.tar.gz
install -m 0755 agentrqd ~/.local/bin/

# macOS
tar xzf agentrqd_*_darwin_*.tar.gz
sudo install -m 0755 agentrqd /usr/local/bin/
```

**Windows (PowerShell)**

```powershell
Expand-Archive agentrqd_*_windows_*.zip -DestinationPath $env:LOCALAPPDATA\agentrqd

# Windows has no user directory that is already on PATH, so add this one.
# Takes effect in new shells.
[Environment]::SetEnvironmentVariable("Path",
  "$env:Path;$env:LOCALAPPDATA\agentrqd", "User")
```

**Do not install it as root or Administrator.** The daemon refuses to start
that way, because an agent it runs would have those powers too. If an install
guide tells you to use `sudo` for anything other than copying the binary into
`/usr/local/bin`, it is working around that check.

## 2. Enrol

In the control panel: **Machines → Add machine** gives you a short code. On the
machine itself:

```sh
agentrqd enroll --server https://your-agentrq-server --code ABCD-EFGH
```

There is no remote enrolment. Somebody has to be at the machine — that is what
makes the code safe to display.

## 3. Run it

**Foreground**, for trying it out:

```sh
agentrqd serve
```

**Linux (systemd user service)** — the supported way:

```sh
mkdir -p ~/.config/systemd/user
cp agentrqd.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now agentrqd

# So it keeps running when you are not logged in:
sudo loginctl enable-linger "$USER"
```

It is a **user** unit. Do not move it to `/etc/systemd/system` and run it as
root.

**macOS (LaunchAgent)** — the supported way:

```sh
cp com.agentrq.agentrqd.plist ~/Library/LaunchAgents/
launchctl load -w ~/Library/LaunchAgents/com.agentrq.agentrqd.plist
```

A LaunchAgent, not a LaunchDaemon: a LaunchDaemon runs as root, which the
daemon refuses.

Under either service manager the daemon exits and lets the manager restart it
when it updates itself. Run by hand, it re-executes instead. Both work; the
service manager is better, because it knows about restart limits and logs.

## 4. Check on it

```sh
agentrqd status
```

says what is running on this machine right now, and whether anybody is watching
a terminal on it. That question is answerable here, from the machine, without
asking the account that would be doing the watching.

## Stopping it

```sh
agentrqd disable --profile <id>
```

removes the machine's credential from this computer. It takes effect
immediately and does not need the server to agree.
