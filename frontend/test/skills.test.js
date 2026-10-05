// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest';

import { renderMarkdown } from '../src/utils/markdown';
import {
  SKILL_FILE,
  SKILL_LINK_ATTR,
  SKILL_LIMIT_BYTES,
  SkillsState,
  dirOf,
  choosablePaths,
  formatSkillSize,
  orderCandidates,
  githubImportUrlValid,
  inlineCodeFileMatch,
  orderSkillFiles,
  orderSkills,
  parseSkillUri,
  resolveWithin,
  skillBody,
  skillBreadcrumb,
  skillEnabledMessage,
  skillFileCount,
  skillFileTree,
  skillFullness,
  skillLinkFromEvent,
  skillLinkTarget,
  skillPagePath,
  skillSource,
  skillUri,
  skillsState,
  skillsTabPath,
} from '../src/composables/useSkills';

describe('skill URIs', () => {
  it('builds one, SKILL.md by default', () => {
    expect(skillUri('tdd')).toBe('skill://tdd/SKILL.md');
    expect(skillUri('tdd', 'refs/a.md')).toBe('skill://tdd/refs/a.md');
  });

  it('reads every spelling of the scheme', () => {
    for (const raw of ['skill://tdd/refs/a.md', 'SKILL://TDD/refs/a.md', 'skill:/tdd/refs/a.md', 'skill:tdd/refs/a.md', ' skill://tdd/./refs/a.md ']) {
      expect(parseSkillUri(raw)).toEqual({ skill: 'tdd', path: 'refs/a.md' });
    }
  });

  it('takes a bare skill for its SKILL.md', () => {
    expect(parseSkillUri('skill://tdd')).toEqual({ skill: 'tdd', path: SKILL_FILE });
    expect(parseSkillUri('skill://tdd/')).toEqual({ skill: 'tdd', path: SKILL_FILE });
  });

  it('refuses what is not a skill file', () => {
    for (const raw of ['memory://x.md', 'refs/a.md', 'skill://', 'skill://Not Valid/x.md', 'skill://tdd/../x.md', 'skill://tdd/.hidden', null, undefined]) {
      expect(parseSkillUri(raw)).toBeNull();
    }
  });
});

describe('resolveWithin', () => {
  it('resolves against a directory and stays inside the skill', () => {
    expect(resolveWithin('refs', 'a.md')).toBe('refs/a.md');
    expect(resolveWithin('refs', './a.md')).toBe('refs/a.md');
    expect(resolveWithin('refs/deep', '../a.md')).toBe('refs/a.md');
    expect(resolveWithin('', 'refs//a.md')).toBe('refs/a.md');
    expect(resolveWithin('refs', '../../a.md')).toBeNull();
    expect(resolveWithin('', '../a.md')).toBeNull();
    expect(resolveWithin('', '.git/config')).toBeNull();
    expect(resolveWithin('refs', '..')).toBeNull();
  });

  it('knows where a file sits', () => {
    expect(dirOf('SKILL.md')).toBe('');
    expect(dirOf('refs/deep/a.md')).toBe('refs/deep');
    expect(dirOf(undefined)).toBe('');
  });
});

describe('skillLinkTarget', () => {
  it('follows a skill:// URI to any skill', () => {
    expect(skillLinkTarget('skill://other', 'tdd')).toEqual({ skill: 'other', path: SKILL_FILE });
    expect(skillLinkTarget('skill:other/x.md', 'tdd', 'refs/a.md')).toEqual({ skill: 'other', path: 'x.md' });
  });

  it('resolves a relative link against the file being read', () => {
    expect(skillLinkTarget('references/x.md', 'tdd')).toEqual({ skill: 'tdd', path: 'references/x.md' });
    expect(skillLinkTarget('./b.md', 'tdd', 'refs/a.md')).toEqual({ skill: 'tdd', path: 'refs/b.md' });
    expect(skillLinkTarget('../SKILL.md', 'tdd', 'refs/a.md')).toEqual({ skill: 'tdd', path: SKILL_FILE });
    expect(skillLinkTarget('run%20me.sh#usage', 'tdd')).toEqual({ skill: 'tdd', path: 'run me.sh' });
    expect(skillLinkTarget('guide.md?raw=1', 'tdd')).toEqual({ skill: 'tdd', path: 'guide.md' });
  });

  it('never leaves the skill, and ignores links that are not files', () => {
    expect(skillLinkTarget('../../etc/passwd', 'tdd', 'refs/a.md')).toBeNull();
    expect(skillLinkTarget('../other/x.md', 'tdd')).toBeNull();
    for (const href of ['https://example.com/x.md', 'mailto:a@b.c', 'memory://m.md', '/abs/x.md', '#section', '', '%zz', null]) {
      expect(skillLinkTarget(href, 'tdd')).toBeNull();
    }
    expect(skillLinkTarget('x.md', '')).toBeNull();
  });
});

