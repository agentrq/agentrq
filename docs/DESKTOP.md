# AgentRQ Desktop

The desktop app is the AgentRQ web interface in a native shell. It is the same
application — the same views, the same shortcuts, the same design — plus the
things a browser tab cannot do: notifications that arrive while the window is in
the background, a menu-bar item, links that open the app at the right place, and
updates that install themselves.

It is a **client**. Your AgentRQ server keeps running wherever you run it —
localhost, a machine on your network, or a hosted instance — and the app
connects to it.

---

## Installing

Download the build for your machine from the
[latest release](https://github.com/agentrq/agentrq/releases/latest).

| Platform | File | Notes |
|---|---|---|
| macOS (Apple silicon) | `AgentRQ-<version>-arm64.dmg` | |
| macOS (Intel) | `AgentRQ-<version>-x64.dmg` | |
| Windows | `AgentRQ Setup <version>.exe` | x64 and arm64 |
| Linux (AppImage) | `AgentRQ-<version>.AppImage` | `chmod +x` then run |
| Linux (Debian/Ubuntu) | `agentrq_<version>_amd64.deb` | `sudo dpkg -i` |

Linux ships both x64 and arm64, so a Raspberry Pi or an ARM cloud instance works
as well as an ordinary desktop.

### The first-launch security warning

Until the project has code-signing certificates, the builds are **unsigned** and
your operating system will say so. This is expected, and it is worth knowing
exactly what it means rather than clicking through blind:

- **macOS**: "AgentRQ cannot be opened because the developer cannot be
  verified." Right-click the app → **Open** → **Open**. You only do this once.
- **Windows**: SmartScreen shows "Windows protected your PC." **More info** →
  **Run anyway**.
- **Linux**: no warning.

The practical consequence is on macOS, and it is not cosmetic: **an unsigned
macOS build cannot update itself.** macOS validates an application's signature
before replacing it, so the app can check for updates and tell you one exists,
but cannot install it — you download the new version manually. Windows and Linux
update normally. See [Auto-update](#auto-update).

---

## Connecting to a server

On first launch the app asks for your AgentRQ server URL, defaulting to
`http://localhost:3000`.

The address is checked before it is saved, so a typo is caught immediately
instead of turning into a mysterious failure to sign in later. Then you sign in
exactly as you would in a browser: root token, Google, or GitHub.

To point at a different server afterwards, use **Switch Server** in the
application menu (macOS: `AgentRQ` menu; Windows and Linux: `File`).
**Log Out** clears the session without forgetting the server.

The choice is stored in `agentrq-desktop.json` under the app's data directory:

| Platform | Location |
|---|---|
| macOS | `~/Library/Application Support/agentrq-desktop/` |
| Windows | `%APPDATA%\agentrq-desktop\` |
| Linux | `~/.config/agentrq-desktop/` |

---

## What the desktop adds

**Notifications.** Native notifications when an agent creates a task, changes a
task's status, or replies. Clicking one opens the app at that task. The unread
count appears on the dock icon or the taskbar. Mute them per workspace in
**Workspace Settings → Notifications → Desktop Alerts**.

Notifications only fire for things *agents* do — you are not told about your own
clicks.

**Tray / menu-bar item.** Unread count, your most recently active workspaces, and
a shortcut to create a task.

**Global shortcut.** `Cmd/Ctrl+Shift+N` creates a task from anywhere, even when
the app is not focused.

**Deep links.** `agentrq://` URLs open the app at a specific place:

```
agentrq://workspaces/<workspaceId>
agentrq://workspaces/<workspaceId>/tasks/<taskId>
agentrq://events
agentrq://workflows
```

Useful in a Slack message, a calendar invite, or a script.

**Window state.** Position and size are remembered between launches — and
checked against the displays you currently have, so the window never reopens on
a monitor you have unplugged.

**Theme.** Follows your AgentRQ theme setting, not your operating system's. If
you choose light inside AgentRQ, the window chrome is light even on a dark
system.

---

## Auto-update

The app checks for updates when it starts and every six hours after that, and
downloads them quietly in the background. When one is ready you get the same
banner the web app shows for a new version, with an **Update now** button.
Ignore it and the update installs the next time you quit.

**Check for Updates…** in the application menu asks immediately and tells you
what it found. A background check that finds nothing stays silent.

> **macOS needs a signed build.** An unsigned build reports *"This build is not
> signed, so it cannot update itself"* — download the new version manually
> instead. Windows and Linux are unaffected.

---

## Troubleshooting

**"Could not reach &lt;url&gt;" on the connection screen.**
The server is not answering at that address. Check it is running and that the
port is right — the default is 3000. If the server is on another machine,
confirm nothing is filtering the port between you and it. If you are running the
server behind a reverse proxy on a sub-path, include the path
(`https://example.com/agentrq`).

**"That URL is not an AgentRQ server."**
Something answered, but it was not AgentRQ. Usually a proxy, a login portal, or
the wrong port on the right host.

**A self-signed HTTPS certificate.**
The app rejects certificates it cannot verify, exactly as a browser does. Either
add the certificate to your system trust store — the app uses it — or connect
over plain `http://` on a network you trust. There is deliberately no "ignore
certificate errors" switch: it would silently apply to every future connection.

**Notifications do not appear.**
Check the operating system first — macOS **System Settings → Notifications →
AgentRQ**, Windows **Settings → System → Notifications**. Then check the
workspace is not muted under **Workspace Settings → Notifications**. Remember
that only agent activity notifies.

**`agentrq://` links do nothing.**
The app registers the scheme when it first runs, so launch it once. On Linux
you may need `update-desktop-database`. If another application has claimed the
scheme, the operating system will prefer that one.

**Dictation does not work.**
It needs microphone permission — macOS asks the first time; grant it under
**System Settings → Privacy & Security → Microphone** if you dismissed it. The
speech model is downloaded on first use, so the first run needs a connection and
a moment.

**The window opened somewhere I cannot see it.**
It should not — saved positions are checked against your current displays. If it
happens, delete `agentrq-window.json` from the data directory above and relaunch.

**Starting over.**
Quit the app and delete its data directory. That clears the server URL, the
session, mute settings and window position. It does not touch anything on your
server.

---

## Building it yourself

```bash
make install        # dependencies for the whole repo
make desktop-dev    # run the desktop app against a local server
make desktop        # build installers into desktop/release/
```

`make desktop-dev` expects an AgentRQ server on `http://localhost:3000` — `make
dev` in another terminal gives you one. Point it elsewhere with
`AGENTRQ_SERVER_URL`:

```bash
AGENTRQ_SERVER_URL=https://agentrq.example.com make desktop-dev
```

The renderer is built from `frontend/src`, so a change to the web app appears in
the desktop app with no separate step.

Deeper detail — how the app:// proxy works, why the connection screen lives
where it does, how to run the verification scripts — is in
[`desktop/README.md`](../desktop/README.md).

---

## Releasing

Pushing a `v*` tag builds on all three platforms and publishes installers and
the update manifests to a GitHub Release, which is also the update feed. The
desktop, frontend and backend versions must agree, and the tag must match them;
the release is published only once every platform has finished, so nobody sees a
release missing the installer for their machine.

To enable signing, add `MAC_CERTIFICATE`, `MAC_CERTIFICATE_PASSWORD`,
`APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD` and `APPLE_TEAM_ID` as repository
secrets, plus `WIN_CERTIFICATE` and `WIN_CERTIFICATE_PASSWORD` for Windows. No
code changes are needed — the workflow builds unsigned when they are absent.
