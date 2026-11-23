// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Which renderer draws the terminal, and what happens when it dies.
 *
 * The three behaviours here all share a shape: **the failure is silent and the
 * terminal keeps looking like a terminal.** A WebGL context that cannot be had
 * throws inside `activate()`, which `loadAddon` calls, so `new WebglAddon()`
 * succeeding proves nothing. A context that is *lost* throws nothing at all —
 * the addon simply stops drawing and the terminal goes blank, which is
 * indistinguishable from an agent that stopped talking.
 *
 * None of that can be reproduced in jsdom, which has no WebGL and no GPU to
 * take one away, so the addons are fakes that fail the way the real ones do.
 * The real renderers are compared in a browser; see the PR.
 */
import { describe, it, expect, vi } from 'vitest'
import {
  useTerminalRenderer,
  DOM_RENDERER,
  RECREATIONS,
} from '../src/composables/useTerminalRenderer.js'

/** A terminal that records what was loaded into it. */
function fakeTerm() {
  return { loaded: [], loadAddon(a) { this.loaded.push(a) } }
}

/**
 * An addon, optionally one that cannot be activated.
 *
 * `failOn: 'load'` throws from `loadAddon`, which is where the real WebGL
 * addon fails — inside `activate()`, not in its constructor.
 */
function fakeAddon({ contextLoss = false, failOn = null, subscription } = {}) {
  let listener = null
  return {
    disposed: 0,
    failOn,
    dispose() {
      this.disposed += 1
      if (this.failOn === 'dispose') throw new Error('context already gone')
    },
    ...(contextLoss
      ? {
          onContextLoss(fn) {
            listener = fn
            // `subscription` lets a test hand back something that cannot be
            // unsubscribed, which is the only way the listener outlives us.
            if (subscription !== undefined) return subscription
            return { dispose: () => { listener = null } }
          },
          /** Fire it, the way a driver reset would. */
          loseContext() {
            listener?.()
          },
          /** Whether anything is still subscribed. */
          watched() {
            return listener !== null
          },
        }
      : {}),
  }
}

/** A terminal whose `loadAddon` respects an addon's `failOn: 'load'`. */
function harness(candidates, opts = {}) {
  const term = fakeTerm()
  const loadAddon = term.loadAddon.bind(term)
  term.loadAddon = (a) => {
    if (a.failOn === 'load') throw new Error('WebGL is not available')
    loadAddon(a)
  }
  const onRenderer = vi.fn()
  const renderer = useTerminalRenderer({ term, candidates, onRenderer, ...opts })
  return { term, renderer, onRenderer }
}

describe('choosing a renderer', () => {
  it('takes the first one that works', () => {
    const webgl = fakeAddon({ contextLoss: true })
    const canvas = fakeAddon()
    const { renderer, term, onRenderer } = harness([
      { name: 'webgl', make: () => webgl },
      { name: 'canvas', make: () => canvas },
    ])

    expect(renderer.attach()).toBe('webgl')
    expect(renderer.current()).toBe('webgl')
    expect(term.loaded).toEqual([webgl])
    expect(onRenderer).toHaveBeenCalledWith('webgl')
  })

  // The case this list exists for: WebGL is absent on plenty of Linux boxes,
  // VMs and locked-down drivers, and it fails inside `activate()`.
  it('moves on when one cannot be activated', () => {
    const webgl = fakeAddon({ failOn: 'load' })
    const canvas = fakeAddon()
    const { renderer, term } = harness([
      { name: 'webgl', make: () => webgl },
      { name: 'canvas', make: () => canvas },
    ])

    expect(renderer.attach()).toBe('canvas')
    expect(term.loaded).toEqual([canvas])
    // The one that could not start is not left half-built.
    expect(webgl.disposed).toBe(1)
  })

  it('falls all the way back to the DOM renderer', () => {
    const { renderer, term, onRenderer } = harness([
      { name: 'webgl', make: () => fakeAddon({ failOn: 'load' }) },
      { name: 'canvas', make: () => fakeAddon({ failOn: 'load' }) },
    ])

    // No addon at all *is* the DOM renderer. Rendering imperfectly beats
    // rendering nothing.
    expect(renderer.attach()).toBe(DOM_RENDERER)
    expect(term.loaded).toEqual([])
    expect(onRenderer).toHaveBeenCalledWith(DOM_RENDERER)
  })

  it('survives an addon that throws on the way out', () => {
    const webgl = fakeAddon({ failOn: 'load' })
    webgl.failOn = 'load'
    const brokenDispose = { failOn: 'load', dispose() { throw new Error('WebGL context already lost') } }
    const { renderer } = harness([
      { name: 'webgl', make: () => brokenDispose },
      { name: 'canvas', make: () => fakeAddon() },
    ])

    // It is being thrown away either way; it must not take the terminal with it.
    expect(() => renderer.attach()).not.toThrow()
    expect(renderer.current()).toBe('canvas')
  })

  it('reports the DOM renderer when given nothing to try', () => {
    const { renderer } = harness([])
    expect(renderer.attach()).toBe(DOM_RENDERER)
  })
})

