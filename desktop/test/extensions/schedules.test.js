import { describe, it, expect, vi } from 'vitest'

import {
  KINDS,
  createSchedules,
  fingerprint,
  idFrom,
  looksSchedulable,
  planFor,
  validateSchedule,
} from '../../src/main/extensions/schedules.js'

/**
 * The reconciler is what makes desktop-only extensions viable rather than a
 * compromise: an extension is not running at 3am, but what it *configured* is.
 *
 * So the tests that matter here are the ones about the second and third pass —
 * reinstalling must not leave duplicates, and uninstalling must take away what
 * was created. A test suite that only proves "create works" would pass against
 * a fire-and-forget implementation, which is exactly the thing this replaces.
 */

const taskEntry = (over = {}) => ({
  id: 'nightly',
  owner: 'standup',
  value: {
    kind: KINDS.task,
    workspaceId: 'ws1',
    title: 'Nightly digest',
    body: 'what happened today',
    cron: '0 9 * * *',
    ...over,
  },
})

const triggerEntry = (over = {}) => ({
  id: 'on-deploy',
  owner: 'standup',
  value: {
    kind: KINDS.trigger,
    workspaceId: 'ws1',
    title: 'Write release notes',
    event: 'deploy_done',
    ...over,
  },
})

/**
 * A supervisor that behaves like the real one: answers are the JSON envelope the
 * REST API sends, carried as text inside the SDK's content array, because that
 * is the shape the broker hands back and unwrapping it is one of the things
 * being tested.
 */
function fakeSupervisor(over = {}) {
  const state = { events: [], triggers: {}, tasks: {} }
  let next = 1
  const id = (prefix) => `${prefix}${next++}`

  const wrap = (payload) => ({ ok: true, result: { content: [{ text: JSON.stringify(payload) }] } })

  const handlers = {
    listEvents: async () => wrap({ events: state.events }),
    createEvent: async ({ name }) => {
      const event = { id: id('ev'), name }
      state.events.push(event)
      state.triggers[event.id] = []
      return wrap({ event })
    },
    deleteEvent: async ({ eventId }) => {
      state.events = state.events.filter((event) => event.id !== eventId)
      return { ok: true, result: 'event deleted' }
    },
    listEventTriggers: async ({ eventId }) => wrap({ eventTriggers: state.triggers[eventId] ?? [] }),
    createEventTrigger: async (args) => {
      const trigger = { id: id('tr'), ...args }
      state.triggers[args.eventId] = [...(state.triggers[args.eventId] ?? []), trigger]
      return wrap({ eventTrigger: trigger })
    },
    updateEventTrigger: async (args) => wrap({ eventTrigger: { id: args.triggerId, ...args } }),
    deleteEventTrigger: async ({ triggerId }) => {
      for (const [eventId, triggers] of Object.entries(state.triggers)) {
        state.triggers[eventId] = triggers.filter((trigger) => trigger.id !== triggerId)
      }
      return { ok: true, result: 'trigger deleted' }
    },
    createTask: async (args) => {
      const task = { id: id('tk'), ...args }
      state.tasks[task.id] = task
      return wrap({ task })
    },
    updateScheduledTask: async (args) => wrap({ task: { id: args.taskId, ...args } }),
    deleteTask: async ({ taskId }) => {
      delete state.tasks[taskId]
      return { ok: true, result: 'task deleted' }
    },
    ...over,
  }

  const calls = []
  const supervisor = vi.fn(async (tool, args = {}) => {
    calls.push({ tool, args })
    const handler = handlers[tool]
    if (!handler) throw new Error(`the test's supervisor has no ${tool}`)
    return handler(args)
  })

  return { supervisor, calls, state, toolsCalled: () => calls.map((call) => call.tool) }
}

function build({ supervisor, records = {} } = {}) {
  const fake = supervisor ?? fakeSupervisor()
  let written = JSON.parse(JSON.stringify(records))
  const store = {
    read: vi.fn(async () => JSON.parse(JSON.stringify(written))),
    write: vi.fn(async (state) => {
      written = JSON.parse(JSON.stringify(state))
    }),
  }
  const logger = { warn: vi.fn() }
  const schedules = createSchedules({
    clientFor: () => ({ supervisor: fake.supervisor }),
    store,
    logger,
  })
  return { schedules, store, logger, fake, saved: () => written }
}

