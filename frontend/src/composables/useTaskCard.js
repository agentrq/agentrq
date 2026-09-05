/**
 * How a task presents itself on a board card — its colour, its label, its age.
 *
 * **Colour follows status, and nothing else.** An earlier draft promoted "this
 * is waiting on you" above the status and painted those cards yellow, which
 * meant a Not Started column came out in two colours and the board no longer
 * matched its own headings. A column and the cards under it saying different
 * things is worse than a signal being one click further away, so the tone is
 * the status and the status is the tone.
 *
 * ## Where these particular colours come from
 *
 * Sampled from the design the board was asked to match: grey `#9b9fa7` for not
 * started, blue `#60aaf3` for in progress, amber `#eeb254` for blocked, green
 * `#67d282` for done. The dark values are those pixels; the light ones step a
 * shade deeper, because a colour picked to sit on near-black does not carry on
 * white.
 *
 * This is a **board-only palette**, and it deliberately differs from the dot
 * colours the task feed and the task detail use — those still say green for
 * ongoing and red for blocked. Aligning them is a separate decision about the
 * whole product rather than something to change quietly from here.
 *
 * The tone is carried three times — the card's left edge, its ground, and the
 * chip its assignee icon sits in — which is what lets a state read from across
 * a room rather than only under inspection.
 */

/**
 * How long ago, in the fewest characters that stay honest.
 *
 * A board is scanned, so this trades precision for width: minutes up to an
 * hour, hours up to a day, then days. "Just now" rather than "0m", because a
 * zero reads as a measurement failing rather than as something recent.
 *
 * @param {string|number|Date} at
 * @param {number} [now]
 */
export function timeAgo(at, now = Date.now()) {
  // `new Date(null)` is the epoch, not an error, so a missing timestamp would
  // otherwise render as fifty-odd years rather than as nothing.
  if (at === null || at === undefined || at === '') return '';

  const then = new Date(at).getTime();
  if (!Number.isFinite(then)) return '';

  const seconds = Math.floor((now - then) / 1000);
  // A clock that disagrees with the server's is ordinary, and a task stamped a
  // few seconds in the future is not worth showing as a negative age.
  if (seconds < 60) return 'just now';

  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;

  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;

  const days = Math.floor(hours / 24);
  if (days < 365) return `${days}d`;
  return `${Math.floor(days / 365)}y`;
}

/** The tone for a task, or for a bare status string. */
export function statusTone(task) {
  const status = typeof task === 'string' ? task : task?.status;
  switch (status) {
    case 'ongoing':
      return 'active';
    case 'notstarted':
      return 'idle';
    case 'completed':
      return 'done';
    case 'rejected':
      return 'rejected';
    case 'blocked':
      return 'blocked';
    case 'cron':
      return 'scheduled';
    default:
      return 'unknown';
  }
}

/**
 * The dot, in the colours the rest of the app already uses.
 *
 * The pulse is separated out because the two places that want a dot want it
 * differently: a card is saying "this one is running", while a column heading
 * is only naming itself, and a heading that throbs at you is noise.
 */
const DOT = {
  active: 'bg-blue-500 dark:bg-[#60aaf3] shadow-[0_0_8px_rgba(96,170,243,0.4)]',
  idle: 'bg-gray-400 dark:bg-[#9b9fa7]',
  done: 'bg-green-500 dark:bg-[#67d282]',
  rejected: 'bg-red-500',
  blocked: 'bg-amber-500 dark:bg-[#eeb254] shadow-[0_0_8px_rgba(238,178,84,0.4)]',
  scheduled: 'bg-cyan-500 dark:bg-cyan-300 shadow-[0_0_8px_rgba(103,232,249,0.4)]',
  unknown: 'bg-gray-300 dark:bg-zinc-600',
};

/**
 * The card's left edge.
 *
 * A heavier, flatter statement than the dot: it is read at a glance down a
 * column rather than inspected, so it carries no glow and no pulse. Both themes
 * are given a value rather than one derived by opacity — a colour that reads as
 * "stopped" on white can read as decorative on black.
 */
