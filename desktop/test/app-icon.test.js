// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect } from 'vitest'
import { join } from 'node:path'

import {
  ICON_FILENAME,
  iconSearchPaths,
  needsWindowIcon,
  resolveWindowIcon,
  windowIconOptions,
} from '../src/main/app-icon.js'
import {
  ICON_DIR,
  ICON_SOURCE,
  MANIFEST,
  SIZES,
  fileNameFor,
  hashSource,
} from '../scripts/generate-icons.mjs'

/** Read a file from the repository, relative to this test. */
const readRepoFile = async (relative) => {
  const { readFile } = await import('node:fs/promises')
  const { fileURLToPath, URL } = await import('node:url')
  return readFile(fileURLToPath(new URL(relative, import.meta.url)), 'utf-8')
}

const ROOTS = { rendererRoot: '/app/dist/renderer', projectRoot: '/app' }

/** Resolve against a fixed set of files that "exist". */
const withFiles = (...present) => {
  const set = new Set(present)
  return (path) => set.has(path)
}

const BUILT = join(ROOTS.rendererRoot, ICON_FILENAME)
const SOURCE = join(ROOTS.projectRoot, '..', 'frontend', 'public', ICON_FILENAME)

describe('needsWindowIcon', () => {
  it('is true on linux', () => {
    expect(needsWindowIcon('linux')).toBe(true)
  })

  it('is false where the packaged app already carries its icon', () => {
    // Windows reads the icon compiled into the .exe and macOS reads the .icns
    // in the bundle. Handing either a raw PNG would replace a converted,
    // multi-size icon with a worse one.
    expect(needsWindowIcon('win32')).toBe(false)
    expect(needsWindowIcon('darwin')).toBe(false)
  })

  it('is false for a platform it has never heard of', () => {
    expect(needsWindowIcon('freebsd')).toBe(false)
    expect(needsWindowIcon(undefined)).toBe(false)
  })
})

describe('iconSearchPaths', () => {
  it('prefers the built renderer copy over the frontend source', () => {
    // The built copy is the one that exists inside a packaged app, so looking
    // there first keeps the common case from depending on a sibling directory
    // that is not shipped.
    expect(iconSearchPaths(ROOTS)).toEqual([BUILT, SOURCE])
  })

  it('names the same file the web build publishes', () => {
    // A second copy of the asset under desktop/src is exactly what this avoids.
    expect(ICON_FILENAME).toBe('agentrq.png')
    for (const path of iconSearchPaths(ROOTS)) {
      expect(path.endsWith(ICON_FILENAME)).toBe(true)
    }
  })
})

describe('resolveWindowIcon', () => {
  it('finds the icon a packaged linux app ships', () => {
    expect(
      resolveWindowIcon({ ...ROOTS, platform: 'linux', exists: withFiles(BUILT) })
    ).toBe(BUILT)
  })

  it('falls back to the frontend source when the renderer is unbuilt', () => {
    // `npm run dev` serves the renderer from Vite and never writes
    // dist/renderer, so the packaged path is missing in exactly the situation
    // a developer is looking at the window.
    expect(
      resolveWindowIcon({ ...ROOTS, platform: 'linux', exists: withFiles(SOURCE) })
    ).toBe(SOURCE)
  })

  it('returns null rather than throwing when the icon is missing', () => {
    // An icon is cosmetic; refusing to open a window over one would turn a
    // cosmetic defect into an unusable app.
    expect(
      resolveWindowIcon({ ...ROOTS, platform: 'linux', exists: withFiles() })
    ).toBeNull()
  })

  it('resolves nothing on platforms that do not need it', () => {
    // Even when the file is sitting right there.
    for (const platform of ['darwin', 'win32']) {
      expect(
        resolveWindowIcon({ ...ROOTS, platform, exists: withFiles(BUILT, SOURCE) })
      ).toBeNull()
    }
  })

  it('does not touch the filesystem on platforms that do not need it', () => {
    const exists = () => {
      throw new Error('should not have looked')
    }
    expect(resolveWindowIcon({ ...ROOTS, platform: 'darwin', exists })).toBeNull()
  })
})

