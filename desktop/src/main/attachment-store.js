import { budgetFor, evictionPlan, withinSizeCap } from '../../../frontend/src/composables/useAttachmentCache.js'

/**
 * Attachment bytes on disk, for the desktop build.
 *
 * ## Why this exists at all
 *
 * On the web an attachment is kept by the service worker, and the page opens it
 * offline without any component knowing. The desktop renderer is built without
 * a service worker, so that route does not exist there — every image and PDF
 * was re-downloaded on every view.
 *
 * Electron's own HTTP cache could not stand in: the attachment endpoint sends
 * no `Cache-Control` and no `ETag`, so the network stack has nothing to cache
 * on. And even with headers it would be the wrong answer, because Chromium's
 * cache is not addressable per workspace — "Clear this workspace" would have to
 * clear everything. That button makes a promise, so the bytes live in a store
 * we own.
 *
 * ## Layout
 *
 *     <dir>/<workspaceId>/<taskId>/<attachmentId>
 *     <dir>/<workspaceId>/<taskId>/<attachmentId>.meta
 *
 * The same shape as the URL the bytes came from, which is what makes the store
 * legible: the path on disk and the path in the request read the same way. The
 * attachment id *is* the filename, because it already names that exact file for
 * all time — the server mints one per attachment and never reuses it.
 *
 * Nesting by task also means removing anything is a directory removal rather
 * than a scan, at whichever level is being removed — a workspace today, a
 * single task if that is ever wanted.
 *
 * ## Why only base62 ever reaches the filesystem
 *
 * Both ids come out of a URL, and a path built from unvalidated input is how a
 * cache becomes a way to write anywhere on disk. They are monoflake ids —
 * digits and letters, nothing else — so anything that is not plain alphanumeric
 * is refused rather than escaped. A refused path is simply not cached, which
 * the caller already handles.
 */

/** Ids are base62 monoflakes. Nothing else may become a path segment. */
const SAFE_ID = /^[0-9A-Za-z]+$/

/** `/api/v1/workspaces/:workspaceId/tasks/:taskId/attachments/:attachmentId` */
const ATTACHMENT_PATH = /\/workspaces\/([^/]+)\/tasks\/([^/]+)\/attachments\/([^/]+)$/

/** Suffix of the file holding a cached response's content type. */
const META = '.meta'

/**
 * The workspace, task and attachment an attachment request names, or null.
 *
 * Null covers both "not an attachment path" and "an attachment path carrying
 * something that must not become a filename", because the caller does the same
 * thing for each: forward without caching.
 */
export function parseAttachmentPath(pathname) {
  const match = ATTACHMENT_PATH.exec(String(pathname ?? ''))
  if (!match) return null

  const [, workspaceId, taskId, attachmentId] = match
  // Every one of these becomes a path segment, so every one is checked.
  if (![workspaceId, taskId, attachmentId].every((part) => SAFE_ID.test(part))) return null
  return { workspaceId, taskId, attachmentId }
}

/**
 * A cache of attachment bytes in one directory, for one profile.
 *
 * Every filesystem call is injected so the rules can be tested in plain Node
 * with no Electron binary and no temporary directories, matching how the rest
 * of this folder is tested.
 *
 * @param {object} options
 * @param {string} options.dir       where the files live, per profile
 * @param {object} options.fs        node:fs/promises, or a stand-in
 * @param {number} [options.budget]  bytes; defaults to the desktop budget
 */
