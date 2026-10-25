// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

import { drawerFrameCSP, drawerFrameDocument } from '../../desktop/src/main/extensions/drawer-frame.js'

/** The shell's copy of the frame document (`core/src/drawer_frame.rs`). */
const TEMPLATE = readFileSync(new URL('../core/src/drawer_frame.html', import.meta.url), 'utf8')

/** What `drawer_frame_document` does with it. */
const render = (nonce, origin) =>
  TEMPLATE.replaceAll('@@CSP@@', drawerFrameCSP(nonce, origin))
    .replaceAll('@@FONT@@', 'system-ui, -apple-system, sans-serif')
    .replaceAll('@@NONCE@@', nonce)

describe("the shell's drawer frame", () => {
  it('is the document Electron serves, byte for byte', () => {
    for (const nonce of ['abc', 'q8+/Zw==']) {
      const electron = drawerFrameDocument({ nonce })
      expect(render(nonce, 'app://agentrq')).toBe(electron.html)
    }
  })

  it('carries the policy the Rust port builds', () => {
    // `drawer_frame::tests::names_the_origin_it_is_served_from`.
    expect(drawerFrameCSP('n0', 'app://localhost')).toBe(
      "default-src 'none'; script-src 'nonce-n0' app://localhost; " +
        "style-src 'unsafe-inline'; img-src data: blob:; font-src data:; " +
        "connect-src 'none'; form-action 'none'; base-uri 'none'",
    )
  })
})
