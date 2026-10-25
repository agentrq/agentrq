#!/bin/sh
# Copyright 2026 Contextual, Inc. https://agentrq.com
# This notice may not be modified or removed.
# SPDX-License-Identifier: Apache-2.0
#
# AgentRQ desktop installer and updater.
#
#   curl -fsSL https://agentrq.com/install.sh | sh
#
# The same command installs and updates: it resolves the newest GitHub Release,
# downloads the artifact matching this machine, verifies it against the checksum
# electron-builder publishes, and swaps it into place.
#
# Why this exists at all: the macOS builds are unsigned, and Squirrel.Mac
# refuses to install an update onto an unsigned app, so the in-app updater can
# detect a new version but never apply it. This script is the supported way for
# a macOS user to move between versions. It also clears the quarantine
# attribute, which is what makes Gatekeeper complain on first launch -- so an
# install done this way never shows the "developer cannot be verified" dialog.
#
# Constraints worth knowing before editing:
#
#   * POSIX sh only. Piped into `sh` this runs under dash on most Linux
#     systems, so bashisms ([[ ]], arrays, local, ${var,,}) are out.
#   * Nothing may read stdin. With `curl ... | sh` the script *is* stdin, so a
#     `read` would consume the rest of the script as its input. Every choice is
#     therefore a flag or an environment variable, never a prompt. Heredocs are
#     fine -- those are parsed with the script rather than read at runtime.
#   * ASCII only. macOS ships bash 3.2 as /bin/sh, and it folds the leading
#     byte of a UTF-8 character into a preceding variable name: "$filename..."
#     with a real ellipsis dies as `filename\xef: unbound variable` under
#     `set -u`. dash is unaffected, so this breaks on exactly the platform the
#     script exists to serve.
#
# Flags (pipe them through as: curl -fsSL ... | sh -s -- --quit)
#
#   --version <tag>   install a specific release, e.g. v0.5.0
#   --dir <path>      install location (default /Applications, ~/.local/bin)
#   --quit            on macOS, quit a running AgentRQ instead of refusing
#   --force           reinstall even when the current version already matches
#   --help
#
# Equivalent environment variables: AGENTRQ_VERSION, AGENTRQ_INSTALL_DIR,
# AGENTRQ_QUIT, AGENTRQ_FORCE. GITHUB_TOKEN, if set, is used for the API call --
# only useful on a shared IP that has exhausted the unauthenticated rate limit.

set -eu

REPO="agentrq/agentrq"
API="https://api.github.com/repos/$REPO/releases"
RELEASES_PAGE="https://github.com/$REPO/releases"
RAW="https://raw.githubusercontent.com/$REPO"

VERSION="${AGENTRQ_VERSION:-}"
INSTALL_DIR="${AGENTRQ_INSTALL_DIR:-}"
QUIT="${AGENTRQ_QUIT:-}"
FORCE="${AGENTRQ_FORCE:-}"
TAURI="${AGENTRQ_TAURI:-}"

# Set once a disk image is attached, so the exit trap can detach it.
MOUNTPOINT=""
TMPDIR_=""

say() { printf '%s\n' "$*"; }
note() { printf '  %s\n' "$*"; }
die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
AgentRQ desktop installer and updater.

Usage: install.sh [--version <tag>] [--dir <path>] [--quit] [--force] [--tauri]

  curl -fsSL https://agentrq.com/install.sh | sh
  curl -fsSL https://agentrq.com/install.sh | sh -s -- --quit
  curl -fsSL https://agentrq.com/install.sh | sh -s -- --tauri
EOF
}

cleanup() {
	# Detach first: the mountpoint lives inside the temp directory, and macOS
	# will not let it be removed while an image is still attached to it.
	if [ -n "$MOUNTPOINT" ]; then
		hdiutil detach "$MOUNTPOINT" -quiet >/dev/null 2>&1 || true
	fi
	if [ -n "$TMPDIR_" ]; then
		rm -rf "$TMPDIR_"
	fi
}
trap cleanup EXIT INT TERM

