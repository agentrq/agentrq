#!/bin/sh
# Copyright 2026 Contextual, Inc. https://agentrq.com
# This notice may not be modified or removed.
# SPDX-License-Identifier: Apache-2.0
#
# AgentRQ daemon (agentrqd) installer and updater.
#
#   curl -fsSL https://agentrq.com/install-agentrqd.sh -o install-agentrqd.sh
#   less install-agentrqd.sh          # this machine is about to run agents
#   sh install-agentrqd.sh
#
# It resolves the newest GitHub Release, downloads the archive matching this
# machine, verifies it against the published SHA-256 checksums, and puts
# `agentrqd` on your PATH. Running it again updates in place.
#
# It installs and nothing else. It does not enrol the machine, does not start
# anything, and does not install a service -- enrolling is where you decide
# which account may run agents here, and that is a decision to make deliberately
# with a code in front of you, not a side effect of an install.
#
# Why the checksum is mandatory here, unlike the desktop installer:
#
#   This binary takes commands from an AgentRQ account and runs them on this
#   machine. That is the whole point of it, and it is also why a download this
#   script cannot vouch for must never reach your disk. Every release publishes
#   checksums.txt, so a missing or unverifiable checksum means something is
#   wrong -- with the release or with the network -- and the right answer is to
#   stop. There is deliberately no flag to skip it.
#
#   The daemon verifies a *signed* manifest for its own self-updates
#   (`agentrqd update`), which is stronger still. This script cannot use that
#   without the release public key compiled into a binary you do not have yet,
#   which is the chicken-and-egg this solves.
#
# It never runs the daemon, and never installs it as root. agentrqd refuses to
# run as root on purpose: an agent should be able to do what you can do and no
# more. An installer that reached for sudo to work around that would be
# removing the only thing keeping it true. It does not use sudo at all: the
# binary goes into ~/.local/bin, which you own, on Linux and macOS alike, so
# nothing asks for a password. If that folder is not on your PATH yet, the
# script prints the one line that adds it, for the shell you use.
#
# Constraints worth knowing before editing (learned the hard way on the desktop
# installer next door):
#
#   * POSIX sh only. Piped into `sh` this runs under dash on most Linux
#     systems, so bashisms ([[ ]], arrays, local, ${var,,}) are out.
#   * Nothing may read stdin. With `curl ... | sh` the script *is* stdin, so a
#     `read` would consume the rest of the script as its input. Every choice is
#     a flag or an environment variable, never a prompt.
#   * ASCII only. macOS ships bash 3.2 as /bin/sh, which folds the leading byte
#     of a UTF-8 character into a preceding variable name, so "$file..." with a
#     real ellipsis dies as `file\xef: unbound variable` under `set -u`.
#
# Flags (pipe them through as: curl -fsSL ... | sh -s -- --dir ~/bin)
#
#   --version <tag>   install a specific release, e.g. 0.7.1 or v0.7.1
#   --dir <path>      install location (default ~/.local/bin)
#   --force           reinstall even when the installed version already matches
#   --help
#
# Equivalent environment variables: AGENTRQD_VERSION, AGENTRQD_INSTALL_DIR,
# AGENTRQD_FORCE. GITHUB_TOKEN, if set, is used for the API call -- only useful
# on a shared IP that has exhausted the unauthenticated rate limit.

set -eu

REPO="agentrq/agentrq"
API="https://api.github.com/repos/$REPO/releases"
RELEASES_PAGE="https://github.com/$REPO/releases"
DOCS="https://agentrq.com/docs/daemon"

VERSION="${AGENTRQD_VERSION:-}"
INSTALL_DIR="${AGENTRQD_INSTALL_DIR:-}"
FORCE="${AGENTRQD_FORCE:-}"

TMPDIR_=""

say() { printf '%s\n' "$*"; }
note() { printf '  %s\n' "$*"; }
die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

cleanup() {
	[ -n "$TMPDIR_" ] && [ -d "$TMPDIR_" ] && rm -rf "$TMPDIR_"
	return 0
}
trap cleanup EXIT INT TERM

