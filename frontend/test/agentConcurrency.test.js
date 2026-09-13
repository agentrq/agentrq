// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi } from 'vitest';
import { nextTick, ref } from 'vue';

import {
  CONFIRMATION_TIMEOUT_MS,
  MIN_CONCURRENCY,
  canSetConcurrency,
  clampConcurrency,
  concurrencyLimit,
  concurrencyRange,
  concurrencySummary,
  displayedConcurrency,
  hasConcurrency,
  isConcurrencyConfirmed,
  isOverLimit,
  useAgentConcurrency,
} from '../src/composables/useAgentConcurrency';

/**
 * The control never learns from its own request whether a change happened: it
 * asks, and the gateway answers later over the event stream — accepted,
 * clamped, or not at all. So what is pinned here is mostly what happens in
 * between, and what happens when the answer never comes.
 */

const reported = {
  maxConcurrency: 4,
  active: 2,
  queued: 3,
  min: 1,
  max: 64,
  canSet: true,
};

const connected = (agentConcurrency = reported) => ({ agentConnected: true, agentConcurrency });

describe('hasConcurrency', () => {
  it('shows a number for a gateway that reported one', () => {
    expect(hasConcurrency(connected())).toBe(true);
    // A gateway that cannot be told to change still has a number worth showing.
    expect(hasConcurrency(connected({ ...reported, canSet: false }))).toBe(true);
  });

  it('shows nothing for a client that never mentioned a limit', () => {
    // Claude Code attached directly is in this state: it has no queue of its
    // own to report, and needs no list of names to keep it out.
    expect(hasConcurrency({ agentConnected: true })).toBe(false);
    expect(hasConcurrency(connected({ ...reported, maxConcurrency: 0 }))).toBe(false);
  });

  it('shows nothing once the agent has gone', () => {
    // A number from something that has disconnected is not news about anything.
    expect(hasConcurrency({ agentConnected: false, agentConcurrency: reported })).toBe(false);
    expect(hasConcurrency(undefined)).toBe(false);
  });
});

describe('canSetConcurrency', () => {
  it('offers the control to a gateway that said it can be told', () => {
    expect(canSetConcurrency(connected())).toBe(true);
  });

  it('refuses a gateway that reports a limit but ignores being told', () => {
    // Every gateway older than this feature. The number is perfectly real; the
    // control would do nothing, so silence is read as no.
    expect(canSetConcurrency(connected({ ...reported, canSet: false }))).toBe(false);
    const { canSet, ...withoutFlag } = reported;
    expect(canSetConcurrency(connected(withoutFlag))).toBe(false);
  });

  it('refuses anything with no number behind it', () => {
    expect(canSetConcurrency({ agentConnected: true })).toBe(false);
    expect(canSetConcurrency(connected({ canSet: true }))).toBe(false);
  });
});

describe('concurrencyLimit', () => {
  it('reads the limit in force', () => {
    expect(concurrencyLimit(reported)).toBe(4);
  });

  it('reads nonsense as nothing reported', () => {
    // Zero is a queue that never hands anything out, which is a stall rather
    // than a setting — not something to show as a limit.
    expect(concurrencyLimit({ maxConcurrency: 0 })).toBe(0);
    expect(concurrencyLimit({ maxConcurrency: -3 })).toBe(0);
    expect(concurrencyLimit({ maxConcurrency: 'four' })).toBe(0);
    expect(concurrencyLimit(undefined)).toBe(0);
  });

  it('takes whole tasks only', () => {
    expect(concurrencyLimit({ maxConcurrency: 4.9 })).toBe(4);
  });
});

