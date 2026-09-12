// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { marked } from 'marked';
import DOMPurify from 'dompurify';

import { COPY_TEXT_ATTR, FILE_LINK_ATTR, filePathFromUrl } from '../composables/useMarkdownLinks';
import { MEMORY_LINK_ATTR, memoryLinkTarget } from '../composables/useMemories';

function escapeHtml(text) {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// CommonMark treats any `<word ...>`-shaped text as raw inline HTML and passes
// it through unescaped (e.g. `List<Item>`, `Vec<T>`), which DOMPurify then
// silently drops as an unrecognized tag. Task/message content is plain
// markdown, not embedded HTML, so every `html` token is escaped back to
// visible text instead of being rendered as markup.
marked.use({ renderer: { html({ text }) { return escapeHtml(text); } } });

const FILE_SCHEME = /^file:/i;

// Where a link can point and actually be followed: the web, a mail client, a
// dialler. Everything else either gets its own treatment below or becomes text.
const FOLLOWABLE_SCHEME = /^(https?:|mailto:|tel:|sms:)/i;

// Links the app answers itself rather than letting the browser follow.
//
// Two of the three cases here get no href at all: DOMPurify strips an unknown
// scheme's href anyway, and neither build should navigate to one — so the
// target travels in a data attribute and a delegated handler decides what to
// do. The third case is a link that points nowhere the app can go, which is
// rendered as plain text.
//
// A scheme this does not recognise falls through to marked's own anchor, which
// is why those branches return false rather than rendering one.
marked.use({
  renderer: {
    link(token) {
      const href = token.href;

      // A link between memories, which an index is made of. The scheme makes
      // the intent explicit where a bare `deploys.md` could as easily be a repo
      // path or a typo.
      const memory = memoryLinkTarget(href);
      if (memory) {
        return (
          `<a class="md-memory-link" ${MEMORY_LINK_ATTR}="${escapeHtml(memory)}"` +
          ` role="link" tabindex="0" title="${escapeHtml(memory)}">${this.parser.parseInline(token.tokens)}</a>`
        );
      }

      if (FILE_SCHEME.test(href)) {
        const path = filePathFromUrl(href);
        // The tooltip is the decoded path, because the link text is usually
        // just a filename and where the file actually lives is the useful part.
        const title = escapeHtml(token.title || path || href);
        return (
          `<a class="md-file-link" ${FILE_LINK_ATTR}="${escapeHtml(href)}"` +
          ` role="link" tabindex="0" title="${title}">${this.parser.parseInline(token.tokens)}</a>`
        );
      }

      // Anything left that the app cannot follow becomes text.
      //
      // A relative link like `[notes](deploys.md)` used to render as a real
      // anchor, and clicking one navigated the whole page to a path no route
      // matches — a blank screen, and the reader loses their place. That is
      // worse than not being a link at all, and it happens in task bodies and
      // messages as much as in memories, because agents write relative paths
      // constantly. The target is kept as a tooltip so nothing is lost.
      if (href && !FOLLOWABLE_SCHEME.test(href)) {
        return `<span class="md-dead-link" title="${escapeHtml(href)}">${this.parser.parseInline(token.tokens)}</span>`;
      }

      return false;
    },
  },
});

// A clipboard, drawn at the text's own size so it sits on the line rather than
// beside it. Inline because an <img> would be a request per link.
const COPY_ICON =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"' +
  ' stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
  '<rect x="9" y="9" width="11" height="11" rx="2"></rect>' +
  '<path d="M5 15V5a2 2 0 0 1 2-2h10"></path></svg>';

/**
 * Give every link a button that copies where it points.
 *
 * Done *after* sanitising, on the DOM rather than on a string: these nodes are
 * the app's own, so building them here means no attribute of theirs is ever
 * assembled out of message text — the escaping question simply does not arise.
 *
 * @param {DocumentFragment} fragment
 */
function addCopyButtons(fragment) {
  for (const anchor of fragment.querySelectorAll('a')) {
    // A local file link has no href by then; the useful thing to copy is the
    // path it names, not the URL form of it. A memory link has no href either,
    // and what is worth copying is the name.
    const fileUrl = anchor.getAttribute(FILE_LINK_ATTR);
    const target =
      anchor.getAttribute(MEMORY_LINK_ATTR) ||
      (fileUrl ? filePathFromUrl(fileUrl) || fileUrl : anchor.getAttribute('href'));
    if (!target) continue;

    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'md-copy-link';
    button.setAttribute(COPY_TEXT_ATTR, target);
    button.title = `Copy ${target}`;
    button.setAttribute('aria-label', `Copy ${target}`);
    button.innerHTML = COPY_ICON;
    anchor.after(button);
  }
}

export function renderMarkdown(text) {
  const clean = DOMPurify.sanitize(marked.parse(text || '', { breaks: true }), {
    RETURN_DOM_FRAGMENT: true,
  });
  addCopyButtons(clean);

  const holder = document.createElement('div');
  holder.append(clean);
  return holder.innerHTML;
}
