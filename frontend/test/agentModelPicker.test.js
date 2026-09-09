import { describe, it, expect, vi } from 'vitest';
import { nextTick, ref } from 'vue';

import {
  CONFIRMATION_TIMEOUT_MS,
  canChooseModel,
  displayedModelId,
  isConfirmed,
  modelGroups,
  useAgentModelPicker,
} from '../src/composables/useAgentModelPicker';

/**
 * The picker never learns from its own request whether a switch happened: it
 * asks, and the agent answers later over the event stream. So what is pinned
 * here is mostly what happens in between, and what happens when the answer
 * never comes — the states a component cannot be trusted to get right by
 * inspection.
 */

const twoModels = {
  configId: 'model',
  currentModel: 'a',
  canSet: true,
  models: [
    { id: 'a', name: 'Model A', group: 'Anthropic' },
    { id: 'b', name: 'Model B', group: 'Google' },
  ],
};

const connected = (agentModels = twoModels) => ({ agentConnected: true, agentModels });

describe('canChooseModel', () => {
  it('offers the picker to an agent that reported models and can switch', () => {
    expect(canChooseModel(connected())).toBe(true);
  });

  it('refuses an agent that reports models but cannot be told to switch', () => {
    // Every gateway older than this feature. The list is perfectly real; the
    // control would do nothing.
    expect(canChooseModel(connected({ ...twoModels, canSet: false }))).toBe(false);
    const { canSet, ...withoutFlag } = twoModels;
    expect(canChooseModel(connected(withoutFlag))).toBe(false);
  });

  it('refuses a client that never mentioned models at all', () => {
    // Claude Code attached directly is in this state, and needs no list of
    // names to keep it out: it simply never reports any.
    expect(canChooseModel({ agentConnected: true })).toBe(false);
    expect(canChooseModel(connected({ ...twoModels, models: [] }))).toBe(false);
    expect(canChooseModel(connected({ ...twoModels, models: undefined }))).toBe(false);
  });

  it('refuses when no agent is attached', () => {
    // A choice sent to nothing goes nowhere.
    expect(canChooseModel({ agentConnected: false, agentModels: twoModels })).toBe(false);
    expect(canChooseModel(undefined)).toBe(false);
  });
});

describe('modelGroups', () => {
  it('keeps the agent’s own grouping and order', () => {
    expect(modelGroups(twoModels)).toEqual([
      { name: 'Anthropic', models: [twoModels.models[0]] },
      { name: 'Google', models: [twoModels.models[1]] },
    ]);
  });

  it('collects ungrouped models under one unnamed group', () => {
    const groups = modelGroups({ models: [{ id: 'a' }, { id: 'b', group: '  ' }] });
    expect(groups).toHaveLength(1);
    expect(groups[0].name).toBe('');
    expect(groups[0].models.map((m) => m.id)).toEqual(['a', 'b']);
  });

  it('gathers a group that appears more than once', () => {
    const groups = modelGroups({
      models: [{ id: 'a', group: 'X' }, { id: 'b', group: 'Y' }, { id: 'c', group: 'X' }],
    });
    expect(groups.map((g) => g.name)).toEqual(['X', 'Y']);
    expect(groups[0].models.map((m) => m.id)).toEqual(['a', 'c']);
  });

  it('skips entries with no id, and copes with nothing at all', () => {
    expect(modelGroups({ models: [null, { name: 'nameless' }, { id: 'a' }] })).toEqual([
      { name: '', models: [{ id: 'a' }] },
    ]);
    expect(modelGroups(undefined)).toEqual([]);
    expect(modelGroups({ models: 'not a list' })).toEqual([]);
  });
});

describe('displayedModelId', () => {
  it('shows what was asked for while it is outstanding', () => {
    // Showing the old value would read as the click having missed.
    expect(displayedModelId(twoModels, 'b')).toBe('b');
  });

  it('shows the agent’s own answer when nothing is outstanding', () => {
    expect(displayedModelId(twoModels, '')).toBe('a');
    expect(displayedModelId(undefined, '')).toBe('');
  });
});

describe('isConfirmed', () => {
  it('is confirmed only when the agent names the model that was asked for', () => {
    expect(isConfirmed('b', { currentModel: 'b' })).toBe(true);
    expect(isConfirmed('b', { currentModel: 'a' })).toBe(false);
    expect(isConfirmed('b', undefined)).toBe(false);
  });

  it('has nothing to confirm when nothing was asked', () => {
    expect(isConfirmed('', { currentModel: 'a' })).toBe(true);
  });
});

