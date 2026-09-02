import { beforeEach, describe, expect, it, vi } from 'vitest'

import { stopTask } from '../src/api'

describe('stopTask', () => {
  beforeEach(() => {
    vi.unstubAllGlobals()
  })

  it('asks the server to stop the task, and reports what happened', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ stopped: true, approvalsDenied: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    expect(await stopTask('ws1', 'task1')).toEqual({ stopped: true, approvalsDenied: 0 })
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/workspaces/ws1/tasks/task1/stop',
      { method: 'POST' },
    )
  })

  // An agent that could not be stopped had its pending command refused
  // instead — a different outcome, and the caller has to be able to tell.
  it('distinguishes a refused approval from an actual stop', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ stopped: false, approvalsDenied: 1 }),
    }))

    expect(await stopTask('ws1', 'task1')).toEqual({ stopped: false, approvalsDenied: 1 })
  })

  // Being told the agent has no stop is the whole point of the refusal; a bare
  // "failed" would leave the human guessing at a task that is still running.
  it('reports the reason the server refused', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({ error: 'the connected agent does not support being stopped' }),
    }))

    await expect(stopTask('ws1', 'task1')).rejects.toThrow(
      'the connected agent does not support being stopped',
    )
  })

  it('still says something when the refusal carries no reason', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => { throw new Error('not json') },
    }))

    await expect(stopTask('ws1', 'task1')).rejects.toThrow('Failed to stop the task')
  })
})
