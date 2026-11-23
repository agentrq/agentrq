// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, afterEach } from 'vitest';

import { withScreen, hasUnsavedInput } from '../src/composables/useWebMCPScreen';

afterEach(() => {
  document.body.innerHTML = '';
});

describe('hasUnsavedInput', () => {
  const page = (html) => {
    document.body.innerHTML = html;
    return hasUnsavedInput();
  };

  it('sees a draft in a textarea or a text field', () => {
    expect(page('<textarea>half a reply</textarea>')).toBe(true);
    expect(page('<input type="text" value="a title">')).toBe(true);
    expect(page('<input value="no type is text">')).toBe(true);
  });

  it('is not fooled by an empty or blank field', () => {
    expect(page('<textarea>  </textarea><input type="text" value="">')).toBe(false);
  });

  it('ignores what is not typed text: a search box, a toggle, a read-only or disabled field', () => {
    expect(
      page(
        '<input type="search" value="q"><input type="checkbox" value="on"><input type="radio" value="a">' +
          '<input type="text" value="fixed" readonly><textarea disabled>locked</textarea>',
      ),
    ).toBe(false);
  });

  it('is false on a page with no fields at all, or with no document', () => {
    expect(page('<p>nothing to type in</p>')).toBe(false);
    expect(hasUnsavedInput(null)).toBe(false);
  });
});

describe('withScreen', () => {
  const setup = ({ screen, current = false, draft = false, execute } = {}) => {
    const deps = {
      go: vi.fn().mockResolvedValue(undefined),
      isCurrent: vi.fn().mockReturnValue(current),
      hasUnsavedInput: vi.fn().mockReturnValue(draft),
      notify: vi.fn(),
    };
    const tool = {
      name: 'restartDaemon',
      description: 'd',
      annotations: {},
      screen,
      execute: execute ?? vi.fn().mockResolvedValue({ ok: true }),
    };
    return { deps, tool, wrapped: withScreen(tool, deps) };
  };

  it('returns a tool with no screen untouched', () => {
    const { tool, wrapped } = setup({ screen: undefined });

    expect(wrapped).toBe(tool);
  });

  it('does not offer the screen to the agent', () => {
    const { wrapped } = setup({ screen: { before: () => '/x' } });

    expect(wrapped).not.toHaveProperty('screen');
    expect(wrapped.name).toBe('restartDaemon');
  });

  it('moves the person before the tool runs, then says what the agent did', async () => {
    const order = [];
    const { deps, wrapped } = setup({
      screen: { before: ({ machineId }) => `/machines/${machineId}` },
      execute: vi.fn(async () => {
        order.push('tool');
        return { ok: true };
      }),
    });
    deps.go.mockImplementation(async () => order.push('go'));

    const result = await wrapped.execute({ machineId: 'm1' }, {});

    expect(order).toEqual(['go', 'tool']);
    expect(deps.go).toHaveBeenCalledWith('/machines/m1');
    expect(result).toEqual({ ok: true });
    expect(deps.notify).toHaveBeenCalledWith('Browser agent ran: restart daemon', null);
  });

  it('moves the person after the tool when the page is only known from its result', async () => {
    const order = [];
    const { deps, wrapped } = setup({
      screen: { after: (args, result) => `/tasks/${result.id}` },
      execute: vi.fn(async () => {
        order.push('tool');
        return { id: 't9' };
      }),
    });
    deps.go.mockImplementation(async () => order.push('go'));

    await wrapped.execute({}, {});

    expect(order).toEqual(['tool', 'go']);
    expect(deps.go).toHaveBeenCalledWith('/tasks/t9');
  });

  it('stays put when the person is already on the page, or there is no page', async () => {
    const already = setup({ screen: { before: () => '/here' }, current: true });
    await already.wrapped.execute({}, {});
    const none = setup({ screen: { after: () => null } });
    await none.wrapped.execute({}, {});

    expect(already.deps.go).not.toHaveBeenCalled();
    expect(none.deps.go).not.toHaveBeenCalled();
    expect(already.deps.notify).toHaveBeenCalledTimes(1);
  });

  it('holds the move over a draft and offers a Show link instead', async () => {
    const { deps, wrapped } = setup({ screen: { before: () => '/machines/m1' }, draft: true });

    await wrapped.execute({}, {});

    expect(deps.go).not.toHaveBeenCalled();
    expect(deps.notify).toHaveBeenCalledWith('Browser agent ran: restart daemon', { path: '/machines/m1', label: 'Show' });
  });

  it('forgets a held page on the next call', async () => {
    const { deps, wrapped } = setup({ screen: { before: () => '/machines/m1' }, draft: true });
    await wrapped.execute({}, {});
    deps.hasUnsavedInput.mockReturnValue(false);
    deps.isCurrent.mockReturnValue(true);

    await wrapped.execute({}, {});

    expect(deps.notify).toHaveBeenLastCalledWith('Browser agent ran: restart daemon', null);
  });

  it('still runs the tool when the person cannot be moved', async () => {
    const { deps, tool, wrapped } = setup({ screen: { before: () => '/x' } });
    deps.go.mockRejectedValue(new Error('a navigation guard refused the move'));

    await expect(wrapped.execute({}, {})).resolves.toEqual({ ok: true });
    expect(tool.execute).toHaveBeenCalled();
  });

  it('says nothing when the tool fails, and passes arguments it was not given as empty', async () => {
    const before = vi.fn().mockReturnValue('/x');
    const { deps, wrapped } = setup({ screen: { before }, execute: vi.fn().mockRejectedValue(new Error('tool failed')) });

    await expect(wrapped.execute(undefined, {})).rejects.toThrow('tool failed');

    expect(before).toHaveBeenCalledWith({});
    expect(deps.notify).not.toHaveBeenCalled();
  });
});
