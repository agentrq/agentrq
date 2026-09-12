// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, expect, it, vi } from 'vitest'

import {
  INSTALL_SCOPE,
  MAX_SCOPE_BYTES,
  MAX_VALUE_BYTES,
  createStorageStore,
  scopeFor,
  sizeOf,
} from '../../src/main/extensions/storage.js'

/**
 * Settings an extension defines the shape of.
 *
 * `config.js` holds what the *manifest* declared and AgentRQ renders. This holds
 * what the extension decided, in whatever shape it decided — never inspected,
 * only stored. The tests below are mostly about the container rather than the
 * contents, because the container is the whole reason this exists instead of
 * the extension writing its own file: uninstall empties it, workspaces stay
 * apart, secrets reach the keychain, and one value cannot grow without bound.
 */

function build({ initial = null, write = null, available = true } = {}) {
  let saved = initial
  const store = {
    read: async () => {
      if (saved === null) throw new Error('no file yet')
      return saved
    },
    write: write ?? vi.fn(async (value) => { saved = JSON.parse(JSON.stringify(value)) }),
  }
  // Base64 rather than a readable prefix, so "the plaintext is not in the file"
  // is a claim the double can actually be held to.
  const vault = {
    available: () => available,
    encrypt: (text) => `enc:${Buffer.from(String(text), 'utf8').toString('base64')}`,
    decrypt: (blob) => {
      if (!String(blob).startsWith('enc:')) throw new Error('not mine')
      return Buffer.from(String(blob).slice(4), 'base64').toString('utf8')
    },
  }
  const logger = { warn: vi.fn() }
  return { storage: createStorageStore({ store, vault, logger }), store, logger, saved: () => saved }
}

describe('sizeOf', () => {
  it('measures a value the way it is stored', () => {
    expect(sizeOf('ab')).toBe(4) // "ab" with the quotes
    expect(sizeOf({ a: 1 })).toBe(7)
    expect(sizeOf(undefined)).toBe(4) // null
  })

  /** Not storable at all, and saying so beats storing a lie about it. */
  it('reports something it cannot serialise', () => {
    const circular = {}
    circular.self = circular
    expect(sizeOf(circular)).toBe(-1)
  })
})

describe('scopeFor', () => {
  it('names a workspace scope', () => {
    expect(scopeFor('0ZzhYQG2qtl')).toBe('ws:0ZzhYQG2qtl')
  })

  /**
   * The check is what keeps `workspace('')` from quietly writing to a scope
   * called `ws:` — one shared bucket wearing a per-workspace name.
   */
  it('refuses anything that could not be a workspace id', () => {
    expect(scopeFor('')).toBe('')
    expect(scopeFor(undefined)).toBe('')
    expect(scopeFor('has space')).toBe('')
    expect(scopeFor('a/b')).toBe('')
    expect(scopeFor('x'.repeat(33))).toBe('')
  })
})

describe('storage for one extension', () => {
  it('keeps and returns a value of any shape', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')

    expect(await at.set('rules', [{ when: 'rm -rf', then: 'deny' }])).toEqual({ ok: true })
    expect(await at.get('rules')).toEqual([{ when: 'rm -rf', then: 'deny' }])
  })

  it('answers with nothing for a key never written', async () => {
    const { storage } = build()
    expect(await storage.for('guardrail').get('rules')).toBeUndefined()
  })

  /**
   * Handing back the stored object would let one caller's edit change what
   * another reads, with no write anywhere in between.
   */
  it('hands back a copy, not the stored object', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')
    await at.set('rules', { list: ['a'] })

    const first = await at.get('rules')
    first.list.push('b')

    expect((await at.get('rules')).list).toEqual(['a'])
  })

  it('lists what it holds', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')
    await at.set('rules', 1)
    await at.set('mode', 'strict')

    expect((await at.keys()).sort()).toEqual(['mode', 'rules'])
    expect(await at.all()).toEqual({ rules: 1, mode: 'strict' })
  })

  it('removes one, and removing what is not there is not a failure', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')
    await at.set('rules', 1)

    expect(await at.delete('rules')).toEqual({ ok: true })
    expect(await at.get('rules')).toBeUndefined()
    expect(await at.delete('rules')).toEqual({ ok: true })
  })

  /** One extension cannot read or write another's: the name is closed over. */
  it('keeps two extensions apart', async () => {
    const { storage } = build()
    await storage.for('guardrail').set('rules', 'mine')
    await storage.for('standup').set('rules', 'theirs')

    expect(await storage.for('guardrail').get('rules')).toBe('mine')
    expect(await storage.for('standup').get('rules')).toBe('theirs')
  })

  it('survives a store with nothing in it yet', async () => {
    const { storage } = build({ initial: null })
    expect(await storage.for('guardrail').all()).toEqual({})
  })

  it('reads back what was already on disk', async () => {
    const { storage } = build({ initial: { data: { guardrail: { [INSTALL_SCOPE]: { rules: 'kept' } } } } })
    expect(await storage.for('guardrail').get('rules')).toBe('kept')
  })
})

