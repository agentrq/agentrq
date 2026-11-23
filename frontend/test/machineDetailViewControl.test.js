// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Restarting and updating a machine's daemon from its page, which only a
 * daemon of 0.9.3 or newer can be asked to do.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h } from 'vue'

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'm1' } }),
  useRouter: () => ({ push: vi.fn() }),
}))
vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect() {}, disconnect() {}, onEvent() {} }),
}))

let machine
const restartDaemon = vi.fn()
const updateMachine = vi.fn()

vi.mock('../src/api', () => ({
  getMachine: () => Promise.resolve({ machine }),
  fetchMachineSessions: () => Promise.resolve({ sessions: [] }),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [] }),
  launchAgent: vi.fn(),
  fetchAcpAgents: () => Promise.resolve({ agents: [] }),
  fetchAcpModels: () => Promise.resolve({ agent: '', models: [] }),
  updateMachine: (...args) => updateMachine(...args),
  deleteMachine: vi.fn(),
  killSession: vi.fn(),
  restartDaemon: (...args) => restartDaemon(...args),
  API_BASE_URL: '/api/v1',
}))

const { default: MachineDetailView } = await import('../src/views/MachineDetailView.vue')
const { useToasts } = await import('../src/composables/useToasts')

const settle = () => new Promise((resolve) => setTimeout(resolve, 30))

let app
async function mount() {
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({ render: () => h(MachineDetailView) })
  app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', {}, slots.default?.()) })
  app.mount(el)
  await settle()
  const button = (label) => [...document.body.querySelectorAll('button')].find((b) => b.textContent.trim() === label)
  const click = async (label) => {
    button(label).click()
    await settle()
  }
  return { el, button, click }
}

const lastToast = () => useToasts().toasts.value.at(-1)

beforeEach(() => {
  machine = { id: 'm1', name: 'pi', enabled: true, online: true, os: 'linux', arch: 'arm64', version: '0.9.3' }
  restartDaemon.mockReset().mockResolvedValue(true)
})
afterEach(() => {
  app?.unmount()
  document.body.innerHTML = ''
})

describe('MachineDetailView: restarting the daemon', () => {
  it('asks, and says so, after a confirm that names what it stops', async () => {
    const page = await mount()
    await page.click('Settings')
    await page.click('Restart daemon')

    expect(document.body.textContent).toContain('Restart agentrqd')
    expect(document.body.textContent).toContain('Nothing is running on it')
    expect(restartDaemon).not.toHaveBeenCalled()

    // The confirm button, which says what it does rather than "Delete".
    const confirm = [...document.body.querySelectorAll('button')].filter((b) => b.textContent.trim() === 'Restart').at(-1)
    confirm.click()
    await settle()

    expect(restartDaemon).toHaveBeenCalledWith('m1', '')
    expect(lastToast().message).toContain('restarting')
  })

  it('says why when the machine refused', async () => {
    restartDaemon.mockRejectedValue(new Error('that machine is not connected'))
    const page = await mount()
    await page.click('Settings')
    await page.click('Restart daemon')
    ;[...document.body.querySelectorAll('button')].filter((b) => b.textContent.trim() === 'Restart').at(-1).click()
    await settle()

    expect(lastToast().message).toBe('that machine is not connected')
  })

  it('cannot be pressed while the machine is offline', async () => {
    machine.online = false
    const page = await mount()
    await page.click('Settings')
    expect(page.button('Restart daemon').disabled).toBe(true)
  })

  it('tells an older daemon to be updated by hand instead', async () => {
    machine.version = '0.9.2'
    machine.availableVersion = '0.9.3'
    const page = await mount()
    await page.click('Settings')

    expect(page.button('Restart daemon')).toBe(undefined)
    expect(page.button('Update daemon')).toBe(undefined)
    expect(page.el.textContent).toContain('by hand to 0.9.3 or newer')
  })
})

describe('MachineDetailView: updating the daemon, the same command', () => {
  it('installs the offered version after a confirm, from the banner', async () => {
    machine.availableVersion = '0.9.4'
    const page = await mount()
    await page.click('Update daemon')
    expect(document.body.textContent).toContain('Update agentrqd to 0.9.4')
    await page.click('Update')

    expect(restartDaemon).toHaveBeenCalledWith('m1', '0.9.4')
    expect(lastToast().message).toContain('updating')
  })

  it('is offered in Settings instead of a plain restart', async () => {
    machine.availableVersion = '0.9.4'
    const page = await mount()
    await page.click('Settings')
    expect(page.button('Restart daemon')).toBe(undefined)
    // The banner's button and the Settings one are the same command.
    const updates = [...page.el.querySelectorAll('button')].filter((b) => b.textContent.trim() === 'Update daemon')
    expect(updates).toHaveLength(2)
    updates[1].click()
    await settle()
    await page.click('Update')
    expect(restartDaemon).toHaveBeenCalledWith('m1', '0.9.4')
  })

  it('says why when the update was refused', async () => {
    machine.availableVersion = '0.9.4'
    restartDaemon.mockRejectedValue(new Error('that machine has not offered that update'))
    const page = await mount()
    await page.click('Update daemon')
    await page.click('Update')

    expect(lastToast().message).toBe('that machine has not offered that update')
  })
})

