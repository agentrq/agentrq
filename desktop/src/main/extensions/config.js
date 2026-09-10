/**
 * What an extension is configured with, and where its secrets live.
 *
 * The manifest declares the fields; this validates what a user typed against
 * them and keeps the answers. The split matters: ordinary values are readable
 * settings, and secrets are not — an API key belongs in the OS keychain and
 * should never appear in a file somebody could back up, sync or paste.
 *
 * ## Secrets are write-only from the interface
 *
 * A saved secret can be replaced but never read back. There is no legitimate
 * reason for the settings screen to display one, and every reason not to: it
 * would put a credential on screen, in a screenshot, and in whatever the
 * renderer's process memory ends up in. The screen shows whether a value is set,
 * which is the only part a person needs.
 */

const fail = (reason) => ({ ok: false, reason })

/** Coerces and checks one declared field. */
function coerce(field, raw) {
  const value = raw ?? ''

  if (field.type === 'boolean') {
    if (typeof value === 'boolean') return { ok: true, value }
    if (value === 'true' || value === 'false') return { ok: true, value: value === 'true' }
    return fail(`"${field.label}" must be true or false.`)
  }

  if (field.type === 'number') {
    const n = typeof value === 'number' ? value : Number(String(value).trim())
    if (!Number.isFinite(n)) return fail(`"${field.label}" must be a number.`)
    return { ok: true, value: n }
  }

  if (typeof value !== 'string') return fail(`"${field.label}" must be text.`)
  return { ok: true, value: value.trim() }
}

/**
 * Validates a whole submission against the fields a manifest declares.
 *
 * Unknown keys are refused rather than dropped. A settings form that silently
 * discards a value looks like it saved it, and the extension then behaves as
 * though the user never typed anything — which is a much harder thing to
 * diagnose than being told the field does not exist.
 */
export function validateConfig(fields = [], submitted = {}) {
  const known = new Map(fields.map((field) => [field.key, field]))

  const unknown = Object.keys(submitted).filter((key) => !known.has(key))
  if (unknown.length > 0) {
    return fail(`This extension has no setting called ${unknown.map((k) => `"${k}"`).join(', ')}.`)
  }

  const values = {}
  const secrets = {}
  for (const field of fields) {
    if (!(field.key in submitted)) continue

    const result = coerce(field, submitted[field.key])
    if (!result.ok) return result

    // Split at the boundary, so nothing downstream has to remember which is
    // which — the two go to different places and never meet again.
    if (field.type === 'secret') secrets[field.key] = String(result.value)
    else values[field.key] = result.value
  }

  return { ok: true, values, secrets }
}

/**
 * What the settings screen is allowed to know about a secret.
 *
 * Set or not set. Never the value, and never its length — which narrows a guess
 * more than people expect.
 */
export function describeSecrets(fields = [], stored = {}) {
  return fields
    .filter((field) => field.type === 'secret')
    .map((field) => ({ key: field.key, label: field.label, set: Boolean(stored[field.key]) }))
}

/**
 * Config and secrets for every installation.
 *
 * @param {object} deps
 * @param {object} deps.store   { read(), write(state) } for the non-secret half.
 * @param {object} deps.vault   { encrypt(text), decrypt(blob), available() }
 */
export function createConfigStore({ store, vault }) {
  let state = null

  async function load() {
    if (state) return state
    try {
      const raw = await store.read()
      state = { config: raw?.config ?? {}, secrets: raw?.secrets ?? {} }
    } catch {
      state = { config: {}, secrets: {} }
    }
    return state
  }

  return {
    /** The readable settings for one extension. */
    async get(name) {
      await load()
      return { ...(state.config[name] ?? {}) }
    },

    /**
     * Which secrets are set, without revealing any of them.
     *
     * This is what the settings screen renders.
     */
    async describe(name, fields) {
      await load()
      return describeSecrets(fields, state.secrets[name] ?? {})
    },

    /**
     * Save a submission.
     *
     * A secret with no encryption available is **refused rather than written in
     * the clear**. Storing it anyway would be the worst of both worlds: the user
     * believes it is protected, and it is sitting in a JSON file.
     */
    async save(name, fields, submitted) {
      await load()
      const result = validateConfig(fields, submitted)
      if (!result.ok) return result

      const secretKeys = Object.keys(result.secrets)
      if (secretKeys.length > 0 && !vault.available()) {
        return fail(
          'This system has no secure storage available, so the secret was not saved. ' +
            'Nothing here will write a credential to a plain file.',
        )
      }

      const secrets = { ...(state.secrets[name] ?? {}) }
      for (const key of secretKeys) {
        // An emptied field clears the secret rather than storing an empty one,
        // which is how somebody removes a credential they no longer want here.
        secrets[key] = result.secrets[key] ? vault.encrypt(result.secrets[key]) : ''
      }

      state.config[name] = { ...(state.config[name] ?? {}), ...result.values }
      state.secrets[name] = secrets
      await store.write(state)
      return { ok: true }
    },

    /**
     * Everything an extension needs to run, secrets included.
     *
     * Only the host calls this, and only to hand values to the extension it is
     * loading. A secret that will not decrypt is omitted rather than passed
     * through as ciphertext, which an extension would otherwise send somewhere
     * as though it were the key.
     */
    async resolve(name) {
      await load()
      const secrets = {}
      for (const [key, blob] of Object.entries(state.secrets[name] ?? {})) {
        if (!blob) continue
        try {
          secrets[key] = vault.decrypt(blob)
        } catch {
          // Left out entirely. The extension sees a missing setting, which it
          // can report, rather than a value that is wrong in a way it cannot.
        }
      }
      return { ...(state.config[name] ?? {}), ...secrets }
    },

    /** Forget everything about an extension, on uninstall. */
    async forget(name) {
      await load()
      delete state.config[name]
      delete state.secrets[name]
      await store.write(state)
      return { ok: true }
    },
  }
}