describe('concurrencyRange', () => {
  it('uses the range the gateway reported', () => {
    expect(concurrencyRange({ maxConcurrency: 4, min: 2, max: 16 })).toEqual({ min: 2, max: 16 });
  });

  it('floors the minimum at one whatever the gateway says', () => {
    expect(concurrencyRange({ maxConcurrency: 4, min: 0 })).toEqual({ min: MIN_CONCURRENCY, max: 0 });
    expect(concurrencyRange({ maxConcurrency: 4, min: -8 }).min).toBe(1);
  });

  it('leaves an unstated ceiling at zero rather than inventing one', () => {
    // The gateway owns its ceiling. A number made up here would be wrong the
    // first time the real one moved.
    expect(concurrencyRange({ maxConcurrency: 4 })).toEqual({ min: 1, max: 0 });
    expect(concurrencyRange({ maxConcurrency: 4, max: 'lots' }).max).toBe(0);
  });

  it('widens the range to contain the limit in force', () => {
    // A gateway started with --max-concurrency 100 can still report max: 64. A
    // control clamped to the reported range could not display the value it was
    // showing, and merely opening it would propose a change nobody asked for.
    expect(concurrencyRange({ maxConcurrency: 100, min: 1, max: 64 })).toEqual({ min: 1, max: 100 });
    expect(concurrencyRange({ maxConcurrency: 2, min: 8, max: 64 })).toEqual({ min: 2, max: 64 });
  });

  it('never produces a range no control can represent', () => {
    const { min, max } = concurrencyRange({ maxConcurrency: 4, min: 8, max: 2 });
    expect(min).toBeLessThanOrEqual(max);
  });

  it('answers for a workspace with nothing reported at all', () => {
    expect(concurrencyRange(undefined)).toEqual({ min: 1, max: 0 });
  });
});

describe('clampConcurrency', () => {
  const range = { min: 2, max: 16 };

  it('leaves a value inside the range alone', () => {
    expect(clampConcurrency(8, range)).toBe(8);
  });

  it('brings an out-of-range value to the nearest bound', () => {
    // The gateway would clamp it anyway, and then answer with a limit that is
    // not the one asked for — which the control reads as its request having
    // been overruled, and says so. Clamping first keeps the answer agreeing
    // with the question.
    expect(clampConcurrency(9000, range)).toBe(16);
    expect(clampConcurrency(1, range)).toBe(2);
  });

  it('ignores an open-ended ceiling', () => {
    expect(clampConcurrency(9000, { min: 1, max: 0 })).toBe(9000);
  });

  it('takes whole tasks only, and refuses what is not a number', () => {
    expect(clampConcurrency('8', range)).toBe(8);
    expect(clampConcurrency(8.7, range)).toBe(8);
    expect(clampConcurrency('lots', range)).toBe(0);
    expect(clampConcurrency(undefined, range)).toBe(0);
  });

  it('falls back to a sane range when given none', () => {
    expect(clampConcurrency(0, undefined)).toBe(1);
  });
});

describe('isOverLimit', () => {
  it('is true while more is running than the limit allows', () => {
    // Not a fault: lowering never interrupts a running task, so this is the
    // feature working and an interface that flagged it red would be wrong.
    expect(isOverLimit({ maxConcurrency: 1, active: 4 })).toBe(true);
  });

  it('is false when the queue is within its limit', () => {
    expect(isOverLimit(reported)).toBe(false);
    expect(isOverLimit({ maxConcurrency: 4, active: 4 })).toBe(false);
  });

  it('is false when there is no limit to be over', () => {
    expect(isOverLimit({ active: 4 })).toBe(false);
    expect(isOverLimit(undefined)).toBe(false);
  });

  it('counts an unreadable active as none running', () => {
    expect(isOverLimit({ maxConcurrency: 1, active: 'lots' })).toBe(false);
  });
});

describe('concurrencySummary', () => {
  it('says what the queue is doing', () => {
    expect(concurrencySummary(reported)).toBe('2 running · 3 queued');
  });

  it('counts absent numbers as none', () => {
    expect(concurrencySummary({ maxConcurrency: 4 })).toBe('0 running · 0 queued');
  });

  it('says nothing when there is nothing to say', () => {
    expect(concurrencySummary(undefined)).toBe('');
  });
});

describe('displayedConcurrency', () => {
  it('shows what was asked for while it is outstanding', () => {
    // Showing the old value would read as the change having missed.
    expect(displayedConcurrency(reported, 8)).toBe(8);
  });

  it('shows the gateway’s own answer when nothing is outstanding', () => {
    expect(displayedConcurrency(reported, 0)).toBe(4);
    expect(displayedConcurrency(undefined, 0)).toBe(0);
  });
});

