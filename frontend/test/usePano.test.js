// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi } from 'vitest';

import {
  PANO_COLORS,
  PANO_DEFAULT_TTL_DAYS,
  PANO_MAX_FILE_BYTES,
  addFileLink,
  checkPanoItem,
  checklistPercent,
  convertPanoDocToTask,
  expiresLabel,
  filterPanoDocs,
  formatFileSize,
  formatMarkdown,
  isOwnPanoDoc,
  linkPanoFiles,
  panoAuthor,
  panoCheckable,
  panoCheckboxIndex,
  panoColor,
  panoColorClass,
  panoDate,
  panoInitial,
  panoTilt,
  panoLinkName,
  panoTaskBody,
  readPanoUpload,
  recolorPanoDoc,
  timeAgo,
  togglePanoCheckbox,
  usePanoBoard,
  usePanoDoc,
  PANO_TASK_STATUSES,
  assignPanoItem,
  insertMention,
  mentionMatches,
  mentionQuery,
  mentionedWorkspaces,
  panoAssignees,
  panoItemMenus,
  panoItemTaskBody,
  panoItemText,
  panoMentionTaskBody,
  panoMentions,
  pingPanoWorkspaces,
} from '../src/composables/usePano';

const DAY = 24 * 60 * 60 * 1000;

/** A browser File as far as the Pano reads one. */
function fakeFile(name, { type = 'text/plain', bytes = [104, 105], size } = {}) {
  const data = new Uint8Array(bytes);
  return { name, type, size: size ?? data.length, arrayBuffer: () => Promise.resolve(data.buffer) };
}

