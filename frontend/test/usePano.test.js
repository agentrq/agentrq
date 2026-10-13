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
} from '../src/composables/usePano';

const DAY = 24 * 60 * 60 * 1000;

/** A browser File as far as the Pano reads one. */
function fakeFile(name, { type = 'text/plain', bytes = [104, 105], size } = {}) {
  const data = new Uint8Array(bytes);
  return { name, type, size: size ?? data.length, arrayBuffer: () => Promise.resolve(data.buffer) };
}

describe('the post-it colors', () => {
  it('names each color in the server\'s order, yellow first', () => {
    expect(PANO_COLORS.map((c) => c.name)).toEqual(['yellow', 'pink', 'green', 'blue', 'orange', 'purple']);
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

  it('gives every color a label, an ink, an ink background, its hex and a tape', () => {
    for (const c of PANO_COLORS) {
      expect(c.label).toMatch(/^[A-Z][a-z]+( [a-z]+)?$/);
      expect(c.ink).toMatch(/^text-\[#[0-9a-f]{6}\]$/);
      expect(c.inkBg).toBe(`bg-[${c.inkHex}]`);
      expect(c.inkHex).toMatch(/^#[0-9a-f]{6}$/);
      expect(c.tape).toMatch(/^bg-\[#[0-9a-f]{6}\]\/60$/);
    }
    expect(new Set(PANO_COLORS.map((c) => c.inkHex)).size).toBe(PANO_COLORS.length);
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
    await d.comment('looks good');
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