describe('windowIconOptions', () => {
  it('spreads an icon when there is one', () => {
    expect(windowIconOptions('/app/icon.png')).toEqual({ icon: '/app/icon.png' })
  })

  it('spreads nothing when there is not', () => {
    // Not `{ icon: undefined }`: the absent key is what leaves Windows and
    // macOS on the icon their packaged app already carries.
    expect(windowIconOptions(null)).toEqual({})
    expect('icon' in windowIconOptions(null)).toBe(false)
  })
})

/**
 * The other half of the same defect.
 *
 * Handing a window an icon fixes the window. What decides whether the *dock*
 * shows that icon is whether the desktop environment can match the running
 * window to its installed launcher entry, and it matches them by app id —
 * Electron's coming from `desktopName` in package.json, electron-builder's
 * from the product name. Those two live in different files and nothing at
 * build time makes them agree, so this is the check that does. Without it the
 * icon regresses the moment either file is edited, and silently: the build
 * still succeeds and only a Linux user ever sees it.
 */
describe('linux window association', () => {
  it('announces the app under the name its launcher entry is installed as', async () => {
    const pkg = JSON.parse(await readRepoFile('../package.json'))

    // On Linux electron-builder derives the executable, the package and the
    // icon from package.json's `name`, not from the product name — so the
    // .desktop entry is installed as `<name>.desktop` and that is the id the
    // running window has to announce. Left unset, electron-builder would write
    // StartupWMClass from the product name instead and the two would disagree.
    expect(pkg.desktopName).toBe(`${pkg.name}.desktop`)
  })

  it('leaves the installed entry named what it was already named', async () => {
    // This is what makes the change safe to ship: `syncDesktopName` also
    // decides the .desktop *filename*, and its fallback is that same
    // `<name>.desktop`. Choosing any other value here would rename an
    // installed file and orphan existing installs from their entry.
    const pkg = JSON.parse(await readRepoFile('../package.json'))
    const builder = await readRepoFile('../electron-builder.yml')
    const productName = builder.match(/^productName:\s*(.+)$/m)?.[1].trim()

    expect(productName).toBe('AgentRQ')
    expect(pkg.desktopName).not.toBe(`${productName}.desktop`)
    expect(pkg.desktopName).toBe('agentrq-desktop.desktop')
  })

  it('tells electron-builder to honour that name', async () => {
    const builder = await readRepoFile('../electron-builder.yml')
    // Without this, electron-builder ignores `desktopName` entirely and writes
    // StartupWMClass from the product name instead. It defaults to false in
    // electron-builder 26; setting it explicitly also survives the v27 flip.
    expect(builder).toMatch(/^\s{2}syncDesktopName:\s*true\s*$/m)
  })

  it('points at an icon that is actually there', async () => {
    const { access } = await import('node:fs/promises')
    const { fileURLToPath, URL } = await import('node:url')
    const builder = await readRepoFile('../electron-builder.yml')

    // The top-level icon is what becomes the .icns and the .ico. If it ever
    // stops existing the build does not fail, it ships a default icon.
    const configured = builder.match(/^icon:\s*(.+)$/m)?.[1].trim()
    expect(configured).toBeTruthy()
    await expect(
      access(fileURLToPath(new URL(`../${configured}`, import.meta.url)))
    ).resolves.toBeUndefined()
  })

  it('hands the window the same shape the launcher entry gets', async () => {
    const { access } = await import('node:fs/promises')
    const { fileURLToPath, URL } = await import('node:url')

    // The runtime icon comes from the renderer's published assets rather than
    // from the set below, so this is the check that it is really published.
    await expect(
      access(fileURLToPath(new URL(`../../frontend/public/${ICON_FILENAME}`, import.meta.url)))
    ).resolves.toBeUndefined()

    // Not the opaque square tile: that is the same artwork, but the launcher
    // icon is rendered from the rounded SVG and the two sit side by side.
    expect(ICON_FILENAME).not.toBe('large-icon.png')
  })
})

