// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The links a task's description and conversation mention, sorted into what
 * they point at: a pull request, an issue, a Pano doc, another task, or
 * anywhere else.
 *
 * Links are read with marked's own lexer, so they are exactly the links the
 * messages render: a URL inside code is not one, and a bare URL ends where the
 * rendered link does, without the full stop after it.
 */
import { marked } from 'marked';

import '../utils/markedAutolinks';

/** The groups, in the order the panel lists them. */
export const LINK_GROUPS = [
  { kind: 'pr', label: 'Pull requests' },
  { kind: 'issue', label: 'Issues' },
  { kind: 'pano', label: 'Pano' },
  { kind: 'task', label: 'Tasks' },
  { kind: 'external', label: 'External' },
];

const GITHUB_ITEM = /^\/([^/]+)\/([^/]+)\/(pull|issues)\/(\d+)(?:\/|$)/;
const TASK_PATHS = [/^\/workspaces\/([^/]+)\/tasks\/([^/]+)\/?$/, /^\/tasks\/[^/]+\/([^/]+)\/([^/]+)\/?$/];
const PANO_PATH = /^\/pano\/([^/]+)\/?$/;

// Messages keep their objects while the task is open, so each is lexed once,
// and again only if its text changes.
const linksByMessage = new WeakMap();

/**
 * Every link in a piece of markdown, in order.
 *
 * @param {string} text
 * @returns {Array<{ href: string, text: string }>}
 */
export function linksInText(text) {
  if (!text) return [];
  const links = [];
  marked.walkTokens(marked.lexer(String(text)), (token) => {
    if (token.type === 'link') links.push({ href: token.href, text: token.text });
  });
  return links;
}

function messageLinks(message) {
  const cached = linksByMessage.get(message);
  if (cached && cached.text === message.text) return cached.links;
  const links = linksInText(message.text);
  linksByMessage.set(message, { text: message.text, links });
  return links;
}

// Link text worth showing in place of the URL: not empty, and not the URL again.
function readableText(link) {
  const text = String(link.text || '').trim();
  return text && text !== link.href && !/^https?:\/\//i.test(text) ? text : '';
}

/**
 * What one link points at, or null for one the panel does not list (a mail
 * address, a local file, a memory).
 *
 * @param {{ href: string, text?: string }} link
 * @param {string} origin this server's origin; a relative link is resolved against it
 */
export function classifyLink(link, origin) {
  let url;
  try {
    url = new URL(link.href, origin);
  } catch {
    return null;
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return null;
  const text = readableText(link);

  if (url.hostname === 'github.com' || url.hostname === 'www.github.com') {
    // `repo#123`, the group telling a pull request from an issue. The panel
    // shows the number first and cuts a long repository name, never the number.
    const item = url.pathname.match(GITHUB_ITEM);
    if (item) {
      const [, owner, repo, path, number] = item;
      const kind = path === 'pull' ? 'pr' : 'issue';
      return { kind, key: `${owner}/${repo}#${number}`, href: url.href, owner, repo, number, label: `${repo}#${number}` };
    }
  }

  if (url.origin === origin) {
    const to = url.pathname + url.search + url.hash;
    for (const pattern of TASK_PATHS) {
      const task = url.pathname.match(pattern);
      if (task && task[2] !== 'new') {
        return { kind: 'task', key: `${task[1]}/${task[2]}`, to, taskId: task[2], label: text || task[2] };
      }
    }
    const pano = url.pathname.match(PANO_PATH);
    if (pano && pano[1] !== 'new' && pano[1] !== 'chat') {
      return { kind: 'pano', key: pano[1], to, label: text || pano[1] };
    }
  }

  const label = text || `${url.host}${url.pathname === '/' ? '' : url.pathname}`;
  return { kind: 'external', key: url.href, href: url.href, label };
}

/**
 * A task's links, grouped and de-duplicated, each in the order it first
 * appears. A link to the task itself is left out. The Pano docs linked to the
 * task come first among its Pano links, by their title, whether or not the
 * conversation mentions them.
 *
 * @param {{ body?: string, messages?: Array<{ text?: string }> }} task
 * @param {{ origin: string, taskId?: string, panoDocs?: Array<{ id: string, title: string }> }} context
 * @returns {{ total: number, groups: Array<{ kind: string, label: string, links: object[] }> }}
 */
export function taskLinks(task, { origin, taskId, panoDocs = [] }) {
  const links = [...linksInText(task?.body), ...(task?.messages || []).flatMap(messageLinks)];
  const seen = new Set();
  const byKind = Object.fromEntries(LINK_GROUPS.map((g) => [g.kind, []]));
  for (const doc of panoDocs) {
    if (seen.has(`pano ${doc.id}`)) continue;
    seen.add(`pano ${doc.id}`);
    byKind.pano.push({ kind: 'pano', key: doc.id, to: `/pano/${doc.id}`, label: doc.title || doc.id });
  }
  for (const link of links) {
    const found = classifyLink(link, origin);
    if (!found || seen.has(`${found.kind} ${found.key}`)) continue;
    if (found.kind === 'task' && found.taskId === taskId) continue;
    seen.add(`${found.kind} ${found.key}`);
    byKind[found.kind].push(found);
  }

  const groups = LINK_GROUPS.map((g) => ({ ...g, links: byKind[g.kind] })).filter((g) => g.links.length);
  return { total: groups.reduce((n, g) => n + g.links.length, 0), groups };
}