describe('inlineCodeFileMatch', () => {
  const files = [{ path: 'SKILL.md' }, { path: 'root-cause-tracing.md' }, { path: 'scripts/find-polluter.sh' }, { path: 'scripts/lib.sh' }];

  it('matches a file of the skill exactly', () => {
    expect(inlineCodeFileMatch('root-cause-tracing.md', files)).toBe('root-cause-tracing.md');
    expect(inlineCodeFileMatch('scripts/find-polluter.sh', files)).toBe('scripts/find-polluter.sh');
    expect(inlineCodeFileMatch(' ./scripts/lib.sh ', files)).toBe('scripts/lib.sh');
    expect(inlineCodeFileMatch('skill://tdd/scripts/lib.sh', files, SKILL_FILE, 'tdd')).toBe('scripts/lib.sh');
    expect(inlineCodeFileMatch('root-cause-tracing.md', ['root-cause-tracing.md'])).toBe('root-cause-tracing.md');
  });

  it('matches from the current file\'s directory, then from the root', () => {
    expect(inlineCodeFileMatch('lib.sh', files, 'scripts/find-polluter.sh')).toBe('scripts/lib.sh');
    expect(inlineCodeFileMatch('root-cause-tracing.md', files, 'scripts/find-polluter.sh')).toBe('root-cause-tracing.md');
  });

  it('matches a repository path that ends in /<skill>/<file>', () => {
    const vc = [{ path: 'visual-companion.md' }];
    expect(inlineCodeFileMatch('skills/brainstorming/visual-companion.md', vc, SKILL_FILE, 'brainstorming')).toBe('visual-companion.md');
    expect(inlineCodeFileMatch('brainstorming/visual-companion.md', vc, SKILL_FILE, 'brainstorming')).toBe('visual-companion.md');
    expect(inlineCodeFileMatch('skills/brainstorming/missing.md', vc, SKILL_FILE, 'brainstorming')).toBeNull();
  });

  it('matches nothing else', () => {
    for (const code of ['lib.sh', 'missing.md', 'npm test', 'root-cause-tracing', 'https://x.test/root-cause-tracing.md', '../root-cause-tracing.md', '', null]) {
      expect(inlineCodeFileMatch(code, files)).toBeNull();
    }
    expect(inlineCodeFileMatch('skill://other/SKILL.md', files, SKILL_FILE, 'tdd')).toBeNull();
    expect(inlineCodeFileMatch('SKILL.md', undefined)).toBeNull();
    expect(inlineCodeFileMatch('skills/brainstorming/visual-companion.md', [{ path: 'visual-companion.md' }])).toBeNull();
  });
});

describe('skillLinkFromEvent', () => {
  it('reads the skill file a click landed on', () => {
    document.body.innerHTML = `<a ${SKILL_LINK_ATTR}="skill://tdd/refs/a.md"><code id="c">a.md</code></a><p id="p">x</p>`;
    expect(skillLinkFromEvent({ target: document.getElementById('c') })).toEqual({ skill: 'tdd', path: 'refs/a.md' });
    expect(skillLinkFromEvent({ target: document.getElementById('p') })).toBeNull();
    expect(skillLinkFromEvent(undefined)).toBeNull();
  });
});