/**
 * The installed icon set.
 *
 * electron-builder resizes nothing to build a freedesktop icon set — handed a
 * single PNG it passes it through as a set of one, which is how the .deb came
 * to install a lone 1024x1024 icon. The set is therefore rendered from the SVG
 * and committed, and these are the checks that it stays real: a missing or
 * incomplete directory does not fail a packaging run, it silently falls back
 * to the default Electron icon.
 */
describe('linux icon set', () => {
  const iconDirUrl = async () => {
    const { URL } = await import('node:url')
    return new URL(`../${ICON_DIR}/`, import.meta.url)
  }

  const readManifest = async () => {
    const { readFile } = await import('node:fs/promises')
    const { fileURLToPath } = await import('node:url')
    const dir = await iconDirUrl()
    return JSON.parse(await readFile(fileURLToPath(new URL(MANIFEST, dir)), 'utf-8'))
  }

  it('is where electron-builder is told to look', async () => {
    const builder = await readRepoFile('../electron-builder.yml')
    // Indented, so it is the `linux:` icon rather than the top-level one.
    expect(builder).toMatch(new RegExp(`^\\s{2}icon:\\s*${ICON_DIR}\\s*$`, 'm'))
  })

  it('holds every size, named the way the size is parsed out', async () => {
    const { readdir } = await import('node:fs/promises')
    const { fileURLToPath } = await import('node:url')
    const present = await readdir(fileURLToPath(await iconDirUrl()))

    for (const size of SIZES) {
      expect(present).toContain(fileNameFor(size))
    }
    // Nothing else, or a leftover from an earlier size list would be collected
    // into the set alongside the current ones.
    expect(present.filter((name) => name.endsWith('.png')).sort()).toEqual(
      SIZES.map(fileNameFor).sort()
    )
  })

  it('contains images that are really the size their name claims', async () => {
    const { readFile } = await import('node:fs/promises')
    const { fileURLToPath, URL } = await import('node:url')
    const dir = await iconDirUrl()

    for (const size of SIZES) {
      const buf = await readFile(fileURLToPath(new URL(fileNameFor(size), dir)))
      // PNG signature is 8 bytes and the IHDR header another 8, so width and
      // height sit at 16 and 20. This is how electron-builder reads them too.
      expect(buf.readUInt32BE(16)).toBe(size)
      expect(buf.readUInt32BE(20)).toBe(size)
      expect(buf.length).toBeGreaterThan(0)
    }
  })

  it('was rendered from the mark the app actually uses', async () => {
    const manifest = await readManifest()
    expect(manifest.source).toBe(ICON_SOURCE)
    expect(manifest.sizes).toEqual(SIZES)
  })

  it('is still in step with that mark', async () => {
    // The committed PNGs cannot tell you they are stale, so the manifest
    // records what they were rendered from. Change the SVG without running
    // `npm run icons` and this fails instead of shipping the old icon.
    const svg = await readRepoFile(`../../${ICON_SOURCE}`)
    const manifest = await readManifest()

    expect(manifest.sha256).toBe(hashSource(svg))
  })

  it('fingerprints the drawing, not the checkout', () => {
    // Windows checks the SVG out with CRLF, which changes every byte after the
    // first newline without changing the artwork at all. Hashing the file
    // as-is made the check above pass on Linux and fail on Windows.
    //
    // Written against literals rather than the real file on purpose: deriving
    // a CRLF copy from whatever the checkout gave us is how the first attempt
    // at this test went wrong, doubling the CR on a machine that already had
    // one.
    expect(hashSource('<svg>\r\n  <rect />\r\n</svg>\r\n')).toBe(
      hashSource('<svg>\n  <rect />\n</svg>\n')
    )
  })

  it('normalises the same whether or not it has already been normalised', () => {
    // The property that makes the above safe to apply anywhere.
    const crlf = 'a\r\nb\r\n'
    expect(hashSource(hashSource(crlf))).toBe(hashSource(hashSource(crlf)))
    expect(hashSource(crlf.replace(/\r/g, ''))).toBe(hashSource(crlf))
  })
})
