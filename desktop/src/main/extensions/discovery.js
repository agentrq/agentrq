import { parseManifest } from './manifest.js'

/**
 * Finding extensions, and remembering what was found.
 *
 * Discovery is a GitHub repository topic. Anyone may publish by adding
 * `agentrq-extension` to a repository and committing a manifest — there is no
 * registry to run, no submission flow, and nobody to arbitrate a namespace. The
 * cost of that openness is that the topic is *discovery, not endorsement*, which
 * is why the catalogue shows an owner and a star count rather than a badge.
 *
 * ## This runs in the main process, and could not run anywhere else
 *
 * The renderer is served from the privileged `app://` scheme, whose entire
 * purpose is that it only ever sees same-origin traffic — that is why `/api`,
 * `/mcp` and `/.well-known` are forwarded from here rather than fetched there.
 * A request to api.github.com from the renderer is exactly the cross-origin call
 * that architecture exists to prevent, and the CSP would refuse it anyway.
 *
 * `fetchJson` and the two file operations are injected, so the whole path is
 * testable without a network or a disk.
 *
 * ## What the live API actually does
 *
 * Measured rather than assumed, because both limits shape the design:
 *
 * - **10 requests a minute** unauthenticated, **30** with a user token.
 * - **1000 results maximum** for any one search, however many exist.
 *
 * The first is why nothing here runs on a page load: the UI reads the cache, and
 * a refresh is a deliberate act or a long timer. The second is why the search is
 * partitioned — the ceiling is not hypothetical, `topic:mcp-server` already
 * returns over 28,000 repositories, of which only the first 1000 are reachable.
 */

/** The topic that makes a repository findable as an extension. */
export const TOPIC = 'agentrq-extension'

/** GitHub's maximum, and its hard ceiling on any single search. */
export const SEARCH_PAGE_SIZE = 100
export const SEARCH_RESULT_CAP = 1000

/** Long enough that a background refresh is never the reason a limit is hit. */
export const REFRESH_INTERVAL_MS = 6 * 60 * 60 * 1000

/**
 * Star ranges the search is split across when one query cannot reach everything.
 *
 * Split by stars rather than by date because the distribution is extreme — the
 * overwhelming majority of repositories have none — so the buckets get finer at
 * the bottom, where the crowd is. Each is a disjoint range, so a repository
 * appears in exactly one and no result is counted twice.
 */
export const STAR_BUCKETS = [
  '>=1000',
  '200..999',
  '50..199',
  '10..49',
  '3..9',
  '1..2',
  '0',
]

/** The search query for the whole topic, or for one slice of it. */
export function buildQuery(starRange = '') {
  return starRange ? `topic:${TOPIC} stars:${starRange}` : `topic:${TOPIC}`
}

/** Where a repository's manifest lives, on whichever branch is its default. */
export function manifestUrl({ fullName, defaultBranch }) {
  return `https://raw.githubusercontent.com/${fullName}/${defaultBranch}/agentrq-extension.json`
}

/** The fields of a search result the catalogue actually shows or needs. */
function toRepo(item) {
  return {
    fullName: item.full_name,
    owner: item.owner?.login ?? item.full_name?.split('/')[0] ?? '',
    name: item.name,
    description: item.description ?? '',
    stars: item.stargazers_count ?? 0,
    pushedAt: item.pushed_at ?? '',
    defaultBranch: item.default_branch || 'main',
    url: item.html_url ?? `https://github.com/${item.full_name}`,
  }
}

/**
 * Every repository carrying the topic, across as many searches as it takes.
 *
 * Returns `truncated` when the ceiling genuinely could not be worked around —
 * a single star bucket holding more than a thousand repositories. Reported
 * rather than hidden: a catalogue quietly missing entries is worse than one that
 * says it is incomplete, because nobody can tell the difference from inside.
 */
