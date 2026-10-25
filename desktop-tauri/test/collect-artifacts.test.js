// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {
  collectArtifacts,
  detectExtension,
  generateManifest,
  normalizeArch,
  renameArtifact,
} from '../scripts/collect-artifacts.mjs'

describe('normalizeArch', () => {
  it('maps amd64, x86_64, and x64 to x64', () => {
    expect(normalizeArch('AgentRQ.Tauri_0.1.0_amd64.AppImage')).toBe('x64')
    expect(normalizeArch('AgentRQ-Tauri-0.1.0-x86_64.AppImage')).toBe('x64')
    expect(normalizeArch('AgentRQ.Tauri_0.1.0_x64.dmg')).toBe('x64')
    expect(normalizeArch('AgentRQ.Tauri_0.1.0_x64-setup.exe')).toBe('x64')
  })

  it('maps aarch64 and arm64 to arm64', () => {
    expect(normalizeArch('AgentRQ.Tauri_0.1.0_aarch64.AppImage')).toBe('arm64')
    expect(normalizeArch('AgentRQ.Tauri_0.1.0_aarch64.dmg')).toBe('arm64')
    expect(normalizeArch('AgentRQ-Tauri-0.1.0-arm64.dmg')).toBe('arm64')
  })

  it('falls back to provided arch when not in filename', () => {
    expect(normalizeArch('AgentRQ.Tauri.dmg', 'arm64')).toBe('arm64')
    expect(normalizeArch('AgentRQ.Tauri.dmg', 'x64')).toBe('x64')
  })
})

describe('detectExtension', () => {
  it('detects compound extensions', () => {
    expect(detectExtension('AgentRQ.Tauri_0.1.0_aarch64.app.tar.gz')).toEqual({
      ext: 'app.tar.gz',
      isSig: false,
    })
    expect(detectExtension('AgentRQ.Tauri_0.1.0_aarch64.app.tar.gz.sig')).toEqual({
      ext: 'app.tar.gz',
      isSig: true,
    })
    expect(detectExtension('AgentRQ.Tauri_0.1.0_amd64.AppImage.tar.gz')).toEqual({
      ext: 'AppImage.tar.gz',
      isSig: false,
    })
    expect(detectExtension('AgentRQ.Tauri_0.1.0_amd64.AppImage.tar.gz.sig')).toEqual({
      ext: 'AppImage.tar.gz',
      isSig: true,
    })
    expect(detectExtension('AgentRQ.Tauri_0.1.0_x64-setup.nsis.zip')).toEqual({
      ext: 'nsis.zip',
      isSig: false,
    })
    expect(detectExtension('AgentRQ.Tauri_0.1.0_x64-setup.nsis.zip.sig')).toEqual({
      ext: 'nsis.zip',
      isSig: true,
    })
  })

  it('detects simple extensions', () => {
    expect(detectExtension('AgentRQ.Tauri_0.1.0_amd64.AppImage')).toEqual({
      ext: 'AppImage',
      isSig: false,
    })
    expect(detectExtension('agentrq-tauri_0.1.0_amd64.deb')).toEqual({
      ext: 'deb',
      isSig: false,
    })
    expect(detectExtension('AgentRQ.Tauri_0.1.0_x64.dmg')).toEqual({
      ext: 'dmg',
      isSig: false,
    })
    expect(detectExtension('AgentRQ.Tauri_0.1.0_x64-setup.exe')).toEqual({
      ext: 'exe',
      isSig: false,
    })
  })
})

describe('renameArtifact', () => {
  it('renames to AgentRQ-Tauri-<version>-<arch>.<ext> with no spaces', () => {
    expect(renameArtifact('AgentRQ.Tauri_0.1.0_amd64.AppImage', '0.1.0')).toBe(
      'AgentRQ-Tauri-0.1.0-x64.AppImage'
    )
    expect(renameArtifact('agentrq-tauri_0.1.0_amd64.deb', '0.1.0')).toBe(
      'AgentRQ-Tauri-0.1.0-x64.deb'
    )
    expect(renameArtifact('AgentRQ.Tauri_0.1.0_aarch64.dmg', '0.1.0')).toBe(
      'AgentRQ-Tauri-0.1.0-arm64.dmg'
    )
    expect(renameArtifact('AgentRQ.Tauri_0.1.0_x64.dmg', '0.1.0')).toBe(
      'AgentRQ-Tauri-0.1.0-x64.dmg'
    )
    expect(renameArtifact('AgentRQ.Tauri_0.1.0_x64-setup.exe', '0.1.0')).toBe(
      'AgentRQ-Tauri-0.1.0-x64.exe'
    )
  })

  it('keeps .sig beside its file', () => {
    expect(renameArtifact('AgentRQ.Tauri_0.1.0_amd64.AppImage.tar.gz.sig', '0.1.0')).toBe(
      'AgentRQ-Tauri-0.1.0-x64.AppImage.tar.gz.sig'
    )
    expect(renameArtifact('AgentRQ.Tauri_0.1.0_aarch64.app.tar.gz.sig', '0.1.0')).toBe(
      'AgentRQ-Tauri-0.1.0-arm64.app.tar.gz.sig'
    )
    expect(renameArtifact('AgentRQ.Tauri_0.1.0_x64-setup.nsis.zip.sig', '0.1.0')).toBe(
      'AgentRQ-Tauri-0.1.0-x64.nsis.zip.sig'
    )
  })
})

