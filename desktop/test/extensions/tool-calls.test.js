// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, expect, it, vi } from 'vitest'
import {
  CONSENT,
  availableConsents,
  consentIn,
  consentLabel,
  createToolCallReview,
  declaredLevel,
  narrowConsent,
  pendingRequests,
  readVerdict,
  review,
} from '../../src/main/extensions/tool-calls.js'

/** One pending permission request, as the event stream delivers it. */
function streamEvent({
  workspaceId = 'ws1',
  taskId = 't1',
  requestId = 'req-1',
  status = 'pending',
  toolName = 'Bash',
  inputPreview = '{"command":"rm -rf /"}',
  messages,
} = {}) {
  return {
    type: 'reply.received',
    payload: {
      workspaceId,
      id: taskId,
      messages: messages ?? [
        { id: 'm1', text: 'working on it' },
        {
          id: 'm2',
          metadata: {
            type: 'permission_request',
            requestId,
            toolName,
            description: 'delete everything',
            inputPreview,
            status,
          },
        },
      ],
    },
  }
}

const grant = (level, workspaces = ['ws1']) => ({
  scope: 'selected',
  workspaces,
  tools: { workspace: [], supervisor: [] },
  hooks: { toolCall: level },
})

describe('consentIn', () => {
  it('reads the three rungs', () => {
    expect(consentIn(grant('none'))).toBe(CONSENT.none)
    expect(consentIn(grant('deny'))).toBe(CONSENT.deny)
    expect(consentIn(grant('decide'))).toBe(CONSENT.decide)
  })

  /**
   * Everything unrecognised has to land on the rung that asks the user. A grant
   * written before this existed carries no `hooks` at all, and a grants file is
   * a file somebody can edit — neither is a reason to answer on their behalf.
   */
  it('reads anything it does not understand as consenting to nothing', () => {
    expect(consentIn(null)).toBe(CONSENT.none)
    expect(consentIn({})).toBe(CONSENT.none)
    expect(consentIn({ hooks: {} })).toBe(CONSENT.none)
    expect(consentIn({ hooks: { toolCall: 'everything' } })).toBe(CONSENT.none)
    expect(consentIn({ hooks: { toolCall: true } })).toBe(CONSENT.none)
  })
})

describe('narrowConsent', () => {
  it('keeps a consent the manifest still asks for', () => {
    expect(narrowConsent('deny', 'deny')).toBe(CONSENT.deny)
    expect(narrowConsent('decide', 'decide')).toBe(CONSENT.decide)
  })

  /**
   * The drift this exists for: an extension installed as one that may only
   * refuse, whose author later widened the manifest to "decide". A linked
   * installation is a folder on disk, so that can happen between two launches
   * with nobody reading anything.
   */
  it('will not let a widened manifest widen a consent', () => {
    expect(narrowConsent('decide', 'deny')).toBe(CONSENT.deny)
  })

  it('takes the consent away entirely when the hook is gone', () => {
    expect(narrowConsent('decide', undefined)).toBe(CONSENT.none)
    expect(narrowConsent('deny', '')).toBe(CONSENT.none)
  })

  it('never raises a narrow consent to meet a broad manifest', () => {
    expect(narrowConsent('none', 'decide')).toBe(CONSENT.none)
    expect(narrowConsent('deny', 'decide')).toBe(CONSENT.deny)
  })
})

describe('declaredLevel', () => {
  it('reads what a manifest asks for, and nothing else', () => {
    expect(declaredLevel('deny')).toBe(CONSENT.deny)
    expect(declaredLevel('decide')).toBe(CONSENT.decide)
    expect(declaredLevel('none')).toBe(CONSENT.none)
    expect(declaredLevel(undefined)).toBe(CONSENT.none)
  })
})