describe('keys', () => {
  it('refuses one that is not a key, and says what a key is', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')

    const refused = await at.set('has space', 1)
    expect(refused.ok).toBe(false)
    expect(refused.reason).toContain('storage key')

    expect((await at.set('', 1)).ok).toBe(false)
    expect((await at.set('a'.repeat(65), 1)).ok).toBe(false)
    expect((await at.set('.hidden', 1)).ok).toBe(false)
  })

  /** A read with a bad key answers empty rather than throwing into `apply`. */
  it('reads a key that is not a string at all as no key', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')

    expect((await at.set(undefined, 1)).ok).toBe(false)
    expect(await at.get(undefined)).toBeUndefined()
  })

  /** `undefined` is a value somebody meant to store, and it stores as null. */
  it('stores an absent value as null rather than refusing it', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')

    expect((await at.set('rules', undefined)).ok).toBe(true)
    expect(await at.get('rules')).toBeNull()
  })

  it('answers emptily rather than throwing on a bad key', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')

    expect(await at.get('has space')).toBeUndefined()
    expect((await at.delete('has space')).ok).toBe(false)
  })
})

describe('size', () => {
  /**
   * Refused rather than truncated, the same rule the workspace memory tools
   * follow: an extension told its value is too large can split it, and one
   * silently cut in half cannot know to.
   */
  it('refuses one value that is too large, naming the limit', async () => {
    const { storage } = build()
    const huge = 'x'.repeat(MAX_VALUE_BYTES + 1)

    const refused = await storage.for('guardrail').set('rules', huge)

    expect(refused.ok).toBe(false)
    expect(refused.reason).toContain(String(MAX_VALUE_BYTES))
    expect(await storage.for('guardrail').get('rules')).toBeUndefined()
  })

  it('refuses a scope that would grow past its limit', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')
    const chunk = 'x'.repeat(MAX_VALUE_BYTES - 10)

    for (let i = 0; i < Math.floor(MAX_SCOPE_BYTES / MAX_VALUE_BYTES); i += 1) {
      expect((await at.set(`k${i}`, chunk)).ok).toBe(true)
    }
    const refused = await at.set('one-more', chunk)

    expect(refused.ok).toBe(false)
    expect(refused.reason).toContain('Remove something first')
  })

  it('refuses a value that is not JSON-shaped at all', async () => {
    const { storage } = build()
    const circular = {}
    circular.self = circular

    const refused = await storage.for('guardrail').set('rules', circular)

    expect(refused.ok).toBe(false)
    expect(refused.reason).toContain('not JSON-shaped')
  })
})

