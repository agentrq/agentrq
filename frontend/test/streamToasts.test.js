// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, it, expect, vi } from 'vitest';

import { createPresenceToasts, isOpenPermissionRequest, lastMessage, snippet, toastFor } from '../src/composables/useStreamToasts';

/**
 * What the live stream is worth interrupting somebody for.
 *
 * The rule that matters: `reply.received` is published for every new message on
 * a task, whoever wrote it, so the event type cannot tell "the agent answered"
 * from "I just sent that". The sender on the last message can — and the bug
 * this file exists for was a branch that had a case for a permission request
 * and a case for a status line, and none at all for an agent simply replying.
 */

const reply = (messages, over = {}) => ({
  type: 'reply.received',
  payload: { id: 't1', workspaceId: 'w1', title: 'Ship it', status: 'ongoing', messages, ...over },
});

const from = (sender, over = {}) => ({ sender, text: 'on it', ...over });

describe('lastMessage', () => {
  it('reads the newest one', () => {
    expect(lastMessage({ messages: [from('human'), from('agent')] })).toMatchObject({ sender: 'agent' });
  });

  it('has nothing to read when there is nothing there', () => {
    expect(lastMessage({ messages: [] })).toBeNull();
    expect(lastMessage({})).toBeNull();
    expect(lastMessage(undefined)).toBeNull();
    expect(lastMessage({ messages: 'lots' })).toBeNull();
  });

  // A row that is present but empty still answers "nothing", so the contract —
  // a message or null — holds rather than handing back undefined.
  it('answers null for a message that is not there', () => {
    expect(lastMessage({ messages: [from('agent'), null] })).toBeNull();
  });
});

describe('isOpenPermissionRequest', () => {
  it('is a request nobody has answered yet', () => {
    const asking = from('agent', { metadata: { type: 'permission_request', tool_name: 'bash' } });
    expect(isOpenPermissionRequest(asking)).toBe(true);
  });

  it('is not one that has been answered either way', () => {
    for (const status of ['allow', 'deny']) {
      const answered = from('agent', { metadata: { type: 'permission_request', status, tool_name: 'bash' } });
      expect(isOpenPermissionRequest(answered)).toBe(false);
    }
  });

  it('is not an ordinary message', () => {
    expect(isOpenPermissionRequest(from('agent'))).toBe(false);
    expect(isOpenPermissionRequest(undefined)).toBe(false);
  });
});

describe('toastFor', () => {
  // The case that was missing, and the commonest thing that happens.
  it('says when the agent replied, and what it said', () => {
    expect(toastFor(reply([from('human'), from('agent', { text: 'Tests pass.\n\nOpening the PR now.' })]))).toEqual({
      tone: 'info',
      kind: 'reply',
      title: 'Ship it',
      message: 'Tests pass. Opening the PR now.',
      taskId: 't1',
      workspaceId: 'w1',
    });
  });

  it('still says it when the reply has no text', () => {
    expect(toastFor(reply([from('agent', { text: '' })]))?.message).toBe('New reply.');
  });

  // Your own message, echoing back off the stream you are subscribed to.
  it('says nothing about your own reply', () => {
    expect(toastFor(reply([from('agent'), from('human')]))).toBeNull();
  });

  it('asks for an answer when the agent wants a tool', () => {
    // The MCP server writes this metadata as `toolName`; older rows carry
    // `tool_name`. Reading only the second gave "Permission required: undefined"
    // for every permission request the app has ever shown.
    const camel = from('agent', { metadata: { type: 'permission_request', toolName: 'bash' } });
    const snake = from('agent', { metadata: { type: 'permission_request', tool_name: 'bash' } });

    for (const asking of [camel, snake]) {
      expect(toastFor(reply([asking]))).toEqual({
        tone: 'error',
        kind: 'permission',
        title: 'Ship it',
        message: 'Permission required: bash',
        taskId: 't1',
        workspaceId: 'w1',
      });
    }
  });

  // The desktop shell runs its own stream and fires a real system notification
  // for this event, and it is the one that honours the per-workspace mute.
  it('leaves an ordinary reply to the shell on desktop', () => {
    expect(toastFor(reply([from('agent')]), { platform: 'desktop' })).toBeNull();
  });

  // But not the ones that say more than the shell's notification does.
  it('still asks about a permission and a status change on desktop', () => {
    const asking = from('agent', { metadata: { type: 'permission_request', toolName: 'bash' } });
    const moved = { type: 'task.status', payload: { taskId: 't1', workspaceId: 'w1', title: 'Ship it', from: 'ongoing', to: 'blocked' } };

    expect(toastFor(reply([asking]), { platform: 'desktop' })?.tone).toBe('error');
    expect(toastFor(moved, { platform: 'desktop' })?.kind).toBe('status');
  });

  // An agent is told to report every few steps. Toasting each one over the task
  // you are reading is being talked over, not being kept informed.
  it('says nothing about the task you are already looking at', () => {
    expect(toastFor(reply([from('agent')]), { openTaskId: 't1' })).toBeNull();
    expect(toastFor(reply([from('agent')]), { openTaskId: 'another' })).not.toBeNull();
  });

  // The server's line about a status change. The change arrives as its own
  // task.status event, with the status it came from; this would say it twice.
  it('leaves a status announcement to the status event', () => {
    expect(toastFor(reply([from('agent', { text: 'Status updated to: ongoing' })]))).toBeNull();
  });

  it('welcomes a task the agent started by itself', () => {
    const event = {
      type: 'task.created',
      payload: { id: 't2', workspaceId: 'w1', title: 'Nightly digest', createdBy: 'agent' },
    };

    expect(toastFor(event)).toEqual({
      tone: 'success',
      kind: 'created',
      title: 'Nightly digest',
      message: 'The agent opened a new task.',
      taskId: 't2',
      workspaceId: 'w1',
    });
  });

  // It starts with nobody watching, whoever wrote the schedule.
  it('says when a scheduled run starts, even from your own schedule', () => {
    for (const createdBy of ['human', 'agent']) {
      const event = {
        type: 'task.created',
        payload: { id: 't3', workspaceId: 'w1', title: 'Nightly digest', createdBy, parentId: 't0' },
      };
      expect(toastFor(event)).toEqual({
        tone: 'info',
        kind: 'scheduled',
        title: 'Nightly digest',
        message: 'A scheduled run just started.',
        taskId: 't3',
        workspaceId: 'w1',
      });
    }
  });

  // A task.created payload missing ids still gets a toast, just one that
  // links nowhere — the empty-string sentinel, not a crash.
  it('still says it with no id to link to', () => {
    const event = { type: 'task.created', payload: { title: 'Nightly digest', createdBy: 'agent' } };

    expect(toastFor(event)).toEqual({
      tone: 'success',
      kind: 'created',
      title: 'Nightly digest',
      message: 'The agent opened a new task.',
      taskId: '',
      workspaceId: '',
    });
  });

  // You just created it. You know.
  it('says nothing about a task you created yourself', () => {
    expect(toastFor({ type: 'task.created', payload: { title: 'Mine', createdBy: 'human' } })).toBeNull();
  });

  it('has nothing to say about the rest of the stream', () => {
    expect(toastFor({ type: 'task.updated', payload: { id: 't1' } })).toBeNull();
    expect(toastFor({ type: 'agent.connected', payload: {} })).toBeNull();
    expect(toastFor({ type: 'reply.received' })).toBeNull();
    expect(toastFor(undefined)).toBeNull();
  });

  it('holds up against a reply carrying no messages', () => {
    expect(toastFor(reply([]))).toBeNull();
    expect(toastFor(reply(undefined))).toBeNull();
  });
});