describe('ordering, size and source', () => {
  it('orders skills by name and files with SKILL.md first', () => {
    expect(orderSkills([{ name: 'b' }, { name: 'a' }]).map((s) => s.name)).toEqual(['a', 'b']);
    expect(orderSkills()).toEqual([]);
    expect(orderSkillFiles([{ path: 'b.md' }, { path: 'SKILL.md' }, { path: 'a.md' }]).map((f) => f.path)).toEqual(['SKILL.md', 'a.md', 'b.md']);
    expect(orderSkillFiles([{ path: 'a.md' }, { path: 'SKILL.md' }]).map((f) => f.path)).toEqual(['SKILL.md', 'a.md']);
    expect(orderSkillFiles()).toEqual([]);
  });

  it('measures SKILL.md against its cap', () => {
    expect(formatSkillSize(2048)).toBe('2.0 KB');
    expect(skillFullness(SKILL_LIMIT_BYTES / 2)).toBe(50);
    expect(skillFullness(SKILL_LIMIT_BYTES * 2)).toBe(100);
    expect(skillFullness(0)).toBe(0);
    expect(skillFullness('x')).toBe(0);
  });

  it('says where a skill came from', () => {
    expect(skillSource({ sourceType: 'manual' })).toBe('manual');
    expect(skillSource(undefined)).toBe('manual');
    expect(skillSource({ sourceType: 'github' })).toBe('manual');
    expect(skillSource({ sourceType: 'github', sourceRepo: 'obra/superpowers', sourceCommit: '0123456789abcdef', sourceRef: 'main' })).toBe('GitHub obra/superpowers@0123456');
    expect(skillSource({ sourceType: 'github', sourceRepo: 'obra/superpowers', sourceRef: 'main' })).toBe('GitHub obra/superpowers@main');
    expect(skillSource({ sourceType: 'github', sourceRepo: 'obra/superpowers' })).toBe('GitHub obra/superpowers');
  });

  it('lays files out as a tree: SKILL.md, then folders, then files, and leaves out a collapsed folder\'s contents', () => {
    const files = [
      { path: 'z.md', sizeBytes: 1 },
      { path: 'references/b.md', sizeBytes: 2 },
      { path: 'SKILL.md', sizeBytes: 3 },
      { path: 'references/deep/c.md', sizeBytes: 4 },
      { path: 'a.md', sizeBytes: 5 },
      { path: 'references/a.md', sizeBytes: 6 },
      { path: 'assets/SKILL.md', sizeBytes: 7 },
    ];
    const rows = (collapsed) => skillFileTree(files, collapsed).map((r) => `${'  '.repeat(r.depth)}${r.kind === 'dir' ? `${r.name}/` : r.name}`);
    expect(rows()).toEqual([
      'SKILL.md',
      'assets/',
      '  SKILL.md',
      'references/',
      '  deep/',
      '    c.md',
      '  a.md',
      '  b.md',
      'a.md',
      'z.md',
    ]);
    expect(rows(new Set(['references']))).toEqual(['SKILL.md', 'assets/', '  SKILL.md', 'references/', 'a.md', 'z.md']);
    expect(rows(new Set(['references/deep']))).toContain('  deep/');
    expect(rows(new Set(['references/deep']))).not.toContain('    c.md');
    expect(skillFileTree(files).find((r) => r.name === 'c.md')).toEqual({ kind: 'file', path: 'references/deep/c.md', name: 'c.md', depth: 2, sizeBytes: 4 });
    expect(skillFileTree(files).find((r) => r.name === 'deep')).toEqual({ kind: 'dir', path: 'references/deep', name: 'deep', depth: 1 });
    expect(skillFileTree()).toEqual([]);
  });

  it('counts files', () => {
    expect(skillFileCount(1)).toBe('1 file');
    expect(skillFileCount(3)).toBe('3 files');
    expect(skillFileCount(undefined)).toBe('0 files');
  });

  it('links a skill\'s page and the tab it is listed on', () => {
    expect(skillPagePath('ws1', 'tdd')).toBe('/workspaces/ws1/settings/skills/tdd');
    expect(skillPagePath('ws 1', 'a/b')).toBe('/workspaces/ws%201/settings/skills/a%2Fb');
    expect(skillsTabPath('ws1')).toBe('/workspaces/ws1/settings?tab=skills');
  });

  it('breaks a path into a breadcrumb', () => {
    expect(skillBreadcrumb('tdd')).toEqual(['tdd', 'SKILL.md']);
    expect(skillBreadcrumb('tdd', 'references/x.md')).toEqual(['tdd', 'references', 'x.md']);
  });
});

describe('skillBody', () => {
  it('drops the frontmatter and keeps the rest', () => {
    expect(skillBody('---\nname: x\ndescription: d\n---\n# Body\n')).toBe('# Body\n');
    expect(skillBody('---\r\nname: x\r\n---\r\nBody')).toBe('Body');
    expect(skillBody('---\nname: x\n---')).toBe('');
    expect(skillBody('# No frontmatter\n---\n')).toBe('# No frontmatter\n---\n');
    expect(skillBody('---\nunterminated')).toBe('---\nunterminated');
    expect(skillBody(undefined)).toBe('');
  });
});

describe('githubImportUrlValid', () => {
  it('accepts the links the importer takes', () => {
    for (const url of ['https://github.com/obra/superpowers', 'https://github.com/obra/superpowers.git', 'https://github.com/obra/superpowers/', 'https://github.com/obra/superpowers/tree/main', 'https://github.com/obra/superpowers/tree/v1.2/skills/tdd', ' https://GitHub.com/obra/superpowers ']) {
      expect(githubImportUrlValid(url)).toBe(true);
    }
  });

  it('refuses the rest', () => {
    for (const url of ['http://github.com/obra/superpowers', 'https://gitlab.com/a/b', 'https://github.com/obra', 'https://github.com/obra/superpowers/blob/main/README.md', 'obra/superpowers', '', null]) {
      expect(githubImportUrlValid(url)).toBe(false);
    }
  });
});

