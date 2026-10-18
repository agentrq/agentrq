// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

// Tests for install-agentrqd.sh, run end to end under `sh` against a fake
// GitHub: `curl` and `uname` are stubs on PATH, everything else is real.
// Run with `node --test installers/test/`.
'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const crypto = require('node:crypto');
const { execFileSync, spawnSync } = require('node:child_process');

const SCRIPT = path.join(__dirname, '../install-agentrqd.sh');
const VERSION = '1.2.3';
const TAG = `v${VERSION}`;
const API = 'api.github.com/repos/agentrq/agentrq/releases';
const DOWNLOAD = `https://github.com/agentrq/agentrq/releases/download/${TAG}`;

function has(bin) {
  return spawnSync('sh', ['-c', `command -v ${bin}`]).status === 0;
}

// Serves a file from $FIXTURES named after the URL, or fails the way curl -f
// does on a 404.
const FAKE_CURL = `#!/bin/sh
out=""; url=""
while [ $# -gt 0 ]; do
	case "$1" in
	-o) out="$2"; shift 2 ;;
	-H) shift 2 ;;
	-*) shift ;;
	*) url="$1"; shift ;;
	esac
done
f="$FIXTURES/$(printf '%s' "$url" | sed 's|^https://||; s|?|_|')"
[ -f "$f" ] || exit 22
if [ -n "$out" ]; then cp "$f" "$out"; else cat "$f"; fi
`;

const PLIST_ARGS = {
  // Through sh, because launchd expands neither ~ nor $HOME.
  current: '<string>/bin/sh</string>\n    <string>-c</string>\n    <string>exec "$HOME/.local/bin/agentrqd" serve</string>',
  // Archives before that named a literal path for the user to fill in.
  old: '<string>/Users/you/.local/bin/agentrqd</string>\n    <string>serve</string>',
};

// A machine of the given kind, with a release published for it, and `files`
// already in its home folder.
function machine({ goos = 'linux', goarch = 'amd64', plist = 'current', files = {} } = {}) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'install-agentrqd-'));
  const home = path.join(root, 'home');
  const bin = path.join(root, 'bin');
  const fixtures = path.join(root, 'fixtures');
  for (const d of [home, bin, fixtures]) fs.mkdirSync(d, { recursive: true });
  for (const [name, body] of Object.entries(files)) fs.writeFileSync(path.join(home, name), body);

  fs.writeFileSync(path.join(bin, 'curl'), FAKE_CURL, { mode: 0o755 });
  fs.writeFileSync(
    path.join(bin, 'uname'),
    `#!/bin/sh\ncase "$1" in -s) echo ${goos === 'darwin' ? 'Darwin' : 'Linux'} ;; -m) echo ${goarch === 'arm64' ? 'arm64' : 'x86_64'} ;; esac\n`,
    { mode: 0o755 },
  );

  // The archive, as goreleaser lays it out.
  const pkg = path.join(root, 'pkg');
  fs.mkdirSync(pkg);
  fs.writeFileSync(path.join(pkg, 'agentrqd'), `#!/bin/sh\necho "agentrqd ${VERSION} $*"\n`, { mode: 0o755 });
  fs.writeFileSync(
    path.join(pkg, 'com.agentrq.agentrqd.plist'),
    `<key>ProgramArguments</key>\n  <array>\n    ${PLIST_ARGS[plist]}\n  </array>\n`,
  );
  const filename = `agentrqd_${VERSION}_${goos}_${goarch}.tar.gz`;
  const download = path.join(fixtures, `github.com/agentrq/agentrq/releases/download/${TAG}`);
  fs.mkdirSync(download, { recursive: true });
  execFileSync('tar', ['czf', path.join(download, filename), '-C', pkg, 'agentrqd', 'com.agentrq.agentrqd.plist']);
  const sum = crypto.createHash('sha256').update(fs.readFileSync(path.join(download, filename))).digest('hex');
  fs.writeFileSync(path.join(download, 'checksums.txt'), `${sum}  ${filename}\n`);

  const release = {
    tag_name: TAG,
    assets: [`${DOWNLOAD}/${filename}`, `${DOWNLOAD}/checksums.txt`].map((u) => ({ browser_download_url: u })),
  };
  fs.mkdirSync(path.join(fixtures, API, 'tags'), { recursive: true });
  fs.writeFileSync(path.join(fixtures, `${API}_per_page=30`), JSON.stringify([release], null, 2));
  fs.writeFileSync(path.join(fixtures, API, 'tags', TAG), JSON.stringify(release, null, 2));

  return { root, home, bin, fixtures };
}

function install(m, { shell, path: extraPath = [], args = [], env: extra = {} } = {}) {
  const env = {
    ...extra,
    HOME: m.home,
    PATH: [...extraPath, m.bin, '/usr/bin', '/bin'].join(':'),
    FIXTURES: m.fixtures,
    TMPDIR: m.root,
  };
  if (shell !== undefined) env.SHELL = shell;
  const r = spawnSync('sh', [SCRIPT, ...args], { env, encoding: 'utf8' });
  assert.equal(r.status, 0, `install failed:\n${r.stdout}\n${r.stderr}`);
  return r.stdout;
}

