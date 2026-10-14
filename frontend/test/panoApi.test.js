// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, afterEach } from 'vitest';

import {
  addPanoComment,
  attachPanoFiles,
  createPanoDoc,
  createTask,
  deletePanoDoc,
  getPanoDoc,
  listPanoDocs,
  updatePanoDoc,
} from '../src/api';

/** Answers every call with `body` and `status`, and records what was asked. */
function serve(body, status = 200) {
  const seen = [];
  vi.stubGlobal('fetch', vi.fn((url, init = {}) => {
    seen.push([url, init.method || 'GET', init.body ? JSON.parse(init.body) : undefined]);
    return Promise.resolve(new Response(JSON.stringify(body), { status }));
  }));
  return seen;
}

describe('the Pano API', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('creates a task linked to a doc, and sends no doc id for one that is not', async () => {
    const seen = serve({ task: { id: 't1' } });
    await createTask('w1', 'Plan', 'body', 'agent', [], 'notstarted', '', false, '', '', false, 'd1');
    await createTask('w1', 'Plan', 'body');
    expect(seen[0][0]).toBe('/api/v1/workspaces/w1/tasks');
    expect(seen[0][2].panoDocId).toBe('d1');
    expect(seen[1][2]).not.toHaveProperty('panoDocId');
    expect(seen[1][2].task.title).toBe('Plan');
  });

  it('lists, reads and deletes docs at relative URLs, the id encoded', async () => {
    const seen = serve({ docs: [] });
    await expect(listPanoDocs()).resolves.toEqual({ docs: [] });
    await getPanoDoc('a/b');
    await expect(deletePanoDoc('d1')).resolves.toBe(true);
    expect(seen).toEqual([
      ['/api/v1/pano', 'GET', undefined],
      ['/api/v1/pano/a%2Fb', 'GET', undefined],
      ['/api/v1/pano/d1', 'DELETE', undefined],
    ]);
  });

  it('creates and saves a doc with every field, defaulting what was left out', async () => {
    const seen = serve({ doc: { id: 'd1' } });
    const file = { filename: 'a.png', mimeType: 'image/png', data: 'AA==' };
    await expect(createPanoDoc({ title: 'T', markdown: 'M', ttlDays: 7, color: 'pink', files: [file] })).resolves.toEqual({ doc: { id: 'd1' } });
    await updatePanoDoc('d1', { title: 'T', markdown: 'M' });
    await updatePanoDoc('d1', { title: 'T', markdown: 'M', version: 4 });
    expect(seen).toEqual([
      ['/api/v1/pano', 'POST', { version: 0, title: 'T', markdown: 'M', ttlDays: 7, color: 'pink', files: [file] }],
      ['/api/v1/pano/d1', 'PUT', { version: 0, title: 'T', markdown: 'M', ttlDays: 0, color: '', files: [] }],
      ['/api/v1/pano/d1', 'PUT', { version: 4, title: 'T', markdown: 'M', ttlDays: 0, color: '', files: [] }],
    ]);
  });

  it('comments and attaches files', async () => {
    const seen = serve({ doc: { id: 'd1' } });
    await addPanoComment('d1', 'hi');
    await attachPanoFiles('d1', [{ filename: 'x' }]);
    await attachPanoFiles('d1');
    expect(seen).toEqual([
      ['/api/v1/pano/d1/comments', 'POST', { body: 'hi' }],
      ['/api/v1/pano/d1/attachments', 'POST', { files: [{ filename: 'x' }] }],
      ['/api/v1/pano/d1/attachments', 'POST', { files: [] }],
    ]);
  });

  it('passes the server\'s refusal on, with its status', async () => {
    serve({ error: { message: 'file x.png is not linked from the markdown' } }, 422);
    await expect(createPanoDoc({ title: 'T', markdown: 'M' })).rejects.toMatchObject({ message: 'file x.png is not linked from the markdown', status: 422 });
  });

  it('passes a stale version on as a 409 with the server\'s sentence', async () => {
    serve({ error: { message: 'the Pano doc changed since you read it' } }, 409);
    await expect(updatePanoDoc('d1', { title: 'T', markdown: 'M', version: 1 })).rejects.toMatchObject({ message: 'the Pano doc changed since you read it', status: 409 });
  });

  it('says what failed when the server says nothing', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response('', { status: 500 }))));
    await expect(listPanoDocs()).rejects.toMatchObject({ message: 'Failed to fetch the Pano', status: 500 });
    await expect(getPanoDoc('d1')).rejects.toMatchObject({ message: 'Failed to fetch the Pano doc' });
    await expect(createPanoDoc({})).rejects.toMatchObject({ message: 'Failed to create the Pano doc' });
    await expect(updatePanoDoc('d1', {})).rejects.toMatchObject({ message: 'Failed to save the Pano doc' });
    await expect(deletePanoDoc('d1')).rejects.toMatchObject({ message: 'Failed to delete the Pano doc' });
    await expect(addPanoComment('d1', 'x')).rejects.toMatchObject({ message: 'Failed to comment on the Pano doc' });
    await expect(attachPanoFiles('d1', [])).rejects.toMatchObject({ message: 'Failed to attach files to the Pano doc' });
  });
});