while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		[ $# -ge 2 ] || die "--version needs a tag, e.g. --version v0.5.0"
		VERSION="$2"
		shift 2
		;;
	--dir)
		[ $# -ge 2 ] || die "--dir needs a path"
		INSTALL_DIR="$2"
		shift 2
		;;
	--quit)
		QUIT=1
		shift
		;;
	--force)
		FORCE=1
		shift
		;;
	--tauri)
		TAURI=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*) die "unknown option: $1 (try --help)" ;;
	esac
done

command -v curl >/dev/null 2>&1 || die "curl is required"

# ---------------------------------------------------------------- the machine

os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
Darwin) platform="mac" ;;
Linux) platform="linux" ;;
*)
	# Windows installs and updates itself through the NSIS installer, so there
	# is nothing useful for this script to do there.
	die "unsupported system: $os. Download an installer from $RELEASES_PAGE"
	;;
esac

case "$arch" in
arm64 | aarch64) arch="arm64" ;;
x86_64 | amd64 | x64) arch="x64" ;;
*) die "unsupported architecture: $arch. See $RELEASES_PAGE" ;;
esac

if [ -n "$TAURI" ]; then
	app_product="AgentRQ Tauri"
	app_bundle="AgentRQ Tauri.app"
	bin_name="agentrq-tauri"
	desktop_file="agentrq-tauri.desktop"
	marker_file=".agentrq-tauri-version"
	scheme="agentrq-tauri"
	wm_class="agentrq-tauri"
	update_cmd="curl -fsSL https://agentrq.com/install.sh | sh -s -- --tauri"
else
	app_product="AgentRQ"
	app_bundle="AgentRQ.app"
	bin_name="agentrq"
	desktop_file="agentrq.desktop"
	marker_file=".agentrq-version"
	scheme="agentrq"
	wm_class="AgentRQ"
	update_cmd="curl -fsSL https://agentrq.com/install.sh | sh"
fi

# --------------------------------------------------------------- the release

if [ -n "$VERSION" ]; then
	api_url="$API/tags/$VERSION"
else
	api_url="$API/latest"
fi

auth=""
if [ -n "${GITHUB_TOKEN:-}" ]; then
	auth="Authorization: Bearer $GITHUB_TOKEN"
fi

say "Looking up the latest AgentRQ release..."
if [ -n "$auth" ]; then
	release="$(curl -fsSL -H 'Accept: application/vnd.github+json' -H "$auth" "$api_url" </dev/null)" ||
		die "could not fetch $api_url -- no such release, or GitHub is unreachable"
else
	release="$(curl -fsSL -H 'Accept: application/vnd.github+json' "$api_url" </dev/null)" ||
		die "could not fetch $api_url -- no such release, or GitHub is unreachable"
fi

# grep -o rather than a JSON parser: jq is not installable from here, and this
# only has to survive GitHub's own formatting, pretty-printed or compact.
json_field() {
	printf '%s' "$release" | grep -o "\"$1\": *\"[^\"]*\"" | head -n 1 |
		sed 's/^[^:]*: *"//; s/"$//'
}

tag="$(json_field tag_name)"
[ -n "$tag" ] || die "no published release found at $api_url"
version="${tag#v}"

assets="$(printf '%s' "$release" |
	grep -o '"browser_download_url": *"[^"]*"' |
	sed 's/^[^:]*: *"//; s/"$//' || true)"
if [ -n "$TAURI" ]; then
	# The Tauri build ships beside Electron under -tauri- names.
	assets="$(printf '%s\n' "$assets" | grep -i -e '-tauri-' -e 'tauri' || true)"
	[ -n "$assets" ] || die "release $tag has no downloadable Tauri assets"
else
	# The Tauri build ships beside the Electron one under AgentRQ-Tauri-* names;
	# this script installs Electron, so none of those may ever be picked.
	assets="$(printf '%s\n' "$assets" | grep -iv -e '-tauri-' -e 'tauri' || true)"
	[ -n "$assets" ] || die "release $tag has no downloadable assets"
