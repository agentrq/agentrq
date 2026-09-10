import { describe, it, expect, vi } from 'vitest'

import {
  createConfigStore,
  describeSecrets,
  validateConfig,
} from '../../src/main/extensions/config.js'

/**
 * The split that matters: ordinary settings are readable, and secrets are not.
 * A saved secret can be replaced but never read back — there is no reason for a
 * settings screen to show a credential, and every reason not to.
 */

const fields = [
  { key: 'teamId', type: 'string', label: 'Team' },
  { key: 'apiKey', type: 'secret', label: 'API key' },
  { key: 'verbose', type: 'boolean', label: 'Verbose' },
  { key: 'limit', type: 'number', label: 'Limit' },
]

/**
 * A vault that round-trips but does not leave the plaintext lying in the output.
 *
 * Deliberately not `enc(${text})`: a fake that keeps the value visible makes
 * "the secret is never stored in the clear" pass against the fake rather than
 * against the code.
 */
const fakeVault = (available = true) => ({
  available: () => available,
  encrypt: (text) => Buffer.from(text, 'utf8').toString('base64'),
  decrypt: (blob) => {
    if (!/^[A-Za-z0-9+/]*={0,2}$/.test(blob)) throw new Error('cannot decrypt')
    return Buffer.from(blob, 'base64').toString('utf8')
  },
})

const fakeStore = (initial = { config: {}, secrets: {} }) => {
  let saved = initial
  return {
    read: vi.fn(async () => saved),
    write: vi.fn(async (state) => {
      saved = JSON.parse(JSON.stringify(state))
    }),
    get saved() {
      return saved
    },
  }
}

describe('validateConfig', () => {
  it('separates secrets from everything else', () => {
    const { values, secrets } = validateConfig(fields, { teamId: 'ENG', apiKey: 'sk-1' })

    expect(values).toEqual({ teamId: 'ENG' })
    expect(secrets).toEqual({ apiKey: 'sk-1' })
  })

  it('refuses a setting the extension never declared', () => {
    // A form that silently discards a value looks like it saved it, and the
    // extension then behaves as though nothing was typed.
    const { ok, reason } = validateConfig(fields, { nope: 'x' })

    expect(ok).toBe(false)
    expect(reason).toBe('This extension has no setting called "nope".')
  })

  it('coerces the types it declares, and refuses what it cannot', () => {
    expect(validateConfig(fields, { verbose: 'true' }).values.verbose).toBe(true)
    expect(validateConfig(fields, { verbose: false }).values.verbose).toBe(false)
    expect(validateConfig(fields, { limit: '20' }).values.limit).toBe(20)
    expect(validateConfig(fields, { limit: 7 }).values.limit).toBe(7)

    expect(validateConfig(fields, { verbose: 'maybe' }).reason).toBe('"Verbose" must be true or false.')
    expect(validateConfig(fields, { limit: 'lots' }).reason).toBe('"Limit" must be a number.')
    expect(validateConfig(fields, { teamId: { a: 1 } }).reason).toBe('"Team" must be text.')
  })

  it('trims text, so a pasted value with a stray space still works', () => {
    expect(validateConfig(fields, { teamId: '  ENG  ' }).values.teamId).toBe('ENG')
  })

  it('treats a field submitted as empty as empty, not as a crash', () => {
    // A form posts a cleared field as null rather than omitting it.
    expect(validateConfig(fields, { teamId: null }).values.teamId).toBe('')
    expect(validateConfig(fields, { teamId: undefined }).values.teamId).toBe('')
  })

  it('leaves out what was not submitted', () => {
    const { values, secrets } = validateConfig(fields, {})

    expect(values).toEqual({})
    expect(secrets).toEqual({})
  })

  it('copes with an extension that declares nothing', () => {
    expect(validateConfig(undefined, {}).ok).toBe(true)
  })
})

describe('describeSecrets', () => {
  it('says whether each is set, and nothing else about it', () => {
    // Not the value, and not its length — which narrows a guess more than
    // people expect.
    const described = describeSecrets(fields, { apiKey: 'c2stMQ==' })

    expect(described).toEqual([{ key: 'apiKey', label: 'API key', set: true }])
  })

  it('reports an unset secret as unset', () => {
    expect(describeSecrets(fields, {})[0].set).toBe(false)
    expect(describeSecrets(fields, { apiKey: '' })[0].set).toBe(false)
  })

  it('copes with nothing declared', () => {
    expect(describeSecrets()).toEqual([])
  })
})

