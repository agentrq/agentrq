// Copyright 2026 Contextual, Inc. https://agentrq.com

import { describe, it, expect, vi } from 'vitest';
import { ref } from 'vue';

import {
  PREFIX,
  RESERVED_SECOND_KEYS,
  SEQUENCE_TIMEOUT_MS,
  conflicts,
  formatSequence,
  isBindableKey,
  useExtensionShortcuts,
} from '../src/composables/useExtensionShortcuts';

/**
 * One reserved prefix instead of a list of forbidden letters. A blocklist would
 * be a tiny namespace, would freeze the application out of every letter an
 * extension claimed, and would still let two extensions collide with each other.
 */

const entry = (over = {}) => ({ key: 'l', owner: 'linear', id: 'create', label: 'Create issue', ...over });

/** A bare keypress, as the dispatcher sees it. */
const press = (key, over = {}) => ({ key, metaKey: false, ctrlKey: false, altKey: false, target: null, ...over });

describe('isBindableKey', () => {
  it('takes a single letter or digit', () => {
    expect(isBindableKey('l')).toBe(true);
    expect(isBindableKey('L')).toBe(true);
    expect(isBindableKey('7')).toBe(true);
  });

  it('refuses the prefix itself', () => {
    // "x x" would mean start a sequence and then start another, which is not a
    // thing a person can press.
    expect(isBindableKey(PREFIX)).toBe(false);
    expect(RESERVED_SECOND_KEYS).toContain(PREFIX);
  });

  it('refuses anything that is not one ordinary key', () => {
    for (const key of ['', 'ab', 'Enter', '?', '-', undefined]) {
      expect(isBindableKey(key), String(key)).toBe(false);
    }
  });
});

describe('conflicts', () => {
  it('accepts a key nobody has taken', () => {
    expect(conflicts([{ key: 'l' }], [])).toEqual({ ok: true, problems: [] });
  });

  it('names the extension already using a key', () => {
    // Told at install, this is a sentence. Discovered at runtime, it is a key
    // that silently does nothing and no amount of looking explains it.
    const { ok, problems } = conflicts([{ key: 'l' }], [{ key: 'l', owner: 'standup' }]);

    expect(ok).toBe(false);
    expect(problems[0]).toBe('"x l" is already used by standup.');
  });

  it('is case-insensitive about a collision', () => {
    expect(conflicts([{ key: 'L' }], [{ key: 'l', owner: 'standup' }]).ok).toBe(false);
  });

  it('refuses the prefix with an explanation rather than a shrug', () => {
    expect(conflicts([{ key: PREFIX }], []).problems[0]).toContain('is the prefix itself');
  });

  it('refuses a key that could never be pressed as one', () => {
    expect(conflicts([{ key: 'Enter' }], []).problems[0]).toContain('not a key an extension can bind');
  });

  it('catches an extension asking for the same key twice', () => {
    expect(conflicts([{ key: 'l' }, { key: 'l' }], []).problems[0]).toContain('twice');
  });

  it('reports every problem, not just the first', () => {
    // An author fixing a manifest wants the whole list.
    const { problems } = conflicts([{ key: 'l' }, { key: 'Enter' }], [{ key: 'l', owner: 'standup' }]);

    expect(problems).toHaveLength(2);
  });

  it('has nothing to say about an extension asking for none', () => {
    expect(conflicts().ok).toBe(true);
  });

  it('copes with an entry that is not a shortcut at all', () => {
    // A manifest is written by hand, so a null in the list is a thing that
    // happens, and it should be a message rather than a crash.
    const { ok, problems } = conflicts([null, {}], []);

    expect(ok).toBe(false);
    expect(problems).toHaveLength(2);
  });
});

describe('formatSequence', () => {
  it('writes a sequence the way it is pressed', () => {
    expect(formatSequence('L')).toBe('x then l');
    expect(formatSequence()).toBe('x then ');
  });
});

