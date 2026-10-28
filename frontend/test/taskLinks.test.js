// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest'
import { classifyLink, linksInText, taskLinks } from '../src/composables/useTaskLinks'

const origin = 'https://app.agentrq.com'

describe('linksInText', () => {
  it('reads markdown links and bare URLs, but not URLs inside code', () => {
    const text = 'See https://github.com/a/b/pull/12. Also [the doc](/pano/d1) and `https://example.com/code`.'
    expect(linksInText(text)).toEqual([
      { href: 'https://github.com/a/b/pull/12', text: 'https://github.com/a/b/pull/12' },
      { href: '/pano/d1', text: 'the doc' },
    ])
  })

  it('returns nothing for empty text', () => {
    expect(linksInText('')).toEqual([])
    expect(linksInText(undefined)).toEqual([])
  })
})

describe('classifyLink', () => {
  it('recognises a GitHub pull request, and its files tab', () => {
    expect(classifyLink({ href: 'https://github.com/agentrq/agentrq/pull/1099/files' }, origin)).toEqual({
      kind: 'pr', key: 'agentrq/agentrq#1099', href: 'https://github.com/agentrq/agentrq/pull/1099/files', owner: 'agentrq', repo: 'agentrq', number: '1099',
    })
    expect(classifyLink({ href: 'https://www.github.com/a/b/pull/7' }, origin).kind).toBe('pr')
  })

  it('treats any other GitHub page as external', () => {
    expect(classifyLink({ href: 'https://github.com/a/b/issues/7' }, origin)).toMatchObject({ kind: 'external', label: 'github.com/a/b/issues/7' })
  })

  it('recognises tasks on this server, absolute or relative, in both route shapes', () => {
    expect(classifyLink({ href: `${origin}/workspaces/w1/tasks/t2`, text: 'Fix login' }, origin))
      .toEqual({ kind: 'task', key: 'w1/t2', to: '/workspaces/w1/tasks/t2', taskId: 't2', label: 'Fix login' })
    expect(classifyLink({ href: '/tasks/ongoing/w1/t3/' }, origin)).toMatchObject({ kind: 'task', key: 'w1/t3', label: 't3' })
    expect(classifyLink({ href: '/workspaces/w1/tasks/new' }, origin).kind).toBe('external')
  })

  it('recognises Pano docs on this server, but not its new or chat pages', () => {
    expect(classifyLink({ href: '/pano/d1?x=1#top', text: 'Roadmap' }, origin))
      .toEqual({ kind: 'pano', key: 'd1', to: '/pano/d1?x=1#top', label: 'Roadmap' })
    expect(classifyLink({ href: `${origin}/pano/d2`, text: `${origin}/pano/d2` }, origin)).toMatchObject({ kind: 'pano', label: 'd2' })
    expect(classifyLink({ href: '/pano/new' }, origin).kind).toBe('external')
    expect(classifyLink({ href: '/pano/chat' }, origin).kind).toBe('external')
  })

  it('treats a task path on another server as external', () => {
    expect(classifyLink({ href: 'https://other.example/workspaces/w1/tasks/t2' }, origin))
      .toMatchObject({ kind: 'external', label: 'other.example/workspaces/w1/tasks/t2' })
  })

  it('labels an external link by its text, or its host and path', () => {
    expect(classifyLink({ href: 'https://example.com/', text: '' }, origin).label).toBe('example.com')
    expect(classifyLink({ href: 'https://example.com/a', text: ' Docs ' }, origin).label).toBe('Docs')
  })

  it('skips what is not a web link, and what is not a URL at all', () => {
    expect(classifyLink({ href: 'mailto:hi@example.com' }, origin)).toBeNull()
    expect(classifyLink({ href: 'file:///tmp/plan.md' }, origin)).toBeNull()
    expect(classifyLink({ href: 'https://[bad' }, origin)).toBeNull()
  })
})

describe('taskLinks', () => {
  const kinds = (result) => result.groups.map((g) => [g.kind, g.links.map((l) => l.label)])

  it('groups the body and messages, de-duplicated, leaving out the task itself', () => {
    const task = {
      body: 'Ship https://github.com/agentrq/agentrq/pull/12 and read [Roadmap](/pano/d1).',
      messages: [
        { text: 'Opened https://github.com/agentrq/agentrq/pull/12 again, see https://example.com/x' },
        { text: `Related: [Login](${origin}/workspaces/w1/tasks/t2), this one ${origin}/workspaces/w1/tasks/t1` },
        { text: 'mailto:hi@example.com and [Roadmap again](/pano/d1)' },
        {},
      ],
    }
    const result = taskLinks(task, { origin, taskId: 't1' })
    expect(result.total).toBe(4)
    expect(kinds(result)).toEqual([
      ['pr', ['gh#12']],
      ['pano', ['Roadmap']],
      ['task', ['Login']],
      ['external', ['example.com/x']],
    ])
    expect(result.groups[0].label).toBe('Pull requests')
  })

  it('names the repository once pull requests come from more than one', () => {
    const task = { body: 'https://github.com/a/front/pull/1 https://github.com/a/back/pull/2' }
    expect(kinds(taskLinks(task, { origin }))).toEqual([['pr', ['front#1', 'back#2']]])
  })

  it('has nothing for a task without links, or without a task', () => {
    expect(taskLinks({ body: 'no links here', messages: [] }, { origin })).toEqual({ total: 0, groups: [] })
    expect(taskLinks(null, { origin })).toEqual({ total: 0, groups: [] })
  })

  it('reads a message again only when its text changes', () => {
    const message = { text: 'https://example.com/a' }
    const first = taskLinks({ messages: [message] }, { origin }).groups[0].links[0]
    expect(taskLinks({ messages: [message] }, { origin }).groups[0].links[0]).toEqual(first)
    message.text = 'https://example.com/b https://example.com/c'
    expect(kinds(taskLinks({ messages: [message] }, { origin }))).toEqual([['external', ['example.com/b', 'example.com/c']]])
  })
})