describe('createConfigStore', () => {
  const build = (vault = fakeVault(), store = fakeStore()) => ({
    config: createConfigStore({ store, vault }),
    store,
  })

  it('saves settings and hands them back', async () => {
    const { config } = build()

    await config.save('linear', fields, { teamId: 'ENG', limit: 5 })

    expect(await config.get('linear')).toEqual({ teamId: 'ENG', limit: 5 })
  })

  it('encrypts a secret and never stores it in the clear', async () => {
    const { config, store } = build()

    await config.save('linear', fields, { apiKey: 'sk-live-1' })

    expect(JSON.stringify(store.saved)).not.toContain('sk-live-1')
    expect(store.saved.secrets.linear.apiKey).toBe(Buffer.from('sk-live-1').toString('base64'))
  })

  it('gives the extension the decrypted value, and only the extension', async () => {
    const { config } = build()
    await config.save('linear', fields, { teamId: 'ENG', apiKey: 'sk-1' })

    expect(await config.resolve('linear')).toEqual({ teamId: 'ENG', apiKey: 'sk-1' })
    // The settings screen sees only whether it is set.
    expect(await config.describe('linear', fields)).toEqual([
      { key: 'apiKey', label: 'API key', set: true },
    ])
  })

  it('refuses to save a secret with nowhere safe to put it', async () => {
    // Writing it anyway is the worst of both worlds: the user believes it is
    // protected and it is sitting in a JSON file.
    const { config, store } = build(fakeVault(false))

    const { ok, reason } = await config.save('linear', fields, { apiKey: 'sk-1' })

    expect(ok).toBe(false)
    expect(reason).toContain('no secure storage available')
    expect(store.write).not.toHaveBeenCalled()
  })

  it('still saves ordinary settings on a system with no secure storage', async () => {
    const { config } = build(fakeVault(false))

    expect((await config.save('linear', fields, { teamId: 'ENG' })).ok).toBe(true)
  })

  it('clears a secret when the field is emptied', async () => {
    // How somebody removes a credential they no longer want stored here.
    const { config } = build()
    await config.save('linear', fields, { apiKey: 'sk-1' })

    await config.save('linear', fields, { apiKey: '' })

    expect(await config.resolve('linear')).toEqual({})
    expect((await config.describe('linear', fields))[0].set).toBe(false)
  })

  it('omits a secret that will not decrypt rather than passing the ciphertext on', async () => {
    // An extension handed ciphertext would send it somewhere as though it were
    // the key. A missing setting is something it can report; a wrong one is not.
    const store = fakeStore({ config: {}, secrets: { linear: { apiKey: 'not base64!!' } } })
    const { config } = build(fakeVault(), store)

    expect(await config.resolve('linear')).toEqual({})
  })

  it('merges an update rather than replacing everything', async () => {
    const { config } = build()
    await config.save('linear', fields, { teamId: 'ENG', limit: 5 })

    await config.save('linear', fields, { limit: 9 })

    expect(await config.get('linear')).toEqual({ teamId: 'ENG', limit: 9 })
  })

  it('refuses an invalid submission without writing anything', async () => {
    const { config, store } = build()

    expect((await config.save('linear', fields, { limit: 'lots' })).ok).toBe(false)
    expect(store.write).not.toHaveBeenCalled()
  })

  it('forgets everything on uninstall', async () => {
    const { config, store } = build()
    await config.save('linear', fields, { teamId: 'ENG', apiKey: 'sk-1' })

    await config.forget('linear')

    expect(await config.get('linear')).toEqual({})
    expect(await config.resolve('linear')).toEqual({})
    expect(store.saved.secrets.linear).toBeUndefined()
  })

  it('starts empty when there is nothing saved yet', async () => {
    const store = { read: vi.fn(async () => { throw new Error('ENOENT') }), write: vi.fn() }
    const { config } = build(fakeVault(), store)

    expect(await config.get('linear')).toEqual({})
  })

  it('copes with a stored shape it does not recognise', async () => {
    const store = fakeStore({})
    const { config } = build(fakeVault(), store)

    expect(await config.get('linear')).toEqual({})
    expect(await config.describe('linear', fields)).toEqual([
      { key: 'apiKey', label: 'API key', set: false },
    ])
  })
})
