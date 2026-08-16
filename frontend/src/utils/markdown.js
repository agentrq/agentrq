import { marked } from 'marked';
import DOMPurify from 'dompurify';

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

export function renderMarkdown(text) {
  return DOMPurify.sanitize(marked.parse(text || '', { breaks: true }));
}
