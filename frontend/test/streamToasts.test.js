// Copyright 2026 Contextual, Inc. https://agentrq.com

import { describe, it, expect } from 'vitest';

import { isOpenPermissionRequest, lastMessage, toastFor } from '../src/composables/useStreamToasts';

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
  payload: { id: 't1', title: 'Ship it', status: 'ongoing', messages, ...over },
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
  it('says when the agent replied', () => {
    expect(toastFor(reply([from('human'), from('agent')]))).toEqual({
      tone: 'info',
      message: 'New reply on "Ship it"',
    });
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
        title: 'Action Needed',
        message: 'Permission required: bash',
      });
    }
  });

  // The desktop shell runs its own stream and fires a real system notification
  // for this event, and it is the one that honours the per-workspace mute.
  it('leaves an ordinary reply to the shell on desktop', () => {
    expect(toastFor(reply([from('agent')]), { platform: 'desktop' })).toBeNull();
  });

  // But not the two that say more than the shell's notification does.
  it('still asks about a permission and a status change on desktop', () => {
    const asking = from('agent', { metadata: { type: 'permission_request', toolName: 'bash' } });
    const said = from('agent', { text: 'Status updated to: ongoing' });

    expect(toastFor(reply([asking]), { platform: 'desktop' })?.tone).toBe('error');
    expect(toastFor(reply([said]), { platform: 'desktop' })?.tone).toBe('info');
  });

  // An agent is told to report every few steps. Toasting each one over the task
  // you are reading is being talked over, not being kept informed.
  it('says nothing about the task you are already looking at', () => {
    expect(toastFor(reply([from('agent')]), { openTaskId: 't1' })).toBeNull();
    expect(toastFor(reply([from('agent')]), { openTaskId: 'another' })).not.toBeNull();
  });

  // Read as the status rather than as the sentence the agent wrote about it.
  it('reads a status announcement as the status', () => {
    const said = from('agent', { text: 'Status updated to: ongoing' });

    expect(toastFor(reply([said]))).toEqual({ tone: 'info', message: 'Task "Ship it" is now ongoing' });
  });

  it('welcomes a task the agent started by itself', () => {
    const event = { type: 'task.created', payload: { title: 'Nightly digest', createdBy: 'agent' } };

    expect(toastFor(event)).toEqual({ tone: 'success', message: 'Agent started a new task: Nightly digest' });
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
