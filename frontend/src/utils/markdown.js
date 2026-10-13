// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { marked } from 'marked';
import DOMPurify from 'dompurify';

import { COPY_KIND_ATTR, COPY_KIND_CODE, COPY_TEXT_ATTR, FILE_LINK_ATTR, filePathFromUrl } from '../composables/useMarkdownLinks';
import { MEMORY_LINK_ATTR, memoryLinkTarget } from '../composables/useMemories';
import { SKILL_LINK_ATTR, inlineCodeFileMatch, skillLinkTarget, skillUri } from '../composables/useSkills';

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

// The skill file being rendered, while `renderMarkdown` is given one. marked's
// renderers are registered once for every caller, so the per-call context
// travels here; parsing is synchronous, so it cannot leak into another call.
let skillContext = null;

function skillAnchor(target, inner) {
  const uri = escapeHtml(skillUri(target.skill, target.path));
  return `<a class="md-skill-link" ${SKILL_LINK_ATTR}="${uri}" role="link" tabindex="0" title="${uri}">${inner}</a>`;
}

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

      // Inside a skill, a relative link or a skill:// URI opens another of its
      // files, which the viewer answers rather than the browser.
      const skillTarget = skillContext && skillLinkTarget(href, skillContext.skillName, skillContext.currentPath);
      if (skillTarget) return skillAnchor(skillTarget, this.parser.parseInline(token.tokens));

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
        // A link's own title rides along, for a page that reads it (the Pano's
        // assignees keep their task's status there).
        const linkTitle = token.title ? ` data-link-title="${escapeHtml(token.title)}"` : '';
        return `<span class="md-dead-link" title="${escapeHtml(href)}"${linkTitle}>${this.parser.parseInline(token.tokens)}</span>`;
      }

      return false;
    },

    // Real skills name their files in code rather than linking them; inside
    // a skill, code that is exactly one of its files opens it.
    codespan(token) {
      const path =
        skillContext &&
        inlineCodeFileMatch(token.text, skillContext.skillFiles, skillContext.currentPath, skillContext.skillName);
      if (!path) return false;
      return skillAnchor({ skill: skillContext.skillName, path }, `<code>${escapeHtml(token.text)}</code>`);
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
 * Give every link a button that copies where it points, and every code block
 * one that copies its code.
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
  // A code block is the part of a message most often copied on its own, and
  // selecting it by hand is hard on a phone.
  for (const pre of fragment.querySelectorAll('pre')) {
    const text = pre.textContent.replace(/\n$/, '');
    if (!text) continue;
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'md-copy-code';
    button.setAttribute(COPY_TEXT_ATTR, text);
    button.setAttribute(COPY_KIND_ATTR, COPY_KIND_CODE);
    button.title = 'Copy code';
    button.setAttribute('aria-label', 'Copy code');
    button.innerHTML = COPY_ICON;
    pre.prepend(button);
  }
}

/**
 * @param {string} text
 * @param {{ skillName: string, skillFiles: Array<string|{path: string}>, currentPath?: string }} [skill]
 *   The skill file being rendered, which makes its references to the skill's
 *   other files clickable. Without it, rendering is exactly as it always was.
 */
export function renderMarkdown(text, skill) {
  skillContext = skill?.skillName ? skill : null;
  let html;
  try {
    html = marked.parse(text || '', { breaks: true });
  } finally {
    skillContext = null;
  }
  const clean = DOMPurify.sanitize(html, {
    RETURN_DOM_FRAGMENT: true,
  });
  addCopyButtons(clean);

  const holder = document.createElement('div');
  holder.append(clean);
  return holder.innerHTML;
}