usage() {
	cat <<'EOF'
AgentRQ daemon installer and updater.

  install-agentrqd.sh [--version <tag>] [--dir <path>] [--force]

Installs `agentrqd` on this machine and verifies it against the checksums
published with the release. It does not enrol the machine or start anything;
it prints what to run next.

  --version <tag>   install a specific release, e.g. 0.7.1 or v0.7.1
  --dir <path>      install location (default ~/.local/bin)
  --force           reinstall even when the installed version already matches
  -h, --help        this

Environment: AGENTRQD_VERSION, AGENTRQD_INSTALL_DIR, AGENTRQD_FORCE, GITHUB_TOKEN
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		[ $# -ge 2 ] || die "--version needs a tag"
		VERSION="$2"
		shift 2
		;;
	--dir)
		[ $# -ge 2 ] || die "--dir needs a path"
		INSTALL_DIR="$2"
		shift 2
		;;
	--force)
		FORCE=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*) die "unknown option: $1 (try --help)" ;;
	esac
done

# The daemon refuses to run as root, so installing it as root would put a
# root-owned binary somewhere only root can update, to run something that will
# then decline to start. Said plainly here rather than discovered later.
if [ "$(id -u)" = "0" ]; then
	die "do not run this as root. agentrqd refuses to run as root, so an agent can only do what you can. Run it as the user that will own the daemon."
fi

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v tar >/dev/null 2>&1 || die "tar is required"

# ---------------------------------------------------------------- the machine

os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
Darwin) goos="darwin" ;;
Linux) goos="linux" ;;
*)
	# Windows is a PowerShell path, not this one: the archive is a .zip and
	# PATH is a registry value rather than a directory convention.
	die "unsupported system: $os. Windows instructions are at $DOCS, downloads at $RELEASES_PAGE"
	;;
esac

case "$arch" in
arm64 | aarch64) goarch="arm64" ;;
x86_64 | amd64) goarch="amd64" ;;
*) die "unsupported architecture: $arch. See $RELEASES_PAGE" ;;
esac

# --------------------------------------------------------------- the release

auth=""
if [ -n "${GITHUB_TOKEN:-}" ]; then
	auth="Authorization: Bearer $GITHUB_TOKEN"
fi

# One place that knows how to talk to the API, so the auth header is not
# spelled out at each call site.
api_get() {
	if [ -n "$auth" ]; then
		curl -fsSL -H 'Accept: application/vnd.github+json' -H "$auth" "$1" </dev/null
	else
		curl -fsSL -H 'Accept: application/vnd.github+json' "$1" </dev/null
	fi
}

# grep -o rather than a JSON parser: jq is not installable from here, and this
# only has to survive GitHub's own formatting, pretty-printed or compact.
#
# Which release to install is decided by the asset, not by the tag.
#
# The daemon used to ship on its own series, tagged agentrqd-v<version>. Since
# 0.7.0 everything is released together under the plain v<version> tag, and
# agentrqd-v0.7.0 is the last of the old series -- so both shapes exist in the
# repository and both are installable.
#
# Matching on either prefix would still be guesswork, because **a v<version>
# tag does not mean the daemon is in it**: v0.6.5 and everything before it are
# desktop-only and carry no agentrqd archive at all. So rather than reason
# about tag names, look for the one file this platform actually needs and take
# the newest release that publishes it. That cannot pick a release it cannot
# install from, and it needs no changes the next time the tagging does.
archive_re="agentrqd_[0-9][^\"/]*_${goos}_${goarch}\.tar\.gz"

if [ -n "$VERSION" ]; then
	# Accept whatever somebody has to hand: agentrqd-v0.7.0, v0.7.0 or 0.7.0.
	bare="${VERSION#agentrqd-}"
	bare="${bare#v}"
	# The unified tag first, then the old series, so a version pinned before
	# 0.7.0 still resolves.
	release=""
	for candidate in "v${bare}" "agentrqd-v${bare}"; do
		say "Looking up agentrqd $candidate..."
		# stderr is dropped because a 404 here is an ordinary outcome -- the
		# first candidate misses for anything released under the other tag --
		# and curl's own "error: 22" would read as a failure when it is not.
		release="$(api_get "$API/tags/$candidate" 2>/dev/null)" && rc=0 || rc=$?
		if [ "$rc" -eq 0 ]; then
			tag="$candidate"
			break
		fi
		release=""
		# 22 is curl saying the server answered and said no. Anything else is
		# the network, and reporting that as "no such release" would send
		# somebody looking for a version that is sitting right there.
		[ "$rc" -eq 22 ] ||
			die "could not reach the GitHub release API. Is the network up?"
	done
	[ -n "$release" ] ||
		die "no release tagged v${bare} or agentrqd-v${bare}. Published versions are listed at $RELEASES_PAGE"
	version="$bare"
