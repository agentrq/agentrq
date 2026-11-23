// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect } from 'vitest'
import { mkdtempSync, rmSync, writeSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { createInstallLog } from '../src/main/install-log.js'

describe('createInstallLog', () => {
  it('gives the installer a descriptor and reads back what it wrote', () => {
    const dir = mkdtempSync(join(tmpdir(), 'install-log-'))
    try {
      const log = createInstallLog({ dir })

      const [stdin, stdout, stderr] = log.stdio
      expect(stdin).toBe('ignore')
      expect(stdout).toBe(stderr)
      expect(log.path).toBe(join(dir, 'agentrq-update.log'))
      expect(log.read()).toBe('')

      writeSync(stdout, 'Downloading x.dmg...\n')
      expect(log.read()).toBe('Downloading x.dmg...\n')

      log.close()
      // Closing twice is harmless: the updater closes on every way out.
      log.close()
    } finally {
      rmSync(dir, { recursive: true, force: true })
    }
  })

  it('reads as empty once the file has gone', () => {
    const dir = mkdtempSync(join(tmpdir(), 'install-log-'))
    const log = createInstallLog({ dir })
    rmSync(dir, { recursive: true, force: true })

    expect(log.read()).toBe('')
    log.close()
  })
})
