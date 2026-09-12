// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect } from 'vitest'

import {
  SPDX_IDENTIFIERS,
  parseDrawerEntry,
  parseDrawers,
  checkCompatibility,
  parseManifest,
  satisfiesRange,
  validateLicense,
} from '../../src/main/extensions/manifest.js'

/**
 * The manifest is what turns a repository carrying the topic into an extension,
 * and everything downstream trusts what comes out of it. So these tests assert
 * the *reasons* as well as the verdicts: a rejected manifest is listed in the
 * catalogue with its reason showing, which makes those strings user-facing copy
 * rather than diagnostics.
 */

/** A manifest with nothing wrong with it, to vary one field at a time. */
const valid = () => ({
  name: 'linear',
  displayName: 'Linear',
  version: '1.2.0',
  description: 'Create and search Linear issues from tasks.',
  license: 'MIT',
  engines: { agentrq: '^1.4' },
  mcp: { workspace: ['getTask', 'reply'], supervisor: ['listAllTasks'] },
  config: [{ key: 'apiKey', type: 'secret', label: 'Linear API key' }],
  provides: { ui: ['workspacePanel'] },
  artifact: { release: 'v1.2.0', asset: 'linear-1.2.0.tgz', sha256: 'a'.repeat(64) },
})

/**
 * A drawer names a file this app reads from disk and hands to a frame to run.
 * So the path is checked here rather than trusted, and the single rule that
 * does the work is that a segment may not begin with a dot — which makes `..`
 * and `.` both unmatchable.
 */
describe('parseDrawerEntry', () => {
  it('takes an ordinary relative path to a script', () => {
    for (const entry of ['drawer.js', 'dist/drawer.js', 'dist/draw/mermaid.mjs', 'a-b_c/d.js']) {
      expect(parseDrawerEntry(entry), entry).toBe(entry)
    }
  })

  it('refuses anything that could name a file outside the package', () => {
    for (const entry of [
      '../secrets.js',
      'dist/../../secrets.js',
      './drawer.js',
      '/etc/passwd.js',
      'dist\\drawer.js',
      '..',
      '.',
      '.hidden.js',
    ]) {
      expect(parseDrawerEntry(entry), entry).toBe('')
    }
  })

  it('refuses anything that is not a script', () => {
    for (const entry of ['drawer.json', 'drawer', 'drawer.js.png', 'index.html']) {
      expect(parseDrawerEntry(entry), entry).toBe('')
    }
  })

  it('refuses nothing, and refuses a path nobody would type', () => {
    expect(parseDrawerEntry('')).toBe('')
    expect(parseDrawerEntry(undefined)).toBe('')
    expect(parseDrawerEntry(`${'a/'.repeat(120)}x.js`)).toBe('')
  })
})

describe('a drawer, through the whole manifest', () => {
  // The integration point that matters: a bad drawer refuses the manifest, and
  // the reason is what the catalogue shows against the extension at install.
  it('refuses the manifest, with the reason a person will read', () => {
    const provides = { drawers: [{ format: 'mermaid', entry: '../../../etc/passwd.js' }] }
    const { ok, reason } = parseManifest({ ...valid(), provides })

    expect(ok).toBe(false)
    expect(reason).toContain('mermaid')
  })

  it('carries a good one through to the parsed manifest', () => {
    const provides = { ui: ['workspacePanel'], drawers: [{ format: 'mermaid', entry: 'dist/draw.js' }] }
    const { ok, manifest } = parseManifest({ ...valid(), provides })

    expect(ok).toBe(true)
    expect(manifest.provides.drawers).toEqual([{ format: 'mermaid', entry: 'dist/draw.js' }])
    // The rest of `provides` is still the author's own, passed through.
    expect(manifest.provides.ui).toEqual(['workspacePanel'])
  })
})

