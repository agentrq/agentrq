// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { collectArtifacts, generateManifest, run } from '../scripts/collect-artifacts.mjs'

const SCRIPT = fileURLToPath(new URL('../scripts/collect-artifacts.mjs', import.meta.url))
const FIXTURES = fileURLToPath(new URL('./fixtures/tauri-bundles/', import.meta.url))
const VERSION = JSON.parse(
  fs.readFileSync(new URL('../src-tauri/tauri.conf.json', import.meta.url), 'utf8')
).version
const RELEASE = 'https://github.com/agentrq/agentrq/releases/download/v0.13.4'

let tmp
beforeEach(() => {
  tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'tauri-collect-'))
})
afterEach(() => {
  fs.rmSync(tmp, { recursive: true, force: true })
})

// Lays out a bundle tree from a fixture: every file a real v2 build left under
// bundle/, each holding its own path so a copy can be traced to its source.
function bundleTree(leg, { drop = [], add = [] } = {}) {
  const dir = path.join(tmp, leg, 'bundle')
  const files = fs.readFileSync(path.join(FIXTURES, `${leg}.txt`), 'utf8').split('\n').filter(Boolean)
  for (const rel of [...files.filter((f) => !drop.includes(f)), ...add]) {
    fs.mkdirSync(path.dirname(path.join(dir, rel)), { recursive: true })
    fs.writeFileSync(path.join(dir, rel), rel)
  }
  return dir
}

function collected(outDir) {
  return Object.fromEntries(
    fs.readdirSync(outDir).sort().map((f) => [f, fs.readFileSync(path.join(outDir, f), 'utf8')])
  )
}

const LEGS = {
  'linux-x64': {
    platform: 'linux',
    arch: 'x64',
    out: {
      'AgentRQ-Tauri-0.13.4-x64.AppImage': 'appimage/AgentRQ Tauri_0.13.4_amd64.AppImage',
      'AgentRQ-Tauri-0.13.4-x64.AppImage.sig': 'appimage/AgentRQ Tauri_0.13.4_amd64.AppImage.sig',
      'AgentRQ-Tauri-0.13.4-x64.deb': 'deb/AgentRQ Tauri_0.13.4_amd64.deb',
    },
  },
  'macos-arm64': {
    platform: 'macos',
    arch: 'arm64',
    out: {
      'AgentRQ-Tauri-0.13.4-arm64.app.tar.gz': 'macos/AgentRQ Tauri.app.tar.gz',
      'AgentRQ-Tauri-0.13.4-arm64.app.tar.gz.sig': 'macos/AgentRQ Tauri.app.tar.gz.sig',
      'AgentRQ-Tauri-0.13.4-arm64.dmg': 'dmg/AgentRQ Tauri_0.13.4_aarch64.dmg',
    },
  },
  'macos-x64': {
    platform: 'macos',
    arch: 'x64',
    out: {
      'AgentRQ-Tauri-0.13.4-x64.app.tar.gz': 'macos/AgentRQ Tauri.app.tar.gz',
      'AgentRQ-Tauri-0.13.4-x64.app.tar.gz.sig': 'macos/AgentRQ Tauri.app.tar.gz.sig',
      'AgentRQ-Tauri-0.13.4-x64.dmg': 'dmg/AgentRQ Tauri_0.13.4_x64.dmg',
    },
  },
  'windows-x64': {
    platform: 'windows',
    arch: 'x64',
    out: {
      'AgentRQ-Tauri-0.13.4-x64.exe': 'nsis/AgentRQ Tauri_0.13.4_x64-setup.exe',
      'AgentRQ-Tauri-0.13.4-x64.exe.sig': 'nsis/AgentRQ Tauri_0.13.4_x64-setup.exe.sig',
    },
  },
}

