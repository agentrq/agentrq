import { describe, it, expect } from 'vitest'

import {
  TRAJECTORY_LANES,
  belongsInThread,
  buildTrajectory,
  defaultDetailTab,
  filterTrajectory,
  trajectoryLaneCounts,
} from '../src/composables/useTrajectory'

const at = (minutes) => new Date(Date.UTC(2026, 8, 4, 10, minutes)).toISOString()

const message = (overrides = {}) => ({
  id: 'm1',
  sender: 'agent',
  text: 'hello',
  createdAt: at(0),
  ...overrides,
})

const permission = (status, overrides = {}) => message({
  id: `perm-${status}`,
  text: 'Permission requested',
  metadata: {
    type: 'permission_request',
    requestId: 'req-1',
    toolName: 'Bash',
    inputPreview: 'ls -la',
    status,
  },
  ...overrides,
})

const thought = (text, overrides = {}) => message({
  id: 'thought-1',
  text: '',
  metadata: { type: 'agent_thought', text },
  ...overrides,
})

const plan = (entries, overrides = {}) => message({
  id: 'plan-1',
  text: '',
  metadata: { type: 'agent_plan', entries },
  ...overrides,
})

const toolCall = (overrides = {}) => ({
  id: 'tc1',
  toolName: 'Bash',
  inputPreview: 'ls -la',
  status: 'allowed',
  createdAt: at(0),
  ...overrides,
})

describe('belongsInThread', () => {
  it('keeps what was said', () => {
    expect(belongsInThread(message())).toBe(true)
    expect(belongsInThread(message({ sender: 'human', text: 'do the thing' }))).toBe(true)
  })

  // A permission request is the one card addressed to the reader: it stays
  // until they have answered it.
  it('keeps a permission request until it has a verdict', () => {
    expect(belongsInThread(permission('pending'))).toBe(true)
    expect(belongsInThread(permission('allow'))).toBe(false)
    expect(belongsInThread(permission('allow_always'))).toBe(false)
    expect(belongsInThread(permission('deny'))).toBe(false)
    expect(belongsInThread(permission('cancelled'))).toBe(false)
  })

  it('treats a permission request with no status as still pending', () => {
    const noStatus = message({ metadata: { type: 'permission_request', toolName: 'Bash' } })

    expect(belongsInThread(noStatus)).toBe(true)
  })

  it('sends reasoning and the counters to the trajectory, and keeps plans', () => {
    expect(belongsInThread(thought('weighing the options'))).toBe(false)
    expect(belongsInThread(message({ metadata: { type: 'agent_usage', used: 10 } }))).toBe(false)
    expect(belongsInThread(plan([{ content: 'step', status: 'pending' }]))).toBe(true)
  })
})

