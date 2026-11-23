// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { closeSync, openSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The file install.sh writes to, so the updater can read its progress back.
 *
 * The installer gets the descriptor as its stdout and stderr, and the file is
 * read back by path.
 */
export function createInstallLog({ dir }) {
  const path = join(dir, 'agentrq-update.log')
  const fd = openSync(path, 'w')
  let open = true
  return {
    path,
    stdio: ['ignore', fd, fd],
    read() {
      try {
        return readFileSync(path, 'utf8')
      } catch {
        return ''
      }
    },
    close() {
      if (!open) return
      open = false
      closeSync(fd)
    },
  }
}
