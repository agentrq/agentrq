// Copyright 2026 Contextual, Inc. https://agentrq.com

import { checkCompatibility, parseManifest } from './manifest.js'
import { checkShortcuts, requestedKeys } from './shortcuts.js'
import { releaseSource } from './source.js'
import { entriesFor, invokeEntry, registryFor } from './surfaces.js'

/**
 * The order things have to happen in, and the one place that knows it.
 *
 * Every piece of this feature was built and tested on its own: the installer
 * writes to disk, the host loads a module, the broker holds a grant, the
 * reconciler puts standing work on the server. None of them knows about the
 * others, which is what made each one testable — and it means installing an
 * extension is a *sequence*, and getting that sequence wrong is how you end up
 * with an extension that is loaded but was never granted anything, or one that
 * was uninstalled and left a cron task running every night.
 *
 * This is that sequence, and it lives here rather than in `index.js` for one
 * reason: `index.js` imports Electron at module scope and is excluded from
 * coverage, so anything that ends up in it is code nobody can test. What belongs
 * there is wiring — a dialog, a path, a session — and what belongs here is every
 * decision about what happens when.
 *
 * ## A grant is checked against the manifest on every load
 *
 * `broker.js` says the tool check is against "the manifest's `mcp` allowlist",
 * and it is really against the grant — which was *built* from a manifest and
 * then outlives it. A linked installation is a folder somebody can edit, and a
 * grant is persisted across restarts, so the two drift. `clampToManifest`
 * intersects them on every load, in the one direction that is safe to do
 * without asking: narrower, never wider.
 *
 * ## Install is two steps, because a grant is a question
 *
 * `inspect` reads a folder and answers what is there and what it would ask for.
 * `install` is called afterwards with what the user agreed to. Splitting them is
 * what lets the grant screen be a screen: a single call would have to either
 * install first and ask later — which is not a permission — or take a grant for
 * a manifest nobody had read yet.
 *
 * ## Taking one away is the sequence run backwards, and every step matters
 *
 * Stop it, so nothing is mid-call. Remove its standing work, **while the grant
 * is still there**, because reconciliation needs it. Then revoke, forget the
 * settings, and remove the installation. A different order leaves something
 * behind: revoking first means the reconciler cannot delete what it created, and
 * the cron task outlives the extension with nothing left that remembers making
 * it.
 */

const fail = (reason) => ({ ok: false, reason })

/**
 * Narrows a grant to what the installed manifest actually asks for.
 *
 * A grant is written once, at install, from the manifest the user was shown —
 * and then it outlives that manifest. It is persisted across restarts, and a
 * **linked** installation is a folder on disk that can be edited at any moment,
 * including its `agentrq-extension.json`. So the list the broker enforces and
 * the list the install screen displayed can drift apart, in the direction that
 * matters: an extension keeps a tool it no longer declares, and nothing on any
 * screen says so.
 *
 * Nothing here widens anything — a manifest asking for more than was granted
 * gets no more than was granted, because only a person can do that, at an
 * install screen. This is the other direction, and it is cheap: an intersection
 * on every load, so the enforced grant is never broader than what is currently
 * written down and shown.
 *
 * The workspace list is left alone. Which workspaces an extension may reach is
 * the user's answer to a question, not something the manifest has an opinion
 * about.
 */
export function clampToManifest(grant, manifest) {
  if (!grant) return null

  const declared = {
    workspace: manifest?.mcp?.workspace ?? [],
    supervisor: manifest?.mcp?.supervisor ?? [],
  }
  const keep = (granted = [], asked) => granted.filter((tool) => asked.includes(tool))

  return {
    ...grant,
    tools: {
      workspace: keep(grant.tools?.workspace, declared.workspace),
      supervisor: keep(grant.tools?.supervisor, declared.supervisor),
    },
  }
}