describe('parseDrawers', () => {
  it('reads what an extension says it can draw', () => {
    const { ok, drawers } = parseDrawers({ drawers: [{ format: 'mermaid', entry: 'dist/mermaid.js' }] })

    expect(ok).toBe(true)
    expect(drawers).toEqual([{ format: 'mermaid', entry: 'dist/mermaid.js' }])
  })

  it('folds the format, so one drawer is not two', () => {
    expect(parseDrawers({ drawers: [{ format: 'Mermaid', entry: 'd.js' }] }).drawers[0].format).toBe('mermaid')
  })

  it('declaring none is not an error', () => {
    expect(parseDrawers(undefined)).toEqual({ ok: true, drawers: [] })
    expect(parseDrawers({})).toEqual({ ok: true, drawers: [] })
    expect(parseDrawers({ drawers: [] })).toEqual({ ok: true, drawers: [] })
  })

  // Refused rather than dropped: an author who typed a path wrong should be
  // told at install, not left wondering why one format never draws.
  it('refuses a path it will not read, and says which format', () => {
    const { ok, reason } = parseDrawers({ drawers: [{ format: 'mermaid', entry: '../../etc/passwd.js' }] })

    expect(ok).toBe(false)
    expect(reason).toContain('mermaid')
    expect(reason).toContain('inside the package')
  })

  it('refuses a format that is not one', () => {
    expect(parseDrawers({ drawers: [{ format: 'BAD FORMAT', entry: 'd.js' }] }).reason).toContain('not a diagram format')
    expect(parseDrawers({ drawers: [{ entry: 'd.js' }] }).reason).toContain('(none)')
  })

  it('refuses the same format claimed twice by one extension', () => {
    const drawers = [{ format: 'math', entry: 'a.js' }, { format: 'math', entry: 'b.js' }]

    expect(parseDrawers({ drawers }).reason).toContain('twice')
  })

  it('refuses a drawers field that is not a list', () => {
    expect(parseDrawers({ drawers: 'mermaid' }).reason).toContain('must be an array')
  })
})

