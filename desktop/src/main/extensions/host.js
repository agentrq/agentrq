import { createRegistries } from './registry.js'

/**
 * Loading an extension, and taking it away again.
 *
 * An extension is a Node module exporting the Cordis-like contract:
 *
 *     export const name = 'linear'
 *     export const inject = ['ui', 'shortcuts']
 *     export function apply(ctx, config) { … }
 *
 * `apply` is called once with a context carrying exactly the registries it
 * declared, and everything it registers is remembered against its name — so
 * unloading is complete by construction rather than by an author remembering to
 * tidy up.
 *
 * ## This is trusted code, and nothing here pretends otherwise
 *
 * An extension runs with the privileges of the process it is in. There is no
 * sandbox: that was considered and deliberately not built, because the value of
 * this design is the ordinary Node ecosystem and a sandbox costs exactly that.
 *
 * What *is* enforced is everything AgentRQ owns — which registries an extension
 * can reach, which names it may claim, and which MCP tools it may call. That is
 * a real boundary around this application's data. It is not a boundary around
 * the machine, and the install screen says so in those words.
 *
 * The one thing this must never do is run in the renderer. That is where the
 * `window.agentrq` bridge exposes files, the clipboard and the shell, and
 * third-party code next to it would have the whole machine through an API built
 * for the app's own UI.
 *
 * ## Failure is loud
 *
 * An extension that throws on load, or repeatedly at runtime, is disabled and
 * the user is told. An app that keeps reloading something broken degrades for
 * reasons nobody can see from the outside.
 */

/** How many failures before an extension is switched off. */
export const FAILURE_LIMIT = 3

const fail = (reason) => ({ ok: false, reason })

/**
 * Checks the module actually looks like an extension before calling into it.
 *
 * A clear refusal here is worth a great deal: the alternative is a TypeError
 * from inside `apply` that names a line number in somebody else's code.
 */
export function validateModule(module, expectedName) {
  if (!module || typeof module !== 'object') return fail('This extension exports nothing.')
  if (typeof module.apply !== 'function') {
    return fail('This extension exports no apply(ctx, config) function.')
  }
  const declared = String(module.name ?? '').trim()
  if (declared && declared !== expectedName) {
    // Worth catching: the manifest name is what everything else addresses it
    // by, so a module disagreeing means half the wiring would point elsewhere.
    return fail(`This extension calls itself "${declared}" but its manifest says "${expectedName}".`)
  }
  const inject = Array.isArray(module.inject) ? module.inject.map(String) : []
  return { ok: true, inject }
}

/**
 * The context an extension is given.
 *
 * Only the registries it declared in `inject`. Handing over everything would
 * make `inject` decorative, and the declaration is what lets the install screen
 * say what an extension touches before it is ever run.
 */
export function buildContext({ name, registries, inject, config, logger }) {
  const missing = inject.filter((key) => !(key in registries))
  if (missing.length > 0) {
    return fail(`This extension asks for something that does not exist: ${missing.join(', ')}.`)
  }

  const ctx = {
    name,
    config,
    logger: {
      info: (...args) => logger.info?.(`[${name}]`, ...args),
      warn: (...args) => logger.warn?.(`[${name}]`, ...args),
    },
  }

  for (const key of inject) {
    const registry = registries[key]
    ctx[key] = {
      /**
       * Registering throws rather than returning a result.
       *
       * `apply` is a procedure, and an author writing one will not check a
       * return value they did not know to expect. A throw is caught by the
       * host, reported against the extension, and abandons a load that was
       * going to be half-applied anyway.
       */
      add(entry) {
        const result = registry.add(name, entry)
        if (!result.ok) throw new Error(result.reason)
        return ctx[key]
      },
    }
  }
  return { ok: true, ctx }
}

/**
 * @param {object} deps
 * @param {(installation: object) => Promise<object>} deps.load  Imports the module.
 * @param {(name: string) => Promise<object>} deps.readConfig
 * @param {(name: string) => Promise<void>} [deps.onDisabled]
 * @param {{info: Function, warn: Function}} [deps.logger]
 */
