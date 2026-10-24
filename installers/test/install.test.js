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
url=""
while [ $# -gt 0 ]; do
	case "$1" in
	-o|-H) shift 2 ;;
	-*) shift ;;
	*) url="$1"; shift ;;
	esac
done
case "$url" in
*api.github.com*) cat "$RELEASE" ;;
*) echo "DOWNLOAD $url" >> "$LOG"; exit 22 ;;
esac
`;

function run({ arch, names }) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'install-'));
  const bin = path.join(root, 'bin');
  fs.mkdirSync(bin);
  fs.mkdirSync(path.join(root, 'home'));
  fs.writeFileSync(path.join(bin, 'curl'), FAKE_CURL, { mode: 0o755 });
  fs.writeFileSync(path.join(bin, 'uname'), `#!/bin/sh\ncase "$1" in -s) echo Linux ;; -m) echo ${arch} ;; esac\n`, { mode: 0o755 });
  const release = { tag_name: TAG, assets: names.map((n) => ({ browser_download_url: `${DOWNLOAD}/${n}` })) };
  const releaseFile = path.join(root, 'release.json');
  fs.writeFileSync(releaseFile, JSON.stringify([release]));
  const log = path.join(root, 'log');
  fs.writeFileSync(log, '');
  const r = spawnSync('sh', [SCRIPT], {
    env: { HOME: path.join(root, 'home'), PATH: `${bin}:/usr/bin:/bin`, RELEASE: releaseFile, LOG: log, TMPDIR: root },
    encoding: 'utf8',
  });
  const downloads = fs.readFileSync(log, 'utf8').split('\n').filter(Boolean);
  return { r, downloads };
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

test('a release with only Tauri assets is refused', () => {
  const { r, downloads } = run({ arch: 'x86_64', names: TAURI });
  assert.notEqual(r.status, 0);
  assert.deepEqual(downloads, []);
});