export function createAttachmentStore({ dir, fs, budget = budgetFor('desktop') } = {}) {
  const workspaceDir = (workspaceId) => `${dir}/${workspaceId}`
  const taskDir = (id) => `${workspaceDir(id.workspaceId)}/${id.taskId}`
  const filePath = (id) => `${taskDir(id)}/${id.attachmentId}`

  /** What a directory holds, or nothing when it cannot be read. */
  async function namesIn(path) {
    try {
      return await fs.readdir(path)
    } catch {
      return []
    }
  }

  /** Every stored attachment across every workspace, least recently used first. */
  async function entries() {
    let workspaces
    try {
      workspaces = await fs.readdir(dir)
    } catch {
      // No directory yet is the ordinary state on a first run, not a failure.
      return []
    }

    const found = []
    for (const workspaceId of workspaces) {
      if (!SAFE_ID.test(workspaceId)) continue
      for (const taskId of await namesIn(workspaceDir(workspaceId))) {
        if (!SAFE_ID.test(taskId)) continue
        const dirOfTask = `${workspaceDir(workspaceId)}/${taskId}`
        for (const name of await namesIn(dirOfTask)) {
          if (name.endsWith(META) || !SAFE_ID.test(name)) continue
          const path = `${dirOfTask}/${name}`
          try {
            const stat = await fs.stat(path)
            found.push({ path, size: stat.size, usedAt: stat.mtimeMs })
          } catch {
            // Vanished between listing and stat, which is not worth an error.
          }
        }
      }
    }
    return found.sort((a, b) => a.usedAt - b.usedAt)
  }

  /** Remove an entry and the file describing it, ignoring either being gone. */
  async function remove(path) {
    await Promise.all([
      fs.rm(path, { force: true }).catch(() => {}),
      fs.rm(`${path}${META}`, { force: true }).catch(() => {}),
    ])
  }

  return {
    /**
     * The stored response for a path, or null.
     *
     * Null covers every reason a hit cannot be served — never stored, half
     * written, meta unreadable, disk error, an id that must not become a
     * filename — because the caller does the same thing in all of them: forward
     * to the server, which is what it would have done anyway.
     */
    async read(pathname) {
      const id = parseAttachmentPath(pathname)
      if (!id) return null

      const path = filePath(id)
      let body
      let meta
      try {
        body = await fs.readFile(path)
        meta = JSON.parse(await fs.readFile(`${path}${META}`, 'utf8'))
      } catch {
        return null
      }
      if (!meta?.contentType) return null

      // Serving makes this the most recently used entry. Best effort: failing
      // to record a hit costs a slightly worse eviction choice later, which is
      // not worth failing the read over.
      await fs.utimes?.(path, new Date(), new Date()).catch(() => {})

      return { body, contentType: meta.contentType, contentDisposition: meta.contentDisposition ?? '' }
    },

    /**
     * Keep a response, unless it is too large to be worth the space.
     *
     * @returns {Promise<boolean>} whether it was kept
     */
    async write(pathname, body, { contentType = '', contentDisposition = '', size } = {}) {
      const id = parseAttachmentPath(pathname)
      if (!id || !contentType) return false
      // Judged from the length the caller measured, so the decision costs
      // nothing beyond what has already been read.
      if (!withinSizeCap({ headers: { get: () => (size === undefined ? null : String(size)) } })) {
        return false
      }

      const path = filePath(id)
      try {
        await fs.mkdir(taskDir(id), { recursive: true })
        await fs.writeFile(path, body)
        await fs.writeFile(`${path}${META}`, JSON.stringify({ contentType, contentDisposition }))
      } catch {
        // A cache that cannot be written behaves like one that was never
        // written, which the caller already handles.
        return false
      }

      await this.evict()
      return true
    },

    /** Drop the least recently used entries until the store fits its budget. */
    async evict() {
      const found = await entries()
      const doomed = evictionPlan(
        found.map((entry) => ({ url: entry.path, size: entry.size })),
        budget
      )
      for (const path of doomed) await remove(path)
      return doomed.length
    },

    /**
     * Forget one workspace's attachments, leaving every other workspace's alone.
     *
     * One directory removal, because the workspace *is* a directory. This is
     * what a store we own buys over cache headers, which cannot be addressed
     * per workspace at all.
     */
    async forgetWorkspace(workspaceId) {
      if (!workspaceId || !SAFE_ID.test(workspaceId)) return false
      try {
        await fs.rm(workspaceDir(workspaceId), { recursive: true, force: true })
        return true
      } catch {
        return false
      }
    },

    /** Everything, for signing out. */
    async forgetAll() {
      try {
        await fs.rm(dir, { recursive: true, force: true })
        return true
      } catch {
        return false
      }
    },
  }
}