describe('buildTrajectory', () => {
  it('files each kind of activity under its lane', () => {
    const items = buildTrajectory([
      message({ id: 'a', sender: 'human', text: 'do it', createdAt: at(0) }),
      thought('checking the config', { id: 'b', createdAt: at(1) }),
      message({ id: 'c', sender: 'agent', text: 'done', createdAt: at(2) }),
      message({ id: 'd', sender: 'slack', text: 'from slack', createdAt: at(3) }),
    ], [toolCall({ id: 'e', createdAt: at(4) })])

    expect(items.map((it) => [it.lane, it.laneLabel])).toEqual([
      ['input', 'INPUT'],
      ['thought', 'THINKING'],
      ['agent', 'AGENT'],
      ['input', 'SLACK'],
      ['tool', 'TOOL'],
    ])
  })

  it('orders everything oldest first, whatever order it arrives in', () => {
    const items = buildTrajectory(
      [message({ id: 'late', createdAt: at(9) }), message({ id: 'early', createdAt: at(1) })],
      [toolCall({ id: 'mid', createdAt: at(5) })],
    )

    expect(items.map((it) => it.id)).toEqual(['m-early', 't-mid', 'm-late'])
  })

  it('previews reasoning by its first line and a plan by its progress', () => {
    const items = buildTrajectory([
      thought('Looking at the failing test\nthen the fix', { id: 't', createdAt: at(0) }),
      plan([
        { content: 'read the code', status: 'completed' },
        { content: 'write the fix', status: 'in_progress' },
      ], { id: 'p', createdAt: at(1) }),
    ], [])

    expect(items[0].preview).toBe('Looking at the failing test')
    expect(items[0].laneLabel).toBe('THINKING')
    expect(items[1].preview).toBe('1/2 · write the fix')
    expect(items[1].laneLabel).toBe('PLAN')
    expect(items[1].lane).toBe('thought')
  })

  it('reads reasoning out of the metadata, which is where revisions land', () => {
    const items = buildTrajectory([thought('the current wording', { text: 'the original body' })], [])

    expect(items[0].raw.text).toBe('the current wording')
  })

  it('falls back to a plan\'s text when it has no entries to count', () => {
    const markdown = message({
      id: 'p',
      metadata: { type: 'agent_plan', planType: 'markdown', content: '# Plan\n\nDo the thing' },
    })
    const empty = message({ id: 'q', text: 'no entries yet', metadata: { type: 'agent_plan', entries: [] } })

    const items = buildTrajectory([markdown, empty], [])

    expect(items[0].preview).toBe('# Plan Do the thing')
    expect(items[1].preview).toBe('no entries yet')
  })

  it('names the step a plan is on, or its last one when it is finished', () => {
    const next = plan([
      { content: 'first', status: 'completed' },
      { content: 'second', status: 'pending' },
    ], { id: 'a', createdAt: at(0) })
    const finished = plan([
      { content: 'first', status: 'completed' },
      { content: 'last', status: 'completed' },
    ], { id: 'b', createdAt: at(1) })

    const items = buildTrajectory([next, finished], [])

    expect(items[0].preview).toBe('1/2 · second')
    expect(items[1].preview).toBe('2/2 · last')
  })

  it('leaves the context counters out — the composer shows those', () => {
    const items = buildTrajectory([
      message({ id: 'u', metadata: { type: 'agent_usage', used: 100, size: 1000 } }),
    ], [])

    expect(items).toEqual([])
  })

  // Every permission request is recorded as a tool call too, so showing both
  // listed each prompted tool twice.
  it('shows the recorded tool call rather than the request that produced it', () => {
    const items = buildTrajectory(
      [permission('allow', { createdAt: at(0) })],
      [toolCall({ id: 'tc1', status: 'allowed', createdAt: at(0) })],
    )

    expect(items).toHaveLength(1)
    expect(items[0].id).toBe('t-tc1')
    expect(items[0].raw.status).toBe('allowed')
  })

  it('pairs each request with one tool call, not all of them', () => {
    const items = buildTrajectory(
      [
        permission('allow', { id: 'p1', createdAt: at(0) }),
        permission('allow', { id: 'p2', createdAt: at(1) }),
        permission('allow', { id: 'p3', createdAt: at(2) }),
      ],
      [toolCall({ id: 'tc1', createdAt: at(0) }), toolCall({ id: 'tc2', createdAt: at(1) })],
    )

    expect(items.filter((it) => it.id.startsWith('m-'))).toHaveLength(1)
    expect(items.filter((it) => it.id.startsWith('t-'))).toHaveLength(2)
  })

  it('matches a request against a tool call whose input was truncated for storage', () => {
    const long = 'x'.repeat(4000)
    const items = buildTrajectory(
      [permission('allow', { metadata: { type: 'permission_request', toolName: 'Bash', inputPreview: long, status: 'allow' } })],
      [toolCall({ inputPreview: `${long.slice(0, 2000)}…` })],
    )

    expect(items).toHaveLength(1)
    expect(items[0].id).toBe('t-tc1')
  })

  // Tasks that ran before tool calls were recorded have the message and
  // nothing else; dropping it would erase their tool history.
  it('keeps a permission request that no tool call stands for', () => {
    const items = buildTrajectory([permission('deny')], [toolCall({ toolName: 'Edit' })])

    expect(items).toHaveLength(2)
    const request = items.find((it) => it.id.startsWith('m-'))
    expect(request.lane).toBe('tool')
    expect(request.raw.status).toBe('denied')
    expect(request.label).toBe('Bash')
  })

  it('reads a permission request written before the API sent camelCase', () => {
    const legacy = message({
      id: 'old',
      metadata: {
        type: 'permission_request',
        tool_name: 'Bash',
        input_preview: 'rm -rf /tmp/x',
        description: 'clean up',
        status: 'allow_always',
      },
    })

    const [item] = buildTrajectory([legacy], [])

    expect(item.label).toBe('Bash')
    expect(item.raw.inputPreview).toBe('rm -rf /tmp/x')
    expect(item.raw.status).toBe('auto_allowed')
  })

  it('falls back to a name and a pending status when the request says neither', () => {
    const bare = message({ id: 'bare', metadata: { type: 'permission_request' } })

    const [item] = buildTrajectory([bare], [])

    expect(item.label).toBe('Permission Request')
    expect(item.raw.status).toBe('pending')
    expect(item.preview).toBe('Permission Request')
  })

  it('describes an elicitation by what it asks for', () => {
    const form = message({
      id: 'f',
      createdAt: at(0),
      metadata: { type: 'elicitation_request', mode: 'form', message: 'Which branch?', requestedSchema: { properties: {} }, content: 'main' },
    })
    const url = message({
      id: 'u',
      createdAt: at(1),
      metadata: { type: 'elicitation_request', mode: 'url', url: 'https://example.com', status: 'accept' },
    })

    const items = buildTrajectory([form, url], [])

    expect(items[0].laneLabel).toBe('ASK')
    expect(items[0].label).toBe('Which branch?')
    expect(JSON.parse(items[0].raw.inputPreview)).toEqual({ mode: 'form', requestedSchema: { properties: {} }, answer: 'main' })
    expect(items[1].label).toBe('Open link')
    expect(items[1].raw.status).toBe('accept')
    expect(JSON.parse(items[1].raw.inputPreview)).toEqual({ mode: 'url', url: 'https://example.com' })
  })

  it('names an elicitation that asks a question without a mode', () => {
    const ask = message({ id: 'a', metadata: { type: 'elicitation_request' } })

    expect(buildTrajectory([ask], [])[0].label).toBe('Answer question')
  })

  it('says so when a message or a tool call has nothing to preview', () => {
    const items = buildTrajectory(
      [message({ id: 'blank', text: '' })],
      [toolCall({ inputPreview: '', description: '' })],
    )

    expect(items.find((it) => it.id === 'm-blank').preview).toBe('(no text)')
    expect(items.find((it) => it.id === 't-tc1').preview).toBe('Bash')
  })

  it('says so when reasoning arrives empty', () => {
    expect(buildTrajectory([thought('   ')], [])[0].preview).toBe('(empty)')
  })

  it('clips a long preview to one line', () => {
    const items = buildTrajectory([message({ text: 'word '.repeat(100) })], [])

    expect(items[0].preview).toHaveLength(141)
    expect(items[0].preview.endsWith('…')).toBe(true)
  })

  it('describes a tool call by its description when it has no input', () => {
    const items = buildTrajectory([], [toolCall({ inputPreview: '', description: 'list the files' })])

    expect(items[0].preview).toBe('Bash list the files')
  })

  it('has nothing to show for a task with no activity', () => {
    expect(buildTrajectory(undefined, undefined)).toEqual([])
    expect(buildTrajectory([], [])).toEqual([])
  })
})