describe('availableConsents', () => {
  it('offers nothing for an extension that reviews nothing', () => {
    expect(availableConsents({})).toEqual([])
    expect(availableConsents({ hooks: {} })).toEqual([])
    expect(availableConsents(null)).toEqual([])
  })

  /**
   * The rung above what was asked for is never offered. An extension that only
   * means to block things cannot be handed the power to approve one because
   * somebody clicked the wrong radio button.
   */
  it('stops at the rung the manifest asked for', () => {
    expect(availableConsents({ hooks: { toolCall: 'deny' } })).toEqual([CONSENT.none, CONSENT.deny])
  })

  it('offers the whole ladder to one that asks to decide', () => {
    expect(availableConsents({ hooks: { toolCall: 'decide' } })).toEqual([
      CONSENT.none,
      CONSENT.deny,
      CONSENT.decide,
    ])
  })

  /** `none` comes first because that is where the screen starts. */
  it('always offers being asked nothing, first', () => {
    expect(availableConsents({ hooks: { toolCall: 'decide' } })[0]).toBe(CONSENT.none)
  })
})

describe('consentLabel', () => {
  it('says what each rung means in a sentence', () => {
    expect(consentLabel(CONSENT.deny)).toContain('refuse')
    expect(consentLabel(CONSENT.decide)).toContain('approve')
    expect(consentLabel(CONSENT.none)).toContain('you answer')
  })
})

describe('pendingRequests', () => {
  it('finds a request still waiting on somebody', () => {
    expect(pendingRequests(streamEvent())).toEqual([
      {
        workspaceId: 'ws1',
        taskId: 't1',
        requestId: 'req-1',
        toolName: 'Bash',
        description: 'delete everything',
        inputPreview: '{"command":"rm -rf /"}',
      },
    ])
  })

  /**
   * One event carries the task's whole message list, so every answered request
   * arrives again on the next reply. Reviewing those would mean asking an
   * extension to decide something that was decided days ago.
   */
  it('ignores a request that has already been answered', () => {
    expect(pendingRequests(streamEvent({ status: 'allow' }))).toEqual([])
    expect(pendingRequests(streamEvent({ status: 'deny' }))).toEqual([])
  })

  it('ignores messages that are not permission requests', () => {
    expect(pendingRequests(streamEvent({ messages: [{ id: 'm1', text: 'hello' }] }))).toEqual([])
    expect(
      pendingRequests(streamEvent({ messages: [{ metadata: { type: 'agent_telemetry' } }] })),
    ).toEqual([])
  })

  /**
   * Server JSON rather than user input, but a partial payload is ordinary and
   * every one of these was a thrown error inside a stream handler before it was
   * a returned empty list.
   */
  it('survives a payload with nothing in it', () => {
    expect(pendingRequests(undefined)).toEqual([])
    expect(pendingRequests({})).toEqual([])
    expect(pendingRequests({ payload: {} })).toEqual([])
    expect(pendingRequests({ payload: { workspaceId: 'ws1' } })).toEqual([])
    expect(pendingRequests({ payload: { workspaceId: 'ws1', id: 't1' } })).toEqual([])
    expect(pendingRequests({ payload: { workspaceId: 'ws1', id: 't1', messages: 'no' } })).toEqual([])
    expect(pendingRequests({ payload: { workspaceId: 'ws1', id: 't1', messages: [null] } })).toEqual([])
  })

  /** A request with no id cannot be answered, so there is nothing to ask about. */
  it('ignores a request that names no request', () => {
    expect(
      pendingRequests(
        streamEvent({ messages: [{ metadata: { type: 'permission_request', status: 'pending' } }] }),
      ),
    ).toEqual([])
  })

  /** The fields are read defensively, because a card can be missing any of them. */
  it('fills in what a sparse request does not say', () => {
    const [request] = pendingRequests(
      streamEvent({
        messages: [{ metadata: { type: 'permission_request', status: 'pending', requestId: 'r' } }],
      }),
    )
    expect(request).toEqual({
      workspaceId: 'ws1',
      taskId: 't1',
      requestId: 'r',
      toolName: '',
      description: '',
      inputPreview: '',
    })
  })
})

