// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * Render the Linux icon set from the app's vector mark.
 *
 * Linux packages install icons into the freedesktop hicolor theme, one file per
 * size. electron-builder will happily accept the single 1024px PNG the other
 * platforms use, but it does not resize it — for the `set` format the output
 * extension *is* `.png`, so a lone PNG source is passed straight through as a
 * set of exactly one, and the `.deb` ships a 1024×1024 icon and nothing else.
 * Pointing `linux.icon` at a directory of `NxN.png` files is what produces a
 * real set, and this is what writes that directory.
 *
 * Rendering each size from the SVG rather than downscaling the big PNG is the
 * point rather than a detail: the mark is drawn with 2px strokes on a 32-unit
 * grid, and those strokes stay crisp when the vector is rasterised at 16px.
 * Resampling a 1024px bitmap down to 16px turns them to mush.
 *
 * Run `npm run icons` after changing the mark. The output is committed, so a
 * packaging run never depends on this script — or on sharp — having been run.
 * That is deliberate: a missing icon directory does not fail an
 * electron-builder run, it silently falls back to the default Electron icon,
 * which is the exact class of quiet wrongness this whole area keeps producing.
 */

import { createHash } from 'node:crypto'
import { mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createRequire } from 'node:module'

const __dirname = dirname(fileURLToPath(import.meta.url))
const DESKTOP_ROOT = join(__dirname, '..')
const REPO_ROOT = join(DESKTOP_ROOT, '..')

/** The vector the whole set is rendered from, relative to the repository root. */
export const ICON_SOURCE = 'frontend/public/favicon.svg'

/** Where electron-builder is pointed by `linux.icon`, relative to `desktop/`. */
export const ICON_DIR = 'resources/icons'

/** Records what the committed PNGs were rendered from, so staleness is checkable. */
export const MANIFEST = 'source.json'

/**
 * Fingerprint the mark, in a way that survives being checked out on Windows.
 *
 * The point of recording this is to notice that the *drawing* changed, and a
 * git checkout that rewrites the SVG's line endings has not changed the
 * drawing — but it does change every byte after the first newline, so hashing
 * the file as-is fails on a CRLF checkout and passes on an LF one. Normalising
 * first is what makes the answer a property of the artwork rather than of the
 * machine the test happens to run on.
 *
 * Every CR is dropped rather than just the CRLF pairs, which makes this
 * idempotent: normalising an already-normalised string has to be a no-op, or
 * the answer depends on how many times it has been applied.
 */
export function hashSource(svg) {
  return createHash('sha256').update(String(svg).replace(/\r/g, '')).digest('hex')
}

/**
 * The sizes a hicolor theme is normally asked for. 16 through 512 are the
 * standard directories; anything larger is legal but nothing looks there
 * first, and the largest entry is also what an AppImage takes as its
 * `.DirIcon`, so 512 is the sensible top.
 */
export const SIZES = [16, 24, 32, 48, 64, 128, 256, 512]

/** The name electron-builder parses the size out of. */
export const fileNameFor = (size) => `${size}x${size}.png`

/** sharp lives in the frontend's dependencies; this package installs no copy. */
async function loadSharp() {
  const require = createRequire(join(REPO_ROOT, 'frontend', 'package.json'))
  try {
    return (await import(require.resolve('sharp'))).default
  } catch (cause) {
    throw new Error(
      'sharp could not be loaded from frontend/node_modules — run `npm ci` in frontend/ first.',
      { cause }
    )
  }
}

async function main() {
  const sharp = await loadSharp()
  const sourcePath = join(REPO_ROOT, ICON_SOURCE)
  const svg = await readFile(sourcePath)
  const outDir = join(DESKTOP_ROOT, ICON_DIR)

  // Remove stale sizes rather than leaving them beside the new ones: a file
  // left behind from an earlier list would still be collected into the set.
  await rm(outDir, { recursive: true, force: true })
  await mkdir(outDir, { recursive: true })

  for (const size of SIZES) {
    // A high density is what makes the rasteriser lay the vector out on a grid
    // big enough to resolve the strokes before it scales to the target box.
    await sharp(svg, { density: 1200 })
      .resize(size, size, { fit: 'contain', background: { r: 0, g: 0, b: 0, alpha: 0 } })
      .png({ compressionLevel: 9 })
      .toFile(join(outDir, fileNameFor(size)))
  }

  await writeFile(
    join(outDir, MANIFEST),
    `${JSON.stringify(
      {
        source: ICON_SOURCE,
        sha256: hashSource(svg.toString('utf-8')),
        sizes: SIZES,
      },
      null,
      2
    )}\n`
  )

  const written = (await readdir(outDir)).filter((name) => name.endsWith('.png'))
  console.log(`✓ ${written.length} icons rendered from ${ICON_SOURCE} into desktop/${ICON_DIR}`)
}

// Only run when invoked directly, so the constants above can be imported by the
// test that checks the committed set still matches the mark.
if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  await main()
}
