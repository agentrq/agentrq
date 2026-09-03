/**
 * Reading the telemetry an agent streams alongside its answer: the reasoning
 * behind it, the plan it is working to, and what the turn is costing.
 *
 * These arrive as ordinary agent messages carrying a metadata type. The
 * metadata is the source of truth, not the message body: a plan card and a
 * usage line are revised in place as the turn goes on, and only the metadata
 * is rewritten — the body keeps whatever it was first written with.
 *
 * Extracted from TaskDetailView so it can be tested: the view is a large
 * component wired to a router, a store and network calls, and the project has
 * no component-test harness.
 */

/** The message metadata types the backend writes agent telemetry as. */
export const AGENT_TELEMETRY_MESSAGE_TYPES = {
  agent_thought: 'thought',
  agent_plan: 'plan',
  agent_usage: 'usage',
};

/**
 * Which kind of telemetry a message is, or null if it is an ordinary message.
 *
 * @param {object} message
 * @returns {'thought'|'plan'|'usage'|null}
 */
export function agentTelemetryKind(message) {
  return AGENT_TELEMETRY_MESSAGE_TYPES[message?.metadata?.type] ?? null;
}

/** @param {object} message */
export function isAgentTelemetry(message) {
  return agentTelemetryKind(message) !== null;
}

/**
 * The current rendering of a piece of telemetry.
 *
 * Prefers the metadata, which is rewritten on every revision, and falls back
 * to the message body for anything written before the metadata carried it.
 *
 * @param {object} message
 * @returns {string}
 */
export function telemetryText(message) {
  const fromMetadata = message?.metadata?.text;
  if (typeof fromMetadata === 'string' && fromMetadata !== '') return fromMetadata;
  return message?.text ?? '';
}

/** Whether the agent has withdrawn the plan this message stands for. */
export function planIsWithdrawn(message) {
  return message?.metadata?.removed === true;
}

/**
 * A plan in the form the card renders.
 *
 * Agents publish plans three ways — as entries, as a markdown document, or as
 * a file they are keeping the plan in — so the card has to know which it has.
 *
 * @param {object} message
 * @returns {{type: 'items', entries: Array<object>} |
 *           {type: 'markdown', content: string} |
 *           {type: 'file', uri: string} |
 *           {type: 'text', text: string}}
 */
export function planContent(message) {
  const metadata = message?.metadata ?? {};
  if (Array.isArray(metadata.entries)) {
    return { type: 'items', entries: metadata.entries.map(normalizePlanEntry) };
  }
  if (metadata.planType === 'markdown') {
    return { type: 'markdown', content: metadata.content ?? '' };
  }
  if (metadata.planType === 'file') {
    return { type: 'file', uri: metadata.uri ?? '' };
  }
  return { type: 'text', text: telemetryText(message) };
}

/** Fills in what an entry does not say, so the card never renders a blank row. */
function normalizePlanEntry(entry) {
  const status = entry?.status ?? 'pending';
  return {
    content: entry?.content ?? '',
    priority: entry?.priority ?? 'medium',
    status,
    done: status === 'completed',
    active: status === 'in_progress',
  };
}

/**
 * How far along an entry-based plan is, or null when there is nothing to
 * count — a markdown plan, or one with no entries yet.
 *
 * @param {object} message
 * @returns {{done: number, total: number}|null}
 */
export function planProgress(message) {
  const plan = planContent(message);
  if (plan.type !== 'items' || plan.entries.length === 0) return null;
  return {
    done: plan.entries.filter((entry) => entry.done).length,
    total: plan.entries.length,
  };
}

/**
 * The context and cost counters, with the percentage worked out when the
 * agent did not send one.
 *
 * @param {object} message
 * @returns {{used: number|null, size: number|null, percent: number|null, text: string}}
 */
export function usageDetail(message) {
  const metadata = message?.metadata ?? {};
  const used = numberOrNull(metadata.used);
  const size = numberOrNull(metadata.size);
  let percent = numberOrNull(metadata.percent);
  if (percent === null && used !== null && size !== null && size > 0) {
    percent = Math.round((used / size) * 100);
  }
  return {
    used,
    size,
    // A bar cannot be drawn outside its track, and an agent over its own
    // reported window would otherwise overflow the card.
    percent: percent === null ? null : Math.min(Math.max(percent, 0), 100),
    text: telemetryText(message),
  };
}

function numberOrNull(value) {
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}

/**
 * A one-line summary of a reasoning block, for the collapsed card.
 *
 * @param {object} message
 * @param {number} [maxLength]
 * @returns {string}
 */
export function thoughtPreview(message, maxLength = 90) {
  const firstLine = telemetryText(message)
    .split('\n')
    .map((line) => line.trim())
    .find((line) => line !== '');
  if (!firstLine) return '';
  const collapsed = firstLine.replace(/\s+/g, ' ');
  if (collapsed.length <= maxLength) return collapsed;
  return `${collapsed.slice(0, maxLength - 1).trimEnd()}…`;
}
