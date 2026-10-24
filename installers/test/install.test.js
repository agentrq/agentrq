// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

// Tests for install.sh's asset pick, run under `sh` against a fake GitHub.
// A Tauri asset beside the Electron ones must never be chosen.
// Run with `node --test installers/test/`.
'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

const SCRIPT = path.join(__dirname, '../install.sh');
const TAG = 'v1.2.3';
const DOWNLOAD = `https://github.com/agentrq/agentrq/releases/download/${TAG}`;

// Prints the URL it was asked for instead of downloading it, and serves the
// release JSON for the API calls.
const FAKE_CURL = `#!/bin/sh
out=""
url=""
while [ $# -gt 0 ]; do
	case "$1" in
	-o) out="$2"; shift 2 ;;
	-H) shift 2 ;;
	-*) shift ;;
	*) url="$1"; shift ;;
	esac
done
case "$url" in
*api.github.com*) cat "$RELEASE" ;;
*)
	echo "DOWNLOAD $url" >> "$LOG"
	if [ -n "$MOCK_INSTALL" ]; then
		[ -n "$out" ] && : > "$out"
		exit 0
	fi
	exit 22
	;;
esac
`;

function run({ arch, names, args = [], osName = 'Linux', env = {} }) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'install-'));
  const bin = path.join(root, 'bin');
  fs.mkdirSync(bin);
  fs.mkdirSync(path.join(root, 'home'));
  fs.writeFileSync(path.join(bin, 'curl'), FAKE_CURL, { mode: 0o755 });
  fs.writeFileSync(path.join(bin, 'uname'), `#!/bin/sh\ncase "$1" in -s) echo ${osName} ;; -m) echo ${arch} ;; esac\n`, { mode: 0o755 });
  const release = { tag_name: TAG, assets: names.map((n) => ({ browser_download_url: `${DOWNLOAD}/${n}` })) };
  const releaseFile = path.join(root, 'release.json');
  fs.writeFileSync(releaseFile, JSON.stringify([release]));
  const log = path.join(root, 'log');
  fs.writeFileSync(log, '');
  const r = spawnSync('sh', [SCRIPT, ...args], {
    env: { HOME: path.join(root, 'home'), PATH: `${bin}:/usr/bin:/bin`, RELEASE: releaseFile, LOG: log, TMPDIR: root, ...env },
    encoding: 'utf8',
  });
  const downloads = fs.readFileSync(log, 'utf8').split('\n').filter(Boolean);
  return { r, downloads, root };
}

const ELECTRON = ['AgentRQ-1.2.3.AppImage', 'AgentRQ-1.2.3-arm64.AppImage'];
const TAURI = ['AgentRQ-Tauri-1.2.3-x86_64.AppImage', 'AgentRQ-Tauri-1.2.3-aarch64.AppImage', 'agentrq-tauri-1.2.3-arm64.AppImage'];

for (const [arch, want] of [['x86_64', 'AgentRQ-1.2.3.AppImage'], ['aarch64', 'AgentRQ-1.2.3-arm64.AppImage']]) {
  for (const order of ['tauri first', 'tauri last']) {
    test(`${arch}: never picks a Tauri AppImage (${order})`, () => {
      const names = order === 'tauri first' ? [...TAURI, ...ELECTRON] : [...ELECTRON, ...TAURI];
      const { downloads } = run({ arch, names });
      assert.deepEqual(downloads, [`DOWNLOAD ${DOWNLOAD}/${want}`]);
    });
  }
}

test('a release with only Tauri assets is refused without --tauri', () => {
  const { r, downloads } = run({ arch: 'x86_64', names: TAURI });
  assert.notEqual(r.status, 0);
  assert.deepEqual(downloads, []);
});

for (const [arch, want] of [['x86_64', 'AgentRQ-Tauri-1.2.3-x86_64.AppImage'], ['aarch64', 'AgentRQ-Tauri-1.2.3-aarch64.AppImage']]) {
  for (const order of ['tauri first', 'electron first']) {
    test(`${arch}: --tauri picks Tauri AppImage (${order})`, () => {
      const names = order === 'tauri first' ? [...TAURI, ...ELECTRON] : [...ELECTRON, ...TAURI];
      const { downloads } = run({ arch, names, args: ['--tauri'] });
      assert.deepEqual(downloads, [`DOWNLOAD ${DOWNLOAD}/${want}`]);
    });
  }
}

