// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest';
import { nextTick, ref } from 'vue';
import {
  applyPreset,
  mergePresetCommands,
  presetCommands,
  presetQuery,
  usePresetCommands,
  usePresetMenu,
  useWorkspacePresets,
} from '../src/composables/usePresets';
import { TELEMETRY_UI_PRESET_INSERT } from '../src/api';

const settle = () => new Promise((r) => setTimeout(r, 0));

const PULL = { name: 'pull-n-push', body: 'pull latest main, tag and push' };
const DEPLOY = { name: 'deploy', body: '\n  deploy to staging\nthen check' };
const STANDUP = { name: 'standup', body: 'post the standup' };
// The account's presets, for tests about the workspace's: none.
const noAccountPresets = () => Promise.resolve({ presets: [] });

describe('presetQuery', () => {
  it.each([
    ['#', ''],
    ['#pull', 'pull'],
    ['fix it then #pu', 'pu'],
    ['line one\n#Pull-N', 'Pull-N'],
  ])('reads %j as asking for %j', (text, want) => {
    expect(presetQuery(text)).toBe(want);
  });

  it.each([
    ['', 'empty text'],
    ['pull', 'no #'],
    ['issue#12', 'a # inside a word'],
    ['## heading', 'a markdown heading'],
    ['#pull and more', 'a # the caret has left'],
    ['#pull ', 'a finished name'],
    [null, 'no text at all'],
  ])('reads %j as not asking (%s)', (text) => {
    expect(presetQuery(text)).toBeNull();
  });
});

describe('applyPreset', () => {
  it('puts the body in place of the #name being typed, keeping what came before', () => {
    expect(applyPreset(PULL, 'then #pu')).toBe('then pull latest main, tag and push');
    expect(applyPreset(PULL, '#')).toBe('pull latest main, tag and push');
    expect(applyPreset(PULL, 'a\n#pull')).toBe('a\npull latest main, tag and push');
  });

  it('adds the body at the end when no #name is being typed', () => {
    expect(applyPreset(PULL, 'done ')).toBe('done pull latest main, tag and push');
    expect(applyPreset(PULL, undefined)).toBe('pull latest main, tag and push');
  });
});

describe('presetCommands', () => {
  it('makes menu entries showing the first line of each body', () => {
    expect(presetCommands([PULL, DEPLOY])).toEqual([
      { name: 'pull-n-push', body: PULL.body, description: 'pull latest main, tag and push', scope: 'workspace' },
      { name: 'deploy', body: DEPLOY.body, description: 'deploy to staging', scope: 'workspace' },
    ]);
  });

  it('leaves out what is not a preset, and a blank body shows no description', () => {
    expect(presetCommands([null, { name: '' , body: 'x' }, { name: 'x' }, { name: 'blank', body: '  ' }])).toEqual([
      { name: 'blank', body: '  ', description: '', scope: 'workspace' },
    ]);
    expect(presetCommands(undefined)).toEqual([]);
  });

  it('shows the hint it is given', () => {
    expect(presetCommands([STANDUP], 'account')[0].scope).toBe('account');
  });
});

describe('mergePresetCommands', () => {
  it('puts the workspace\'s first, tags the account\'s, and the workspace\'s wins a name both have', () => {
    const accountDeploy = { name: 'deploy', body: 'the account\'s deploy' };
    expect(mergePresetCommands([PULL, DEPLOY], [accountDeploy, STANDUP]).map((c) => [c.name, c.scope, c.body])).toEqual([
      ['pull-n-push', 'workspace', PULL.body],
      ['deploy', 'workspace', DEPLOY.body],
      ['standup', 'account', STANDUP.body],
    ]);
  });

  it('reads a missing list as none', () => {
    expect(mergePresetCommands(undefined, [STANDUP]).map((c) => c.name)).toEqual(['standup']);
    expect(mergePresetCommands([PULL], undefined).map((c) => c.name)).toEqual(['pull-n-push']);
  });
});