describe('parseManifest', () => {
  it('accepts a well-formed manifest and normalises it', () => {
    const { ok, manifest } = parseManifest(valid())

    expect(ok).toBe(true)
    expect(manifest.name).toBe('linear')
    expect(manifest.license).toBe('MIT')
    expect(manifest.mcp.workspace).toEqual(['getTask', 'reply'])
    expect(manifest.artifact.sha256).toBe('a'.repeat(64))
  })

  it('reads a manifest that arrives as text', () => {
    // It arrives from GitHub as a string, not an object.
    expect(parseManifest(JSON.stringify(valid())).ok).toBe(true)
  })

  it('says so plainly when the file is not JSON at all', () => {
    const { ok, reason } = parseManifest('<!doctype html>')
    expect(ok).toBe(false)
    expect(reason).toBe('This is not valid JSON.')
  })

  it('refuses anything that is not a JSON object', () => {
    for (const input of ['[]', '"a string"', 'null', '7']) {
      expect(parseManifest(input).ok, input).toBe(false)
    }
  })

  it('defaults the display name and description rather than demanding them', () => {
    const { displayName, ...rest } = valid()
    delete rest.description
    const { manifest } = parseManifest(rest)

    expect(manifest.displayName).toBe('linear')
    expect(manifest.description).toBe('')
  })

  describe('name', () => {
    it('requires one', () => {
      const m = valid()
      delete m.name
      expect(parseManifest(m).reason).toBe('"name" is required.')
    })

    it('accepts lowercase words joined by single hyphens', () => {
      for (const name of ['linear', 'linear-issues', 'a1', 'one-two-three']) {
        expect(parseManifest({ ...valid(), name }).ok, name).toBe(true)
      }
    })

    it('refuses a name that could not be addressed', () => {
      // The name goes in a route, in registry keys and in a directory path — a
      // name with a space in it is not one.
      for (const name of ['Linear', 'linear issues', 'linear_issues', '-linear', 'linear-', 'linear--x']) {
        const { ok, reason } = parseManifest({ ...valid(), name })
        expect(ok, name).toBe(false)
        expect(reason, name).toContain('lowercase words joined by single hyphens')
      }
    })
  })

  describe('license', () => {
    it('is required, because it is part of the decision to install', () => {
      const m = valid()
      delete m.license
      const { ok, reason } = parseManifest(m)

      expect(ok).toBe(false)
      expect(reason).toContain('"license" is required')
      expect(reason).toContain('UNLICENSED')
    })

    it('accepts UNLICENSED as an honest answer', () => {
      expect(parseManifest({ ...valid(), license: 'UNLICENSED' }).ok).toBe(true)
    })

    it('corrects the spelling rather than just refusing it', () => {
      // "mit" is not a slip to reject flatly — it is somebody who meant MIT.
      const { ok, reason } = validateLicense('mit')

      expect(ok).toBe(false)
      expect(reason).toContain('"MIT"')
      expect(reason).toContain('exactly')
    })

    it('refuses free text, so licenses stay comparable', () => {
      const { ok, reason } = validateLicense('MIT License')
      expect(ok).toBe(false)
      expect(reason).toContain('not a recognised SPDX identifier')
    })

    it('refuses an identifier that merely looks like one', () => {
      expect(validateLicense('MITT').ok).toBe(false)
    })

    it('refuses a blank string as firmly as a missing field', () => {
      expect(validateLicense('   ').reason).toContain('"license" is required')
    })

    it('recognises the identifiers people actually publish under', () => {
      for (const id of ['MIT', 'Apache-2.0', 'AGPL-3.0-only', 'BSD-3-Clause', 'ISC', 'MPL-2.0']) {
        expect(SPDX_IDENTIFIERS.has(id), id).toBe(true)
      }
    })
  })

  describe('version and engines', () => {
    it('requires a version it can compare', () => {
      expect(parseManifest({ ...valid(), version: 'latest' }).reason).toContain('"version" must be a version')
      const m = valid()
      delete m.version
      expect(parseManifest(m).reason).toContain('(missing)')
    })

    it('requires an engines range, so an incompatible build can say so', () => {
      const m = valid()
      delete m.engines
      expect(parseManifest(m).reason).toContain('"engines.agentrq" is required')
    })

    it('refuses a range it does not understand rather than guessing', () => {
      // A range nobody parsed correctly is how an extension gets installed on a
      // version it cannot run on, failing far from the manifest that caused it.
      const { ok, reason } = parseManifest({ ...valid(), engines: { agentrq: '1.x || >=2 <3' } })

      expect(ok).toBe(false)
      expect(reason).toContain('not a range this understands')
      expect(reason).toContain('"^1.4"')
    })
  })

  describe('mcp', () => {
    it('treats both surfaces as optional', () => {
      const m = valid()
      delete m.mcp
      const { ok, manifest } = parseManifest(m)

      expect(ok).toBe(true)
      expect(manifest.mcp).toEqual({ workspace: [], supervisor: [] })
    })

    it('refuses anything that is not a list of names', () => {
      expect(parseManifest({ ...valid(), mcp: { workspace: 'getTask' } }).reason).toContain(
        '"mcp.workspace" must be a list',
      )
      expect(parseManifest({ ...valid(), mcp: { supervisor: [7] } }).reason).toContain(
        'not a tool name',
      )
    })

    it('refuses a tool listed twice', () => {
      const mcp = { workspace: ['getTask', 'getTask'] }
      expect(parseManifest({ ...valid(), mcp }).reason).toContain('lists "getTask" twice')
    })
  })

  describe('hooks', () => {
    // Everything else in a manifest says what the extension reaches for. This
    // says what it may be *asked*, and the answer stands in for the user's — so
    // the two levels are kept apart rather than collapsed into one "may review".
    it('accepts the two levels a tool-call reviewer can ask for', () => {
      expect(parseManifest({ ...valid(), hooks: { toolCall: 'deny' } }).manifest.hooks).toEqual({
        toolCall: 'deny',
      })
      expect(parseManifest({ ...valid(), hooks: { toolCall: 'decide' } }).manifest.hooks).toEqual({
        toolCall: 'decide',
      })
    })

    it('asks to be asked nothing, by default', () => {
      expect(parseManifest(valid()).manifest.hooks).toEqual({})
      expect(parseManifest({ ...valid(), hooks: {} }).manifest.hooks).toEqual({})
      expect(parseManifest({ ...valid(), hooks: { toolCall: '' } }).manifest.hooks).toEqual({})
    })

    /**
     * A level nobody recognises must not be read as the narrow one and quietly
     * installed: the author asked for something, and being told which two words
     * are available is the only useful answer.
     */
    it('refuses a level it does not recognise, naming the two that exist', () => {
      const { ok, reason } = parseManifest({ ...valid(), hooks: { toolCall: 'always' } })
      expect(ok).toBe(false)
      expect(reason).toContain('"deny"')
      expect(reason).toContain('"decide"')
    })

    it('refuses a hook that does not exist', () => {
      expect(parseManifest({ ...valid(), hooks: { onReply: 'deny' } }).reason).toContain('Unknown hook: "onReply"')
      expect(parseManifest({ ...valid(), hooks: { a: 1, b: 2 } }).reason).toContain('Unknown hooks:')
    })

    it('refuses anything that is not an object of hooks', () => {
      expect(parseManifest({ ...valid(), hooks: 'toolCall' }).reason).toContain('must be an object')
      expect(parseManifest({ ...valid(), hooks: ['toolCall'] }).reason).toContain('must be an object')
      expect(parseManifest({ ...valid(), hooks: null }).reason).toContain('must be an object')
    })
  })

  describe('net', () => {
    // The field is a declaration by the author, not a restriction: nothing
    // enforces it, and these tests are about the manifest being readable, not
    // about anything being contained.
    it('accepts hostnames, including a leading wildcard', () => {
      const { manifest } = parseManifest({ ...valid(), net: ['hooks.slack.com', '*.example.com'] })
      expect(manifest.net).toEqual(['hooks.slack.com', '*.example.com'])
    })

    it('is absent by default rather than required', () => {
      expect(parseManifest(valid()).manifest.net).toEqual([])
    })

    it('folds case, so one host is not two', () => {
      expect(parseManifest({ ...valid(), net: ['Hooks.Slack.com'] }).manifest.net).toEqual(['hooks.slack.com'])
    })

    it('refuses a whole URL, and says what to do about it', () => {
      const { ok, reason } = parseManifest({ ...valid(), net: ['https://hooks.slack.com/services'] })
      expect(ok).toBe(false)
      expect(reason).toContain('no scheme or path')
    })

    it('refuses anything that is not a list of hostnames', () => {
      expect(parseManifest({ ...valid(), net: 'hooks.slack.com' }).reason).toContain('list of hostnames')
      expect(parseManifest({ ...valid(), net: ['localhost'] }).ok).toBe(false)
      expect(parseManifest({ ...valid(), net: [''] }).ok).toBe(false)
      expect(parseManifest({ ...valid(), net: ['api.example.com:443'] }).ok).toBe(false)
      expect(parseManifest({ ...valid(), net: ['-bad.example.com'] }).ok).toBe(false)
    })

    it('refuses the same host twice', () => {
      const { reason } = parseManifest({ ...valid(), net: ['a.example.com', 'A.example.com'] })
      expect(reason).toContain('twice')
    })
  })

  describe('config', () => {
    it('is optional and defaults its labels to the key', () => {
      const { manifest } = parseManifest({ ...valid(), config: [{ key: 'teamId' }] })
      expect(manifest.config).toEqual([{ key: 'teamId', type: 'string', label: 'teamId' }])
    })

    it('refuses a field with no key, a duplicate, or an unknown type', () => {
      expect(parseManifest({ ...valid(), config: [{ type: 'string' }] }).reason).toContain('needs a "key"')
      expect(
        parseManifest({ ...valid(), config: [{ key: 'a' }, { key: 'a' }] }).reason,
      ).toContain('declares "a" twice')
      expect(
        parseManifest({ ...valid(), config: [{ key: 'a', type: 'file' }] }).reason,
      ).toContain('unknown type "file"')
    })

    it('refuses a config that is not a list', () => {
      expect(parseManifest({ ...valid(), config: {} }).reason).toContain('"config" must be a list')
      expect(parseManifest({ ...valid(), config: ['a'] }).reason).toContain('not a field')
    })
  })

  describe('artifact', () => {
    it('is required, since there is nothing to install without one', () => {
      const m = valid()
      delete m.artifact
      expect(parseManifest(m).reason).toContain('"artifact" is required')
    })

    it('needs the release and the asset named', () => {
      const artifact = { asset: 'x.tgz', sha256: 'a'.repeat(64) }
      expect(parseManifest({ ...valid(), artifact }).reason).toContain('"artifact.release" is required')
      expect(
        parseManifest({ ...valid(), artifact: { release: 'v1', sha256: 'a'.repeat(64) } }).reason,
      ).toContain('"artifact.asset" is required')
    })

    it('demands a real digest, because a git tag can be moved under an install', () => {
      for (const sha256 of ['', 'abc', 'g'.repeat(64), 'a'.repeat(63)]) {
        const { ok, reason } = parseManifest({ ...valid(), artifact: { release: 'v1', asset: 'x', sha256 } })
        expect(ok, sha256).toBe(false)
        expect(reason).toContain('64-character hex SHA-256')
      }
    })

    it('accepts an uppercase digest, lowercased', () => {
      const artifact = { release: 'v1', asset: 'x', sha256: 'A'.repeat(64) }
      expect(parseManifest({ ...valid(), artifact }).manifest.artifact.sha256).toBe('a'.repeat(64))
    })
  })

  it('fills in the optional fields when they are simply absent', () => {
    // A minimal manifest is a legitimate manifest: an extension that provides
    // nothing but a task menu item declares no config, no provides and no
    // shortcuts, and must not have to write empty containers to say so.
    const m = valid()
    delete m.config
    delete m.provides
    delete m.shortcuts
    const { ok, manifest } = parseManifest(m)

    expect(ok).toBe(true)
    expect(manifest.config).toEqual([])
    expect(manifest.provides).toEqual({ drawers: [] })
    expect(manifest.shortcuts).toEqual([])
  })

  it('carries declared shortcuts through, for the install-time conflict check', () => {
    // Conflicts are caught at install from the manifest, not at runtime, so the
    // declaration has to survive parsing to be checked against what is bound.
    const shortcuts = [{ key: 'l', action: 'create-issue', title: 'Create Linear issue' }]
    const { manifest } = parseManifest({ ...valid(), shortcuts })

    expect(manifest.shortcuts).toEqual(shortcuts)
  })

  it('ignores optional fields of the wrong shape rather than adopting them', () => {
    const { manifest } = parseManifest({ ...valid(), provides: 'everything', shortcuts: 'x' })

    expect(manifest.provides).toEqual({ drawers: [] })
    expect(manifest.shortcuts).toEqual([])
  })

  it('reports an unknown field rather than ignoring it', () => {
    // A mistyped key is an author expecting behaviour they will not get, and
    // silence is the one response that guarantees they never find out.
    const { ok, reason } = parseManifest({ ...valid(), licence: 'MIT' })

    expect(ok).toBe(false)
    expect(reason).toBe('Unknown field: "licence".')
  })

  it('lists every unknown field, not just the first', () => {
    const { reason } = parseManifest({ ...valid(), licence: 'MIT', autor: 'me' })
    expect(reason).toBe('Unknown fields: "licence", "autor".')
  })
})