export async function searchRepos(fetchJson, { token = '' } = {}) {
  const first = await fetchJson(searchUrl(buildQuery(), 1), token)
  const total = first?.total_count ?? 0

  if (total <= SEARCH_RESULT_CAP) {
    const repos = await drainPages(fetchJson, buildQuery(), total, token, first)
    return { repos, truncated: false }
  }

  const seen = new Map()
  let truncated = false
  for (const bucket of STAR_BUCKETS) {
    const query = buildQuery(bucket)
    const head = await fetchJson(searchUrl(query, 1), token)
    const count = head?.total_count ?? 0
    if (count > SEARCH_RESULT_CAP) truncated = true

    for (const repo of await drainPages(fetchJson, query, count, token, head)) {
      // Buckets are disjoint, so this only guards against a repository gaining
      // or losing a star between two requests — cheap insurance against a
      // duplicate or a gap that would otherwise be invisible.
      seen.set(repo.fullName, repo)
    }
  }
  return { repos: [...seen.values()], truncated }
}

function searchUrl(query, page) {
  const params = new URLSearchParams({
    q: query,
    per_page: String(SEARCH_PAGE_SIZE),
    page: String(page),
    sort: 'stars',
    order: 'desc',
  })
  return `https://api.github.com/search/repositories?${params}`
}

/** Pages through one query, reusing the response that told us how many there are. */
async function drainPages(fetchJson, query, total, token, first) {
  const reachable = Math.min(total, SEARCH_RESULT_CAP)
  const pages = Math.ceil(reachable / SEARCH_PAGE_SIZE)
  const repos = (first?.items ?? []).map(toRepo)

  for (let page = 2; page <= pages; page += 1) {
    const body = await fetchJson(searchUrl(query, page), token)
    repos.push(...(body?.items ?? []).map(toRepo))
  }
  return repos.slice(0, reachable)
}

/**
 * One catalogue row: a repository, and either its extension or why it has none.
 *
 * A repository carrying the topic with a manifest that does not parse is kept
 * here as **broken, with its reason**, rather than dropped. The author needs to
 * see why — a silently missing extension is undebuggable for them — and anyone
 * following a link to it needs the catalogue to explain itself rather than
 * appear empty.
 */
export function toEntry(repo, manifestSource) {
  if (manifestSource === null) {
    return { ...repo, ok: false, reason: 'No agentrq-extension.json in this repository.' }
  }
  const parsed = parseManifest(manifestSource)
  return parsed.ok
    ? { ...repo, ok: true, manifest: parsed.manifest }
    : { ...repo, ok: false, reason: parsed.reason }
}

/** Runs `worker` over `items`, a few at a time. */
async function pooled(items, limit, worker) {
  const out = []
  for (let i = 0; i < items.length; i += limit) {
    out.push(...(await Promise.all(items.slice(i, i + limit).map(worker))))
  }
  return out
}

/**
 * The catalogue: what is known, and how to go and find out again.
 *
 * @param {object} deps
 * @param {(url: string, token: string) => Promise<any>} deps.fetchJson  Resolves null on 404.
 * @param {(url: string, token: string) => Promise<string|null>} deps.fetchText
 * @param {() => Promise<string>} deps.readFile   Rejects when the cache is absent.
 * @param {(contents: string) => Promise<void>} deps.writeFile
 * @param {() => number} [deps.now]
 * @param {number} [deps.concurrency]
 */
