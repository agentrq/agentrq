// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi } from 'vitest'

import {
  REFRESH_INTERVAL_MS,
  SEARCH_RESULT_CAP,
  STAR_BUCKETS,
  buildQuery,
  createDiscovery,
  describeFailure,
  manifestUrl,
  searchRepos,
  toEntry,
} from '../../src/main/extensions/discovery.js'

/**
 * Two measured facts drive almost everything here: GitHub answers 10 searches a
 * minute unauthenticated, and returns at most 1000 results for any one query
 * however many exist. The first is why nothing searches on a page load; the
 * second is why the search is partitioned. `topic:mcp-server` already holds over
 * 28,000 repositories, so the ceiling is not hypothetical.
 */

/** A search result item in the shape GitHub actually returns. */
const item = (n, over = {}) => ({
  full_name: `owner${n}/ext${n}`,
  name: `ext${n}`,
  owner: { login: `owner${n}` },
  description: `Extension ${n}`,
  stargazers_count: n,
  pushed_at: `2026-09-0${(n % 9) + 1}T00:00:00Z`,
  default_branch: 'main',
  html_url: `https://github.com/owner${n}/ext${n}`,
  ...over,
})

const manifestFor = (name) =>
  JSON.stringify({
    name,
    version: '1.0.0',
    license: 'MIT',
    engines: { agentrq: '^1.4' },
    artifact: { release: 'v1.0.0', asset: `${name}.tgz`, sha256: 'a'.repeat(64) },
  })

/** A fake GitHub: a page of results per query, and a manifest per repository. */
function fakeGitHub({ totals = {}, manifests = {}, defaultTotal = 2 } = {}) {
  const calls = { search: [], manifest: [] }

  const fetchJson = vi.fn(async (url, token) => {
    const parsed = new URL(url)
    const query = parsed.searchParams.get('q')
    const page = Number(parsed.searchParams.get('page'))
    calls.search.push({ query, page, token })

    const total = totals[query] ?? defaultTotal
    const reachable = Math.min(total, SEARCH_RESULT_CAP)
    const start = (page - 1) * 100
    const count = Math.max(0, Math.min(100, reachable - start))
    // Number the items by query so buckets return distinct repositories.
    const offset = Object.keys(totals).indexOf(query) * 1000
    return {
      total_count: total,
      items: Array.from({ length: count }, (_, i) => item(offset + start + i + 1)),
    }
  })

  const fetchText = vi.fn(async (url, token) => {
    calls.manifest.push({ url, token })
    const name = url.split('/')[4]
    return name in manifests ? manifests[name] : manifestFor(name)
  })

  return { fetchJson, fetchText, calls }
}

/** An in-memory cache file. */
function fakeStore(initial = null) {
  let contents = initial
  return {
    readFile: vi.fn(async () => {
      if (contents === null) throw new Error('ENOENT')
      return contents
    }),
    writeFile: vi.fn(async (next) => {
      contents = next
    }),
    get contents() {
      return contents
    },
  }
}

describe('buildQuery and manifestUrl', () => {
  it('searches the topic, and one slice of it', () => {
    expect(buildQuery()).toBe('topic:agentrq-extension')
    expect(buildQuery('10..49')).toBe('topic:agentrq-extension stars:10..49')
  })

  it('reads the manifest from whichever branch is the default', () => {
    // Not every repository is on main, and guessing costs a 404 per repo.
    expect(manifestUrl({ fullName: 'a/b', defaultBranch: 'trunk' })).toBe(
      'https://raw.githubusercontent.com/a/b/trunk/agentrq-extension.json',
    )
  })
})

