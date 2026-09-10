import { parseManifest } from './manifest.js'
import { describeSource, pinFor, verifyDigest } from './source.js'

/**
 * Installing an extension, and everything that happens to one afterwards.
 *
 * Three sources arrive here — a published release asset, a repository the user
 * cloned, or a folder on this machine — and after this point they are one thing:
 * a directory, a manifest, and a record of where it came from.
 *
 * ## Nothing is unpacked before it is verified
 *
 * A release asset is checked against the digest in its manifest *before* it is
 * written anywhere. Git tags are mutable, so `v1.2.0` today need not be
 * `v1.2.0` tomorrow, and a mismatch is refused outright rather than warned
 * about — it means the bytes are not what the manifest described, which is
 * either a tampered release or a corrupt download and neither is a thing to
 * click past.
 *
 * ## A private repository needs no credentials from us
 *
 * The user pastes a URL and `git` does the authenticating, with whatever SSH key
 * or credential helper they already have. Nothing here ever sees a token, which
 * is both less to secure and less to get wrong — and it is what makes a private
 * extension work at all without a registry.
 *
 * ## An install that fails leaves nothing behind
 *
 * Every path stages into a temporary directory and moves it into place only once
 * everything has succeeded. A half-written extension directory is worse than no
 * extension: it looks installed, loads partially, and fails somewhere far from
 * the install that caused it.
 */

/**
 * What went wrong, as a sentence.
 *
 * One helper rather than the same expression at every catch: not everything
 * thrown is an Error, and a reason reading "[object Object]" is worse than
 * useless in a message a user is expected to act on.
 */
export function reasonFrom(error) {
  const message = error?.message
  if (typeof message === 'string' && message) return message
  // Only a primitive is worth stringifying. String() on an object gives
  // "[object Object]", and on an Error with no message it gives a bare "Error"
  // — both of which look like a message while telling nobody anything.
  if (typeof error === 'string' && error.trim()) return error
  return 'Unknown error'
}

/** What an installation records. Desktop-local; nothing about this is on a server. */
function record(manifest, source, pin, { enabled = true } = {}) {
  return {
    name: manifest.name,
    version: manifest.version,
    source,
    sourceLabel: describeSource(source),
    pin,
    manifest,
    enabled,
    failures: 0,
    installedAt: null,
  }
}

/**
 * Whether an update asks for more than what was granted.
 *
 * This is what stops an extension acquiring supervisor access in a patch release
 * nobody read. An update that asks for the same or less installs quietly; one
 * that asks for anything new has to go back to the user.
 *
 * Narrowing is deliberately not a prompt. Being asked to re-approve an extension
 * that now wants *less* teaches people that the prompt is noise, and the whole
 * value of the prompt is that it is rare enough to read.
 */
export function scopesWidened(before, after) {
  const added = (from = [], to = []) => to.filter((tool) => !from.includes(tool))

  const workspace = added(before?.workspace, after?.workspace)
  const supervisor = added(before?.supervisor, after?.supervisor)

  return {
    widened: workspace.length > 0 || supervisor.length > 0,
    workspace,
    supervisor,
    // Called out separately because it is a different order of grant: the
    // supervisor reaches every workspace on the account, not just this one.
    reachesAllWorkspaces: (before?.supervisor?.length ?? 0) === 0 && supervisor.length > 0,
  }
}

/**
 * @param {object} deps
 * @param {(source: object, into: string) => Promise<{dir: string, commit?: string, sha256?: string}>} deps.fetchSource
 * @param {(dir: string) => Promise<string|null>} deps.readManifest
 * @param {(from: string, to: string) => Promise<void>} deps.move
 * @param {(dir: string) => Promise<void>} deps.remove
 * @param {() => Promise<string>} deps.makeTempDir
 * @param {(name: string) => string} deps.dirFor
 * @param {object} deps.store   { read(): Promise<object>, write(state): Promise<void> }
 * @param {() => number} [deps.now]
 */