describe('readVerdict', () => {
  it('reads the two answers that settle a request', () => {
    expect(readVerdict('allow')).toEqual({ behavior: 'allow', reason: '' })
    expect(readVerdict({ behavior: 'deny', reason: 'destructive' })).toEqual({
      behavior: 'deny',
      reason: 'destructive',
    })
  })

  /**
   * Strict on purpose. Every generous reading of a malformed answer is a guess
   * about whether somebody meant to approve something.
   */
  it('treats anything else as having no opinion', () => {
    expect(readVerdict(undefined).behavior).toBe('')
    expect(readVerdict(null).behavior).toBe('')
    expect(readVerdict(true).behavior).toBe('')
    expect(readVerdict('yes').behavior).toBe('')
    expect(readVerdict({ behaviour: 'deny' }).behavior).toBe('')
    expect(readVerdict({}).behavior).toBe('')
  })
})

const reviewer = (owner, review, consent = CONSENT.decide, id = 'r') => ({ owner, id, review, consent })
const request = { workspaceId: 'ws1', taskId: 't1', requestId: 'req-1', toolName: 'Bash' }

describe('review', () => {
  it('takes the first answer that settles the question', async () => {
    const verdict = await review(
      [
        reviewer('a', () => undefined),
        reviewer('b', () => ({ behavior: 'deny', reason: 'no' })),
      ],
      request,
    )

    expect(verdict).toEqual({ behavior: 'deny', by: 'b', id: 'r', reason: 'no' })
  })

  /** A refusal ends the question; nobody after it is asked. */
  it('stops asking once something has refused', async () => {
    const later = vi.fn()
    await review([reviewer('a', () => 'deny'), reviewer('b', later)], request)

    expect(later).not.toHaveBeenCalled()
  })

  it('leaves it to the user when nobody has an opinion', async () => {
    const verdict = await review([reviewer('a', () => undefined), reviewer('b', () => null)], request)

    expect(verdict.behavior).toBe('')
    expect(verdict.by).toBe('')
  })

  it('hands the whole request to the reviewer', async () => {
    const seen = vi.fn()
    await review([reviewer('a', seen)], request)

    expect(seen).toHaveBeenCalledWith(request)
  })

  /**
   * A throwing reviewer has not denied anything — it has failed to answer, and
   * the next one is still entitled to be asked.
   */
  it('steps over a reviewer that throws, and counts it', async () => {
    const onError = vi.fn()
    const verdict = await review(
      [
        reviewer('a', () => {
          throw new Error('boom')
        }),
        reviewer('b', () => 'deny'),
      ],
      request,
      { onError },
    )

    expect(verdict.by).toBe('b')
    expect(onError).toHaveBeenCalledWith('a', expect.objectContaining({ message: 'boom' }))
  })

  it('steps over one that rejects', async () => {
    const onError = vi.fn()
    const verdict = await review([reviewer('a', () => Promise.reject(new Error('nope')))], request, {
      onError,
    })

    expect(verdict.behavior).toBe('')
    expect(onError).toHaveBeenCalledWith('a', expect.objectContaining({ message: 'nope' }))
  })

  /** A reviewer that never answers must not hold a request open forever. */
  it('gives up on one that does not answer in time', async () => {
    const onError = vi.fn()
    const verdict = await review([reviewer('a', () => new Promise(() => {})), reviewer('b', () => 'deny')], request, {
      onError,
      timeoutMs: 10,
      delay: (fn) => setTimeout(fn, 0),
    })

    expect(verdict.by).toBe('b')
    expect(onError).toHaveBeenCalledWith('a', expect.objectContaining({ message: expect.stringContaining('10ms') }))
  })

  /**
   * The asymmetry the whole feature is built around: an extension consented to
   * refuse things cannot approve one, and is told rather than downgraded.
   */
  it('will not let a deny-only reviewer approve anything', async () => {
    const onRefused = vi.fn()
    const verdict = await review([reviewer('a', () => 'allow', CONSENT.deny)], request, { onRefused })

    expect(verdict.behavior).toBe('')
    expect(onRefused).toHaveBeenCalledWith('a', expect.stringContaining('only allowed to refuse'))
  })

  it('still lets a deny-only reviewer refuse', async () => {
    const verdict = await review([reviewer('a', () => 'deny', CONSENT.deny)], request)

    expect(verdict).toEqual({ behavior: 'deny', by: 'a', id: 'r', reason: '' })
  })

  it('lets a reviewer consented to decide approve one', async () => {
    const verdict = await review([reviewer('a', () => 'allow', CONSENT.decide)], request)

    expect(verdict.behavior).toBe('allow')
  })

  it('asks nobody when there is nobody to ask', async () => {
    expect(await review([], request)).toEqual({ behavior: '', by: '', id: '', reason: '' })
  })

  /**
   * Called with nothing but the reviewers, which is how the pure function reads
   * on its own: a throw, a refused approval and a deadline all have to be
   * survivable without a caller having wired up a handler for each.
   */
  it('needs nothing but the reviewers to survive all three failures', async () => {
    const thrown = await review([reviewer('a', () => { throw new Error('boom') })], request)
    expect(thrown.behavior).toBe('')

    const overreach = await review([reviewer('a', () => 'allow', CONSENT.deny)], request)
    expect(overreach.behavior).toBe('')

    // Its own timer rather than an injected one, so the default `setTimeout` is
    // the thing being exercised.
    const late = await review([reviewer('a', () => new Promise(() => {}))], request, { timeoutMs: 1 })
    expect(late.behavior).toBe('')
  })
})