test('--tauri with only Electron assets is refused', () => {
  const { r, downloads } = run({ arch: 'x86_64', names: ELECTRON, args: ['--tauri'] });
  assert.notEqual(r.status, 0);
  assert.deepEqual(downloads, []);
});

const ELECTRON_MAC = ['AgentRQ-1.2.3-arm64.dmg', 'AgentRQ-1.2.3-x64.dmg'];
const TAURI_MAC = ['AgentRQ-Tauri-1.2.3-arm64.dmg', 'AgentRQ-Tauri-1.2.3-x64.dmg'];

for (const [arch, want] of [['arm64', 'AgentRQ-Tauri-1.2.3-arm64.dmg'], ['x64', 'AgentRQ-Tauri-1.2.3-x64.dmg']]) {
  test(`macOS ${arch}: --tauri picks Tauri dmg`, () => {
    const names = [...TAURI_MAC, ...ELECTRON_MAC];
    const { downloads } = run({ arch, names, args: ['--tauri'], osName: 'Darwin' });
    assert.deepEqual(downloads, [`DOWNLOAD ${DOWNLOAD}/${want}`]);
  });
}

const TAURI_DEFAULT = ['AgentRQ.Tauri_0.1.0_amd64.AppImage', 'AgentRQ.Tauri_0.1.0_aarch64.AppImage'];
const TAURI_DEFAULT_MAC = ['AgentRQ.Tauri_0.1.0_x64.dmg', 'AgentRQ.Tauri_0.1.0_aarch64.dmg'];
const TAURI_RENAMED = ['AgentRQ-Tauri-1.2.3-x64.AppImage', 'AgentRQ-Tauri-1.2.3-arm64.AppImage'];

for (const [arch, want] of [['x86_64', 'AgentRQ.Tauri_0.1.0_amd64.AppImage'], ['aarch64', 'AgentRQ.Tauri_0.1.0_aarch64.AppImage']]) {
  for (const order of ['tauri first', 'electron first']) {
    test(`${arch}: --tauri picks real default Tauri AppImage (${order})`, () => {
      const names = order === 'tauri first' ? [...TAURI_DEFAULT, ...ELECTRON] : [...ELECTRON, ...TAURI_DEFAULT];
      const { downloads } = run({ arch, names, args: ['--tauri'] });
      assert.deepEqual(downloads, [`DOWNLOAD ${DOWNLOAD}/${want}`]);
    });
  }
}

test('Linux x86_64: --tauri picks amd64 when aarch64 is listed first', () => {
  const names = ['AgentRQ.Tauri_0.1.0_aarch64.AppImage', 'AgentRQ.Tauri_0.1.0_amd64.AppImage'];
  const { downloads } = run({ arch: 'x86_64', names, args: ['--tauri'] });
  assert.deepEqual(downloads, [`DOWNLOAD ${DOWNLOAD}/AgentRQ.Tauri_0.1.0_amd64.AppImage`]);
});

test('Linux aarch64: --tauri picks aarch64 when amd64 is listed first', () => {
  const names = ['AgentRQ.Tauri_0.1.0_amd64.AppImage', 'AgentRQ.Tauri_0.1.0_aarch64.AppImage'];
  const { downloads } = run({ arch: 'aarch64', names, args: ['--tauri'] });
  assert.deepEqual(downloads, [`DOWNLOAD ${DOWNLOAD}/AgentRQ.Tauri_0.1.0_aarch64.AppImage`]);
});

for (const [arch, want] of [['x86_64', 'AgentRQ-Tauri-1.2.3-x64.AppImage'], ['aarch64', 'AgentRQ-Tauri-1.2.3-arm64.AppImage']]) {
  for (const order of ['tauri first', 'electron first']) {
    test(`${arch}: --tauri picks renamed Tauri AppImage (${order})`, () => {
      const names = order === 'tauri first' ? [...TAURI_RENAMED, ...ELECTRON] : [...ELECTRON, ...TAURI_RENAMED];
      const { downloads } = run({ arch, names, args: ['--tauri'] });
      assert.deepEqual(downloads, [`DOWNLOAD ${DOWNLOAD}/${want}`]);
    });
  }
}

