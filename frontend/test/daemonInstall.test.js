// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

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
  it('unpacks and installs, per platform', () => {
    expect(installSteps('linux').join(' ')).toContain('linux')
    expect(installSteps('macos').join(' ')).toContain('darwin')
    expect(installSteps('windows').join(' ')).toContain('windows')
  })

  // Not a curl-pipe-to-shell one-liner: it asks somebody to run code they have
  // not seen, on the machine they are about to grant command access to.
  it('never pipes a download into a shell', () => {
    for (const platform of PLATFORMS) {
      const text = installSteps(platform).join('\n')
      expect(text, platform).not.toMatch(/curl[^|]*\|\s*(sh|bash)/)
      expect(text, platform).not.toMatch(/iwr[^|]*\|\s*iex/i)
    }
  })

  // The daemon refuses to run as root, so an install that reached for sudo on
  // Linux would be teaching somebody to work around the check that keeps an
  // agent to what they can do themselves.
  it('installs to a user path on Linux', () => {
    expect(installSteps('linux').join(' ')).toContain('~/.local/bin')
    expect(installSteps('linux').join(' ')).not.toContain('sudo')
  })

  // Windows has no user directory that is already on PATH, so the step that
  // says it puts the binary there has to actually do it. Unpacking into
  // %LOCALAPPDATA% and stopping would leave somebody with a binary they cannot
  // run by name, after a step that claimed otherwise.
  it('actually puts it on PATH on Windows', () => {
    const text = installSteps('windows').join('\n')
    expect(text).toContain('Path')
    expect(text).toContain('SetEnvironmentVariable')
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
  it('puts the download before the enrolment', () => {
    const guide = installGuide('linux', 'agentrqd enroll --server https://x --code ABCD')
    const titles = guide.steps.map((s) => s.title)
    expect(titles).toEqual(['Download it', 'Put it on your PATH', 'Enrol this machine', 'Run it'])
  })

  it('carries the enrol command it was given', () => {
    const cmd = 'agentrqd enroll --server https://agentrq.example --code ABCD-EFGH'
    const guide = installGuide('macos', cmd)
    expect(guide.steps[2].lines).toEqual([cmd])
    expect(guide.label).toBe('macOS')
    expect(guide.platform).toBe('macos')
  })

  // Before a code is minted there is nothing to enrol with, and an empty line
  // renders as an empty code block rather than as nothing.
  it('leaves the enrol step empty when there is no code yet', () => {
    expect(installGuide('linux', '').steps[2].lines).toEqual([])
    expect(installGuide('linux', undefined).steps[2].lines).toEqual([])
  })

  it('links the download at the step that needs it', () => {
    const guide = installGuide('windows', 'x')
    expect(guide.steps[0].link).toBe(RELEASES_URL)
    expect(guide.steps[1].link).toBeUndefined()
  })
})

describe('the links', () => {
  // The releases page filtered to the daemon's own tags, not
  // releases/latest/download: this repository also releases the desktop app,
  // and "latest" would be whichever of the two shipped most recently.
  it('points at the daemon releases rather than the newest release', () => {
    expect(RELEASES_URL).toContain('agentrqd')
    expect(RELEASES_URL).not.toContain('latest/download')
  })

  it('points the guide at the documentation site', () => {
    expect(DAEMON_DOCS_URL).toMatch(/^https:\/\//)
  })
})