fi

pick_asset() {
	printf '%s\n' "$assets" | grep -i -- "$1" | head -n 1 || true
}

if [ "$platform" = "mac" ]; then
	if [ -n "$TAURI" ]; then
		if [ "$arch" = "arm64" ]; then
			asset="$(printf '%s\n' "$assets" | grep -i '\.dmg$' | grep -i -e 'arm64' -e 'aarch64' | head -n 1 || true)"
		else
			asset="$(printf '%s\n' "$assets" | grep -i '\.dmg$' | grep -i -e 'x64' -e 'x86_64' | head -n 1 || true)"
		fi
	else
		# electron-builder.yml pins the dmg name to include the architecture, so
		# both slices are always distinguishable.
		asset="$(pick_asset "-${arch}\\.dmg$")"
	fi
	[ -n "$asset" ] || die "release $tag has no macOS $arch disk image"
else
	# AppImage over deb: no root needed, and it works on any distribution.
	# electron-builder only appends the architecture for non-x64 builds, so
	# x64 is identified by the absence of an arm64 suffix rather than by name.
	appimages="$(printf '%s\n' "$assets" | grep -i '\.AppImage$' || true)"
	if [ -n "$TAURI" ]; then
		if [ "$arch" = "arm64" ]; then
			asset="$(printf '%s\n' "$appimages" | grep -i -e 'arm64' -e 'aarch64' | head -n 1 || true)"
		else
			asset="$(printf '%s\n' "$appimages" | grep -i -e 'x86_64' -e 'x64' -e 'amd64' | head -n 1 || true)"
		fi
	else
		if [ "$arch" = "arm64" ]; then
			asset="$(printf '%s\n' "$appimages" | grep -i 'arm64' | head -n 1 || true)"
		else
			asset="$(printf '%s\n' "$appimages" | grep -iv 'arm64' | head -n 1 || true)"
		fi
	fi
	[ -n "$asset" ] || die "release $tag has no Linux $arch AppImage"
fi

filename="${asset##*/}"

# ------------------------------------------------------- already up to date?

if [ -z "$FORCE" ]; then
	installed=""
	if [ "$platform" = "mac" ]; then
		probe="${INSTALL_DIR:-/Applications}/$app_bundle"
		[ -d "$probe" ] || probe="$HOME/Applications/$app_bundle"
		if [ -d "$probe" ]; then
			installed="$(defaults read "$probe/Contents/Info" \
				CFBundleShortVersionString 2>/dev/null || true)"
		fi
	else
		# An AppImage does not report its version without being run, so the
		# installer records what it wrote alongside the binary.
		marker="${INSTALL_DIR:-$HOME/.local/bin}/$marker_file"
		if [ -f "$marker" ]; then
			installed="$(cat "$marker" 2>/dev/null || true)"
		fi
	fi

	if [ -n "$installed" ] && [ "$installed" = "$version" ]; then
		say "$app_product $version is already installed. Nothing to do."
		note "Reinstall anyway with --force."
		exit 0
	fi
fi

# ----------------------------------------------------------------- download

TMPDIR_="$(mktemp -d "${TMPDIR:-/tmp}/agentrq.XXXXXX")"
download="$TMPDIR_/$filename"

say "Downloading $filename..."
curl -fL --progress-bar -o "$download" "$asset" </dev/null ||
	die "download failed: $asset"

# Verify against the manifest electron-updater already relies on. This matters
# more than usual here: the builds are unsigned, so the checksum is the only
# integrity signal between the release and the disk. Best effort by design -- a
# missing manifest or a machine without openssl warns rather than blocks.
case "$platform" in
mac) manifest_name="latest-mac.yml" ;;
linux)
	if [ "$arch" = "arm64" ]; then
		manifest_name="latest-linux-arm64.yml"
	else
		manifest_name="latest-linux.yml"
	fi
	;;
