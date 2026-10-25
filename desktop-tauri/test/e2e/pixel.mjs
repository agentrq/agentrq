// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// `node pixel.mjs shot.png #09090b [tolerance]`: exits 1 unless the middle
// pixel of the PNG is that colour, within a tolerance per channel (macOS
// shots are colour-managed, so a hex is never exact there).
import { readFileSync } from 'node:fs'
import { inflateSync } from 'node:zlib'

const CHANNELS = { 0: 1, 2: 3, 4: 2, 6: 4 }

/** The RGB of one pixel of an 8-bit, non-interlaced PNG. */
export function pixelAt(png, fx = 0.5, fy = 0.5) {
  let width = 0
  let height = 0
  let kind = 0
  const data = []
  for (let at = 8; at < png.length; ) {
    const size = png.readUInt32BE(at)
    const type = png.toString('latin1', at + 4, at + 8)
    const body = png.subarray(at + 8, at + 8 + size)
    if (type === 'IHDR') {
      width = body.readUInt32BE(0)
      height = body.readUInt32BE(4)
      kind = body[9]
      if (body[8] !== 8 || body[12] !== 0 || !CHANNELS[kind]) throw new Error('unsupported PNG')
    } else if (type === 'IDAT') data.push(body)
    at += size + 12
  }
  const bpp = CHANNELS[kind]
  const raw = inflateSync(Buffer.concat(data))
  const stride = width * bpp
  const rows = []
  for (let y = 0; y < height; y++) {
    const filter = raw[y * (stride + 1)]
    const row = Buffer.from(raw.subarray(y * (stride + 1) + 1, (y + 1) * (stride + 1)))
    const above = rows[y - 1]
    for (let i = 0; i < stride; i++) {
      const a = i >= bpp ? row[i - bpp] : 0
      const b = above ? above[i] : 0
      const c = above && i >= bpp ? above[i - bpp] : 0
      const p = a + b - c
      const [pa, pb, pc] = [Math.abs(p - a), Math.abs(p - b), Math.abs(p - c)]
      const paeth = pa <= pb && pa <= pc ? a : pb <= pc ? b : c
      row[i] = (row[i] + [0, a, b, (a + b) >> 1, paeth][filter]) & 255
    }
    rows.push(row)
  }
  const row = rows[Math.min(height - 1, Math.floor(height * fy))]
  const at = Math.min(width - 1, Math.floor(width * fx)) * bpp
  return kind === 0 || kind === 4 ? [row[at], row[at], row[at]] : [row[at], row[at + 1], row[at + 2]]
}

const near = (rgb, hex, tolerance) =>
  [1, 3, 5].every((i, c) => Math.abs(rgb[c] - parseInt(hex.slice(i, i + 2), 16)) <= tolerance)

export function check(file, hex, tolerance = 8) {
  const rgb = pixelAt(readFileSync(file))
  const ok = near(rgb, hex, tolerance)
  console.log(`${file}: middle pixel rgb(${rgb}) ${ok ? 'is' : 'is NOT'} ${hex} ±${tolerance}`)
  return ok
}

if (process.argv[1]?.endsWith('pixel.mjs') && process.argv[2]) {
  process.exit(check(process.argv[2], process.argv[3], Number(process.argv[4] ?? 8)) ? 0 : 1)
}
