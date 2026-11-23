// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Draws the iOS launch screens, the app icon on the page background, and
// writes their <link> tags into index.html. iOS shows one only on an exact
// match of device size, pixel ratio and orientation, so each device size gets
// its own. Run from frontend/: `node scripts/ios-launch-screens.mjs`.

import { mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import sharp from 'sharp'

// [CSS width, CSS height, pixel ratio], portrait. iPhones open a Home Screen
// app in portrait, so they get no landscape image; iPads get both.
export const IPHONES = [
  [440, 956, 3], // 16 Pro Max, 17 Pro Max
  [430, 932, 3], // 14 Pro Max, 15 Plus and Pro Max, 16 Plus
  [428, 926, 3], // 12 and 13 Pro Max, 14 Plus
  [420, 912, 3], // Air
  [414, 896, 2], // XR, 11
  [402, 874, 3], // 16 Pro, 17, 17 Pro
  [393, 852, 3], // 14 Pro, 15, 15 Pro, 16
  [390, 844, 3], // 12, 13, 14, 16e
  [375, 812, 3], // X, XS, 11 Pro, 12 and 13 mini
  [375, 667, 2], // SE, 8
]
export const IPADS = [
  [1032, 1376, 2], // 13-inch Pro and Air (M4 and later)
  [1024, 1366, 2], // 12.9-inch Pro
  [834, 1210, 2], // 11-inch Pro (M4 and later)
  [834, 1194, 2], // 11-inch Pro, 11-inch Air
  [820, 1180, 2], // Air 4th and 5th gen, iPad 10th gen and later
  [744, 1133, 2], // mini 6th gen and later
]

// What index.html paints first: bg-white md:bg-zinc-100 dark:bg-zinc-950.
const background = (cssWidth, dark) => (dark ? '#09090b' : cssWidth >= 768 ? '#f4f4f5' : '#ffffff')
const ICON_CSS_PX = 120

export function launchScreens() {
  const screens = []
  const add = ([w, h, dpr], orientation) => {
    for (const scheme of ['light', 'dark']) {
      const [width, height] = orientation === 'portrait' ? [w * dpr, h * dpr] : [h * dpr, w * dpr]
      screens.push({
        file: `launch/${width}x${height}-${scheme}.png`,
        width,
        height,
        dpr,
        background: background(orientation === 'portrait' ? w : h, scheme === 'dark'),
        media: `(device-width: ${w}px) and (device-height: ${h}px) and (-webkit-device-pixel-ratio: ${dpr}) and (orientation: ${orientation}) and (prefers-color-scheme: ${scheme})`,
      })
    }
  }
  for (const d of IPHONES) add(d, 'portrait')
  for (const d of IPADS) add(d, 'portrait'), add(d, 'landscape')
  return screens
}

export const START = '<!-- launch screens: scripts/ios-launch-screens.mjs -->'
export const END = '<!-- /launch screens -->'

async function main() {
  const svg = readFileSync('public/favicon.svg')
  rmSync('public/launch', { recursive: true, force: true })
  mkdirSync('public/launch')
  const screens = launchScreens()
  for (const s of screens) {
    const size = ICON_CSS_PX * s.dpr
    const icon = await sharp(svg, { density: (72 * size) / 32 }).resize(size, size).png().toBuffer()
    await sharp({ create: { width: s.width, height: s.height, channels: 3, background: s.background } })
      .composite([{ input: icon, gravity: 'centre' }])
      .png({ palette: true, compressionLevel: 9 })
      .toFile(`public/${s.file}`)
  }
  const links = screens.map((s) => `    <link rel="apple-touch-startup-image" media="${s.media}" href="/${s.file}" />`)
  const html = readFileSync('index.html', 'utf-8')
  const from = html.indexOf(START)
  const to = html.indexOf(END)
  if (from < 0 || to < 0) throw new Error(`index.html needs the ${START} and ${END} markers`)
  writeFileSync('index.html', `${html.slice(0, from + START.length)}\n${links.join('\n')}\n    ${html.slice(to)}`)
  const bytes = readdirSync('public/launch').reduce((n, f) => n + readFileSync(`public/launch/${f}`).length, 0)
  console.log(`${screens.length} launch screens, ${Math.round(bytes / 1024)} KiB`)
}

if (import.meta.url === `file://${process.argv[1]}`) await main()
