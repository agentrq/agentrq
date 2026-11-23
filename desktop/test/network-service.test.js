// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest'

import {
  NETWORK_SERVICE,
  createProcessGoneHandler,
  describeProcessGone,
  isNetworkServiceGone,
} from '../src/main/network-service.js'

/** The details Electron passes to `child-process-gone` for the network service. */
const networkService = (over = {}) => ({
  type: 'Utility',
  serviceName: NETWORK_SERVICE,
  name: 'Network Service',
  reason: 'crashed',
  exitCode: 133,
  ...over,
})

describe('isNetworkServiceGone', () => {
  it('recognises the network service', () => {
    expect(isNetworkServiceGone(networkService())).toBe(true)
  })

  it('ignores another utility process', () => {
    // An extension host dying is a different event with a different answer;
    // reconnecting the event stream for it would be noise.
    expect(isNetworkServiceGone(networkService({ serviceName: 'node.mojom.NodeService' }))).toBe(false)
  })

  it('ignores a renderer or GPU process', () => {
    expect(isNetworkServiceGone({ type: 'GPU', reason: 'crashed' })).toBe(false)
    expect(isNetworkServiceGone({ type: 'Renderer', reason: 'killed' })).toBe(false)
  })

  it('tolerates details that are missing entirely', () => {
    expect(isNetworkServiceGone(undefined)).toBe(false)
    expect(isNetworkServiceGone({})).toBe(false)
  })
})

describe('describeProcessGone', () => {
  it('names the service, the reason and the exit code', () => {
    expect(describeProcessGone(networkService())).toBe(`${NETWORK_SERVICE} (crashed, exit 133)`)
  })

  it('falls back to the process name, then the type', () => {
    expect(describeProcessGone({ name: 'Audio Service', reason: 'killed' })).toBe('Audio Service (killed)')
    expect(describeProcessGone({ type: 'GPU', reason: 'crashed' })).toBe('GPU (crashed)')
  })

  it('says so rather than inventing detail it was not given', () => {
    expect(describeProcessGone({})).toBe('unknown (unknown)')
    expect(describeProcessGone(undefined)).toBe('unknown (unknown)')
  })
})

describe('createProcessGoneHandler', () => {
  it('reconnects the event stream when the network service goes', () => {
    const restartEventStream = vi.fn()
    const log = vi.fn()
    const handle = createProcessGoneHandler({ restartEventStream, log })

    expect(handle(networkService())).toBe(true)
    expect(restartEventStream).toHaveBeenCalledOnce()
    expect(log.mock.calls[0][0]).toContain('reconnecting the event stream')
  })

  it('leaves the stream alone for any other process', () => {
    const restartEventStream = vi.fn()
    const log = vi.fn()
    const handle = createProcessGoneHandler({ restartEventStream, log })

    expect(handle({ type: 'GPU', reason: 'crashed' })).toBe(false)
    expect(restartEventStream).not.toHaveBeenCalled()
    expect(log.mock.calls[0][0]).toBe('child process gone: GPU (crashed)')
  })

  it('works without a logger', () => {
    const restartEventStream = vi.fn()
    const handle = createProcessGoneHandler({ restartEventStream })

    expect(handle(networkService())).toBe(true)
    expect(handle({ type: 'GPU' })).toBe(false)
  })
})