describe('usePresetCommands', () => {
  it('loads the workspace\'s presets, and again when it changes', async () => {
    const id = ref('ws1');
    const load = vi.fn((ws) => Promise.resolve({ presets: ws === 'ws1' ? [PULL] : [DEPLOY] }));
    const commands = usePresetCommands(id, { load, loadAccount: noAccountPresets });
    await settle();
    expect(commands.value.map((c) => c.name)).toEqual(['pull-n-push']);
    id.value = 'ws2';
    await nextTick();
    await settle();
    expect(commands.value.map((c) => c.name)).toEqual(['deploy']);
  });

  it('lets only the newest load write, so a quick switch keeps no stale presets', async () => {
    const id = ref('ws1');
    const pending = {};
    const load = (ws) => new Promise((resolve) => { pending[ws] = resolve; });
    const commands = usePresetCommands(id, { load, loadAccount: noAccountPresets });
    id.value = 'ws2';
    await nextTick();
    pending.ws2({ presets: [DEPLOY] });
    await settle();
    pending.ws1({ presets: [PULL] });
    await settle();
    expect(commands.value.map((c) => c.name)).toEqual(['deploy']);
  });

  it('offers nothing with no workspace, and leaves out a list that fails to load', async () => {
    const load = vi.fn();
    const loadAccount = vi.fn();
    expect(usePresetCommands(ref(''), { load, loadAccount }).value).toEqual([]);
    expect(load).not.toHaveBeenCalled();
    expect(loadAccount).not.toHaveBeenCalled();
    const failed = usePresetCommands(ref('ws1'), {
      load: () => Promise.reject(new Error('network unreachable')),
      loadAccount: () => Promise.resolve({ presets: [STANDUP] }),
    });
    await settle();
    expect(failed.value.map((c) => c.name)).toEqual(['standup']);
    const threw = usePresetCommands(ref('ws1'), {
      load: () => { throw new Error('no such export'); },
      loadAccount: () => Promise.resolve({ presets: [STANDUP] }),
    });
    await settle();
    expect(threw.value.map((c) => c.name)).toEqual(['standup']);
    const accountFailed = usePresetCommands(ref('ws1'), {
      load: () => Promise.resolve({ presets: [PULL] }),
      loadAccount: () => Promise.reject(new Error('network unreachable')),
    });
    await settle();
    expect(accountFailed.value.map((c) => c.name)).toEqual(['pull-n-push']);
  });

  it('loads through the API by default', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation((url) => Promise.resolve(
      new Response(JSON.stringify({ presets: String(url).includes('/account/') ? [STANDUP] : [PULL] }), { status: 200 })
    ));
    const commands = usePresetCommands(ref('ws1'));
    await settle();
    expect(fetchSpy.mock.calls.map(([url]) => url)).toEqual([
      expect.stringMatching(/\/workspaces\/ws1\/presets$/),
      expect.stringMatching(/\/account\/presets$/),
    ]);
    expect(commands.value.map((c) => [c.name, c.scope])).toEqual([['pull-n-push', 'workspace'], ['standup', 'account']]);
    fetchSpy.mockRestore();
  });
});

describe('usePresetMenu', () => {
  it('opens on #, and choosing puts the body in and counts it against the workspace', async () => {
    const text = ref('then #pu');
    const record = vi.fn();
    const menu = usePresetMenu({ text, workspaceId: ref('ws1'), load: () => Promise.resolve({ presets: [DEPLOY, PULL] }), loadAccount: noAccountPresets, record });
    await settle();
    expect(menu.open.value).toBe(true);
    expect(menu.matches.value.map((c) => c.name)).toEqual(['pull-n-push']);
    const claimed = menu.handleKeydown({ key: 'Enter' });
    expect(claimed).toEqual({ text: 'then pull latest main, tag and push' });
    expect(record).toHaveBeenCalledWith(TELEMETRY_UI_PRESET_INSERT, 'ws1');
    text.value = claimed.text;
    expect(menu.open.value).toBe(false);
  });

  it('records through the API by default', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}', { status: 200 }));
    const menu = usePresetMenu({ text: ref('#'), workspaceId: ref('ws1'), load: () => Promise.resolve({ presets: [PULL] }), loadAccount: noAccountPresets });
    await settle();
    menu.accept();
    const telemetry = fetchSpy.mock.calls.find(([url]) => String(url).endsWith('/telemetry'));
    expect(JSON.parse(telemetry[1].body)).toEqual({ action: 'ui_preset_insert', workspaceId: 'ws1' });
    fetchSpy.mockRestore();
  });
});

