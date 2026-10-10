// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { reactive, ref } from 'vue';

import { STEP_LABELS, canSpinUp, spinUpFailure, spinUpName, useSpinUp } from '../src/composables/useSpinUp';
import { lastLaunchChoice, lastLaunchParams, rememberLaunchChoice } from '../src/composables/useAgentLaunch';
import { MAX_WORKSPACE_NAME } from '../src/composables/useWorkspaceForks';

const parent = { id: 'p1', name: 'ops', workingDirectory: '/srv/ops' };
const task = { id: 't1', title: 'Fix the login page', status: 'notstarted' };
const M1 = { id: 'm1', name: 'pi', enabled: true, online: true };
const M2 = { id: 'm2', name: 'laptop', enabled: true, online: true };
const settle = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => localStorage.clear());

describe('canSpinUp', () => {
  it('is offered on work waiting for an agent in a workspace that can be forked', () => {
    for (const status of ['notstarted', 'blocked']) {
      expect(canSpinUp({ ...task, status }, parent)).toBe(true);
    }
  });

  it('is not offered on work an agent has started, finished work, a schedule, a fork or the supervisor', () => {
    expect(canSpinUp({ ...task, status: 'ongoing' }, parent)).toBe(false);
    expect(canSpinUp({ ...task, status: 'completed' }, parent)).toBe(false);
    expect(canSpinUp({ ...task, status: 'rejected' }, parent)).toBe(false);
    expect(canSpinUp({ ...task, status: 'cron' }, parent)).toBe(false);
    expect(canSpinUp(task, { ...parent, forkOfId: 'x' })).toBe(false);
    expect(canSpinUp(task, { id: 's', name: 'supervisor' })).toBe(false);
    expect(canSpinUp(null, parent)).toBe(false);
  });
});

describe('names and failures', () => {
  it('names the fork after the task, cut to fit', () => {
    expect(spinUpName(task)).toBe('fix-the-login-page');
    expect(spinUpName({ title: 'x'.repeat(300) })).toHaveLength(MAX_WORKSPACE_NAME);
    expect(spinUpName({ title: '   ' })).toBe('spin-up');
    expect(spinUpName({ title: '¿¡' })).toBe('spin-up');
    expect(spinUpName(null)).toBe('spin-up');
  });

  it('says which step failed, and that the fork is there when it is', () => {
    expect(spinUpFailure('fork', new Error('rate limit exceeded'), 'x')).toBe('Could not fork the workspace: rate limit exceeded');
    expect(spinUpFailure('move', new Error('task not found'), 'Fix it')).toBe(`Forked Fix it, but could not ${STEP_LABELS.move}: task not found`);
    expect(spinUpFailure('launch', {}, 'Fix it')).toBe('Forked Fix it, but could not start the agent in the fork: unknown error');
  });
});

function setup(over = {}) {
  const deps = {
    fetchMachines: vi.fn(() => Promise.resolve({ machines: [M1, M2] })),
    forkWorkspace: vi.fn(() => Promise.resolve({ workspace: { id: 'f1', name: 'fix-the-login-page' } })),
    moveTask: vi.fn(() => Promise.resolve({})),
    launchAgent: vi.fn(() => Promise.resolve({ session: { id: 's1' } })),
    recordTelemetry: vi.fn(),
    measureTerminalSize: vi.fn(() => Promise.resolve({ cols: 80, rows: 24 })),
    fetchAcpAgents: vi.fn(() => Promise.resolve({ agents: [] })),
    fetchAcpModels: vi.fn(() => Promise.resolve({ models: [] })),
    ...over,
  };
  return { spin: useSpinUp(deps), deps };
}

