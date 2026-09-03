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
 * Telemetry that belongs in the message thread, as opposed to the composer.
 *
 * Reasoning and plans are moments in a conversation and are read in sequence.
 * The context counters are not: they describe the session as a whole, and only
 * the current value means anything — so they live on the composer as a gauge
 * rather than as a message that scrolls away.
 *
 * @param {object} message
 */
export function isThreadTelemetry(message) {
  const kind = agentTelemetryKind(message);
  return kind === 'thought' || kind === 'plan';
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

/** Where a filling context window stops being routine. */
const CONTEXT_HIGH_PERCENT = 75;
const CONTEXT_CRITICAL_PERCENT = 90;

/**
 * How much attention the current context level deserves.
 *
 * A number alone does not read at a glance; the gauge changes colour so that
 * a session about to run out of room is noticeable without being read.
 *
 * @param {number|null} percent
 * @returns {'normal'|'high'|'critical'}
 */
export function usageTone(percent) {
  if (percent === null || percent === undefined) return 'normal';
  if (percent >= CONTEXT_CRITICAL_PERCENT) return 'critical';
  if (percent >= CONTEXT_HIGH_PERCENT) return 'high';
  return 'normal';
}

/**
 * The most recent context report in a thread, or null if there is none.
 *
 * Searched from the end: the counters are cumulative, so only the last one
 * describes the session as it stands.
 *
 * @param {Array<object>} messages
 * @returns {object|null}
 */
export function latestUsageMessage(messages) {
  const list = Array.isArray(messages) ? messages : [];
  for (let i = list.length - 1; i >= 0; i -= 1) {
    if (agentTelemetryKind(list[i]) === 'usage') return list[i];
  }
  return null;
}

/** Thousands-separated, so six-figure token counts stay readable. */
function formatTokens(tokens) {
  return tokens.toLocaleString('en-US');
}

/**
 * A cost with enough precision to be worth showing. Turns routinely cost
 * fractions of a cent, and "0.00 USD" says nothing.
 */
function formatCost(cost) {
  const amount =
    cost.amount !== 0 && Math.abs(cost.amount) < 0.01
      ? cost.amount.toFixed(4)
      : cost.amount.toFixed(2);
  return `${amount} ${cost.currency ?? ''}`.trim();
}

/** The lines the gauge's tooltip shows on hover. */
function usageTooltip(message, detail) {
  const lines = [];
  if (detail.used !== null && detail.size !== null) {
    const share = detail.percent === null ? '' : ` (${detail.percent}%)`;
    lines.push(`Context ${formatTokens(detail.used)} / ${formatTokens(detail.size)} tokens${share}`);
  }
  const cost = message?.metadata?.cost;
  if (cost && numberOrNull(cost.amount) !== null) {
    lines.push(`Session cost ${formatCost(cost)}`);
  }
  // Nothing structured to work from: fall back to whatever the agent rendered.
  return lines.length > 0 ? lines.join('\n') : detail.text;
}

/**
 * Everything the context ring needs, or null when nothing has been reported.
 *
 * The ring is drawn as a stroked circle whose dash gap is the unused part, so
 * the geometry is worked out here rather than in the template.
 *
 * @param {object} message  a usage telemetry message
 * @param {number} [radius] the ring's radius in the SVG's own units
 * @returns {{percent: number|null, dashArray: number, dashOffset: number,
 *            tone: string, tooltip: string}|null}
 */
export function contextGauge(message, radius = 8) {
  if (agentTelemetryKind(message) !== 'usage') return null;
  const detail = usageDetail(message);
  const circumference = 2 * Math.PI * radius;
  return {
    percent: detail.percent,
    dashArray: circumference,
    // An agent that reported no window size still gets a ring, drawn empty:
    // the tooltip carries the numbers, and hiding the ring would hide those
    // too.
    dashOffset: circumference * (1 - (detail.percent ?? 0) / 100),
    tone: usageTone(detail.percent),
    tooltip: usageTooltip(message, detail),
  };
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
