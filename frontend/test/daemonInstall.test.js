// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest'
import {
  detectPlatform,
  platformLabel,
  installSteps,
  runSteps,
  installGuide,
  PLATFORMS,
  RELEASES_URL,
  DAEMON_DOCS_URL,
  INSTALL_HINT,
  INSTALLER_URL,
  INSTALLER_URL_WINDOWS,
  usesInstaller,
} from '../src/composables/useDaemonInstall.js'

describe('detectPlatform', () => {
  it('recognises the three it ships for', () => {
    expect(detectPlatform('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)')).toBe('macos')
    expect(detectPlatform('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')).toBe('windows')
    expect(detectPlatform('Mozilla/5.0 (X11; Linux x86_64)')).toBe('linux')
  })

  // A guess that only picks which tab opens. A machine somebody runs agents on
  // unattended is more often Linux than anything else.
  it('falls back to Linux for anything it cannot place', () => {
    expect(detectPlatform('')).toBe('linux')
    expect(detectPlatform(undefined)).toBe('linux')
    expect(detectPlatform('Mozilla/5.0 (PlayStation 5)')).toBe('linux')
    expect(detectPlatform(null)).toBe('linux')
  })

  it('does not care about case', () => {
    expect(detectPlatform('MACINTOSH')).toBe('macos')
    expect(detectPlatform('WIN64')).toBe('windows')
  })
})

describe('platformLabel', () => {
  it('names each one the way its makers do', () => {
    expect(platformLabel('linux')).toBe('Linux')
    expect(platformLabel('macos')).toBe('macOS')
    expect(platformLabel('windows')).toBe('Windows')
  })
  it('falls back rather than rendering undefined', () => {
    expect(platformLabel('haiku')).toBe('Linux')
    expect(platformLabel(undefined)).toBe('Linux')
  })
})

describe('installSteps', () => {
  // Linux and macOS install with the hosted `sh` script.
  it('installs with the sh script on Linux and macOS', () => {
    for (const platform of ['linux', 'macos']) {
      expect(installSteps(platform), platform).toEqual([`curl -fsSL ${INSTALLER_URL} | sh`])
    }
  })

  // Windows has no `sh`, so it gets its own PowerShell script, piped into `iex`
  // the same way the other two pipe into `sh`.
  it('installs with the PowerShell script on Windows', () => {
    expect(installSteps('windows')).toEqual([`irm ${INSTALLER_URL_WINDOWS} | iex`])
  })

  // The whole reason this is one line rather than four is that the script
  // verifies what it downloaded and the manual steps never did. Fetching it
  // over plain http would hand that away to anyone on the path.
  it('fetches the installer over https, from the official host', () => {
    expect(INSTALLER_URL).toMatch(/^https:\/\/agentrq\.com\//)
    expect(INSTALLER_URL_WINDOWS).toMatch(/^https:\/\/agentrq\.com\//)
    expect(installSteps('linux').join('\n')).toContain(INSTALLER_URL)
    expect(installSteps('windows').join('\n')).toContain(INSTALLER_URL_WINDOWS)
  })

  // The daemon refuses to run as root or Administrator, so an install that
  // reached for sudo/elevation would be teaching somebody to work around the
  // check that keeps an agent to what they can do themselves. The script asks
  // for it only to copy a file into /usr/local/bin on macOS, and that is its
  // business, not this panel's.
  it('never tells anybody to install it as root', () => {
    for (const platform of PLATFORMS) {
      expect(installSteps(platform).join('\n'), platform).not.toContain('sudo')
    }
  })

  it('falls back to Linux for a platform it does not know', () => {
    expect(installSteps('haiku')).toEqual(installSteps('linux'))
  })
})

describe('runSteps', () => {
  it('always starts with the command that runs it', () => {
    for (const platform of PLATFORMS) {
      expect(runSteps(platform)[0], platform).toBe('agentrqd serve')
    }
  })

  // The archive ships the unit files and nothing else tells you they are there.
  it('says how to keep it running where a service manager exists', () => {
    expect(runSteps('linux').join(' ')).toContain('agentrqd.service')
    expect(runSteps('macos').join(' ')).toContain('LaunchAgents')
  })

  // Both user-level: a systemd user unit and a LaunchAgent, never a system
  // service and never a LaunchDaemon.
  it('never tells anybody to run it as root', () => {
    for (const platform of PLATFORMS) {
      const text = runSteps(platform).join('\n')
      expect(text, platform).not.toContain('sudo')
      expect(text, platform).not.toContain('LaunchDaemons')
      expect(text, platform).not.toContain('/etc/systemd/system')
    }
  })

  it('falls back to Linux for a platform it does not know', () => {
    expect(runSteps('haiku')).toEqual(runSteps('linux'))
  })
})

describe('installGuide', () => {
  // The order is the point. Printing the enrol command first is exactly the
  // bug this replaces: a command for a binary that is not there yet.
  it('puts the install before the enrolment', () => {
    const guide = installGuide('linux', 'agentrqd enroll --server https://x --code ABCD')
    const titles = guide.steps.map((s) => s.title)
    expect(titles).toEqual(['Install it', 'Enrol this machine', 'Run it'])
  })

  // Windows installs with a script too now, so it has the same three steps as
  // every other platform.
  it('has the same steps on Windows as everywhere else', () => {
    const titles = installGuide('windows', 'cmd').steps.map((s) => s.title)
    expect(titles).toEqual(['Install it', 'Enrol this machine', 'Run it'])
  })

  // The command is the install, so the step says to copy it rather than
  // offering a download link beside it, which read as a second way.
  it('tells you to copy the install command, with no download link', () => {
    for (const platform of PLATFORMS) {
      const step = installGuide(platform, '').steps[0]
      expect(step.hint, platform).toBe(INSTALL_HINT)
      expect(step.hint, platform).toBe('Copy the following code into your terminal to install.')
      expect(step.link, platform).toBeUndefined()
    }
  })

  it('carries the enrol command it was given', () => {
    const cmd = 'agentrqd enroll --server https://agentrq.example --code ABCD-EFGH'
    const guide = installGuide('macos', cmd)
    const enrol = guide.steps.find((step) => step.title === 'Enrol this machine')
    expect(enrol.lines).toEqual([cmd])
    expect(guide.label).toBe('macOS')
    expect(guide.platform).toBe('macos')
  })

  it('knows every platform installs with a script', () => {
    for (const platform of PLATFORMS) {
      expect(usesInstaller(platform), platform).toBe(true)
    }
  })

  // Before a code is minted there is nothing to enrol with, and an empty line
  // renders as an empty code block rather than as nothing.
  it('leaves the enrol step empty when there is no code yet', () => {
    const enrolStep = (cmd) =>
      installGuide('linux', cmd).steps.find((step) => step.title === 'Enrol this machine')
    expect(enrolStep('').lines).toEqual([])
    expect(enrolStep(undefined).lines).toEqual([])
  })
})

describe('the links', () => {
  // Desktop and daemon are released together under unified tags, so the releases
  // page lists both without needing tag filters.
  it('points at the latest releases rather than a direct asset download', () => {
    expect(RELEASES_URL).toContain('releases/latest')
    expect(RELEASES_URL).not.toContain('latest/download')
  })

  it('points the guide at the documentation site', () => {
    expect(DAEMON_DOCS_URL).toMatch(/^https:\/\//)
  })
})