for (const [arch, want] of [['arm64', 'AgentRQ.Tauri_0.1.0_aarch64.dmg'], ['x64', 'AgentRQ.Tauri_0.1.0_x64.dmg']]) {
  test(`macOS ${arch}: --tauri picks real default Tauri dmg`, () => {
    const names = [...TAURI_DEFAULT_MAC, ...ELECTRON_MAC];
    const { downloads } = run({ arch, names, args: ['--tauri'], osName: 'Darwin' });
    assert.deepEqual(downloads, [`DOWNLOAD ${DOWNLOAD}/${want}`]);
  });
}

test('macOS x64: --tauri picks x64 dmg when aarch64 is listed first', () => {
  const names = ['AgentRQ.Tauri_0.1.0_aarch64.dmg', 'AgentRQ.Tauri_0.1.0_x64.dmg'];
  const { downloads } = run({ arch: 'x64', names, args: ['--tauri'], osName: 'Darwin' });
  assert.deepEqual(downloads, [`DOWNLOAD ${DOWNLOAD}/AgentRQ.Tauri_0.1.0_x64.dmg`]);
});

test('macOS arm64: --tauri picks aarch64 dmg when x64 is listed first', () => {
  const names = ['AgentRQ.Tauri_0.1.0_x64.dmg', 'AgentRQ.Tauri_0.1.0_aarch64.dmg'];
  const { downloads } = run({ arch: 'arm64', names, args: ['--tauri'], osName: 'Darwin' });
  assert.deepEqual(downloads, [`DOWNLOAD ${DOWNLOAD}/AgentRQ.Tauri_0.1.0_aarch64.dmg`]);
});

for (const [arch, want] of [['x86_64', 'AgentRQ-1.2.3.AppImage'], ['aarch64', 'AgentRQ-1.2.3-arm64.AppImage']]) {
  test(`${arch}: never picks real default Tauri AppImage`, () => {
    const names = [...TAURI_DEFAULT, ...ELECTRON];
    const { downloads } = run({ arch, names });
    assert.deepEqual(downloads, [`DOWNLOAD ${DOWNLOAD}/${want}`]);
  });
}

test('Linux: Electron install writes unchanged desktop file with StartupWMClass=AgentRQ', () => {
  const { r, root } = run({
    arch: 'x86_64',
    names: ELECTRON,
    env: { MOCK_INSTALL: '1' },
  });
  assert.equal(r.status, 0);
  const desktopPath = path.join(root, 'home', '.local', 'share', 'applications', 'agentrq.desktop');
  const content = fs.readFileSync(desktopPath, 'utf8');
  const expected = [
    '[Desktop Entry]',
    'Type=Application',
    'Name=AgentRQ',
    'Comment=Task orchestration for coding agents',
    `Exec=${path.join(root, 'home', '.local', 'bin', 'agentrq')} %u`,
    'Icon=agentrq',
    'Terminal=false',
    'Categories=Development;',
    'MimeType=x-scheme-handler/agentrq;',
    'StartupWMClass=AgentRQ',
    '',
  ].join('\n');
  assert.equal(content, expected);
});

test('Linux: Tauri install writes desktop file with StartupWMClass=agentrq-tauri', () => {
  const { r, root } = run({
    arch: 'x86_64',
    names: TAURI,
    args: ['--tauri'],
    env: { MOCK_INSTALL: '1' },
  });
  assert.equal(r.status, 0);
  const desktopPath = path.join(root, 'home', '.local', 'share', 'applications', 'agentrq-tauri.desktop');
  const content = fs.readFileSync(desktopPath, 'utf8');
  const expected = [
    '[Desktop Entry]',
    'Type=Application',
    'Name=AgentRQ Tauri',
    'Comment=Task orchestration for coding agents',
    `Exec=${path.join(root, 'home', '.local', 'bin', 'agentrq-tauri')} %u`,
    'Icon=agentrq-tauri',
    'Terminal=false',
    'Categories=Development;',
    'MimeType=x-scheme-handler/agentrq-tauri;',
    'StartupWMClass=agentrq-tauri',
    '',
  ].join('\n');
  assert.equal(content, expected);
});
