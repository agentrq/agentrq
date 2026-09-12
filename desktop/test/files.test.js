// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, expect, it } from 'vitest'

import { FileOpenAction, fileOpenAction, localPathFromFileUrl } from '../src/main/files.js'

describe('localPathFromFileUrl', () => {
  it('reads an ordinary local file URL', () => {
    expect(localPathFromFileUrl('file:///Users/mt/brain/skills_support_plan.md'))
      .toBe('/Users/mt/brain/skills_support_plan.md')
  })

  it('decodes percent-escapes, so the path is the one on disk', () => {
    expect(localPathFromFileUrl('file:///Users/mt/My%20Notes/a%2Bb.md')).toBe('/Users/mt/My Notes/a+b.md')
  })

  it('accepts localhost, which is this machine spelled out', () => {
    expect(localPathFromFileUrl('file://localhost/etc/hosts')).toBe('/etc/hosts')
  })

  it('refuses a host that is another machine', () => {
    // Reaching for it would block on a network mount at best.
    expect(localPathFromFileUrl('file://fileserver/share/report.pdf')).toBe('')
  })

  it('refuses every other scheme', () => {
    expect(localPathFromFileUrl('https://example.com/a.md')).toBe('')
    expect(localPathFromFileUrl('app://agentrq/tasks/1')).toBe('')
    expect(localPathFromFileUrl('javascript:alert(1)')).toBe('')
  })

  it('refuses junk', () => {
    expect(localPathFromFileUrl('/Users/mt/plan.md')).toBe('')
    expect(localPathFromFileUrl('')).toBe('')
    expect(localPathFromFileUrl(null)).toBe('')
  })

  it('refuses a path carrying a NUL byte', () => {
    // The platform's file APIs truncate there, so what got opened would not be
    // what was inspected.
    expect(localPathFromFileUrl('file:///Users/mt/plan.md%00.command')).toBe('')
  })

  it('keeps a malformed escape rather than refusing the file', () => {
    expect(localPathFromFileUrl('file:///tmp/100%.md')).toBe('/tmp/100%.md')
  })

  it('drops the slash the URL form adds to a Windows drive path', () => {
    expect(localPathFromFileUrl('file:///C:/Users/mt/plan.md')).toBe('C:/Users/mt/plan.md')
  })
})

describe('fileOpenAction', () => {
  const action = (path, stat) => fileOpenAction(path, stat)

  it('opens the documents people actually link to', () => {
    for (const path of [
      '/Users/mt/skills_support_plan.md',
      '/Users/mt/notes.txt',
      '/Users/mt/report.pdf',
      '/Users/mt/data.csv',
      '/Users/mt/main.go',
      '/Users/mt/screenshot.png',
      '/Users/mt/demo.mp4',
      '/Users/mt/deck.pptx',
    ]) {
      expect(action(path), path).toBe(FileOpenAction.Open)
    }
  })

  it('is not fooled by an uppercase extension', () => {
    expect(action('/Users/mt/PLAN.MD')).toBe(FileOpenAction.Open)
  })

  it('reveals anything that would run rather than open it', () => {
    // The link text is written by an agent; `plan.md` may well point here.
    for (const path of [
      '/Users/mt/install.sh',
      '/Users/mt/setup.command',
      '/Applications/Calculator.app',
      '/Users/mt/setup.exe',
      '/Users/mt/go.bat',
      '/Users/mt/thing.scpt',
    ]) {
      expect(action(path), path).toBe(FileOpenAction.Reveal)
    }
  })

  it('reveals markup that opens in a browser', () => {
    // An .html or .svg file carries script and markup its author chose.
    expect(action('/Users/mt/report.html')).toBe(FileOpenAction.Reveal)
    expect(action('/Users/mt/icon.svg')).toBe(FileOpenAction.Reveal)
  })

  it('reveals an archive rather than inviting a click on what is inside', () => {
    expect(action('/Users/mt/bundle.zip')).toBe(FileOpenAction.Reveal)
  })

  it('reveals an extension nobody listed', () => {
    // An allowlist, because the interesting case is the one not thought about.
    expect(action('/Users/mt/thing.wat')).toBe(FileOpenAction.Reveal)
  })

  it('reveals a file with no extension, where the platform decides', () => {
    expect(action('/Users/mt/Makefile')).toBe(FileOpenAction.Reveal)
  })

  it('treats a leading dot as a name, not an extension', () => {
    expect(action('/Users/mt/.zshrc')).toBe(FileOpenAction.Reveal)
  })

  it('does not read an extension out of a directory name', () => {
    expect(action('/Users/mt/notes.md/run')).toBe(FileOpenAction.Reveal)
  })

  it('opens a folder, which has nothing to run', () => {
    expect(action('/Users/mt/brain', { isDirectory: true })).toBe(FileOpenAction.Open)
    expect(action('/Users/mt/scripts.sh', { isDirectory: true })).toBe(FileOpenAction.Open)
  })

  it('reads the extension off a Windows path too', () => {
    expect(action('C:\\Users\\mt\\plan.md')).toBe(FileOpenAction.Open)
    expect(action('C:\\Users\\mt\\setup.exe')).toBe(FileOpenAction.Reveal)
  })

  it('reveals nothing it cannot name', () => {
    expect(action('')).toBe(FileOpenAction.Reveal)
    expect(action(null)).toBe(FileOpenAction.Reveal)
  })
})