/** A dispatcher with one consented reviewer, and the calls it made. */
function build({
  reviewers = [],
  grants = { guardrail: grant(CONSENT.decide) },
  sendVerdict = vi.fn(async () => ({ ok: true })),
  ...rest
} = {}) {
  const logger = { info: vi.fn(), warn: vi.fn() }
  const onFailure = vi.fn()

  const dispatcher = createToolCallReview({
    reviewers: () => reviewers,
    grantFor: (name) => grants[name] ?? null,
    allows: (held, workspaceId) =>
      held?.workspaces?.includes(workspaceId) ? { ok: true } : { ok: false, reason: 'not that one' },
    sendVerdict,
    onFailure,
    logger,
    ...rest,
  })

  return { dispatcher, sendVerdict, logger, onFailure }
}

describe('createToolCallReview', () => {
  it('sends the verdict a reviewer reached', async () => {
    const { dispatcher, sendVerdict } = build({
      reviewers: [{ owner: 'guardrail', id: 'shell', review: () => ({ behavior: 'deny', reason: 'destructive' }) }],
    })

    const decided = await dispatcher.handle(streamEvent())

    expect(decided).toEqual([
      expect.objectContaining({ behavior: 'deny', by: 'guardrail', requestId: 'req-1', delivered: true }),
    ])
    expect(sendVerdict).toHaveBeenCalledWith(
      expect.objectContaining({ workspaceId: 'ws1', taskId: 't1', requestId: 'req-1' }),
      expect.objectContaining({ behavior: 'deny', by: 'guardrail' }),
    )
  })

  it('sends nothing when the request is left to the user', async () => {
    const { dispatcher, sendVerdict } = build({
      reviewers: [{ owner: 'guardrail', id: 'shell', review: () => undefined }],
    })

    expect(await dispatcher.handle(streamEvent())).toEqual([])
    expect(sendVerdict).not.toHaveBeenCalled()
  })

  /**
   * One reply produces several events and each carries the task's whole message
   * list, so without this a single request would be handed to extension code
   * once per event until the first verdict landed.
   */
  it('reviews one request once, however many times it arrives', async () => {
    const review = vi.fn(() => 'deny')
    const { dispatcher } = build({ reviewers: [{ owner: 'guardrail', id: 'shell', review }] })

    await dispatcher.handle(streamEvent())
    await dispatcher.handle(streamEvent())
    await dispatcher.handle(streamEvent())

    expect(review).toHaveBeenCalledTimes(1)
    expect(dispatcher.reviewed()).toBe(1)
  })

  /**
   * The opposite of the rule above, and the reason it is written where it is: a
   * request that arrives while nothing is consented must stay reviewable,
   * because consent can be given from the extension's row a moment later and the
   * request is very likely still waiting.
   */
  it('does not use up a request nobody was asked about', async () => {
    const review = vi.fn(() => 'deny')
    const entry = { owner: 'guardrail', id: 'shell', review }
    const grants = { guardrail: grant(CONSENT.none) }
    const { dispatcher } = build({ reviewers: [entry], grants })

    await dispatcher.handle(streamEvent())
    expect(review).not.toHaveBeenCalled()

    grants.guardrail = grant(CONSENT.decide)
    await dispatcher.handle(streamEvent())

    expect(review).toHaveBeenCalledTimes(1)
  })

  it('asks nobody without consent', async () => {
    const review = vi.fn(() => 'deny')
    const { dispatcher, sendVerdict } = build({
      reviewers: [{ owner: 'guardrail', id: 'shell', review }],
      grants: { guardrail: grant(CONSENT.none) },
    })

    await dispatcher.handle(streamEvent())

    expect(review).not.toHaveBeenCalled()
    expect(sendVerdict).not.toHaveBeenCalled()
  })

  /**
   * Reviewing a call means seeing the tool and its arguments, which is reading
   * from that workspace by another name. An extension granted one workspace
   * gets no opinion on another.
   */
  it('asks nobody about a workspace they were not granted', async () => {
    const review = vi.fn(() => 'deny')
    const { dispatcher } = build({
      reviewers: [{ owner: 'guardrail', id: 'shell', review }],
      grants: { guardrail: grant(CONSENT.decide, ['ws9']) },
    })

    await dispatcher.handle(streamEvent({ workspaceId: 'ws1' }))

    expect(review).not.toHaveBeenCalled()
  })

  it('ignores an entry that registered no reviewer', async () => {
    const { dispatcher, sendVerdict } = build({
      reviewers: [{ owner: 'guardrail', id: 'shell' }, { owner: 'guardrail', id: 'other', review: 'no' }],
    })

    await dispatcher.handle(streamEvent())

    expect(sendVerdict).not.toHaveBeenCalled()
  })

  /**
   * Losing a race with the user is the ordinary outcome, not a failure: they
   * clicked Allow while the reviewer was thinking, the server answered 410, and
   * the question has been settled by the person it was asked of.
   */
  it('reports a verdict the server would not take, and carries on', async () => {
    const { dispatcher, logger } = build({
      reviewers: [{ owner: 'guardrail', id: 'shell', review: () => 'deny' }],
      sendVerdict: async () => ({ ok: false, reason: 'already answered' }),
    })

    const decided = await dispatcher.handle(streamEvent())

    expect(decided[0].delivered).toBe(false)
    expect(logger.warn).toHaveBeenCalledWith(expect.stringContaining('already answered'))
  })

  it('says something even when the send answers nothing at all', async () => {
    const { dispatcher, logger } = build({
      reviewers: [{ owner: 'guardrail', id: 'shell', review: () => 'deny' }],
      sendVerdict: async () => undefined,
    })

    const decided = await dispatcher.handle(streamEvent())

    expect(decided[0].delivered).toBe(false)
    expect(logger.warn).toHaveBeenCalledWith(expect.stringContaining('unknown'))
  })

  /** A reviewer that throws on every request is as broken as one that throws on start. */
  it('counts a failing reviewer against its extension', async () => {
    const { dispatcher, onFailure } = build({
      reviewers: [
        {
          owner: 'guardrail',
          id: 'shell',
          review: () => {
            throw new Error('boom')
          },
        },
      ],
    })

    await dispatcher.handle(streamEvent())

    expect(onFailure).toHaveBeenCalledWith('guardrail', expect.objectContaining({ message: 'boom' }))
  })

  /** Not everything thrown is an Error, and the log line still has to read. */
  it('logs a reviewer that throws something with no message', async () => {
    const { dispatcher, logger } = build({
      reviewers: [
        {
          owner: 'guardrail',
          id: 'shell',
          review: () => {
            throw 'no idea'
          },
        },
      ],
    })

    await dispatcher.handle(streamEvent())

    expect(logger.warn).toHaveBeenCalledWith(expect.stringContaining('no idea'))
  })

  it('says when a reviewer tried to approve something it may only refuse', async () => {
    const { dispatcher, logger, sendVerdict } = build({
      reviewers: [{ owner: 'guardrail', id: 'shell', review: () => 'allow' }],
      grants: { guardrail: grant(CONSENT.deny) },
    })

    await dispatcher.handle(streamEvent())

    expect(sendVerdict).not.toHaveBeenCalled()
    expect(logger.warn).toHaveBeenCalledWith(expect.stringContaining('only allowed to refuse'))
  })

  it('logs what was decided, with the reason when there is one', async () => {
    const { dispatcher, logger } = build({
      reviewers: [{ owner: 'guardrail', id: 'shell', review: () => ({ behavior: 'allow' }) }],
    })

    await dispatcher.handle(streamEvent())

    expect(logger.info).toHaveBeenCalledWith(expect.stringContaining('approved Bash'))
  })

  it('reviews every pending request in one event', async () => {
    const { dispatcher, sendVerdict } = build({
      reviewers: [{ owner: 'guardrail', id: 'shell', review: () => 'deny' }],
    })

    await dispatcher.handle(
      streamEvent({
        messages: [
          { metadata: { type: 'permission_request', status: 'pending', requestId: 'a', toolName: 'Bash' } },
          { metadata: { type: 'permission_request', status: 'pending', requestId: 'b', toolName: 'Write' } },
        ],
      }),
    )

    expect(sendVerdict).toHaveBeenCalledTimes(2)
  })

  /**
   * Built with only the four things it cannot work without. The rest have
   * defaults so that a missing wire is a quieter log rather than a TypeError
   * thrown from inside an event handler on a stream that has to keep reading.
   */
  it('runs on its own defaults when nothing optional is wired in', async () => {
    const info = vi.spyOn(console, 'info').mockImplementation(() => {})
    const sendVerdict = vi.fn(async () => ({ ok: true }))
    const dispatcher = createToolCallReview({
      reviewers: () => [
        { owner: 'guardrail', id: 'thrower', review: () => { throw new Error('boom') } },
        { owner: 'guardrail', id: 'shell', review: () => 'deny' },
      ],
      grantFor: () => grant(CONSENT.decide),
      allows: () => ({ ok: true }),
      sendVerdict,
    })

    const decided = await dispatcher.handle(streamEvent())

    expect(decided[0]).toMatchObject({ behavior: 'deny', by: 'guardrail' })
    expect(info).toHaveBeenCalledWith(expect.stringContaining('refused Bash'))
    info.mockRestore()
  })

  it('does nothing with an event carrying no request', async () => {
    const { dispatcher, sendVerdict } = build({
      reviewers: [{ owner: 'guardrail', id: 'shell', review: () => 'deny' }],
    })

    expect(await dispatcher.handle({ type: 'task.created', payload: {} })).toEqual([])
    expect(sendVerdict).not.toHaveBeenCalled()
  })
})