describe('collectArtifacts', () => {
  for (const [leg, { platform, arch, out }] of Object.entries(LEGS)) {
    it(`takes exactly the release files from a real ${leg} bundle tree`, () => {
      const outDir = path.join(tmp, 'out')
      const names = collectArtifacts({ bundleDir: bundleTree(leg), outDir, version: '0.13.4', platform, arch, signed: true })
      expect(collected(outDir)).toEqual(out)
      expect(names.sort()).toEqual(Object.keys(out))
    })
  }

  it('leaves the signatures out of an unsigned build', () => {
    const outDir = path.join(tmp, 'out')
    collectArtifacts({ bundleDir: bundleTree('linux-x64'), outDir, version: '0.13.4', platform: 'linux', arch: 'x64' })
    expect(Object.keys(collected(outDir))).toEqual(['AgentRQ-Tauri-0.13.4-x64.AppImage', 'AgentRQ-Tauri-0.13.4-x64.deb'])
  })

  it('names files after tauri.conf.json when no version is given', () => {
    const outDir = path.join(tmp, 'out')
    collectArtifacts({ bundleDir: bundleTree('windows-x64'), outDir, platform: 'windows', arch: 'x64' })
    expect(Object.keys(collected(outDir))).toEqual([`AgentRQ-Tauri-${VERSION}-x64.exe`])
  })

  it('fails, copying nothing, when a bundle or its signature is missing', () => {
    const outDir = path.join(tmp, 'out')
    const bundleDir = bundleTree('macos-arm64', {
      drop: ['dmg/AgentRQ Tauri_0.13.4_aarch64.dmg', 'macos/AgentRQ Tauri.app.tar.gz.sig'],
    })
    expect(() => collectArtifacts({ bundleDir, outDir, version: '0.13.4', platform: 'macos', arch: 'arm64', signed: true })).toThrow(
      'no *.dmg in bundle/dmg; no signature beside bundle/macos/AgentRQ Tauri.app.tar.gz'
    )
    expect(fs.existsSync(outDir)).toBe(false)
  })

  it('fails when a target dir is missing altogether', () => {
    expect(() => collectArtifacts({ bundleDir: path.join(tmp, 'none'), outDir: tmp, version: '1', platform: 'windows', arch: 'x64' })).toThrow(
      'no *-setup.exe in bundle/nsis'
    )
  })

  it('fails when two bundles would get the same name', () => {
    const bundleDir = bundleTree('macos-arm64', { add: ['dmg/AgentRQ Tauri_0.13.4_x64.dmg'] })
    expect(() => collectArtifacts({ bundleDir, outDir: tmp, version: '0.13.4', platform: 'macos', arch: 'arm64' })).toThrow(
      'AgentRQ Tauri_0.13.4_aarch64.dmg, AgentRQ Tauri_0.13.4_x64.dmg would all be AgentRQ-Tauri-0.13.4-arm64.dmg'
    )
  })

  it('refuses an unknown platform or arch', () => {
    expect(() => collectArtifacts({ platform: 'linux', arch: 'aarch64' })).toThrow('unknown arch "aarch64"')
    expect(() => collectArtifacts({ platform: 'android', arch: 'arm64' })).toThrow('unknown platform "android"')
  })
})

// What the release holds once every leg has uploaded.
function releaseSigs(dir) {
  fs.mkdirSync(dir, { recursive: true })
  for (const [leg, { platform, arch }] of Object.entries(LEGS)) {
    const names = collectArtifacts({ bundleDir: bundleTree(leg), outDir: path.join(tmp, `${leg}-out`), version: '0.13.4', platform, arch, signed: true })
    for (const name of names.filter((n) => n.endsWith('.sig'))) {
      fs.copyFileSync(path.join(tmp, `${leg}-out`, name), path.join(dir, name))
    }
  }
  return dir
}

const FEED = {
  version: '0.13.4',
  notes: 'Release v0.13.4',
  pub_date: '2026-10-25T01:00:00.000Z',
  platforms: {
    'darwin-aarch64': { signature: 'macos/AgentRQ Tauri.app.tar.gz.sig', url: `${RELEASE}/AgentRQ-Tauri-0.13.4-arm64.app.tar.gz` },
    'darwin-x86_64': { signature: 'macos/AgentRQ Tauri.app.tar.gz.sig', url: `${RELEASE}/AgentRQ-Tauri-0.13.4-x64.app.tar.gz` },
    'linux-x86_64': { signature: 'appimage/AgentRQ Tauri_0.13.4_amd64.AppImage.sig', url: `${RELEASE}/AgentRQ-Tauri-0.13.4-x64.AppImage` },
    'windows-x86_64': { signature: 'nsis/AgentRQ Tauri_0.13.4_x64-setup.exe.sig', url: `${RELEASE}/AgentRQ-Tauri-0.13.4-x64.exe` },
  },
}

describe('generateManifest', () => {
  it('keys every leg to its own platform', () => {
    const dir = releaseSigs(path.join(tmp, 'sigs'))
    expect(generateManifest({ dir, tag: 'v0.13.4', pubDate: FEED.pub_date })).toEqual(FEED)
  })

  it('leaves out a leg that uploaded nothing, and never aliases Intel to arm64', () => {
    const dir = releaseSigs(path.join(tmp, 'sigs'))
    fs.rmSync(path.join(dir, 'AgentRQ-Tauri-0.13.4-x64.app.tar.gz.sig'))
    const { platforms } = generateManifest({ dir, tag: 'v0.13.4', repo: 'o/r' })
    expect(Object.keys(platforms)).toEqual(['darwin-aarch64', 'linux-x86_64', 'windows-x86_64'])
    expect(platforms['darwin-aarch64'].url).toBe('https://github.com/o/r/releases/download/v0.13.4/AgentRQ-Tauri-0.13.4-arm64.app.tar.gz')
  })

  it('fails on a signature it cannot place, naming it', () => {
    const dir = releaseSigs(path.join(tmp, 'sigs'))
    for (const f of ['AgentRQ-Tauri-0.13.4-x64.deb.sig', 'AgentRQ-Tauri-0.13.3-x64.AppImage.sig', 'AgentRQ-Tauri-0.13.4-arm64.AppImage.sig']) {
      fs.writeFileSync(path.join(dir, f), 'x')
    }
    expect(() => generateManifest({ dir, tag: 'v0.13.4' })).toThrow(
      'signatures matching no updater platform for 0.13.4: AgentRQ-Tauri-0.13.3-x64.AppImage.sig, AgentRQ-Tauri-0.13.4-arm64.AppImage.sig, AgentRQ-Tauri-0.13.4-x64.deb.sig'
    )
  })

  it('fails when there is no signature at all', () => {
    fs.writeFileSync(path.join(tmp, 'AgentRQ-Tauri-0.13.4-x64.AppImage'), 'x')
    expect(() => generateManifest({ dir: tmp, tag: 'v0.13.4' })).toThrow(`no .sig files in ${tmp}`)
  })
})