// The line the script tells you to run, the one under "Add it by running this once".
function hint(out) {
  const lines = out.split('\n');
  const at = lines.findIndex((l) => l.includes('is not on your PATH yet'));
  if (at < 0) return null;
  return lines[at + 2].trim();
}

test('installs into ~/.local/bin and verifies the checksum', () => {
  const m = machine();
  const out = install(m, { shell: '/bin/bash' });
  assert.match(out, /Checksum verified\./);
  const bin = path.join(m.home, '.local/bin/agentrqd');
  assert.equal(execFileSync(bin, { encoding: 'utf8' }).trim(), `agentrqd ${VERSION}`);
});

test('refuses an archive whose checksum does not match, and installs nothing', () => {
  const m = machine();
  const sums = path.join(m.fixtures, `github.com/agentrq/agentrq/releases/download/${TAG}/checksums.txt`);
  fs.writeFileSync(sums, fs.readFileSync(sums, 'utf8').replace(/^\w+/, '0'.repeat(64)));
  const r = spawnSync('sh', [SCRIPT], {
    env: { HOME: m.home, PATH: `${m.bin}:/usr/bin:/bin`, FIXTURES: m.fixtures, TMPDIR: m.root, SHELL: '/bin/zsh' },
    encoding: 'utf8',
  });
  assert.notEqual(r.status, 0);
  assert.match(r.stderr, /checksum mismatch/);
  assert.equal(fs.existsSync(path.join(m.home, '.local/bin/agentrqd')), false);
});

// Not everybody uses zsh: the line offered first is for the shell in $SHELL.
const SHELLS = [
  { shell: '/bin/zsh', goos: 'linux', want: `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc` },
  { shell: '/usr/local/bin/zsh', goos: 'darwin', want: `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc` },
  { shell: '/bin/bash', goos: 'linux', want: `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc` },
  // Terminal.app opens login shells, which read .bash_profile and not .bashrc.
  { shell: '/bin/bash', goos: 'darwin', want: `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bash_profile` },
  { shell: '/opt/homebrew/bin/fish', goos: 'darwin', want: `fish -c 'fish_add_path "$HOME/.local/bin"'` },
  { shell: '/bin/tcsh', goos: 'linux', want: `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.profile` },
  { shell: undefined, goos: 'linux', want: `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.profile` },
];

// bash, which is macOS's `sh`, fills in an unset $SHELL from the user's login
// shell before the script runs, so there it cannot be unset at all.
const shFillsShell = spawnSync('sh', ['-c', 'printf %s "${SHELL-}"'], { env: {}, encoding: 'utf8' }).stdout !== '';

for (const { shell, goos, want } of SHELLS) {
  const skip = shell === undefined && shFillsShell && 'sh sets $SHELL itself';
  test(`offers the PATH line for ${shell || 'an unset $SHELL'} on ${goos}`, { skip }, () => {
    const out = install(machine({ goos }), { shell });
    assert.equal(hint(out), want);
  });
}

test('lists the other shells after the one in $SHELL, not the same one twice', () => {
  const out = install(machine(), { shell: '/bin/bash' });
  assert.match(out, /Using a different shell\?/);
  for (const other of ['zsh:', 'fish:', 'sh:']) assert.ok(out.includes(`  ${other}`), other);
  assert.equal(out.includes('  bash:'), false);
});

test('says nothing about PATH when the folder is already on it', () => {
  const m = machine();
  const out = install(m, { shell: '/bin/zsh', path: [path.join(m.home, '.local/bin')] });
  assert.equal(hint(out), null);
  assert.doesNotMatch(out, /not on your PATH/);
});

test('names a custom --dir as given', () => {
  const m = machine();
  const dir = path.join(m.root, 'tools');
  const out = install(m, { shell: '/bin/zsh', args: ['--dir', dir] });
  assert.equal(hint(out), `echo 'export PATH="${dir}:$PATH"' >> ~/.zshrc`);
});

// The real test of a hint: run it, open a new shell the way a terminal would,
// and find agentrqd there.
const RUNNABLE = [
  { shell: 'zsh', goos: 'linux', open: ['-i', '-c'] },
  { shell: 'zsh', goos: 'linux', open: ['-i', '-c'], zdotdir: true },
  { shell: 'bash', goos: 'linux', open: ['-i', '-c'] },
  { shell: 'bash', goos: 'darwin', open: ['-l', '-c'] },
  // The one that matters: their .profile must still be read afterwards.
  { shell: 'bash', goos: 'darwin', open: ['-l', '-c'], files: { '.profile': 'export KEPT=1\n' } },
  { shell: 'sh', goos: 'linux', open: ['-l', '-c'], loginShell: 'dash' },
];