const EDGE = {
  active: 'border-l-blue-500 dark:border-l-[#60aaf3]',
  idle: 'border-l-gray-400 dark:border-l-[#9b9fa7]',
  done: 'border-l-green-500 dark:border-l-[#67d282]',
  rejected: 'border-l-red-400 dark:border-l-red-500/70',
  blocked: 'border-l-amber-500 dark:border-l-[#eeb254]',
  scheduled: 'border-l-cyan-500 dark:border-l-cyan-400',
  unknown: 'border-l-gray-200 dark:border-l-zinc-700',
};

/**
 * The card's own ground.
 *
 * Dark gets a translucent wash over the surface rather than a lighter colour,
 * because a lighter card on black reads as "selected" rather than as tinted,
 * and that would fight the drag state.
 */
const SURFACE = {
  active: 'bg-blue-50 dark:bg-[#60aaf3]/[0.08]',
  idle: 'bg-white dark:bg-zinc-900',
  done: 'bg-green-50/60 dark:bg-[#67d282]/[0.06]',
  rejected: 'bg-red-50/50 dark:bg-red-500/[0.05]',
  blocked: 'bg-amber-50 dark:bg-[#eeb254]/[0.09]',
  scheduled: 'bg-cyan-50 dark:bg-cyan-500/[0.08]',
  unknown: 'bg-white dark:bg-zinc-900',
};

/**
 * The chip the assignee icon sits in.
 *
 * Tinted to the status rather than left neutral, which is what stops the icon
 * reading as a separate control bolted to the card.
 */
const CHIP = {
  active: 'bg-blue-500/20 text-blue-700 dark:text-[#b0d2f4]',
  idle: 'bg-gray-100 dark:bg-zinc-800 text-gray-500 dark:text-zinc-400',
  done: 'bg-green-500/15 text-green-700/70 dark:text-[#67d282]/80',
  rejected: 'bg-red-500/15 text-red-700/70 dark:text-red-300/70',
  blocked: 'bg-amber-500/20 text-amber-700 dark:text-[#eeb254]',
  scheduled: 'bg-cyan-500/20 text-cyan-700 dark:text-cyan-300',
  unknown: 'bg-gray-100 dark:bg-zinc-800 text-gray-500 dark:text-zinc-400',
};

/** The word for a state, for the hover and for assistive technology. */
const LABEL = {
  active: 'Running',
  idle: 'Not started',
  done: 'Done',
  rejected: 'Rejected',
  blocked: 'Blocked',
  scheduled: 'Scheduled',
  unknown: 'Unknown',
};

/**
 * Finished work is recessed in its text, not in its colour.
 *
 * The edge keeps the green this product means by completed; it is the title
 * that goes quiet, so a long Done column stops competing with work still in
 * flight without becoming a grey state of its own.
 */
const TEXT = {
  done: 'text-gray-400 dark:text-zinc-500',
  rejected: 'text-gray-400 dark:text-zinc-500',
};
const TEXT_DEFAULT = 'text-gray-700 dark:text-zinc-200';

// No fallback on these: `statusTone` is exhaustive and already answers
// `unknown` for anything it does not recognise, so a `??` here would be a
// branch that can never run. What protects them is the test that walks every
// status and asserts each one produces a class.
export function toneDotClass(task, { pulse = false } = {}) {
  const tone = statusTone(task);
  return pulse && tone === 'active' ? `${DOT[tone]} animate-pulse` : DOT[tone];
}

export function toneEdgeClass(task) {
  return EDGE[statusTone(task)];
}

export function toneSurfaceClass(task) {
  return SURFACE[statusTone(task)];
}

export function toneChipClass(task) {
  return CHIP[statusTone(task)];
}

export function toneLabel(task) {
  return LABEL[statusTone(task)];
}

export function toneTitleClass(task) {
  return TEXT[statusTone(task)] ?? TEXT_DEFAULT;
}
