#!/bin/sh
# Install deeprouter-setup.
# From a clone:  sh install.sh
# From the web:  curl -fsSL https://raw.githubusercontent.com/zfg5118/deeprouter-setup/main/install.sh | sh
set -eu

VERSION="${DEEPROUTER_SETUP_VERSION:-0.1.0}"
REPO="${DEEPROUTER_SETUP_REPO:-zfg5118/deeprouter-setup}"
PREFIX="${PREFIX:-${HOME}/.local/bin}"
FROM_SOURCE=0

usage() {
  cat <<'EOF'
Usage: install.sh [--prefix DIR] [--from-source] [--version]

Installs the deeprouter-setup binary.

  --prefix DIR     Destination directory (default: ~/.local/bin)
  --from-source    Build with the local Go toolchain instead of downloading
  --version        Print the installer version and exit

When this script sits next to go.mod, it builds from that source.
Otherwise it downloads a release binary from GitHub.
Override the repository with DEEPROUTER_SETUP_REPO=owner/name.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --prefix)
      PREFIX=${2:-}
      if [ -z "$PREFIX" ]; then
        echo "install.sh: --prefix needs a directory" >&2
        exit 1
      fi
      shift 2
      ;;
    --prefix=*)
      PREFIX=${1#--prefix=}
      shift
      ;;
    --from-source)
      FROM_SOURCE=1
      shift
      ;;
    --version)
      printf '%s\n' "$VERSION"
      exit 0
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "install.sh: unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

say() {
  printf '%s\n' "$1"
}

fail() {
  printf 'install.sh: %s\n' "$1" >&2
  exit 1
}

discover_source() {
  if [ -n "${DEEPROUTER_SETUP_SOURCE:-}" ] && [ -f "${DEEPROUTER_SETUP_SOURCE}/go.mod" ]; then
    CDPATH= cd -- "$DEEPROUTER_SETUP_SOURCE" && pwd
    return 0
  fi
  case "$0" in
    *install.sh)
      dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
      if [ -f "$dir/go.mod" ]; then
        printf '%s\n' "$dir"
        return 0
      fi
      ;;
  esac
  if [ -f "./go.mod" ] && grep -q "deeprouter-setup" ./go.mod 2>/dev/null; then
    CDPATH= cd -- . && pwd
    return 0
  fi
  return 1
}

goos() {
  case "$(uname -s)" in
    Darwin) printf 'darwin\n' ;;
    Linux) printf 'linux\n' ;;
    MINGW*|MSYS*|CYGWIN*) printf 'windows\n' ;;
    *) fail "unsupported operating system: $(uname -s)" ;;
  esac
}

goarch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64\n' ;;
    arm64|aarch64) printf 'arm64\n' ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac
}

build_from_source() {
  src=$1
  command -v go >/dev/null 2>&1 || fail "Go is not installed, so the binary cannot be built from source"
  mkdir -p "$PREFIX"
  dest="$PREFIX/deeprouter-setup"
  say "Building deeprouter-setup from $src"
  (
    cd "$src"
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "$dest" .
  )
  chmod 755 "$dest"
}

download_release() {
  os=$(goos)
  arch=$(goarch)
  name="deeprouter-setup-${os}-${arch}"
  dest="$PREFIX/deeprouter-setup"
  if [ "$os" = "windows" ]; then
    name="${name}.exe"
    dest="${dest}.exe"
  fi
  url="https://github.com/${REPO}/releases/download/v${VERSION}/${name}"
  sum_url="https://github.com/${REPO}/releases/download/v${VERSION}/checksums.txt"
  tmp=$(mktemp)
  say "Downloading $url"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$tmp" || fail "download failed. Clone the repo and run sh install.sh, or set DEEPROUTER_SETUP_REPO."
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$tmp" "$url" || fail "download failed. Clone the repo and run sh install.sh, or set DEEPROUTER_SETUP_REPO."
  else
    fail "curl or wget is required to download a release"
  fi
  if command -v curl >/dev/null 2>&1; then
    sums=$(mktemp)
    if curl -fsSL "$sum_url" -o "$sums"; then
      verify_checksum "$tmp" "$name" "$sums"
    else
      say "checksums.txt was not published for v${VERSION}; skipped verification"
    fi
    rm -f "$sums"
  fi
  mkdir -p "$PREFIX"
  mv "$tmp" "$dest"
  chmod 755 "$dest"
}

verify_checksum() {
  file=$1
  name=$2
  sums=$3
  if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "$file" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    got=$(shasum -a 256 "$file" | awk '{print $1}')
  else
    say "sha256sum is not installed; skipped checksum verification"
    return 0
  fi
  want=$(awk -v name="$name" '$2 == name { print $1 }' "$sums")
  if [ -z "$want" ]; then
    fail "checksums.txt has no entry for $name"
  fi
  if [ "$got" != "$want" ]; then
    fail "checksum mismatch for $name"
  fi
  say "Checksum verified."
}

src=""
if src=$(discover_source); then
  :
else
  src=""
fi

if [ "$FROM_SOURCE" -eq 1 ]; then
  if [ -z "$src" ]; then
    fail "--from-source needs the repository checkout (go.mod next to install.sh)"
  fi
  build_from_source "$src"
elif [ -n "$src" ] && command -v go >/dev/null 2>&1; then
  build_from_source "$src"
else
  download_release
fi

say "Installed deeprouter-setup to $PREFIX/deeprouter-setup"
case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *)
    say "Add it to PATH if this is a new directory:"
    say "  export PATH=\"$PREFIX:\$PATH\""
    ;;
esac