// A phone gets icons: a switch for Enable/Disable, a refresh icon for Update
// and Restart. Both are the same actions as the text buttons desktop shows.
describe('MachineDetailView: the phone controls', () => {
  it('toggles the machine off with the switch', async () => {
    updateMachine.mockReset().mockResolvedValue({ machine: { ...machine, enabled: false } })
    const page = await mount()
    await page.click('Settings')
    const toggle = page.el.querySelector('[role="switch"]')
    expect(toggle.getAttribute('aria-checked')).toBe('true')
    toggle.click()
    await settle()
    expect(updateMachine).toHaveBeenCalledWith('m1', { enabled: false })
  })

  it('names the icon-only update and restart buttons for a screen reader', async () => {
    const page = await mount()
    await page.click('Settings')
    expect(page.el.querySelector('[aria-label="Restart daemon"] svg')).not.toBe(null)
  })

  it('names the icon-only update buttons too, in the banner and in Settings', async () => {
    machine.availableVersion = '0.9.4'
    const page = await mount()
    expect(page.el.querySelectorAll('[aria-label="Update daemon"] svg')).toHaveLength(1)
    await page.click('Settings')
    expect(page.el.querySelectorAll('[aria-label="Update daemon"] svg')).toHaveLength(2)
  })
})

// Empty is the server's default, 0 is no cap, and a bad number is refused here.
describe('MachineDetailView: the agent limit', () => {
  async function save(page, value) {
    const input = page.el.querySelector('[data-test="machine-limit-input"]')
    input.value = value
    input.dispatchEvent(new Event('input'))
    await settle()
    input.form.requestSubmit()
    await settle()
  }

  it('says which limit applies', async () => {
    machine.sessionLimit = 8
    machine.maxSessions = null
    let page = await mount()
    await page.click('Settings')
    expect(page.el.querySelector('[data-test="machine-limit-text"]').textContent).toContain('8 agents at once')
    expect(page.el.textContent).toContain("The server's default.")
    app.unmount()

    machine.sessionLimit = 0
    machine.maxSessions = 0
    page = await mount()
    await page.click('Settings')
    expect(page.el.querySelector('[data-test="machine-limit-text"]').textContent).toContain('No limit')
    expect(page.el.querySelector('[data-test="machine-limit-input"]').value).toBe('0')
  })

  it('saves a number, and empty as the default', async () => {
    updateMachine.mockReset().mockResolvedValue({ machine: { ...machine, maxSessions: 3, sessionLimit: 3 } })
    const page = await mount()
    await page.click('Settings')
    await save(page, '3')
    expect(updateMachine).toHaveBeenLastCalledWith('m1', { maxSessions: 3 })
    expect(page.el.querySelector('[data-test="machine-limit-text"]').textContent).toContain('3 agents at once')
    expect(page.el.textContent).toContain('Set for this machine.')
    expect(lastToast().message).toBe('Agent limit saved')

    await save(page, '')
    expect(updateMachine).toHaveBeenLastCalledWith('m1', { maxSessions: null })
  })

  it('refuses a number that is not one, and says when the save fails', async () => {
    updateMachine.mockReset().mockRejectedValue(new Error('nope'))
    const page = await mount()
    await page.click('Settings')
    for (const bad of ['1.5', '-1']) {
      await save(page, bad)
      expect(updateMachine).not.toHaveBeenCalled()
    }

    await save(page, '2')
    expect(lastToast().message).toBe('nope')
  })

  // Under the server's ceiling a machine can only go lower, and never to "no cap".
  it("holds a machine's limit to the server's", async () => {
    machine.sessionCeiling = 8
    machine.sessionLimit = 8
    machine.maxSessions = null
    let page = await mount()
    await page.click('Settings')
    expect(page.el.textContent).toContain("The server's limit. Set a lower number for this machine.")
    for (const bad of ['9', '0']) {
      await save(page, bad)
      expect(updateMachine).not.toHaveBeenCalled()
    }
    app.unmount()

    machine.maxSessions = 3
    machine.sessionLimit = 3
    page = await mount()
    await page.click('Settings')
    expect(page.el.textContent).toContain("Set for this machine, up to the server's 8. Empty uses the server's.")
  })

  // agentrqd's own --max-per-profile holds the account too; the server cannot see it, so it is named.
  it("names the server's limit, this machine's and agentrqd's flag", async () => {
    const sources = (page) => page.el.querySelector('[data-test="machine-limit-sources"]').textContent.trim()
    const daemon = "agentrqd's --max-per-profile on the machine can also limit it; the lowest applies."
    Object.assign(machine, { sessionCeiling: 8, sessionLimit: 8, maxSessions: null })
    let page = await mount()
    await page.click('Settings')
    expect(sources(page)).toBe(`Server 8 · This machine not set. ${daemon}`)
    app.unmount()

    Object.assign(machine, { sessionCeiling: 0, sessionLimit: 0, maxSessions: 0 })
    page = await mount()
    await page.click('Settings')
    expect(sources(page)).toBe(`Server no limit · This machine no limit. ${daemon}`)
    app.unmount()

    Object.assign(machine, { sessionCeiling: 8, sessionLimit: 3, maxSessions: 3 })
    page = await mount()
    await page.click('Settings')
    expect(sources(page)).toBe(`Server 8 · This machine 3. ${daemon}`)
  })
})
