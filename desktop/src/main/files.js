/**
 * Opening a local file that a message linked to.
 *
 * The renderer never navigates to a `file:` URL — it asks the shell to open
 * one, and this is the part that decides what "open" is allowed to mean.
 *
 * That decision matters because the link came from message content, which is
 * written by an agent and can therefore be wrong or hostile. Handing any path
 * to `shell.openPath` would mean a link labelled `plan.md` could point at a
 * `.command` file and run it on click. So only file types that are *read* by
 * their default application open that way; everything else is revealed in the
 * file manager, where the person can see what it actually is and decide for
 * themselves. A revealed file has been shown, not run.
 *
 * The list is therefore an allowlist. An unknown extension is not assumed
 * harmless — the interesting case is always the one nobody thought about.
 *
 * Kept apart from index.js because that module needs a live Electron to import,
 * and this decision is the part worth testing.
 */

export const FileOpenAction = {
  /** Hand to the default application. */
  Open: 'open',
  /** Show in the file manager without opening it. */
  Reveal: 'reveal',
}

/**
 * Extensions whose default application displays the file rather than executing
 * it. Notably absent, and deliberately:
 *
 * - `.html`/`.svg`, which open in a browser and can carry script and markup a
 *   message author chose;
 * - `.sh`, `.command`, `.app`, `.exe`, `.bat`, `.scpt` and friends, which run;
 * - archives, which invite a double-click on whatever is inside them;
 * - no extension at all, where the platform decides based on metadata this
 *   process has not looked at.
 */
const READABLE_EXTENSIONS = new Set([
  // Text and source
  'txt', 'md', 'markdown', 'rst', 'adoc', 'org', 'log', 'csv', 'tsv',
  'json', 'jsonl', 'yaml', 'yml', 'toml', 'ini', 'cfg', 'conf', 'env', 'xml',
  'go', 'mod', 'sum', 'js', 'mjs', 'cjs', 'jsx', 'ts', 'tsx', 'vue', 'svelte',
  'py', 'rb', 'rs', 'java', 'kt', 'swift', 'c', 'h', 'cc', 'cpp', 'hpp', 'cs',
  'php', 'sql', 'graphql', 'proto', 'diff', 'patch', 'css', 'scss', 'less',
  // Documents
  'pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'odt', 'ods', 'odp',
  'rtf', 'pages', 'numbers', 'key', 'epub',
  // Media
  'png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'tif', 'tiff', 'heic', 'avif',
  'mp3', 'wav', 'm4a', 'flac', 'aac', 'ogg',
  'mp4', 'mov', 'm4v', 'webm', 'mkv', 'avi',
])

/**
 * The filesystem path a `file:` URL names.
 *
 * Returns '' for anything that is not a local file URL, including
 * `file://server/share/x` — that names a machine which is not this one, and
 * reaching for it would hang on a network mount at best.
 *
 * @param {string} rawUrl
 * @returns {string}
 */
export function localPathFromFileUrl(rawUrl) {
  let url
  try {
    url = new URL(String(rawUrl ?? ''))
  } catch {
    return ''
  }
  if (url.protocol !== 'file:') return ''
  if (url.host && url.host !== 'localhost') return ''

  let path
  try {
    path = decodeURIComponent(url.pathname)
  } catch {
    // A stray `%` in a filename is not a reason to refuse the whole path.
    path = url.pathname
  }

  // Windows drive paths arrive as `/C:/Users/…`; the leading slash belongs to
  // the URL form, not the path.
  if (/^\/[A-Za-z]:/.test(path)) path = path.slice(1)

  // A NUL byte truncates the path inside the platform's own file APIs, so what
  // gets opened would not be what was inspected here.
  return path.includes('\0') ? '' : path
}

/**
 * What may be done with a path, from its name alone.
 *
 * @param {string} path
 * @param {{ isDirectory?: boolean }} [stat]
 * @returns {typeof FileOpenAction[keyof typeof FileOpenAction]}
 */
export function fileOpenAction(path, { isDirectory = false } = {}) {
  // A folder has no default application to run; opening one shows its contents.
  if (isDirectory) return FileOpenAction.Open

  const name = String(path ?? '').split(/[\\/]/).pop()
  const dot = name.lastIndexOf('.')
  // A leading dot is `.zshrc` — a name, not an extension.
  if (dot <= 0) return FileOpenAction.Reveal

  const extension = name.slice(dot + 1).toLowerCase()
  return READABLE_EXTENSIONS.has(extension) ? FileOpenAction.Open : FileOpenAction.Reveal
}
