import { describe, it, expect, vi } from 'vitest'

import {
  MAX_RECENT_WORKSPACES,
  buildTrayMenuTemplate,
  createRecentWorkspaces,
  trayTooltip,
} from '../src/main/tray.js'
import { findMenuItems } from '../src/main/menu.js'

describe('createRecentWorkspaces', () => {
  it('records activity', () => {
    const recent = createRecentWorkspaces()
    expect(recent.touch('ws1', 'Alpha')).toEqual([{ id: 'ws1', name: 'Alpha' }])
  })

  it('orders by most recent, not by name', () => {
    // The tray is a shortcut to what is happening now; a fixed order would
    // make it a worse version of the sidebar.
    const recent = createRecentWorkspaces()
    recent.touch('ws1', 'Alpha')
    recent.touch('ws2', 'Beta')

    expect(recent.list.map((w) => w.id)).toEqual(['ws2', 'ws1'])
  })

  it('moves a repeat visitor to the front without duplicating it', () => {
    const recent = createRecentWorkspaces()
    recent.touch('ws1', 'Alpha')
    recent.touch('ws2', 'Beta')
    recent.touch('ws1', 'Alpha')

    expect(recent.list.map((w) => w.id)).toEqual(['ws1', 'ws2'])
  })

  it('keeps the list short enough to scan', () => {
    const recent = createRecentWorkspaces()
    for (let i = 0; i < 12; i += 1) recent.touch(`ws${i}`, `W${i}`)

    expect(recent.list).toHaveLength(MAX_RECENT_WORKSPACES)
    expect(recent.list[0].id).toBe('ws11')
  })

  it('honours a custom limit', () => {
    const recent = createRecentWorkspaces({ limit: 2 })
    recent.touch('a')
    recent.touch('b')
    recent.touch('c')

    expect(recent.list.map((w) => w.id)).toEqual(['c', 'b'])
  })

  it('ignores activity with no workspace', () => {
    const recent = createRecentWorkspaces()
    expect(recent.touch(undefined, 'Nameless')).toEqual([])
  })

  it('fills in a name that was unknown at the time', () => {
    // Activity can arrive before the workspace list has been fetched.
    const recent = createRecentWorkspaces()
    recent.touch('ws1')

    expect(recent.rename('ws1', 'Alpha')).toEqual([{ id: 'ws1', name: 'Alpha' }])
    expect(recent.rename('unknown', 'x')).toEqual([{ id: 'ws1', name: 'Alpha' }])
  })

  it('stores an empty name rather than undefined', () => {
    const recent = createRecentWorkspaces()
    expect(recent.touch('ws1')).toEqual([{ id: 'ws1', name: '' }])
  })
})

describe('buildTrayMenuTemplate', () => {
  const actions = { open: vi.fn(), newTask: vi.fn(), openWorkspace: vi.fn(), quit: vi.fn() }

  it('leads with the unread count, which is why anyone looks', () => {
    expect(buildTrayMenuTemplate({ unreadCount: 3, actions })[0]).toMatchObject({
      id: 'status',
      label: '3 unread',
      enabled: false,
    })
  })

  it('says so plainly when there is nothing waiting', () => {
    expect(buildTrayMenuTemplate({ unreadCount: 0, actions })[0].label).toBe('No unread notifications')
  })

  it('always offers open, new task and quit', () => {
    const template = buildTrayMenuTemplate({ actions })

    for (const id of ['open', 'new-task', 'quit']) {
      expect(findMenuItems(template, id)).toHaveLength(1)
    }
  })

  it('lists recent workspaces when there are any', () => {
    const template = buildTrayMenuTemplate({
      workspaces: [{ id: 'ws1', name: 'Alpha' }, { id: 'ws2', name: 'Beta' }],
      actions,
    })

    expect(findMenuItems(template, 'workspace-ws1')[0].label).toBe('Alpha')
    expect(findMenuItems(template, 'workspace-ws2')[0].label).toBe('Beta')
    expect(findMenuItems(template, 'recent-heading')).toHaveLength(1)
  })

  it('omits the recent section entirely when there is nothing to list', () => {
    const template = buildTrayMenuTemplate({ workspaces: [], actions })
    expect(findMenuItems(template, 'recent-heading')).toEqual([])
  })

  it('gives an unnamed workspace a usable label rather than a blank line', () => {
    const template = buildTrayMenuTemplate({ workspaces: [{ id: 'ws1', name: '' }], actions })
    expect(findMenuItems(template, 'workspace-ws1')[0].label).toBe('Untitled workspace')
  })

  it('wires each action', () => {
    const open = vi.fn()
    const newTask = vi.fn()
    const openWorkspace = vi.fn()
    const quit = vi.fn()
    const template = buildTrayMenuTemplate({
      workspaces: [{ id: 'ws1', name: 'Alpha' }],
      actions: { open, newTask, openWorkspace, quit },
    })

    findMenuItems(template, 'open')[0].click()
    findMenuItems(template, 'new-task')[0].click()
    findMenuItems(template, 'workspace-ws1')[0].click()
    findMenuItems(template, 'quit')[0].click()

    expect(open).toHaveBeenCalledOnce()
    expect(newTask).toHaveBeenCalledOnce()
    expect(openWorkspace).toHaveBeenCalledWith('ws1')
    expect(quit).toHaveBeenCalledOnce()
  })

  it('does not throw when no actions are supplied', () => {
    const template = buildTrayMenuTemplate({ workspaces: [{ id: 'ws1', name: 'Alpha' }] })
    expect(() => findMenuItems(template, 'workspace-ws1')[0].click()).not.toThrow()
  })
})

describe('trayTooltip', () => {
  it('is just the name when nothing is waiting', () => {
    expect(trayTooltip(0)).toBe('AgentRQ')
  })

  it('carries the count when something is', () => {
    expect(trayTooltip(4)).toBe('AgentRQ — 4 unread notifications')
  })

  it('gets the singular right', () => {
    expect(trayTooltip(1)).toBe('AgentRQ — 1 unread notification')
  })
})
