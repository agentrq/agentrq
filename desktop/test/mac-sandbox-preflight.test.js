// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect } from 'vitest'

import {
  HELPER_RESOURCES,
  QUARANTINE_ATTRIBUTE,
  checkMacSandbox,
  describeSandboxProblem,
} from '../scripts/mac-sandbox-preflight.mjs'

const BUNDLE = '/repo/desktop/node_modules/electron/dist/Electron.app'
const EXECUTABLE = `${BUNDLE}/Contents/MacOS/Electron`

describe('describeSandboxProblem', () => {
  const healthy = { platform: 'darwin', bundlePath: BUNDLE, resourcesMissing: false, quarantined: false }

  it('says nothing when the bundle is intact', () => {
    // The sandbox line is usually the machine's business, not ours. Warning on
    // every dev run would train people to ignore the one run that matters.
    expect(describeSandboxProblem(healthy)).toBeNull()
  })

  it('says nothing on a platform that has no sandbox extensions', () => {
    for (const platform of ['linux', 'win32']) {
      expect(describeSandboxProblem({ ...healthy, platform, quarantined: true })).toBeNull()
    }
  })

  it('names the quarantine attribute and the command that clears it', () => {
    const message = describeSandboxProblem({ ...healthy, quarantined: true })
    expect(message).toContain(QUARANTINE_ATTRIBUTE)
    expect(message).toContain(`xattr -dr ${QUARANTINE_ATTRIBUTE} node_modules/electron/dist`)
    expect(message).toContain('sandbox_extension_issue_file')
  })

  it('reports a damaged bundle ahead of quarantine, and says how to replace it', () => {
    // Both can be true at once; reinstalling fixes either, so it is the advice
    // to give. Clearing the attribute on a bundle macOS has already gutted
    // would look like a fix and change nothing.
    const message = describeSandboxProblem({ ...healthy, resourcesMissing: true, quarantined: true })
    expect(message).toContain('rm -rf node_modules/electron && npm install')
    expect(message).not.toContain('xattr -dr')
  })
})

describe('checkMacSandbox', () => {
  const run = (over = {}) =>
    checkMacSandbox({
      platform: 'darwin',
      electronPath: EXECUTABLE,
      exists: () => true,
      hasQuarantine: () => false,
      ...over,
    })

  it('looks for the resources the failing extension actually covers', () => {
    const asked = []
    run({ exists: (path) => (asked.push(path), true) })
    expect(asked).toEqual([`${BUNDLE}/${HELPER_RESOURCES}`])
  })

  it('checks the bundle, not the executable inside it, for quarantine', () => {
    // The attribute is set on what was unpacked; asking about the binary alone
    // misses it.
    const asked = []
    run({ hasQuarantine: (path) => (asked.push(path), true) })
    expect(asked).toEqual([BUNDLE])
  })

  it('passes a healthy install', () => {
    expect(run()).toBeNull()
  })

  it('reports a quarantined install', () => {
    expect(run({ hasQuarantine: () => true })).toContain(QUARANTINE_ATTRIBUTE)
  })

  it('reports a bundle whose helper resources are gone', () => {
    expect(run({ exists: () => false })).toContain('npm install')
  })

  it('does nothing off macOS, without touching the disk', () => {
    const touched = []
    const result = checkMacSandbox({
      platform: 'linux',
      electronPath: '/repo/node_modules/electron/dist/electron',
      exists: (p) => (touched.push(p), true),
      hasQuarantine: (p) => (touched.push(p), true),
    })
    expect(result).toBeNull()
    expect(touched).toEqual([])
  })

  it('stays quiet when the path is not the bundle layout it knows', () => {
    // An overridden ELECTRON_OVERRIDE_DIST_PATH, or a future layout. Being
    // confidently wrong about an unfamiliar tree is worse than being silent.
    expect(run({ electronPath: '/opt/somewhere/electron' })).toBeNull()
  })
})
