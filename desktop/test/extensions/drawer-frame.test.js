// Copyright 2026 Contextual, Inc. https://agentrq.com

import { describe, it, expect } from 'vitest'

import {
  APP_ORIGIN,
  DRAWER_CODE_PREFIX,
  DRAWER_FRAME_PATH,
  drawerCodeUrl,
  drawerFrameCSP,
  drawerFrameDocument,
  newNonce,
} from '../../src/main/extensions/drawer-frame.js'

/**
 * The document an extension's drawer runs in.
 *
 * Served from a URL rather than written into a `srcdoc`, because a local-scheme
 * document inherits the embedder's CSP — and under this app's policy, combined
 * with a sandboxed frame's opaque origin, that means no script can run in one at
 * all. These tests pin the policy the document carries instead.
 */

describe('drawerFrameCSP', () => {
  const csp = drawerFrameCSP('NONCE')

  it('refuses everything by default', () => {
    expect(csp).toContain("default-src 'none'")
  })

  // A drawer that could fetch is a drawer that could send what it was handed
  // somewhere. Easier to relax later than to tighten.
  it('gives a drawer no network at all', () => {
    expect(csp).toContain("connect-src 'none'")
    expect(csp).not.toMatch(/https?:/)
    expect(csp).not.toContain('*')
  })

  /**
   * A nonce rather than `'unsafe-inline'`, which is what VS Code's webview
   * guide argues for. With `'unsafe-inline'` the bootstrap runs and so would
   * any inline script the drawer injects afterwards. The drawer is already
   * code we chose to run, so this is not the wall — but there is no reason to
   * leave a door open beside it.
   */
  it('admits the bootstrap by name, and no other inline script', () => {
    expect(csp).toContain("script-src 'nonce-NONCE'")
    expect(csp).not.toMatch(/script-src[^;]*unsafe-inline/)
  })

  // The drawer is imported from a URL this app serves, and that origin is the
  // only one named. A drawer cannot pull code from anywhere else.
  it('admits code from this app and nowhere else', () => {
    expect(csp).toContain("script-src 'nonce-NONCE' app://agentrq")
    expect(csp).not.toContain('https:')
  })

  it('leaves nowhere to navigate or submit to', () => {
    expect(csp).toContain("form-action 'none'")
    expect(csp).toContain("base-uri 'none'")
  })
})

describe('newNonce', () => {
  it('is different every time, or it is not a nonce', () => {
    const seen = new Set(Array.from({ length: 50 }, () => newNonce()))

    expect(seen.size).toBe(50)
    expect([...seen][0].length).toBeGreaterThan(16)
  })
})

describe('DRAWER_FRAME_PATH', () => {
  // Namespaced so it can never collide with a renderer asset, and absolute so
  // the handler can match it exactly rather than by prefix.
  it('is a path of its own', () => {
    expect(DRAWER_FRAME_PATH.startsWith('/')).toBe(true)
    expect(DRAWER_FRAME_PATH).toContain('__agentrq')
  })
})

describe('drawerCodeUrl', () => {
  it('addresses a drawer by format, on this app origin', () => {
    expect(drawerCodeUrl('mermaid')).toBe(`${APP_ORIGIN}${DRAWER_CODE_PREFIX}mermaid.js`)
  })

  // The format reaches a URL, so it is encoded rather than trusted to be safe
  // in one — even though the manifest already refuses anything but a word.
  it('encodes a format that has no business in a path, and folds case', () => {
    expect(drawerCodeUrl('../evil')).not.toContain('../')
    expect(drawerCodeUrl('a b')).not.toContain(' ')
    expect(drawerCodeUrl('MERMAID')).toBe(drawerCodeUrl('mermaid'))
    expect(drawerCodeUrl(undefined)).toContain('/.js')
  })

  it('can be pointed at another origin, which is how it is tested', () => {
    expect(drawerCodeUrl('mermaid', 'app://other')).toBe(`app://other${DRAWER_CODE_PREFIX}mermaid.js`)
  })
})

describe('drawerFrameDocument', () => {
  // Returned together so they cannot be generated apart and disagree, which
  // would refuse the bootstrap and look exactly like a broken frame.
  it('carries its own policy, with the same nonce as the script it admits', () => {
    const { html, csp } = drawerFrameDocument()
    const nonce = html.match(/nonce="([^"]+)"/)[1]

    expect(html).toContain(csp)
    expect(csp).toContain(`'nonce-${nonce}'`)
  })

  it('uses a fresh nonce for every document', () => {
    const first = drawerFrameDocument().html.match(/nonce="([^"]+)"/)[1]
    const second = drawerFrameDocument().html.match(/nonce="([^"]+)"/)[1]

    expect(first).not.toBe(second)
  })

  it('holds the bootstrap that receives, draws and reports back', () => {
    const doc = drawerFrameDocument().html

    expect(doc).toContain("addEventListener('message'")
    expect(doc).toContain('import(request.url)')
    expect(doc).toContain("type: 'ready'")
    expect(doc).toContain("type: 'drawn'")
    expect(doc).toContain("type: 'failed'")
  })

  // The frame cannot read the page's stylesheet, so a drawer inherits this
  // app's look only if it is carried in.
  it('carries the look in with it', () => {
    expect(drawerFrameDocument({ font: 'Inter, sans-serif' }).html).toContain('Inter, sans-serif')
    expect(drawerFrameDocument().html).toContain('system-ui')
  })

  // Transparent, because the figure's own background is drawn by the block
  // around it — the frame holds a picture and nothing else.
  it('is transparent, and does not scroll inside itself', () => {
    const doc = drawerFrameDocument().html

    expect(doc).toContain('background: transparent')
    expect(doc).toContain('overflow: hidden')
  })

  // Measured twice on purpose: an animation frame is the accurate moment, and
  // it never arrives in a window that is not painting — a hidden window, a
  // background tab, a minimised app. A block waiting only for it would sit on
  // "Drawing…" until the timeout.
  it('reports a height straight away, and again on the next frame', () => {
    const doc = drawerFrameDocument().html

    expect(doc).toContain('measure();')
    expect(doc).toContain('requestAnimationFrame(measure)')
  })
})