else
	say "Looking up the latest agentrqd release..."
	releases="$(api_get "$API?per_page=30")" ||
		die "could not reach the GitHub release API. Is the network up?"
	# Newest first, which is the order the API returns them in. The download
	# URL carries both the tag and the version:
	#   .../releases/download/<tag>/agentrqd_<version>_<os>_<arch>.tar.gz
	archive_url="$(printf '%s' "$releases" |
		grep -o "\"browser_download_url\": *\"[^\"]*/${archive_re}\"" |
		sed 's/^[^:]*: *"//; s/"$//' | head -n 1 || true)"
	[ -n "$archive_url" ] ||
		die "no agentrqd $goos/$goarch archive in the latest 30 releases. See $RELEASES_PAGE"
	asset_name="${archive_url##*/}"
	tag="${archive_url%/*}"
	tag="${tag##*/}"
	version="${asset_name#agentrqd_}"
	version="${version%%_*}"
	# Fetched by tag even though the archive URL is already in hand: the rest
	# of this needs the release's other assets, checksums.txt above all.
	release="$(api_get "$API/tags/$tag")" ||
		die "could not fetch the release $tag"
fi

[ -n "$version" ] || die "could not read a version out of the tag $tag"

assets="$(printf '%s' "$release" |
	grep -o '"browser_download_url": *"[^"]*"' |
	sed 's/^[^:]*: *"//; s/"$//' || true)"
[ -n "$assets" ] || die "release $tag has no downloadable assets"

pick_asset() {
	printf '%s\n' "$assets" | grep -- "$1" | head -n 1 || true
}

# goreleaser's name_template: agentrqd_<version>_<os>_<arch>.tar.gz
filename="agentrqd_${version}_${goos}_${goarch}.tar.gz"
asset="$(pick_asset "/${filename}\$")"
[ -n "$asset" ] || die "release $tag has no $goos/$goarch archive ($filename). See $RELEASES_PAGE"

# ------------------------------------------------------- where it is going

default_dir="$HOME/.local/bin"
dest_dir="$INSTALL_DIR"
if [ -z "$dest_dir" ]; then
	# Not /usr/local/bin, on macOS either: that needs a password, and the daemon
	# is a per-user thing -- it runs as whoever enrolled it.
	dest_dir="$default_dir"
	# An update goes where the last install went, when that is somewhere this
	# user can write. Installed beside it instead, the old copy would stay first
	# on PATH and keep answering to `agentrqd`.
	existing="$(command -v agentrqd 2>/dev/null || true)"
	case "$existing" in
	/*/agentrqd)
		if [ -w "$existing" ] && [ -w "${existing%/*}" ]; then
			dest_dir="${existing%/*}"
		fi
		;;
	esac
fi

# Already current? Re-downloading a release somebody already has is noise, and
# on a slow link it is a surprising amount of it.
if [ -z "$FORCE" ] && [ -x "$dest_dir/agentrqd" ]; then
	# `agentrqd version`, not `--version`: the CLI takes subcommands, and the
	# flag form exits non-zero with a usage block, which the `|| true` here
	# would have quietly turned into "not installed" forever.
	current="$("$dest_dir/agentrqd" version 2>/dev/null | head -n 1 || true)"
	case "$current" in
	*"$version"*)
		say "agentrqd $version is already installed at $dest_dir/agentrqd."
		note "Reinstall anyway with --force."
		exit 0
		;;
	esac
fi

# ------------------------------------------------------------- the download

TMPDIR_="$(mktemp -d "${TMPDIR:-/tmp}/agentrqd.XXXXXX")"
download="$TMPDIR_/$filename"

say "Downloading $filename..."

# The progress bar redraws one line with carriage returns, which is right in a
# terminal and wrong everywhere else: a log, a pipe, or anything that renders
# captured output as HTML turns every redraw into another row, because a
# browser reads a carriage return as a line break. So the bar is asked for only
# when something is watching stderr. `-sS` keeps errors and drops the bar.
if [ -t 2 ]; then
	progress='--progress-bar'
else
	progress='-sS'
fi

curl -fL "$progress" -o "$download" "$asset" </dev/null ||
	die "download failed: $asset"

# Mandatory, and fail-closed. See the note at the top: this binary runs
# commands on this machine, so an archive we cannot vouch for does not get
# unpacked, let alone installed.
checksums_url="$(pick_asset '/checksums\.txt$')"
[ -n "$checksums_url" ] || die "release $tag publishes no checksums.txt, so $filename cannot be verified. Nothing was installed."

checksums="$(curl -fsL "$checksums_url" </dev/null || true)"
[ -n "$checksums" ] || die "could not download the checksums for $tag. Nothing was installed."

expected="$(printf '%s\n' "$checksums" | awk -v want="$filename" '
	{ name = $2; sub(/^\*/, "", name) }
	name == want { print $1; exit }
