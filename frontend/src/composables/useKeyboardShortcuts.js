import { onMounted, onUnmounted } from 'vue';

/**
 * The application's keyboard shortcuts.
 *
 * ## Why so few of these use a modifier
 *
 * The obvious bindings — Cmd+N for a new task, Cmd+H for history, Cmd+M for
 * messages — cannot be implemented. They never reach the page:
 *
 * - **Cmd/Ctrl+N** is *New Window* in every browser, claimed by the browser's
 *   own menu. (Cmd+Shift+N is *New Incognito Window*, so shifting it does not
 *   help either — see `desktop/src/main/menu.js`, which reaches the same
 *   conclusion for the desktop menu.)
 * - **Cmd+H** is *Hide Application* on macOS, handled by the window server
 *   before any application — browser or Electron — is offered the event.
 * - **Cmd+M** is *Minimize Window* on macOS, in the standard Window menu.
 *
 * A handler bound to those would simply never fire, which is worse than having
 * no shortcut: the app would document a key that does nothing.
 *
 * So the scheme is the one every web task tracker converges on for exactly this
 * reason — Linear, GitHub, Jira, Gmail: **Cmd/Ctrl+K for the finder, and bare
 * letters for the rest.** Cmd+K is the one modifier combination browsers leave
 * to the page, and a bare letter collides with nothing at all as long as it is
 * suppressed while the user is typing (see `isTypingTarget`).
 *
 * ## The modifier alternates, and where they are real
 *
 * The three action shortcuts also answer to **Cmd/Ctrl+Shift+key**, which is
 * what people reach for first. Those chords are bound, but they are not
 * available everywhere, and the difference is not ours to fix:
 *
 * - **In the desktop app they work.** It owns its own menu bar, so nothing
 *   above the page claims them. Cmd/Ctrl+Shift+N already opened the task form
 *   there through the application menu before this existed.
 * - **In a browser most of them never arrive.** Cmd+Shift+T reopens the last
 *   closed tab, Cmd+Shift+N opens a private window, and Cmd+Shift+M switches
 *   profile in Chrome. The browser's own menu takes those before the page is
 *   offered the event.
 *
 * So the bare letters stay, and stay the advertised binding on the web. They
 * are the ones that work in every build. The help sheet shows the chord only
 * where it can actually fire, because advertising a shortcut that silently
 * does nothing is worse than not having one.
 */
export const SHORTCUTS = [
  {
    id: 'find-task',
    key: 'k',
    mod: true,
    label: 'Find task by ID',
    // The finder is how you leave a screen you are typing on, so unlike the
    // bare letters it stays live while a text box has focus.
    hint: 'Titles from your recent tasks; a full ID reaches any task you own',
    scope: 'global',
  },
  {
    id: 'new-task',
    key: 'n',
    label: 'New task',
    hint: 'Opens the task form for the workspace you are in',
    scope: 'global',
    withModifier: true,
  },
  {
    id: 'show-help',
    key: '?',
    label: 'Show keyboard shortcuts',
    scope: 'global',
  },
  {
    // `M` for messages, not `C` for chat: `C` sits close enough to copying to
    // read as a conflict even though it is not one — a bare letter never fires
    // with a modifier held, so Cmd+C is untouched either way.
    id: 'chat-view',
    key: 'm',
    label: 'Chat view',
    hint: 'The message thread',
    scope: 'task',
    withModifier: true,
  },
  {
    id: 'trajectory-view',
    key: 't',
    label: 'Trajectory view',
    hint: "The agent's reasoning and tool calls",
    scope: 'task',
    withModifier: true,
  },
];

/**
 * Which physical key stands for "the command modifier" here.
 *
 * Two sources, because the two builds know different things. The desktop shell
 * passes `process.platform` down to the platform store, which is authoritative.
 * The browser build has no such value — the store deliberately leaves `os`
 * empty there — so it falls back to what the navigator reports.
 *
 * This is *not* the user-agent sniffing the frontend guide warns against. That
 * rule is about deciding which build is running, a question the platform store
 * answers. "Does this keyboard have a Command key" is a different question, and
 * in the browser the navigator is the only thing that can answer it.
 *
 * @param {{ os?: string }} [platform] the platform store's state
 * @param {{ userAgentData?: { platform?: string }, platform?: string }} [nav]
 * @returns {boolean} true when Command, false when Control
 */
export function usesCommandKey(platform = {}, nav = globalThis.navigator) {
  if (platform.os === 'darwin') return true;

  const reported = nav?.userAgentData?.platform || nav?.platform || '';
  return /mac/i.test(reported);
}

/**
 * Whether a key event is the user writing text rather than driving the app.
 *
 * This is the whole reason a bare letter is safe: `n` opens the task form from
 * the board, but typing "not started" into the reply box must stay text. A
 * `contenteditable` counts, and so does anything the page has explicitly opted
 * out with `data-shortcuts="off"`.
 *
 * @param {EventTarget | null} target
 */
export function isTypingTarget(target) {
  const el = /** @type {HTMLElement | null} */ (target);
  if (!el || typeof el !== 'object') return false;
  if (el.isContentEditable) return true;
  if (el.closest?.('[data-shortcuts="off"]')) return true;

  const tag = typeof el.tagName === 'string' ? el.tagName.toUpperCase() : '';
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT';
}

/**
 * The shortcut a key event stands for, or null.
 *
 * A modifier-less shortcut must be pressed with *no* modifier at all: that keeps
 * the app clear of every browser and OS binding built on Cmd, Ctrl or Alt, which
 * is what makes bare letters usable in the first place. It is also why a bare
 * letter is ignored while typing, whereas Cmd+K is not — the finder is how you
 * get out of a screen you are writing on.
 *
 * @param {KeyboardEvent} event
 * @param {{ mac?: boolean, shortcuts?: typeof SHORTCUTS }} [options]
 */