describe('useSpinUp: opening', () => {
  it('opens on the agent and model the parent last ran, and remembers them for the fork too', async () => {
    rememberLaunchChoice('p1', { machineId: 'm2', kind: 'acp-gateway', params: { agent: 'gemini', model: 'gemini-3-pro' } });
    const { spin } = setup();
    await spin.open(task, parent);
    expect(spin.params.value).toEqual({ agent: 'gemini', model: 'gemini-3-pro' });

    await spin.run();
    expect(lastLaunchParams('p1', 'acp-gateway')).toEqual({ agent: 'gemini', model: 'gemini-3-pro' });
    expect(lastLaunchParams('f1', 'acp-gateway')).toEqual({ agent: 'gemini', model: 'gemini-3-pro' });
  });

  it('lists the registry\'s agents it is handed', async () => {
    const registryAgents = ref([{ id: 'codex-acp', name: 'Codex' }]);
    const { spin } = setup({ registryAgents });
    await spin.open(task, parent);
    spin.kind.value = 'acp-gateway';
    expect(spin.acpAgents.value).toEqual([{ id: 'codex-acp', name: 'Codex' }]);
  });

  it('opens on the parent\'s last launch when that machine is online', async () => {
    rememberLaunchChoice('p1', { machineId: 'm2', kind: 'acp-gateway' });
    const { spin } = setup();
    await spin.open(task, parent, { x: 4, y: 9 });
    expect(spin.state).toMatchObject({ task, workspace: parent, x: 4, y: 9 });
    expect(spin.machineId.value).toBe('m2');
    expect(spin.kind.value).toBe('acp-gateway');
    expect(spin.available.value).toHaveLength(2);
  });

  it('asks when the last machine is gone and there are several', async () => {
    rememberLaunchChoice('p1', { machineId: 'gone', kind: 'claude-code' });
    const { spin } = setup();
    await spin.open(task, parent);
    expect(spin.machineId.value).toBe('');
    expect(spin.blockers.value[0].reason).toBe('Pick a machine to run on.');
    expect(spin.canRun.value).toBe(false);
  });

  it('picks the only machine', async () => {
    const { spin } = setup({ fetchMachines: vi.fn(() => Promise.resolve({})) });
    await spin.open(task, parent);
    expect(spin.machineId.value).toBe('');
    const one = setup({ fetchMachines: vi.fn(() => Promise.resolve({ machines: [M1] })) }).spin;
    await one.open(task, parent);
    expect(one.machineId.value).toBe('m1');
    expect(one.canRun.value).toBe(true);
  });

  it('says so when the machines cannot be loaded', async () => {
    const { spin } = setup({ fetchMachines: vi.fn(() => Promise.reject(new Error('network unreachable'))) });
    await spin.open(task, parent);
    expect(spin.error.value).toBe('network unreachable');
    const bare = setup({ fetchMachines: vi.fn(() => Promise.reject({})) }).spin;
    await bare.open(task, parent);
    expect(bare.error.value).toBe('Failed to load machines');
  });

  it('refuses before forking when the parent has no folder to make the fork\'s from', async () => {
    const { spin } = setup({ fetchMachines: vi.fn(() => Promise.resolve({ machines: [M1] })) });
    await spin.open(task, { ...parent, workingDirectory: '' });
    expect(spin.blockers.value.map((b) => b.reason)).toEqual([
      "ops has no working directory, so there is no folder to make the fork's from.",
    ]);
    expect(spin.blockers.value[0].fix.to).toBe('/workspaces/p1/settings');
    expect(await spin.run()).toBe(null);
  });

  it('refuses before forking when an agent takes the task on while it is open', async () => {
    const { spin } = setup({ fetchMachines: vi.fn(() => Promise.resolve({ machines: [M1] })) });
    const live = reactive({ ...task });
    await spin.open(live, parent);
    expect(spin.blockers.value).toEqual([]);
    live.status = 'ongoing';
    expect(spin.blockers.value.map((b) => b.reason)).toEqual([
      'An agent has already started on this task, so it stays here.',
    ]);
    expect(await spin.run()).toBe(null);
  });

  it('closes', async () => {
    const { spin } = setup();
    await spin.open(task, parent);
    spin.close();
    expect(spin.state.task).toBe(null);
    expect(spin.canRun.value).toBe(false);
  });
});