/**
 * What `inspect` reports about a folder.
 *
 * Deliberately more than "is it valid": the screen that follows has to show what
 * the extension is, what it would be allowed to reach, and — before anybody
 * agrees to anything — whether it can run here at all. Answering those in one
 * pass is what keeps the grant screen from being able to offer a grant for
 * something that was never going to load.
 */
export function describeCandidate(source, { appVersion, workspaceTools, supervisorTools, taken = [] } = {}) {
  const parsed = parseManifest(source)
  if (!parsed.ok) return fail(parsed.reason)

  const manifest = parsed.manifest
  const compatibility = checkCompatibility(manifest, { appVersion, workspaceTools, supervisorTools })

  // Checked here rather than at load, because a key that is already taken is
  // something a person can act on now — and a shortcut that silently does
  // nothing is the one failure no amount of looking at the screen explains.
  const shortcuts = checkShortcuts(manifest, taken)

  return {
    ok: true,
    manifest,
    compatible: compatibility.compatible,
    reasons: compatibility.reasons,
    shortcutProblems: shortcuts.problems,
    keys: requestedKeys(manifest),
  }
}

/**
 * @param {object} deps
 * @param {object} deps.installer   From `install.js`.
 * @param {object} deps.host        From `host.js`.
 * @param {object} deps.broker      From `broker.js`.
 * @param {object} deps.schedules   From `schedules.js`.
 * @param {object} deps.configStore From `config.js`.
 * @param {() => Promise<string|null>} deps.readManifest  Reads a folder's manifest.
 * @param {() => object} [deps.servers]  The app version and the live tool lists.
 * @param {{info: Function, warn: Function}} [deps.logger]
 */
