#!/bin/sh
# magpie installer: curl -fsSL https://usemagpie.ai/install.sh | sh
#
# macOS: puts magpie.app in /Applications (~/Applications when that is not
# writable) and links the magpie command into ~/.local/bin.
# Linux: puts the magpie command in ~/.local/bin.
# Every download is checked against the release's SHA-256.
set -eu

site=https://usemagpie.ai
bin="${MAGPIE_BIN_DIR:-$HOME/.local/bin}"

say() { printf '  %s\n' "$*"; }
die() { printf 'magpie: %s\n' "$*" >&2; exit 1; }

case "$(uname -m)" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64) arch=amd64 ;;
  *) die "unsupported CPU: $(uname -m)" ;;
esac
case "$(uname -s)" in
  Darwin) os=darwin; file="magpie-darwin-$arch.zip" ;;
  Linux) os=linux; file="magpie-cli-linux-$arch" ;;
  *) die "unsupported system: $(uname -s); see $site" ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

feed=$(curl -fsSL "$site/api/latest") || die "could not reach $site"
# The feed is one line of JSON; pull this file's url and hash out of it.
entry=$(printf '%s' "$feed" | sed -n "s/.*\"$file\":{\([^}]*\)}.*/\1/p")
url=$(printf '%s' "$entry" | sed -n 's/.*"url":"\([^"]*\)".*/\1/p')
sum=$(printf '%s' "$entry" | sed -n 's/.*"sha256":"\([^"]*\)".*/\1/p')
version=$(printf '%s' "$feed" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')
[ -n "$url" ] && [ -n "$sum" ] || die "the newest release has no $file"

say "downloading magpie $version ($file)"
curl -fsSL -o "$tmp/$file" "$url"
if command -v sha256sum >/dev/null; then got=$(sha256sum "$tmp/$file" | cut -d' ' -f1)
else got=$(shasum -a 256 "$tmp/$file" | cut -d' ' -f1); fi
[ "$got" = "$sum" ] || die "$file does not match its checksum"

mkdir -p "$bin"
if [ "$os" = darwin ]; then
  apps=/Applications
  [ -w "$apps" ] || { apps="$HOME/Applications"; mkdir -p "$apps"; }
  ditto -x -k "$tmp/$file" "$tmp/x"
  rm -rf "$apps/magpie.app"
  mv "$tmp/x/magpie.app" "$apps/magpie.app"
  ln -sf "$apps/magpie.app/Contents/MacOS/magpie" "$bin/magpie"
  say "installed $apps/magpie.app"
else
  install -m 755 "$tmp/$file" "$bin/magpie"
fi
say "installed $bin/magpie"
case ":$PATH:" in
  *":$bin:"*) ;;
  *) say "add $bin to your PATH to run magpie from a terminal" ;;
esac
[ "$os" = darwin ] && say "open it: open -a magpie" || say "run it: magpie"