describe('useAgentModelPicker', () => {
  /** A workspace the test can rewrite, the way the event stream rewrites it. */
  function harness({ selectModel = vi.fn().mockResolvedValue({}), clock = { t: 0 } } = {}) {
    const workspace = ref(connected());
    const picker = useAgentModelPicker({
      workspace: () => workspace.value,
      selectModel,
      now: () => clock.t,
    });
    /** What arrives when the agent reports, replacing the object in place. */
    const agentReports = async (currentModel) => {
      workspace.value = connected({ ...twoModels, currentModel });
      await nextTick();
    };
    return { picker, workspace, selectModel, clock, agentReports };
  }

  it('shows the asked-for model, then the agent’s once it agrees', async () => {
    const { picker, agentReports } = harness();

    await picker.choose('b');
    expect(picker.selectedId.value).toBe('b');
    expect(picker.isPending.value).toBe(true);
    expect(picker.selectedName.value).toBe('Model B');

    await agentReports('b');

    expect(picker.isPending.value).toBe(false);
    expect(picker.selectedId.value).toBe('b');
  });

  it('sends the chosen model exactly once', async () => {
    const { picker, selectModel } = harness();

    await picker.choose('b');

    expect(selectModel).toHaveBeenCalledTimes(1);
    expect(selectModel).toHaveBeenCalledWith('b');
  });

  it('does nothing when the model is already current, or is nothing', async () => {
    // A round trip whose answer is already on screen.
    const { picker, selectModel } = harness();

    await picker.choose('a');
    await picker.choose('');

    expect(selectModel).not.toHaveBeenCalled();
    expect(picker.isPending.value).toBe(false);
  });

  it('gives up and says so when the server refuses', async () => {
    // A refusal means nothing was asked, so there is nothing to wait for — the
    // picker must not sit pending on a request that never happened.
    const selectModel = vi.fn().mockRejectedValue(new Error('the connected agent does not offer that model'));
    const { picker } = harness({ selectModel });

    await picker.choose('b');

    expect(picker.isPending.value).toBe(false);
    expect(picker.selectedId.value).toBe('a');
    expect(picker.error.value).toBe('the connected agent does not offer that model');
  });

  it('still says something when a refusal carries no message', async () => {
    const { picker } = harness({ selectModel: vi.fn().mockRejectedValue({}) });

    await picker.choose('b');

    expect(picker.error.value).toBe('Could not change the model');
  });

  it('reverts when the agent never confirms', async () => {
    // The switch was asked for and nothing came back. Reverting puts back what
    // the agent last said is true, rather than leaving a model on screen that
    // nothing ever adopted.
    const clock = { t: 0 };
    const { picker } = harness({ clock });

    await picker.choose('b');
    clock.t = CONFIRMATION_TIMEOUT_MS;
    picker.expirePending();

    expect(picker.isPending.value).toBe(false);
    expect(picker.selectedId.value).toBe('a');
    expect(picker.error.value).toBe('The agent did not confirm the change');
  });

  it('keeps waiting until the timeout is actually up', async () => {
    const clock = { t: 0 };
    const { picker } = harness({ clock });

    await picker.choose('b');
    clock.t = CONFIRMATION_TIMEOUT_MS - 1;
    picker.expirePending();

    expect(picker.isPending.value).toBe(true);
    expect(picker.error.value).toBe('');
  });

  it('has nothing to expire when nothing is outstanding', () => {
    const { picker } = harness();

    picker.expirePending();

    expect(picker.error.value).toBe('');
  });

  it('takes a report naming a different model as the agent’s answer', async () => {
    // The agent declined, or a second gateway answered, or an unrelated update
    // landed mid-flight. The request is over and what arrived is the truth — but
    // which of those it was is not knowable here, so the message says only what
    // is running rather than accusing the agent of refusing.
    const { picker, workspace } = harness();

    await picker.choose('b');
    workspace.value = connected({ ...twoModels, currentModel: 'a', models: [...twoModels.models] });
    await nextTick();

    expect(picker.isPending.value).toBe(false);
    expect(picker.selectedId.value).toBe('a');
    expect(picker.error.value).toBe('The agent is running Model A');
  });

  it('does not claim a refusal for an unrelated report that lands mid-switch', async () => {
    // The backend republishes on a changed config option or list, not only on a
    // switch. Calling that a refusal was wrong, and a later confirming report
    // would then contradict the message the human had already been shown.
    const { picker, workspace } = harness();

    await picker.choose('b');
    // Same current model, different config option — a real republish that says
    // nothing about the switch.
    workspace.value = connected({ ...twoModels, configId: 'model_id' });
    await nextTick();
    expect(picker.error.value).not.toMatch(/stayed|refus/i);

    // And the switch it was really waiting for still lands correctly.
    workspace.value = connected({ ...twoModels, currentModel: 'b' });
    await nextTick();
    expect(picker.selectedId.value).toBe('b');
    expect(picker.isPending.value).toBe(false);
  });

  it('calls off an outstanding switch when the running model is picked again', async () => {
    // The only way to take back a choice. Without it the picker showed the
    // abandoned model for the full timeout, with nothing the human could do.
    const { picker, selectModel } = harness();

    await picker.choose('b');
    expect(picker.isPending.value).toBe(true);

    await picker.choose('a');

    expect(picker.isPending.value).toBe(false);
    expect(picker.selectedId.value).toBe('a');
    // Nothing asked of the agent: it is already running this model.
    expect(selectModel).toHaveBeenCalledTimes(1);
  });

  it('tracks whether a request is in flight', async () => {
    let release;
    const selectModel = vi.fn(() => new Promise((resolve) => { release = resolve; }));
    const { picker } = harness({ selectModel });

    const choosing = picker.choose('b');
    expect(picker.sending.value).toBe(true);

    release({});
    await choosing;
    expect(picker.sending.value).toBe(false);
  });

  it('uses a real clock when none is supplied', async () => {
    // The component passes no clock, so the default is what actually runs in
    // the app — and a switch asked for just now is not yet expired.
    const workspace = ref(connected());
    const picker = useAgentModelPicker({
      workspace: () => workspace.value,
      selectModel: vi.fn().mockResolvedValue({}),
    });

    await picker.choose('b');
    picker.expirePending();

    expect(picker.isPending.value).toBe(true);
  });

  it('exposes the workspace’s own answer to whether a picker belongs', async () => {
    const { picker, workspace } = harness();
    expect(picker.available.value).toBe(true);
    expect(picker.groups.value.map((g) => g.name)).toEqual(['Anthropic', 'Google']);

    workspace.value = { agentConnected: false };
    await nextTick();

    expect(picker.available.value).toBe(false);
    expect(picker.groups.value).toEqual([]);
  });
});