for (const { shell, goos, open, loginShell, zdotdir, files = {} } of RUNNABLE) {
  const run = loginShell || shell;
  const with_ = [zdotdir && 'ZDOTDIR', ...Object.keys(files)].filter(Boolean);
  const name = `the ${shell} line on ${goos}${with_.length ? ` with ${with_.join(', ')}` : ''} puts agentrqd on PATH in a new ${shell}`;
  test(name, { skip: !has(run) && `${run} not installed` }, () => {
    const m = machine({ goos, files });
    const env = { HOME: m.home, PATH: '/usr/bin:/bin', TERM: 'dumb' };
    if (zdotdir) {
      env.ZDOTDIR = path.join(m.root, 'zdot');
      fs.mkdirSync(env.ZDOTDIR);
    }
    const out = install(m, { shell: `/bin/${shell}`, env: zdotdir ? { ZDOTDIR: env.ZDOTDIR } : {} });
    execFileSync('sh', ['-c', hint(out)], { env });
    const found = spawnSync(run, [...open, 'command -v agentrqd; echo "kept=$KEPT"'], { env, encoding: 'utf8' });
    const lines = found.stdout.trim().split('\n');
    assert.ok(lines.includes(path.join(m.home, '.local/bin/agentrqd')), found.stdout + found.stderr);
    if (files['.profile']) assert.ok(lines.includes('kept=1'), 'their ~/.profile is no longer read');
    if (zdotdir) assert.equal(fs.existsSync(path.join(m.home, '.zshrc')), false);
  });
}

test('zsh with ZDOTDIR is pointed at the .zshrc zsh will read', () => {
  const out = install(machine(), { shell: '/bin/zsh', env: { ZDOTDIR: '/somewhere' } });
  assert.equal(hint(out), `echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$ZDOTDIR/.zshrc"`);
});

// A login bash reads only the first of these that exists.
for (const existing of ['.bash_profile', '.bash_login', '.profile']) {
  test(`bash on macOS adds to the ~/${existing} it already has`, () => {
    const files = { [existing]: '# theirs\n' };
    if (existing !== '.profile') files['.profile'] = '# read only when nothing above exists\n';
    const out = install(machine({ goos: 'darwin', files }), { shell: '/bin/bash' });
    assert.equal(hint(out), `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/${existing}`);
  });
}

const RELOAD = [
  { shell: '/bin/zsh', want: 'or run: source ~/.zshrc' },
  { shell: '/bin/bash', want: 'or run: source ~/.bashrc' },
  { shell: '/bin/sh', want: 'or run: . ~/.profile' },
  { shell: '/usr/bin/fish', want: 'It takes effect in every fish session right away.' },
];
for (const { shell, want } of RELOAD) {
  test(`tells ${shell} how to use it in this terminal too`, () => {
    assert.ok(install(machine(), { shell }).includes(want));
  });
}

function savedPlist(m) {
  return fs.readFileSync(path.join(m.home, 'Library/Application Support/agentrqd/com.agentrq.agentrqd.plist'), 'utf8');
}

// What launchd does with ProgramArguments: run the first as the program and
// pass the rest, with no shell of its own and nothing expanded.
function launch(m, plist) {
  const block = plist.match(/<key>ProgramArguments<\/key>\s*<array>([\s\S]*?)<\/array>/)[1];
  const args = [...block.matchAll(/<string>([^<]*)<\/string>/g)].map((x) =>
    x[1].replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&'),
  );
  return spawnSync(args[0], args.slice(1), { env: { HOME: m.home, PATH: '/usr/bin:/bin' }, encoding: 'utf8' });
}

test('the saved LaunchAgent starts the installed agentrqd serve', () => {
  const m = machine({ goos: 'darwin', goarch: 'arm64' });
  install(m, { shell: '/bin/zsh' });
  assert.equal(launch(m, savedPlist(m)).stdout.trim(), `agentrqd ${VERSION} serve`);
});

test('the saved LaunchAgent follows --dir, whatever the path holds', () => {
  const m = machine({ goos: 'darwin' });
  const dir = path.join(m.root, "it's & <odd> $HOME");
  install(m, { shell: '/bin/zsh', args: ['--dir', dir] });
  const r = launch(m, savedPlist(m));
  assert.equal(r.stdout.trim(), `agentrqd ${VERSION} serve`, r.stderr);
});

test('an older archive naming /Users/you/ is saved with the real path', () => {
  const m = machine({ goos: 'darwin', plist: 'old' });
  install(m, { shell: '/bin/zsh' });
  const plist = savedPlist(m);
  assert.doesNotMatch(plist, /\/Users\/you\//);
  assert.equal(launch(m, plist).stdout.trim(), `agentrqd ${VERSION} serve`);
});

// The file as shipped must start the daemon for anybody, without editing.
test('the LaunchAgent in the archive starts ~/.local/bin/agentrqd for any user', () => {
  const m = machine({ goos: 'darwin' });
  install(m, { shell: '/bin/zsh' });
  const shipped = `<key>ProgramArguments</key><array>${PLIST_ARGS.current}</array>`;
  assert.equal(launch(m, shipped).stdout.trim(), `agentrqd ${VERSION} serve`);
});