')"
[ -n "$expected" ] || die "checksums.txt for $tag does not list $filename. Nothing was installed."

if command -v sha256sum >/dev/null 2>&1; then
	actual="$(sha256sum "$download" | cut -d' ' -f1)"
elif command -v shasum >/dev/null 2>&1; then
	actual="$(shasum -a 256 "$download" | cut -d' ' -f1)"
elif command -v openssl >/dev/null 2>&1; then
	actual="$(openssl dgst -sha256 "$download" | sed 's/^.*= *//')"
else
	die "no sha256 tool found (sha256sum, shasum or openssl). Cannot verify $filename, so nothing was installed."
fi

if [ "$actual" != "$expected" ]; then
	die "checksum mismatch for $filename -- the download is corrupt or has been tampered with. Nothing was installed."
fi
note "Checksum verified."

# ---------------------------------------------------------------- install

tar xzf "$download" -C "$TMPDIR_" || die "could not unpack $filename"
[ -f "$TMPDIR_/agentrqd" ] || die "$filename did not contain an agentrqd binary"

mkdir -p "$dest_dir" 2>/dev/null || true

say "Installing to $dest_dir/agentrqd..."
[ -w "$dest_dir" ] ||
	die "$dest_dir is not writable by you. Pick a folder you own, e.g. --dir \$HOME/.local/bin"
install -m 0755 "$TMPDIR_/agentrqd" "$dest_dir/agentrqd" ||
	die "could not install into $dest_dir"

# The archive carries the service definitions. They are not installed -- whether
# a machine starts agents at boot is a decision, not a default -- but somebody
# who wants one should not have to download the archive again to find it.
#
# They go in the daemon's own config directory, the one `agentrqd` builds from
# os.UserConfigDir(), so this adds nothing to the machine that enrolling would
# not create anyway.
if [ "$goos" = "darwin" ]; then
	config_dir="$HOME/Library/Application Support/agentrqd"
else
	config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/agentrqd"
fi

xml_escape() { sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g'; }
sed_escape() { sed 's/[|\\&]/\\&/g'; }

services=""
if [ -f "$TMPDIR_/agentrqd.service" ] || [ -f "$TMPDIR_/com.agentrq.agentrqd.plist" ]; then
	if mkdir -p "$config_dir" 2>/dev/null; then
		cp "$TMPDIR_/agentrqd.service" "$config_dir/" 2>/dev/null || true
		# launchd expands nothing in ProgramArguments, so the LaunchAgent runs
		# `exec "$HOME/.local/bin/agentrqd" serve` through sh. The saved copy
		# names the path it was really installed to, which --dir can change.
		# Archives before that named a literal /Users/you/... path instead, and
		# a pinned --version can still bring one, so that is replaced too.
		if [ -f "$TMPDIR_/com.agentrq.agentrqd.plist" ]; then
			bin_abs="$(printf '%s' "$dest_dir/agentrqd" | xml_escape | sed_escape)"
			bin_sh="$(printf '%s' "$dest_dir/agentrqd" |
				sed "s/'/'\\\\''/g; s/^/'/; s/\$/'/" | xml_escape | sed_escape)"
			sed -e "s|<string>/[^<]*/agentrqd</string>|<string>$bin_abs</string>|" \
				-e "s|\"\\\$HOME/\\.local/bin/agentrqd\"|$bin_sh|" \
				"$TMPDIR_/com.agentrq.agentrqd.plist" >"$config_dir/com.agentrq.agentrqd.plist" 2>/dev/null || true
		fi
		services="$config_dir"
	fi