esac

manifest_url="$(pick_asset "/${manifest_name}$")"
expected=""
if [ -n "$manifest_url" ]; then
	manifest="$(curl -fsL "$manifest_url" </dev/null || true)"
	# files:
	#   - url: AgentRQ-0.5.0-arm64.dmg
	#     sha512: <base64>
	expected="$(printf '%s\n' "$manifest" | awk -v want="$filename" '
		$1 == "-" && $2 == "url:" && $3 == want { found = 1; next }
		found && $1 == "sha512:" { print $2; exit }
		found && $1 == "-" { found = 0 }
	' | sed 's/^"//; s/"$//' || true)"
fi

if [ -z "$expected" ]; then
	note "No published checksum for $filename; skipping verification."
elif ! command -v openssl >/dev/null 2>&1; then
	note "openssl not found; skipping checksum verification."
else
	actual="$(openssl dgst -sha512 -binary "$download" | openssl base64 -A)"
	if [ "$actual" != "$expected" ]; then
		die "checksum mismatch for $filename -- the download is corrupt or has been tampered with. Nothing was installed."
	fi
	note "Checksum verified."
fi

# ------------------------------------------------------------------ install

if [ "$platform" = "mac" ]; then
	dest_dir="$INSTALL_DIR"
	if [ -z "$dest_dir" ]; then
		# A managed Mac can have a read-only /Applications; a per-user install
		# is the working fallback rather than a failure.
		if [ -w /Applications ]; then
			dest_dir="/Applications"
		else
			dest_dir="$HOME/Applications"
		fi
	fi
	mkdir -p "$dest_dir"
	[ -w "$dest_dir" ] || die "$dest_dir is not writable. Pass --dir <path> to install elsewhere."

	app="$dest_dir/$app_bundle"

	# Replacing the bundle under a running app leaves it in a state where the
	# window still works but every later launch is broken, which is a confusing
	# thing to discover hours afterwards.
	if pgrep -f "/$app_bundle/Contents/MacOS/" >/dev/null 2>&1; then
		[ -n "$QUIT" ] || die "$app_product is running. Quit it and run this again, or pass --quit to have the installer close it."
		say "Quitting $app_product..."
		osascript -e "quit app \"$app_product\"" >/dev/null 2>&1 || true
		waited=0
		while pgrep -f "/$app_bundle/Contents/MacOS/" >/dev/null 2>&1; do
			[ "$waited" -lt 15 ] || die "$app_product did not quit. Close it manually and run this again."
			sleep 1
			waited=$((waited + 1))
		done
	fi

	say "Installing to $app..."
	MOUNTPOINT="$TMPDIR_/mnt"
	mkdir -p "$MOUNTPOINT"
	# hdiutil attach now prints a deprecation warning pointing at the "image"
	# verb diskutil gained in macOS 15, and names the replacement flags in the
	# warning text itself. Probed rather than gated on a macOS version: an
	# "Unrecognized verb" is how diskutil reports that this system predates
	# it, which is a fact rather than a guess about which OS shipped it.
	if diskutil image </dev/null 2>&1 | grep -qi 'unrecognized verb'; then
		hdiutil attach -nobrowse -readonly -noautoopen -mountpoint "$MOUNTPOINT" \
			"$download" >/dev/null </dev/null || die "could not open $filename"
	else
		diskutil image attach --mountOptions nobrowse --readOnly --mountPoint "$MOUNTPOINT" \
			"$download" >/dev/null </dev/null || die "could not open $filename"
	fi

	src_app="$(find "$MOUNTPOINT" -maxdepth 1 -name '*.app' 2>/dev/null | head -n 1 || true)"
	[ -n "$src_app" ] || die "no application found inside $filename"

	# Stage the copy, then swap. The existing install is only removed once the
	# new one is complete on the same volume, so an interrupted copy or a full
	# disk cannot leave the machine with no AgentRQ at all.
	staging="$dest_dir/.$app_bundle.incoming.$$"
	rm -rf "$staging"
	cp -R "$src_app" "$staging" || die "could not copy the application into $dest_dir"

	# The reason this script is nicer than downloading the dmg by hand: without
	# clearing quarantine, an unsigned build trips Gatekeeper, and on macOS 15
	# and later there is no right-click bypass left -- the user has to go
	# digging in System Settings.
	xattr -dr com.apple.quarantine "$staging" 2>/dev/null || true

	rm -rf "$app"
	mv "$staging" "$app"

	hdiutil detach "$MOUNTPOINT" -quiet >/dev/null 2>&1 || true
	MOUNTPOINT=""

	say ""
	say "$app_product $version installed to $app"
	if [ -n "$TAURI" ]; then
		note "Launch it with: open -a '$app_product'"
	else
		note "Launch it with: open -a $app_product"
	fi
