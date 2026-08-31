import { describe, it, expect, vi } from 'vitest'

import {
  PICKER_MESSAGES,
  PickerUnavailable,
  chooseDirectory,
  directoryPickerState,
  workingDirectoryPlaceholder,
} from '../src/composables/useDirectoryPicker'

const workingBridge = { chooseDirectory: vi.fn().mockResolvedValue('/Users/mt/Code/agentrq') }

describe('directoryPickerState', () => {
  it('is available on desktop with a bridge that can open it', () => {
    expect(directoryPickerState({ isDesktop: true, bridge: workingBridge })).toEqual({
      available: true,
      reason: '',
    })
  })

  it('is unavailable in the browser', () => {
    // A browser never reveals an absolute path, so there is nothing to offer.
    expect(directoryPickerState({ isDesktop: false, bridge: workingBridge })).toEqual({
      available: false,
      reason: PickerUnavailable.Web,
    })
  })

  it('is unavailable on a desktop build whose bridge lacks the chooser', () => {
    // The case that produced a button which did nothing when clicked: the
    // platform says desktop, but the preload has no chooser to call, so
    // optional chaining resolved to undefined and the click was a no-op.
    for (const bridge of [undefined, null, {}, { chooseDirectory: 'nope' }]) {
      expect(directoryPickerState({ isDesktop: true, bridge })).toEqual({
        available: false,
        reason: PickerUnavailable.Bridge,
      })
    }
  })
})

describe('chooseDirectory', () => {
  it('returns the chosen path', async () => {
    const bridge = { chooseDirectory: vi.fn().mockResolvedValue('/Users/mt/Code/agentrq') }

    await expect(chooseDirectory({ isDesktop: true, bridge })).resolves.toBe('/Users/mt/Code/agentrq')
  })

  it('opens where the field already points', async () => {
    const bridge = { chooseDirectory: vi.fn().mockResolvedValue('') }

    await chooseDirectory({ isDesktop: true, bridge, currentPath: '/Users/mt/Code' })

    expect(bridge.chooseDirectory).toHaveBeenCalledWith('/Users/mt/Code')
  })

  it('returns empty when the dialog is dismissed, so nothing is cleared', async () => {
    for (const dismissed of ['', undefined, null]) {
      const bridge = { chooseDirectory: vi.fn().mockResolvedValue(dismissed) }
      await expect(chooseDirectory({ isDesktop: true, bridge })).resolves.toBe('')
    }
  })

  it('explains itself rather than doing nothing when it cannot open', async () => {
    // Silence is the failure this replaces: a click that does nothing looks
    // like a bug in the app, and leaves the person with nowhere to go.
    await expect(chooseDirectory({ isDesktop: false, bridge: workingBridge })).rejects.toThrow(
      PICKER_MESSAGES[PickerUnavailable.Web]
    )
    await expect(chooseDirectory({ isDesktop: true, bridge: {} })).rejects.toThrow(
      PICKER_MESSAGES[PickerUnavailable.Bridge]
    )
  })

  it('lets a failure from the shell surface', async () => {
    const bridge = { chooseDirectory: vi.fn().mockRejectedValue(new Error('dialog exploded')) }

    await expect(chooseDirectory({ isDesktop: true, bridge })).rejects.toThrow('dialog exploded')
  })
})

describe('workingDirectoryPlaceholder', () => {
  it('shows a path from the platform the person is on', () => {
    expect(workingDirectoryPlaceholder('win32')).toBe('C:\\Users\\you\\Code\\project')
    expect(workingDirectoryPlaceholder('linux')).toBe('/home/you/code/project')
    expect(workingDirectoryPlaceholder('darwin')).toBe('/Users/you/Code/project')
  })

  it('falls back to a Unix example when the platform is unknown', () => {
    expect(workingDirectoryPlaceholder(undefined)).toBe('/Users/you/Code/project')
    expect(workingDirectoryPlaceholder('')).toBe('/Users/you/Code/project')
  })
})