describe('satisfiesRange', () => {
  it('reads a caret range as anything up to the next major', () => {
    expect(satisfiesRange('1.4.0', '^1.4').satisfied).toBe(true)
    expect(satisfiesRange('1.9.9', '^1.4').satisfied).toBe(true)
    expect(satisfiesRange('2.0.0', '^1.4').satisfied).toBe(false)
    expect(satisfiesRange('1.3.9', '^1.4').satisfied).toBe(false)
  })

  it('reads a tilde range as anything up to the next minor', () => {
    expect(satisfiesRange('1.4.7', '~1.4').satisfied).toBe(true)
    expect(satisfiesRange('1.5.0', '~1.4').satisfied).toBe(false)
  })

  it('reads >= as having no ceiling', () => {
    expect(satisfiesRange('9.0.0', '>=1.4').satisfied).toBe(true)
    expect(satisfiesRange('1.3.0', '>=1.4').satisfied).toBe(false)
  })

  it('reads a bare version as exactly itself', () => {
    expect(satisfiesRange('1.4.0', '1.4.0').satisfied).toBe(true)
    expect(satisfiesRange('1.4.1', '1.4.0').satisfied).toBe(false)
  })

  it('treats a missing part as zero, and tolerates a v prefix', () => {
    expect(satisfiesRange('1.4', '^1.4').satisfied).toBe(true)
    expect(satisfiesRange('v1.4.0', '^1.4').satisfied).toBe(true)
    // A bare major on both sides: "2" is 2.0.0, and satisfies ">=1".
    expect(satisfiesRange('2', '>=1').satisfied).toBe(true)
    expect(satisfiesRange('1', '^2').satisfied).toBe(false)
  })

  it('refuses a version or a range it cannot compare', () => {
    expect(satisfiesRange('nightly', '^1.4').ok).toBe(false)
    expect(satisfiesRange('1.4.0', '').reason).toContain('"engines.agentrq" is required')
    expect(satisfiesRange('1.4.0', '^banana').ok).toBe(false)
  })
})

