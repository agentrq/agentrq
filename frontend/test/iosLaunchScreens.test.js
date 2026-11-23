// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// @vitest-environment node

import { readFileSync, readdirSync, statSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { JSDOM } from 'jsdom'
import sharp from 'sharp'

import { END, START, launchScreens } from '../scripts/ios-launch-screens.mjs'

const file = (path) => fileURLToPath(new URL(`../${path}`, import.meta.url))
const html = readFileSync(file('index.html'), 'utf-8')
const links = [...new JSDOM(html).window.document.querySelectorAll('link[rel="apple-touch-startup-image"]')]
  .map((l) => ({ media: l.getAttribute('media'), href: l.getAttribute('href') }))

describe('the iOS launch screens', () => {
  const screens = launchScreens()

  it('are all linked from index.html, as the script last wrote them', () => {
    expect(html.indexOf(START)).toBeLessThan(html.indexOf(END))
    expect(links).toEqual(screens.map((s) => ({ media: s.media, href: `/${s.file}` })))
  })

  it('come in light and dark for every device size and orientation', () => {
    const keys = screens.map((s) => s.media.replace(/ and \(prefers-color-scheme: \w+\)$/, ''))
    for (const key of new Set(keys)) expect(keys.filter((k) => k === key)).toHaveLength(2)
    expect(new Set(screens.map((s) => s.media)).size).toBe(screens.length)
  })

  it('are drawn at the size each link promises, and nothing else is in the folder', async () => {
    expect(readdirSync(file('public/launch')).sort()).toEqual(screens.map((s) => s.file.slice('launch/'.length)).sort())
    for (const s of screens) {
      const [, w, h, dpr, orientation] = s.media.match(/device-width: (\d+)px\) and \(device-height: (\d+)px\) and \(-webkit-device-pixel-ratio: (\d)\) and \(orientation: (\w+)\)/)
      const [width, height] = orientation === 'portrait' ? [w * dpr, h * dpr] : [h * dpr, w * dpr]
      const meta = await sharp(file(`public/${s.file}`)).metadata()
      expect(`${meta.width}x${meta.height}`).toBe(`${width}x${height}`)
    }
  })

  it('stay small and out of the precache', () => {
    const bytes = screens.reduce((n, s) => n + statSync(file(`public/${s.file}`)).size, 0)
    expect(bytes).toBeLessThan(512 * 1024)
    expect(readFileSync(file('vite.config.js'), 'utf-8')).toMatch(/globIgnores: \[[^\]]*'\*\*\/launch\/\*\*'/)
  })
})