describe('workspace scopes', () => {
  it('keeps two workspaces apart', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')

    await at.workspace('ws1').set('rules', 'one')
    await at.workspace('ws2').set('rules', 'two')

    expect(await at.workspace('ws1').get('rules')).toBe('one')
    expect(await at.workspace('ws2').get('rules')).toBe('two')
  })

  /** The install-wide scope is not a workspace and cannot collide with one. */
  it('keeps a workspace apart from the installation itself', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')

    await at.set('rules', 'everywhere')
    await at.workspace('ws1').set('rules', 'here')

    expect(await at.get('rules')).toBe('everywhere')
    expect(await at.workspace('ws1').get('rules')).toBe('here')
  })

  /**
   * A store that refuses rather than one that writes somewhere shared — and a
   * store rather than `null`, because the extension's next line is `.get` and a
   * TypeError from inside `apply` is a worse answer than a sentence.
   */
  it('refuses everything for an id that is not one', async () => {
    const { storage } = build()
    const nowhere = storage.for('guardrail').workspace('')

    expect(await nowhere.get('rules')).toBeUndefined()
    expect(await nowhere.all()).toEqual({})
    expect(await nowhere.keys()).toEqual([])
    expect((await nowhere.set('rules', 1)).reason).toContain('not a workspace id')
    expect((await nowhere.delete('rules')).reason).toContain('not a workspace id')
  })
})

describe('secrets', () => {
  it('stores one through the vault and reads it back', async () => {
    const { storage, saved } = build()
    const at = storage.for('guardrail')

    expect(await at.secret.set('apiKey', 'sk-123')).toEqual({ ok: true })
    expect(await at.secret.get('apiKey')).toBe('sk-123')
    // Never in the clear, anywhere in the file.
    expect(JSON.stringify(saved())).not.toContain('sk-123')
  })

  it('says whether one is set without revealing it', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')

    expect(await at.secret.has('apiKey')).toBe(false)
    await at.secret.set('apiKey', 'sk-123')
    expect(await at.secret.has('apiKey')).toBe(true)
    expect(await at.secret.has('bad key')).toBe(false)
  })

  /** An emptied value clears it — how somebody removes a credential. */
  it('clears one by storing nothing', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')
    await at.secret.set('apiKey', 'sk-123')

    await at.secret.set('apiKey', '')

    expect(await at.secret.has('apiKey')).toBe(false)
    expect(await at.secret.get('apiKey')).toBe('')
  })

  it('forgets one entirely, and forgetting what is absent is not a failure', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')
    await at.secret.set('apiKey', 'sk-123')

    expect(await at.secret.clear('apiKey')).toEqual({ ok: true })
    expect(await at.secret.has('apiKey')).toBe(false)
    expect(await at.secret.clear('apiKey')).toEqual({ ok: true })
    expect((await at.secret.clear('bad key')).ok).toBe(false)
  })

  /**
   * Storing it anyway would be the worst of both worlds: the user believes it
   * is protected, and it is sitting in a JSON file.
   */
  it('refuses to store one where there is no secure storage', async () => {
    const { storage } = build({ available: false })

    const refused = await storage.for('guardrail').secret.set('apiKey', 'sk-123')

    expect(refused.ok).toBe(false)
    expect(refused.reason).toContain('no secure storage')
  })

  it('refuses a key that is not one', async () => {
    const { storage } = build()
    expect((await storage.for('guardrail').secret.set('bad key', 'x')).ok).toBe(false)
    expect(await storage.for('guardrail').secret.get('bad key')).toBe('')
    expect((await storage.for('guardrail').secret.clear('bad key')).ok).toBe(false)
  })

  it('clears one for an extension that never stored any', async () => {
    const { storage } = build()
    expect(await storage.for('guardrail').secret.clear('apiKey')).toEqual({ ok: true })
  })

  /**
   * Answered as absent rather than as ciphertext: an extension handed a value
   * that will not decrypt would send it somewhere as though it were the key.
   */
  it('treats one it cannot decrypt as missing', async () => {
    const { storage } = build({ initial: { secrets: { guardrail: { apiKey: 'from-another-machine' } } } })

    expect(await storage.for('guardrail').secret.get('apiKey')).toBe('')
  })

  it('has nothing to say about a secret never stored', async () => {
    const { storage } = build()
    expect(await storage.for('guardrail').secret.get('apiKey')).toBe('')
  })
})