describe('looksSchedulable', () => {
  it('accepts a fixed minute', () => {
    expect(looksSchedulable('0 9 * * *')).toBe(true)
    expect(looksSchedulable('59 * * * *')).toBe(true)
  })

  // The backend enforces hourly-minimum granularity, and these are the exact
  // spellings it documents as rejected.
  it.each(['* * * * *', '*/5 * * * *', '0-30 9 * * *', '0,30 9 * * *', '60 9 * * *'])(
    'refuses %s',
    (cron) => {
      expect(looksSchedulable(cron)).toBe(false)
    },
  )

  it('refuses anything that is not five fields', () => {
    expect(looksSchedulable('0 9 * *')).toBe(false)
    expect(looksSchedulable('0 9 * * * *')).toBe(false)
    expect(looksSchedulable('')).toBe(false)
    expect(looksSchedulable(undefined)).toBe(false)
  })
})

describe('validateSchedule', () => {
  it('accepts a well-formed cron task', () => {
    expect(validateSchedule(taskEntry()).ok).toBe(true)
  })

  it('accepts a well-formed trigger', () => {
    expect(validateSchedule(triggerEntry()).ok).toBe(true)
    expect(validateSchedule(triggerEntry({ emitEvent: 'notes_written', cron: '0 9 * * *' })).ok).toBe(true)
  })

  it('refuses a declaration that is not an object', () => {
    expect(validateSchedule({ id: 'x' }).reason).toMatch(/needs a declaration/)
    expect(validateSchedule({ id: 'x', value: 'nightly' }).reason).toMatch(/needs a declaration/)
  })

  it('names the kinds when given one it does not know', () => {
    const { reason } = validateSchedule({ id: 'x', value: { kind: 'webhook' } })
    expect(reason).toContain('"webhook"')
    expect(reason).toContain('task')
    expect(reason).toContain('trigger')
  })

  it('says "(none)" rather than an empty pair of quotes', () => {
    expect(validateSchedule({ id: 'x', value: {} }).reason).toContain('(none)')
  })

  it('requires a title and a workspace', () => {
    expect(validateSchedule(taskEntry({ title: '  ' })).reason).toMatch(/title/)
    expect(validateSchedule(taskEntry({ workspaceId: '' })).reason).toMatch(/workspace/)
  })

  // A field left out entirely, rather than left blank. Manifests are written by
  // hand, so this is the ordinary way to get one wrong.
  it('says the same thing about a field that is missing as one that is empty', () => {
    expect(validateSchedule({ id: 'x', value: { kind: KINDS.task } }).reason).toMatch(/title/)
    expect(validateSchedule({ id: 'x', value: { kind: KINDS.task, title: 'Digest' } }).reason).toMatch(/workspace/)
    expect(
      validateSchedule({ id: 'x', value: { kind: KINDS.task, title: 'Digest', workspaceId: 'ws1' } }).reason,
    ).toMatch(/hourly/)
    expect(
      validateSchedule({ id: 'x', value: { kind: KINDS.trigger, title: 'Notes', workspaceId: 'ws1' } }).reason,
    ).toMatch(/event name/)
  })

  it('explains the granularity rule rather than just refusing', () => {
    const { reason } = validateSchedule(taskEntry({ cron: '*/5 * * * *' }))
    expect(reason).toContain('*/5 * * * *')
    expect(reason).toContain('hourly')
  })

  it('refuses an event name the server would not accept', () => {
    expect(validateSchedule(triggerEntry({ event: 'Deploy-Done' })).reason).toMatch(/event name/)
    expect(validateSchedule(triggerEntry({ event: '' })).reason).toMatch(/event name/)
    expect(validateSchedule(triggerEntry({ emitEvent: 'Notes Written' })).reason).toMatch(/event name/)
  })

  it("refuses a bad cron on the trigger's spawned task", () => {
    expect(validateSchedule(triggerEntry({ cron: '* * * * *' })).reason).toMatch(/trigger creates/)
  })
})

