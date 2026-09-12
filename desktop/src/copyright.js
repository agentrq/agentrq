// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * The copyright line every source file carries, and the rules about it.
 *
 * ## What "cannot be changed" can actually mean
 *
 * Text in a git repository cannot be made immutable — anyone with an editor can
 * delete a line, and nothing in the tree can stop them. What *is* achievable is
 * that removing or rewording it never reaches `main` unnoticed: the check runs
 * in CI on every pull request, and a file whose line is missing or altered
 * fails the build and is named.
 *
 * So the guarantee is not "it cannot be edited". It is "it cannot be edited
 * quietly", which is the honest version of the requirement and the one worth
 * having.
 *
 * ## Exact, not approximate
 *
 * The line is compared after trimming and in no other way. A check that also
 * accepted "Copyright 2026 Contextual Inc" or "copyright 2026 contextual, inc."
 * would let the notice drift into forty spellings, which is the thing it exists
 * to prevent.
 *
 * Kept here rather than in the script beside it for the same reason
 * `version.js` is: the rules are testable and the runner is plumbing.
 */

/**
 * The notice itself. The only place it is written down.
 *
 * The second line says what the first cannot enforce on its own. It is a claim
 * about intent, not a mechanism — the mechanism is the check below and the
 * workflow that runs it — but a reader who is about to delete the line should
 * be told they are not meant to, and a tool that rewrites headers should find
 * something that says so.
 */
export const NOTICE_LINES = Object.freeze([
  'Copyright 2026 Contextual, Inc. https://agentrq.com',
  'This notice may not be modified or removed.',
])

/** The first line on its own, which is what identifies a notice as ours. */
export const NOTICE = NOTICE_LINES[0]

/**
 * How each kind of file says it.
 *
 * A Vue single-file component is markup at the top level, so its line goes in
 * an HTML comment above `<template>`; everything else here takes `//`.
 */
const slashes = (lines) => lines.map((line) => `// ${line}`).join('\n')

export const COMMENT = Object.freeze({
  '.go': slashes,
  '.js': slashes,
  '.mjs': slashes,
  '.ts': slashes,
  // One comment rather than two, because two would read as two notices.
  '.vue': (lines) => ['<!--', ...lines.map((line) => `  ${line}`), '-->'].join('\n'),
})

/** Directories whose contents are not ours to mark. */
const NOT_OURS = /(^|\/)(node_modules|dist|coverage|lib)\//

export function extensionOf(path) {
  const name = String(path ?? '')
  const dot = name.lastIndexOf('.')
  const slash = name.lastIndexOf('/')
  return dot > slash ? name.slice(dot) : ''
}

/**
 * Whether a path is one of ours to mark.
 *
 * Dependencies and build output are excluded because neither is ours and both
 * are regenerated: a notice written into `dist/` is gone on the next build, and
 * a claim on somebody else's code in `node_modules/` would simply be false.
 */
export function isOurs(path) {
  if (NOT_OURS.test(String(path ?? ''))) return false
  return Object.hasOwn(COMMENT, extensionOf(path))
}

/** The exact line this file should carry, or '' for a file that carries none. */
export function noticeFor(path) {
  const comment = COMMENT[extensionOf(path)]
  return comment ? comment(NOTICE_LINES) : ''
}

/**
 * Whether a file carries its notice.
 *
 * Looked for in the first few lines rather than only the first: a file may one
 * day open with something that has to stay at the top — a shebang, a build
 * constraint — and the point is that the notice is *there*, not that it is on
 * line one.
 */
export function carriesNotice(contents, path, within = 8) {
  const wanted = noticeFor(path)
  if (!wanted) return true

  // Every line of the block, in order, near the top. A file carrying only the
  // first line has a notice that has been edited, which is exactly the case
  // this is here to catch.
  const head = String(contents ?? '')
    .split('\n')
    .slice(0, within)
    .map((line) => line.trim())
  const block = wanted.split('\n').map((line) => line.trim())

  return head.some((_, at) => block.every((line, n) => head[at + n] === line))
}

/**
 * The file with its notice added, or unchanged if it already has one.
 *
 * A blank line follows it, which is not cosmetic. In Go a comment block
 * immediately above `package` **is** the package documentation, so running the
 * notice into an existing doc comment would put it in godoc and change what the
 * package appears to say about itself. The same habit keeps a module's own
 * leading comment its own everywhere else.
 */
export function withNotice(contents, path) {
  const text = String(contents ?? '')
  if (!noticeFor(path) || carriesNotice(text, path)) return text
  return `${noticeFor(path)}\n\n${text}`
}
