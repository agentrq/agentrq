// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The account's skills, as the Skills page and a workspace's Skills tab show
 * them.
 *
 * A skill belongs to the account and is turned on per workspace; an agent sees
 * it only where it is on and while the account-wide switch is on too. Every
 * helper that takes a `workspaceId` reads the account's view without one.
 *
 * A skill is a SKILL.md and the files it points to. The list shows only the
 * skills themselves; a skill's page lists its files, and also opens them by
 * following references, so most of what is worth testing here is how a
 * reference turns into a file: which spellings count, where a relative path
 * lands, and which inline code is really a file name.
 */

import { formatMemorySize } from './useMemories';
import { isFork } from './useWorkspaceForks';

/** The file every skill has, and the one opened first. */
export const SKILL_FILE = 'SKILL.md';

/** What SKILL.md may weigh; the meter is measured against it. */
export const SKILL_LIMIT_BYTES = 96 * 1024;

/**
 * Where `renderMarkdown` parks a reference to a skill file. The sanitizer
 * strips an unknown scheme's href, so the target travels in an attribute.
 */
export const SKILL_LINK_ATTR = 'data-skill-link';
export const SKILL_LINK_SELECTOR = `[${SKILL_LINK_ATTR}]`;

// One slash or none is accepted as well as two, as for memory:// links.
const SKILL_SCHEME = /^skill:\/{0,2}/i;
const ANY_SCHEME = /^[a-z][a-z0-9+.-]*:/i;
const NAME = /^[a-z0-9]+(-[a-z0-9]+)*$/;

/** A skill file's URI. */
export function skillUri(skill, path = SKILL_FILE) {
  return `skill://${skill}/${path}`;
}

/** The directory a file sits in, '' at the skill's root. */
export function dirOf(path) {
  const i = String(path ?? '').lastIndexOf('/');
  return i < 0 ? '' : path.slice(0, i);
}

/**
 * `rel` resolved against `dir`, or null when it would leave the skill, names
 * a hidden file, or names nothing.
 */
export function resolveWithin(dir, rel) {
  const out = dir ? dir.split('/') : [];
  for (const segment of rel.split('/')) {
    if (segment === '' || segment === '.') continue;
    if (segment === '..') {
      if (out.length === 0) return null;
      out.pop();
      continue;
    }
    if (segment.startsWith('.')) return null;
    out.push(segment);
  }
  return out.length ? out.join('/') : null;
}

/**
 * The skill and file a skill:// URI names, or null when it is not one.
 * `skill://<name>` alone is its SKILL.md.
 */
export function parseSkillUri(raw) {
  const text = String(raw ?? '').trim();
  if (!SKILL_SCHEME.test(text)) return null;
  const rest = text.replace(SKILL_SCHEME, '');
  const slash = rest.indexOf('/');
  const skill = (slash < 0 ? rest : rest.slice(0, slash)).toLowerCase();
  if (!NAME.test(skill)) return null;
  const tail = slash < 0 ? '' : rest.slice(slash + 1);
  if (tail.replace(/\/+$/, '') === '') return { skill, path: SKILL_FILE };
  const path = resolveWithin('', tail);
  return path ? { skill, path } : null;
}

/**
 * The file a reference in a skill file points to, as `{skill, path}`, or null.
 *
 * Accepts a skill:// URI in any of its spellings, and a relative path —
 * `references/x.md`, `./x.md`, `../x.md` — resolved against the directory of
 * the file being read and never allowed out of the skill. Anything else, a web
 * link or an absolute path, is not a skill file.
 */