describe('useExtensionShortcuts', () => {
  function build({ bindings = [entry()], clock = { t: 1000 } } = {}) {
    const onInvoke = vi.fn();
    const onPrefix = vi.fn();
    const installed = ref(bindings);
    const shortcuts = useExtensionShortcuts({
      entries: () => installed.value,
      onInvoke,
      onPrefix,
      now: () => clock.t,
    });
    return { shortcuts, onInvoke, onPrefix, clock, installed };
  }

  it('invokes on the second key of the sequence', () => {
    const { shortcuts, onInvoke } = build();

    expect(shortcuts.handle(press('x'))).toBe(true);
    expect(shortcuts.armed.value).toBe(true);
    expect(shortcuts.handle(press('l'))).toBe(true);

    expect(onInvoke).toHaveBeenCalledWith(expect.objectContaining({ owner: 'linear', id: 'create' }));
    expect(shortcuts.armed.value).toBe(false);
  });

  it('announces the prefix, so the options can be shown', () => {
    // A prefix nobody can see the options for is a prefix nobody uses twice.
    const { shortcuts, onPrefix } = build();

    shortcuts.handle(press('x'));

    expect(onPrefix).toHaveBeenCalledWith([expect.objectContaining({ sequence: 'x then l' })]);
  });

  it('leaves every other key alone', () => {
    // The application's own bare letters have to keep working.
    const { shortcuts, onInvoke } = build();

    expect(shortcuts.handle(press('n'))).toBe(false);
    expect(onInvoke).not.toHaveBeenCalled();
  });

  it('consumes a second key nobody claimed without inventing a meaning for it', () => {
    const { shortcuts, onInvoke } = build();
    shortcuts.handle(press('x'));

    expect(shortcuts.handle(press('q'))).toBe(false);
    expect(onInvoke).not.toHaveBeenCalled();
    expect(shortcuts.armed.value).toBe(false);
  });

  it('abandons a sequence on escape, which is what everybody tries', () => {
    const { shortcuts, onInvoke } = build();
    shortcuts.handle(press('x'));

    expect(shortcuts.handle(press('Escape'))).toBe(true);
    expect(shortcuts.armed.value).toBe(false);
    expect(onInvoke).not.toHaveBeenCalled();
  });

  it('lets a half-typed sequence go stale', () => {
    // A stray x must not turn the next letter, minutes later, into a command.
    const clock = { t: 1000 };
    const { shortcuts, onInvoke } = build({ clock });
    shortcuts.handle(press('x'));

    clock.t += SEQUENCE_TIMEOUT_MS + 1;

    expect(shortcuts.handle(press('l'))).toBe(false);
    expect(onInvoke).not.toHaveBeenCalled();
  });

  it('starts a fresh sequence when the stale key is the prefix again', () => {
    const clock = { t: 1000 };
    const { shortcuts, onInvoke } = build({ clock });
    shortcuts.handle(press('x'));
    clock.t += SEQUENCE_TIMEOUT_MS + 1;

    expect(shortcuts.handle(press('x'))).toBe(true);
    expect(shortcuts.handle(press('l'))).toBe(true);
    expect(onInvoke).toHaveBeenCalledOnce();
  });

  it('still fires within the timeout', () => {
    const clock = { t: 1000 };
    const { shortcuts, onInvoke } = build({ clock });
    shortcuts.handle(press('x'));

    clock.t += SEQUENCE_TIMEOUT_MS;
    shortcuts.handle(press('l'));

    expect(onInvoke).toHaveBeenCalledOnce();
  });

  it('stays out of the way while somebody is typing', () => {
    // The same rule the application's own bare letters follow.
    const { shortcuts, onInvoke } = build();
    const textarea = { tagName: 'TEXTAREA', closest: () => null };

    expect(shortcuts.handle(press('x', { target: textarea }))).toBe(false);
    expect(shortcuts.armed.value).toBe(false);
    expect(onInvoke).not.toHaveBeenCalled();
  });

  it('drops a sequence the moment focus moves into a text field', () => {
    const { shortcuts, onInvoke } = build();
    shortcuts.handle(press('x'));

    shortcuts.handle(press('l', { target: { tagName: 'INPUT', closest: () => null } }));

    expect(onInvoke).not.toHaveBeenCalled();
    expect(shortcuts.armed.value).toBe(false);
  });

  it('ignores a keystroke held with a modifier', () => {
    // A held modifier means the keystroke belongs to the browser or the system.
    const { shortcuts } = build();

    expect(shortcuts.handle(press('x', { metaKey: true }))).toBe(false);
    expect(shortcuts.handle(press('x', { ctrlKey: true }))).toBe(false);
    expect(shortcuts.handle(press('x', { altKey: true }))).toBe(false);
    expect(shortcuts.armed.value).toBe(false);
  });

  it('follows what is installed, without a reload', () => {
    const { shortcuts, installed, onInvoke } = build({ bindings: [] });

    shortcuts.handle(press('x'));
    expect(shortcuts.handle(press('l'))).toBe(false);

    installed.value = [entry()];
    shortcuts.handle(press('x'));
    shortcuts.handle(press('l'));

    expect(onInvoke).toHaveBeenCalledOnce();
  });

  it('leaves out a binding whose key could never be pressed', () => {
    const { shortcuts } = build({ bindings: [entry({ key: 'Enter' }), entry()] });

    expect(shortcuts.bindings.value.map((b) => b.key)).toEqual(['l']);
  });

  it('falls back to the id when a binding has no label', () => {
    const { shortcuts } = build({ bindings: [entry({ label: undefined })] });

    expect(shortcuts.bindings.value[0].label).toBe('create');
  });

  it('ignores an event with no key at all', () => {
    expect(build().shortcuts.handle({})).toBe(false);
    expect(build().shortcuts.handle(undefined)).toBe(false);
  });

  it('can be reset from outside, for a view that is going away', () => {
    const { shortcuts } = build();
    shortcuts.handle(press('x'));

    shortcuts.reset();

    expect(shortcuts.armed.value).toBe(false);
  });

  it('works with nothing supplied at all', () => {
    const shortcuts = useExtensionShortcuts();

    expect(shortcuts.bindings.value).toEqual([]);
    expect(shortcuts.handle(press('x'))).toBe(true);
    expect(shortcuts.handle(press('l'))).toBe(false);
  });

  it('invokes through the default handler without a caller supplying one', () => {
    // Nothing listening is not a reason to throw on the way past.
    const shortcuts = useExtensionShortcuts({ entries: () => [entry()] });

    shortcuts.handle(press('x'));

    expect(() => shortcuts.handle(press('l'))).not.toThrow();
  });
});