describe('fingerprint', () => {
  it('is the same for two declarations that say the same thing', () => {
    const a = { kind: KINDS.task, workspaceId: 'ws1', title: 'Digest', cron: '0 9 * * *' }
    const b = { title: 'Digest', cron: '0 9 * * *', kind: KINDS.task, workspaceId: 'ws1' }
    // Field by field rather than JSON.stringify: key order is not a change.
    expect(fingerprint(a)).toBe(fingerprint(b))
  })

  it('changes when anything meaningful changes', () => {
    const base = taskEntry().value
    expect(fingerprint({ ...base, cron: '0 10 * * *' })).not.toBe(fingerprint(base))
    expect(fingerprint({ ...base, title: 'Other' })).not.toBe(fingerprint(base))
    expect(fingerprint({ ...base, allowAllCommands: true })).not.toBe(fingerprint(base))
  })

  it('treats an omitted field and its default as the same', () => {
    const base = taskEntry().value
    expect(fingerprint({ ...base, assignee: 'agent' })).toBe(fingerprint(base))
  })
})

describe('planFor', () => {
  it('creates what has no record', () => {
    const plan = planFor([taskEntry()])
    expect(plan.create.map((item) => item.id)).toEqual(['nightly'])
    expect(plan.update).toEqual([])
    expect(plan.remove).toEqual([])
  })

  it('leaves alone what already matches', () => {
    const entry = taskEntry()
    const plan = planFor([entry], { nightly: { kind: KINDS.task, fingerprint: fingerprint(entry.value) } })
    expect(plan.keep.map((item) => item.id)).toEqual(['nightly'])
    expect(plan.create).toEqual([])
  })

  it('updates what has drifted', () => {
    const plan = planFor([taskEntry()], { nightly: { kind: KINDS.task, fingerprint: 'something else' } })
    expect(plan.update.map((item) => item.id)).toEqual(['nightly'])
  })

  it('removes a record nothing declares any more', () => {
    const plan = planFor([], { nightly: { kind: KINDS.task, fingerprint: 'x' } })
    expect(plan.remove.map((item) => item.id)).toEqual(['nightly'])
  })

  // The distinction that matters: a typo is not a retraction.
  it('does not remove a record whose declaration merely failed validation', () => {
    const plan = planFor([taskEntry({ cron: 'nonsense' })], { nightly: { fingerprint: 'x' } })
    expect(plan.remove).toEqual([])
    expect(plan.problems.map((problem) => problem.id)).toEqual(['nightly'])
  })

  it('reports a declaration with no id at all', () => {
    const plan = planFor([{ value: { kind: 'nope' } }])
    expect(plan.problems).toHaveLength(1)
    expect(plan.problems[0].id).toBe('')
  })
})

describe('idFrom', () => {
  it('reads an id out of the SDK content array', () => {
    expect(idFrom({ content: [{ text: '{"task":{"id":"tk1"}}' }] }, ['task', 'id'])).toBe('tk1')
  })

  it('reads one out of a bare JSON string, and out of an object', () => {
    expect(idFrom('{"task":{"id":"tk1"}}', ['task', 'id'])).toBe('tk1')
    expect(idFrom({ task: { id: 'tk1' } }, ['task', 'id'])).toBe('tk1')
  })

  // An id that cannot be found must be empty rather than undefined: the caller
  // turns that into a failure, and a record pointing at `undefined` would be
  // worse than no record at all.
  it('answers with an empty string when there is no id to find', () => {
    expect(idFrom('not json', ['task', 'id'])).toBe('')
    expect(idFrom({ content: [{}] }, ['task', 'id'])).toBe('')
    expect(idFrom({ task: {} }, ['task', 'id'])).toBe('')
    expect(idFrom({ task: 'a string' }, ['task', 'id'])).toBe('')
    expect(idFrom({ task: { id: '' } }, ['task', 'id'])).toBe('')
    expect(idFrom(null, ['task', 'id'])).toBe('')
  })
})

