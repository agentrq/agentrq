// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest';

import {
  currentMachineId,
  machineLabel,
  machineRoute,
  matchMachines,
  useMachineSwitcher,
} from '../src/composables/useMachineSwitcher';

const machine = (id, name, extra = {}) => ({ id, name, hostname: `${id}.local`, online: true, ...extra });

const MACHINES = [
  machine('m1', 'build'),
  machine('m2', 'build-arm', { online: false }),
  machine('m3', 'laptop', { online: false }),
  machine('m4', 'workshop-pi'),
  machine('m5', '', { hostname: 'unnamed-box' }),
];

describe('machineLabel', () => {
  it('prefers the name and falls back to the hostname', () => {
    expect(machineLabel(MACHINES[0])).toBe('build');
    expect(machineLabel(MACHINES[4])).toBe('unnamed-box');
  });

  it('is empty for nothing at all', () => {
    expect(machineLabel(null)).toBe('');
    expect(machineLabel({})).toBe('');
  });
});

describe('matchMachines', () => {
  it('lists every machine on an empty query, online ones first', () => {
    expect(matchMachines(MACHINES, '').map((m) => m.id)).toEqual(['m1', 'm4', 'm5', 'm2', 'm3']);
  });

  it('treats whitespace and a missing query as empty', () => {
    expect(matchMachines(MACHINES, '   ')).toHaveLength(5);
    expect(matchMachines(MACHINES, undefined)).toHaveLength(5);
  });

  it('ranks an exact name above a longer name that starts with it', () => {
    expect(matchMachines(MACHINES, 'BUILD').map((m) => m.id)).toEqual(['m1', 'm2']);
  });

  it('ranks a prefix above a substring', () => {
    const list = [machine('a', 'my-pi'), machine('b', 'pi-zero')];
    expect(matchMachines(list, 'pi').map((m) => m.id)).toEqual(['b', 'a']);
  });

  it('finds an unnamed machine by its hostname, and a renamed one by its old hostname', () => {
    expect(matchMachines(MACHINES, 'unnamed').map((m) => m.id)).toEqual(['m5']);
    expect(matchMachines(MACHINES, 'm3.local').map((m) => m.id)).toEqual(['m3']);
  });

  it('matches nothing when nothing contains the query', () => {
    expect(matchMachines(MACHINES, 'zzz')).toEqual([]);
  });

  it('caps the list', () => {
    expect(matchMachines(MACHINES, '', 2)).toHaveLength(2);
  });

  it('survives a list that is not a list', () => {
    expect(matchMachines(null, '')).toEqual([]);
    expect(matchMachines([{ id: 'x' }], 'x')).toEqual([]);
  });
});

describe('machineRoute', () => {
  it("is a machine's page, from a machine or an id", () => {
    expect(machineRoute({ id: 'm1' })).toBe('/machines/m1');
    expect(machineRoute('m2')).toBe('/machines/m2');
  });

  it('falls back to the machines page', () => {
    expect(machineRoute(null)).toBe('/machines');
    expect(machineRoute({})).toBe('/machines');
  });
});

describe('currentMachineId', () => {
  it("reads the id on a machine's page", () => {
    expect(currentMachineId({ path: '/machines/m1', params: { id: 'm1' } })).toBe('m1');
  });

  it("does not take a workspace's id for a machine's", () => {
    expect(currentMachineId({ path: '/workspaces/ws1', params: { id: 'ws1' } })).toBe('');
    expect(currentMachineId({ path: '/machines', params: {} })).toBe('');
    expect(currentMachineId(null)).toBe('');
  });

  it('is empty when the path names a machine but there is no id', () => {
    expect(currentMachineId({ path: '/machines/' })).toBe('');
  });
});

describe('useMachineSwitcher', () => {
  it('loads the machines', async () => {
    const { machines, loading, error, load } = useMachineSwitcher({
      fetchMachines: () => Promise.resolve({ machines: MACHINES }),
    });

    const done = load();
    expect(loading.value).toBe(true);
    await done;

    expect(machines.value).toEqual(MACHINES);
    expect(loading.value).toBe(false);
    expect(error.value).toBe('');
  });

  it('reads a response with no list as no machines', async () => {
    const { machines, load } = useMachineSwitcher({ fetchMachines: () => Promise.resolve(null) });
    await load();
    expect(machines.value).toEqual([]);
  });

  it('reports a failure, with a fallback message', async () => {
    const a = useMachineSwitcher({ fetchMachines: () => Promise.reject(new Error('offline')) });
    await a.load();
    expect(a.error.value).toBe('offline');
    expect(a.loading.value).toBe(false);

    const b = useMachineSwitcher({ fetchMachines: () => Promise.reject({}) });
    await b.load();
    expect(b.error.value).toBe('Failed to load machines');
  });

  it('keeps only the latest answer when a reopen overtakes a slow fetch', async () => {
    const pending = [];
    const fetchMachines = vi.fn(
      () => new Promise((resolve, reject) => pending.push({ resolve, reject }))
    );
    const { machines, loading, error, load } = useMachineSwitcher({ fetchMachines });

    const first = load();
    const second = load();
    const third = load();

    pending[2].resolve({ machines: [MACHINES[0]] });
    await third;
    pending[0].resolve({ machines: MACHINES });
    pending[1].reject(new Error('superseded request failed'));
    await Promise.all([first, second]);

    expect(machines.value).toEqual([MACHINES[0]]);
    expect(error.value).toBe('');
    expect(loading.value).toBe(false);
  });

  it('uses the real API by default', () => {
    expect(typeof useMachineSwitcher().load).toBe('function');
  });
});
