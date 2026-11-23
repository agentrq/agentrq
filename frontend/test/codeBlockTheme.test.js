// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A code block's dark surface belongs behind `dark:`. Written bare, it is
 * black in the light theme too, a stray dark box on a light card — the
 * permission card and the trajectory payload both shipped that way.
 */

import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'

const SRC = join(__dirname, '..', 'src')

function vueFiles(dir) {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) return vueFiles(path)
    return name.endsWith('.vue') ? [path] : []
  })
}

// A near-black background not preceded by a variant such as `dark:`.
const BARE_DARK_BG = /(^|\s)bg-(zinc|gray|slate|neutral|stone)-9\d\d(\s|$)/

describe('code blocks follow the theme', () => {
  it('no <pre> has a dark background outside dark mode', () => {
    const offenders = []
    for (const file of vueFiles(SRC)) {
      for (const m of readFileSync(file, 'utf8').matchAll(/<pre\b[^>]*\bclass="([^"]*)"/g)) {
        if (BARE_DARK_BG.test(m[1])) offenders.push(`${file.slice(SRC.length + 1)}: ${m[1]}`)
      }
    }
    expect(offenders).toEqual([])
  })
})