export function createHost({ load, readConfig, onDisabled = async () => {}, logger = console }) {
  const registries = createRegistries()
  /** @type {Map<string, object>} what is loaded right now. */
  const loaded = new Map()
  /**
   * How many times each extension has failed.
   *
   * Kept apart from `loaded` deliberately. A failed load retracts everything the
   * extension registered, which includes forgetting it is loaded — and if the
   * count lived there it would be forgotten with it, resetting to zero on every
   * attempt so the limit could never be reached.
   */
  const failures = new Map()

  /**
   * Undo everything an extension did.
   *
   * Registry entries are keyed by owner precisely so this is complete without
   * the extension participating — an author who forgot to clean up, or whose
   * `apply` threw halfway through, still leaves nothing behind.
   */
  function retract(name) {
    let removed = 0
    for (const registry of Object.values(registries)) removed += registry.removeOwner(name)
    loaded.delete(name)
    return removed
  }

  return {
    registries,

    /** What is loaded right now. */
    list() {
      return [...loaded.values()].map((installation) => ({
        name: installation.name,
        version: installation.version,
        failures: failures.get(installation.name) ?? 0,
      }))
    },

    /**
     * Load one installation.
     *
     * A failure at any point retracts whatever was registered before it. A
     * partially applied extension is the worst outcome available: it looks
     * loaded, half its contributions are missing, and nothing says why.
     */
    async start(installation) {
      const { name } = installation
      if (loaded.has(name)) return fail(`${name} is already loaded.`)
      if (installation.enabled === false) return fail(`${name} is disabled.`)

      let module
      try {
        module = await load(installation)
      } catch (error) {
        return this.recordFailure(name, error)
      }

      const shape = validateModule(module, name)
      if (!shape.ok) return this.recordFailure(name, new Error(shape.reason))

      let config = {}
      try {
        config = await readConfig(name)
      } catch (error) {
        logger.warn?.(`[${name}] could not read configuration:`, error)
      }

      const context = buildContext({
        name,
        registries,
        inject: shape.inject,
        config,
        logger,
      })
      if (!context.ok) return this.recordFailure(name, new Error(context.reason))

      loaded.set(name, installation)
      try {
        await module.apply(context.ctx, config)
      } catch (error) {
        retract(name)
        return this.recordFailure(name, error)
      }

      // A load that worked clears the slate. Otherwise two failures months
      // apart would eventually disable something that has been working fine.
      failures.delete(name)
      return { ok: true, registered: Object.values(registries).reduce(
        (n, registry) => n + registry.listFor(name).length,
        0,
      ) }
    },

    /** Unload it, taking every entry it registered with it. */
    stop(name) {
      if (!loaded.has(name)) return fail(`${name} is not loaded.`)
      const removed = retract(name)
      return { ok: true, removed }
    },

    /**
     * Count a failure, disabling the extension once it is clearly not working.
     *
     * Three strikes rather than one: a transient failure — a network blip in a
     * `apply` that fetches something — should not permanently switch off an
     * extension somebody relies on. Three of them is not transient.
     */
    async recordFailure(name, error) {
      const reason = error?.message || String(error ?? 'Unknown error')
      const count = (failures.get(name) ?? 0) + 1
      failures.set(name, count)

      logger.warn?.(`[${name}] failed:`, reason)

      if (count >= FAILURE_LIMIT) {
        retract(name)
        await onDisabled(name, reason)
        return { ok: false, reason, disabled: true }
      }
      return { ok: false, reason, disabled: false }
    },

    /** Everything contributed to one registry, with lazy entries resolved. */
    resolve(registry, context) {
      const target = registries[registry]
      if (!target) return []
      return target.resolve(context, {
        onError: (owner, error) => {
          logger.warn?.(`[${owner}] failed while building a ${target.name}:`, error?.message ?? error)
        },
      })
    },

    /** Unload everything, for shutdown. */
    stopAll() {
      for (const name of [...loaded.keys()]) retract(name)
      failures.clear()
    },
  }
}