fi

# ------------------------------------------------------------------- done

say ""
say "Installed agentrqd $version to $dest_dir/agentrqd"

# A binary that is not on PATH is a binary the next instruction cannot name.
# The line that fixes it depends on the shell, and the script cannot ask which
# one (stdin is the script), so it reads $SHELL and prints that one first.
case "$dest_dir" in
"$HOME"/*) shown_dir="\$HOME/${dest_dir#"$HOME"/}" ;;
*) shown_dir="$dest_dir" ;;
esac
# The startup file each shell will actually read.
rc_file() {
	case "$1" in
	zsh)
		# ZDOTDIR moves every zsh startup file, and ~/.zshrc is then never read.
		if [ -n "${ZDOTDIR:-}" ]; then
			printf '%s' '"$ZDOTDIR/.zshrc"'
		else
			printf '%s' '~/.zshrc'
		fi
		;;
	bash)
		# macOS Terminal opens login shells, and a login bash reads only the
		# first of these that exists: creating .bash_profile beside somebody's
		# .profile would quietly stop bash reading the .profile.
		if [ "$goos" = "darwin" ]; then
			for rc in .bash_profile .bash_login .profile; do
				if [ -f "$HOME/$rc" ]; then
					printf '~/%s' "$rc"
					return
				fi
			done
			printf '%s' '~/.bash_profile'
		else
			printf '%s' '~/.bashrc'
		fi
		;;
	*) printf '%s' '~/.profile' ;;
	esac
}
path_line() {
	case "$1" in
	fish) printf '%s' "fish -c 'fish_add_path \"$shown_dir\"'" ;;
	*) printf '%s' "echo 'export PATH=\"$shown_dir:\$PATH\"' >> $(rc_file "$1")" ;;
	esac
}
# Loads it into the terminal that ran the installer. fish needs nothing:
# fish_add_path sets a universal variable, which running fish sessions share.
reload_line() {
	case "$1" in
	fish) ;;
	zsh | bash) printf 'source %s' "$(rc_file "$1")" ;;
	*) printf '. %s' "$(rc_file "$1")" ;;
	esac
}
case ":${PATH}:" in
*":$dest_dir:"*)
	# On PATH, but an older copy somewhere earlier would still win.
	found="$(command -v agentrqd 2>/dev/null || true)"
	case "$found" in
	/*/agentrqd)
		if [ "$found" != "$dest_dir/agentrqd" ]; then
			say ""
			note "Another agentrqd comes first on your PATH: $found"
			note "Remove it, or 'agentrqd' will keep running that one."
		fi
		;;
	esac
	;;
*)
	user_shell="${SHELL:-sh}"
	user_shell="${user_shell##*/}"
	say ""
	note "$dest_dir is not on your PATH yet. Add it by running this once:"
	note ""
	note "  $(path_line "$user_shell")"
	note ""
	reload="$(reload_line "$user_shell")"
	if [ -n "$reload" ]; then
		note "then open a new terminal window, or run: $reload"
	else
		note "It takes effect in every fish session right away."
	fi
	note ""
	note "Using a different shell?"
	for other in zsh bash fish sh; do
		[ "$other" = "$user_shell" ] && continue
		note "  $(printf '%-5s' "$other:") $(path_line "$other")"
	done
	note "Until then, run it by its full path: $dest_dir/agentrqd"
	;;
esac

say ""
say "Next: enrol this machine against your account."
note "Machines -> Add machine in AgentRQ gives you a short code. Then run:"
note "  agentrqd enroll --server https://your-agentrq-server --code ABCD-EFGH"
note ""
note "Enrolling lets anyone who can sign in to that account run commands on"
note "this machine, as you. Run it here, on the machine itself."
note ""
note "Then start it in the foreground to check it connects:"
note "  agentrqd serve"
note ""
if [ -n "$services" ]; then
	note "To run it at boot, service definitions were saved to:"
	note "  $services"
fi
note "Full guide: $DOCS"