describe('the post-it colors', () => {
  it('names each color in the server\'s order, yellow first', () => {
    expect(PANO_COLORS.map((c) => c.name)).toEqual(['yellow', 'pink', 'green', 'blue', 'orange', 'purple', 'amber']);
  });

  it('gives a known color its classes and an unknown one yellow\'s', () => {
    expect(panoColorClass('pink')).toBe(PANO_COLORS[1].card);
    expect(panoColorClass('magenta')).toBe(PANO_COLORS[0].card);
    expect(panoColorClass(undefined)).toBe(PANO_COLORS[0].card);
  });

  it('finds a color by name, and an unknown one is yellow', () => {
    expect(panoColor('green')).toBe(PANO_COLORS[2]);
    expect(panoColor('magenta')).toBe(PANO_COLORS[0]);
    expect(panoColor(undefined)).toBe(PANO_COLORS[0]);
  });

  it('draws every color as the Pano\'s design does', () => {
    // [name, paper, title, body, footer, label, label text, accent, avatar, track, tape]
    const design = [
      ['yellow', '#fffbeb', '#713f12', '#542d0c', '#854d0e', '#fef08a', '#713f12', '#b15f00', '#b15f00', '#fef9c3', '#fef08a'],
      ['pink', '#fdf2f8', '#9d174d', '#831843', '#be185d', '#fce7f3', '#be185d', '#db2777', '#be185d', '#fce7f3', '#fbcfe8'],
      ['green', '#f0fdf4', '#166534', '#14532d', '#15803d', '#dcfce7', '#15803d', '#16a34a', '#15803d', '#dcfce7', '#bbf7d0'],
      ['blue', '#eff6ff', '#1e40af', '#1e3a8a', '#1e40af', '#dbeafe', '#1e40af', '#2563eb', '#1e40af', '#dbeafe', '#bfdbfe'],
      ['orange', '#fff1f2', '#881337', '#4c0519', '#9f1239', '#ffe4e6', '#be123c', '#e11d48', '#be123c', '#ffe4e6', '#fecdd3'],
      ['purple', '#faf5ff', '#6b21a8', '#581c87', '#7e22ce', '#f3e8ff', '#7e22ce', '#9333ea', '#7e22ce', '#f3e8ff', '#e9d5ff'],
      ['amber', '#fef3c7', '#92400e', '#451a03', '#92400e', '#fde68a', '#92400e', '#b45309', '#b45309', '#fde68a', '#fcd34d'],
    ];
    for (const [name, paper, title, body, muted, tag, tagInk, accent, avatar, track, tape] of design) {
      const c = panoColor(name);
      expect([c.card, c.title, c.body, c.muted]).toEqual([`bg-[${paper}]`, `text-[${title}]`, `text-[${body}]`, `text-[${muted}]`]);
      expect([c.tag, c.tagHex, c.tagInkHex, c.ink]).toEqual([`bg-[${tag}] text-[${tagInk}]`, tag, tagInk, `text-[${tagInk}]`]);
      expect([c.inkBg, c.inkHex, c.avatar, c.track, c.tape]).toEqual([`bg-[${accent}]`, accent, `bg-[${avatar}]`, `bg-[${track}]`, `bg-[${tape}]/80`]);
      expect(c.swatch).toMatch(/^bg-\[#[0-9a-f]{6}\]$/);
      expect(c.dot).toMatch(/^bg-\[#[0-9a-f]{6}\]$/);
      expect(c.label).toMatch(/^[A-Z][a-z]+( [a-z]+)?$/);
    }
    expect(new Set(PANO_COLORS.map((c) => c.label)).size).toBe(PANO_COLORS.length);
  });
});

describe('filtering the board', () => {
  const docs = [{ title: 'Release Plan' }, { title: 'Notes' }, {}];

  it('keeps every doc for no query, or only blanks', () => {
    expect(filterPanoDocs(docs, '')).toBe(docs);
    expect(filterPanoDocs(docs, null)).toBe(docs);
    expect(filterPanoDocs(docs, '   ')).toBe(docs);
  });

  it('keeps only the docs in the color picked, a doc with no color counting as yellow', () => {
    const colored = [{ title: 'a', color: 'blue' }, { title: 'b' }, { title: 'ab', color: 'blue' }];
    expect(filterPanoDocs(colored, '', 'blue').map((d) => d.title)).toEqual(['a', 'ab']);
    expect(filterPanoDocs(colored, '', 'yellow').map((d) => d.title)).toEqual(['b']);
    expect(filterPanoDocs(colored, 'b', 'blue').map((d) => d.title)).toEqual(['ab']);
  });

  it('keeps the docs whose title holds the query, ignoring case', () => {
    expect(filterPanoDocs(docs, ' release ')).toEqual([{ title: 'Release Plan' }]);
    expect(filterPanoDocs(docs, 'zzz')).toEqual([]);
  });
});

describe('who wrote it', () => {
  it('is "You" for the person, else the workspace by name or id', () => {
    expect(panoAuthor('')).toBe('You');
    expect(panoAuthor(null, [{ id: 'ws1', name: 'Home' }])).toBe('You');
    expect(panoAuthor('ws1', [{ id: 'ws1', name: 'Home' }])).toBe('Home');
    expect(panoAuthor('ws9', [{ id: 'ws1', name: 'Home' }])).toBe('Workspace ws9');
    expect(panoAuthor('ws9')).toBe('Workspace ws9');
  });
});

describe('when a doc expires', () => {
  const now = Date.UTC(2026, 9, 13, 12);

  it('counts whole days left', () => {
    expect(expiresLabel(new Date(now + 3 * DAY + 1000).toISOString(), now)).toBe('Expires in 3 days');
    expect(expiresLabel(new Date(now + DAY + 1000).toISOString(), now)).toBe('Expires in 1 day');
  });

  it('says today for less than a day, a past date or none at all', () => {
    expect(expiresLabel(new Date(now + 1000).toISOString(), now)).toBe('Expires today');
    expect(expiresLabel(new Date(now - DAY).toISOString(), now)).toBe('Expires today');
    expect(expiresLabel('not a date', now)).toBe('Expires today');
  });

  it('counts from now when not told otherwise', () => {
    expect(expiresLabel(new Date(Date.now() + 10 * DAY + 60000).toISOString())).toBe('Expires in 10 days');
  });
});

describe('a linked file\'s name', () => {
  it('turns what a link cannot name into "-", and drops a leading one', () => {
    expect(panoLinkName('my chart (1).png')).toBe('my-chart-1-.png');
    expect(panoLinkName('.hidden')).toBe('hidden');
    expect(panoLinkName('__x')).toBe('x');
  });

  it('keeps at most 128 characters', () => {
    expect(panoLinkName('a'.repeat(200))).toBe('a'.repeat(128));
  });

  it('is "file" for a name with nothing left', () => {
    expect(panoLinkName('...')).toBe('file');
    expect(panoLinkName('')).toBe('file');
    expect(panoLinkName(undefined)).toBe('file');
  });
});

describe('linking a file from the markdown', () => {
  it('embeds an image and links anything else', () => {
    expect(addFileLink('', 'a.png', 'image/png')).toBe('![a.png](a.png)');
    expect(addFileLink(null, 'a.txt', 'text/plain')).toBe('[a.txt](a.txt)');
    expect(addFileLink(undefined, 'a.bin')).toBe('[a.bin](a.bin)');
  });

  it('puts the link on a line of its own', () => {
    expect(addFileLink('# Notes\n', 'a.txt', 'text/plain')).toBe('# Notes\n[a.txt](a.txt)');
    expect(addFileLink('# Notes', 'a.txt', 'text/plain')).toBe('# Notes\n\n[a.txt](a.txt)');
  });
});

describe('reading an upload', () => {
  it('base64s the content under the file\'s own name and type', async () => {
    await expect(readPanoUpload(fakeFile('hi.txt'))).resolves.toEqual({ filename: 'hi.txt', mimeType: 'text/plain', data: 'aGk=' });
  });

  it('takes another name, and no type as empty', async () => {
    await expect(readPanoUpload(fakeFile('hi there.txt', { type: '' }), 'hi-there.txt')).resolves.toEqual({
      filename: 'hi-there.txt', mimeType: '', data: 'aGk=',
    });
  });

  it('reads a file larger than one chunk whole', async () => {
    const bytes = Array.from({ length: 0x8000 * 2 + 5 }, (_, i) => i % 256);
    const upload = await readPanoUpload(fakeFile('big.bin', { bytes }));
    expect(Uint8Array.from(atob(upload.data), (c) => c.charCodeAt(0))).toEqual(new Uint8Array(bytes));
  });
});

describe('pointing links at the doc\'s files', () => {
  const files = [
    { filename: 'chart.png', url: 'https://x/storage/chart' },
    { filename: 'a b.txt', url: 'https://x/storage/ab' },
    { filename: 'nourl.txt' },
  ];

  it('leaves the html as it is for a doc with no files', () => {
    expect(linkPanoFiles('<p>x</p>')).toBe('<p>x</p>');
    expect(linkPanoFiles('<p>x</p>', [])).toBe('<p>x</p>');
  });

  it('points images and links at the files they name, however they name them', () => {
    const html = [
      '<img src="chart.png">',
      '<img src="./chart.png?raw#top">',
      '<a href="a%20b.txt">ab</a>',
      '<a href="%E0%A4%A">bad</a>',
      '<a href="nourl.txt">n</a>',
      '<a href="">empty</a>',
      '<a href="https://elsewhere/x">x</a>',
    ].join('');
    const holder = document.createElement('div');
    holder.innerHTML = linkPanoFiles(html, files);
    expect([...holder.querySelectorAll('img')].map((el) => el.getAttribute('src'))).toEqual([
      'https://x/storage/chart', 'https://x/storage/chart',
    ]);
    expect([...holder.querySelectorAll('a')].map((el) => el.getAttribute('href'))).toEqual([
      'https://x/storage/ab', '%E0%A4%A', 'nourl.txt', '', 'https://elsewhere/x',
    ]);
  });

  it('links a file named by an undecodable target', () => {
    const out = linkPanoFiles('<a href="%E0%A4%A">bad</a>', [{ filename: '%E0%A4%A', url: 'u' }]);
    expect(out).toBe('<a href="u">bad</a>');
  });
});

describe('dates and sizes', () => {
  it('shows a date as month, day and 24-hour time', () => {
    const iso = new Date(2026, 9, 13, 14, 5).toISOString();
    expect(panoDate(iso)).toBe('Oct 13, 14:05');
  });

  it('shows a size in B, KB or MB', () => {
    expect(formatFileSize(undefined)).toBe('0 B');
    expect(formatFileSize('abc')).toBe('0 B');
    expect(formatFileSize(1023)).toBe('1023 B');
    expect(formatFileSize(1536)).toBe('1.5 KB');
    expect(formatFileSize(PANO_MAX_FILE_BYTES)).toBe('2.0 MB');
  });
});

describe('how long ago', () => {
  const now = Date.UTC(2026, 9, 13, 12);
  const ago = (ms) => new Date(now - ms).toISOString();

  it('is "just now" under a minute, in the future, or for no date', () => {
    expect(timeAgo(ago(0), now)).toBe('just now');
    expect(timeAgo(ago(59 * 1000), now)).toBe('just now');
    expect(timeAgo(ago(-5 * 60000), now)).toBe('just now');
    expect(timeAgo('not a date', now)).toBe('just now');
  });

  it('counts minutes, then hours, then days', () => {
    expect(timeAgo(ago(60000), now)).toBe('1m ago');
    expect(timeAgo(ago(59 * 60000), now)).toBe('59m ago');
    expect(timeAgo(ago(60 * 60000), now)).toBe('1h ago');
    expect(timeAgo(ago(DAY - 1), now)).toBe('23h ago');
    expect(timeAgo(ago(DAY), now)).toBe('1d ago');
    expect(timeAgo(ago(3 * DAY + 5000), now)).toBe('3d ago');
  });

  it('counts from now when not told otherwise', () => {
    expect(timeAgo(new Date(Date.now() - 2 * 60 * 60000 - 1000).toISOString())).toBe('2h ago');
  });
});

describe('a name\'s mark', () => {
  it('is its first letter, upper-cased, ignoring leading blanks', () => {
    expect(panoInitial('ops')).toBe('O');
    expect(panoInitial('  you')).toBe('Y');
  });

  it('is "?" for no name', () => {
    expect(panoInitial('')).toBe('?');
    expect(panoInitial('   ')).toBe('?');
    expect(panoInitial(null)).toBe('?');
  });
});

describe('the formatting buttons', () => {
  it('wraps the selection, and selects what it wrapped', () => {
    expect(formatMarkdown('say hi now', 4, 6, 'bold')).toEqual({ value: 'say **hi** now', start: 6, end: 8 });
    expect(formatMarkdown('say hi now', 4, 6, 'italic')).toEqual({ value: 'say _hi_ now', start: 5, end: 7 });
    expect(formatMarkdown('say hi now', 4, 6, 'code')).toEqual({ value: 'say `hi` now', start: 5, end: 7 });
  });

  it('puts a word to replace where nothing is selected, and selects it', () => {
    expect(formatMarkdown('ab', 1, 1, 'bold')).toEqual({ value: 'a**bold**b', start: 3, end: 7 });
    expect(formatMarkdown(null, 0, 0, 'italic')).toEqual({ value: '_italic_', start: 1, end: 7 });
    expect(formatMarkdown(undefined, 0, 0, 'code')).toEqual({ value: '`code`', start: 1, end: 5 });
  });

  it('starts the line the caret is on, keeping the caret where it was in the text', () => {
    expect(formatMarkdown('one\ntwo', 5, 5, 'list')).toEqual({ value: 'one\n- two', start: 7, end: 7 });
    expect(formatMarkdown('one\ntwo', 7, 7, 'list')).toEqual({ value: 'one\n- two', start: 9, end: 9 });
    expect(formatMarkdown('', 0, 0, 'checkbox')).toEqual({ value: '- [ ] ', start: 6, end: 6 });
  });

  it('starts every selected line, from the start of the first', () => {
    const r = formatMarkdown('a\nbb\ncc\nd', 3, 7, 'checkbox');
    expect(r.value).toBe('a\n- [ ] bb\n- [ ] cc\nd');
    expect(r.start).toBe(r.end);
    expect(r.value.slice(0, r.start)).toBe('a\n- [ ] bb\n- [ ] cc');
  });
});

describe('ticking a checklist item', () => {
  const md = ['# List', '- [ ] one', '* [x] two', '```', '- [ ] in code', '```', '  ~~~', '- [ ] tilde code', '  ~~~', '1. [X] three', '2) [ ]', '- [y] not one', '-[ ] nor this', '+ [ ]tight'].join('\n');

  it('checks and unchecks the item by its place, counting only real items', () => {
    expect(togglePanoCheckbox(md, 0, true).split('\n')[1]).toBe('- [x] one');
    expect(togglePanoCheckbox(md, 1, false).split('\n')[2]).toBe('* [ ] two');
    expect(togglePanoCheckbox(md, 2, false).split('\n')[9]).toBe('1. [ ] three');
    expect(togglePanoCheckbox(md, 3, true).split('\n')[10]).toBe('2) [x]');
  });

  it('changes nothing else, and nothing for an item that is not there', () => {
    const out = togglePanoCheckbox(md, 0, true).split('\n');
    const was = md.split('\n');
    expect(out.filter((l, i) => l !== was[i])).toEqual(['- [x] one']);
    expect(togglePanoCheckbox(md, 4, true)).toBe(md);
    expect(togglePanoCheckbox(md, -1, true)).toBe(md);
  });

  it('keeps what follows the box, and takes no markdown as none', () => {
    expect(togglePanoCheckbox('  - [ ] keep [this]', 0, true)).toBe('  - [x] keep [this]');
    expect(togglePanoCheckbox(null, 0, true)).toBe('');
    expect(togglePanoCheckbox(undefined, 0, true)).toBe('');
  });

  it('counts nothing in a fence left open', () => {
    expect(togglePanoCheckbox('```\n- [ ] a', 0, true)).toBe('```\n- [ ] a');
  });
});

describe('clickable checkboxes', () => {
  it('drops disabled from checkboxes only', () => {
    expect(panoCheckable('<li><input disabled="" type="checkbox"> a</li>')).toBe('<li><input type="checkbox"> a</li>');
    expect(panoCheckable('<input checked="" disabled="" type="checkbox">')).toBe('<input checked="" type="checkbox">');
    expect(panoCheckable('<input type="checkbox" disabled>')).toBe('<input type="checkbox">');
    expect(panoCheckable('<input disabled="" type="text">')).toBe('<input disabled="" type="text">');
    expect(panoCheckable('<p>x</p>')).toBe('<p>x</p>');
  });

  it('takes no html as none', () => {
    expect(panoCheckable(null)).toBe('');
    expect(panoCheckable(undefined)).toBe('');
  });

  it('says which box was clicked, or -1 for anything else', () => {
    const holder = document.createElement('div');
    holder.innerHTML = '<input type="checkbox"><p>x</p><input type="text"><input type="checkbox">';
    const [first, p, text, second] = holder.children;
    expect(panoCheckboxIndex(holder, first)).toBe(0);
    expect(panoCheckboxIndex(holder, second)).toBe(1);
    expect(panoCheckboxIndex(holder, p)).toBe(-1);
    expect(panoCheckboxIndex(holder, text)).toBe(-1);
    expect(panoCheckboxIndex(holder, null)).toBe(-1);
    expect(panoCheckboxIndex(holder, document.createTextNode('t'))).toBe(-1);
    expect(panoCheckboxIndex(holder, document.createElement('input'))).toBe(-1);
    const elsewhere = document.createElement('input');
    elsewhere.type = 'checkbox';
    expect(panoCheckboxIndex(holder, elsewhere)).toBe(-1);
  });
});

describe('how much of a checklist is done', () => {
  it('is a whole percentage, and 0 for no checklist', () => {
    expect(checklistPercent({ checklistDone: 1, checklistTotal: 3 })).toBe(33);
    expect(checklistPercent({ checklistDone: 2, checklistTotal: 3 })).toBe(67);
    expect(checklistPercent({ checklistDone: 6, checklistTotal: 6 })).toBe(100);
    expect(checklistPercent({ checklistDone: 0, checklistTotal: 0 })).toBe(0);
    expect(checklistPercent({})).toBe(0);
    expect(checklistPercent(null)).toBe(0);
  });
});

describe('changing a card\'s doc from the board', () => {
  const full = { id: 'd1', title: 'Plan', markdown: '- [ ] a\n- [ ] b', color: 'blue' };
  const fakeApi = () => ({
    getPanoDoc: vi.fn(() => Promise.resolve({ doc: full })),
    updatePanoDoc: vi.fn((id, body) => Promise.resolve({ doc: { id, ...body } })),
  });

  it('reads the doc, ticks the item and writes it back as it was otherwise', async () => {
    const api = fakeApi();
    const out = await checkPanoItem({ id: 'd1' }, 1, true, { api });
    expect(api.getPanoDoc).toHaveBeenCalledWith('d1');
    expect(api.updatePanoDoc).toHaveBeenCalledWith('d1', { title: 'Plan', markdown: '- [ ] a\n- [x] b', ttlDays: 0, color: '', files: [] });
    expect(out.markdown).toBe('- [ ] a\n- [x] b');
  });

  it('reads the doc and writes it back in another color', async () => {
    const api = fakeApi();
    const out = await recolorPanoDoc({ id: 'd1' }, 'pink', { api });
    expect(api.updatePanoDoc).toHaveBeenCalledWith('d1', { title: 'Plan', markdown: full.markdown, ttlDays: 0, color: 'pink', files: [] });
    expect(out.color).toBe('pink');
  });

  it('passes a failure on', async () => {
    const api = fakeApi();
    api.getPanoDoc.mockRejectedValueOnce(new Error('gone'));
    await expect(recolorPanoDoc({ id: 'd1' }, 'pink', { api })).rejects.toThrow('gone');
    api.updatePanoDoc.mockRejectedValueOnce(new Error('no'));
    await expect(checkPanoItem({ id: 'd1' }, 0, true, { api })).rejects.toThrow('no');
  });
});

describe('turning a doc into a task', () => {
  it('is for the person\'s own docs only', () => {
    expect(isOwnPanoDoc({ id: 'd1' })).toBe(true);
    expect(isOwnPanoDoc({ id: 'd1', createdByWorkspaceId: 'ws1' })).toBe(false);
    expect(isOwnPanoDoc(null)).toBe(true);
  });

  it('carries the markdown and where it came from', () => {
    expect(panoTaskBody({ id: 'd1', markdown: '# Hi' })).toBe(
      '# Hi\n\n---\nFrom the Pano doc `d1`: read it with the pano tool for its files and comments.',
    );
  });

  it('creates the task from a doc it already has whole', async () => {
    const api = { getPanoDoc: vi.fn(), createTask: vi.fn(() => Promise.resolve({ task: { id: 't1' } })) };
    const task = await convertPanoDocToTask({ id: 'd1', title: 'Plan', markdown: 'x' }, 'ws1', { api });
    expect(task).toEqual({ id: 't1' });
    expect(api.getPanoDoc).not.toHaveBeenCalled();
    expect(api.createTask).toHaveBeenCalledWith('ws1', 'Plan', panoTaskBody({ id: 'd1', markdown: 'x' }));
  });

  it('reads a list card\'s doc first, which has no markdown', async () => {
    const api = {
      getPanoDoc: vi.fn(() => Promise.resolve({ doc: { id: 'd1', title: 'Plan', markdown: 'full' } })),
      createTask: vi.fn(() => Promise.resolve({ task: { id: 't2' } })),
    };
    await expect(convertPanoDocToTask({ id: 'd1', title: 'Plan' }, 'ws2', { api })).resolves.toEqual({ id: 't2' });
    expect(api.getPanoDoc).toHaveBeenCalledWith('d1');
    expect(api.createTask).toHaveBeenCalledWith('ws2', 'Plan', panoTaskBody({ id: 'd1', markdown: 'full' }));
  });
});

describe('the board', () => {
  it('loads the docs, showing it is loading, and filters them by title', async () => {
    let resolve;
    const api = { listPanoDocs: vi.fn(() => new Promise((r) => { resolve = r; })) };
    const board = usePanoBoard({ api });
    const loading = board.load();
    expect(board.loading.value).toBe(true);
    resolve({ docs: [{ id: 'a', title: 'Alpha' }, { id: 'b', title: 'Beta' }] });
    await loading;
    expect(board.loading.value).toBe(false);
    expect(board.shown.value).toHaveLength(2);
    board.query.value = 'bet';
    expect(board.shown.value.map((d) => d.id)).toEqual(['b']);
  });

  it('reloads quietly, and takes no docs as none', async () => {
    let resolve;
    const api = { listPanoDocs: vi.fn(() => new Promise((r) => { resolve = r; })) };
    const board = usePanoBoard({ api });
    const loading = board.load({ quiet: true });
    expect(board.loading.value).toBe(false);
    resolve({});
    await loading;
    expect(board.docs.value).toEqual([]);
  });

  it('says what failed, and clears it on the next load', async () => {
    const api = { listPanoDocs: vi.fn(() => Promise.reject(new Error('boom'))) };
    const board = usePanoBoard({ api });
    await board.load();
    expect(board.error.value).toBe('boom');
    api.listPanoDocs.mockRejectedValueOnce({});
    await board.load();
    expect(board.error.value).toBe('Failed to fetch the Pano');
    api.listPanoDocs.mockResolvedValueOnce({ docs: [] });
    await board.load();
    expect(board.error.value).toBe('');
    expect(board.loading.value).toBe(false);
  });
});

describe('one doc', () => {
  const DOC = { id: 'd1', title: 'Plan', markdown: '# Plan', ttlDays: 7, color: 'blue' };
  const fakeApi = (overrides = {}) => ({
    getPanoDoc: vi.fn(() => Promise.resolve({ doc: DOC })),
    createPanoDoc: vi.fn(() => Promise.resolve({ doc: { id: 'new1' } })),
    updatePanoDoc: vi.fn(() => Promise.resolve({ doc: { id: 'd1' } })),
    deletePanoDoc: vi.fn(() => Promise.resolve(true)),
    addPanoComment: vi.fn(() => Promise.resolve({})),
    attachPanoFiles: vi.fn(() => Promise.resolve({})),
    ...overrides,
  });

  it('opens a doc not written yet in the editor, with the defaults, and reads nothing', async () => {
    const api = fakeApi();
    const d = usePanoDoc(null, { api });
    expect(d.editing.value).toBe(true);
    expect(d.draft.value).toEqual({ title: '', markdown: '', ttlDays: PANO_DEFAULT_TTL_DAYS, color: 'yellow', files: [] });
    await d.load();
    expect(api.getPanoDoc).not.toHaveBeenCalled();
  });

  it('starts a new doc in the color asked for, or yellow for an unknown one', () => {
    expect(usePanoDoc(null, { api: fakeApi(), color: 'pink' }).draft.value.color).toBe('pink');
    expect(usePanoDoc(null, { api: fakeApi(), color: 'magenta' }).draft.value.color).toBe('yellow');
    expect(usePanoDoc(null, { api: fakeApi(), color: undefined }).draft.value.color).toBe('yellow');
  });

  it('reads a doc, showing it is loading unless quiet', async () => {
    const api = fakeApi();
    const d = usePanoDoc('d1', { api });
    expect(d.editing.value).toBe(false);
    const p = d.load();
    expect(d.loading.value).toBe(true);
    await p;
    expect(d.loading.value).toBe(false);
    expect(d.doc.value).toEqual(DOC);
    const q = d.load({ quiet: true });
    expect(d.loading.value).toBe(false);
    await q;
  });

  it('says a missing doc is missing or expired, and any other failure as it is', async () => {
    const api = fakeApi({ getPanoDoc: vi.fn(() => Promise.reject(Object.assign(new Error('nf'), { status: 404 }))) });
    const d = usePanoDoc('d1', { api });
    await d.load();
    expect(d.error.value).toBe('This Pano doc does not exist, or it has expired.');
    api.getPanoDoc.mockRejectedValueOnce(new Error('down'));
    await d.load();
    expect(d.error.value).toBe('down');
    api.getPanoDoc.mockRejectedValueOnce({});
    await d.load();
    expect(d.error.value).toBe('Failed to fetch the Pano doc');
    api.getPanoDoc.mockResolvedValueOnce({ doc: DOC });
    await d.load();
    expect(d.error.value).toBe('');
  });

  it('edits a copy of the doc, and cancels the edit', async () => {
    const d = usePanoDoc('d1', { api: fakeApi() });
    await d.load();
    d.startEditing();
    expect(d.editing.value).toBe(true);
    expect(d.draft.value).toEqual({ title: 'Plan', markdown: '# Plan', ttlDays: 7, color: 'blue', files: [] });
    d.cancelEditing();
    expect(d.editing.value).toBe(false);
  });

  it('adds a file to the draft, linked, and replaces one of the same name', async () => {
    const d = usePanoDoc(null, { api: fakeApi() });
    await d.addDraftFile(fakeFile('my chart.png', { type: 'image/png' }));
    expect(d.draft.value.markdown).toBe('![my-chart.png](my-chart.png)');
    await d.addDraftFile(fakeFile('notes.txt'));
    await d.addDraftFile(fakeFile('my chart.png', { type: 'image/png', bytes: [1] }));
    expect(d.draft.value.files.map((f) => f.filename)).toEqual(['notes.txt', 'my-chart.png']);
    expect(d.draft.value.files[1].data).toBe('AQ==');
    d.removeDraftFile('notes.txt');
    expect(d.draft.value.files.map((f) => f.filename)).toEqual(['my-chart.png']);
  });

  it('refuses a draft file over the limit', async () => {
    const d = usePanoDoc(null, { api: fakeApi() });
    await expect(d.addDraftFile(fakeFile('huge.bin', { size: PANO_MAX_FILE_BYTES + 1 }))).rejects.toThrow('huge.bin is over the 2.0 MB limit');
    expect(d.draft.value.files).toEqual([]);
  });

  it('posts a new doc and returns its id', async () => {
    const api = fakeApi();
    const d = usePanoDoc(null, { api });
    Object.assign(d.draft.value, { title: 'T', markdown: 'M', ttlDays: 'x', color: 'green' });
    const p = d.save();
    expect(d.busy.value).toBe(true);
    await expect(p).resolves.toBe('new1');
    expect(api.createPanoDoc).toHaveBeenCalledWith({ title: 'T', markdown: 'M', ttlDays: 0, color: 'green', files: [] });
    expect(api.getPanoDoc).not.toHaveBeenCalled();
    expect(d.editing.value).toBe(false);
    expect(d.busy.value).toBe(false);
  });

  it('saves an edit and reads the doc back', async () => {
    const api = fakeApi();
    const d = usePanoDoc('d1', { api });
    await d.load();
    d.startEditing();
    d.draft.value.ttlDays = '14';
    await expect(d.save()).resolves.toBe('d1');
    expect(api.updatePanoDoc).toHaveBeenCalledWith('d1', { title: 'Plan', markdown: '# Plan', ttlDays: 14, color: 'blue', files: [] });
    expect(api.getPanoDoc).toHaveBeenCalledTimes(2);
    expect(d.editing.value).toBe(false);
  });

  it('stays in the editor when the save fails', async () => {
    const api = fakeApi({ createPanoDoc: vi.fn(() => Promise.reject(new Error('file x.png is not linked'))) });
    const d = usePanoDoc(null, { api });
    await expect(d.save()).rejects.toThrow('file x.png is not linked');
    expect(d.editing.value).toBe(true);
    expect(d.busy.value).toBe(false);
  });

  it('ticks a checklist item of the doc as shown, and reads it back', async () => {
    const api = fakeApi({ getPanoDoc: vi.fn(() => Promise.resolve({ doc: { ...DOC, markdown: '- [ ] a\n- [x] b' } })) });
    const d = usePanoDoc('d1', { api });
    await d.load();
    const p = d.checkItem(1, false);
    expect(d.busy.value).toBe(true);
    await p;
    expect(api.updatePanoDoc).toHaveBeenCalledWith('d1', { title: 'Plan', markdown: '- [ ] a\n- [ ] b', ttlDays: 0, color: '', files: [] });
    expect(api.getPanoDoc).toHaveBeenCalledTimes(2);
    expect(d.busy.value).toBe(false);
    api.updatePanoDoc.mockRejectedValueOnce(new Error('no'));
    await expect(d.checkItem(0, true)).rejects.toThrow('no');
    expect(api.getPanoDoc).toHaveBeenCalledTimes(2);
    expect(d.busy.value).toBe(false);
  });

  it('pings each workspace a comment mentions, after posting it', async () => {
    const api = fakeApi({ createTask: vi.fn((ws) => Promise.resolve({ task: { id: `t-${ws}` } })) });
    const d = usePanoDoc('d1', { api });
    const wait = vi.fn(() => Promise.resolve());
    const pinged = await d.comment('@api please', [{ id: 'w1', name: 'api' }, { id: 'w2', name: 'web' }], { wait });
    expect(pinged).toEqual({ tasks: [{ id: 't-w1' }, { id: 't-w2' }], failed: [] });
    expect(wait).toHaveBeenCalledTimes(1);
    expect(api.createTask).toHaveBeenCalledWith('w1', 'Pano: Plan', panoMentionTaskBody(DOC, '@api please'));
  });

  it('assigns a checklist item: a task for the workspace, linked from the item', async () => {
    const api = fakeApi({
      getPanoDoc: vi.fn(() => Promise.resolve({ doc: { ...DOC, markdown: '- [ ] ship it\n- [ ] docs' } })),
      createTask: vi.fn(() => Promise.resolve({ task: { id: 't9' } })),
    });
    const d = usePanoDoc('d1', { api });
    await d.load();
    const p = d.assignItem(0, { id: 'w1', name: 'api' });
    expect(d.busy.value).toBe(true);
    expect(await p).toEqual({ id: 't9' });
    expect(api.createTask).toHaveBeenCalledWith('w1', 'ship it', panoItemTaskBody({ ...DOC, markdown: '- [ ] ship it\n- [ ] docs' }, 'ship it'));
    expect(api.updatePanoDoc).toHaveBeenCalledWith('d1', {
      title: 'Plan', markdown: '- [ ] ship it → [@api](/workspaces/w1/tasks/t9 "notstarted")\n- [ ] docs', ttlDays: 0, color: '', files: [],
    });
    expect(api.getPanoDoc).toHaveBeenCalledTimes(2);
    expect(d.busy.value).toBe(false);
    api.createTask.mockRejectedValueOnce(new Error('no'));
    await expect(d.assignItem(0, { id: 'w1', name: 'api' })).rejects.toThrow('no');
    expect(api.updatePanoDoc).toHaveBeenCalledTimes(1);
    expect(d.busy.value).toBe(false);
  });

  it('deletes the doc', async () => {
    const api = fakeApi();
    const d = usePanoDoc('d1', { api });
    await d.remove();
    expect(api.deletePanoDoc).toHaveBeenCalledWith('d1');
    expect(d.busy.value).toBe(false);
    api.deletePanoDoc.mockRejectedValueOnce(new Error('no'));
    await expect(d.remove()).rejects.toThrow('no');
    expect(d.busy.value).toBe(false);
  });

  it('comments and reads the doc back', async () => {
    const api = fakeApi();
    const d = usePanoDoc('d1', { api });
    expect(await d.comment('looks good')).toEqual({ tasks: [], failed: [] });
    expect(api.addPanoComment).toHaveBeenCalledWith('d1', 'looks good');
    expect(api.getPanoDoc).toHaveBeenCalledTimes(1);
    api.addPanoComment.mockRejectedValueOnce(new Error('no'));
    await expect(d.comment('x')).rejects.toThrow('no');
    expect(d.busy.value).toBe(false);
  });

  it('attaches files under their own names and reads the doc back', async () => {
    const api = fakeApi();
    const d = usePanoDoc('d1', { api });
    await d.attach([fakeFile('a b.txt'), fakeFile('c.txt', { type: '' })]);
    expect(api.attachPanoFiles).toHaveBeenCalledWith('d1', [
      { filename: 'a b.txt', mimeType: 'text/plain', data: 'aGk=' },
      { filename: 'c.txt', mimeType: '', data: 'aGk=' },
    ]);
    expect(api.getPanoDoc).toHaveBeenCalledTimes(1);
    expect(d.busy.value).toBe(false);
    api.attachPanoFiles.mockRejectedValueOnce(new Error('no'));
    await expect(d.attach([fakeFile('x')])).rejects.toThrow('no');
    expect(d.busy.value).toBe(false);
  });

  it('refuses to attach anything when one file is over the limit', async () => {
    const api = fakeApi();
    const d = usePanoDoc('d1', { api });
    await expect(d.attach([fakeFile('ok'), fakeFile('big', { size: PANO_MAX_FILE_BYTES + 1 })])).rejects.toThrow('big is over the 2.0 MB limit');
    expect(api.attachPanoFiles).not.toHaveBeenCalled();
    expect(d.busy.value).toBe(false);
  });
});

describe('panoTilt', () => {
  it('leans a note by an angle its id picks, the same every time', () => {
    const tilts = new Set()
    for (const id of ['0kGVgW2hMzR', '0kGYJNfKDfF', '0kGa2kadsxN', 'a', 'b', 'c', 'd', 'e', 'f', 'g']) {
      const t = panoTilt(id)
      expect(panoTilt(id)).toBe(t)
      expect(Math.abs(t)).toBeGreaterThan(0.5)
      expect(Math.abs(t)).toBeLessThanOrEqual(2)
      tilts.add(t)
    }
    expect(tilts.size).toBeGreaterThan(2)
  })

  it('leans a note with no id too', () => {
    expect(typeof panoTilt(undefined)).toBe('number')
  })
})

describe('assigning a checklist item', () => {
  const PATH = '/workspaces/w1/tasks/t1';

  it('reads an item\'s text without its assignee, skipping fenced code', () => {
    const md = '```\n- [ ] code\n```\n- [ ] first → [@a](/workspaces/w/tasks/t "ongoing")\n* [x] second';
    expect(panoItemText(md, 0)).toBe('first');
    expect(panoItemText(md, 1)).toBe('second');
    expect(panoItemText(md, 2)).toBe('');
    expect(panoItemText(undefined, 0)).toBe('');
  });

  it('ends the item with its assignee, replacing an earlier one, and leaves others', () => {
    expect(assignPanoItem('- [ ] a\n- [ ] b', 1, 'api', PATH)).toBe(`- [ ] a\n- [ ] b → [@api](${PATH} "notstarted")`);
    expect(assignPanoItem('- [x] a → [@old](/workspaces/w0/tasks/t0 "completed")', 0, 'new [x]', PATH)).toBe(`- [x] a → [@new x](${PATH} "notstarted")`);
    expect(assignPanoItem('- [ ] a', 3, 'api', PATH)).toBe('- [ ] a');
    expect(assignPanoItem(null, 0, 'api', PATH)).toBe('');
  });

  it('makes an assignee a chip with its task\'s status, linked or not', () => {
    const html = `<li>x → <span class="md-dead-link" title="${PATH}" data-link-title="blocked">@api</span></li>`;
    expect(panoAssignees(html)).toBe(`<li>x <a class="pano-assignee" href="${PATH}" data-pano-task="${PATH}" data-status="blocked" title="Blocked · task t1"><span class="pano-status" aria-hidden="true"></span>@api<span class="pano-status-label">Blocked</span></a></li>`);
    expect(panoAssignees(html, { link: false })).toBe('<li>x <span class="pano-assignee" data-status="blocked" title="Blocked · task t1"><span class="pano-status" aria-hidden="true"></span>@api<span class="pano-status-label">Blocked</span></span></li>');
  });

  it('reads a missing or unknown status as not started, and leaves other dead links', () => {
    expect(panoAssignees(`<span class="md-dead-link" title="${PATH}">@a</span>`)).toContain('data-status="notstarted"');
    expect(panoAssignees(`<span class="md-dead-link" title="${PATH}" data-link-title="nope">@a</span>`)).toContain('title="Not started · task t1"');
    const other = '→ <span class="md-dead-link" title="notes.md">notes</span>';
    expect(panoAssignees(other)).toBe(other);
    expect(panoAssignees(undefined)).toBe('');
    expect(PANO_TASK_STATUSES.done).toBe('Completed');
  });

  it('puts a menu button after each checkbox', () => {
    const html = panoItemMenus('<li><input type="checkbox"> a</li><li><input type="text"></li>');
    expect(html.match(/data-pano-assign/g)).toHaveLength(1);
    expect(html).toMatch(/^<li><input type="checkbox"><button type="button" class="pano-assign"[^>]*aria-label="Assign to a workspace"[^>]*><svg/);
    expect(panoItemMenus(null)).toBe('');
  });

  it('tells the agent which doc the item is from, and that completing the task ticks it', () => {
    const body = panoItemTaskBody({ id: 'd1', title: 'Plan' }, 'ship');
    expect(body).toMatch(/^ship\n\n---\n/);
    expect(body).toContain('`d1` ("Plan")');
    expect(body).toBe('ship\n\n---\nA checklist item of the Pano doc `d1` ("Plan"). Read the doc with the pano tool for its context, files and comments.');
  });
});

describe('mentioning a workspace in a comment', () => {
  const WS = [{ id: 'w1', name: 'api' }, { id: 'w2', name: 'api-v2' }, { id: 'w3', name: 'web app' }, { id: 'w4', name: 'rapid' }];

  it('finds the mention being typed at the caret', () => {
    expect(mentionQuery('hi @ap', 6)).toEqual({ start: 3, query: 'ap' });
    expect(mentionQuery('@', 1)).toEqual({ start: 0, query: '' });
    expect(mentionQuery('mail a@b', 8)).toBeNull();
    expect(mentionQuery('hi @api done', 12)).toBeNull();
    expect(mentionQuery(undefined, 0)).toBeNull();
  });

  it('suggests the workspaces starting with it first, then containing it', () => {
    expect(mentionMatches(WS, 'AP').map((w) => w.id)).toEqual(['w1', 'w2', 'w3', 'w4']);
    expect(mentionMatches(WS, 'app').map((w) => w.id)).toEqual(['w3']);
    expect(mentionMatches(WS, '', 2).map((w) => w.id)).toEqual(['w1', 'w2']);
    expect(mentionMatches(undefined, undefined)).toEqual([]);
  });

  it('puts the picked name in place of what was typed', () => {
    expect(insertMention('hi @ap there', 3, 6, 'api')).toEqual({ value: 'hi @api  there', caret: 8 });
    expect(insertMention(undefined, 0, 0, 'web app')).toEqual({ value: '@web app ', caret: 9 });
  });

  it('pings only the workspaces named whole', () => {
    expect(mentionedWorkspaces('@api-v2 and @web app, cc@rapid', WS).map((w) => w.id)).toEqual(['w2', 'w3']);
    expect(mentionedWorkspaces('@api.', WS).map((w) => w.id)).toEqual(['w1']);
    expect(mentionedWorkspaces(undefined, undefined)).toEqual([]);
    expect(mentionedWorkspaces('@x', [{ id: 'w', name: '' }])).toEqual([]);
  });

  it('highlights the mentions in a rendered comment, never inside a tag', () => {
    const html = '<p>@api-v2 and <a title="@api">@api</a> &amp; @web app</p>';
    expect(panoMentions(html, WS)).toBe('<p><span class="pano-mention">@api-v2</span> and <a title="@api"><span class="pano-mention">@api</span></a> &amp; <span class="pano-mention">@web app</span></p>');
    expect(panoMentions('<p>@a&lt;b</p>', [{ id: 'x', name: 'a<b' }])).toBe('<p><span class="pano-mention">@a&lt;b</span></p>');
    expect(panoMentions('<p>@api</p>', [])).toBe('<p>@api</p>');
    expect(panoMentions(undefined, undefined)).toBe('');
    expect(panoMentions(undefined, WS)).toBe('');
  });

  it('gives each mentioned workspace a task that quotes the comment, a second apart', async () => {
    const api = { createTask: vi.fn((ws) => Promise.resolve({ task: { id: `t-${ws}` } })) };
    const wait = vi.fn(() => Promise.resolve());
    const doc = { id: 'd1', title: 'Plan' };
    expect(await pingPanoWorkspaces(doc, 'one\ntwo', WS.slice(0, 3), { api, wait })).toEqual({ tasks: [{ id: 't-w1' }, { id: 't-w2' }, { id: 't-w3' }], failed: [] });
    expect(wait.mock.calls).toEqual([[1100], [1100]]);
    expect(api.createTask).toHaveBeenCalledWith('w1', 'Pano: Plan', panoMentionTaskBody(doc, 'one\ntwo'));
    expect(panoMentionTaskBody(doc, 'one\ntwo')).toMatch(/^> one\n> two\n\n---\nYou were mentioned in a comment on the Pano doc `d1` \("Plan"\)/);
  });

  it('keeps pinging past a workspace it could not, and says which and why', async () => {
    const api = { createTask: vi.fn()
      .mockRejectedValueOnce(new Error('rate limit exceeded'))
      .mockRejectedValueOnce(new Error(''))
      .mockResolvedValueOnce({ task: { id: 't3' } }) };
    const res = await pingPanoWorkspaces({ id: 'd1', title: 'Plan' }, 'hi', WS.slice(0, 3), { api, wait: () => Promise.resolve() });
    expect(res).toEqual({ tasks: [{ id: 't3' }], failed: [{ workspace: WS[0], error: 'rate limit exceeded' }, { workspace: WS[1], error: 'failed' }] });
  });

  it('waits by default, on a real timer', async () => {
    vi.useFakeTimers();
    const api = { createTask: vi.fn(() => Promise.resolve({ task: { id: 't' } })) };
    const p = pingPanoWorkspaces({ id: 'd1', title: 'Plan' }, 'hi', WS.slice(0, 2), { api });
    await vi.advanceTimersByTimeAsync(1000);
    expect(api.createTask).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(100);
    expect((await p).tasks).toHaveLength(2);
    vi.useRealTimers();
  });
});
