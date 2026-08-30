import { describe, it, expect, vi } from 'vitest'

import { buildMenuTemplate, findMenuItems } from '../src/main/menu.js'

const actions = { switchServer: vi.fn(), logOut: vi.fn() }

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

describe('findMenuItems', () => {
  it('searches nested submenus', () => {
    const template = [{ submenu: [{ submenu: [{ id: 'deep' }] }] }]
    expect(findMenuItems(template, 'deep')).toHaveLength(1)
  })

  it('returns nothing for an unknown id', () => {
    expect(findMenuItems(buildMenuTemplate({ platform: 'darwin', actions }), 'nope')).toEqual([])
  })
})
