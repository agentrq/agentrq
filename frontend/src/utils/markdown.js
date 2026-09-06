import { marked } from 'marked';
import DOMPurify from 'dompurify';

import { COPY_TEXT_ATTR, FILE_LINK_ATTR, filePathFromUrl } from '../composables/useMarkdownLinks';

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

// A link to a local file gets no href at all: DOMPurify would strip a `file:`
// one anyway, and both builds refuse to navigate to it — see `useMarkdownLinks`,
// which turns the surviving `data-file-url` back into an open request. Every
// other scheme falls through to marked's own anchor, which is why this returns
// false rather than rendering one itself.
marked.use({
  renderer: {
    link(token) {
      if (!FILE_SCHEME.test(token.href)) return false;
      const path = filePathFromUrl(token.href);
      // The tooltip is the decoded path, because the link text is usually just
      // a filename and where the file actually lives is the useful part.
      const title = escapeHtml(token.title || path || token.href);
      return (
        `<a class="md-file-link" ${FILE_LINK_ATTR}="${escapeHtml(token.href)}"` +
        ` role="link" tabindex="0" title="${title}">${this.parser.parseInline(token.tokens)}</a>`
      );
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
    // path it names, not the URL form of it.
    const fileUrl = anchor.getAttribute(FILE_LINK_ATTR);
    const target = fileUrl ? filePathFromUrl(fileUrl) || fileUrl : anchor.getAttribute('href');
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
