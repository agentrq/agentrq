// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// @vitest-environment node

import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import sharp from 'sharp'

import { manifest } from '../vite.config.js'

const publicFile = (src) => fileURLToPath(new URL(`../public/${src}`, import.meta.url))

describe('the installed app manifest', () => {
  it('keeps its identity and does not lock the orientation', () => {
    expect(manifest.id).toBe('/')
    expect(manifest).not.toHaveProperty('orientation')
  })

  it('opens the app where it is served, at the root or under a base path', () => {
    for (const base of ['https://h/', 'https://h/agentrq/']) {
      const manifestUrl = new URL('manifest.webmanifest', base)
      expect(new URL(manifest.start_url, manifestUrl).href).toBe(base)
      expect(new URL(manifest.scope, manifestUrl).href).toBe(base)
    }
  })

  it('describes the app with the README tagline', () => {
    const readme = readFileSync(fileURLToPath(new URL('../../README.md', import.meta.url)), 'utf-8')
    expect(readme.split('\n')[0]).toContain(manifest.description)
  })

  it('has a maskable icon of its own, with the logo inside the safe zone', async () => {
    const maskable = manifest.icons.filter((i) => i.purpose === 'maskable')
    expect(maskable).toHaveLength(1)
    const [icon] = maskable
    expect(manifest.icons.filter((i) => i.src === icon.src)).toHaveLength(1)

    const { data, info } = await sharp(publicFile(icon.src)).raw().toBuffer({ resolveWithObject: true })
    expect(`${info.width}x${info.height}`).toBe(icon.sizes)
    // The logo is white on a dark field; every bright pixel must sit inside
    // the centre circle of radius 40%, the part every launcher mask keeps.
    const c = info.width / 2
    let radius = 0
    for (let y = 0; y < info.height; y++) {
      for (let x = 0; x < info.width; x++) {
        if (data[(y * info.width + x) * info.channels] > 128) {
          radius = Math.max(radius, Math.hypot(x + 0.5 - c, y + 0.5 - c))
        }
      }
    }
    expect(radius).toBeGreaterThan(0)
    expect(radius).toBeLessThan(info.width * 0.4)
  })

  it('has one narrow and one wide screenshot, at the sizes it declares', async () => {
    expect(manifest.screenshots.map((s) => s.form_factor).sort()).toEqual(['narrow', 'wide'])
    for (const shot of manifest.screenshots) {
      const { width, height } = await sharp(publicFile(shot.src)).metadata()
      expect(`${width}x${height}`).toBe(shot.sizes)
      expect(shot.form_factor === 'wide' ? width > height : height > width).toBe(true)
      expect(shot.label).toBeTruthy()
    }
  })
})
