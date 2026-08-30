import { describe, it, expect, vi } from 'vitest'

import { QUICK_CREATE_ACCELERATOR, buildMenuTemplate, findMenuItems } from '../src/main/menu.js'

const actions = { switchServer: vi.fn(), logOut: vi.fn(), newTask: vi.fn(), checkForUpdates: vi.fn() }

describe('buildMenuTemplate', () => {
  it('offers switch-server and log-out on macOS', () => {
    const template = buildMenuTemplate({ platform: 'darwin', actions })

    expect(findMenuItems(template, 'switch-server')).toHaveLength(1)
    expect(findMenuItems(template, 'log-out')).toHaveLength(1)
  })

  it('offers them on Windows and Linux too', () => {
    // These are the only way to reach either action, so a platform that lost
    // them would strand the user on one server.
    for (const platform of ['win32', 'linux']) {
      const template = buildMenuTemplate({ platform, actions })
      expect(findMenuItems(template, 'switch-server')).toHaveLength(1)
      expect(findMenuItems(template, 'log-out')).toHaveLength(1)
    }
  })

  it('puts account actions in the application menu on macOS, and under File elsewhere', () => {
    const mac = buildMenuTemplate({ platform: 'darwin', appName: 'AgentRQ', actions })
    expect(mac[0].label).toBe('AgentRQ')
    expect(findMenuItems([mac[0]], 'switch-server')).toHaveLength(1)

    const linux = buildMenuTemplate({ platform: 'linux', actions })
    const file = linux.find((item) => item.label === 'File')
    expect(findMenuItems([file], 'switch-server')).toHaveLength(1)
  })

  it('has no application menu on Windows and Linux', () => {
    const template = buildMenuTemplate({ platform: 'win32', actions })
    expect(template.map((item) => item.label)).not.toContain('AgentRQ')
  })

  it('wires each action to its handler', () => {
    const switchServer = vi.fn()
    const logOut = vi.fn()
    const template = buildMenuTemplate({ platform: 'darwin', actions: { switchServer, logOut } })

    findMenuItems(template, 'switch-server')[0].click()
    findMenuItems(template, 'log-out')[0].click()

    expect(switchServer).toHaveBeenCalledOnce()
    expect(logOut).toHaveBeenCalledOnce()
  })

  it('keeps quit reachable on every platform', () => {
    for (const platform of ['darwin', 'win32', 'linux']) {
      const template = buildMenuTemplate({ platform, actions })
      const roles = JSON.stringify(template)
      expect(roles).toContain('"role":"quit"')
    }
  })

  it('defaults its name and tolerates no actions at all', () => {
    const template = buildMenuTemplate({ platform: 'darwin' })

    expect(template[0].label).toBe('AgentRQ')
    expect(findMenuItems(template, 'switch-server')[0].click).toBeUndefined()
  })
})

describe('buildMenuTemplate — AgentRQ actions', () => {
  it('offers New Task on every platform, with the global accelerator', () => {
    for (const platform of ['darwin', 'win32', 'linux']) {
      const item = findMenuItems(buildMenuTemplate({ platform, actions }), 'new-task')[0]
      expect(item, platform).toBeTruthy()
      expect(item.accelerator).toBe(QUICK_CREATE_ACCELERATOR)
    }
  })

  it('keeps the quick-create shortcut clear of the standard new-window binding', () => {
    // It is registered globally, so it must not collide with Cmd/Ctrl+N.
    expect(QUICK_CREATE_ACCELERATOR).toBe('CommandOrControl+Shift+N')
  })

  it('offers Check for Updates on every platform', () => {
    for (const platform of ['darwin', 'win32', 'linux']) {
      expect(findMenuItems(buildMenuTemplate({ platform, actions }), 'check-for-updates'), platform)
        .toHaveLength(1)
    }
  })

  it('disables Check for Updates until an updater is wired in', () => {
    // Phase 6 supplies the handler. A menu item that silently does nothing is
    // worse than one that is visibly not ready.
    const without = buildMenuTemplate({ platform: 'darwin', actions: { switchServer: vi.fn() } })
    expect(findMenuItems(without, 'check-for-updates')[0].enabled).toBe(false)

    const with_ = buildMenuTemplate({ platform: 'darwin', actions })
    expect(findMenuItems(with_, 'check-for-updates')[0].enabled).toBe(true)
  })

  it('dispatches the AgentRQ actions', () => {
    const newTask = vi.fn()
    const checkForUpdates = vi.fn()
    const template = buildMenuTemplate({ platform: 'darwin', actions: { newTask, checkForUpdates } })

    findMenuItems(template, 'new-task')[0].click()
    findMenuItems(template, 'check-for-updates')[0].click()

    expect(newTask).toHaveBeenCalledOnce()
    expect(checkForUpdates).toHaveBeenCalledOnce()
  })

  it('gives Windows and Linux a Help menu, which macOS gets for free', () => {
    expect(buildMenuTemplate({ platform: 'linux', actions }).map((i) => i.label)).toContain('Help')
    expect(buildMenuTemplate({ platform: 'darwin', actions }).map((i) => i.label)).not.toContain('Help')
  })
})

describe('findMenuItems', () => {
  it('searches nested submenus', () => {
    const template = [{ submenu: [{ submenu: [{ id: 'deep' }] }] }]
    expect(findMenuItems(template, 'deep')).toHaveLength(1)
  })

  it('returns nothing for an unknown id', () => {
    expect(findMenuItems(buildMenuTemplate({ platform: 'darwin', actions }), 'nope')).toEqual([])
  })
})
