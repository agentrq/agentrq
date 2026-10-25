// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { deflateSync } from 'node:zlib'
import { describe, expect, it, vi } from 'vitest'
import { check, pixelAt } from './e2e/pixel.mjs'

/** A PNG of `kind` (2 = RGB, 6 = RGBA, 0 = grey), each row with the given filter. */
function png(width, height, kind, pixel, filters = [0]) {
  const bpp = { 0: 1, 2: 3, 6: 4 }[kind]
  const rows = []
  for (let y = 0; y < height; y++) {
    const filter = filters[y % filters.length]
    const row = Buffer.alloc(1 + width * bpp)
    row[0] = filter
    for (let x = 0; x < width; x++) pixel.slice(0, bpp).forEach((v, i) => (row[1 + x * bpp + i] = v))
    rows.push(row)
  }
  // Filtering is the inverse of what pixelAt undoes: encode each row against
  // the true, unfiltered one above it.
  const raw = Buffer.concat(rows)
  const stride = 1 + width * bpp
  const out = Buffer.from(raw)
  for (let y = 0; y < height; y++) {
    const filter = raw[y * stride]
    for (let i = 1; i < stride; i++) {
      const at = y * stride + i
      const a = i > bpp ? raw[at - bpp] : 0
      const b = y ? raw[at - stride] : 0
      const c = y && i > bpp ? raw[at - stride - bpp] : 0
      const p = a + b - c
      const [pa, pb, pc] = [Math.abs(p - a), Math.abs(p - b), Math.abs(p - c)]
      const paeth = pa <= pb && pa <= pc ? a : pb <= pc ? b : c
      out[at] = (raw[at] - [0, a, b, (a + b) >> 1, paeth][filter]) & 255
    }
  }
  const chunk = (type, body) => {
    const head = Buffer.alloc(8)
    head.writeUInt32BE(body.length, 0)
    head.write(type, 4, 'latin1')
    return Buffer.concat([head, body, Buffer.alloc(4)])
  }
  const ihdr = Buffer.alloc(13)
  ihdr.writeUInt32BE(width, 0)
  ihdr.writeUInt32BE(height, 4)
  ihdr[8] = 8
  ihdr[9] = kind
  return Buffer.concat([Buffer.alloc(8), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(out)), chunk('IEND', Buffer.alloc(0))])
}

describe('the middle pixel of a screenshot', () => {
  it('reads RGB, RGBA and grey, through every row filter', () => {
    expect(pixelAt(png(4, 5, 2, [9, 9, 11], [0, 1, 2, 3, 4]))).toEqual([9, 9, 11])
    expect(pixelAt(png(3, 3, 6, [250, 250, 250, 255], [4, 3, 2]))).toEqual([250, 250, 250])
    expect(pixelAt(png(2, 2, 0, [7]))).toEqual([7, 7, 7])
    expect(pixelAt(png(10, 10, 2, [1, 2, 3]), 1, 1)).toEqual([1, 2, 3])
  })

  it('refuses a PNG it cannot read', () => {
    const palette = png(2, 2, 2, [0, 0, 0])
    palette[8 + 8 + 9] = 3
    expect(() => pixelAt(palette)).toThrow('unsupported PNG')
  })

  it('checks it against a colour within a tolerance, and says which', () => {
    const file = join(mkdtempSync(join(tmpdir(), 'pixel-')), 'shot.png')
    writeFileSync(file, png(4, 4, 2, [9, 9, 11]))
    const log = vi.spyOn(console, 'log').mockImplementation(() => {})
    expect(check(file, '#09090b', 0)).toBe(true)
    expect(check(file, '#0a0a0c', 1)).toBe(true)
    expect(check(file, '#fafafa')).toBe(false)
    expect(log.mock.calls.map(([line]) => line.includes('is NOT'))).toEqual([false, false, true])
  })
})
