// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { mcpPost, workspaceToken } from '../lib/mcp.js'
import { apiPort } from '../lib/settings.js'

// The workspace server serves the AgentRQ workspace skill over the MCP Skills
// extension, so an agent with no plugin installed still gets it. This walks
// what such an agent does: find the skill, read its SKILL.md, follow each of
// its links to a reference file, and read one with the skills tool too, as
// help task.reply's guide link sends it to.

const origin = `http://localhost:${apiPort}`
const skillURI = 'skill://agentrq/SKILL.md'

test('an agent reads the workspace skill and every reference it links, over MCP', async ({ page, workspace }) => {
  const token = await workspaceToken(page, workspace.id)
  const init = await mcpPost(origin, workspace.id, token, {
    jsonrpc: '2.0', id: 1, method: 'initialize',
    params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'agentrq-qa', version: '0' } },
  })
  expect(init.status, 'initialize').toBe(200)
  expect(init.message.result.capabilities.extensions, 'the Skills extension is advertised')
    .toEqual({ 'io.modelcontextprotocol/skills': { directoryRead: true } })
  const session = init.session
  await mcpPost(origin, workspace.id, token, { jsonrpc: '2.0', method: 'notifications/initialized' }, { session })
  let id = 1
  const rpc = async (method, params) => {
    const res = await mcpPost(origin, workspace.id, token, { jsonrpc: '2.0', id: ++id, method, params }, { session })
    expect(res.status, `${method} ${JSON.stringify(params)}`).toBe(200)
    expect(res.message.error, `${method} ${JSON.stringify(params)}`).toBeUndefined()
    return res.message.result
  }

  // A client that knows the extension lists the skill with every file in it.
  const { skills } = await rpc('skills/list', {})
  expect(skills.map((s) => s.uri), 'skills/list').toEqual([skillURI])
  const manifest = skills[0].resources.map((r) => r.uri)

  // Any client sees the same files among the resources.
  const { resources } = await rpc('resources/list', {})
  const listed = resources.map((r) => r.uri).filter((uri) => uri.startsWith('skill://'))
  expect(listed.sort(), 'the skill files are resources').toEqual([...manifest].sort())

  const read = async (uri) => {
    const { contents } = await rpc('resources/read', { uri })
    expect(contents[0].mimeType, uri).toBe('text/markdown')
    return contents[0].text
  }
  const skill = await read(skillURI)
  expect(skill, 'the SKILL.md').toContain('name: agentrq')

  // Each link resolves against the SKILL.md's URI to a file the agent reads.
  const links = [...skill.matchAll(/\]\(([^)]+)\)/g)].map((m) => m[1])
  expect(links, 'the references SKILL.md links').toEqual([
    'references/task.md', 'references/memory.md', 'references/skills.md',
    'references/workspace.md', 'references/pano.md', 'references/siteTools.md',
  ])
  for (const link of links) {
    const uri = new URL(link, skillURI).href
    expect(manifest, `${link} resolves to a served file`).toContain(uri)
    const text = await read(uri)
    const tool = link.slice('references/'.length, -'.md'.length)
    expect(text.startsWith(`# ${tool}\n`), `${uri} is the ${tool} reference`).toBe(true)
    expect(text, `${uri} has examples`).toContain('Examples:\n```json\n{ "action": ')
  }

  // The skills tool reads them too when the account has no agentrq skill.
  const loaded = await rpc('tools/call', { name: 'skills', arguments: { action: 'load', payload: { uri: 'skill://agentrq/references/a2ui.md' } } })
  expect(loaded.content[0].text, 'the skills tool on a built-in file').toBe(await read('skill://agentrq/references/a2ui.md'))
})
