/**
 * Where a task's activity belongs: the conversation, or the trajectory.
 *
 * The chat thread is what was *said* — what a person asked for, what the agent
 * answered, and anything still waiting on a human. Everything else is how the
 * agent got there: its reasoning, and every tool it ran. That record is worth
 * keeping, but reading it inline means scrolling past a hundred resolved
 * permission cards to find the sentence that mattered, so it lives in the
 * trajectory instead — the one place where it can be filtered and searched.
 *
 * A card only leaves the thread once it has nothing left to ask of anyone: a
 * permission request stays put until it has a verdict, because it is the thing
 * the human is being asked to act on.
 *
 * The logic lives here rather than in TrajectoryPanel.vue so it can be tested:
 * the project has no component-test harness.
 */

import {
  agentTelemetryKind,
  isAgentTelemetry,
  planContent,
  planProgress,
  telemetryText,
  thoughtPreview,
} from './useAgentTelemetry';

/**
 * The trajectory's categories, in the order they are shown.
 *
 * A lane is the category an entry is filed under; an entry can still carry a
 * more specific badge of its own — a plan sits in THINKING but says PLAN, an
 * elicitation sits in TOOLS but says ASK.
 */
export const TRAJECTORY_LANES = [
  { key: 'input', label: 'INPUT' },
  { key: 'agent', label: 'AGENT' },
  { key: 'thought', label: 'THINKING' },
  { key: 'tool', label: 'TOOLS' },
];

/** A permission request that has been decided one way or the other. */
function isResolvedPermissionRequest(message) {
  if (message?.metadata?.type !== 'permission_request') return false;
  return (message.metadata.status ?? 'pending') !== 'pending';
}

/**
 * Whether a message belongs in the chat thread.
 *
 * Plans are the one piece of telemetry that stays: an agent publishes a plan
 * to tell the human what it intends to do, and revises the same card as it
 * works, so it reads as part of the conversation rather than a trace of it.
 *
 * @param {object} message
 * @returns {boolean}
 */
export function belongsInThread(message) {
  if (isAgentTelemetry(message)) return agentTelemetryKind(message) === 'plan';
  return !isResolvedPermissionRequest(message);
}

/** Collapses whitespace and clips to a single readable line. */
function truncate(text, len) {
  if (!text) return '';
  const t = String(text).trim().replace(/\s+/g, ' ');
  return t.length > len ? `${t.slice(0, len)}…` : t;
}

/** The status a verdict on a permission request reads as in the tool lane. */
function permissionStatus(status) {
  switch (status) {
    case 'allow': return 'allowed';
    case 'allow_always': return 'auto_allowed';
    case 'deny': return 'denied';
    default: return status || 'pending';
  }
}

/**
 * How a tool call is recognised as the one a permission request is asking
 * about.
 *
 * Both records are written from the same notification, so the tool and its
 * input identify the pair — but only the tool call's input is truncated for
 * storage, so the comparison is made on a prefix short enough that both sides
 * always have it.
 */
const MATCH_PREFIX = 200;
function toolIdentity(toolName, inputPreview) {
  return `${toolName} ${(inputPreview ?? '').slice(0, MATCH_PREFIX)}`;
}

/**
 * Counts each tool call by identity, so a permission request can find the row
 * that already stands for it. A count rather than a set: an agent that runs
 * the same command twice has two of everything, and each request should pair
 * with one row rather than all of them collapsing into one.
 */