describe('isConcurrencyConfirmed', () => {
  it('is confirmed only when the gateway names the limit that was asked for', () => {
    expect(isConcurrencyConfirmed(8, { maxConcurrency: 8 })).toBe(true);
    expect(isConcurrencyConfirmed(8, { maxConcurrency: 4 })).toBe(false);
    expect(isConcurrencyConfirmed(8, undefined)).toBe(false);
  });

  it('has nothing to confirm when nothing was asked', () => {
    expect(isConcurrencyConfirmed(0, { maxConcurrency: 4 })).toBe(true);
  });
});

describe('useAgentConcurrency', () => {
  /** A workspace the test can rewrite, the way the event stream rewrites it. */
  function harness({ setConcurrency = vi.fn().mockResolvedValue({}), clock = { t: 0 } } = {}) {
    const workspace = ref(connected());
    const control = useAgentConcurrency({
      workspace: () => workspace.value,
      setConcurrency,
      now: () => clock.t,
    });
    /** What arrives when the gateway reports, replacing the object in place. */
    const gatewayReports = async (patch) => {
      workspace.value = connected({ ...reported, ...patch });
      await nextTick();
    };
    return { control, workspace, setConcurrency, clock, gatewayReports };
  }

  it('exposes what the gateway is doing right now', () => {
    const { control } = harness();

    expect(control.visible.value).toBe(true);
    expect(control.editable.value).toBe(true);
    expect(control.value.value).toBe(4);
    expect(control.active.value).toBe(2);
    expect(control.queued.value).toBe(3);
    expect(control.summary.value).toBe('2 running · 3 queued');
    expect(control.range.value).toEqual({ min: 1, max: 64 });
    expect(control.overLimit.value).toBe(false);
  });

  it('shows the asked-for limit, then the gateway’s once it agrees', async () => {
    const { control, gatewayReports } = harness();

    await control.choose(8);
    expect(control.value.value).toBe(8);
    expect(control.isPending.value).toBe(true);

    await gatewayReports({ maxConcurrency: 8 });

    expect(control.isPending.value).toBe(false);
    expect(control.value.value).toBe(8);
  });

  it('sends the chosen limit exactly once', async () => {
    const { control, setConcurrency } = harness();

    await control.choose(8);

    expect(setConcurrency).toHaveBeenCalledTimes(1);
    expect(setConcurrency).toHaveBeenCalledWith(8);
  });

  it('clamps before sending rather than letting the gateway overrule it', async () => {
    // Asking for 9000 against a ceiling of 64 is obeyed as well as it can be,
    // but the report that comes back names 64 — and a report naming a limit
    // other than the one asked for is how being overruled looks from here. So
    // the request is brought inside the range before it is sent.
    const { control, setConcurrency } = harness();

    await control.choose(9000);

    expect(setConcurrency).toHaveBeenCalledWith(64);
  });

  it('does nothing when the limit is already in force, or is not a number', async () => {
    const { control, setConcurrency } = harness();

    await control.choose(4);
    await control.choose('lots');

    expect(setConcurrency).not.toHaveBeenCalled();
    expect(control.isPending.value).toBe(false);
  });

  it('calls off an outstanding change when asked for what is already running', async () => {
    // Without this the control went on showing the abandoned value until the
    // timeout, with no way to take it back.
    const { control } = harness();

    await control.choose(8);
    expect(control.isPending.value).toBe(true);

    await control.choose(4);
    expect(control.isPending.value).toBe(false);
  });

  it('settles on a report that names some other limit', async () => {
    // The gateway clamped the value, or a second gateway answered. Either way
    // the request is over and what just arrived is the truth — but why it
    // happened is not established, so the message says only what is known.
    const { control, gatewayReports } = harness();

    await control.choose(8);
    await gatewayReports({ maxConcurrency: 6 });

    expect(control.isPending.value).toBe(false);
    expect(control.value.value).toBe(6);
    expect(control.error.value).toBe('The gateway is running 6 at a time');
  });

  it('stays pending when a report carries no limit at all', async () => {
    // Nothing has been answered: there is no number in it to settle against.
    const { control, gatewayReports } = harness();

    await control.choose(8);
    await gatewayReports({ maxConcurrency: 0 });

    expect(control.isPending.value).toBe(true);
  });

  it('gives up and says so when the server refuses', async () => {
    // A refusal means nothing was asked, so there is nothing to wait for.
    const setConcurrency = vi.fn().mockRejectedValue(
      new Error('the connected agent does not support changing its concurrency'),
    );
    const { control } = harness({ setConcurrency });

    await control.choose(8);

    expect(control.isPending.value).toBe(false);
    expect(control.value.value).toBe(4);
    expect(control.error.value).toBe('the connected agent does not support changing its concurrency');
  });

  it('still says something when a refusal carries no message', async () => {
    const { control } = harness({ setConcurrency: vi.fn().mockRejectedValue({}) });

    await control.choose(8);

    expect(control.error.value).toBe('Could not change the concurrency');
  });

  it('reverts when the gateway never confirms', async () => {
    // The change was asked for and nothing came back. Reverting puts back what
    // the gateway last said is true.
    const clock = { t: 0 };
    const { control } = harness({ clock });

    await control.choose(8);
    clock.t = CONFIRMATION_TIMEOUT_MS - 1;
    control.expirePending();
    expect(control.isPending.value).toBe(true);

    clock.t = CONFIRMATION_TIMEOUT_MS;
    control.expirePending();

    expect(control.isPending.value).toBe(false);
    expect(control.value.value).toBe(4);
    expect(control.error.value).toBe('The gateway did not confirm the change');
  });

  it('has nothing to expire when nothing is outstanding', () => {
    const { control } = harness();

    control.expirePending();

    expect(control.error.value).toBe('');
  });

  it('reports more running than allowed rather than hiding it', async () => {
    // Lowering never interrupts a running task: they finish, and the queue
    // stops handing out new ones until the active count drops below the limit.
    const { control, gatewayReports } = harness();

    await gatewayReports({ maxConcurrency: 1, active: 4 });

    expect(control.overLimit.value).toBe(true);
    expect(control.active.value).toBe(4);
    expect(control.value.value).toBe(1);
  });

  it('uses the wall clock when it is given none', async () => {
    // The component builds it this way. A change asked for right now is not
    // stale, so the real Date.now has to keep it pending rather than expire it
    // on the spot.
    const workspace = ref(connected());
    const control = useAgentConcurrency({ workspace: () => workspace.value, setConcurrency: vi.fn().mockResolvedValue({}) });

    await control.choose(8);
    control.expirePending();

    expect(control.isPending.value).toBe(true);
    expect(control.error.value).toBe('');
  });

  it('draws nothing for a client with no queue of its own', () => {
    const workspace = ref({ agentConnected: true });
    const control = useAgentConcurrency({ workspace: () => workspace.value, setConcurrency: vi.fn() });

    expect(control.visible.value).toBe(false);
    expect(control.editable.value).toBe(false);
    expect(control.value.value).toBe(0);
    expect(control.active.value).toBe(0);
    expect(control.queued.value).toBe(0);
    expect(control.summary.value).toBe('');
  });

  it('shows a read-only number for a gateway that cannot be told', () => {
    const workspace = ref(connected({ ...reported, canSet: false }));
    const control = useAgentConcurrency({ workspace: () => workspace.value, setConcurrency: vi.fn() });

    expect(control.visible.value).toBe(true);
    expect(control.editable.value).toBe(false);
  });

  it('marks a send in flight while the request is out', async () => {
    let release;
    const setConcurrency = vi.fn(() => new Promise((resolve) => { release = resolve; }));
    const { control } = harness({ setConcurrency });

    const sent = control.choose(8);
    expect(control.sending.value).toBe(true);

    release({});
    await sent;
    expect(control.sending.value).toBe(false);
  });
});
