#!/bin/sh
# Build the directory tree to upload to https://deeprouter.top.
#
# Usage:
#   sh scripts/mirror-layout.sh --version v0.3.3
#   sh scripts/mirror-layout.sh --version v0.3.3 --out dist-mirror --assets ./release-files
#
# --version is v0.3.3 or 0.3.3. --assets is a directory that already contains
# checksums.txt and the six release binaries. Without --assets, those files
# are downloaded from GitHub (curl or wget required). This script does not
# upload anything and does not create a git tag.
#
# The server must serve the contents of the output directory at the site root.
# Available once that upload is deployed:
#
#   https://deeprouter.top/install.sh
#   https://deeprouter.top/install.ps1
#   https://deeprouter.top/releases/latest.txt          body: v0.3.3 plus a newline
#   https://deeprouter.top/releases/latest/<asset>      copies, not a symlink
#   https://deeprouter.top/releases/v0.3.3/<asset>
#
# <asset> is checksums.txt and:
#   deeprouter-darwin-amd64
#   deeprouter-darwin-arm64
#   deeprouter-linux-amd64
#   deeprouter-linux-arm64
#   deeprouter-windows-amd64.exe
#   deeprouter-windows-arm64.exe
#
# install.sh and install.ps1 in this tree default to the mirror
# (BUILTIN_DOWNLOAD_BASE / BuiltinDownloadBase = https://deeprouter.top).
# The installer fetches ${BASE}/releases/latest/<asset> or
# ${BASE}/releases/v<version>/<asset>. It does not read latest.txt;
# that file is only a pointer for operators.
# GitHub URLs stay https://github.com/<repo>/releases/latest/download/<asset>
# and https://github.com/<repo>/releases/download/v<version>/<asset>.
set -eu

VERSION=""
OUT="dist-mirror"
ASSETS=""
REPO="zfg5118/deeprouter"
MIRROR_ORIGIN="https://deeprouter.top"

usage() {
  cat <<'EOF'
Usage: scripts/mirror-layout.sh --version v0.3.3 [--out dist-mirror] [--assets DIR] [--repo owner/name]

Writes the mirror tree described in the script header. Upload the contents
of the output directory to the site root of https://deeprouter.top.
EOF
}

fail() {
  printf 'mirror-layout.sh: %s\n' "$1" >&2
  exit 1
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version)
      VERSION=${2:-}
      [ -n "$VERSION" ] || fail "--version needs a value"
      shift 2
      ;;
    --version=*)
      VERSION=${1#--version=}
      shift
      ;;
    --out)
      OUT=${2:-}
      [ -n "$OUT" ] || fail "--out needs a directory"
      shift 2
      ;;
    --out=*)
      OUT=${1#--out=}
      shift
      ;;
    --assets)
      ASSETS=${2:-}
      [ -n "$ASSETS" ] || fail "--assets needs a directory"
      shift 2
      ;;
    --assets=*)
      ASSETS=${1#--assets=}
      shift
      ;;
    --repo)
      REPO=${2:-}
      [ -n "$REPO" ] || fail "--repo needs owner/name"
      shift 2
      ;;
    --repo=*)
      REPO=${1#--repo=}
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

[ -n "$VERSION" ] || fail "--version is required (for example v0.3.3)"
case "$VERSION" in
  v*) TAG=$VERSION ;;
  *) TAG=v$VERSION ;;
esac
case "$OUT" in
  ""|/|.) fail "refusing to replace $OUT" ;;
esac

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
NAMES="checksums.txt deeprouter-darwin-amd64 deeprouter-darwin-arm64 deeprouter-linux-amd64 deeprouter-linux-arm64 deeprouter-windows-amd64.exe deeprouter-windows-arm64.exe"

fetch_asset() {
  name=$1
  dest=$2
  url="https://github.com/${REPO}/releases/download/${TAG}/${name}"
  printf 'Downloading %s\n' "$url"
  if command -v curl >/dev/null 2>&1; then
    # Space-separated values. Ubuntu curl 8.5 rejects a timeout glued on with '='.
    curl -fsSL --connect-timeout 5 --max-time 120 -o "$dest" "$url" || fail "download failed: $url"
    return 0
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -O "$dest" -T 120 "$url" || fail "download failed: $url"
    return 0
  fi
  fail "curl or wget is required when --assets is not set"
}

rewrite_installer() {
  src=$1
  dest=$2
  empty=$3
  filled=$4
  awk -v empty="$empty" -v filled="$filled" '
    BEGIN { n = 0 }
    $0 == empty { print filled; n++; next }
    { print }
    END { if (n != 1) exit 1 }
  ' "$src" > "$dest" || fail "could not rewrite $src (the builtin default line was not found once)"
}

rm -rf "$OUT"
mkdir -p "$OUT/releases/latest" "$OUT/releases/$TAG"

for name in $NAMES; do
  if [ -n "$ASSETS" ]; then
    [ -f "$ASSETS/$name" ] || fail "missing $ASSETS/$name"
    cp "$ASSETS/$name" "$OUT/releases/$TAG/$name"
  else
    fetch_asset "$name" "$OUT/releases/$TAG/$name"
  fi
  cp "$OUT/releases/$TAG/$name" "$OUT/releases/latest/$name"
done

printf '%s\n' "$TAG" > "$OUT/releases/latest.txt"

sh_empty='BUILTIN_DOWNLOAD_BASE=""'
sh_filled="BUILTIN_DOWNLOAD_BASE=\"${MIRROR_ORIGIN}\""
ps1_empty=$(printf '%s' "\$script:BuiltinDownloadBase = ''")
ps1_filled=$(printf '%s' "\$script:BuiltinDownloadBase = '${MIRROR_ORIGIN}'")
rewrite_installer "$ROOT/install.sh" "$OUT/install.sh" "$sh_empty" "$sh_filled"
rewrite_installer "$ROOT/install.ps1" "$OUT/install.ps1" "$ps1_empty" "$ps1_filled"
chmod 755 "$OUT/install.sh" "$OUT/install.ps1"

printf 'Wrote %s\n' "$OUT"
printf 'Upload its contents so %s/install.sh is served at %s/install.sh\n' "$OUT" "$MIRROR_ORIGIN"
