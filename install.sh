#!/bin/sh
# Install deeprouter-setup on macOS and Linux.
# From a clone:  sh install.sh
# From the web:  curl -fsSL https://raw.githubusercontent.com/zfg5118/deeprouter-setup/main/install.sh | sh
#
# Windows (cmd, PowerShell, Git Bash, MSYS) uses install.ps1 instead.
# /dev/tty does not exist in a Windows console.
#
# After a successful install, a controlling terminal starts the interactive
# setup from the absolute binary path, with stdin on /dev/tty (curl | sh
# leaves stdin as the script). No terminal, or DEEPROUTER_NO_SETUP=1, skips
# that and prints the next command instead.
set -eu

uname_s=$(uname -s 2>/dev/null || printf '%s\n' unknown)
case "$uname_s" in
  MINGW*|MSYS*|CYGWIN*|Windows_NT)
    cat >&2 <<'EOF'
install.sh: this installer does not run on Windows (cmd, PowerShell, or Git Bash).
Install with PowerShell:

  irm https://raw.githubusercontent.com/zfg5118/deeprouter-setup/main/install.ps1 | iex

From cmd:

  powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/zfg5118/deeprouter-setup/main/install.ps1 | iex"
EOF
    exit 1
    ;;
esac

# Empty means the latest GitHub release. Set DEEPROUTER_SETUP_VERSION
# without a leading v (for example 0.1.1) to pin one.
VERSION="${DEEPROUTER_SETUP_VERSION:-}"
REPO="${DEEPROUTER_SETUP_REPO:-zfg5118/deeprouter-setup}"
PREFIX="${PREFIX:-${HOME}/.local/bin}"
FROM_SOURCE=0
NO_PATH=0
INSTALLED=""

usage() {
  cat <<'EOF'
Usage: install.sh [--prefix DIR] [--from-source] [--version]

Installs the deeprouter-setup binary.

  --prefix DIR     Destination directory (default: ~/.local/bin)
  --from-source    Build with the local Go toolchain instead of downloading
  --no-path        Do not add ~/.local/bin to the shell startup file
  --version        Print the installer version and exit

When this script sits next to go.mod, it builds from that source.
The embedded version is "git describe --tags", or dev when that fails.
Otherwise it downloads a release binary from GitHub. With
DEEPROUTER_SETUP_VERSION unset, that is releases/latest/download.
Set DEEPROUTER_SETUP_VERSION without a leading v to pin a release.
Override the repository with DEEPROUTER_SETUP_REPO=owner/name.

When /dev/tty is a terminal, the installer then runs the binary it just
wrote (absolute path, stdin from /dev/tty). Set DEEPROUTER_NO_SETUP=1 to
skip that and print the command instead. CI and other sessions with no
controlling terminal skip it too.
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
    --no-path)
      NO_PATH=1
      shift
      ;;
    --version)
      if [ -n "$VERSION" ]; then
        printf '%s\n' "$VERSION"
      else
        printf '%s\n' latest
      fi
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
    *) fail "unsupported operating system: $(uname -s). On Windows, run install.ps1." ;;
  esac
}

goarch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64\n' ;;
    arm64|aarch64) printf 'arm64\n' ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac
}

# source_version is embedded in a from-source binary.
# DEEPROUTER_SETUP_VERSION wins. Otherwise git describe --tags, then dev.
source_version() {
  src=$1
  if [ -n "$VERSION" ]; then
    printf '%s\n' "$VERSION"
    return 0
  fi
  if command -v git >/dev/null 2>&1; then
    desc=$(git -C "$src" describe --tags 2>/dev/null || true)
    if [ -n "$desc" ]; then
      printf '%s\n' "$desc"
      return 0
    fi
  fi
  printf '%s\n' dev
}

release_asset_url() {
  name=$1
  if [ -n "$VERSION" ]; then
    printf 'https://github.com/%s/releases/download/v%s/%s\n' "$REPO" "$VERSION" "$name"
  else
    printf 'https://github.com/%s/releases/latest/download/%s\n' "$REPO" "$name"
  fi
}

build_from_source() {
  src=$1
  command -v go >/dev/null 2>&1 || fail "Go is not installed, so the binary cannot be built from source"
  mkdir -p "$PREFIX"
  dest="$PREFIX/deeprouter-setup"
  ver=$(source_version "$src")
  say "Building deeprouter-setup from $src"
  (
    cd "$src"
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${ver}" -o "$dest" .
  )
  chmod 755 "$dest"
  INSTALLED=$dest
}

