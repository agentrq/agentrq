// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, afterEach } from 'vitest';
import { createApp, h } from 'vue';

import { notifyWebMCPChange, onWebMCPChange } from '../src/composables/useWebMCPChanges';

const tick = () => new Promise((r) => setTimeout(r, 0));

describe('onWebMCPChange', () => {
  const offs = [];
  const listen = (fn) => {
    const off = onWebMCPChange(fn);
    offs.push(off);
    return off;
  };
  afterEach(() => {
    offs.splice(0).forEach((off) => off());
    vi.restoreAllMocks();
  });

  it('re-runs the reload on every change', async () => {
    const reload = vi.fn();
    listen(reload);

    notifyWebMCPChange();
    await tick();
    notifyWebMCPChange();
    await tick();

    expect(reload).toHaveBeenCalledTimes(2);
  });

  it('stops once told to', async () => {
    const reload = vi.fn();
    listen(reload)();

    notifyWebMCPChange();
    await tick();

    expect(reload).not.toHaveBeenCalled();
  });

  it('never overlaps runs, and folds a burst into one more', async () => {
    const pending = [];
    const reload = vi.fn(() => new Promise((r) => pending.push(r)));
    listen(reload);

    notifyWebMCPChange();
    // Three more while the first is still in flight: an older response must
    // not land after a newer one, and nine calls need not mean nine reloads.
    notifyWebMCPChange();
    notifyWebMCPChange();
    notifyWebMCPChange();
    expect(reload).toHaveBeenCalledTimes(1);

    pending.shift()();
    await tick();
    expect(reload).toHaveBeenCalledTimes(2);

    pending.shift()();
    await tick();
    expect(reload).toHaveBeenCalledTimes(2);

    notifyWebMCPChange();
    expect(reload).toHaveBeenCalledTimes(3);
    pending.shift()();
  });

  it('survives a failed reload, and tries again on the next change', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {});
    const reload = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValue(undefined);
    const other = vi.fn();
    listen(reload);
    listen(other);

    notifyWebMCPChange();
    await tick();
    expect(error).toHaveBeenCalledWith('Refreshing after a WebMCP change failed:', expect.any(Error));
    expect(other).toHaveBeenCalledTimes(1);

    notifyWebMCPChange();
    await tick();
    expect(reload).toHaveBeenCalledTimes(2);
  });

  it('stops listening when the component that asked unmounts', async () => {
    const reload = vi.fn();
    const app = createApp({ setup: () => { onWebMCPChange(reload); return () => h('div'); } });
    app.mount(document.createElement('div'));

    notifyWebMCPChange();
    await tick();
    app.unmount();
    notifyWebMCPChange();
    await tick();

    expect(reload).toHaveBeenCalledTimes(1);
  });
});
