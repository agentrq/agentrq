/**
 * What a workflow's boxes say when you hover them.
 *
 * The canvas draws fixed-width nodes and the palette is a narrow column, so
 * both truncate: a workspace called "agentrq-release-engineering" reads as
 * "agentrq-release-engi…" and there was no way to see the rest. Worse, the
 * truncated thing is the one fact a reader most needs — which workspace a
 * running event is about to create work in.
 *
 * So every label that can be cut carries a tooltip, and the tooltip leads with
 * the full name, then says what the box actually is. The shared tooltip
 * preserves newlines, so these are written as short lines rather than one long
 * sentence.
 *
 * Kept out of the view so it can be tested: the project has no component-test
 * harness.
 */

/** The name a workspace goes by, or a marker when it is no longer there. */
export function workspaceLabel(workspacesById, workspaceId) {
  return workspacesById?.[workspaceId]?.name ?? '(deleted workspace)';
}

/** The name an event goes by, or a marker when it is no longer there. */
export function eventLabel(eventsById, eventId) {
  return eventsById?.[eventId]?.name ?? '(deleted event)';
}

function lines(...parts) {
  return parts.filter((part) => part).join('\n');
}

/**
 * The tooltip for a node on the workflow canvas.
 *
 * @param {object} node a graph node: {kind, label, isStart?, step?, trigger?}
 * @returns {string}
 */
export function nodeTooltip(node) {
  if (!node) return '';
  const name = node.label ?? '';
  switch (node.kind) {
    case 'event':
      return lines(name, node.isStart ? 'Event · starts this workflow' : 'Event');
    case 'global-event':
      return lines(name, 'Event · published outside this workflow');
    case 'step':
      return lines(name, taskLine(node.step), scheduleLine(node.step));
    case 'global':
      return lines(
        name,
        taskLine(node.trigger),
        scheduleLine(node.trigger),
        'Global subscriber · always runs on this event',
      );
    default:
      return name;
  }
}

/**
 * What a box in a workspace column will do when the event above it fires.
 * This is the answer to "which workspace gets the work, and what work" — the
 * node itself only has room for the first half.
 */
function taskLine(target) {
  const title = target?.title?.trim();
  return title ? `Workspace · creates "${title}"` : 'Workspace';
}

function scheduleLine(target) {
  const cron = target?.cronSchedule?.trim();
  return cron ? `Runs on schedule ${cron}` : '';
}

/**
 * The tooltip for a draggable chip in the palette. The chips are the narrowest
 * thing on the page, so this is often the only place the full name is legible.
 *
 * @param {'workspace'|'event'} kind
 * @param {string} name
 * @returns {string}
 */
export function paletteTooltip(kind, name) {
  const label = name ?? '';
  return kind === 'event'
    ? lines(label, 'Event · drag onto a workspace to emit it on completion')
    : lines(label, 'Workspace · drag onto an event to create tasks here');
}

/**
 * The tooltip for the event a step publishes when its task completes. The line
 * is rendered at nine pixels and truncates almost immediately, so the name it
 * names is usually only half there.
 *
 * @param {string} name
 * @returns {string}
 */
export function emittedEventTooltip(name) {
  return lines(name ?? '', 'Event · published when this task completes');
}

/**
 * The tooltip for the workspace a trigger fires into, on an event's page.
 *
 * @param {string} name
 * @returns {string}
 */
export function triggerWorkspaceTooltip(name) {
  return lines(name ?? '', 'Workspace · a task is created here when this event fires');
}