describe('checkCompatibility', () => {
  const manifest = () => parseManifest(valid()).manifest
  const tools = { workspaceTools: ['getTask', 'reply'], supervisorTools: ['listAllTasks'] }

  it('passes when the app is new enough and every tool exists', () => {
    const { compatible, reasons } = checkCompatibility(manifest(), { appVersion: '1.4.2', ...tools })

    expect(compatible).toBe(true)
    expect(reasons).toEqual([])
  })

  it('says which version is needed and which is running', () => {
    const { compatible, reasons } = checkCompatibility(manifest(), { appVersion: '1.3.0', ...tools })

    expect(compatible).toBe(false)
    expect(reasons[0]).toBe('Needs AgentRQ ^1.4; this is 1.3.0.')
  })

  it('names a tool the connected server does not offer', () => {
    // The real check: a self-hosted backend may be older than the extension.
    const { compatible, reasons } = checkCompatibility(manifest(), {
      appVersion: '1.4.0',
      workspaceTools: ['getTask'],
      supervisorTools: [],
    })

    expect(compatible).toBe(false)
    expect(reasons).toContain('The workspace server does not offer "reply".')
    expect(reasons).toContain('The supervisor server does not offer "listAllTasks".')
  })

  it('reports every reason, not just the first', () => {
    // An author fixing a manifest wants the whole list.
    const { reasons } = checkCompatibility(manifest(), { appVersion: '1.0.0' })

    expect(reasons.length).toBe(4)
  })

  it('carries an unparseable range through as its own reason', () => {
    const broken = { ...manifest(), engines: { agentrq: 'whenever' } }
    const { compatible, reasons } = checkCompatibility(broken, { appVersion: '1.4.0', ...tools })

    expect(compatible).toBe(false)
    expect(reasons[0]).toContain('not a range this understands')
  })

  it('assumes nothing is offered when told nothing', () => {
    const { compatible } = checkCompatibility(manifest(), { appVersion: '1.4.0' })
    expect(compatible).toBe(false)
  })
})
