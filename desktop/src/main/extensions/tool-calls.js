// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * Letting an extension answer the question the app normally puts to the user.
 *
 * When an agent wants to run something it cannot run unattended, its harness
 * sends a permission request to the workspace MCP server. The server checks the
 * stored auto-allow rules, and if none match it writes the request into the task
 * as a message and waits for a person to click Allow or Deny.
 *
 * This is the third answer: an installed extension that has been given consent
 * to review those requests, and whose verdict is sent as if — but never
 * *pretending* to be — the person's own.
 *
 * ## Everything here is built around one asymmetry
 *
 * **Refusing costs a tool call. Approving costs whatever the tool call does.**
 * Every rule below follows from that and nothing else:
 *
 * - **A single refusal settles it, immediately.** Reviewers are not voting.
 *   One extension saying no is the whole answer, and the others are not asked.
 * - **Silence is never an allow.** A reviewer that abstains, throws, hangs past
 *   its deadline, or was never consulted leaves the request exactly where it
 *   was: in front of the user. The failure mode of every bug in an extension is
 *   therefore "the user is asked", which is what would have happened anyway.
 * - **Approving needs a level of consent that refusing does not.** `deny` lets
 *   an extension block; only `decide` lets it approve. An extension granted
 *   `deny` that answers `allow` is treated as having abstained, and it is said
 *   so in the log rather than quietly downgraded.
 *
 * ## Why the desktop app, and why from the event stream
 *
 * Extensions are desktop-only trusted Node — nothing of theirs runs on the
 * server — so the review has to happen where they are. The main process already
 * holds its own SSE connection for notifications (`sse.js`), and a pending
 * permission request arrives on it as an ordinary message with
 * `metadata.type === "permission_request"`. Reading what is already there is
 * what makes this possible without inventing a second channel, and it means an
 * extension reviews exactly what the user would have seen.
 *
 * The consequence is worth stating plainly rather than discovering: **review
 * only happens while the desktop app is running and signed in.** With the app
 * closed, every request waits for a person, as it always did. This is a
 * convenience that can be absent, never a control that can be relied upon — an
 * extension that refuses something is not a policy engine, and a request nobody
 * reviewed is not a request that was approved.
 *
 * ## Racing the user is fine, and is decided by the server
 *
 * The user can click Allow while a reviewer is still thinking. Whichever verdict
 * reaches the server first wins; the second is answered with `410 Gone` for a
 * request that no longer exists, which is a normal outcome here and is not
 * reported as a failure. Nothing is retried, because the only reason a verdict
 * gets to arrive twice is that the question has already been answered.
 */

/**
 * What a grant may let an extension do when a tool call is reviewed.
 *
 * Ordered, because they escalate — and `none` is a real rung rather than the
 * absence of one. It is what the install screen starts on: an extension is asked
 * nothing until somebody says it may be.
 */
export const CONSENT = {
  none: 'none',
  deny: 'deny',
  decide: 'decide',
}

export const CONSENT_ORDER = [CONSENT.none, CONSENT.deny, CONSENT.decide]

/** How long one reviewer gets before the question goes back to the user. */
export const REVIEW_TIMEOUT_MS = 5000

/**
 * The consent in a grant, as one of the three rungs.
 *
 * Anything unrecognised — an old grant written before this existed, a file
 * somebody edited, a rung that no longer exists — reads as `none`. The default
 * for "I do not understand this" has to be the one that asks the user.
 */
export function consentIn(grant) {
  const level = String(grant?.hooks?.toolCall ?? '')
  return CONSENT_ORDER.includes(level) ? level : CONSENT.none
}

/**
 * Consent narrowed to what a manifest currently asks for.
 *
 * The same job `clampToManifest` does for tools, for the same reason: a grant
 * outlives the manifest it was written from, and a linked installation is a
 * folder somebody can edit. An extension that dropped its hook keeps no consent
 * for it, and one that asked for `deny` cannot be sitting on a `decide` consent
 * because its author widened the manifest after the fact.
 *
 * Only ever narrower. Widening is a question, and a question needs a person.
 */
export function narrowConsent(level, declared) {
  const granted = CONSENT_ORDER.indexOf(consentIn({ hooks: { toolCall: level } }))
  const asked = CONSENT_ORDER.indexOf(declaredLevel(declared))
  return CONSENT_ORDER[Math.min(granted, asked)]
}

/** What a manifest declares, as a rung. A manifest that declares nothing is `none`. */
export function declaredLevel(declared) {
  const level = String(declared ?? '')
  return CONSENT_ORDER.includes(level) && level !== CONSENT.none ? level : CONSENT.none
}