describe('searchRepos', () => {
  it('pages through a result set that fits under the ceiling', async () => {
    const { fetchJson, calls } = fakeGitHub({ totals: { 'topic:agentrq-extension': 250 } })

    const { repos, truncated } = await searchRepos(fetchJson)

    expect(repos).toHaveLength(250)
    expect(truncated).toBe(false)
    expect(calls.search.map((c) => c.page)).toEqual([1, 2, 3])
  })

  it('does not partition when it does not have to', async () => {
    const { fetchJson, calls } = fakeGitHub({ totals: { 'topic:agentrq-extension': 40 } })

    await searchRepos(fetchJson)

    // One query, one page: the budget is ten searches a minute.
    expect(calls.search).toHaveLength(1)
  })

  it('partitions by stars once the topic outgrows a single search', async () => {
    // The case that is already real for a popular topic: more repositories exist
    // than any one query can reach, so the search is split into disjoint slices.
    const totals = { 'topic:agentrq-extension': 4000 }
    for (const bucket of STAR_BUCKETS) totals[buildQuery(bucket)] = 100
    const { fetchJson, calls } = fakeGitHub({ totals })

    const { repos, truncated } = await searchRepos(fetchJson)

    expect(truncated).toBe(false)
    expect(repos).toHaveLength(STAR_BUCKETS.length * 100)
    const queried = calls.search.map((c) => c.query)
    for (const bucket of STAR_BUCKETS) expect(queried).toContain(buildQuery(bucket))
  })

  it('says so when even a slice cannot be reached in full', async () => {
    // Honest rather than quietly short: a catalogue missing entries with no way
    // to tell is worse than one that reports itself incomplete.
    const totals = { 'topic:agentrq-extension': 40000 }
    for (const bucket of STAR_BUCKETS) totals[buildQuery(bucket)] = 50
    totals[buildQuery('0')] = 5000
    const { fetchJson } = fakeGitHub({ totals })

    const { truncated } = await searchRepos(fetchJson)

    expect(truncated).toBe(true)
  })

  it('never returns more than the ceiling from one query', async () => {
    const { fetchJson } = fakeGitHub({ totals: { 'topic:agentrq-extension': 1000 } })

    const { repos } = await searchRepos(fetchJson)

    expect(repos).toHaveLength(SEARCH_RESULT_CAP)
  })

  it('sends the token when there is one, and nothing when there is not', async () => {
    const { fetchJson, calls } = fakeGitHub()

    await searchRepos(fetchJson, { token: 'ghp_x' })
    await searchRepos(fetchJson)

    expect(calls.search[0].token).toBe('ghp_x')
    expect(calls.search.at(-1).token).toBe('')
  })

  it('copes with a response carrying no items at all', async () => {
    const fetchJson = vi.fn(async () => ({}))
    const { repos } = await searchRepos(fetchJson)
    expect(repos).toEqual([])
  })

  it('copes with a later page that comes back empty', async () => {
    // The count and the pages are two separate requests; a repository deleted
    // between them leaves a short page rather than an error.
    const fetchJson = vi.fn(async (url) => {
      const page = Number(new URL(url).searchParams.get('page'))
      return page === 1 ? { total_count: 150, items: [item(1)] } : {}
    })

    const { repos } = await searchRepos(fetchJson)

    expect(repos).toHaveLength(1)
  })

  it('copes with a bucket whose head response says nothing', async () => {
    const fetchJson = vi.fn(async (url) => {
      const query = new URL(url).searchParams.get('q')
      return query === 'topic:agentrq-extension' ? { total_count: 4000, items: [] } : null
    })

    const { repos, truncated } = await searchRepos(fetchJson)

    expect(repos).toEqual([])
    expect(truncated).toBe(false)
  })

  it('fills in what a sparse search result leaves out', async () => {
    // GitHub omits a description that is null, and a repository can be missing
    // fields the catalogue still has to render something for.
    const fetchJson = vi.fn(async () => ({
      total_count: 1,
      items: [{ full_name: 'solo/ext' }],
    }))

    const { repos } = await searchRepos(fetchJson)

    expect(repos[0]).toEqual({
      fullName: 'solo/ext',
      owner: 'solo',
      name: undefined,
      description: '',
      stars: 0,
      pushedAt: '',
      defaultBranch: 'main',
      url: 'https://github.com/solo/ext',
    })
  })

  it('has an owner even when the result carries neither owner nor full name', async () => {
    const fetchJson = vi.fn(async () => ({ total_count: 1, items: [{ name: 'ext' }] }))

    const { repos } = await searchRepos(fetchJson)

    expect(repos[0].owner).toBe('')
  })
})