// The reason a launch was refused reaches the browser on this event and used
// to stop there. The row is deleted the instant it fails, so the page the
// launch navigated to has nothing left to read — a toast is the only place
// this is ever said, and without it the reason lives in the daemon's log on
// the machine it was refused on.
describe('a launch that was refused', () => {
  const session = (payload) => ({ type: 'session.updated', payload });

  it('says why', () => {
    const refused = session({
      id: 's1',
      status: 'failed',
      error: 'supervisor: too many sessions running: 8 already running for profile "default", and the limit is 8',
    });

    expect(toastFor(refused)).toEqual({
      tone: 'error',
      kind: 'failed',
      title: 'Agent could not start',
      message:
        'supervisor: too many sessions running: 8 already running for profile "default", and the limit is 8',
      taskId: '',
      workspaceId: '',
    });
  });

  // It says it on the desktop too. The shell's own notifier handles task
  // events and knows nothing about sessions, so skipping this there would
  // leave the desktop with no way to hear it at all.
  it('says it on the desktop as well', () => {
    const refused = session({ id: 's1', status: 'failed', error: 'no such directory: /srv/gone' });

    expect(toastFor(refused, { platform: 'desktop' })?.message).toBe('no such directory: /srv/gone');
  });

  // The terminal of an agent that has ended cannot be opened, so the last line
  // it printed is said here — for the gateway, the reason it gave up.
  it('says why an agent exited failing', () => {
    const exited = session({ id: 's1', status: 'exited', exitCode: 1, error: '[acp-gateway] No build for this machine.' });

    expect(toastFor(exited)).toEqual({
      tone: 'error',
      kind: 'failed',
      title: 'Agent exited',
      message: '[acp-gateway] No build for this machine.',
      taskId: '',
      workspaceId: '',
    });
  });

  // An exit code with nothing to explain it is not worth interrupting for.
  it('stays quiet about a failure with no reason to give', () => {
    expect(toastFor(session({ id: 's1', status: 'failed' }))).toBeNull();
    expect(toastFor(session({ id: 's1', status: 'failed', error: '' }))).toBeNull();
    expect(toastFor(session({ id: 's1', status: 'exited', exitCode: 1 }))).toBeNull();
  });

  it('stays quiet about the ordinary life of a session', () => {
    expect(toastFor(session({ id: 's1', status: 'running' }))).toBeNull();
    expect(toastFor(session({ id: 's1', status: 'exited', exitCode: 0 }))).toBeNull();
    expect(toastFor(session({ id: 's1', status: 'killed' }))).toBeNull();
    expect(toastFor(session({ id: 's1', status: 'killed', error: 'stopped' }))).toBeNull();
  });
});