describe('useWorkspacePresets', () => {
  it('loads, adds in name order and removes', async () => {
    const list = vi.fn().mockResolvedValue({ presets: [PULL] });
    const create = vi.fn((id, name, body) => Promise.resolve({ preset: { name, body } }));
    const remove = vi.fn().mockResolvedValue(true);
    const p = useWorkspacePresets(() => 'ws1', { list, create, remove });
    const loading = p.load();
    expect(p.loading.value).toBe(true);
    await loading;
    expect(p.loading.value).toBe(false);
    expect(p.presets.value).toEqual([PULL]);

    expect(await p.add(' deploy ', 'ship it')).toBe(true);
    expect(create).toHaveBeenCalledWith('ws1', 'deploy', 'ship it');
    expect(p.presets.value.map((x) => x.name)).toEqual(['deploy', 'pull-n-push']);
    expect(await p.add('zz', 'last')).toBe(true);
    expect(p.presets.value.map((x) => x.name)).toEqual(['deploy', 'pull-n-push', 'zz']);

    await p.remove('pull-n-push');
    expect(remove).toHaveBeenCalledWith('ws1', 'pull-n-push');
    expect(p.presets.value.map((x) => x.name)).toEqual(['deploy', 'zz']);
    expect(p.error.value).toBe('');
  });

  it('edits a preset in place, keeping name order through a rename', async () => {
    const list = vi.fn().mockResolvedValue({ presets: [DEPLOY, PULL] });
    const update = vi.fn((id, name, newName, body) => Promise.resolve({ preset: { name: newName, body } }));
    const p = useWorkspacePresets(() => 'ws1', { list, update });
    await p.load();

    expect(await p.edit('deploy', 'deploy', 'deploy to prod')).toBe(true);
    expect(update).toHaveBeenCalledWith('ws1', 'deploy', 'deploy', 'deploy to prod');
    expect(p.presets.value).toEqual([{ name: 'deploy', body: 'deploy to prod' }, PULL]);

    expect(await p.edit('deploy', ' zz ', 'last')).toBe(true);
    expect(update).toHaveBeenLastCalledWith('ws1', 'deploy', 'zz', 'last');
    expect(p.presets.value.map((x) => x.name)).toEqual(['pull-n-push', 'zz']);
    expect(p.error.value).toBe('');
  });

  it('shows the server\'s sentence when it refuses, and keeps the list', async () => {
    const p = useWorkspacePresets(() => 'ws1', {
      list: () => Promise.reject(new Error('Failed to fetch presets')),
      create: () => Promise.reject(new Error('a preset named "deploy" exists')),
      update: () => Promise.reject(new Error('a preset named "ship" exists')),
      remove: () => Promise.reject(new Error('Failed to remove preset')),
    });
    await p.load();
    expect(p.error.value).toBe('Failed to fetch presets');
    expect(await p.add('deploy', 'x')).toBe(false);
    expect(p.error.value).toBe('a preset named "deploy" exists');
    expect(p.saving.value).toBe(false);
    p.presets.value = [PULL];
    expect(await p.edit('pull-n-push', 'ship', 'x')).toBe(false);
    expect(p.error.value).toBe('a preset named "ship" exists');
    expect(p.presets.value).toEqual([PULL]);
    await p.remove('deploy');
    expect(p.error.value).toBe('Failed to remove preset');
  });

  it('falls back to a sentence of its own for an error without one', async () => {
    const p = useWorkspacePresets(() => 'ws1', {
      list: () => Promise.reject(new Error('')),
      create: () => Promise.reject(null),
      remove: () => Promise.reject(undefined),
    });
    await p.load();
    expect(p.error.value).toBe('Failed to fetch presets');
    await p.add('a', 'b');
    expect(p.error.value).toBe('Failed to save preset');
    await p.remove('a');
    expect(p.error.value).toBe('Failed to remove preset');
  });

  it('reads an empty answer as no presets', async () => {
    const list = vi.fn().mockResolvedValue(null);
    const empty = useWorkspacePresets(() => 'ws1', { list });
    await empty.load();
    expect(empty.presets.value).toEqual([]);
  });

  it('calls the API by default', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation((url, init = {}) => {
      if (init.method === 'POST') return Promise.resolve(new Response(JSON.stringify({ preset: PULL }), { status: 201 }));
      if (init.method === 'PUT') return Promise.resolve(new Response(JSON.stringify({ preset: { ...PULL, body: 'new' } }), { status: 200 }));
      if (init.method === 'DELETE') return Promise.resolve(new Response(null, { status: 204 }));
      return Promise.resolve(new Response(JSON.stringify({ presets: [] }), { status: 200 }));
    });
    const p = useWorkspacePresets(() => 'ws1');
    await p.load();
    await p.add('pull-n-push', PULL.body);
    await p.edit('pull-n-push', 'pull-n-push', 'new');
    await p.remove('pull-n-push');
    expect(fetchSpy.mock.calls.map(([url, init]) => `${init?.method || 'GET'} ${url}`)).toEqual([
      expect.stringMatching(/^GET .*\/workspaces\/ws1\/presets$/),
      expect.stringMatching(/^POST .*\/workspaces\/ws1\/presets$/),
      expect.stringMatching(/^PUT .*\/workspaces\/ws1\/presets\/pull-n-push$/),
      expect.stringMatching(/^DELETE .*\/workspaces\/ws1\/presets\/pull-n-push$/),
    ]);
    fetchSpy.mockRestore();
  });

  it('works on the account\'s own presets with no workspace', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation((url, init = {}) => {
      if (init.method === 'POST') return Promise.resolve(new Response(JSON.stringify({ preset: STANDUP }), { status: 201 }));
      if (init.method === 'PUT') return Promise.resolve(new Response(JSON.stringify({ preset: { ...STANDUP, body: 'new' } }), { status: 200 }));
      if (init.method === 'DELETE') return Promise.resolve(new Response(null, { status: 204 }));
      return Promise.resolve(new Response(JSON.stringify({ presets: [] }), { status: 200 }));
    });
    const p = useWorkspacePresets(() => '');
    await p.load();
    await p.add('standup', STANDUP.body);
    await p.edit('standup', 'standup', 'new');
    await p.remove('standup');
    expect(fetchSpy.mock.calls.map(([url, init]) => `${init?.method || 'GET'} ${url}`)).toEqual([
      expect.stringMatching(/^GET .*\/account\/presets$/),
      expect.stringMatching(/^POST .*\/account\/presets$/),
      expect.stringMatching(/^PUT .*\/account\/presets\/standup$/),
      expect.stringMatching(/^DELETE .*\/account\/presets\/standup$/),
    ]);
    expect(JSON.parse(fetchSpy.mock.calls[1][1].body)).toEqual({ name: 'standup', body: STANDUP.body });
    fetchSpy.mockRestore();
  });
});

