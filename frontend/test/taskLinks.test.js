// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

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
      kind: 'pr', key: 'agentrq/agentrq#1099', href: 'https://github.com/agentrq/agentrq/pull/1099/files', mark: 'github', owner: 'agentrq', repo: 'agentrq', sigil: '#', number: '1099', label: 'agentrq#1099',
    })
    expect(classifyLink({ href: 'https://www.github.com/a/b/pull/7' }, origin).kind).toBe('pr')
  })

  it('recognises a GitHub issue, and a comment on it', () => {
    expect(classifyLink({ href: 'https://github.com/agentrq/agentrq/issues/1447#issuecomment-1' }, origin)).toEqual({
      kind: 'issue', key: 'agentrq/agentrq#1447', href: 'https://github.com/agentrq/agentrq/issues/1447#issuecomment-1', mark: 'github', owner: 'agentrq', repo: 'agentrq', sigil: '#', number: '1447', label: 'agentrq#1447',
    })
  })

  it('recognises a GitHub Actions run, and a job or attempt inside it, as the run', () => {
    expect(classifyLink({ href: 'https://github.com/agentrq/agentrq/actions/runs/19734520384' }, origin)).toEqual({
      kind: 'run', key: 'agentrq/agentrq#19734520384', href: 'https://github.com/agentrq/agentrq/actions/runs/19734520384', mark: 'github', owner: 'agentrq', repo: 'agentrq', sigil: '#', number: '19734520384', label: 'agentrq#19734520384',
    })
    expect(classifyLink({ href: 'https://github.com/a/b/actions/runs/5/job/6' }, origin)).toMatchObject({ kind: 'run', key: 'a/b#5' })
    expect(classifyLink({ href: 'https://github.com/a/b/actions/runs/5/attempts/2' }, origin)).toMatchObject({ kind: 'run', key: 'a/b#5' })
  })

  it('recognises a GitLab merge request, issue and pipeline, on gitlab.com or its own host, under subgroups', () => {
    expect(classifyLink({ href: 'https://gitlab.com/gitlab-org/gitlab/-/merge_requests/123/diffs' }, origin)).toEqual({
      kind: 'pr', key: 'gitlab.com/gitlab-org/gitlab!123', href: 'https://gitlab.com/gitlab-org/gitlab/-/merge_requests/123/diffs', mark: 'gitlab', owner: 'gitlab-org', repo: 'gitlab', number: '123', sigil: '!', label: 'gitlab!123',
    })
    expect(classifyLink({ href: 'https://gitlab.com/a/b/-/issues/4#note_1' }, origin)).toMatchObject({ kind: 'issue', key: 'gitlab.com/a/b#4', mark: 'gitlab', label: 'b#4' })
    expect(classifyLink({ href: 'https://gitlab.com/a/b/-/work_items/5' }, origin)).toMatchObject({ kind: 'issue', label: 'b#5' })
    expect(classifyLink({ href: 'https://git.example.com/group/sub/proj/-/pipelines/678' }, origin)).toMatchObject({
      kind: 'run', key: 'git.example.com/group/sub/proj#678', owner: 'group/sub', repo: 'proj', number: '678', label: 'proj#678',
    })
  })

  it('treats any other GitLab page, and a job, as external', () => {
    expect(classifyLink({ href: 'https://gitlab.com/a/b/-/merge_requests' }, origin).kind).toBe('external')
    expect(classifyLink({ href: 'https://gitlab.com/a/b/-/issues/new' }, origin).kind).toBe('external')
    expect(classifyLink({ href: 'https://gitlab.com/a/b/-/jobs/9' }, origin).kind).toBe('external')
    expect(classifyLink({ href: 'https://gitlab.com/a/b' }, origin).kind).toBe('external')
  })

  it('treats any other GitHub page as external', () => {
    expect(classifyLink({ href: 'https://github.com/a/b/issues' }, origin)).toMatchObject({ kind: 'external', label: 'github.com/a/b/issues' })
    expect(classifyLink({ href: 'https://github.com/a/b/issues/new' }, origin).kind).toBe('external')
    expect(classifyLink({ href: 'https://github.com/a/b/actions' }, origin).kind).toBe('external')
    expect(classifyLink({ href: 'https://github.com/a/b/actions/workflows/ci.yml' }, origin).kind).toBe('external')
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
      ['pr', ['agentrq#12']],
      ['pano', ['Roadmap']],
      ['task', ['Login']],
      ['external', ['example.com/x']],
    ])
    expect(result.groups[0].label).toBe('Pull requests')
  })

  it('lists the Pano docs linked to the task first, by title, mentioned or not', () => {
    const task = { body: `Read [${origin}/pano/d2](${origin}/pano/d2) and [Roadmap](/pano/d1).` }
    const panoDocs = [{ id: 'd2', title: 'Spec' }, { id: 'd3', title: '' }, { id: 'd2', title: 'Spec again' }]
    const result = taskLinks(task, { origin, panoDocs })
    expect(kinds(result)).toEqual([['pano', ['Spec', 'd3', 'Roadmap']]])
    expect(result.groups[0].links[0]).toEqual({ kind: 'pano', key: 'd2', to: '/pano/d2', label: 'Spec' })
    expect(taskLinks({}, { origin, panoDocs: [{ id: 'd4', title: 'Plan' }] }).total).toBe(1)
  })

  it('labels pull requests and issues by repository and number, each in its own group', () => {
    const task = { body: 'https://github.com/a/front/pull/1 fixes https://github.com/a/back/issues/2 and https://github.com/a/back/issues/2' }
    const result = taskLinks(task, { origin })
    expect(kinds(result)).toEqual([['pr', ['front#1']], ['issue', ['back#2']]])
    expect(result.groups[1].label).toBe('Issues')
  })

  it('lists Actions runs after issues, a run and its jobs once, apart from a pull request of the same number', () => {
    const task = {
      body: 'https://github.com/a/back/actions/runs/7/job/70 failed on https://github.com/a/back/pull/7',
      messages: [{ text: 'Rerun: https://github.com/a/back/actions/runs/7 and https://github.com/a/back/issues/3' }],
    }
    const result = taskLinks(task, { origin })
    expect(kinds(result)).toEqual([['pr', ['back#7']], ['issue', ['back#3']], ['run', ['back#7']]])
    expect(result.groups[2].label).toBe('Actions')
    expect(result.groups[2].links[0].href).toBe('https://github.com/a/back/actions/runs/7/job/70')
  })

  it('groups GitLab merge requests, issues and pipelines with GitHub\'s, apart from a GitHub repository of the same name', () => {
    const task = {
      body: 'https://gitlab.com/a/back/-/merge_requests/7 and https://github.com/a/back/pull/7',
      messages: [{ text: 'https://gitlab.com/a/back/-/pipelines/9 for https://gitlab.com/a/back/-/issues/3, https://gitlab.com/a/back/-/merge_requests/7/diffs' }],
    }
    expect(kinds(taskLinks(task, { origin }))).toEqual([['pr', ['back!7', 'back#7']], ['issue', ['back#3']], ['run', ['back#9']]])
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