describe('collectArtifacts', () => {
  it('collects and renames bundle files and .sig files into outDir', () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'tauri-collect-'))
    const bundleDir = path.join(tmp, 'bundle')
    const outDir = path.join(tmp, 'out')
    fs.mkdirSync(path.join(bundleDir, 'appimage'), { recursive: true })
    fs.mkdirSync(path.join(bundleDir, 'deb'), { recursive: true })

    const appimage = path.join(bundleDir, 'appimage', 'AgentRQ.Tauri_0.1.0_amd64.AppImage')
    const appimageTar = path.join(bundleDir, 'appimage', 'AgentRQ.Tauri_0.1.0_amd64.AppImage.tar.gz')
    const appimageSig = path.join(bundleDir, 'appimage', 'AgentRQ.Tauri_0.1.0_amd64.AppImage.tar.gz.sig')
    const deb = path.join(bundleDir, 'deb', 'agentrq-tauri_0.1.0_amd64.deb')

    fs.writeFileSync(appimage, 'appimage content')
    fs.writeFileSync(appimageTar, 'tar.gz content')
    fs.writeFileSync(appimageSig, 'sig content')
    fs.writeFileSync(deb, 'deb content')

    const collected = collectArtifacts({ bundleDir, outDir, version: '0.1.0' })
    expect(collected.length).toBe(4)

    expect(fs.existsSync(path.join(outDir, 'AgentRQ-Tauri-0.1.0-x64.AppImage'))).toBe(true)
    expect(fs.existsSync(path.join(outDir, 'AgentRQ-Tauri-0.1.0-x64.AppImage.tar.gz'))).toBe(true)
    expect(fs.existsSync(path.join(outDir, 'AgentRQ-Tauri-0.1.0-x64.AppImage.tar.gz.sig'))).toBe(true)
    expect(fs.existsSync(path.join(outDir, 'AgentRQ-Tauri-0.1.0-x64.deb'))).toBe(true)

    fs.rmSync(tmp, { recursive: true, force: true })
  })
})

describe('generateManifest', () => {
  it('generates latest-tauri.json format with platforms keyed darwin-aarch64, darwin-x86_64, linux-x86_64, windows-x86_64', () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'tauri-manifest-'))
    fs.writeFileSync(path.join(tmp, 'AgentRQ-Tauri-0.1.0-arm64.app.tar.gz.sig'), 'darwin-arm64-sig')
    fs.writeFileSync(path.join(tmp, 'AgentRQ-Tauri-0.1.0-x64.AppImage.tar.gz.sig'), 'linux-x64-sig')
    fs.writeFileSync(path.join(tmp, 'AgentRQ-Tauri-0.1.0-x64.nsis.zip.sig'), 'win-x64-sig')

    const manifest = generateManifest({
      dir: tmp,
      version: '0.1.0',
      tag: 'v0.1.0',
      repo: 'agentrq/agentrq',
      notes: 'Test release',
      pubDate: '2026-10-25T01:00:00.000Z',
    })

    expect(manifest).not.toBeNull()
    expect(manifest.version).toBe('0.1.0')
    expect(manifest.notes).toBe('Test release')
    expect(manifest.pub_date).toBe('2026-10-25T01:00:00.000Z')

    expect(manifest.platforms).toEqual({
      'darwin-aarch64': {
        signature: 'darwin-arm64-sig',
        url: 'https://github.com/agentrq/agentrq/releases/download/v0.1.0/AgentRQ-Tauri-0.1.0-arm64.app.tar.gz',
      },
      'darwin-x86_64': {
        signature: 'darwin-arm64-sig',
        url: 'https://github.com/agentrq/agentrq/releases/download/v0.1.0/AgentRQ-Tauri-0.1.0-arm64.app.tar.gz',
      },
      'linux-x86_64': {
        signature: 'linux-x64-sig',
        url: 'https://github.com/agentrq/agentrq/releases/download/v0.1.0/AgentRQ-Tauri-0.1.0-x64.AppImage.tar.gz',
      },
      'windows-x86_64': {
        signature: 'win-x64-sig',
        url: 'https://github.com/agentrq/agentrq/releases/download/v0.1.0/AgentRQ-Tauri-0.1.0-x64.nsis.zip',
      },
    })

    fs.rmSync(tmp, { recursive: true, force: true })
  })

  it('returns null when no .sig files exist', () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'tauri-manifest-empty-'))
    const manifest = generateManifest({ dir: tmp, version: '0.1.0' })
    expect(manifest).toBeNull()
    fs.rmSync(tmp, { recursive: true, force: true })
  })
})