describe('skillsState', () => {
  it('checks failure before emptiness', () => {
    expect(skillsState({ loading: true, error: null, skills: [] })).toBe(SkillsState.Loading);
    expect(skillsState({ loading: false, error: new Error('network unreachable'), skills: [] })).toBe(SkillsState.Failed);
    expect(skillsState({ loading: false, error: null, skills: [] })).toBe(SkillsState.Empty);
    expect(skillsState({ loading: false, error: null, skills: undefined })).toBe(SkillsState.Empty);
    expect(skillsState({ loading: false, error: null, skills: [{}] })).toBe(SkillsState.Ready);
  });
});

describe('renderMarkdown in a skill', () => {
  const ctx = { skillName: 'systematic-debugging', skillFiles: [{ path: 'SKILL.md' }, { path: 'root-cause-tracing.md' }, { path: 'scripts/find.sh' }] };
  const links = (html) => {
    const div = document.createElement('div');
    div.innerHTML = html;
    return [...div.querySelectorAll(`[${SKILL_LINK_ATTR}]`)].map((a) => [a.getAttribute(SKILL_LINK_ATTR), a.textContent]);
  };

  it('makes inline code that names a file, and relative and skill:// links, open it', () => {
    const html = renderMarkdown(
      'Read `root-cause-tracing.md`, run [the script](scripts/find.sh), see [tdd](skill://tdd) and `npm test`.',
      ctx,
    );
    expect(links(html)).toEqual([
      ['skill://systematic-debugging/root-cause-tracing.md', 'root-cause-tracing.md'],
      ['skill://systematic-debugging/scripts/find.sh', 'the script'],
      ['skill://tdd/SKILL.md', 'tdd'],
    ]);
    expect(html).toContain('<code>npm test</code>');
    expect(html).not.toContain('href="scripts');
  });

  it('resolves against the file being read', () => {
    const html = renderMarkdown('[back](../root-cause-tracing.md) and `find.sh`', { ...ctx, currentPath: 'scripts/find.sh' });
    expect(links(html)).toEqual([
      ['skill://systematic-debugging/root-cause-tracing.md', 'back'],
      ['skill://systematic-debugging/scripts/find.sh', 'find.sh'],
    ]);
  });

  it('escapes what it wraps', () => {
    const html = renderMarkdown('`a<b>.md`', { ...ctx, skillFiles: [{ path: 'a<b>.md' }] });
    expect(html).toContain('<code>a&lt;b&gt;.md</code>');
    const div = document.createElement('div');
    div.innerHTML = html;
    expect(div.querySelector('b')).toBeNull();
    expect(div.querySelector(`[${SKILL_LINK_ATTR}]`).getAttribute(SKILL_LINK_ATTR)).toBe('skill://systematic-debugging/a<b>.md');
  });

  it('renders exactly as before without a skill', () => {
    const md = 'Read `root-cause-tracing.md` and [the script](scripts/find.sh) and [tdd](skill://tdd).';
    expect(renderMarkdown(md)).toBe(renderMarkdown(md, undefined));
    expect(renderMarkdown(md, {})).toBe(renderMarkdown(md));
    expect(links(renderMarkdown(md))).toEqual([]);
    // The context does not outlive the call that was given it.
    renderMarkdown(md, ctx);
    expect(links(renderMarkdown(md))).toEqual([]);
  });
});

describe('choosing from a repository too large to import whole', () => {
  const candidates = [
    { name: 'ship', path: 'ship', sizeBytes: 9, reason: 'too big' },
    { name: 'guard', path: 'b/guard', sizeBytes: 1 },
    { name: 'guard', path: 'a/guard', sizeBytes: 1 },
    { name: 'careful', path: 'careful', sizeBytes: 1 },
    { name: 'audit', path: 'audit', sizeBytes: 1, reason: 'too big' },
  ];

  it('orders those that can be chosen first, then by name and path', () => {
    expect(orderCandidates(candidates).map((c) => c.path)).toEqual(['careful', 'a/guard', 'b/guard', 'audit', 'ship']);
    expect(orderCandidates()).toEqual([]);
  });

  it('chooses only those without a reason', () => {
    expect(choosablePaths(candidates)).toEqual(['b/guard', 'a/guard', 'careful']);
    expect(choosablePaths()).toEqual([]);
  });
});

describe('skillEnabledMessage', () => {
  it('says which way a skill was switched', () => {
    expect(skillEnabledMessage('tdd', false)).toBe('tdd is now hidden from agents');
    expect(skillEnabledMessage('tdd', true)).toBe('tdd is available to agents again');
  });
});