describe('reconcile', () => {
  it('creates a cron task and remembers it', async () => {
    const { schedules, fake, saved } = build()

    const result = await schedules.reconcile('standup', [taskEntry()])

    expect(result).toMatchObject({ ok: true, created: ['nightly'], updated: [], removed: [] })
    expect(fake.calls[0]).toMatchObject({
      tool: 'createTask',
      args: { workspaceId: 'ws1', title: 'Nightly digest', cronSchedule: '0 9 * * *', assignee: 'agent' },
    })
    expect(saved().standup.nightly).toMatchObject({ kind: KINDS.task, taskId: 'tk1' })
  })

  // The whole reason reconciliation exists rather than "create on install".
  it('does nothing at all on a second pass with the same declaration', async () => {
    const { schedules, fake } = build()
    await schedules.reconcile('standup', [taskEntry()])
    const after = fake.calls.length

    const result = await schedules.reconcile('standup', [taskEntry()])

    expect(fake.calls.length).toBe(after)
    expect(result).toMatchObject({ created: [], updated: [], unchanged: ['nightly'] })
  })

  it('revises a task in place when the declaration changes', async () => {
    const { schedules, fake, saved } = build()
    await schedules.reconcile('standup', [taskEntry()])

    const result = await schedules.reconcile('standup', [taskEntry({ cron: '0 10 * * *' })])

    expect(result.updated).toEqual(['nightly'])
    expect(fake.calls.at(-1)).toMatchObject({
      tool: 'updateScheduledTask',
      args: { taskId: 'tk1', cronSchedule: '0 10 * * *' },
    })
    // Still one task, not two.
    expect(Object.keys(saved().standup)).toEqual(['nightly'])
  })

  it('removes what the extension stopped declaring', async () => {
    const { schedules, fake, saved } = build()
    await schedules.reconcile('standup', [taskEntry()])

    const result = await schedules.reconcile('standup', [])

    expect(result.removed).toEqual(['nightly'])
    expect(fake.calls.at(-1)).toMatchObject({ tool: 'deleteTask', args: { taskId: 'tk1' } })
    expect(saved().standup).toEqual({})
  })

  it('creates the event a trigger names, then the trigger', async () => {
    const { schedules, fake, saved } = build()

    await schedules.reconcile('standup', [triggerEntry()])

    expect(fake.toolsCalled()).toEqual(['listEvents', 'createEvent', 'createEventTrigger'])
    expect(saved().standup['on-deploy']).toMatchObject({
      kind: KINDS.trigger,
      eventId: 'ev1',
      triggerId: 'tr2',
      createdEvents: ['ev1'],
    })
  })

  it('reuses an event that already exists, and does not claim to have made it', async () => {
    const fake = fakeSupervisor()
    await fake.supervisor('createEvent', { name: 'deploy_done' })
    const { schedules, saved } = build({ supervisor: fake })

    await schedules.reconcile('standup', [triggerEntry()])

    expect(fake.toolsCalled().filter((tool) => tool === 'createEvent')).toHaveLength(1)
    // The one it made itself, not the one it found.
    expect(saved().standup['on-deploy'].createdEvents).toEqual([])
  })

  it('resolves the chained event too', async () => {
    const { schedules, fake, saved } = build()

    await schedules.reconcile('standup', [triggerEntry({ emitEvent: 'notes_written' })])

    const created = fake.calls.find((call) => call.tool === 'createEventTrigger')
    expect(created.args.emitEventId).toBe('ev2')
    expect(saved().standup['on-deploy'].createdEvents).toEqual(['ev1', 'ev2'])
  })

  it('revises a trigger in place', async () => {
    const { schedules, fake } = build()
    await schedules.reconcile('standup', [triggerEntry()])

    await schedules.reconcile('standup', [triggerEntry({ title: 'Write the notes' })])

    expect(fake.calls.at(-1)).toMatchObject({
      tool: 'updateEventTrigger',
      args: { triggerId: 'tr2', title: 'Write the notes' },
    })
  })

  // Somebody deleted the event by hand between passes. Making it again is what
  // the extension asked for, and the record has to learn that this one is now
  // ours to tidy away later.
  it('takes ownership of an event it had to recreate during an update', async () => {
    const { schedules, fake, saved } = build()
    await schedules.reconcile('standup', [triggerEntry({ emitEvent: 'notes_written' })])
    await fake.supervisor('deleteEvent', { eventId: 'ev1' })
    // As though the first pass had found that event rather than made it.
    const record = await schedules.recordsFor('standup')
    expect(record['on-deploy'].createdEvents).toContain('ev1')

    await schedules.reconcile('standup', [triggerEntry({ emitEvent: 'notes_written', title: 'Different' })])

    expect(saved().standup['on-deploy'].eventId).toBe('ev4')
    expect(saved().standup['on-deploy'].createdEvents).toEqual(['ev1', 'ev2', 'ev4'])
  })

  it('records an event the update had to create', async () => {
    const { schedules, saved } = build()
    await schedules.reconcile('standup', [triggerEntry()])

    await schedules.reconcile('standup', [triggerEntry({ emitEvent: 'notes_written' })])

    expect(saved().standup['on-deploy'].createdEvents).toEqual(['ev1', 'ev3'])
  })

  // A declaration that changed kind is not an edit of the same thing.
  it('takes the old one away and makes the new one when the kind changes', async () => {
    const { schedules, fake, saved } = build()
    await schedules.reconcile('standup', [taskEntry()])

    const result = await schedules.reconcile('standup', [taskEntry({ kind: KINDS.trigger, event: 'deploy_done' })])

    expect(result.updated).toEqual(['nightly'])
    expect(fake.toolsCalled()).toContain('deleteTask')
    expect(saved().standup.nightly).toMatchObject({ kind: KINDS.trigger })
  })

  it('reports what could not be created, and keeps going', async () => {
    const fake = fakeSupervisor({
      createTask: async () => ({ ok: false, reason: 'This extension may not call "createTask".' }),
    })
    const { schedules, logger, saved } = build({ supervisor: fake })

    const result = await schedules.reconcile('standup', [taskEntry(), triggerEntry()])

    expect(result.ok).toBe(false)
    expect(result.problems).toEqual([{ id: 'nightly', reason: 'This extension may not call "createTask".' }])
    // The other one still happened.
    expect(result.created).toEqual(['on-deploy'])
    expect(saved().standup.nightly).toBeUndefined()
    expect(logger.warn).toHaveBeenCalledWith(expect.stringContaining('nightly'))
  })

  it('refuses to record a create that answered with no id', async () => {
    const fake = fakeSupervisor({ createTask: async () => ({ ok: true, result: '{"task":{}}' }) })
    const { schedules, saved } = build({ supervisor: fake })

    const result = await schedules.reconcile('standup', [taskEntry()])

    expect(result.problems[0].reason).toMatch(/no id/)
    expect(saved().standup).toEqual({})
  })

  it('reports a trigger create that answered with no id', async () => {
    const fake = fakeSupervisor({ createEventTrigger: async () => ({ ok: true, result: '{}' }) })
    const { schedules } = build({ supervisor: fake })

    const result = await schedules.reconcile('standup', [triggerEntry()])

    expect(result.problems[0].reason).toMatch(/no id/)
  })

  it('reports an event create that answered with no id', async () => {
    const fake = fakeSupervisor({ createEvent: async () => ({ ok: true, result: '{}' }) })
    const { schedules } = build({ supervisor: fake })

    const result = await schedules.reconcile('standup', [triggerEntry()])

    expect(result.problems[0].reason).toMatch(/no id/)
  })

  it('reports a refused listEvents rather than creating a duplicate event', async () => {
    const fake = fakeSupervisor({
      listEvents: async () => ({ ok: false, reason: 'This extension was not granted access to all workspaces.' }),
    })
    const { schedules } = build({ supervisor: fake })

    const result = await schedules.reconcile('standup', [triggerEntry()])

    expect(result.problems[0].reason).toMatch(/not granted/)
    expect(fake.toolsCalled()).not.toContain('createEvent')
  })

  it('sends an empty body rather than nothing when a declaration has none', async () => {
    const { schedules, fake } = build()
    const bodyless = taskEntry()
    delete bodyless.value.body

    await schedules.reconcile('standup', [bodyless])
    await schedules.reconcile('standup', [{ ...bodyless, value: { ...bodyless.value, cron: '0 10 * * *' } }])

    expect(fake.calls[0].args.body).toBe('')
    expect(fake.calls.at(-1).args.body).toBe('')
  })

  it('reports a refused createEventTrigger', async () => {
    const fake = fakeSupervisor({ createEventTrigger: async () => ({ ok: false, reason: 'refused' }) })
    const { schedules } = build({ supervisor: fake })

    expect((await schedules.reconcile('standup', [triggerEntry()])).problems[0].reason).toBe('refused')
  })

  // The kind changed, so the old one has to go first — and if it will not go,
  // making the new one anyway would leave two things running.
  it('does not make the replacement when the old one could not be removed', async () => {
    const fake = fakeSupervisor({ deleteTask: async () => ({ ok: false, reason: 'workspace not found' }) })
    const { schedules, saved } = build({ supervisor: fake })
    await schedules.reconcile('standup', [taskEntry()])

    const result = await schedules.reconcile('standup', [taskEntry({ kind: KINDS.trigger, event: 'deploy_done' })])

    expect(result.problems[0].reason).toBe('workspace not found')
    expect(fake.toolsCalled()).not.toContain('createEventTrigger')
    // Still the task it could not remove, so the next pass tries again.
    expect(saved().standup.nightly).toMatchObject({ kind: KINDS.task })
  })

  // The trigger's own event was found, and only the chained one was refused —
  // the two lookups fail in different places and the second is the easier one
  // to leave untested.
  it('reports a refused chained event when the trigger event itself resolved', async () => {
    let events = []
    const fake = fakeSupervisor({
      listEvents: async () => ({ ok: true, result: JSON.stringify({ events }) }),
      createEvent: async ({ name }) => {
        if (name === 'notes_written') return { ok: false, reason: 'refused' }
        events = [{ id: 'ev1', name }]
        return { ok: true, result: `{"event":{"id":"ev1","name":"${name}"}}` }
      },
    })
    const { schedules } = build({ supervisor: fake })
    await schedules.reconcile('standup', [triggerEntry()])

    const result = await schedules.reconcile('standup', [triggerEntry({ emitEvent: 'notes_written' })])

    expect(result.problems[0].reason).toBe('refused')
  })

  // A record written before chained events existed has no `createdEvents` at
  // all, and reading one must not be how the reconciler falls over.
  it('reads a record from an older version that has no createdEvents', async () => {
    const fake = fakeSupervisor()
    await fake.supervisor('createEvent', { name: 'deploy_done' })
    const { schedules, saved } = build({
      supervisor: fake,
      records: { standup: { 'on-deploy': { kind: KINDS.trigger, workspaceId: 'ws1', eventId: 'ev1', triggerId: 'tr2', fingerprint: 'old' } } },
    })

    await schedules.reconcile('standup', [triggerEntry({ emitEvent: 'notes_written' })])

    // The one it had to make on the way through, and nothing invented for the
    // event that was already there.
    expect(saved().standup['on-deploy'].createdEvents).toEqual(['ev2'])
  })

  it('removes a record from an older version without trying to tidy events it never made', async () => {
    const fake = fakeSupervisor()
    const { schedules } = build({
      supervisor: fake,
      records: { standup: { 'on-deploy': { kind: KINDS.trigger, triggerId: 'tr1', fingerprint: 'old' } } },
    })

    const result = await schedules.reconcile('standup', [])

    expect(result.removed).toEqual(['on-deploy'])
    expect(fake.toolsCalled()).toEqual(['deleteEventTrigger'])
  })

  it('reports a refused createEvent', async () => {
    const fake = fakeSupervisor({ createEvent: async () => ({ ok: false, reason: 'refused' }) })
    const { schedules } = build({ supervisor: fake })

    expect((await schedules.reconcile('standup', [triggerEntry()])).problems[0].reason).toBe('refused')
  })

  it('reports a refused chained event', async () => {
    let calls = 0
    const fake = fakeSupervisor({
      createEvent: async ({ name }) => {
        calls += 1
        return calls === 1
          ? { ok: true, result: `{"event":{"id":"ev1","name":"${name}"}}` }
          : { ok: false, reason: 'refused' }
      },
    })
    const { schedules } = build({ supervisor: fake })

    const result = await schedules.reconcile('standup', [triggerEntry({ emitEvent: 'notes_written' })])

    expect(result.problems[0].reason).toBe('refused')
  })

  it('survives a listEvents answer that is not JSON', async () => {
    const fake = fakeSupervisor({ listEvents: async () => ({ ok: true, result: 'not json' }) })
    const { schedules, saved } = build({ supervisor: fake })

    await schedules.reconcile('standup', [triggerEntry()])

    // Nothing found means nothing to reuse, so it made one.
    expect(saved().standup['on-deploy'].createdEvents).toEqual(['ev1'])
  })

  // A record pointing at something the user deleted by hand would otherwise be
  // permanently broken, with no way back short of a reinstall.
  it('makes the task again when the update finds nothing to revise', async () => {
    const fake = fakeSupervisor({ updateScheduledTask: async () => ({ ok: false, reason: 'task not found' }) })
    const { schedules, saved } = build({ supervisor: fake })
    await schedules.reconcile('standup', [taskEntry()])

    const result = await schedules.reconcile('standup', [taskEntry({ cron: '0 10 * * *' })])

    expect(result.updated).toEqual(['nightly'])
    expect(fake.toolsCalled().filter((tool) => tool === 'createTask')).toHaveLength(2)
    expect(saved().standup.nightly.taskId).toBe('tk2')
  })

  it('makes the trigger again when the update finds nothing to revise', async () => {
    const fake = fakeSupervisor({ updateEventTrigger: async () => ({ ok: false, reason: 'trigger not found' }) })
    const { schedules, saved } = build({ supervisor: fake })
    await schedules.reconcile('standup', [triggerEntry()])

    const result = await schedules.reconcile('standup', [triggerEntry({ title: 'Different' })])

    expect(result.updated).toEqual(['on-deploy'])
    expect(saved().standup['on-deploy'].triggerId).toBe('tr3')
  })

  it('reports a refused event lookup during an update', async () => {
    let calls = 0
    const fake = fakeSupervisor({
      listEvents: async () => {
        calls += 1
        return calls === 1 ? { ok: true, result: '{"events":[]}' } : { ok: false, reason: 'refused' }
      },
    })
    const { schedules } = build({ supervisor: fake })
    await schedules.reconcile('standup', [triggerEntry()])

    const result = await schedules.reconcile('standup', [triggerEntry({ title: 'Different' })])

    expect(result.problems[0].reason).toBe('refused')
  })

  it('reports a refused chained event during an update', async () => {
    let creates = 0
    const fake = fakeSupervisor({
      createEvent: async ({ name }) => {
        creates += 1
        return creates <= 1
          ? { ok: true, result: `{"event":{"id":"ev1","name":"${name}"}}` }
          : { ok: false, reason: 'refused' }
      },
    })
    const { schedules } = build({ supervisor: fake })
    await schedules.reconcile('standup', [triggerEntry()])

    const result = await schedules.reconcile('standup', [triggerEntry({ emitEvent: 'notes_written' })])

    expect(result.problems[0].reason).toBe('refused')
  })

  it('reports a removal that failed, and keeps the record so it is tried again', async () => {
    const fake = fakeSupervisor({ deleteTask: async () => ({ ok: false, reason: 'task not found' }) })
    const { schedules, saved, logger } = build({ supervisor: fake })
    await schedules.reconcile('standup', [taskEntry()])

    const result = await schedules.reconcile('standup', [])

    expect(result.removed).toEqual([])
    expect(result.problems[0]).toMatchObject({ id: 'nightly' })
    expect(saved().standup.nightly).toBeDefined()
    expect(logger.warn).toHaveBeenCalled()
  })

  it('reports a failed trigger delete without touching its events', async () => {
    const fake = fakeSupervisor({ deleteEventTrigger: async () => ({ ok: false, reason: 'refused' }) })
    const { schedules } = build({ supervisor: fake })
    await schedules.reconcile('standup', [triggerEntry()])

    const result = await schedules.reconcile('standup', [])

    expect(result.problems[0].reason).toBe('refused')
    expect(fake.toolsCalled()).not.toContain('deleteEvent')
  })

  it('starts from nothing when the record file cannot be read', async () => {
    const store = { read: vi.fn(async () => { throw new Error('ENOENT') }), write: vi.fn(async () => {}) }
    const fake = fakeSupervisor()
    const schedules = createSchedules({ clientFor: () => ({ supervisor: fake.supervisor }), store })

    const result = await schedules.reconcile('standup', [taskEntry()])

    expect(result.created).toEqual(['nightly'])
  })

  it('starts from nothing when the record file holds nothing', async () => {
    const store = { read: vi.fn(async () => null), write: vi.fn(async () => {}) }
    const fake = fakeSupervisor()
    const schedules = createSchedules({ clientFor: () => ({ supervisor: fake.supervisor }), store })

    expect((await schedules.reconcile('standup', [])).ok).toBe(true)
  })

  it('reconciles nothing when given nothing', async () => {
    const { schedules, fake } = build()

    const result = await schedules.reconcile('standup')

    expect(result).toMatchObject({ ok: true, created: [], removed: [] })
    expect(fake.calls).toEqual([])
  })

  it('logs through console when no logger is given', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const fake = fakeSupervisor()
    const schedules = createSchedules({
      clientFor: () => ({ supervisor: fake.supervisor }),
      store: { read: async () => ({}), write: async () => {} },
    })

    await schedules.reconcile('standup', [taskEntry({ cron: 'nonsense' })])

    expect(warn).toHaveBeenCalled()
    warn.mockRestore()
  })
})

