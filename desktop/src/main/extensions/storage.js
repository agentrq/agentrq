// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * Where an extension keeps its own settings — the shape of which is none of
 * AgentRQ's business.
 *
 * `config.js` already stores settings, and it is the wrong tool for this. There,
 * the *manifest* declares the fields, the app renders the form, and the values
 * come back validated against a schema the author wrote months earlier. That is
 * right for an API key and a "look back N hours" number, and hopeless for
 * anything an extension wants to let somebody build: a list of rules, a set of
 * per-workspace overrides, anything with structure.
 *
 * So this is the other half. The extension draws its own screen, decides what a
 * setting even is, and writes whatever it likes. **Nothing here inspects the
 * value**; it is stored and handed back.
 *
 * ## Why AgentRQ owns the container even though the data is the extension's
 *
 * An extension is trusted Node. It can `import fs` and write its settings to a
 * file of its own today, with no permission from anybody — so this is not a
 * capability being granted. It is a container being offered, because writing
 * that file yourself silently gives up four things:
 *
 * - **Uninstall stops meaning anything.** `forget` runs as part of removal and
 *   the data is actually gone. A file an extension wrote outlives it, and
 *   nothing in the app knows it is there to offer.
 * - **Secrets lose the keychain.** A secret written here is refused outright on
 *   a machine with no secure storage rather than written in the clear, which is
 *   a scruple an extension rolling its own has no reason to have thought about.
 * - **Workspaces do not stay apart.** Per-workspace state is the common case
 *   for anything configurable, and an extension keeping one blob and indexing it
 *   by hand gets that wrong in a way nobody notices until two workspaces
 *   disagree.
 * - **Size is unbounded.** A runaway value belongs to the app that has to load
 *   the file at startup.
 *
 * ## Refused, never truncated
 *
 * Same rule as the workspace memory tools, for the same reason: an extension
 * told its value is too large can split it, and one silently cut in half cannot
 * know to.
 *
 * ## What a key and a scope are
 *
 * A key is a short name the extension chooses. A scope is either the
 * installation (`_`) or one workspace (`ws:<id>`), and the extension picks by
 * calling `ctx.storage` or `ctx.storage.workspace(id)` — never by putting the
 * workspace into a key itself, because then only the extension knows the data is
 * per-workspace and the container cannot act on it.
 */

/** As much as one value may be, serialised. Refused above this, never trimmed. */
export const MAX_VALUE_BYTES = 64 * 1024

/** As much as one extension may keep in one scope, across every key. */
export const MAX_SCOPE_BYTES = 512 * 1024

/** A key: short, ordinary, and recognisable in a file somebody opens. */
const KEY_RE = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/

/** The installation-wide scope. Not a workspace id, and cannot collide with one. */
export const INSTALL_SCOPE = '_'

const fail = (reason) => ({ ok: false, reason })

/** How much a value costs, measured the way it is stored. */
export function sizeOf(value) {
  try {
    return new TextEncoder().encode(JSON.stringify(value ?? null)).length
  } catch {
    // Circular, or something holding a BigInt. Not storable at all, and saying
    // so is better than storing a lie about it.
    return -1
  }
}

/**
 * The scope key for a workspace, or the reason there is not one.
 *
 * Workspace ids are base62 monoflakes, so the shape is checkable — and checking
 * it is what keeps `workspace('')` from quietly writing to a scope named
 * `ws:`, which would be one shared bucket wearing a per-workspace name.
 */
export function scopeFor(workspaceId) {
  const id = String(workspaceId ?? '').trim()
  if (!/^[A-Za-z0-9]{1,32}$/.test(id)) return ''
  return `ws:${id}`
}

/**
 * Storage for every installation.
 *
 * @param {object} deps
 * @param {object} deps.store  { read(), write(state) } for the non-secret half.
 * @param {object} deps.vault  { encrypt(text), decrypt(blob), available() }
 * @param {{warn: Function}} [deps.logger]
 */