// An agent moving a task: the news the stream used to drop, since the server's
// status line only surfaced on the next reply.
describe('a status change', () => {
  const moved = (from, to, over = {}) => ({
    type: 'task.status',
    payload: { taskId: 't1', workspaceId: 'w1', title: 'Approve DB migration script', from, to, ...over },
  });

  it('shows where the task came from and where it went', () => {
    expect(toastFor(moved('ongoing', 'blocked'))).toEqual({
      tone: 'error',
      kind: 'status',
      title: 'Approve DB migration script',
      message: 'The agent needs your input.',
      from: 'ongoing',
      to: 'blocked',
      taskId: 't1',
      workspaceId: 'w1',
    });
  });

  it('reads each status for what it means', () => {
    const said = (to) => toastFor(moved('notstarted', to));
    expect(said('completed')).toMatchObject({ tone: 'success', message: 'The agent finished this task.' });
    expect(said('ongoing')).toMatchObject({ tone: 'info', message: 'The agent is working on it.' });
    expect(said('rejected')).toMatchObject({ tone: 'info', message: 'The agent declined this task.' });
    expect(toastFor(moved('ongoing', 'notstarted'))).toMatchObject({ message: 'Moved back to not started.' });
    expect(said('cron')).toMatchObject({ message: 'Turned into a scheduled task.' });
    expect(said('someday')).toMatchObject({ tone: 'info', message: 'Now someday.' });
  });

  // The status is the news, and the open task shows it only in its header.
  it('says it over the task you are looking at too', () => {
    expect(toastFor(moved('ongoing', 'completed'), { openTaskId: 't1' })).not.toBeNull();
  });

  it('has nothing to say without a move', () => {
    expect(toastFor(moved('ongoing', 'ongoing'))).toBeNull();
    expect(toastFor(moved('ongoing', ''))).toBeNull();
  });

  it('holds up against a payload missing its ids', () => {
    const event = { type: 'task.status', payload: { to: 'completed' } };
    expect(toastFor(event)).toMatchObject({ title: '', from: '', taskId: '', workspaceId: '' });
  });
});

describe('snippet', () => {
  it('flattens a reply to one line', () => {
    expect(snippet('  a\n\n b\tc ')).toBe('a b c');
    expect(snippet(undefined)).toBe('');
  });

  it('cuts a long one short', () => {
    const cut = snippet('x'.repeat(400));
    expect(cut).toHaveLength(280);
    expect(cut.endsWith('…')).toBe(true);
  });
});

// A dropped stream reconnects in a second or two and says disconnect, then
// connect, on the way. That is not news; an agent gone for good is.
describe('createPresenceToasts', () => {
  const setup = (state = {}) => {
    vi.useFakeTimers();
    const shown = [];
    const onEvent = createPresenceToasts({
      notify: (toast) => shown.push(toast),
      wasConnected: (id) => state[id],
      graceMs: 1000,
    });
    // The store takes the event after the toast logic reads it.
    const send = (workspaceId, connected) => {
      onEvent({ workspaceId, connected });
      state[workspaceId] = connected;
    };
    return { shown, send };
  };

  afterEach(() => vi.useRealTimers());

  it('says an agent connected', () => {
    const { shown, send } = setup({ w1: false });
    send('w1', true);
    expect(shown).toEqual([{
      tone: 'success',
      kind: 'connected',
      title: 'Agent connected',
      message: 'Ready to take tasks.',
      taskId: '',
      workspaceId: 'w1',
    }]);
  });

  it('says an agent left, once it has stayed away', () => {
    const { shown, send } = setup({ w1: true });
    send('w1', false);
    expect(shown).toEqual([]);
    vi.advanceTimersByTime(1000);
    expect(shown).toMatchObject([{ kind: 'disconnected', title: 'Agent disconnected', workspaceId: 'w1' }]);
  });

  it('says nothing about a blip', () => {
    const { shown, send } = setup({ w1: true });
    send('w1', false);
    send('w1', false);
    send('w1', true);
    vi.advanceTimersByTime(5000);
    expect(shown).toEqual([]);
  });

  it('says it is back after saying it left', () => {
    const { shown, send } = setup({ w1: true });
    send('w1', false);
    vi.advanceTimersByTime(1000);
    send('w1', true);
    expect(shown.map((t) => t.kind)).toEqual(['disconnected', 'connected']);
  });

  // A workspace the list has not loaded yet: its state is unknown, and so is
  // whether anything changed.
  it('says nothing it cannot know', () => {
    const { shown, send } = setup({});
    send('w1', true);
    send('w2', false);
    send('', true);
    vi.advanceTimersByTime(5000);
    expect(shown).toEqual([]);
  });

  it('holds up against an event with no payload', () => {
    const shown = [];
    const onEvent = createPresenceToasts({ notify: (t) => shown.push(t), wasConnected: () => true });
    onEvent();
    expect(shown).toEqual([]);
  });
});