describe('recordsFor', () => {
  it('answers with a copy, so a caller cannot edit what was created', async () => {
    const { schedules, saved } = build()
    await schedules.reconcile('standup', [taskEntry()])

    const records = await schedules.recordsFor('standup')
    records.nightly.taskId = 'tampered'

    expect(saved().standup.nightly.taskId).toBe('tk1')
  })

  it('answers with nothing for an extension that has created nothing', async () => {
    const { schedules } = build()
    expect(await schedules.recordsFor('linear')).toEqual({})
  })
})

describe('removeOwner', () => {
  it('takes away everything an extension set up', async () => {
    const { schedules, fake, saved } = build()
    await schedules.reconcile('standup', [taskEntry(), triggerEntry()])

    const result = await schedules.removeOwner('standup')

    expect(result.ok).toBe(true)
    expect(result.removed.sort()).toEqual(['nightly', 'on-deploy'])
    expect(fake.toolsCalled()).toContain('deleteTask')
    expect(fake.toolsCalled()).toContain('deleteEventTrigger')
    // The extension is gone from the record entirely, not left as an empty map.
    expect(saved().standup).toBeUndefined()
  })

  // Deleting it regardless would silently break somebody else's trigger.
  it('deletes an event it created only once nothing is left listening', async () => {
    const fake = fakeSupervisor()
    const { schedules } = build({ supervisor: fake })
    await schedules.reconcile('standup', [triggerEntry()])
    // A second trigger on the same event, from somewhere else.
    await fake.supervisor('createEventTrigger', { eventId: 'ev1', title: 'somebody else' })

    await schedules.removeOwner('standup')

    expect(fake.toolsCalled()).not.toContain('deleteEvent')
  })

  it('deletes an event it created when it is the last one out', async () => {
    const { schedules, fake } = build()
    await schedules.reconcile('standup', [triggerEntry()])

    await schedules.removeOwner('standup')

    expect(fake.calls.at(-1)).toMatchObject({ tool: 'deleteEvent', args: { eventId: 'ev1' } })
  })

  it('reads an answer that names no triggers at all as an unused event', async () => {
    const fake = fakeSupervisor({ listEventTriggers: async () => ({ ok: true, result: '{}' }) })
    const { schedules } = build({ supervisor: fake })
    await schedules.reconcile('standup', [triggerEntry()])

    await schedules.removeOwner('standup')

    expect(fake.toolsCalled()).toContain('deleteEvent')
  })

  it('leaves an event alone when it cannot tell whether anything still uses it', async () => {
    const fake = fakeSupervisor({ listEventTriggers: async () => ({ ok: false, reason: 'refused' }) })
    const { schedules } = build({ supervisor: fake })
    await schedules.reconcile('standup', [triggerEntry()])

    await schedules.removeOwner('standup')

    expect(fake.toolsCalled()).not.toContain('deleteEvent')
  })

  it('leaves an event alone when the list of triggers is unreadable', async () => {
    const fake = fakeSupervisor({ listEventTriggers: async () => ({ ok: true, result: 'not json' }) })
    const { schedules } = build({ supervisor: fake })
    await schedules.reconcile('standup', [triggerEntry()])

    await schedules.removeOwner('standup')

    expect(fake.toolsCalled()).not.toContain('deleteEvent')
  })

  // The worst outcome available is a cron task still firing that nothing
  // remembers creating, so a failed removal keeps its record.
  it('keeps the record of what it could not remove, and says so', async () => {
    const fake = fakeSupervisor({ deleteTask: async () => ({ ok: false, reason: 'workspace not found' }) })
    const { schedules, saved, logger } = build({ supervisor: fake })
    await schedules.reconcile('standup', [taskEntry()])

    const result = await schedules.removeOwner('standup')

    expect(result.ok).toBe(false)
    expect(result.problems).toEqual([{ id: 'nightly', reason: 'workspace not found' }])
    expect(saved().standup.nightly).toBeDefined()
    expect(logger.warn).toHaveBeenCalledWith(expect.stringContaining('could not remove'))
  })

  it('is a no-op for an extension that created nothing', async () => {
    const { schedules, fake } = build()

    const result = await schedules.removeOwner('linear')

    expect(result).toMatchObject({ ok: true, removed: [] })
    expect(fake.calls).toEqual([])
  })
})