export function createStorageStore({ store, vault, logger = console }) {
  /** @type {{data: object, secrets: object}|null} */
  let state = null
  /**
   * The read in flight, so a cold store is read once however many callers want
   * it.
   *
   * Without it two `set` calls that arrive before the first read finishes each
   * start their own, each end up holding a *different* state object, and the
   * second write persists a state that never saw the first — one extension's
   * settings silently discarding another's, or a form's second field discarding
   * its first. Every caller here is somebody else's code and none of them
   * awaits the others.
   */
  let reading = null

  async function load() {
    if (state) return state
    if (!reading) {
      reading = (async () => {
        try {
          const raw = await store.read()
          return { data: raw?.data ?? {}, secrets: raw?.secrets ?? {} }
        } catch {
          // No file yet, or one that will not parse. An extension with no
          // stored settings is the state every fresh install is in.
          return { data: {}, secrets: {} }
        }
      })()
    }
    const loaded = await reading
    // Checked again: a caller that woke first may already have installed it,
    // and replacing it here would throw away whatever it has since written.
    if (!state) state = loaded
    return state
  }

  /**
   * Persist, reporting a failure rather than throwing it.
   *
   * The same treatment `configStore` gives a failed write: this is called from
   * inside extension code, and a rejected promise there surfaces as the
   * extension having thrown — which counts a disk problem against its author.
   */
  async function persist() {
    try {
      await store.write(state)
      return { ok: true }
    } catch (error) {
      logger.warn?.('could not write extension storage:', error?.message ?? error)
      return { ok: false, reason: 'That could not be saved.' }
    }
  }

  const bucket = (name, scope) => state.data[name]?.[scope] ?? {}

  function checkKey(key) {
    const value = String(key ?? '').trim()
    if (!KEY_RE.test(value)) {
      return fail(`"${key}" is not a storage key. Use letters, digits, dot, dash or underscore, up to 64 characters.`)
    }
    return { ok: true, key: value }
  }

  return {
    /**
     * The object an extension is handed as `ctx.storage`.
     *
     * Closed over its name, so it cannot read or write another extension's
     * settings — the same arrangement as the broker's client, and for the same
     * reason. It is not containment (this is trusted Node), it is that the
     * ordinary path does the right thing without anybody having to be careful.
     */
    for(name) {
      const at = (scope) => ({
        /** One value, or `undefined` when nothing is stored under that key. */
        async get(key) {
          const checked = checkKey(key)
          if (!checked.ok) return undefined
          await load()
          const held = bucket(name, scope)[checked.key]
          // Copied on the way out. Handing back the stored object would let one
          // caller's edit change what another reads, without a write.
          return held === undefined ? undefined : JSON.parse(JSON.stringify(held))
        },

        /** Everything in this scope, as one object. */
        async all() {
          await load()
          return JSON.parse(JSON.stringify(bucket(name, scope)))
        },

        /** Every key in this scope, for an extension listing what it has. */
        async keys() {
          await load()
          return Object.keys(bucket(name, scope))
        },

        /**
         * Store one value. Any JSON-shaped thing; never inspected.
         *
         * Answers `{ ok, reason }` rather than throwing, so an extension that
         * stored too much gets a sentence it can show somebody instead of an
         * exception its author has to catch to find out about.
         */
        async set(key, value) {
          const checked = checkKey(key)
          if (!checked.ok) return checked
          await load()

          const size = sizeOf(value)
          if (size < 0) return fail(`"${checked.key}" could not be stored: it is not JSON-shaped.`)
          if (size > MAX_VALUE_BYTES) {
            return fail(`"${checked.key}" is ${size} bytes; the most one value may be is ${MAX_VALUE_BYTES}. Split it across keys.`)
          }

          const current = bucket(name, scope)
          const next = { ...current, [checked.key]: JSON.parse(JSON.stringify(value ?? null)) }
          const total = sizeOf(next)
          if (total > MAX_SCOPE_BYTES) {
            return fail(`This would take ${name} past ${MAX_SCOPE_BYTES} bytes of stored settings. Remove something first.`)
          }

          state.data[name] = { ...(state.data[name] ?? {}), [scope]: next }
          return persist()
        },

        /** Remove one value. Removing what is not there is not an error. */
        async delete(key) {
          const checked = checkKey(key)
          if (!checked.ok) return checked
          await load()

          const current = { ...bucket(name, scope) }
          if (!(checked.key in current)) return { ok: true }
          delete current[checked.key]
          // No `?? {}` here, unlike `set`: reaching this line means the key was
          // found, which means this extension already has a bucket.
          state.data[name] = { ...state.data[name], [scope]: current }
          return persist()
        },
      })

      const install = at(INSTALL_SCOPE)

      return {
        ...install,

        /**
         * The same store, for one workspace.
         *
         * A bad id is refused with a store that says so on every call rather
         * than one that writes somewhere shared. Returning `null` would be
         * worse: the extension's next line is `.get`, and a TypeError from
         * inside `apply` is a worse answer than a sentence.
         */
        workspace(workspaceId) {
          const scope = scopeFor(workspaceId)
          if (!scope) {
            const reason = `"${workspaceId}" is not a workspace id.`
            return {
              get: async () => undefined,
              all: async () => ({}),
              keys: async () => [],
              set: async () => fail(reason),
              delete: async () => fail(reason),
            }
          }
          return at(scope)
        },

        /**
         * Secrets, which are write-only by design.
         *
         * `set` and `clear`, and `has` to ask whether one is stored — never a
         * `get` for the renderer's benefit. An extension's own code *can* read
         * them, because it has to use them, and that read happens in the main
         * process where the key was going to be anyway.
         *
         * A machine with no secure storage refuses the write rather than
         * putting a credential in a JSON file the user believes is protected.
         */
        secret: {
          async set(key, value) {
            const checked = checkKey(key)
            if (!checked.ok) return checked
            if (!vault.available()) {
              return fail(
                'This system has no secure storage available, so the secret was not saved. ' +
                  'Nothing here will write a credential to a plain file.',
              )
            }
            await load()
            const held = { ...(state.secrets[name] ?? {}) }
            // An emptied value clears it rather than storing an empty secret,
            // which is how somebody removes a credential they no longer want.
            held[checked.key] = value ? vault.encrypt(String(value)) : ''
            state.secrets[name] = held
            return persist()
          },

          async has(key) {
            const checked = checkKey(key)
            if (!checked.ok) return false
            await load()
            return Boolean(state.secrets[name]?.[checked.key])
          },

          async get(key) {
            const checked = checkKey(key)
            if (!checked.ok) return ''
            await load()
            const blob = state.secrets[name]?.[checked.key]
            if (!blob) return ''
            try {
              return vault.decrypt(blob)
            } catch {
              // Answered as absent rather than as ciphertext. An extension
              // handed a value that will not decrypt would send it somewhere as
              // though it were the key.
              return ''
            }
          },

          async clear(key) {
            const checked = checkKey(key)
            if (!checked.ok) return checked
            await load()
            const held = { ...(state.secrets[name] ?? {}) }
            if (!(checked.key in held)) return { ok: true }
            delete held[checked.key]
            state.secrets[name] = held
            return persist()
          },
        },
      }
    },

    /**
     * Forget everything one extension stored, on uninstall.
     *
     * This is the whole reason the container is the app's rather than the
     * extension's. A write that fails is reported and swallowed, the way every
     * other persistence path here treats one: throwing would throw out of an
     * uninstall that has already stopped the extension and revoked its grant,
     * leaving it listed as installed with no way back short of a restart.
     */
    async forget(name) {
      await load()
      delete state.data[name]
      delete state.secrets[name]
      const written = await persist()
      return written.ok ? { ok: true } : { ok: true, persisted: false }
    },

    /**
     * How much one extension is keeping, for a screen that wants to say so.
     *
     * Per scope, because "this extension is holding 300 KB" is less useful than
     * knowing which workspace it is holding it for.
     */
    async usage(name) {
      await load()
      return Object.entries(state.data[name] ?? {}).map(([scope, values]) => ({
        scope,
        keys: Object.keys(values).length,
        bytes: sizeOf(values),
      }))
    },
  }
}