export function createDiscovery({
  fetchJson,
  fetchText,
  readFile,
  writeFile,
  now = () => Date.now(),
  concurrency = 6,
}) {
  // `fetchedAt` is null for "never looked", not 0 — an epoch timestamp is a
  // legitimate value, and a sentinel that collides with one is a bug waiting for
  // whoever writes the test that happens to start its clock there.
  let index = { entries: [], fetchedAt: null, truncated: false }
  /**
   * The one read of the cache, shared.
   *
   * A `loaded` flag set before the `await` is not the same thing: a second
   * caller arriving while the first is still reading would see the flag, skip
   * the read and be handed the empty starting index — so opening the screen
   * while anything else asks would show a catalogue that is on disk as empty.
   */
  let reading = null

  async function load() {
    if (!reading) {
      reading = (async () => {
        try {
          const parsed = JSON.parse(await readFile())
          if (Array.isArray(parsed?.entries)) index = { truncated: false, fetchedAt: null, ...parsed }
        } catch {
          // No cache yet, or one we cannot read. Either way the catalogue is
          // empty until a refresh, which is a state the UI already has to
          // render.
        }
      })()
    }
    await reading
    // Read after the wait rather than resolved with: `refresh` replaces this,
    // and a promise resolved with the old object would hand every later caller
    // the catalogue from before the refresh.
    return index
  }

  return {
    /**
     * What is known right now, without going anywhere.
     *
     * This is what the UI calls. At ten requests a minute, a catalogue that
     * searched on every page load would spend its budget on people who opened a
     * panel and closed it again.
     */
    async list() {
      const current = await load()
      // The entries array is copied too. A shallow spread hands the caller the
      // very array this holds, so anything that sorted or filtered it in place
      // would quietly edit the catalogue for everyone who asks next.
      return { ...current, entries: [...current.entries] }
    },

    /**
     * Whether enough time has passed that a background refresh is due.
     *
     * A catalogue that has never been fetched is stale by definition, however
     * recently the app started. Treating "never" as an age would leave a first
     * run reporting itself fresh and empty.
     */
    async isStale() {
      const { fetchedAt } = await load()
      return fetchedAt === null || now() - fetchedAt >= REFRESH_INTERVAL_MS
    },

    /**
     * Go and look again.
     *
     * Manifests are only re-fetched for repositories that have actually been
     * pushed to since the last look. A catalogue of any size is mostly unchanged
     * between refreshes, and re-reading every manifest to discover that would
     * turn a routine refresh into hundreds of requests.
     *
     * A failure leaves the cache exactly as it was. Replacing a working
     * catalogue with an empty one because a request timed out would be a worse
     * answer than the slightly stale one already in hand.
     */
    async refresh({ token = '' } = {}) {
      const previous = await load()
      const known = new Map(previous.entries.map((entry) => [entry.fullName, entry]))

      let found
      try {
        found = await searchRepos(fetchJson, { token })
      } catch (error) {
        return {
          ok: false,
          reason: describeFailure(error),
          index: { ...previous, entries: [...previous.entries] },
        }
      }

      const entries = await pooled(found.repos, concurrency, async (repo) => {
        const cached = known.get(repo.fullName)
        if (cached && cached.pushedAt && cached.pushedAt === repo.pushedAt) {
          // Unchanged since we last read it: keep the verdict, refresh the
          // things that move on their own, like the star count.
          return { ...cached, ...repo }
        }
        try {
          return toEntry(repo, await fetchText(manifestUrl(repo), token))
        } catch (error) {
          return { ...repo, ok: false, reason: describeFailure(error) }
        }
      })

      index = { entries, fetchedAt: now(), truncated: found.truncated }
      try {
        await writeFile(JSON.stringify(index))
      } catch {
        // An unwritable cache costs the next launch a refresh; it does not make
        // this one wrong, and failing here would throw away results we have.
      }
      return { ok: true, index: { ...index, entries: [...entries] } }
    },
  }
}

/** Turns a failure into something worth putting in front of a person. */
export function describeFailure(error) {
  const message = String(error?.message ?? error ?? 'Unknown error')

  if (/rate limit|\b403\b/i.test(message)) {
    // No token setting exists — `installExtensions` never passes one — so the
    // advice to add one sent people looking for a control that is not there.
    // Say the one thing that is true and actionable instead.
    return 'GitHub is rate limiting this app. Try again in a few minutes.'
  }
  if (/ENOTFOUND|ETIMEDOUT|ECONNREFUSED|EAI_AGAIN|network|fetch failed/i.test(message)) {
    return 'Could not reach GitHub.'
  }
  return message
}
