import { describe, it, expect, vi } from 'vitest'

import { THEMES, applyTheme, backgroundColorFor, themeSourceFor } from '../src/main/theme.js'

describe('themeSourceFor', () => {
  it('passes the app\'s own setting through', () => {
    for (const theme of THEMES) {
      expect(themeSourceFor(theme)).toBe(theme)
    }
  })

  it('follows the system for an unrecognised value', () => {
    for (const bad of ['sepia', '', null, undefined, 42]) {
      expect(themeSourceFor(bad)).toBe('system')
    }
  })
})

describe('backgroundColorFor', () => {
  it('uses the app\'s dark surface in dark mode', () => {
    expect(backgroundColorFor('dark')).toBe('#09090b')
  })

  it('uses the app\'s light surface in light mode', () => {
    expect(backgroundColorFor('light')).toBe('#fafafa')
  })

  it('ignores the system when the user chose explicitly', () => {
    // The whole point: a user who picked light inside AgentRQ should not get a
    // dark shell because their OS is dark.
    expect(backgroundColorFor('light', true)).toBe('#fafafa')
    expect(backgroundColorFor('dark', false)).toBe('#09090b')
  })

  it('follows the system only when the setting says to', () => {
    expect(backgroundColorFor('system', true)).toBe('#09090b')
    expect(backgroundColorFor('system', false)).toBe('#fafafa')
  })

  it('treats an unknown setting as system', () => {
    expect(backgroundColorFor('sepia', true)).toBe('#09090b')
  })
})

describe('applyTheme', () => {
  const makeWindow = () => ({ setBackgroundColor: vi.fn() })

  it('sets the native theme source from the app setting', () => {
    const nativeTheme = { themeSource: 'system', shouldUseDarkColors: true }

    const result = applyTheme('light', { nativeTheme, windows: () => [] })

    expect(nativeTheme.themeSource).toBe('light')
    expect(result.source).toBe('light')
  })

  it('repaints every open window', () => {
    // The background is what shows during a reload, before the renderer paints
    // — getting it wrong is a white flash on every navigation in dark mode.
    const windows = [makeWindow(), makeWindow()]
    const nativeTheme = { themeSource: 'system', shouldUseDarkColors: false }

    applyTheme('dark', { nativeTheme, windows: () => windows })

    for (const win of windows) {
      expect(win.setBackgroundColor).toHaveBeenCalledWith('#09090b')
    }
  })

  it('asks nativeTheme what system currently means', () => {
    // With 'system' the answer belongs to the OS, and nativeTheme is the thing
    // that knows it — recomputing here would be a guess.
    const nativeTheme = { themeSource: 'light', shouldUseDarkColors: true }

    const result = applyTheme('system', { nativeTheme, windows: () => [] })

    expect(result).toEqual({ source: 'system', color: '#09090b' })
  })

  it('degrades an unknown setting to following the system', () => {
    const nativeTheme = { themeSource: 'dark', shouldUseDarkColors: false }

    expect(applyTheme('nonsense', { nativeTheme, windows: () => [] })).toEqual({
      source: 'system',
      color: '#fafafa',
    })
  })
})
