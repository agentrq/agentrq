# Installing agentrqd

`agentrqd` is one static binary. Put it somewhere on your `PATH`, enrol it, and
run it.

**Read [`DAEMON.md`](DAEMON.md) before you enrol a machine.** Enrolling grants
the AgentRQ account the ability to run commands on this computer as you, and
that is not something to find out afterwards.

## 1. Install the binary

You already have the archive, so the steps below are the shortest path from
here. For the *next* machine — or for updating this one — there is a script
that picks the right build and checks it against the published checksums,
which these steps do not do:

```sh
# Linux, macOS
curl -fsSL https://agentrq.com/install-agentrqd.sh | sh
```

```powershell
# Windows
irm https://agentrq.com/install-agentrqd.ps1 | iex
```

**Linux and macOS**

Pick the archive for your platform from the
[releases page](https://github.com/agentrq/agentrq/releases/latest),
then:

```sh
# Linux
tar xzf agentrqd_*_linux_*.tar.gz
mkdir -p ~/.local/bin
install -m 0755 agentrqd ~/.local/bin/

# macOS
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

**Do not install it as root or Administrator.** The daemon refuses to start
that way, because an agent it runs would have those powers too. If an install
guide tells you to use `sudo` for any of this, it is working around that check.

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