/** The rungs an install screen may offer for a manifest. */
export function availableConsents(manifest) {
  const declared = declaredLevel(manifest?.hooks?.toolCall)
  if (declared === CONSENT.none) return []
  // `none` is always offered and always first: consent to answer on somebody's
  // behalf is not a thing to arrive pre-selected, and an extension installed
  // without it is inert rather than broken — the switch is on its row.
  return declared === CONSENT.decide
    ? [CONSENT.none, CONSENT.deny, CONSENT.decide]
    : [CONSENT.none, CONSENT.deny]
}

/** Plain sentences for the rungs, so the ladder reads as an escalation. */
export function consentLabel(level) {
  if (level === CONSENT.deny) return 'It may refuse a tool call'
  if (level === CONSENT.decide) return 'It may approve or refuse a tool call'
  return 'It is not asked — you answer every time'
}

/**
 * The tool-call requests in a stream event that are still waiting on somebody.
 *
 * A `reply.received` event carries the whole task, so the same answered requests
 * arrive again and again; only the ones still marked pending are of interest,
 * and `seen` upstream keeps a request from being reviewed twice if two events
 * carry it before the first verdict lands.
 *
 * Nothing here trusts the payload's shape. It is server JSON rather than user
 * input, but a message with no metadata, a task with no id and a workspace that
 * is not there are all ordinary during a partial write, and each of them would
 * otherwise become a thrown error inside an event handler.
 */
export function pendingRequests(event) {
  const task = event?.payload
  const workspaceId = String(task?.workspaceId ?? '')
  const taskId = String(task?.id ?? '')
  if (!workspaceId || !taskId) return []

  const messages = Array.isArray(task?.messages) ? task.messages : []
  return messages.flatMap((message) => {
    const meta = message?.metadata
    if (meta?.type !== 'permission_request' || meta?.status !== 'pending') return []

    const requestId = String(meta.requestId ?? '')
    if (!requestId) return []

    return [{
      workspaceId,
      taskId,
      requestId,
      toolName: String(meta.toolName ?? ''),
      description: String(meta.description ?? ''),
      // Handed over as the server wrote it — a JSON string — rather than parsed
      // here. A reviewer deciding on `input.command` wants the real arguments,
      // and the one thing this must not do is hand out a half-parsed version
      // that silently differs from what the agent is about to run.
      inputPreview: String(meta.inputPreview ?? ''),
    }]
  })
}

/**
 * What a reviewer said, reduced to the three things it can mean.
 *
 * Deliberately strict. A reviewer returning `true`, `'yes'`, or an object with a
 * spelling mistake in it abstains rather than being interpreted, because every
 * generous reading of a malformed answer is a guess about whether somebody meant
 * to approve something.
 */
export function readVerdict(answer) {
  const behavior = String(answer?.behavior ?? answer ?? '')
  if (behavior !== 'allow' && behavior !== 'deny') return { behavior: '', reason: '' }
  return { behavior, reason: String(answer?.reason ?? '') }
}

/** A reviewer that never answers must not hold a request open forever. */
function withDeadline(promise, ms, delay) {
  let timer = null
  const timeout = new Promise((resolve) => {
    timer = delay(() => resolve({ timedOut: true }), ms)
  })
  return Promise.race([
    Promise.resolve(promise).then((value) => ({ value })),
    timeout,
  ]).finally(() => clearTimeout(timer))
}

/**
 * Ask the reviewers, in order, and stop at the first answer that settles it.
 *
 * Sequential rather than parallel, on purpose. A refusal ends the question, and
 * asking five extensions to think about a call that the first one has already
 * blocked wastes their time and the user's. It also makes "who decided this"
 * answerable without a tie-break rule nobody would be able to predict.
 *
 * @param {object[]} reviewers  `{ owner, id, review, consent }`, already filtered
 *                              to those consented to and permitted here.
 * @param {object} request      From `pendingRequests`.
 * @param {object} [deps]
 * @param {(fn: Function, ms: number) => any} [deps.delay]  `setTimeout`, injected.
 * @param {(owner: string, error: Error) => void} [deps.onError]
 * @param {(owner: string, message: string) => void} [deps.onRefused]
 */