describe('useSpinUp: running', () => {
  it('forks, moves the task in, launches there, and remembers the choice', async () => {
    const { spin, deps } = setup({ fetchMachines: vi.fn(() => Promise.resolve({ machines: [M1] })) });
    await spin.open(task, parent);
    const result = await spin.run();

    expect(deps.forkWorkspace).toHaveBeenCalledWith('p1', { name: 'fix-the-login-page' });
    expect(deps.moveTask).toHaveBeenCalledWith('p1', 't1', 'f1');
    expect(deps.launchAgent).toHaveBeenCalledWith('f1', { machineId: 'm1', kind: 'claude-code', cols: 80, rows: 24 });
    expect(deps.recordTelemetry).toHaveBeenCalledWith('ui_spin_up', 'p1');
    expect(result).toEqual({ task, fork: { id: 'f1', name: 'fix-the-login-page' }, session: { id: 's1' } });
    expect(lastLaunchChoice('p1')).toEqual({ machineId: 'm1', kind: 'claude-code' });
    expect(lastLaunchChoice('f1')).toEqual({ machineId: 'm1', kind: 'claude-code' });
    expect(spin.state.task).toBe(null);
    expect(spin.running.value).toBe(false);
  });

  it('launches Claude Code with the model chosen for it, and remembers that too', async () => {
    const { spin, deps } = setup({ fetchMachines: vi.fn(() => Promise.resolve({ machines: [M1] })) });
    await spin.open(task, parent);
    spin.params.value = { model: 'opus' };
    await spin.run();
    expect(deps.launchAgent).toHaveBeenCalledWith('f1', { machineId: 'm1', kind: 'claude-code', cols: 80, rows: 24, model: 'opus' });
    expect(JSON.parse(localStorage.getItem('agentrq:lastClaudeCode'))).toEqual({ model: 'opus', effort: '' });
  });

  it('launches the gateway with its agent, and remembers that too', async () => {
    const { spin, deps } = setup({
      fetchMachines: vi.fn(() => Promise.resolve({ machines: [M1] })),
      launchAgent: vi.fn(() => Promise.resolve({})),
    });
    await spin.open(task, parent);
    spin.kind.value = 'acp-gateway';
    spin.params.value = { agent: 'gemini', model: '' };
    await settle();
    const result = await spin.run();
    expect(deps.launchAgent).toHaveBeenCalledWith('f1', { machineId: 'm1', kind: 'acp-gateway', cols: 80, rows: 24, agent: 'gemini' });
    expect(result.session).toBe(null);
    expect(JSON.parse(localStorage.getItem('agentrq:lastAcpGateway'))).toEqual({ agent: 'gemini', model: '' });
  });

  it('stops at a failed move, keeps the fork, says which step, and will not fork again', async () => {
    const { spin, deps } = setup({
      fetchMachines: vi.fn(() => Promise.resolve({ machines: [M1] })),
      moveTask: vi.fn(() => Promise.reject(new Error('task not found'))),
    });
    await spin.open(task, parent);
    const result = await spin.run();
    expect(result).toEqual({ task, fork: { id: 'f1', name: 'fix-the-login-page' }, failedStep: 'move' });
    expect(spin.error.value).toBe('Forked fix-the-login-page, but could not move the task into the fork: task not found');
    expect(spin.forked.value).toEqual({ id: 'f1', name: 'fix-the-login-page' });
    expect(spin.canRun.value).toBe(false);
    expect(deps.launchAgent).not.toHaveBeenCalled();
    expect(deps.recordTelemetry).not.toHaveBeenCalled();
  });

  it('stops at a failed launch with the server\'s reason', async () => {
    const { spin } = setup({
      fetchMachines: vi.fn(() => Promise.resolve({ machines: [M1] })),
      launchAgent: vi.fn(() => Promise.reject(new Error('update agentrqd on this machine to run a fork'))),
    });
    await spin.open(task, parent);
    const result = await spin.run();
    expect(result.failedStep).toBe('launch');
    expect(spin.error.value).toMatch(/but could not start the agent in the fork: update agentrqd/);
  });

  it('reports a failed fork with no fork', async () => {
    const { spin } = setup({
      fetchMachines: vi.fn(() => Promise.resolve({ machines: [M1] })),
      forkWorkspace: vi.fn(() => Promise.reject(new Error('the supervisor workspace cannot be forked'))),
    });
    await spin.open(task, parent);
    const result = await spin.run();
    expect(result).toEqual({ task, fork: null, failedStep: 'fork' });
    expect(spin.error.value).toBe('Could not fork the workspace: the supervisor workspace cannot be forked');
    // With no fork made, trying again is harmless.
    expect(spin.canRun.value).toBe(true);
    await spin.open(task, parent);
    expect(spin.error.value).toBe('');
  });

  it('treats an answer with no fork in it as a failed fork', async () => {
    const { spin } = setup({
      fetchMachines: vi.fn(() => Promise.resolve({ machines: [M1] })),
      forkWorkspace: vi.fn(() => Promise.resolve({})),
    });
    await spin.open(task, parent);
    const result = await spin.run();
    expect(result.failedStep).toBe('move');
    expect(result.fork).toBe(null);
  });
});

describe('useSpinUp: defaults', () => {
  it('builds on the real API when nothing is replaced, and asks the gateway about the parent\'s folder', async () => {
    const spin = useSpinUp();
    expect(spin.kind.value).toBe('claude-code');
    expect(spin.canRun.value).toBe(false);

    const { spin: gw, deps } = setup({ fetchMachines: vi.fn(() => Promise.resolve({ machines: [M1] })) });
    await gw.open(task, parent);
    gw.kind.value = 'acp-gateway';
    gw.params.value = { agent: 'gemini', model: '' };
    await settle();
    expect(deps.fetchAcpModels).toHaveBeenCalledWith('p1', 'm1', 'gemini');
  });
});
