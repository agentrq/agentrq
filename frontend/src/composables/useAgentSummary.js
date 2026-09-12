// Copyright 2026 Contextual, Inc. https://agentrq.com

/**
 * What to say about the agent attached to a workspace, beyond that it is there.
 *
 * The Overview has one short line per workspace, so this is deliberately a
 * summary rather than a panel: who is connected, and what it is running. Both
 * halves are optional and often only one is known — a client always names
 * itself on connecting, while a model is only known once the agent has
 * reported one, which an agent that offers no choice never does.
 *
 * Extracted from the view so it can be tested: the project has no
 * component-test harness, and what is worth pinning here is which of the two
 * halves appear and what a missing one does.
 */

/** Joins the halves the way the card renders them. */
const SEPARATOR = ' · ';

/**
 * The display name of the model the agent is running, or undefined.
 *
 * The agent reports an ID and, usually, a list that gives that ID a
 * human-readable name. The name is preferred because it is what the agent
 * itself calls the model; the raw ID is the fallback rather than nothing, since
 * `gpt-5-codex` still tells a reader more than a blank does.
 */
export function currentModelName(agentModels) {
  const currentId = agentModels?.currentModel;
  if (!currentId) return undefined;

  const models = Array.isArray(agentModels.models) ? agentModels.models : [];
  const match = models.find((m) => m && m.id === currentId);
  const name = typeof match?.name === 'string' ? match.name.trim() : '';
  return name || currentId;
}

/**
 * The client's own name for itself, or undefined.
 *
 * Shown verbatim wherever it is rendered. Prettifying it would mean a lookup
 * table of names this project does not own, which would be wrong for every
 * client not in it.
 */
export function agentClientName(workspace) {
  const client = workspace?.agentClient?.name;
  if (typeof client !== 'string') return undefined;
  const name = client.trim();
  return name || undefined;
}

/**
 * The two halves the card lays out, or null when there is nothing to lay out.
 *
 * Returned as parts rather than one string because the card gives each its own
 * end of a row: two short values that can each truncate on their own read
 * better in a narrow card than one long line that truncates as a whole, losing
 * whichever half happened to be last.
 *
 * null rather than empty parts when nothing is known: a connected agent that
 * has said nothing about itself is an ordinary state — every non-ACP client is
 * in it — and the card drops the whole row rather than drawing an empty one.
 */
export function agentDetails(workspace) {
  const client = agentClientName(workspace);
  const model = currentModelName(workspace?.agentModels);
  if (!client && !model) return null;
  return { client, model };
}

/**
 * Both halves on one line, for the places that have room for only one string —
 * the card's tooltip, where truncation has hidden something.
 */
export function agentSummary(workspace) {
  const details = agentDetails(workspace);
  if (!details) return '';
  return [details.client, details.model].filter(Boolean).join(SEPARATOR);
}