download_release() {
  os=$(goos)
  arch=$(goarch)
  name="deeprouter-setup-${os}-${arch}"
  dest="$PREFIX/deeprouter-setup"
  url=$(release_asset_url "$name")
  sum_url=$(release_asset_url checksums.txt)
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
      if [ -n "$VERSION" ]; then
        say "checksums.txt was not published for v${VERSION}; skipped verification"
      else
        say "checksums.txt was not published for the latest release; skipped verification"
      fi
    fi
    rm -f "$sums"
  fi
  mkdir -p "$PREFIX"
  mv "$tmp" "$dest"
  chmod 755 "$dest"
  INSTALLED=$dest
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

finish_install() {
  installed=$(absolute_path "$1")
  say "Installed deeprouter-setup to $installed"
  on_path=0
  case ":$PATH:" in
    *":$PREFIX:"*) on_path=1 ;;
  esac
  if [ "$on_path" -eq 1 ]; then
    say "$PREFIX is on PATH."
  else
    if on_default_prefix; then
      line=$(path_line)
    else
      line="export PATH=\"$PREFIX:\$PATH\""
    fi
    say "$PREFIX is not on PATH. Add it with:"
    say "  $line"
    if [ "$NO_PATH" -eq 0 ] && on_default_prefix; then
      add_path_line "$(shell_rc)" "$line"
    fi
  fi
  if [ "${DEEPROUTER_NO_SETUP:-}" = "1" ] || ! tty_ready; then
    say ""
    say "Next, run:"
    say "  $installed"
    return 0
  fi
  say ""
  say "Starting $installed"
  # curl | sh feeds this script on stdin. The wizard has to read the
  # terminal, or it sees EOF and treats the session as non-interactive.
  exec "$installed" </dev/tty
}

# tty_ready is true when this process has a controlling terminal.
# Opening /dev/tty fails without one. That failure is a skip, not an error.
tty_ready() {
  [ -t 0 ] </dev/tty >/dev/null 2>&1
}

absolute_path() {
  target=$1
  dir=$(dirname -- "$target")
  base=$(basename -- "$target")
  abs=$(CDPATH= cd -- "$dir" && pwd) || {
    printf '%s\n' "$target"
    return 0
  }
  printf '%s\n' "$abs/$base"
}

on_default_prefix() {
  def="${HOME}/.local/bin"
  if [ ! -d "$PREFIX" ] || [ ! -d "$def" ]; then
    return 1
  fi
  a=$(CDPATH= cd -- "$PREFIX" && pwd) || return 1
  b=$(CDPATH= cd -- "$def" && pwd) || return 1
  [ "$a" = "$b" ]
}

shell_name() {
  base=${SHELL:-sh}
  base=${base##*/}
  printf '%s\n' "$base"
}

shell_rc() {
  case "$(shell_name)" in
    zsh) printf '%s\n' "${HOME}/.zshrc" ;;
    fish) printf '%s\n' "${HOME}/.config/fish/config.fish" ;;
    bash) printf '%s\n' "${HOME}/.bashrc" ;;
    *) printf '%s\n' "${HOME}/.profile" ;;
  esac
}

path_line() {
  case "$(shell_name)" in
    fish) printf '%s\n' 'fish_add_path "$HOME/.local/bin"' ;;
    *) printf '%s\n' 'export PATH="$HOME/.local/bin:$PATH"' ;;
  esac
}

add_path_line() {
  rc=$1
  line=$2
  if [ -f "$rc" ] && grep -F -q '# deeprouter-setup: PATH' "$rc"; then
    say "PATH is already recorded in $rc"
    return 0
  fi
  mkdir -p "$(dirname "$rc")" 2>/dev/null || {
    say "Could not update $rc. Add this line yourself:"
    say "  $line"
    return 0
  }
  if [ -f "$rc" ] && [ -s "$rc" ]; then
    printf '\n# deeprouter-setup: PATH\n%s\n' "$line" >> "$rc" || {
      say "Could not update $rc. Add this line yourself:"
      say "  $line"
      return 0
    }
  else
    printf '# deeprouter-setup: PATH\n%s\n' "$line" >> "$rc" || {
      say "Could not update $rc. Add this line yourself:"
      say "  $line"
      return 0
    }
  fi
  say "Added $PREFIX to PATH in $rc"
  say "Open a new terminal so the change takes effect."
}

finish_install "$INSTALLED"