function toolCallIdentities(toolCalls) {
  const counts = new Map();
  for (const call of toolCalls) {
    const key = toolIdentity(call?.toolName, call?.inputPreview);
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  return counts;
}

function byCreatedAt(a, b) {
  return new Date(a?.createdAt) - new Date(b?.createdAt);
}

/** The API sends camelCase; messages written before that have snake_case. */
function permissionToolName(metadata) {
  return metadata.toolName || metadata.tool_name || 'Permission Request';
}

function permissionInputPreview(metadata) {
  return metadata.inputPreview || metadata.input_preview;
}

/** A one-line summary of a plan: how far along it is, and what it is on. */
function planPreview(message) {
  const plan = planContent(message);
  const progress = planProgress(message);
  if (!progress) return truncate(plan.type === 'markdown' ? plan.content : telemetryText(message), 140);
  const current = plan.entries.find((entry) => entry.active)
    ?? plan.entries.find((entry) => !entry.done)
    ?? plan.entries[plan.entries.length - 1];
  return truncate(`${progress.done}/${progress.total} · ${current.content}`, 140);
}

/**
 * One message as a trajectory entry, or null if it does not belong there.
 *
 * The counters are the one thing kept out: they describe the session as a
 * whole and are shown as a gauge on the composer, so a running total of them
 * down the trajectory would say nothing the reader can use.
 */
function messageEntry(message, matchedToolCalls) {
  const kind = agentTelemetryKind(message);
  if (kind === 'usage') return null;

  const base = { id: `m-${message.id}`, createdAt: message.createdAt, raw: message };

  if (kind === 'thought') {
    return {
      ...base,
      lane: 'thought',
      laneLabel: 'THINKING',
      label: 'Thinking',
      preview: thoughtPreview(message, 140) || '(empty)',
      raw: { ...message, text: telemetryText(message) },
    };
  }
  if (kind === 'plan') {
    return {
      ...base,
      lane: 'thought',
      laneLabel: 'PLAN',
      label: 'Plan',
      preview: planPreview(message),
      raw: { ...message, text: telemetryText(message) },
    };
  }

  const metadata = message.metadata ?? {};
  if (metadata.type === 'permission_request') {
    const toolName = permissionToolName(metadata);
    const inputPreview = permissionInputPreview(metadata);
    // The tool call recorded alongside this request already stands for it, and
    // carries the verdict as it changes — so the request is only shown when
    // nothing was recorded, as on tasks that predate tool calls being kept.
    const key = toolIdentity(toolName, inputPreview);
    const unmatched = matchedToolCalls.get(key) ?? 0;
    if (unmatched > 0) {
      matchedToolCalls.set(key, unmatched - 1);
      return null;
    }
    return {
      ...base,
      lane: 'tool',
      laneLabel: 'TOOL',
      label: toolName,
      preview: truncate(`${toolName} ${inputPreview || metadata.description || ''}`, 140),
      raw: {
        ...message,
        toolName,
        description: metadata.description,
        inputPreview,
        status: permissionStatus(metadata.status),
      },
    };
  }

  if (metadata.type === 'elicitation_request') {
    const label = metadata.message || (metadata.mode === 'url' ? 'Open link' : 'Answer question');
    const payload = metadata.mode === 'form'
      ? { mode: 'form', requestedSchema: metadata.requestedSchema }
      : { mode: 'url', url: metadata.url };
    if (metadata.content) payload.answer = metadata.content;
    return {
      ...base,
      lane: 'tool',
      laneLabel: 'ASK',
      label,
      preview: truncate(label, 140),
      raw: {
        ...message,
        toolName: label,
        inputPreview: JSON.stringify(payload),
        status: metadata.status || 'pending',
      },
    };
  }

  const isAgent = message.sender === 'agent';
  const isSlack = message.sender === 'slack';
  return {
    ...base,
    lane: isAgent ? 'agent' : 'input',
    laneLabel: isAgent ? 'AGENT' : (isSlack ? 'SLACK' : 'INPUT'),
    label: isAgent ? 'Agent' : (isSlack ? 'Slack' : 'You'),
    preview: truncate(message.text, 140) || '(no text)',
  };
}

function toolCallEntry(call) {
  return {
    id: `t-${call.id}`,
    lane: 'tool',
    laneLabel: 'TOOL',
    createdAt: call.createdAt,
    label: call.toolName,
    preview: truncate(`${call.toolName} ${call.inputPreview || call.description || ''}`, 140),
    raw: call,
  };
}

/**
 * Everything that happened in a task, oldest first.
 *
 * @param {Array<object>} messages
 * @param {Array<object>} toolCalls
 * @returns {Array<object>} entries carrying {id, lane, laneLabel, label, preview, createdAt, raw}
 */
export function buildTrajectory(messages, toolCalls) {
  const calls = Array.isArray(toolCalls) ? [...toolCalls].sort(byCreatedAt) : [];
  const unmatched = toolCallIdentities(calls);
  const entries = Array.isArray(messages) ? [...messages].sort(byCreatedAt) : [];

  const fromMessages = [];
  for (const message of entries) {
    const entry = messageEntry(message, unmatched);
    if (entry) fromMessages.push(entry);
  }
  return [...fromMessages, ...calls.map(toolCallEntry)].sort(byCreatedAt);
}

/**
 * The tab an entry opens on.
 *
 * A tool's summary is the interesting part — what ran, and whether it was
 * allowed. Reasoning and plans are the opposite: the summary knows only when
 * they were written, and what the reader came for is the text itself.
 *
 * @param {object|null} item
 * @returns {'summary'|'content'}
 */
export function defaultDetailTab(item) {
  return item?.lane === 'thought' ? 'content' : 'summary';
}

/**
 * How many entries each lane holds, so the filter can say what it would show
 * before it is clicked.
 *
 * @param {Array<object>} items
 * @returns {Record<string, number>}
 */
export function trajectoryLaneCounts(items) {
  const counts = Object.fromEntries(TRAJECTORY_LANES.map((lane) => [lane.key, 0]));
  for (const item of items ?? []) {
    if (item?.lane in counts) counts[item.lane] += 1;
  }
  return counts;
}

/**
 * The entries a lane filter and a search leave visible.
 *
 * @param {Array<object>} items
 * @param {{lane?: string|null, query?: string}} [filter] a null lane is every lane
 * @returns {Array<object>}
 */
export function filterTrajectory(items, { lane = null, query = '' } = {}) {
  const needle = query.trim().toLowerCase();
  return (items ?? []).filter((item) => {
    if (lane && item.lane !== lane) return false;
    if (!needle) return true;
    return [item.label, item.preview, item.raw?.description]
      .some((field) => field?.toLowerCase().includes(needle));
  });
}