else
	bindir="${INSTALL_DIR:-$HOME/.local/bin}"
	mkdir -p "$bindir"
	[ -w "$bindir" ] || die "$bindir is not writable. Pass --dir <path> to install elsewhere."

	target="$bindir/$bin_name"
	staging="$target.incoming.$$"

	say "Installing to $target..."
	cp "$download" "$staging"
	chmod 755 "$staging"
	mv "$staging" "$target"
	printf '%s\n' "$version" >"$bindir/$marker_file"

	# Without a file behind `Icon=...` below, the launcher, dock and
	# alt-tab switcher all show a generic placeholder. The AppImage does carry
	# the icon, but digging it out means `--appimage-extract`, which unpacks
	# the whole ~150MB squashfs and executes the binary we just downloaded --
	# far too much machinery for a decorative asset. These are the same PNGs
	# electron-builder packages, taken from the repository at this exact tag.
	#
	# Entirely best effort: an icon that will not download is a cosmetic loss,
	# never a reason to fail an install that has already succeeded.
	icons_dir="$HOME/.local/share/icons/hicolor"
	icon_ok=""
	for size in 512x512 192x192; do
		case "$size" in
		512x512) src_icon="pwa-512x512.png" ;;
		*) src_icon="pwa-192x192.png" ;;
		esac
		if mkdir -p "$icons_dir/$size/apps" 2>/dev/null &&
			curl -fsL -o "$icons_dir/$size/apps/$bin_name.png" \
				"$RAW/$tag/frontend/public/$src_icon" </dev/null; then
			icon_ok=1
		else
			rm -f "$icons_dir/$size/apps/$bin_name.png"
		fi
	done
	if [ -n "$icon_ok" ]; then
		# Some desktops read the cache rather than the directory; others have
		# no such tool. Either way this must not be fatal.
		gtk-update-icon-cache -f -t "$icons_dir" >/dev/null 2>&1 || true
	else
		note "Could not fetch the application icon; the launcher will show a generic one."
	fi

	# A launcher entry, and with it the custom scheme handler -- the Linux
	# equivalent of the `protocols` block, which only
	# takes effect for installs that went through a package.
	apps_dir="$HOME/.local/share/applications"
	mkdir -p "$apps_dir"
	cat >"$apps_dir/$desktop_file" <<EOF
[Desktop Entry]
Type=Application
Name=$app_product
Comment=Task orchestration for coding agents
Exec=$target %u
Icon=$bin_name
Terminal=false
Categories=Development;
MimeType=x-scheme-handler/$scheme;
StartupWMClass=$wm_class
EOF
	update-desktop-database "$apps_dir" >/dev/null 2>&1 || true

	say ""
	say "$app_product $version installed to $target"

	case ":$PATH:" in
	*":$bindir:"*) note "Launch it with: $bin_name" ;;
	*)
		note "$bindir is not on your PATH. Add this to your shell profile:"
		note "  export PATH=\"$bindir:\$PATH\""
		;;
	esac
fi

say ""
say "Update later by running the same command:"
say "  $update_cmd"