describe('trajectoryLaneCounts', () => {
  it('counts what each category holds', () => {
    const items = buildTrajectory([
      message({ id: 'a', sender: 'human', createdAt: at(0) }),
      message({ id: 'b', sender: 'agent', createdAt: at(1) }),
      thought('one', { id: 'c', createdAt: at(2) }),
      plan([{ content: 'step', status: 'pending' }], { id: 'd', createdAt: at(3) }),
    ], [toolCall({ id: 'e', createdAt: at(4) })])

    expect(trajectoryLaneCounts(items)).toEqual({ input: 1, agent: 1, thought: 2, tool: 1 })
  })

  it('counts an empty trajectory as empty rather than absent', () => {
    expect(trajectoryLaneCounts([])).toEqual({ input: 0, agent: 0, thought: 0, tool: 0 })
    expect(trajectoryLaneCounts(undefined)).toEqual({ input: 0, agent: 0, thought: 0, tool: 0 })
  })

  it('ignores an entry filed under no lane at all', () => {
    expect(trajectoryLaneCounts([{ lane: 'nowhere' }, null])).toEqual({
      input: 0, agent: 0, thought: 0, tool: 0,
    })
  })

  it('has a count for every lane the panel offers', () => {
    const counts = trajectoryLaneCounts([])

    expect(Object.keys(counts)).toEqual(TRAJECTORY_LANES.map((lane) => lane.key))
  })
})

describe('filterTrajectory', () => {
  const items = buildTrajectory([
    message({ id: 'a', sender: 'human', text: 'deploy the backend', createdAt: at(0) }),
    thought('the backend needs a migration first', { id: 'b', createdAt: at(1) }),
  ], [toolCall({ id: 'c', toolName: 'Bash', inputPreview: 'make deploy', description: 'ship it', createdAt: at(2) })])

  it('shows everything when nothing is asked of it', () => {
    expect(filterTrajectory(items)).toHaveLength(3)
    expect(filterTrajectory(items, {})).toHaveLength(3)
  })

  it('narrows to one category', () => {
    expect(filterTrajectory(items, { lane: 'thought' }).map((it) => it.id)).toEqual(['m-b'])
  })

  it('searches the label, the preview and the description', () => {
    expect(filterTrajectory(items, { query: 'migration' }).map((it) => it.id)).toEqual(['m-b'])
    expect(filterTrajectory(items, { query: 'bash' }).map((it) => it.id)).toEqual(['t-c'])
    expect(filterTrajectory(items, { query: 'ship it' }).map((it) => it.id)).toEqual(['t-c'])
  })

  it('ignores case and surrounding space in the search', () => {
    expect(filterTrajectory(items, { query: '  DEPLOY  ' })).toHaveLength(2)
  })

  it('applies the category and the search together', () => {
    expect(filterTrajectory(items, { lane: 'input', query: 'deploy' }).map((it) => it.id)).toEqual(['m-a'])
    expect(filterTrajectory(items, { lane: 'input', query: 'migration' })).toEqual([])
  })

  it('has nothing to filter when there is nothing there', () => {
    expect(filterTrajectory(undefined, { query: 'anything' })).toEqual([])
  })
})

describe('defaultDetailTab', () => {
  // What a reader came to a thought for is the text; the summary of one knows
  // only when it was written.
  it('opens reasoning and plans on their content', () => {
    expect(defaultDetailTab({ lane: 'thought' })).toBe('content')
  })

  it('opens everything else on its summary', () => {
    expect(defaultDetailTab({ lane: 'tool' })).toBe('summary')
    expect(defaultDetailTab({ lane: 'agent' })).toBe('summary')
    expect(defaultDetailTab(null)).toBe('summary')
  })
})