export function matchShortcut(event, { mac = false, shortcuts = SHORTCUTS } = {}) {
  if (!event?.key) return null;

  const modPressed = mac ? Boolean(event.metaKey) : Boolean(event.ctrlKey);
  const otherMod = mac ? Boolean(event.ctrlKey) : Boolean(event.metaKey);
  const typing = isTypingTarget(event.target);

  const clean = !otherMod && !event.altKey;
  const shift = Boolean(event.shiftKey);

  return (
    shortcuts.find((s) => {
      if (event.key.toLowerCase() !== s.key) return false;

      // A chord the table declares outright, like Cmd+K. Shift is pinned so
      // Cmd+Shift+K is not silently the same shortcut.
      if (s.mod) return modPressed && clean && !shift;

      // The modifier alternate: the same action reached as Cmd/Ctrl+Shift+key.
      // Held with the modifier it is unambiguous, so it fires while typing too.
      if (modPressed) return Boolean(s.withModifier) && clean && shift;

      // `?` is Shift+/ on most layouts, so Shift is judged by the resulting
      // character rather than by the flag.
      return !typing && clean;
    }) ?? null
  );
}

/**
 * How a shortcut is written on screen.
 *
 * macOS spells modifiers as glyphs with no separator, which is what users there
 * expect to see next to a menu item; everywhere else they are words joined by
 * `+`.
 *
 * @param {{ key: string, mod?: boolean }} shortcut
 * @param {{ mac?: boolean, modifier?: boolean }} [options]
 *        `modifier` renders the Cmd/Ctrl+Shift alternate rather than the key
 *        on its own.
 */
export function formatShortcut(shortcut, { mac = false, modifier = false } = {}) {
  const key = shortcut.key.toUpperCase();
  if (modifier) return mac ? `⌘⇧${key}` : `Ctrl+Shift+${key}`;
  if (!shortcut.mod) return key;
  return mac ? `⌘${key}` : `Ctrl+${key}`;
}

/**
 * The registrations a key press is dispatched to, newest first.
 *
 * A stack rather than a map so a view can take over a shortcut the shell also
 * defines: the task detail mounts after the shell, so its handler sits on top
 * and wins. Module scope is deliberate — there is one keyboard, and therefore
 * one listener, however many components are listening through it.
 *
 * @type {Array<Record<string, (event: KeyboardEvent) => void>>}
 */
const registrations = [];

/** Test seam: forget every registration. */
export function resetShortcuts() {
  registrations.length = 0;
}

/**
 * Run the handler for whatever `event` means, if anyone is listening for it.
 *
 * A matched shortcut has its default suppressed — without that, Cmd+K would
 * also drop the browser into its address bar — but only when it was actually
 * handled. An unclaimed shortcut is left alone so the browser keeps its
 * behaviour on screens that do not implement it.
 *
 * @param {KeyboardEvent} event
 * @param {{ mac?: boolean, shortcuts?: typeof SHORTCUTS }} [options]
 * @returns {string | null} the id that ran, for tests and callers
 */
export function dispatchShortcut(event, options = {}) {
  const shortcut = matchShortcut(event, options);
  if (!shortcut) return null;

  for (let i = registrations.length - 1; i >= 0; i -= 1) {
    const handler = registrations[i][shortcut.id];
    if (!handler) continue;
    event.preventDefault?.();
    handler(event);
    return shortcut.id;
  }
  return null;
}

/**
 * Listen for shortcuts for as long as the calling component is mounted.
 *
 * @param {Record<string, (event: KeyboardEvent) => void>} handlers
 *        keyed by shortcut id; ids nobody handles simply do nothing
 * @param {{ mac?: () => boolean, target?: EventTarget,
 *           onMounted?: Function, onUnmounted?: Function }} [options]
 */
export function useShortcuts(handlers, options = {}) {
  const {
    mac = () => false,
    target = globalThis.window,
    onMounted: mount = onMounted,
    onUnmounted: unmount = onUnmounted,
  } = options;

  const onKeydown = (event) => dispatchShortcut(event, { mac: mac() });

  mount(() => {
    registrations.push(handlers);
    target?.addEventListener('keydown', onKeydown);
  });

  unmount(() => {
    const at = registrations.indexOf(handlers);
    if (at !== -1) registrations.splice(at, 1);
    target?.removeEventListener('keydown', onKeydown);
  });

  return { onKeydown };
}

/**
 * Where the new-task shortcut goes.
 *
 * The task form belongs to a workspace, so the shortcut has to pick one. The
 * workspace on screen is the obvious answer; failing that, a user with a single
 * workspace can only have meant that one. With no way to tell, the workspace
 * list is the honest destination — better than guessing and dropping a task in
 * the wrong place.
 *
 * The desktop menu answers the same question from its recent-workspace list
 * (`newTaskRoute` in `desktop/src/main/index.js`); this is the browser's
 * equivalent, using what the router already knows.
 *
 * @param {string | undefined} currentWorkspaceId
 * @param {Array<{ id: string }>} [workspaces]
 */
export function newTaskRoute(currentWorkspaceId, workspaces = []) {
  if (currentWorkspaceId) return `/workspaces/${currentWorkspaceId}/tasks/new`;
  if (workspaces.length === 1) return `/workspaces/${workspaces[0].id}/tasks/new`;
  return '/';
}