export function createInstaller({
  fetchSource,
  readManifest,
  move,
  remove,
  makeTempDir,
  dirFor,
  store,
  now = () => Date.now(),
}) {
  let state = null

  async function load() {
    if (state) return state
    try {
      const raw = await store.read()
      state = { installations: Array.isArray(raw?.installations) ? raw.installations : [] }
    } catch {
      state = { installations: [] }
    }
    return state
  }

  const find = (name) => state.installations.find((i) => i.name === name)

  async function persist() {
    try {
      await store.write(state)
    } catch {
      // The extension is installed on disk either way. Losing the record is bad
      // but recoverable; throwing here would abandon a completed install and
      // leave exactly the half-state this whole function avoids.
    }
  }

  /** Stages, verifies and swaps in. Cleans up whatever it made if anything fails. */
  async function stage(source) {
    const temp = await makeTempDir()
    try {
      const fetched = await fetchSource(source, temp)

      if (source.kind === 'release') {
        const digest = verifyDigest(source.sha256, fetched.sha256)
        if (!digest.ok) return { ok: false, reason: digest.reason, temp }
      }

      const manifestSource = await readManifest(fetched.dir)
      if (manifestSource === null) {
        return { ok: false, reason: 'There is no agentrq-extension.json in this extension.', temp }
      }

      const parsed = parseManifest(manifestSource)
      if (!parsed.ok) return { ok: false, reason: parsed.reason, temp }

      return { ok: true, temp, dir: fetched.dir, manifest: parsed.manifest, commit: fetched.commit }
    } catch (error) {
      return { ok: false, reason: reasonFrom(error), temp }
    }
  }

  async function discard(temp) {
    try {
      await remove(temp)
    } catch {
      // A temporary directory nobody could delete is untidy, not broken, and is
      // not worth turning a clear failure message into a confusing one.
    }
  }

  return {
    /** Everything installed on this machine. */
    async list() {
      await load()
      return state.installations.map((i) => ({ ...i }))
    },

    /**
     * Install from any of the three sources.
     *
     * A **linked** local folder is recorded rather than copied, so editing it is
     * editing the installed extension — which is the only way developing one is
     * tolerable. It is marked as linked precisely because it can change
     * underneath the app, unlike everything else here.
     */
    async install(source, { linked = false } = {}) {
      await load()

      if (source.kind === 'local' && linked) {
        const manifestSource = await readManifest(source.path)
        if (manifestSource === null) {
          return { ok: false, reason: 'There is no agentrq-extension.json in that folder.' }
        }
        const parsed = parseManifest(manifestSource)
        if (!parsed.ok) return { ok: false, reason: parsed.reason }

        return commit(parsed.manifest, source, pinFor(source), source.path)
      }

      const staged = await stage(source)
      if (!staged.ok) {
        await discard(staged.temp)
        return { ok: false, reason: staged.reason }
      }

      const target = dirFor(staged.manifest.name)
      try {
        // Whatever was there is removed only once the replacement is staged and
        // verified, so a failed update cannot leave the machine with neither.
        await remove(target)
        await move(staged.dir, target)
      } catch (error) {
        await discard(staged.temp)
        return { ok: false, reason: `Could not install into ${target}: ${reasonFrom(error)}` }
      }

      return commit(staged.manifest, source, pinFor(source, staged), target)
    },

    /**
     * Update an installation in place, reporting when the ask has widened.
     *
     * The scope comparison happens here rather than at the grant screen, because
     * the decision is about two manifests and the caller only needs the answer.
     */
    async update(name, source) {
      await load()
      const existing = find(name)
      if (!existing) return { ok: false, reason: `${name} is not installed.` }

      const staged = await stage(source)
      if (!staged.ok) {
        await discard(staged.temp)
        return { ok: false, reason: staged.reason }
      }

      const scopes = scopesWidened(existing.manifest?.mcp, staged.manifest.mcp)
      if (scopes.widened) {
        // Staged but not installed: the caller shows the grant screen and calls
        // install() if the user agrees. Installing first and asking after would
        // make the prompt a formality.
        await discard(staged.temp)
        return { ok: false, needsGrant: true, scopes, manifest: staged.manifest }
      }

      const target = dirFor(name)
      try {
        await remove(target)
        await move(staged.dir, target)
      } catch (error) {
        await discard(staged.temp)
        return { ok: false, reason: `Could not update ${name}: ${reasonFrom(error)}` }
      }

      return commit(staged.manifest, source, pinFor(source, staged), target, { enabled: existing.enabled })
    },

    /** Stop loading it, without removing anything. */
    async setEnabled(name, enabled) {
      await load()
      const installation = find(name)
      if (!installation) return { ok: false, reason: `${name} is not installed.` }

      installation.enabled = Boolean(enabled)
      // A disable is often somebody stopping an extension that keeps failing;
      // re-enabling it should give it a clean slate rather than one strike from
      // being disabled again.
      if (installation.enabled) installation.failures = 0
      await persist()
      return { ok: true, installation: { ...installation } }
    },

    /**
     * Remove it, and everything installed with it.
     *
     * A linked folder is only unlinked — deleting it would delete the user's own
     * working copy, which is emphatically not what "uninstall" means.
     */
    async uninstall(name) {
      await load()
      const installation = find(name)
      if (!installation) return { ok: false, reason: `${name} is not installed.` }

      if (installation.source?.kind !== 'local') {
        try {
          await remove(dirFor(name))
        } catch (error) {
          return { ok: false, reason: `Could not remove ${name}: ${reasonFrom(error)}` }
        }
      }

      state.installations = state.installations.filter((i) => i.name !== name)
      await persist()
      return { ok: true }
    },

    /**
     * Count a failure, disabling the extension once it is clearly not working.
     *
     * An extension that crashes every time it loads is not going to fix itself,
     * and an app that keeps loading it degrades quietly for reasons a user
     * cannot see. Disabling is loud, recoverable and honest.
     */
    async recordFailure(name, limit = 3) {
      await load()
      const installation = find(name)
      if (!installation) return { ok: false, reason: `${name} is not installed.` }

      installation.failures += 1
      const disabled = installation.failures >= limit
      if (disabled) installation.enabled = false
      await persist()
      return { ok: true, disabled, failures: installation.failures }
    },
  }

  async function commit(manifest, source, pin, dir, options) {
    const entry = { ...record(manifest, source, pin, options), dir, installedAt: now() }
    state.installations = [...state.installations.filter((i) => i.name !== manifest.name), entry]
    await persist()
    return { ok: true, installation: { ...entry } }
  }
}