export async function review(reviewers, request, { delay = setTimeout, onError = () => {}, onRefused = () => {}, timeoutMs = REVIEW_TIMEOUT_MS } = {}) {
  for (const reviewer of reviewers) {
    let answer
    try {
      answer = await withDeadline(reviewer.review(request), timeoutMs, delay)
    } catch (error) {
      // Counted against the extension and then stepped over. A reviewer that
      // throws has not denied anything — it has failed to answer, and the next
      // one is still entitled to be asked.
      onError(reviewer.owner, error)
      continue
    }

    if (answer.timedOut) {
      onError(reviewer.owner, new Error(`did not answer within ${timeoutMs}ms`))
      continue
    }

    const verdict = readVerdict(answer.value)
    if (!verdict.behavior) continue

    if (verdict.behavior === 'allow' && reviewer.consent !== CONSENT.decide) {
      // Said out loud rather than silently ignored. An author whose extension
      // approves things and is installed at "may refuse" would otherwise see
      // nothing happen and have no way to tell that from their own bug.
      onRefused(reviewer.owner, 'tried to approve a tool call, but it was only allowed to refuse one.')
      continue
    }

    return { behavior: verdict.behavior, by: reviewer.owner, id: reviewer.id, reason: verdict.reason }
  }

  // Nobody had an opinion. The user is asked, exactly as before.
  return { behavior: '', by: '', id: '', reason: '' }
}

/**
 * The live half: watch the stream, ask whoever may be asked, send the verdict.
 *
 * @param {object} deps
 * @param {() => object[]} deps.reviewers   Every `hooks` entry registered, with its owner.
 * @param {(name: string) => object} deps.grantFor
 * @param {(grant: object, workspaceId: string) => {ok: boolean}} deps.allows
 * @param {(request: object, verdict: object) => Promise<object>} deps.sendVerdict
 * @param {(owner: string, error: Error) => void} [deps.onFailure]
 * @param {{info: Function, warn: Function}} [deps.logger]
 */
export function createToolCallReview({
  reviewers,
  grantFor,
  allows,
  sendVerdict,
  onFailure = () => {},
  logger = console,
  delay = setTimeout,
  timeoutMs = REVIEW_TIMEOUT_MS,
}) {
  /**
   * Requests already handed to a reviewer.
   *
   * One event repeats a task's whole message list and several events arrive for
   * one reply, so without this a single request would be reviewed once per
   * event until the first verdict landed — every one of them a real call into
   * extension code, and on a slow reviewer several at once.
   *
   * Never cleared during a run. A request id is a monoflake and this is a map of
   * the ones seen while the app was open; a session that reviewed a hundred
   * thousand tool calls has other problems.
   */
  const seen = new Set()

  /**
   * Who may be asked about this request, in registry order.
   *
   * Two questions, the same shape as the broker's: has the user consented to
   * this extension answering at all, and does its grant reach the workspace the
   * request came from? An extension granted one workspace must not get an
   * opinion on another — reviewing a call is seeing the tool and its arguments,
   * which is reading from that workspace by another name.
   */
  function asked(workspaceId) {
    return reviewers()
      .filter((entry) => typeof entry.review === 'function')
      .map((entry) => ({ ...entry, consent: consentIn(grantFor(entry.owner)) }))
      .filter((entry) => entry.consent !== CONSENT.none && allows(grantFor(entry.owner), workspaceId).ok)
  }

  return {
    /** Exposed for the tests and for a status line; not part of the decision. */
    reviewed: () => seen.size,

    /**
     * Review everything pending in one stream event.
     *
     * Answers with what was decided, so a caller can log or count it. Nothing is
     * returned for a request left to the user, because "not decided" is the
     * normal case and a list of them would be a list of ordinary events.
     */
    async handle(event) {
      const decided = []

      for (const request of pendingRequests(event)) {
        if (seen.has(request.requestId)) continue

        const panel = asked(request.workspaceId)
        // Marked only once somebody is actually going to be asked. A request
        // that arrives while nothing is consented must stay reviewable: an
        // extension can be given consent from its row a moment later, and the
        // request is very likely still waiting.
        if (panel.length === 0) continue
        seen.add(request.requestId)

        const verdict = await review(panel, request, {
          delay,
          timeoutMs,
          onError: (owner, error) => {
            logger.warn?.(`[${owner}] failed to review ${request.toolName}: ${error?.message ?? error}`)
            onFailure(owner, error)
          },
          onRefused: (owner, message) => logger.warn?.(`[${owner}] ${message}`),
        })
        if (!verdict.behavior) continue

        logger.info?.(
          `[${verdict.by}] ${verdict.behavior === 'allow' ? 'approved' : 'refused'} ${request.toolName}` +
            `${verdict.reason ? `: ${verdict.reason}` : ''}`,
        )

        const sent = await sendVerdict(request, verdict)
        // A verdict the server would not take is worth a line and nothing more.
        // The overwhelmingly likely reason is that the user answered first,
        // which is not a failure of anything.
        if (!sent?.ok) logger.warn?.(`[${verdict.by}] verdict not delivered: ${sent?.reason ?? 'unknown'}`)
        decided.push({ ...verdict, requestId: request.requestId, delivered: Boolean(sent?.ok) })
      }

      return decided
    },
  }
}
