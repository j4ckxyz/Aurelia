#!/bin/sh
# Installs Aurelia, or updates it, on macOS and Linux:
#
#   curl -fsSL https://raw.githubusercontent.com/j4ckxyz/Aurelia/main/install.sh | sh
#
# On macOS it puts Aurelia.app in /Applications, or in ~/Applications
# where that cannot be written. A copy fetched this way is not marked as
# downloaded from the internet, so macOS opens it without the warning it
# shows for apps of developers it does not know. On Linux it runs the
# release's own installer, which puts the app in ~/.local, without root.
#
#   sh install.sh ARCHIVE       installs a release's archive you have
#   sh install.sh --uninstall   removes the app; settings and music stay
#
# Aurelia then updates itself (Settings > Updates).
set -eu

repo='j4ckxyz/Aurelia'
releases="https://github.com/$repo/releases/latest/download"

say() { printf '%s\n' "$*"; }
fail() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" > /dev/null 2>&1 || fail "$1 is needed, and is not installed"; }

need curl
need tar
case "$(uname -s)" in
Linux)
	# The release's installer does the rest: it knows the machine's kind.
	script=$(mktemp)
	trap 'rm -f "$script"' EXIT
	curl -fsSL "$releases/install.sh" -o "$script" || fail "could not fetch the Linux installer from $releases"
	sh "$script" "$@"
	exit
	;;
Darwin) ;;
*) fail "this script installs on macOS and Linux; on Windows, see the README" ;;
esac

# macOS. AURELIA_DEST names another folder than Applications.
dest="${AURELIA_DEST:-/Applications}"
[ -n "${AURELIA_DEST:-}" ] || [ -w "$dest" ] || dest="$HOME/Applications"
app="$dest/Aurelia.app"

if [ "${1:-}" = "--uninstall" ]; then
	osascript -e 'quit app "Aurelia"' > /dev/null 2>&1 || true
	for dir in "$dest" /Applications "$HOME/Applications"; do
		[ -d "$dir/Aurelia.app" ] && rm -rf "$dir/Aurelia.app" && say "Removed $dir/Aurelia.app"
	done
	say "Your settings and downloads are in ~/Library/Application Support/Aurelia."
	exit 0
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
archive="${1:-}"
if [ -z "$archive" ]; then
	# The list of the latest version names its archive: one app for both
	# kinds of Mac.
	manifest=$(curl -fsSL "$releases/update-darwin-universal.json") || fail "could not read the latest release of $repo"
	url=$(printf '%s' "$manifest" | sed -n 's/.*"url": *"\([^"]*\)".*/\1/p' | head -n 1)
	version=$(printf '%s' "$manifest" | sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' | head -n 1)
	[ -n "$url" ] || fail "the latest release has no build for macOS"
	say "Downloading Aurelia ${version}..."
	archive="$work/aurelia.tar.gz"
	curl -fL --progress-bar "$url" -o "$archive" || fail "could not download $url"
fi
[ -f "$archive" ] || fail "$archive is not a file"
tar -xzf "$archive" -C "$work" || fail "$archive is not an archive of Aurelia"
[ -d "$work/Aurelia.app" ] || fail "$archive holds no Aurelia.app"

mkdir -p "$dest"
if [ -d "$app" ]; then
	osascript -e 'quit app "Aurelia"' > /dev/null 2>&1 || true
	sleep 1
	rm -rf "$app"
fi
mv "$work/Aurelia.app" "$app"
# Nothing marks it as downloaded; were it marked, this unmarks it.
xattr -dr com.apple.quarantine "$app" 2> /dev/null || true
say "Aurelia is installed in $dest."
if [ -z "${AURELIA_NO_OPEN:-}" ]; then
	open "$app"
fi
