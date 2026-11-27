// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/** The GitLab connection calls on the wire, and how a refusal reads. */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import * as api from '../src/api.js';

describe('the GitLab API', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ host: 'gitlab.com' }) }));
  });
  afterEach(() => vi.unstubAllGlobals());

  it('reads the connection', async () => {
    expect(await api.getGitLabConnection()).toEqual({ host: 'gitlab.com' });
    expect(fetch.mock.calls[0][0]).toBe('/api/v1/gitlab');
  });

  it('finishes a connect with GitLab\'s code and state', async () => {
    await api.connectGitLab('c', 's');
    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe('/api/v1/gitlab/connection');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({ code: 'c', state: 's' });
  });

  it('disconnects', async () => {
    expect(await api.disconnectGitLab()).toBe(true);
    expect(fetch.mock.calls[0][0]).toBe('/api/v1/gitlab/connection');
    expect(fetch.mock.calls[0][1].method).toBe('DELETE');
  });

  it('says what failed', async () => {
    fetch.mockResolvedValue({ ok: false, status: 500, json: async () => ({}) });
    await expect(api.getGitLabConnection()).rejects.toThrow('Failed to fetch the GitLab connection');
    await expect(api.connectGitLab('c', 's')).rejects.toThrow('Failed to connect GitLab');
    await expect(api.disconnectGitLab()).rejects.toThrow('Failed to disconnect GitLab');
  });
});