export function createRuntime({
  installer,
  host,
  broker,
  schedules,
  configStore,
  readManifest,
  /**
   * Reads a drawer's file out of an installation's directory.
   *
   * Injected rather than imported so the rule about *where* a file may be —
   * inside the directory it was installed into, symlinks included — is checked
   * by a test with no filesystem in it.
   *
   * Required, with no default. A no-op default would turn a forgotten wire into
   * "no extension ever draws anything" — which looks like a feature that does
   * not work rather than a dependency nobody passed.
   *
   * @type {(dir: string, entry: string) => Promise<{ok: true, code: string} | {ok: false, reason: string}>}
   */
  readDrawer,
  servers = () => ({}),
  logger = console,
}) {
  /** Grants live with the installation, so they survive a restart. */
  const grants = new Map()

  /** Every shortcut already spoken for, so a new extension can be told. */
  function takenKeys(except = '') {
    return host
      .registries.shortcuts.list()
      .filter((entry) => entry.owner !== except)
      .map((entry) => ({ key: entry.key, owner: entry.owner }))
  }

  /**
   * Loads one installation and makes its standing work true.
   *
   * The grant is set **before** the load, not after. `apply` runs during the
   * load and an extension is entitled to call MCP from it; a grant applied
   * afterwards would refuse exactly those calls, and the extension author would
   * see a permission error for a permission the user had granted.
   */
  /**
   * The half of an install that is the same whichever source it came from:
   * store the settings, record the grant, and start it.
   *
   * Shared rather than written twice, because the order matters — settings are
   * written before the extension runs, and a refused write undoes the install
   * rather than leaving one configured with half its values.
   */
  async function settle(installed, manifest, { grant = null, config = null } = {}) {
    const name = manifest.name

    if (config) {
      // Always an array: `parseManifest` normalises it, and this manifest came
      // from there.
      const saved = await configStore.save(name, manifest.config, config)
      // Refused rather than written in the clear — a machine with no secure
      // storage does not get a plaintext credential — and refusing the whole
      // install is right, because an extension configured with half its
      // settings is one that fails somewhere less obvious.
      if (!saved.ok) {
        await installer.uninstall(name)
        return saved
      }
    }

    if (grant) grants.set(name, grant)
    const started = await start({ ...installed.installation, grant: grants.get(name) ?? null })
    if (!started.ok) return { ok: true, installation: installed.installation, loadFailure: started.reason }

    return { ok: true, installation: installed.installation, ...started }
  }

  async function start(installation) {
    const grant = clampToManifest(installation.grant ?? grants.get(installation.name), installation.manifest)
    if (grant) {
      grants.set(installation.name, grant)
      broker.setGrant(installation.name, grant)
    }

    const started = await host.start(installation)
    if (!started.ok) {
      // Counted against the installation as well as the host: the host's own
      // count lives in memory and this one has to survive a restart, or an
      // extension that crashes the app on load would be retried forever.
      await installer.recordFailure(installation.name)
      return started
    }

    const declared = host.resolve('schedules', {})
      .filter((entry) => entry.owner === installation.name)
    const reconciled = await schedules.reconcile(installation.name, declared)
    const scheduleProblems = reconciled.problems ?? []
    // Reported, not fatal. An extension whose schedule was refused still has its
    // pages and its shortcuts, and taking those away over a cron field helps
    // nobody — but it must not look like it worked.
    for (const problem of scheduleProblems) {
      logger.warn?.(`[${installation.name}] ${problem.reason}`)
    }

    return { ok: true, registered: started.registered, scheduleProblems }
  }

  return {
    /**
     * What is contributed to one surface, as messages rather than entries.
     *
     * Here rather than in `index.js` for the reason at the top of this file: a
     * predicate runs on this side because it cannot run on the other, and that
     * is a decision, not wiring. It lived in the wrapper `index.js` builds until
     * a verification script asked for it and found nothing there.
     */
    entries(surface, context = {}) {
      return entriesFor(host.resolve(registryFor(surface), context), surface, context, {
        onError: (owner, error) =>
          logger.warn?.(`[${owner}] failed while deciding a ${surface}:`, error?.message ?? error),
      })
    },

    /** Run one of them, and answer with what it drew. */
    invoke(target = {}, context = {}) {
      return invokeEntry(host.resolve(registryFor(target.surface), context), target, context)
    },

    /** What is installed, with what each one contributed. */
    async state() {
      const installations = await installer.list()
      const loaded = new Map(host.list().map((entry) => [entry.name, entry]))

      return installations.map((installation) => {
        const contributions = Object.entries(host.registries).map(([registry, target]) => [
          registry,
          target.listFor(installation.name).length,
        ])

        return {
          name: installation.name,
          version: installation.version,
          displayName: installation.manifest?.displayName ?? installation.name,
          description: installation.manifest?.description ?? '',
          license: installation.manifest?.license ?? '',
          enabled: installation.enabled !== false,
          // Whether it is *running*, which is not the same as enabled: an
          // enabled extension that threw on load is neither, and a row that
          // conflated the two would have nothing to explain the difference.
          loaded: loaded.has(installation.name),
          failures: installation.failures ?? 0,
          source: installation.sourceLabel ?? '',
          // What was recorded, not what the source kind implies: a folder can
          // be linked or copied, and only the linked one changes underneath the
          // app.
          linked: installation.linked ?? installation.source?.kind === 'local',
          grant: broker.grantFor(installation.name),
          contributes: Object.fromEntries(contributions),
        }
      })
    },

    /**
     * Read a folder without installing anything from it.
     *
     * Answers with everything the grant screen needs, so that screen never has
     * to reach back into the main process for a second opinion.
     */
    async inspect(path) {
      let source
      try {
        source = await readManifest(path)
      } catch (error) {
        return fail(`Could not read that folder: ${error?.message ?? error}`)
      }
      if (source === null || source === undefined) {
        return fail('There is no agentrq-extension.json in that folder.')
      }

      const described = describeCandidate(source, { ...servers(), taken: takenKeys() })
      return described.ok ? { ...described, path } : described
    },

    /**
     * Install from a local folder, with the grant the user agreed to.
     *
     * **Linked**, always, for a folder. Recording it rather than copying is what
     * makes developing an extension tolerable — editing the folder is editing
     * the installed extension — and it is marked linked precisely because it can
     * change underneath the app, unlike everything else that gets installed.
     */
    async installLocal(path, { grant = null, config = null } = {}) {
      const inspected = await this.inspect(path)
      if (!inspected.ok) return inspected
      // `reasons` is never empty when `compatible` is false — `checkCompatibility`
      // pushes one before it can say no — so there is nothing to fall back to.
      if (!inspected.compatible) return fail(inspected.reasons.join(' '))
      if (inspected.shortcutProblems.length > 0) {
        return fail(inspected.shortcutProblems.join(' '))
      }

      const name = inspected.manifest.name
      // Replacing what is there rather than installing beside it: the name is an
      // address, so two of them cannot coexist. Stopping first means the old one
      // is not still registered when the new one claims the same names.
      if (host.list().some((entry) => entry.name === name)) host.stop(name)

      const installed = await installer.install({ kind: 'local', path }, { linked: true })
      if (!installed.ok) return installed

      return settle(installed, inspected.manifest, { grant, config })
    },

    /**
     * Install a catalogue entry from the release its manifest names.
     *
     * The counterpart to `installLocal`, and deliberately **not** linked: a
     * downloaded release is a copy this app owns, so removing the extension
     * removes it. A folder is the author's own working copy and is only
     * recorded.
     *
     * The entry is passed in rather than looked up here because the caller
     * resolves it from its own index — nothing a renderer sends is trusted to
     * describe what gets downloaded, and the digest in the manifest is what
     * decides whether the bytes are the ones that were promised.
     */
    async installFromCatalogue(entry, { grant = null, config = null } = {}) {
      const built = releaseSource(entry)
      if (!built.ok) return built

      if (!entry?.ok) return fail(entry?.reason ?? 'That manifest could not be read.')
      // `reasons` is never empty when `compatible` is false — `decorate` sets the
      // two together from `checkCompatibility`, which pushes one before it can
      // say no — so there is nothing to fall back to.
      if (entry.compatible === false) return fail(entry.reasons.join(' '))

      const name = entry.manifest?.name
      if (!name) return fail('That catalogue entry names no extension.')

      // The name is an address, so a reinstall replaces what is there rather
      // than installing beside it. Stopping first means the old one is not still
      // registered when the new one claims the same names.
      if (host.list().some((installed) => installed.name === name)) host.stop(name)

      const installed = await installer.install(built.source)
      if (!installed.ok) return installed

      // The manifest inside the downloaded package, not the catalogue's copy of
      // it: the digest proves they are the same bytes, and the one that was
      // actually unpacked is the one whose settings get saved.
      return settle(installed, installed.installation.manifest ?? entry.manifest, { grant, config })
    },

    /**
     * The code for a drawer, or why there is none.
     *
     * Looked up by format across what is installed and enabled, and read from
     * the installation's own directory. The renderer sends a format and gets
     * text back — it never names a path, because a path from the page is a path
     * somebody could aim.
     *
     * The entry was already checked at parse time (`parseDrawerEntry`), and it
     * is checked again here against the resolved directory. Two guards that
     * fail differently: one is a rule about the manifest, the other is a fact
     * about the filesystem, and a symlink is only visible to the second.
     */
    /**
     * Whether anything installed draws a format, without reading the file.
     *
     * What the renderer asks, and all it needs: the answer decides whether a
     * block shows a frame or a sentence. The code itself is fetched by the
     * frame from a URL — a real drawer is megabytes, and sending that over the
     * bridge to answer a yes-or-no question would be absurd.
     */
    async hasDrawer(format) {
      const wanted = String(format ?? '').toLowerCase()
      if (!wanted) return fail('No format was named.')

      for (const installation of await installer.list()) {
        if (installation.enabled === false) continue
        const declared = (installation.manifest?.provides?.drawers ?? []).some(
          (drawer) => drawer.format === wanted,
        )
        if (declared) return { ok: true, owner: installation.name }
      }
      return fail(`Nothing installed draws "${wanted}".`)
    },

    async drawerFor(format) {
      const wanted = String(format ?? '').toLowerCase()
      if (!wanted) return fail('No format was named.')

      for (const installation of await installer.list()) {
        if (installation.enabled === false) continue

        const declared = (installation.manifest?.provides?.drawers ?? []).find(
          (drawer) => drawer.format === wanted,
        )
        if (!declared) continue

        const read = await readDrawer(installation.dir, declared.entry)
        // Named rather than swallowed: an extension that declares a drawer and
        // ships no file is a broken package, and its author should hear so.
        if (!read.ok) return fail(`${installation.name} declares a "${wanted}" drawer it does not ship: ${read.reason}`)
        return { ok: true, code: read.code, owner: installation.name }
      }

      return fail(`Nothing installed draws "${wanted}".`)
    },

    /**
     * Take one away completely.
     *
     * The order is the install run backwards and every step of it matters; the
     * reasoning is at the top of this file.
     */
    async remove(name) {
      host.stop(name)

      const removed = await schedules.removeOwner(name)
      // Reported and then carried on with. Refusing to uninstall because a cron
      // task could not be deleted would leave somebody unable to remove an
      // extension at all — the record is kept either way, so nothing is
      // forgotten silently.
      const leftBehind = (removed.problems ?? []).map((problem) => problem.reason)

      broker.revoke(name)
      grants.delete(name)
      await configStore.forget(name)

      const uninstalled = await installer.uninstall(name)
      if (!uninstalled.ok) return uninstalled

      return { ok: true, leftBehind }
    },

    /**
     * Turn one off without removing it, or back on.
     *
     * Disabling stops it and takes its standing work down, because an extension
     * that is off should not still be creating tasks at three in the morning —
     * which is exactly what "disabled" would otherwise fail to mean.
     */
    async setEnabled(name, enabled) {
      const result = await installer.setEnabled(name, enabled)
      if (!result.ok) return result

      if (!enabled) {
        host.stop(name)
        await schedules.removeOwner(name)
        return { ok: true, installation: result.installation }
      }

      const started = await start(result.installation)
      return started.ok ? { ok: true, installation: result.installation, ...started } : started
    },

    /** Save settings, and reload so the extension sees them. */
    async configure(name, values) {
      const installations = await installer.list()
      const installation = installations.find((entry) => entry.name === name)
      if (!installation) return fail(`${name} is not installed.`)

      const saved = await configStore.save(name, installation.manifest?.config ?? [], values)
      if (!saved.ok) return saved

      // Reloaded rather than left running: `apply` reads config once, so an
      // extension carrying on with the old values would show a settings screen
      // that appears to have done nothing.
      host.stop(name)
      return start(installation)
    },

    /**
     * Load everything that should be running, at startup.
     *
     * One extension failing does not stop the next: an app that gives up on the
     * first broken extension is one where a single bad install takes away
     * everything else somebody had.
     */
    async startAll() {
      const installations = await installer.list()
      const results = []

      for (const installation of installations) {
        if (installation.enabled === false) continue
        const started = await start(installation)
        if (!started.ok) logger.warn?.(`[${installation.name}] did not start: ${started.reason}`)
        results.push({ name: installation.name, ok: started.ok, reason: started.reason })
      }
      return results
    },

    /** Remember a grant read back from disk, without loading anything. */
    rememberGrant(name, grant) {
      if (!grant) return
      grants.set(name, grant)
      broker.setGrant(name, grant)
    },

    /** Every grant currently held, for persisting them. */
    grants: () => Object.fromEntries(grants),

    /** Unload everything, for shutdown. Leaves the server alone. */
    stopAll() {
      host.stopAll()
    },
  }
}
