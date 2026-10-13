// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

import {
  RETIRED_TOOLS,
  SUPERVISOR_ACTIONS,
  SUPERVISOR_TOOLS,
  WORKSPACE_ACTIONS,
  WORKSPACE_TOOLS,
  currentCall,
  currentToolName,
  serverTools,
} from '../../src/main/extensions/servers.js'
import { checkCompatibility, parseManifest } from '../../src/main/extensions/manifest.js'
import standup from '../../../examples/extensions/standup/agentrq-extension.json'
import digest from '../../../examples/extensions/digest/agentrq-extension.json'
import taskStats from '../../../examples/extensions/task-stats/agentrq-extension.json'

/**
 * The lists are written down here and registered in Go, so the only thing worth
 * testing is that the two agree. A tool added on one side has to fail on the
 * other or the install screen starts lying about what an extension can have.
 */

const REPO = join(dirname(fileURLToPath(import.meta.url)), '../../..')

/** `mcp.AddTool(mcpSrv, &mcp.Tool{ Name: "x"` — the per-workspace server. */
const WORKSPACE_RE = /mcp\.AddTool\(mcpSrv, &mcp\.Tool\{\s*Name:\s*"([^"]+)"/g

/** `mcp.AddTool(s.server, &mcp.Tool{ Name: "x"` — the supervisor. */
const SUPERVISOR_RE = /mcp\.AddTool\(s\.server, &mcp\.Tool\{\s*Name:\s*"([^"]+)"/g

/**
 * Every non-test Go file in a package, not one named file.
 *
 * The supervisor registers across `server.go`, `events.go` and `workflows.go`,
 * and a check that read only the first would miss two thirds of the surface —
 * which is exactly the blind spot the plugin documentation test had.
 */
function registeredUnder(dir, re) {
  return readdirSync(join(REPO, dir))
    .filter((name) => name.endsWith('.go') && !name.endsWith('_test.go'))
    .sort()
    .flatMap((name) => [...readFileSync(join(REPO, dir, name), 'utf-8').matchAll(re)].map((m) => m[1]))
}

describe('WORKSPACE_TOOLS', () => {
  it('is exactly what the workspace server registers', () => {
    expect([...WORKSPACE_TOOLS].sort()).toEqual(
      registeredUnder('backend/internal/controller/mcp', WORKSPACE_RE).sort(),
    )
  })

  // Named on its own, because it is a decision rather than an omission: an
  // agent connected to a workspace acts on the task it was given.
  it('offers no way to list tasks', () => {
    expect(WORKSPACE_TOOLS).not.toContain('listTasks')
    expect(WORKSPACE_TOOLS).not.toContain('listAllTasks')
    expect(WORKSPACE_ACTIONS.task.filter((action) => action.startsWith('list'))).toEqual([])
  })

  it('cannot be edited by whoever it is handed to', () => {
    expect(Object.isFrozen(WORKSPACE_TOOLS)).toBe(true)
  })
})

describe('SUPERVISOR_TOOLS', () => {
  it('is exactly what the supervisor registers, across every file', () => {
    expect([...SUPERVISOR_TOOLS].sort()).toEqual(
      registeredUnder('backend/internal/handler/coremcp', SUPERVISOR_RE).sort(),
    )
  })

  it('is the surface that does list things', () => {
    // The asymmetry with the workspace server, stated so it reads as intended.
    expect(SUPERVISOR_ACTIONS.task).toContain('listAll')
    expect(SUPERVISOR_ACTIONS.task).toContain('list')
  })

  it('cannot be edited by whoever it is handed to', () => {
    expect(Object.isFrozen(SUPERVISOR_TOOLS)).toBe(true)
  })
})

/** `Name struct { Action string `json:"action" jsonschema:"enum: a, b"`` — an action tool's actions. */
function actionsIn(file, struct) {
  const re = new RegExp(`${struct} struct \\{\\s*Action\\s+string\\s+\`json:"action" jsonschema:"enum: ([^"]+)"\``)
  const match = re.exec(readFileSync(join(REPO, file), 'utf-8'))
  expect(match, `${struct} in ${file}`).not.toBeNull()
  return match[1].split(', ')
}

const WS = 'backend/internal/controller/mcp/'
const CORE = 'backend/internal/handler/coremcp/'