export function skillLinkTarget(href, currentSkill, currentPath = SKILL_FILE) {
  const text = String(href ?? '').trim();
  if (SKILL_SCHEME.test(text)) return parseSkillUri(text);
  if (!text || !currentSkill || ANY_SCHEME.test(text) || /^[/#]/.test(text)) return null;
  let rel = text.replace(/[?#].*$/, '');
  try {
    rel = decodeURIComponent(rel);
  } catch {
    return null;
  }
  const path = resolveWithin(dirOf(currentPath), rel);
  return path ? { skill: currentSkill, path } : null;
}

/**
 * The file of the current skill that a piece of inline code names, or null.
 *
 * Real skills name their files in code rather than linking them —
 * `root-cause-tracing.md`, or `skills/brainstorming/visual-companion.md` as
 * the file sits in the skill's repository. Only an exact match against a file
 * that exists counts, from the current file's directory, from the skill's
 * root, or after `/<skill>/`; anything else stays code, so nothing clickable
 * ever leads nowhere.
 */
export function inlineCodeFileMatch(text, files, currentPath = SKILL_FILE, skillName = '') {
  const code = String(text ?? '').trim();
  if (!code || /\s/.test(code)) return null;
  const known = new Set((files || []).map((f) => (typeof f === 'string' ? f : f.path)));

  const candidates = [];
  const uri = parseSkillUri(code);
  if (uri) {
    if (uri.skill === skillName) candidates.push(uri.path);
  } else if (!ANY_SCHEME.test(code)) {
    candidates.push(resolveWithin(dirOf(currentPath), code), resolveWithin('', code));
    const marker = skillName ? `/${skillName}/` : '';
    const at = marker ? `/${code}`.lastIndexOf(marker) : -1;
    if (at >= 0) candidates.push(resolveWithin('', `/${code}`.slice(at + marker.length)));
  }
  return candidates.find((c) => c && known.has(c)) || null;
}

/**
 * A SKILL.md's body without its YAML frontmatter, for the rendered view.
 *
 * Markdown reads the closing `---` as a heading underline, so the frontmatter
 * would render as a bold line of `name: … description: …`. The row already
 * shows the description, and the raw view keeps everything.
 */
export function skillBody(content) {
  const text = String(content ?? '');
  const match = /^---\r?\n[\s\S]*?\r?\n---[ \t]*(\r?\n|$)/.exec(text);
  return match ? text.slice(match[0].length) : text;
}

/** The skill file a click asked for, or null when the click was not on one. */
export function skillLinkFromEvent(event) {
  const anchor = event?.target?.closest?.(SKILL_LINK_SELECTOR);
  return anchor ? parseSkillUri(anchor.getAttribute(SKILL_LINK_ATTR)) : null;
}

/** Skills by name, the order the API returns and the one that stays put. */
export function orderSkills(skills = []) {
  return [...skills].sort((a, b) => a.name.localeCompare(b.name));
}

/** A skill's files, SKILL.md first, then by path. */
export function orderSkillFiles(files = []) {
  return [...files].sort((a, b) => {
    if (a.path === SKILL_FILE) return -1;
    if (b.path === SKILL_FILE) return 1;
    return a.path.localeCompare(b.path);
  });
}

/**
 * A skill's files as the rows of a tree: `{ kind: 'dir', path, name, depth }`
 * or `{ kind: 'file', path, name, depth, sizeBytes }`. At each level SKILL.md
 * comes first, then folders, then files, each by name. The contents of a
 * folder in `collapsed` (a Set of folder paths) are left out.
 */
export function skillFileTree(files = [], collapsed = new Set()) {
  const root = { dirs: new Map(), files: [] };
  for (const f of files) {
    const parts = String(f.path).split('/');
    let node = root;
    for (const part of parts.slice(0, -1)) {
      if (!node.dirs.has(part)) node.dirs.set(part, { dirs: new Map(), files: [] });
      node = node.dirs.get(part);
    }
    node.files.push({ ...f, name: parts.at(-1) });
  }
  const rows = [];
  const walk = (node, prefix, depth) => {
    const file = (f) => rows.push({ kind: 'file', path: f.path, name: f.name, depth, sizeBytes: f.sizeBytes });
    const files = [...node.files].sort((x, y) => x.name.localeCompare(y.name));
    files.filter((f) => f.path === SKILL_FILE).forEach(file);
    for (const name of [...node.dirs.keys()].sort((x, y) => x.localeCompare(y))) {
      const path = prefix + name;
      rows.push({ kind: 'dir', path, name, depth });
      if (!collapsed.has(path)) walk(node.dirs.get(name), `${path}/`, depth + 1);
    }
    files.filter((f) => f.path !== SKILL_FILE).forEach(file);
  };
  walk(root, '', 0);
  return rows;
}

/** A size in the units a person reads — the same as a memory's. */
export const formatSkillSize = formatMemorySize;

/** How full a SKILL.md is against its 96 KB cap, as a percentage 0–100. */
export function skillFullness(bytes) {
  const n = Number(bytes);
  if (!Number.isFinite(n) || n <= 0) return 0;
  return Math.min(100, Math.round((n / SKILL_LIMIT_BYTES) * 100));
}

/**
 * Where a skill came from: "manual", or its repository and the commit it was
 * imported at (the ref when GitHub did not say which commit).
 */
export function skillSource(skill) {
  if (skill?.sourceType !== 'github' || !skill.sourceRepo) return 'manual';
  const at = skill.sourceCommit ? skill.sourceCommit.slice(0, 7) : skill.sourceRef;
  return at ? `GitHub ${skill.sourceRepo}@${at}` : `GitHub ${skill.sourceRepo}`;
}

/**
 * The page that shows one skill: on the Skills page, or in a workspace's
 * settings in place of its Skills tab.
 */
export function skillPagePath(workspaceId, name) {
  if (!workspaceId) return `/skills/${encodeURIComponent(name)}`;
  return `/workspaces/${encodeURIComponent(workspaceId)}/settings/skills/${encodeURIComponent(name)}`;
}

/** The Skills tab of a workspace's settings, where a skill's page leads back to. */
export function skillsTabPath(workspaceId) {
  return `/workspaces/${encodeURIComponent(workspaceId)}/settings?tab=skills`;
}

/** The list a skill's page leads back to: the workspace's tab, or the Skills page. */
export function skillsListPath(workspaceId) {
  return workspaceId ? skillsTabPath(workspaceId) : '/skills';
}

/** What a toast says once a skill is turned on or off for the whole account. */
export function skillEnabledMessage(name, enabled) {
  return enabled ? `${name} is available to agents again` : `${name} is now hidden from agents`;
}

/** What a toast says once a skill is turned on or off in one workspace. */
export function skillWorkspaceMessage(name, on, where = 'this workspace') {
  return `${name} is ${on ? 'on' : 'off'} in ${where}`;
}

/**
 * Where the switch on a skill's card stands: the workspace's own switch in a
 * workspace, the account-wide one on the Skills page. The server leaves
 * `workspaceEnabled` out when it is off.
 */
export function skillSwitchOn(skill, workspaceId) {
  return workspaceId ? skill?.workspaceEnabled === true : skill?.enabled !== false;
}

/** Whether agents see the skill: in a workspace, both switches must be on. */
export function skillSeenByAgents(skill, workspaceId) {
  return skill?.enabled !== false && (!workspaceId || skill?.workspaceEnabled === true);
}

/**
 * A skill with a switch's answer merged in. Read through a workspace, an
 * answer that leaves `workspaceEnabled` out means off, so it is set either way.
 */
export function mergeSkill(skill, updated, workspaceId) {
  const merged = { ...skill, ...updated };
  if (workspaceId) merged.workspaceEnabled = updated?.workspaceEnabled === true;
  return merged;
}

/** "On in 3 workspaces", or that it is on in none. */
export function skillWorkspaceCount(skill) {
  const n = skill?.workspaceIds?.length || 0;
  if (!n) return 'Not on in any workspace';
  return `On in ${n} ${n === 1 ? 'workspace' : 'workspaces'}`;
}

/** `ids` with `id` turned on or off, each id once. */
export function withWorkspace(ids = [], id, on) {
  const rest = ids.filter((x) => String(x) !== String(id));
  return on ? [...rest, String(id)] : rest;
}

/**
 * The workspaces a skill can be turned on in: not archived, and not forks,
 * which use their parent's skills.
 */
export function skillWorkspaceChoices(workspaces = []) {
  return workspaces.filter((w) => !isFork(w) && !w.archivedAt);
}

/** The workspace whose switches a workspace uses: a fork's parent, else itself. */
export function contentWorkspaceId(workspace, workspaceId) {
  return String(workspace?.forkOfId || workspaceId || '');
}

/** "1 file", "3 files". */
export function skillFileCount(n) {
  const count = Number(n) || 0;
  return `${count} ${count === 1 ? 'file' : 'files'}`;
}

/** The segments of the path being read, for the breadcrumb. */
export function skillBreadcrumb(skill, path = SKILL_FILE) {
  return [skill, ...String(path).split('/')];
}

const GITHUB_URL = /^https:\/\/github\.com\/[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+?(\.git)?(\/tree\/[A-Za-z0-9_.-]+(\/[A-Za-z0-9_.\-/]*)?)?\/?$/i;

/**
 * Whether a URL looks like something the importer takes, for a hint while
 * typing. The server decides; this only saves a round trip on a typo.
 */
export function githubImportUrlValid(url) {
  return GITHUB_URL.test(String(url ?? '').trim());
}

/**
 * The skills a repository too large to import whole offers, those that can be
 * chosen first, each by name. A candidate with a reason cannot be imported.
 */
export function orderCandidates(candidates = []) {
  return [...candidates].sort((a, b) => Number(!!a.reason) - Number(!!b.reason) || a.name.localeCompare(b.name) || a.path.localeCompare(b.path));
}

/** The paths of the candidates that can be chosen. */
export function choosablePaths(candidates = []) {
  return candidates.filter((c) => !c.reason).map((c) => c.path);
}

/** What the panel should be showing. */
export const SkillsState = {
  Loading: 'loading',
  Ready: 'ready',
  /** No skills yet — nothing imported and nothing written by an agent. */
  Empty: 'empty',
  /** The list could not be fetched, which is not the same as there being none. */
  Failed: 'failed',
};

/** @param {{ loading: boolean, error: unknown, skills: Array<unknown> }} state */
export function skillsState({ loading, error, skills }) {
  if (loading) return SkillsState.Loading;
  // Before emptiness: a failed fetch leaves the list empty too.
  if (error) return SkillsState.Failed;
  return skills?.length ? SkillsState.Ready : SkillsState.Empty;
}
