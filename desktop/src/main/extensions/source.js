// Copyright 2026 Contextual, Inc. https://agentrq.com

/**
 * Where an extension is being installed from, and what that implies.
 *
 * Three sources, and the difference between them is not plumbing — it is how
 * much is known about what is arriving, and therefore what can be verified:
 *
 * - **release** — a published asset found through the catalogue. A stranger's
 *   code, pinned by a digest, because the tag naming it can be moved afterwards.
 * - **git** — a repository the user typed in, cloned with their own credentials.
 *   Usually their own or their organisation's, and often private, which is the
 *   point: nothing here ever sees a token, because `git` already knows how to
 *   authenticate and we are not going to reimplement that badly.
 * - **local** — a directory on this machine. Theirs by definition, and the only
 *   source that can be *linked* rather than copied, which is what makes
 *   developing an extension bearable.
 *
 * The trust story differs across the three, so the source is recorded with the
 * installation and shown at the grant screen. "From a repository you entered" is
 * a materially different sentence from "from the public catalogue", and
 * flattening them would throw away the one distinction a user actually has.
 */

/** A digest is only meaningful for a downloaded asset — see `pinFor`. */
const SHA256_RE = /^[a-f0-9]{64}$/

/** `owner/repo`, with the shapes GitHub actually allows. */
const SHORTHAND_RE = /^[\w.-]+\/[\w.-]+$/

const fail = (reason) => ({ ok: false, reason })

/**
 * Works out what the user meant.
 *
 * Deliberately generous about form — `owner/repo`, a browser URL, an SSH remote
 * and an absolute path are all things somebody will reasonably paste — and
 * deliberately strict about the result, so everything downstream deals in one
 * shape.
 */
export function resolveSource(input, { isAbsolute = (p) => p.startsWith('/') } = {}) {
  const value = typeof input === 'string' ? input.trim() : ''
  if (!value) return fail('Enter a repository or a folder to install from.')

  if (value.startsWith('.') || isAbsolute(value) || value.startsWith('file:')) {
    return { ok: true, source: { kind: 'local', path: value.replace(/^file:\/\//, '') } }
  }

  if (value.startsWith('git@') || value.endsWith('.git')) {
    return { ok: true, source: { kind: 'git', url: value, ref: '' } }
  }

  if (/^https?:\/\//.test(value)) {
    let url
    try {
      url = new URL(value)
    } catch {
      return fail(`This is not a URL this understands: "${value}".`)
    }
    // A branch or tag pasted from a browser — /tree/<ref> — is a ref the user
    // meant, and throwing it away would quietly install the wrong thing.
    const [owner, repo, kind, ...rest] = url.pathname.replace(/^\//, '').split('/')
    if (!owner || !repo) return fail(`This URL names no repository: "${value}".`)
    const ref = kind === 'tree' ? rest.join('/') : ''
    return {
      ok: true,
      source: { kind: 'git', url: `${url.origin}/${owner}/${repo.replace(/\.git$/, '')}.git`, ref },
    }
  }

  if (SHORTHAND_RE.test(value)) {
    return { ok: true, source: { kind: 'git', url: `https://github.com/${value}.git`, ref: '' } }
  }

  return fail(`This is not a repository or a folder: "${value}".`)
}

/**
 * The source for a catalogue entry, which is the only kind that carries a digest.
 */
export function releaseSource(entry) {
  const { artifact, name } = entry?.manifest ?? {}
  if (!artifact) return fail('This catalogue entry has no release to install.')
  return {
    ok: true,
    source: {
      kind: 'release',
      name,
      repo: entry.fullName,
      release: artifact.release,
      asset: artifact.asset,
      sha256: artifact.sha256,
    },
  }
}

/**
 * What identifies this install afterwards, and whether it can be verified.
 *
 * A release asset is pinned by its **sha256**, because the tag naming it is
 * mutable — an author can move `v1.2.0` to different code after you installed
 * it, and without the digest "install v1.2.0" is a promise nobody is keeping.
 *
 * A clone is pinned by the **commit** it landed on, which is a digest already
 * and cannot be moved even though the branch pointing at it can.
 *
 * A linked folder is pinned by **nothing at all**, and says so. It is a
 * directory the user edits; pretending to have verified it would be a lie, and
 * the honest answer is what lets the interface mark it as live rather than
 * fixed.
 */
export function pinFor(source, resolved = {}) {
  if (source.kind === 'release') return { kind: 'sha256', value: source.sha256 }
  if (source.kind === 'git') return { kind: 'commit', value: resolved.commit ?? '' }
  return { kind: 'none', value: '' }
}

/** Whether a downloaded asset is the one the manifest named. */
export function verifyDigest(expected, actual) {
  const want = String(expected ?? '').toLowerCase()
  const got = String(actual ?? '').toLowerCase()

  if (!SHA256_RE.test(want)) return fail('This release has no usable checksum to verify against.')
  if (!SHA256_RE.test(got)) return fail('The downloaded file could not be checksummed.')
  if (want !== got) {
    // Refused outright rather than warned about. A tag can be moved after an
    // install, so a mismatch means the bytes are not what the manifest
    // described — which is either a tampered release or a corrupted download,
    // and neither is something to click past.
    return fail(
      'The download does not match the checksum in the manifest, so it was not installed. ' +
        'The release may have been changed after it was published.',
    )
  }
  return { ok: true }
}

/** A one-line description of where an installation came from, for the UI. */
export function describeSource(source) {
  if (source?.kind === 'release') return `${source.repo} · ${source.release}`
  if (source?.kind === 'git') return source.ref ? `${source.url} · ${source.ref}` : source.url
  if (source?.kind === 'local') return `${source.path} · linked`
  return 'Unknown source'
}