describe('the action tools', () => {
  it.each([
    ['workspace', WORKSPACE_ACTIONS, 'task', WS + 'task.go', 'TaskParams'],
    ['workspace', WORKSPACE_ACTIONS, 'memory', WS + 'tools.go', 'MemoryParams'],
    ['workspace', WORKSPACE_ACTIONS, 'skills', WS + 'tools.go', 'SkillsParams'],
    ['workspace', WORKSPACE_ACTIONS, 'workspace', WS + 'tools.go', 'WorkspaceParams'],
    ['workspace', WORKSPACE_ACTIONS, 'siteTools', WS + 'tools.go', 'SiteToolsParams'],
    ['workspace', WORKSPACE_ACTIONS, 'pano', WS + 'pano.go', 'PanoParams'],
    ['supervisor', SUPERVISOR_ACTIONS, 'workspace', CORE + 'tools.go', 'WorkspaceToolParams'],
    ['supervisor', SUPERVISOR_ACTIONS, 'task', CORE + 'task.go', 'TaskParams'],
    ['supervisor', SUPERVISOR_ACTIONS, 'memory', CORE + 'tools.go', 'MemoryToolParams'],
    ['supervisor', SUPERVISOR_ACTIONS, 'skills', CORE + 'tools.go', 'SkillsToolParams'],
    ['supervisor', SUPERVISOR_ACTIONS, 'events', CORE + 'tools.go', 'EventsToolParams'],
    ['supervisor', SUPERVISOR_ACTIONS, 'workflows', CORE + 'tools.go', 'WorkflowsToolParams'],
    ['supervisor', SUPERVISOR_ACTIONS, 'machines', CORE + 'tools.go', 'MachinesToolParams'],
    ['supervisor', SUPERVISOR_ACTIONS, 'pano', WS + 'pano.go', 'PanoParams'],
  ])('the %s server\'s %s offers exactly the actions its Go enum does', (_surface, actions, tool, file, struct) => {
    expect([...actions[tool]]).toEqual(actionsIn(file, struct))
  })

  it('are every tool each surface offers, but help', () => {
    expect(Object.keys(WORKSPACE_ACTIONS).sort()).toEqual(WORKSPACE_TOOLS.filter((tool) => tool !== 'help').sort())
    expect(Object.keys(SUPERVISOR_ACTIONS).sort()).toEqual(SUPERVISOR_TOOLS.filter((tool) => tool !== 'help').sort())
  })

  it.each([
    ['workspace', 'backend/internal/controller/mcp/tools.go'],
    ['supervisor', 'backend/internal/handler/coremcp/tools.go'],
  ])('retire on the %s surface exactly the names its legacyTools map does', (surface, file) => {
    const legacy = Object.fromEntries(
      [...readFileSync(join(REPO, file), 'utf-8').matchAll(/"(\w+)":\s*\{Tool: "(\w+)", Action: "(\w+)"/g)].map((m) => [
        m[1],
        `${m[2]}.${m[3]}`,
      ]),
    )
    expect({ ...RETIRED_TOOLS[surface] }).toEqual(legacy)
  })

  it('name a real action for every retired tool, which no server lists any more', () => {
    for (const [surface, actions, tools] of [
      ['workspace', WORKSPACE_ACTIONS, WORKSPACE_TOOLS],
      ['supervisor', SUPERVISOR_ACTIONS, SUPERVISOR_TOOLS],
    ]) {
      for (const [old, current] of Object.entries(RETIRED_TOOLS[surface])) {
        const [tool, action] = current.split('.')
        expect(actions[tool]).toContain(action)
        // getWorkspace on one surface is the tool workspace on neither.
        expect(tools).not.toContain(old)
      }
    }
  })
})

describe('currentToolName and currentCall', () => {
  it('read a retired name as the task action it became, and leave every other alone', () => {
    expect(currentToolName('supervisor', 'updateTaskOrder')).toBe('task.updateSortingOrder')
    expect(currentToolName('workspace', 'listTasks')).toBe('listTasks')
    expect(currentToolName('elsewhere', 'getTask')).toBe('getTask')
  })

  it('turn an old call into an action call, its arguments the payload and its workspace the address', () => {
    expect(currentCall('supervisor', 'createTask', { workspaceId: 'ws1', title: 't' })).toEqual({
      tool: 'task',
      args: { workspaceId: 'ws1', action: 'create', payload: { title: 't' } },
    })
    expect(currentCall('workspace', 'reply', { chatId: 't1', text: 'hi' })).toEqual({
      tool: 'task',
      args: { action: 'reply', payload: { taskId: 't1', text: 'hi' } },
    })
    // Only the workspace reply renames: the supervisor's always said taskId.
    expect(currentCall('supervisor', 'replyToTask', { taskId: 't1', text: 'hi' }).args.payload).toEqual({ taskId: 't1', text: 'hi' })
    expect(currentCall('workspace', 'getWorkspace', { workspaceId: 'ws1' })).toEqual({
      tool: 'workspace',
      args: { workspaceId: 'ws1', action: 'get', payload: {} },
    })
    expect(currentCall('workspace', 'help', { action: 'task' })).toEqual({ tool: 'help', args: { action: 'task' } })
    expect(currentCall('workspace', 'getTask').args).toEqual({ action: 'get', payload: {} })
  })
})

describe('serverTools', () => {
  it('answers in the shape checkCompatibility takes', () => {
    const tools = serverTools('0.5.21')

    expect(tools.appVersion).toBe('0.5.21')
    expect(tools.workspaceTools).toEqual([...WORKSPACE_TOOLS])
    expect(tools.supervisorTools).toEqual([...SUPERVISOR_TOOLS])
    expect(tools.workspaceActions).toBe(WORKSPACE_ACTIONS)
    expect(tools.supervisorActions).toBe(SUPERVISOR_ACTIONS)
  })

  it('hands over copies, so a caller cannot edit what everything else reads', () => {
    const tools = serverTools('0.5.21')
    tools.workspaceTools.push('listTasks')

    expect(WORKSPACE_TOOLS).not.toContain('listTasks')
  })
})

/**
 * The check that would have caught what shipped.
 *
 * The app passed only `appVersion`, so `workspaceTools` defaulted to empty and
 * every extension wanting any tool at all was refused with "the workspace
 * server does not offer …". Asserting the examples install is the shortest
 * statement of the thing that was broken.
 */
describe('the examples, judged the way the app judges them', () => {
  it.each([
    ['task-stats', taskStats],
    ['standup', standup],
    ['digest', digest],
  ])('%s can be installed', (_name, source) => {
    const { ok, manifest, reason } = parseManifest(source)
    expect(ok, reason).toBe(true)

    const { compatible, reasons } = checkCompatibility(manifest, serverTools('0.5.21'))

    expect(compatible, reasons.join(' ')).toBe(true)
  })

  it('still refuses one asking for something no server has', () => {
    const { manifest } = parseManifest({ ...standup, mcp: { workspace: ['listTasks'] } })

    const { compatible, reasons } = checkCompatibility(manifest, serverTools('0.5.21'))

    expect(compatible).toBe(false)
    expect(reasons[0]).toContain('does not offer "listTasks"')
  })
})
