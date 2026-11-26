// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A scheduled task's attachments, on its own page, before it has ever run
 * (#1459): drawn under the prompt and opened in the shared preview.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/workspaces/ws1/tasks/t1/instances', params: { id: 'ws1', taskId: 't1' } }),
  useRouter: () => ({ push: vi.fn() }),
}))

let tasks = []
vi.mock('../src/api', async (importOriginal) => ({
  ...await importOriginal(),
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops' } }),
  fetchTasks: () => Promise.resolve({ tasks }),
  recordTelemetry: vi.fn(),
}))

const { default: ScheduledTaskInstancesView } = await import('../src/views/ScheduledTaskInstancesView.vue')

const cron = (attachments) => ({ id: 't1', title: 'Weekly check', status: 'cron', cronSchedule: '0 9 * * 1', body: 'Read the checklist.', attachments })
const settle = () => new Promise((r) => setTimeout(r, 30))
let app

async function mount() {
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({ render: () => h(ScheduledTaskInstancesView) })
  app.use(createPinia())
  app.mount(el)
  await settle()
  return el
}

const chip = (el, name) => [...el.querySelectorAll('span[title]')].find((s) => s.textContent.trim() === name)

beforeEach(() => {
  app?.unmount()
  document.body.innerHTML = ''
})

describe('a scheduled task with attachments', () => {
  it('shows each one under the prompt, an image with its thumbnail', async () => {
    tasks = [cron([
      { id: 'a1', filename: 'shot.png', mimeType: 'image/png', url: 'https://agentrq.example/storage/a1' },
      { id: 'a2', filename: 'list.pdf', mimeType: 'application/pdf' },
    ])]
    const el = await mount()
    expect(chip(el, 'shot.png')).toBeTruthy()
    expect(chip(el, 'list.pdf')).toBeTruthy()
    expect(chip(el, 'shot.png').closest('div.cursor-pointer').querySelector('img').getAttribute('src'))
      .toBe('/api/v1/workspaces/ws1/tasks/t1/attachments/a1')
  })

  it('opens one in the preview, with its public link, and closes it', async () => {
    tasks = [cron([{ id: 'a1', filename: 'shot.png', mimeType: 'image/png', url: 'https://agentrq.example/storage/a1' }])]
    const el = await mount()
    chip(el, 'shot.png').closest('div.cursor-pointer').click()
    await settle()
    expect(el.querySelector('[aria-label="Open in browser"]').getAttribute('href')).toBe('https://agentrq.example/storage/a1')
    el.querySelector('.backdrop-blur-sm').click()
    await settle()
    expect(el.querySelector('[aria-label="Download"]')).toBeNull()
  })

  it('offers only the signed-in route for a file with no public link', async () => {
    tasks = [cron([{ id: 'a2', filename: 'list.pdf', mimeType: 'application/pdf' }])]
    const el = await mount()
    chip(el, 'list.pdf').closest('div.cursor-pointer').click()
    await settle()
    expect(el.querySelector('[aria-label="Download"]').getAttribute('href')).toBe('/api/v1/workspaces/ws1/tasks/t1/attachments/a2')
    expect(el.querySelector('[aria-label="Open in browser"]')).toBeNull()
  })

  it('draws no attachment row when there are none', async () => {
    tasks = [cron([])]
    const el = await mount()
    expect(el.querySelector('div.cursor-pointer.rounded-xl')).toBeNull()
    expect(el.textContent).toContain('Weekly check')
  })
})
