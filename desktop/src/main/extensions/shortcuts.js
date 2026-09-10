/**
 * Whether an extension's shortcuts can be given to it.
 *
 * Asked at **install**, from the manifest, and that timing is the whole point.
 * A conflict discovered at runtime is a key that silently does nothing: nothing
 * on screen explains it, and the two extensions involved each look correct in
 * isolation. Asked here, it is a sentence naming the one already using it, in
 * front of somebody who can still decide not to install.
 *
 * The renderer has its own copy of the prefix rules for dispatching. This is the
 * gate, and it deliberately answers the narrower question — may these be
 * claimed — rather than trying to be a second dispatcher.
 */

/** The one key reserved for extensions. Mirrors the renderer's constant. */
export const PREFIX = 'x'

/** A single letter or digit, and never the prefix itself. */
export function isBindableKey(key) {
  const value = String(key ?? '').toLowerCase()
  return value.length === 1 && value !== PREFIX && /^[a-z0-9]$/.test(value)
}

/**
 * What an extension is asking to bind, from its manifest.
 *
 * Malformed entries are dropped here rather than refused: `conflicts` reports
 * them, and reading the list twice with two different ideas of what counts
 * would be a way for the two to disagree.
 */
export function requestedKeys(manifest) {
  return (manifest?.shortcuts ?? [])
    .map((shortcut) => ({
      key: String(shortcut?.key ?? '').toLowerCase(),
      action: String(shortcut?.action ?? ''),
      title: String(shortcut?.title ?? ''),
    }))
    .filter((shortcut) => shortcut.key !== '')
}

/**
 * Whether this extension's shortcuts can all be claimed.
 *
 * `taken` is what every *other* installed extension already holds — an
 * extension updating in place must not conflict with the version of itself
 * being replaced, which would make every update after the first impossible.
 */
export function checkShortcuts(manifest, taken = []) {
  const owner = manifest?.name ?? ''
  const held = new Map(
    taken
      .filter((entry) => entry.owner !== owner)
      .map((entry) => [String(entry.key).toLowerCase(), entry.owner]),
  )

  const problems = []
  const seen = new Set()

  for (const { key } of requestedKeys(manifest)) {
    if (!isBindableKey(key)) {
      problems.push(
        key === PREFIX
          ? `"${PREFIX}" is the prefix itself and cannot also be a shortcut.`
          : `"${key}" is not a key an extension can bind. Use a single letter or digit.`,
      )
      continue
    }
    if (seen.has(key)) {
      problems.push(`This extension asks for "${PREFIX} ${key}" twice.`)
      continue
    }
    seen.add(key)

    const other = held.get(key)
    if (other) problems.push(`"${PREFIX} ${key}" is already used by ${other}.`)
  }

  return { ok: problems.length === 0, problems }
}

/** Everything currently claimed, for the check above and for the help sheet. */
export function claimedShortcuts(installations = []) {
  return installations
    .filter((installation) => installation.enabled !== false)
    .flatMap((installation) =>
      requestedKeys(installation.manifest)
        .filter(({ key }) => isBindableKey(key))
        .map(({ key, action, title }) => ({
          key,
          owner: installation.name,
          id: action,
          label: title || action,
        })),
    )
}