describe('losing the GPU context', () => {
  it('rebuilds the same renderer', () => {
    const made = []
    const { renderer, term } = harness([
      { name: 'webgl', make: () => { const a = fakeAddon({ contextLoss: true }); made.push(a); return a } },
      { name: 'canvas', make: () => fakeAddon() },
    ])
    renderer.attach()

    // Nothing throws when a context is lost — the addon just stops drawing, so
    // without this the terminal goes blank and stays blank.
    made[0].loseContext()

    expect(renderer.current()).toBe('webgl')
    expect(made).toHaveLength(2)
    expect(made[0].disposed).toBe(1)
    expect(term.loaded).toEqual([made[0], made[1]])
  })

  it('gives up on it after the budget and drops to the next one', () => {
    const made = []
    const canvas = fakeAddon()
    const { renderer } = harness([
      { name: 'webgl', make: () => { const a = fakeAddon({ contextLoss: true }); made.push(a); return a } },
      { name: 'canvas', make: () => canvas },
    ])
    renderer.attach()

    for (let i = 0; i <= RECREATIONS; i += 1) made[i].loseContext()

    // A context lost as fast as it is granted is a machine saying no, and
    // rebuilding forever burns a GPU context per attempt.
    expect(renderer.current()).toBe('canvas')
    expect(made).toHaveLength(RECREATIONS + 1)
  })

  it('ends at the DOM renderer when there is nothing left to drop to', () => {
    const made = []
    const { renderer } = harness(
      [{ name: 'webgl', make: () => { const a = fakeAddon({ contextLoss: true }); made.push(a); return a } }],
      { recreations: 0 },
    )
    renderer.attach()

    made[0].loseContext()
    expect(renderer.current()).toBe(DOM_RENDERER)
  })

  it('ignores a loss that arrives after unmount', () => {
    const made = []
    const { renderer } = harness([
      { name: 'webgl', make: () => { const a = fakeAddon({ contextLoss: true }); made.push(a); return a } },
    ])
    renderer.attach()
    const first = made[0]

    renderer.dispose()
    // Disposing the terminal is what loses the context, so this ordering is
    // the normal one rather than a corner case.
    first.loseContext()

    expect(made).toHaveLength(1)
    expect(renderer.current()).toBe(DOM_RENDERER)
  })

  it('is not watched for once it has been let go', () => {
    const made = []
    const { renderer } = harness([
      { name: 'webgl', make: () => { const a = fakeAddon({ contextLoss: true }); made.push(a); return a } },
    ])
    renderer.attach()
    expect(made[0].watched()).toBe(true)

    renderer.dispose()
    expect(made[0].watched()).toBe(false)
    expect(made[0].disposed).toBe(1)
  })

  // The `stopped` guard exists for this and only this: an addon that hands back
  // no way to unsubscribe leaves the listener live after unmount, and a lost
  // context would then rebuild a renderer onto a terminal that is gone.
  it.each([
    ['nothing at all', null],
    ['something with no dispose', {}],
  ])('ignores a loss from a subscription that returns %s', (_label, subscription) => {
    const made = []
    const { renderer } = harness([
      {
        name: 'webgl',
        make: () => {
          const a = fakeAddon({ contextLoss: true, subscription })
          made.push(a)
          return a
        },
      },
    ])
    renderer.attach()

    renderer.dispose()
    expect(() => made[0].loseContext()).not.toThrow()

    expect(made).toHaveLength(1)
    expect(renderer.current()).toBe(DOM_RENDERER)
  })

  it('needs no context-loss support from a renderer that has none', () => {
    // Only the WebGL addon reports this; the canvas one has nothing to lose.
    const canvas = fakeAddon()
    const { renderer } = harness([{ name: 'canvas', make: () => canvas }])

    expect(renderer.attach()).toBe('canvas')
    expect(() => renderer.dispose()).not.toThrow()
  })
})

describe('defaults', () => {
  it('needs neither candidates nor a listener', () => {
    const renderer = useTerminalRenderer({ term: fakeTerm() })
    expect(renderer.attach()).toBe(DOM_RENDERER)
  })

  it('rebuilds once before giving up, by default', () => {
    expect(RECREATIONS).toBe(1)
  })
})