describe('forget', () => {
  /** The whole reason the container is the app's rather than the extension's. */
  it('empties everything one extension stored, in every scope', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')
    await at.set('rules', 1)
    await at.workspace('ws1').set('rules', 2)
    await at.secret.set('apiKey', 'sk-123')

    expect(await storage.forget('guardrail')).toEqual({ ok: true })

    expect(await at.get('rules')).toBeUndefined()
    expect(await at.workspace('ws1').get('rules')).toBeUndefined()
    expect(await at.secret.has('apiKey')).toBe(false)
  })

  it('leaves other extensions alone', async () => {
    const { storage } = build()
    await storage.for('guardrail').set('rules', 1)
    await storage.for('standup').set('rules', 2)

    await storage.forget('guardrail')

    expect(await storage.for('standup').get('rules')).toBe(2)
  })

  /**
   * Throwing here would throw out of an uninstall that has already stopped the
   * extension and revoked its grant, leaving it listed as installed with no way
   * back short of a restart. The settings surviving is the smaller problem.
   */
  it('reports a failed write rather than failing the uninstall', async () => {
    const { storage, logger } = build({ write: async () => { throw new Error('disk full') } })

    expect(await storage.forget('guardrail')).toEqual({ ok: true, persisted: false })
    expect(logger.warn).toHaveBeenCalled()
  })
})

describe('a logger with nothing on it', () => {
  /**
   * `console` is the default and has `warn`, so the optional call only matters
   * for a caller that passed something narrower — and a failed write must not
   * become a TypeError on top of a disk error.
   */
  it('does not turn a failed write into a second failure', async () => {
    const storage = createStorageStore({
      store: { read: async () => ({}), write: async () => { throw new Error('disk full') } },
      vault: { available: () => true, encrypt: (t) => t, decrypt: (t) => t },
      logger: {},
    })

    expect((await storage.for('guardrail').set('rules', 1)).ok).toBe(false)
  })
})

describe('a failed write during ordinary use', () => {
  /**
   * A rejected promise inside extension code surfaces as the extension having
   * thrown, which counts a disk problem against its author.
   */
  it('comes back as a reason rather than a throw', async () => {
    const { storage, logger } = build({ write: async () => { throw new Error('disk full') } })

    const refused = await storage.for('guardrail').set('rules', 1)

    expect(refused).toEqual({ ok: false, reason: 'That could not be saved.' })
    expect(logger.warn).toHaveBeenCalledWith(expect.any(String), 'disk full')
  })

  /** Not everything thrown is an Error, and the log line still has to read. */
  it('logs something thrown that has no message', async () => {
    const { storage, logger } = build({ write: async () => { throw 'disk full' } })

    expect((await storage.for('guardrail').set('rules', 1)).ok).toBe(false)
    expect(logger.warn).toHaveBeenCalledWith(expect.any(String), 'disk full')
  })
})

describe('two writes that arrive before the first read finishes', () => {
  /**
   * The file is read once, however many callers want it.
   *
   * Every caller here is somebody else's code and none of them awaits the
   * others: two extensions writing as the app starts, or one form saving two
   * keys. Reading the file per caller gave each of them a *different* state
   * object, and the last write persisted one that had never seen the others —
   * settings that were saved, reported saved, and silently gone.
   */
  it('keeps both, and reads the file once', async () => {
    const { storage, store, saved } = build({ initial: { data: {}, secrets: {} } })
    const read = vi.spyOn(store, 'read')
    const at = storage.for('guardrail')

    const [first, second] = await Promise.all([at.set('rules', 'a'), at.set('limits', 'b')])

    expect(first.ok && second.ok).toBe(true)
    expect(read).toHaveBeenCalledTimes(1)
    expect(saved().data.guardrail[INSTALL_SCOPE]).toEqual({ rules: 'a', limits: 'b' })
  })
})

describe('usage', () => {
  it('says what one extension is holding, per scope', async () => {
    const { storage } = build()
    const at = storage.for('guardrail')
    await at.set('rules', 'abc')
    await at.workspace('ws1').set('rules', 'de')

    const usage = await storage.usage('guardrail')

    expect(usage).toEqual([
      { scope: INSTALL_SCOPE, keys: 1, bytes: sizeOf({ rules: 'abc' }) },
      { scope: 'ws:ws1', keys: 1, bytes: sizeOf({ rules: 'de' }) },
    ])
  })

  it('says nothing about one holding nothing', async () => {
    const { storage } = build()
    expect(await storage.usage('guardrail')).toEqual([])
  })
})