describe('run', () => {
  function rig() {
    const lines = []
    return { lines, log: (l) => lines.push(l) }
  }

  it('collects one leg', () => {
    const { lines, log } = rig()
    const out = path.join(tmp, 'out')
    const code = run(
      ['--platform', 'windows', '--arch', 'x64', '--bundle-dir', bundleTree('windows-x64'), '--out', out, '--version', '0.13.4', '--signed'],
      { log }
    )
    expect(code).toBe(0)
    expect(lines).toEqual(['Collected 2 files:', '  AgentRQ-Tauri-0.13.4-x64.exe', '  AgentRQ-Tauri-0.13.4-x64.exe.sig'])
    expect(Object.keys(collected(out))).toEqual(['AgentRQ-Tauri-0.13.4-x64.exe', 'AgentRQ-Tauri-0.13.4-x64.exe.sig'])
  })

  it('reports a failed collection as an error and a non-zero exit', () => {
    const { lines, log } = rig()
    const code = run(['--platform', 'linux', '--arch', 'x64', '--bundle-dir', path.join(tmp, 'none'), '--out', tmp], { log })
    expect(code).toBe(1)
    expect(lines).toEqual(['::error::no *.AppImage in bundle/appimage; no *.deb in bundle/deb'])
  })

  it('writes latest-tauri.json from the release signatures', () => {
    const { lines, log } = rig()
    const dir = releaseSigs(path.join(tmp, 'sigs'))
    const code = run(['--manifest', '--dir', dir, '--tag', 'v0.13.4'], { log, env: { GITHUB_REPOSITORY: 'agentrq/agentrq' } })
    expect(code).toBe(0)
    const text = fs.readFileSync(path.join(dir, 'latest-tauri.json'), 'utf8')
    const { pub_date: pubDate } = JSON.parse(text)
    expect(text).toBe(JSON.stringify({ ...FEED, pub_date: pubDate }, null, 2) + '\n')
    expect(new Date(pubDate).toISOString()).toBe(pubDate)
    expect(lines).toEqual([`Wrote ${path.join(dir, 'latest-tauri.json')} for darwin-aarch64, darwin-x86_64, linux-x86_64, windows-x86_64`])
  })

  it('takes the repository from --repo over the environment', () => {
    const dir = releaseSigs(path.join(tmp, 'sigs'))
    expect(run(['--manifest', '--dir', dir, '--tag', 'v0.13.4', '--repo', 'o/r'], { log: () => {}, env: { GITHUB_REPOSITORY: 'x/y' } })).toBe(0)
    const feed = JSON.parse(fs.readFileSync(path.join(dir, 'latest-tauri.json'), 'utf8'))
    expect(feed.platforms['linux-x86_64'].url).toBe('https://github.com/o/r/releases/download/v0.13.4/AgentRQ-Tauri-0.13.4-x64.AppImage')
  })

  it('fails the manifest without a tag, or with nothing to sign', () => {
    const { lines, log } = rig()
    expect(run(['--manifest', '--dir', tmp], { log })).toBe(1)
    const cwd = process.cwd()
    process.chdir(tmp)
    try {
      expect(run(['--manifest', '--tag', 'v1.0.0'], { log })).toBe(1)
    } finally {
      process.chdir(cwd)
    }
    expect(lines).toEqual(['::error::--manifest needs --tag', `::error::no .sig files in ${fs.realpathSync(tmp)}`])
  })

  describe('as a script', () => {
    const argv = process.argv

    beforeEach(() => {
      vi.resetModules()
      vi.spyOn(console, 'log').mockImplementation(() => {})
    })
    afterEach(() => {
      process.argv = argv
      process.exitCode = undefined
      vi.restoreAllMocks()
    })

    it('sets the exit code from the command line', async () => {
      const dir = releaseSigs(path.join(tmp, 'sigs'))
      process.argv = [argv[0], SCRIPT, '--manifest', '--dir', dir, '--tag', 'v0.13.4']
      await import('../scripts/collect-artifacts.mjs')
      expect(process.exitCode).toBe(0)
      expect(fs.existsSync(path.join(dir, 'latest-tauri.json'))).toBe(true)

      vi.resetModules()
      process.argv = [argv[0], SCRIPT, '--manifest', '--dir', dir]
      await import('../scripts/collect-artifacts.mjs')
      expect(process.exitCode).toBe(1)
      expect(console.log).toHaveBeenLastCalledWith('::error::--manifest needs --tag')
    })

    it('does nothing when imported', async () => {
      process.argv = [argv[0]]
      await import('../scripts/collect-artifacts.mjs')
      expect(process.exitCode).toBeUndefined()
    })
  })
})