describe('the preset API calls', () => {
  const answer = (status, body) => vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(body, { status }));

  it('report a failed list or removal', async () => {
    const api = await import('../src/api');
    const spy = answer(500, '{}');
    await expect(api.listWorkspacePresets('ws1')).rejects.toThrow('Failed to fetch presets');
    await expect(api.deleteWorkspacePreset('ws1', 'a b')).rejects.toThrow('Failed to remove preset');
    expect(spy.mock.calls[1][0]).toContain('/presets/a%20b');
    spy.mockRestore();
  });

  it('refuse a save with the server\'s sentence, or a sentence of their own', async () => {
    const api = await import('../src/api');
    let spy = answer(409, JSON.stringify({ error: { code: 409, message: 'a preset named "a" exists' } }));
    await expect(api.createWorkspacePreset('ws1', 'a', 'b')).rejects.toThrow('a preset named "a" exists');
    spy.mockRestore();
    spy = answer(500, 'not json');
    await expect(api.createWorkspacePreset('ws1', 'a', 'b')).rejects.toThrow('Failed to save preset');
    spy.mockRestore();
    spy = answer(409, JSON.stringify({ error: { code: 409, message: 'a preset named "c" exists' } }));
    await expect(api.updateWorkspacePreset('ws1', 'a b', 'c', 'd')).rejects.toThrow('a preset named "c" exists');
    expect(spy.mock.calls[0][0]).toContain('/presets/a%20b');
    expect(JSON.parse(spy.mock.calls[0][1].body)).toEqual({ name: 'c', body: 'd' });
    spy.mockRestore();
    spy = answer(500, 'not json');
    await expect(api.updateWorkspacePreset('ws1', 'a', 'a', 'b')).rejects.toThrow('Failed to save preset');
    spy.mockRestore();
  });
});