describe('toEntry', () => {
  const repo = { fullName: 'a/b', owner: 'a', stars: 3 }

  it('keeps a valid manifest', () => {
    const entry = toEntry(repo, manifestFor('linear'))

    expect(entry.ok).toBe(true)
    expect(entry.manifest.name).toBe('linear')
    expect(entry.stars).toBe(3)
  })

  it('keeps a repository with no manifest, and says that is what is wrong', () => {
    const entry = toEntry(repo, null)

    expect(entry.ok).toBe(false)
    expect(entry.reason).toBe('No agentrq-extension.json in this repository.')
  })

  it('keeps a broken manifest, and carries the reason through', () => {
    // The author needs to see why. A silently missing extension is
    // undebuggable for them, and looks like a broken catalogue to everyone else.
    const entry = toEntry(repo, JSON.stringify({ name: 'x', version: '1.0.0' }))

    expect(entry.ok).toBe(false)
    expect(entry.reason).toContain('"license" is required')
  })

  it('keeps a repository whose manifest is not JSON', () => {
    expect(toEntry(repo, '<!doctype html>').reason).toBe('This is not valid JSON.')
  })
})

describe('createDiscovery', () => {
  const build = (github = fakeGitHub(), store = fakeStore(), extra = {}) =>
    createDiscovery({ ...github, ...store, ...extra })

  it('starts empty and goes nowhere until asked', async () => {
    // Ten searches a minute is not a budget to spend on people who opened a
    // panel and closed it again.
    const github = fakeGitHub()
    const discovery = build(github)

    const index = await discovery.list()

    expect(index.entries).toEqual([])
    expect(github.fetchJson).not.toHaveBeenCalled()
  })

  it('reads a cache written by an earlier run', async () => {
    const cached = JSON.stringify({
      entries: [{ fullName: 'a/b', ok: true, manifest: { name: 'b' } }],
      fetchedAt: 1000,
    })
    const discovery = build(fakeGitHub(), fakeStore(cached))

    const index = await discovery.list()

    expect(index.entries).toHaveLength(1)
    expect(index.fetchedAt).toBe(1000)
  })

  it('ignores a cache it cannot make sense of', async () => {
    const discovery = build(fakeGitHub(), fakeStore('{ not json'))
    expect((await discovery.list()).entries).toEqual([])
  })

  it('ignores a cache of the wrong shape', async () => {
    const discovery = build(fakeGitHub(), fakeStore('{"entries":"lots"}'))
    expect((await discovery.list()).entries).toEqual([])
  })

  it('searches, reads every manifest, and writes what it found', async () => {
    const github = fakeGitHub({ totals: { 'topic:agentrq-extension': 2 } })
    const store = fakeStore()
    const discovery = build(github, store, { now: () => 5000 })

    const { ok, index } = await discovery.refresh()

    expect(ok).toBe(true)
    expect(index.entries).toHaveLength(2)
    expect(index.entries.every((e) => e.ok)).toBe(true)
    expect(index.fetchedAt).toBe(5000)
    expect(JSON.parse(store.contents).entries).toHaveLength(2)
  })

  it('re-reads only the repositories that have actually changed', async () => {
    // A catalogue is mostly unchanged between refreshes. Re-reading every
    // manifest to discover that would turn a routine refresh into hundreds of
    // requests against a limit measured in tens per minute.
    const github = fakeGitHub({ totals: { 'topic:agentrq-extension': 2 } })
    const store = fakeStore()
    const discovery = build(github, store)

    await discovery.refresh()
    const afterFirst = github.fetchText.mock.calls.length
    await discovery.refresh()

    expect(afterFirst).toBe(2)
    expect(github.fetchText.mock.calls.length).toBe(2)
  })

  it('re-reads a repository that has been pushed to since', async () => {
    const github = fakeGitHub({ totals: { 'topic:agentrq-extension': 1 } })
    const store = fakeStore()
    const discovery = build(github, store)
    await discovery.refresh()

    github.fetchJson.mockImplementationOnce(async () => ({
      total_count: 1,
      items: [item(1, { pushed_at: '2026-10-01T00:00:00Z' })],
    }))
    await discovery.refresh()

    expect(github.fetchText.mock.calls.length).toBe(2)
  })

  it('keeps the star count current even for an unchanged repository', async () => {
    // Stars move on their own; the manifest does not.
    const github = fakeGitHub({ totals: { 'topic:agentrq-extension': 1 } })
    const discovery = build(github, fakeStore())
    await discovery.refresh()

    github.fetchJson.mockImplementationOnce(async () => ({
      total_count: 1,
      items: [item(1, { stargazers_count: 99 })],
    }))
    const { index } = await discovery.refresh()

    expect(index.entries[0].stars).toBe(99)
    expect(index.entries[0].ok).toBe(true)
  })

  it('keeps the previous catalogue when the search fails', async () => {
    // Replacing a working catalogue with an empty one because a request timed
    // out is a worse answer than the slightly stale one already in hand.
    const github = fakeGitHub({ totals: { 'topic:agentrq-extension': 2 } })
    const discovery = build(github, fakeStore())
    await discovery.refresh()

    github.fetchJson.mockRejectedValueOnce(new Error('fetch failed'))
    const { ok, reason, index } = await discovery.refresh()

    expect(ok).toBe(false)
    expect(reason).toBe('Could not reach GitHub.')
    expect(index.entries).toHaveLength(2)
  })

  it('records a manifest that could not be fetched as that repository’s reason', async () => {
    const github = fakeGitHub({ totals: { 'topic:agentrq-extension': 1 } })
    github.fetchText.mockRejectedValueOnce(new Error('ETIMEDOUT'))
    const discovery = build(github, fakeStore())

    const { index } = await discovery.refresh()

    expect(index.entries[0].ok).toBe(false)
    expect(index.entries[0].reason).toBe('Could not reach GitHub.')
  })

  it('keeps the results when the cache cannot be written', async () => {
    const store = fakeStore()
    store.writeFile.mockRejectedValueOnce(new Error('EACCES'))
    const discovery = build(fakeGitHub({ totals: { 'topic:agentrq-extension': 1 } }), store)

    const { ok, index } = await discovery.refresh()

    expect(ok).toBe(true)
    expect(index.entries).toHaveLength(1)
  })

  it('passes the token to both the search and the manifest reads', async () => {
    const github = fakeGitHub({ totals: { 'topic:agentrq-extension': 1 } })
    const discovery = build(github, fakeStore())

    await discovery.refresh({ token: 'ghp_x' })

    expect(github.calls.search[0].token).toBe('ghp_x')
    expect(github.calls.manifest[0].token).toBe('ghp_x')
  })

  it('carries truncation through to the catalogue', async () => {
    const totals = { 'topic:agentrq-extension': 40000 }
    for (const bucket of STAR_BUCKETS) totals[buildQuery(bucket)] = 10
    totals[buildQuery('0')] = 5000
    const discovery = build(fakeGitHub({ totals }), fakeStore())

    expect((await discovery.refresh()).index.truncated).toBe(true)
  })

  it('knows when a refresh is due', async () => {
    let clock = 0
    const discovery = build(fakeGitHub({ totals: { 'topic:agentrq-extension': 1 } }), fakeStore(), {
      now: () => clock,
    })

    expect(await discovery.isStale()).toBe(true)
    await discovery.refresh()
    expect(await discovery.isStale()).toBe(false)

    clock = REFRESH_INTERVAL_MS
    expect(await discovery.isStale()).toBe(true)
  })

  it('hands out a copy, so a caller cannot edit the catalogue', async () => {
    const discovery = build(fakeGitHub({ totals: { 'topic:agentrq-extension': 1 } }), fakeStore())
    await discovery.refresh()

    const first = await discovery.list()
    first.entries.length = 0

    expect((await discovery.list()).entries).toHaveLength(1)
  })
})

describe('describeFailure', () => {
  it('names the rate limit, and only advice somebody can act on', () => {
    for (const error of [new Error('API rate limit exceeded'), new Error('HTTP 403')]) {
      expect(describeFailure(error)).toContain('rate limit')
    }
    // It used to say "add a personal access token in settings". There is no
    // such setting — nothing ever passes a token — so it sent people looking
    // for a control that does not exist.
    expect(describeFailure(new Error('API rate limit exceeded'))).not.toContain('token')
  })

  it('reports an unreachable network plainly', () => {
    for (const message of ['ENOTFOUND', 'ETIMEDOUT', 'ECONNREFUSED', 'fetch failed']) {
      expect(describeFailure(new Error(message)), message).toBe('Could not reach GitHub.')
    }
  })

  it('passes anything else through rather than inventing a diagnosis', () => {
    expect(describeFailure(new Error('Unexpected token <'))).toBe('Unexpected token <')
    expect(describeFailure(undefined)).toBe('Unknown error')
  })
})
