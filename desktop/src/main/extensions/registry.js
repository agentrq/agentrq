/**
 * Named registries, and the three rules that make them predictable.
 *
 * Every extension does the same thing: it contributes entries. A page in the
 * sidebar, an action in a workspace header, an item on a task's context menu, a
 * keyboard shortcut, a schedule — all of them are entries in a registry, so
 * adding a surface later is an entry type rather than a subsystem.
 *
 * The shape is borrowed from Cordis, which the DeepSeek Harness is built on and
 * which this project already ships a plugin for. What is deliberately *not*
 * borrowed is its trust model: Cordis hands a plugin real capability with no
 * boundary because its plugins are trusted in-process code. Ours are too, which
 * is a decision made with eyes open — but it means the ergonomics are the part
 * worth copying and nothing here should be mistaken for containment.
 *
 * ## Three rules
 *
 * **Unique names, rejected loudly.** A duplicate registration fails with a
 * message naming both claimants. Silently letting the last one win is how two
 * extensions end up fighting over a shortcut and neither author can tell why.
 *
 * **Explicit order.** Entries carry an `order`, and ties break on owner and id,
 * so what the user sees never depends on which extension happened to load first.
 *
 * **Lazy values.** An entry's value may be a function of context, resolved when
 * something asks. That is what lets a task menu item decide whether it applies
 * to *this* task rather than being a permanent row on every one of them.
 */

/** Where a name has to be unique. */
export const SCOPES = {
  /** One claimant across every extension — shortcuts, tool names. */
  global: 'global',
  /** One claimant per extension — pages, which are addressed by owner and id. */
  owner: 'owner',
}

const fail = (reason) => ({ ok: false, reason })

/** Ordered, then stable. Never dependent on load sequence. */
function byOrderThenName(a, b) {
  return a.order - b.order || a.owner.localeCompare(b.owner) || a.id.localeCompare(b.id)
}

/**
 * One registry.
 *
 * @param {object} options
 * @param {string} options.name    What it holds, for the messages.
 * @param {'global'|'owner'} [options.scope]
 * @param {string} [options.uniqueBy]  The field that has to be unique, when it
 *   is not the id. A shortcut is claimed by its *key*: two extensions binding
 *   `x s` collide however they name their entries, and two entries an extension
 *   happens to call `open` do not.
 */
export function createRegistry({ name, scope = SCOPES.global, uniqueBy = 'id' }) {
  /** @type {Map<string, object>} keyed by whatever `scope` makes unique. */
  const entries = new Map()

  /**
   * What has to be unique, normalised.
   *
   * A key is folded, so `S` and `s` are one claim rather than two — the
   * dispatcher lowercases what it reads from the keyboard, and a registry that
   * did not would hand out a second claim on a key already spoken for. An id is
   * left as written, because it is an address rather than a keystroke.
   */
  const claimOf = (value) => {
    const text = String(value ?? '').trim()
    return uniqueBy === 'id' ? text : text.toLowerCase()
  }

  const keyFor = (owner, claim) => (scope === SCOPES.owner ? `${owner}:${claim}` : claim)

  return {
    name,
    scope,

    /**
     * Claim a name.
     *
     * The message names the extension already holding it, because "duplicate
     * id" tells an author nothing they can act on — the useful fact is *who*
     * they are colliding with.
     */
    add(owner, entry) {
      const id = String(entry?.id ?? '').trim()
      if (!owner) return fail(`Cannot register a ${name} without an extension.`)
      if (!id) return fail(`A ${name} needs an id.`)

      const claim = claimOf(uniqueBy === 'id' ? id : entry?.[uniqueBy])
      if (!claim) return fail(`A ${name} needs a ${uniqueBy}.`)

      const key = keyFor(owner, claim)
      const existing = entries.get(key)
      if (existing) {
        return existing.owner === owner
          ? fail(`${owner} registers the ${name} "${claim}" twice.`)
          : fail(`${owner} cannot register the ${name} "${claim}": ${existing.owner} already has it.`)
      }

      entries.set(key, {
        ...entry,
        id,
        owner,
        // Defaulted rather than required: most entries have no opinion, and
        // making every author pick a number would produce arbitrary ones.
        order: Number.isFinite(entry?.order) ? entry.order : 100,
      })
      return { ok: true }
    },

    /** Everything registered, in the order it should appear. */
    list() {
      return [...entries.values()].sort(byOrderThenName)
    },

    /** Just this extension's, which is what an uninstall needs to know. */
    listFor(owner) {
      return this.list().filter((entry) => entry.owner === owner)
    },

    /**
     * Everything registered, with each lazy value resolved against `context`.
     *
     * An entry whose value throws is **dropped, not propagated**. One extension
     * deciding badly must not empty a menu that other extensions are also in,
     * and the failure is reported so it can be counted against that extension
     * rather than disappearing.
     */
    resolve(context, { onError = () => {} } = {}) {
      return this.list().flatMap((entry) => {
        if (typeof entry.value !== 'function') return [entry]
        try {
          const value = entry.value(context)
          return value === undefined ? [] : [{ ...entry, value }]
        } catch (error) {
          onError(entry.owner, error)
          return []
        }
      })
    },

    /** Give up every name this extension holds. */
    removeOwner(owner) {
      let removed = 0
      for (const [key, entry] of entries) {
        if (entry.owner === owner) {
          entries.delete(key)
          removed += 1
        }
      }
      return removed
    },

    /** Whether a name is already taken, and by whom. */
    claimedBy(owner, id) {
      return entries.get(keyFor(owner, claimOf(id)))?.owner ?? ''
    },
  }
}

/**
 * The set of registries the host offers.
 *
 * Named here rather than created ad hoc so that `inject` can be checked against
 * something: an extension asking for a registry that does not exist should be
 * told at load, not discover it when a call returns undefined.
 */
export function createRegistries() {
  return {
    // Addressed as /extensions/:name/:pageId, so a page only has to be unique
    // within the extension that owns it.
    ui: createRegistry({ name: 'view', scope: SCOPES.owner }),
    // A key sequence has one meaning, whoever asked for it — so the *key* is
    // what is claimed here, not the id. Keying it by id let two extensions bind
    // the same letter, and made two extensions that both called their entry
    // "open" collide over nothing.
    shortcuts: createRegistry({ name: 'shortcut', scope: SCOPES.global, uniqueBy: 'key' }),
    // A schedule is reconciled by id against what already exists on the server.
    schedules: createRegistry({ name: 'schedule', scope: SCOPES.owner }),
    // A fenced-code language has one renderer, whoever asked first — two
    // extensions both drawing ```mermaid would be resolved by load order,
    // which is no answer at all.
    renderers: createRegistry({ name: 'renderer', scope: SCOPES.global, uniqueBy: 'language' }),
  }
}
