// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Electron's `safeStorage`, with the key kept in the shell.
 *
 * `config.js` and `storage.js` call their vault synchronously — `encrypt` while
 * building the state they are about to write, `decrypt` while reading it — and
 * the keychain is in the shell, one round trip away. So the work moves to the
 * two moments that are already asynchronous: the store's `write` and `read`.
 *
 * - `encrypt` hands back a placeholder and keeps the text in memory; `write`
 *   asks the shell to seal every placeholder in the state before it reaches
 *   the disk. Plain text is never written.
 * - `read` asks the shell to open every sealed value in the file, so `decrypt`
 *   is a lookup.
 * - `available` is the shell's answer, asked when the host starts and again
 *   before each read: no keychain, no secrets, as `safeStorage` on a Linux box
 *   with no keyring.
 */

/** What a value sealed by the shell starts with (`ext_host.rs`'s `SEALED_PREFIX`). */
export const SEALED_PREFIX = 'arqv1:'

/** What a value waiting to be sealed is, in memory only. */
const PENDING = `\u0000agentrq-pending:${Math.random().toString(36).slice(2)}:`

/** Every string anywhere in `value`. */
function strings(value, found = []) {
  if (typeof value === 'string') found.push(value)
  else if (value && typeof value === 'object') for (const item of Object.values(value)) strings(item, found)
  return found
}

/** `value` with every string replaced through `swap`, the original untouched. */
function mapStrings(value, swap) {
  if (typeof value === 'string') return swap(value)
  if (Array.isArray(value)) return value.map((item) => mapStrings(item, swap))
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, mapStrings(item, swap)]))
  }
  return value
}

/**
 * @param {{request: (method: string, params?: object) => Promise<any>}} rpc
 */
export function createVault(rpc) {
  let available = false
  let counter = 0
  /** A placeholder, or a sealed value, to the text it stands for. */
  const plain = new Map()
  /** A placeholder to the value it was sealed as, so it is sealed once. */
  const sealedAs = new Map()

  async function refresh() {
    try {
      available = Boolean((await rpc.request('vault'))?.available)
    } catch {
      available = false
    }
    return available
  }

  async function seal(texts) {
    const answer = await rpc.request('seal', { texts })
    if (!answer?.ok) throw new Error(answer?.reason || 'This system has no secure storage available.')
    return answer.sealed
  }

  async function open(blobs) {
    if (blobs.length === 0) return
    let answer
    try {
      answer = await rpc.request('open', { blobs })
    } catch {
      // Unopened is unreadable: `decrypt` throws for each, which both stores
      // turn into an absent secret rather than ciphertext.
      return
    }
    blobs.forEach((blob, i) => {
      if (typeof answer?.opened?.[i] === 'string') plain.set(blob, answer.opened[i])
    })
  }

  return {
    refresh,

    /** The vault `config.js` and `storage.js` are handed. */
    vault: {
      available: () => available,
      encrypt(text) {
        const token = `${PENDING}${++counter}`
        plain.set(token, String(text))
        return token
      },
      decrypt(blob) {
        if (!plain.has(blob)) throw new Error('This value cannot be read on this machine.')
        return plain.get(blob)
      },
    },

    /** `store` with its secrets sealed on the way out and opened on the way in. */
    wrap(store) {
      return {
        async read() {
          // Asked first: a missing file throws, and the store then saves into it.
          await refresh()
          const raw = await store.read()
          await open([...new Set(strings(raw).filter((s) => s.startsWith(SEALED_PREFIX) && !plain.has(s)))])
          return raw
        },
        async write(state) {
          const waiting = [...new Set(strings(state).filter((s) => s.startsWith(PENDING) && !sealedAs.has(s)))]
          if (waiting.length > 0) {
            const sealed = await seal(waiting.map((token) => plain.get(token)))
            waiting.forEach((token, i) => {
              sealedAs.set(token, sealed[i])
              plain.set(sealed[i], plain.get(token))
            })
          }
          return store.write(mapStrings(state, (s) => (sealedAs.has(s) ? sealedAs.get(s) : s)))
        },
      }
    },
  }
}
