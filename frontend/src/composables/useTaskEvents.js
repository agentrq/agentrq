/**
 * Folding a task pushed over SSE into the task already on screen.
 *
 * An event payload is meant to be the whole task, and the view used to take it
 * at its word and replace what it had. But the payloads are built in several
 * places in the backend, and one built without a relation is indistinguishable
 * from one whose relation is genuinely empty — so a single publisher that
 * forgot to load the tool calls emptied the trajectory's tool lane the moment
 * any new message arrived.
 *
 * Merging instead of replacing makes that unrepresentable: a relation the
 * payload does not carry keeps the value the client already had. Nothing in
 * the product removes a message or a tool call from a task, so the only way an
 * arriving list can be shorter than the one on screen is that it was not
 * loaded.
 */

/** The task's collection-valued relations, in the shape the API sends them. */
const TASK_RELATIONS = ['messages', 'toolCalls'];

function hasEntries(value) {
  return Array.isArray(value) && value.length > 0;
}

/**
 * The task to render, given the one on screen and the one an event carried.
 *
 * Scalar fields always come from the event — status, title and the rest are
 * exactly what it is reporting. Relations come from the event too, unless it
 * carries none and the current task does.
 *
 * @param {object|null} current  the task the view is holding, if any
 * @param {object|null} incoming the task payload the event carried
 * @returns {object|null}
 */
export function mergeTaskUpdate(current, incoming) {
  if (!incoming) return current ?? null;
  // A payload for a different task replaces nothing: it describes something
  // else, and merging the two would splice one task's history onto another.
  if (!current || current.id !== incoming.id) return incoming;

  const merged = { ...incoming };
  for (const relation of TASK_RELATIONS) {
    if (!hasEntries(merged[relation]) && hasEntries(current[relation])) {
      merged[relation] = current[relation];
    }
  }
  return merged;
}
