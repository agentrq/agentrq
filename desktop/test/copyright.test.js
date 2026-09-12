// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect } from 'vitest'

import {
  COMMENT,
  NOTICE,
  NOTICE_LINES,
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

  /**
   * The second line says what the first cannot enforce on its own. It is a
   * claim about intent — the mechanism is the check and the workflow that runs
   * it — but somebody about to delete the line should be told they are not
   * meant to, and a tool rewriting headers should find something that says so.
   */
  it('says out loud that it is not to be changed', () => {
    expect(NOTICE_LINES).toHaveLength(2)
    expect(NOTICE_LINES[0]).toBe(NOTICE)
    expect(NOTICE_LINES[1]).toBe('This notice may not be modified or removed.')
  })

  // A file carrying only the first line has a notice that has been edited,
  // which is exactly the case this exists to catch.
  it('is not satisfied by half of itself', () => {
    expect(carriesNotice(`// ${NOTICE}`, 'a.js')).toBe(false)
    expect(carriesNotice(`// ${NOTICE_LINES[1]}`, 'a.js')).toBe(false)
  })

  // It is compared after trimming and in no other way. Anything looser lets the
  // notice drift into forty spellings, which is what it exists to prevent.
  it('is matched exactly, not approximately', () => {
    const file = 'x.js'

    expect(carriesNotice(noticeFor(file), file)).toBe(true)
    for (const near of [
      'Copyright 2026 Contextual Inc. https://agentrq.com',
      'copyright 2026 contextual, inc. https://agentrq.com',
      'Copyright 2025 Contextual, Inc. https://agentrq.com',
      'Copyright 2026 Contextual, Inc.',
    ]) {
      expect(carriesNotice(`// ${near}\n// ${NOTICE_LINES[1]}`, file), near).toBe(false)
    }
    // And the second line is held to the same standard as the first.
    expect(carriesNotice(`// ${NOTICE}\n// This notice may not be removed.`, file)).toBe(false)
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
    expect(noticeFor('a.go')).toBe(`// ${NOTICE_LINES[0]}\n// ${NOTICE_LINES[1]}`)
    expect(noticeFor('a.js')).toBe(noticeFor('a.go'))
    // Markup at the top level, so the notice is markup too — and one comment
    // rather than two, because two would read as two notices.
    expect(noticeFor('A.vue')).toBe(`<!--\n  ${NOTICE_LINES[0]}\n  ${NOTICE_LINES[1]}\n-->`)
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
    expect(carriesNotice(`#!/usr/bin/env node\n${noticeFor('a.js')}\n`, 'a.js')).toBe(true)
    expect(carriesNotice(`//go:build linux\n\n${noticeFor('a.go')}\n`, 'a.go')).toBe(true)
  })

  // Far enough down and it is a mention rather than a notice.
  it('does not count one buried in the body', () => {
    expect(carriesNotice(`${'\n'.repeat(20)}${noticeFor('a.js')}`, 'a.js')).toBe(false)
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

    expect(withNotice(go, 'app.go')).toBe(`${noticeFor('app.go')}\n\n${go}`)
  })

  it('puts a Vue notice above the template, as markup', () => {
    expect(withNotice('<template>\n', 'A.vue')).toBe(`${noticeFor('A.vue')}\n\n<template>\n`)
  })

  // Running it twice must not stack two notices.
  it('adds nothing to a file that already carries one', () => {
    const already = `${noticeFor('a.js')}\n\nconst a = 1\n`

    expect(withNotice(already, 'a.js')).toBe(already)
    expect(withNotice(withNotice(already, 'a.js'), 'a.js')).toBe(already)
  })

  it('leaves a file it does not mark exactly as it was', () => {
    expect(withNotice('{"a":1}', 'package.json')).toBe('{"a":1}')
    expect(withNotice(undefined, 'a.js')).toContain(NOTICE)
  })
})
