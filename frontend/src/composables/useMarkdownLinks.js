/**
 * Links inside rendered message content: following one to a local file, and
 * copying any of them.
 *
 * A `file:` href cannot simply be left on the anchor. DOMPurify's default URI
 * allowlist has no `file:` in it, so the sanitiser strips the href and leaves
 * `<a>plan.md</a>` — text that looks like a link and does nothing. Relaxing the
 * allowlist would not help either: a browser tab refuses to navigate to
 * `file://` from an http page, and in the desktop shell a `file:` URL arriving
 * from a message body is exactly the shape of link that must never be followed
 * (`classifyLink` blocks it, deliberately).
 *
 * So the path rides in a `data-file-url` attribute, which survives sanitising,
 * and the anchor carries no href. Clicking it is a request the app answers
 * here, not a navigation — which also means the answer can differ by platform
 * without the markdown differing.
 */

/** Where `renderMarkdown` parks the URL, and what the click handler looks for. */
export const FILE_LINK_ATTR = 'data-file-url';
export const FILE_LINK_SELECTOR = `[${FILE_LINK_ATTR}]`;

/**
 * The file URL a click or keypress asked for, or '' when it asked for nothing.
 *
 * Message bodies are injected as HTML in half a dozen views, so activation is
 * delegated from the app root rather than bound per view — which means this
 * sees every click and every keystroke in the application and must decide
 * quickly that almost all of them are not its business. Space in particular
 * cannot be swallowed up front: that would stop people typing.
 *
 * @param {Event} event
 * @returns {string}
 */
export function fileLinkFromEvent(event) {
  if (event?.type === 'keydown' && event.key !== 'Enter' && event.key !== ' ') return '';
  // `closest` walks up from the click target, which is usually a text node's
  // parent inside the anchor rather than the anchor itself.
  const anchor = event?.target?.closest?.(FILE_LINK_SELECTOR);
  return anchor?.getAttribute(FILE_LINK_ATTR) || '';
}

/**
 * Where `renderMarkdown` parks the text a link's copy button puts on the
 * clipboard — the decoded path for a local file, the URL for anything else.
 */
export const COPY_TEXT_ATTR = 'data-copy-text';
export const COPY_SELECTOR = `[${COPY_TEXT_ATTR}]`;

/**
 * The text a copy button was clicked for, or '' when the click was not one.
 *
 * Buttons fire a click of their own on Enter and space, so unlike a file link
 * this needs no keyboard case.
 *
 * @param {Event} event
 * @returns {string}
 */
export function copyTargetFromEvent(event) {
  if (event?.type !== 'click') return '';
  return event?.target?.closest?.(COPY_SELECTOR)?.getAttribute(COPY_TEXT_ATTR) || '';
}

/**
 * Put text on the clipboard.
 *
 * The desktop shell is asked first, because the browser's Clipboard API
 * refuses to write from a document that is not focused — true of the window
 * behind a dialog, and true again the moment anything steals focus between the
 * click and the write. The shell's clipboard has no such condition. In the
 * browser there is nothing else to use.
 *
 * @param {string} text
 * @param {{ bridge?: unknown, clipboard?: Clipboard }} context
 */
export async function writeClipboard(text, { bridge, clipboard }) {
  if (typeof bridge?.write === 'function') {
    await bridge.write(text);
    return;
  }
  await clipboard.writeText(text);
}

/**
 * Put a link's target on the clipboard.
 *
 * @param {string} text
 * @param {{ copyText?: (text: string) => Promise<void> }} context
 * @returns {Promise<{ tone: 'success'|'error', title: string, message: string }>}
 *          the target is the message either way — on success so it is clear
 *          *what* was copied, and on failure so it can still be read off the
 *          screen and selected by hand.
 */
export async function copyLinkTarget(text, { copyText }) {
  try {
    await copyText?.(text);
    return { tone: 'success', title: 'Copied', message: text };
  } catch {
    // Denied permission, or no secure context.
    return { tone: 'error', title: 'Could not copy', message: text };
  }
}

/** What following the link can do, given where the app is running. */
export const FileLinkAction = {
  /** Ask the desktop shell to open or reveal it. */
  Open: 'open',
  /** No shell to ask: hand the person the path so they can open it themselves. */
  Copy: 'copy',
};

/**
 * The filesystem path a `file:` URL names, decoded for display.
 *
 * Returns '' for anything that is not a local file URL — including
 * `file://server/share/x`, which names a machine that is not this one.
 *
 * @param {string} rawUrl
 * @returns {string}
 */
export function filePathFromUrl(rawUrl) {
  let url;
  try {
    url = new URL(String(rawUrl ?? ''));
  } catch {
    return '';
  }
  if (url.protocol !== 'file:') return '';
  // An empty host is the ordinary `file:///path`; "localhost" is the same thing
  // spelled out. Any other host is a remote share this app cannot reach.
  if (url.host && url.host !== 'localhost') return '';

  let path;
  try {
    path = decodeURIComponent(url.pathname);
  } catch {
    // A stray `%` in a filename is not a reason to lose the whole link.
    path = url.pathname;
  }

  // Windows drive paths arrive as `/C:/Users/…`; the leading slash is an
  // artefact of the URL form and not part of the path.
  return /^\/[A-Za-z]:/.test(path) ? path.slice(1) : path;
}

/**
 * Which of the two things a click can do here.
 *
 * The bridge is checked as well as the platform because those are different
 * questions: a desktop build whose preload predates this feature is still
 * "desktop", and calling a method it does not have would fail silently — the
 * very bug this is fixing. Copying the path is the honest fallback.
 *
 * @param {{ isDesktop: boolean, bridge: unknown }} context
 */
export function fileLinkAction({ isDesktop, bridge }) {
  return isDesktop && typeof bridge?.open === 'function' ? FileLinkAction.Open : FileLinkAction.Copy;
}

/**
 * Follow a local file link.
 *
 * @param {string} rawUrl the `file:` URL from the anchor
 * @param {{ isDesktop: boolean, bridge?: unknown, copyText?: (text: string) => Promise<void> }} context
 * @returns {Promise<{ tone: 'success'|'info'|'error', message: string }>} `message`
 *          is '' when the outcome speaks for itself — the file opened, and a
 *          toast saying so would only be in the way.
 */
export async function followFileLink(rawUrl, { isDesktop, bridge, copyText }) {
  const path = filePathFromUrl(rawUrl);
  if (!path) {
    return { tone: 'error', message: 'That link does not point at a file on this computer.' };
  }

  if (fileLinkAction({ isDesktop, bridge }) === FileLinkAction.Open) {
    let result;
    try {
      result = await bridge.open(rawUrl);
    } catch (err) {
      return { tone: 'error', message: err?.message || `Could not open ${path}` };
    }
    if (!result?.ok) return { tone: 'error', message: result?.error || `Could not open ${path}` };
    // Programs and scripts are shown rather than run, so say which happened —
    // otherwise a file manager appearing instead of the editor looks like a bug.
    return result.revealed
      ? { tone: 'info', message: `AgentRQ does not run files, so ${path} is shown in your file manager instead.` }
      : { tone: 'success', message: '' };
  }

  const preamble = 'A browser cannot open a file on your computer.';
  try {
    await copyText?.(path);
    return { tone: 'info', message: `${preamble} The path is on your clipboard: ${path}` };
  } catch {
    // Clipboard access is denied often enough (no permission, no secure
    // context) that the path itself has to be the fallback.
    return { tone: 'info', message: `${preamble} The file is at ${path}` };
  }
}
