// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

import { SUPERVISOR_TOOLS, WORKSPACE_TOOLS, serverTools } from '../../src/main/extensions/servers.js'
import { checkCompatibility, parseManifest } from '../../src/main/extensions/manifest.js'
import standup from '../../../examples/extensions/standup/agentrq-extension.json'
import digest from '../../../examples/extensions/digest/agentrq-extension.json'
import taskStats from '../../../examples/extensions/task-stats/agentrq-extension.json'

/**
 * The lists are written down here and registered in Go, so the only thing worth
 * testing is that the two agree. A tool added on one side has to fail on the
 * other or the install screen starts lying about what an extension can have.
 *
 * Same arrangement as `frontend/test/workspaceSettings.test.js`, which guards
 * the settings snippet against the same source.
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
    expect(SUPERVISOR_TOOLS).toContain('listAllTasks')
    expect(SUPERVISOR_TOOLS).toContain('listTasks')
  })

  it('cannot be edited by whoever it is handed to', () => {
    expect(Object.isFrozen(SUPERVISOR_TOOLS)).toBe(true)
  })
})

describe('serverTools', () => {
  it('answers in the shape checkCompatibility takes', () => {
    const tools = serverTools('0.5.21')

    expect(tools.appVersion).toBe('0.5.21')
    expect(tools.workspaceTools).toEqual([...WORKSPACE_TOOLS])
    expect(tools.supervisorTools).toEqual([...SUPERVISOR_TOOLS])
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
