// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * How to install `agentrqd` on a machine.
 *
 * The enrolment panel used to show only the enrol command — which you can only
 * run once the daemon is already there. That left the one question somebody
 * asks at exactly that moment unanswered: where do I get this?
 *
 * The important thing here is what the detected platform is *for*. You are
 * usually setting up a machine other than the one you are browsing from — a
 * build box, a server, a spare laptop — so the browser's OS picks the tab that
 * opens, and never which instructions exist. All three stay reachable.
 */

/** Where the daemon's releases live. */
export const RELEASES_URL = 'https://github.com/agentrq/agentrq/releases?q=agentrqd&expanded=true'

/** The user-facing guide, which carries the trust model. */
export const DAEMON_DOCS_URL = 'https://agentrq.com/docs/daemon'

/** The platforms the daemon ships for. */
export const PLATFORMS = ['linux', 'macos', 'windows']

/**
 * Which platform's instructions to open with.
 *
 * A guess, and treated as one: it selects a tab and nothing more. Unknown
 * agents fall back to Linux, because a machine somebody runs agents on
 * unattended is more often Linux than anything else.
 */
export function detectPlatform(userAgent = '') {
  const ua = String(userAgent).toLowerCase()
  if (ua.includes('mac')) return 'macos'
  if (ua.includes('win')) return 'windows'
  return 'linux'
}

/** The label a tab shows. */
export function platformLabel(platform) {
  return { linux: 'Linux', macos: 'macOS', windows: 'Windows' }[platform] ?? 'Linux'
}

/**
 * The commands that put the binary on a machine's PATH.
 *
 * Deliberately not a curl-pipe-to-shell one-liner. That is the shortest thing
 * to print and it asks somebody to run code they have not seen, on a machine
 * they are about to grant an AgentRQ account command access to — the exact
 * moment to be showing what is happening rather than hiding it.
 *
 * `~/.local/bin` on Linux and `/usr/local/bin` on macOS because those are
 * where each platform expects a user-installed binary, and neither needs the
 * daemon to run as root — which it refuses to do anyway.
 *
 * Windows has no such directory that is already on PATH, so the step that
 * claims to put it on PATH has to actually do that. Unpacking into
 * %LOCALAPPDATA% and saying nothing more would leave somebody with a binary
 * they cannot run by name and a step that lied about what it did.
 */
export function installSteps(platform) {
  switch (platform) {
    case 'macos':
      return [
        'tar xzf agentrqd_*_darwin_*.tar.gz',
        'sudo install -m 0755 agentrqd /usr/local/bin/',
      ]
    case 'windows':
      return [
        'Expand-Archive agentrqd_*_windows_*.zip -DestinationPath $env:LOCALAPPDATA\\agentrqd',
        '# add it to PATH for future shells:',
        '[Environment]::SetEnvironmentVariable("Path",',
        '  "$env:Path;$env:LOCALAPPDATA\\agentrqd", "User")',
      ]
    default:
      return [
        'tar xzf agentrqd_*_linux_*.tar.gz',
        'install -m 0755 agentrqd ~/.local/bin/',
      ]
  }
}

/**
 * Running it, and keeping it running.
 *
 * `agentrqd serve` is the same everywhere, so it is the first line everywhere.
 * What differs is how you make it survive a logout, and that is worth saying
 * because the archive ships the unit files and nothing otherwise tells you
 * they are in there.
 *
 * Both are user-level: a systemd **user** unit and a **LaunchAgent**, not a
 * system service and not a LaunchDaemon. The daemon refuses to run as root, so
 * an instruction that reached for sudo here would be telling somebody to work
 * around the only thing keeping an agent to what they can do themselves.
 */
export function runSteps(platform) {
  switch (platform) {
    case 'macos':
      return [
        'agentrqd serve',
        '# or, to keep it running: copy com.agentrq.agentrqd.plist from the',
        '# archive into ~/Library/LaunchAgents and load it',
      ]
    case 'windows':
      return ['agentrqd serve']
    default:
      return [
        'agentrqd serve',
        '# or, to keep it running: copy agentrqd.service from the archive into',
        '# ~/.config/systemd/user and enable it',
      ]
  }
}

/**
 * The whole panel's content for one platform.
 *
 * Assembled here rather than in the template so the ordering — install, enrol,
 * run — is a decision with a test on it rather than the order somebody
 * happened to write the markup in. Getting it wrong prints an enrol command
 * for a binary that is not there yet, which is the bug this replaces.
 */
export function installGuide(platform, enrolCommand) {
  return {
    platform,
    label: platformLabel(platform),
    steps: [
      { title: 'Download it', lines: [], link: RELEASES_URL },
      { title: 'Put it on your PATH', lines: installSteps(platform) },
      { title: 'Enrol this machine', lines: enrolCommand ? [enrolCommand] : [] },
      { title: 'Run it', lines: runSteps(platform) },
    ],
  }
}
