// Copyright 2026 Contextual, Inc. https://agentrq.com

import { describe, it, expect } from 'vitest'

import {
  COMMENT,
  NOTICE,
  carriesNotice,
  extensionOf,
  isOurs,
  noticeFor,
  withNotice,
} from '../src/copyright.js'

/**
 * The copyright line, on every source file.
 *
 * Text in a repository cannot be made immutable — anyone with an editor can
 * delete a line. What these rules buy is that removing or rewording it fails
 * CI and names the file, so it cannot be done quietly. That is the honest
 * version of the requirement, and these tests are what make it true.
 */

describe('NOTICE', () => {
  it('is the line, written down once', () => {
    expect(NOTICE).toBe('Copyright 2026 Contextual, Inc. https://agentrq.com')
  })

  // It is compared after trimming and in no other way. Anything looser lets the
  // notice drift into forty spellings, which is what it exists to prevent.
  it('is matched exactly, not approximately', () => {
    const file = 'x.js'

    expect(carriesNotice(`// ${NOTICE}`, file)).toBe(true)
    for (const near of [
      'Copyright 2026 Contextual Inc. https://agentrq.com',
      'copyright 2026 contextual, inc. https://agentrq.com',
      'Copyright 2025 Contextual, Inc. https://agentrq.com',
      'Copyright 2026 Contextual, Inc.',
    ]) {
      expect(carriesNotice(`// ${near}`, file), near).toBe(false)
    }
  })
})

describe('extensionOf', () => {
  it('reads the extension', () => {
    expect(extensionOf('a/b/c.go')).toBe('.go')
    expect(extensionOf('App.vue')).toBe('.vue')
  })

  // A dotted directory with an extensionless file in it is not a `.d` file.
  it('is not fooled by a dot in a directory name', () => {
    expect(extensionOf('a.b/Makefile')).toBe('')
    expect(extensionOf('no-extension')).toBe('')
    expect(extensionOf('')).toBe('')
    expect(extensionOf(undefined)).toBe('')
  })
})

describe('isOurs', () => {
  it('claims the source we write', () => {
    for (const path of ['backend/internal/app/app.go', 'frontend/src/App.vue', 'desktop/src/main/index.js', 'a/b.mjs', 'c.ts']) {
      expect(isOurs(path), path).toBe(true)
    }
  })

  // Neither is ours, and both are regenerated: a notice written into `dist/` is
  // gone on the next build, and one in `node_modules/` is simply false.
  it('claims nothing it did not write', () => {
    for (const path of [
      'frontend/node_modules/vue/index.js',
      'desktop/dist/renderer/app.js',
      'frontend/coverage/lcov-report/x.js',
      'plugins/deepseek-harness/lib/index.js',
    ]) {
      expect(isOurs(path), path).toBe(false)
    }
  })

  it('holds up against nothing at all', () => {
    expect(isOurs(undefined)).toBe(false)
    expect(isOurs('')).toBe(false)
  })

  it('leaves alone what has nowhere to put a comment', () => {
    for (const path of ['package.json', 'README.md', 'go.sum', 'logo.png', '.gitignore']) {
      expect(isOurs(path), path).toBe(false)
    }
  })
})

describe('noticeFor', () => {
  it('says it the way each language says it', () => {
    expect(noticeFor('a.go')).toBe(`// ${NOTICE}`)
    expect(noticeFor('a.js')).toBe(`// ${NOTICE}`)
    // Markup at the top level, so the line is markup too.
    expect(noticeFor('A.vue')).toBe(`<!-- ${NOTICE} -->`)
  })

  it('has nothing to say about a file it does not mark', () => {
    expect(noticeFor('a.json')).toBe('')
    expect(noticeFor('README.md')).toBe('')
  })

  it('covers every kind it claims', () => {
    for (const extension of Object.keys(COMMENT)) {
      expect(noticeFor(`file${extension}`), extension).toContain(NOTICE)
    }
  })
})

describe('carriesNotice', () => {
  it('finds it near the top, not only on the first line', () => {
    expect(carriesNotice(`#!/usr/bin/env node\n// ${NOTICE}\n`, 'a.js')).toBe(true)
    expect(carriesNotice(`//go:build linux\n\n// ${NOTICE}\n`, 'a.go')).toBe(true)
  })

  // Far enough down and it is a mention rather than a notice.
  it('does not count one buried in the body', () => {
    expect(carriesNotice(`${'\n'.repeat(20)}// ${NOTICE}`, 'a.js')).toBe(false)
  })

  it('is satisfied by a file it does not mark', () => {
    expect(carriesNotice('{}', 'package.json')).toBe(true)
  })

  it('holds up against nothing at all', () => {
    expect(carriesNotice('', 'a.js')).toBe(false)
    expect(carriesNotice(undefined, 'a.js')).toBe(false)
  })
})

describe('withNotice', () => {
  /**
   * The blank line is not cosmetic. In Go a comment block immediately above
   * `package` **is** the package documentation, so running the notice into an
   * existing doc comment would put it in godoc and change what the package
   * appears to say about itself.
   */
  it('keeps a leading doc comment its own', () => {
    const go = '// Package app wires everything together.\npackage app\n'

    expect(withNotice(go, 'app.go')).toBe(`// ${NOTICE}\n\n${go}`)
  })

  it('puts a Vue notice above the template, as markup', () => {
    expect(withNotice('<template>\n', 'A.vue')).toBe(`<!-- ${NOTICE} -->\n\n<template>\n`)
  })

  // Running it twice must not stack two notices.
  it('adds nothing to a file that already carries one', () => {
    const already = `// ${NOTICE}\n\nconst a = 1\n`

    expect(withNotice(already, 'a.js')).toBe(already)
    expect(withNotice(withNotice(already, 'a.js'), 'a.js')).toBe(already)
  })

  it('leaves a file it does not mark exactly as it was', () => {
    expect(withNotice('{"a":1}', 'package.json')).toBe('{"a":1}')
    expect(withNotice(undefined, 'a.js')).toContain(NOTICE)
  })
})
