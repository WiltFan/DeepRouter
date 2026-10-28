#!/bin/sh
# Install deeprouter on macOS and Linux.
# From a clone:  sh install.sh
# From the web:  curl -fsSL https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.sh | sh
#
# Windows (cmd, PowerShell, Git Bash, MSYS) uses install.ps1 instead.
# /dev/tty does not exist in a Windows console.
#
# After a successful install, a controlling terminal starts the interactive
# setup from the absolute binary path, with stdin on /dev/tty (curl | sh
# leaves stdin as the script). No terminal, or DEEPROUTER_NO_SETUP=1, skips
# that and prints the next command instead.
set -eu

# ui_en is true only for a clearly English choice, the same rule deeprouter
# uses: DEEPROUTER_LANG (zh or en) wins, then LC_ALL, then LANG (en, en_US,
# en-GB). C, POSIX, empty, and other languages stay Simplified Chinese.
ui_en() {
  choice=$(printf '%s' "${DEEPROUTER_LANG:-}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
  case "$choice" in
    en|en-us|en_us|en-gb|en_gb) return 0 ;;
    zh|zh-cn|zh_cn|zh-hans) return 1 ;;
  esac
  loc=$(printf '%s' "${LC_ALL:-}" | tr -d '[:space:]')
  if [ -z "$loc" ]; then
    loc=${LANG:-}
  fi
  loc=$(printf '%s' "$loc" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
  loc=${loc%%.*}
  loc=${loc%%@*}
  case "$loc" in
    en|en_*|en-*) return 0 ;;
  esac
  return 1
}

UI_EN=0
if ui_en; then
  UI_EN=1
fi

# say prints English when the locale is clearly English, otherwise Chinese.
# Pass the Chinese text as the second argument. A missing translation
# falls back to English.
say() {
  if [ "$UI_EN" -eq 1 ] || [ -z "${2:-}" ]; then
    printf '%s\n' "$1"
  else
    printf '%s\n' "$2"
  fi
}

fail() {
  if [ "$UI_EN" -eq 1 ] || [ -z "${2:-}" ]; then
    printf 'install.sh: %s\n' "$1" >&2
  else
    printf 'install.sh：%s\n' "$2" >&2
  fi
  exit 1
}

uname_s=$(uname -s 2>/dev/null || printf '%s\n' unknown)
case "$uname_s" in
  MINGW*|MSYS*|CYGWIN*|Windows_NT)
    if [ "$UI_EN" -eq 1 ]; then
      cat >&2 <<'EOF'
install.sh: this installer does not run on Windows (cmd, PowerShell, or Git Bash).
Install with PowerShell:

  irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex

From cmd:

  powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex"

If GitHub is slow, use the mirror instead:

  irm https://deeprouter.top/install.ps1 | iex

  powershell -ExecutionPolicy Bypass -c "irm https://deeprouter.top/install.ps1 | iex"
EOF
    else
      cat >&2 <<'EOF'
install.sh：这个安装脚本不能在 Windows 上运行（cmd、PowerShell 或 Git Bash）。
请在 PowerShell 里安装：

  irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex

在 cmd 里：

  powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex"

GitHub 访问慢时，改用镜像：

  irm https://deeprouter.top/install.ps1 | iex

  powershell -ExecutionPolicy Bypass -c "irm https://deeprouter.top/install.ps1 | iex"
EOF
    fi
    exit 1
    ;;
esac

# Empty means the latest GitHub release. Set DEEPROUTER_SETUP_VERSION
# without a leading v (for example 0.1.1) to pin one.
VERSION="${DEEPROUTER_SETUP_VERSION:-}"
REPO="${DEEPROUTER_SETUP_REPO:-zfg5118/deeprouter}"
PREFIX="${PREFIX:-${HOME}/.local/bin}"
FROM_SOURCE=0
NO_PATH=0
INSTALLED=""
PROVIDER="${DEEPROUTER_PROVIDER:-}"

# BUILTIN_DOWNLOAD_BASE is empty in the GitHub copy. scripts/mirror-layout.sh
# rewrites this exact line to https://deeprouter.top in the file it uploads,
# so that copy tries the mirror first. Keep the double quotes.
BUILTIN_DOWNLOAD_BASE=""
KNOWN_MIRROR="https://deeprouter.top"
OFFICIAL_REPO="zfg5118/deeprouter"
PRIMARY_KIND=""
PRIMARY_BASE=""
PRIMARY_LABEL=""
SECONDARY_KIND=""
SECONDARY_BASE=""
SECONDARY_LABEL=""

usage() {
  if [ "$UI_EN" -eq 0 ]; then
    cat <<'EOF'
用法：install.sh [--prefix 目录] [--from-source] [--no-path] [--provider 名称] [--version]

安装 deeprouter 程序。

  --prefix 目录     安装目录（默认：~/.local/bin）
  --from-source     用本机的 Go 从源码构建，不下载
  --no-path         不把 ~/.local/bin 写进 shell 启动文件
  --provider 名称   deeprouter、wolfai 或 linkmesh。作为 DEEPROUTER_PROVIDER
                    传给随后的配置。不加这个参数时，沿用已有的
                    DEEPROUTER_PROVIDER。
  --version         打印安装脚本的版本后退出

脚本在 go.mod 旁边时，从这份源码构建，内嵌的版本号是
"git describe --tags"，失败时是 dev。否则下载发布版本：没有设置
DEEPROUTER_SETUP_VERSION 时，GitHub 用 releases/latest/download，镜像用
releases/latest。DEEPROUTER_SETUP_VERSION 不带前导 v，用来固定版本。
DEEPROUTER_SETUP_REPO=owner/name 换成别的仓库。
DEEPROUTER_DOWNLOAD_BASE 指定先试哪个下载地址：GitHub 地址用 GitHub 的
发布链接，其它地址用 BASE/releases/<版本或 latest>/<文件>，checksums.txt
也一样。GitHub 上的脚本先试 GitHub，再试 https://deeprouter.top（仅限
zfg5118/deeprouter）；镜像上的脚本先试镜像。一个地址下载失败会换另一个。
Fork 不会用 deeprouter.top 镜像，除非设置了 DEEPROUTER_DOWNLOAD_BASE。

/dev/tty 是终端时，安装完成后会运行刚装好的程序（绝对路径，输入来自
/dev/tty）。设置 DEEPROUTER_NO_SETUP=1 跳过这一步，只打印要运行的命令。
CI 等没有控制终端的会话也会跳过。
EOF
    return 0
  fi
  cat <<'EOF'
Usage: install.sh [--prefix DIR] [--from-source] [--no-path] [--provider NAME] [--version]

Installs the deeprouter binary.

  --prefix DIR     Destination directory (default: ~/.local/bin)
  --from-source    Build with the local Go toolchain instead of downloading
  --no-path        Do not add ~/.local/bin to the shell startup file
  --provider NAME  deeprouter, wolfai, or linkmesh. Exported as
                   DEEPROUTER_PROVIDER for the setup that follows.
                   An existing DEEPROUTER_PROVIDER is used when this flag
                   is omitted.
  --version        Print the installer version and exit

When this script sits next to go.mod, it builds from that source.
The embedded version is "git describe --tags", or dev when that fails.
Otherwise it downloads a release binary. With DEEPROUTER_SETUP_VERSION
unset, GitHub is releases/latest/download and a mirror is releases/latest.
Set DEEPROUTER_SETUP_VERSION without a leading v to pin a release.
Override the repository with DEEPROUTER_SETUP_REPO=owner/name.
DEEPROUTER_DOWNLOAD_BASE overrides the first download host. A GitHub host
uses GitHub release URLs. Any other host uses
BASE/releases/<version-or-latest>/<asset>, including checksums.txt.
The GitHub-hosted script tries GitHub first, then https://deeprouter.top
for zfg5118/deeprouter. The mirror-hosted script tries the mirror first.
A failed download falls back to the other host. Forks do not use the
deeprouter.top mirror unless DEEPROUTER_DOWNLOAD_BASE is set.

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
        fail "--prefix needs a directory" "--prefix 需要一个目录"
      fi
      shift 2
      ;;
    --prefix=*)
      PREFIX=${1#--prefix=}
      if [ -z "$PREFIX" ]; then
        fail "--prefix needs a directory" "--prefix 需要一个目录"
      fi
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
    --provider)
      PROVIDER=${2:-}
      if [ -z "$PROVIDER" ]; then
        fail "--provider needs a name" "--provider 需要一个名称"
      fi
      shift 2
      ;;
    --provider=*)
      PROVIDER=${1#--provider=}
      if [ -z "$PROVIDER" ]; then
        fail "--provider needs a name" "--provider 需要一个名称"
      fi
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
      if [ "$UI_EN" -eq 1 ]; then
        printf 'install.sh: unknown argument: %s\n' "$1" >&2
      else
        printf 'install.sh：未知参数：%s\n' "$1" >&2
      fi
      usage >&2
      exit 1
      ;;
  esac
done

# shown_label is a download source label in the UI language.
shown_label() {
  if [ "$1" = mirror ] && [ "$UI_EN" -eq 0 ]; then
    printf '%s\n' '镜像'
  else
    printf '%s\n' "$1"
  fi
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
  if [ -f "./go.mod" ] && grep -q '^module github.com/zfg5118/deeprouter$' ./go.mod 2>/dev/null; then
    CDPATH= cd -- . && pwd
    return 0
  fi
  return 1
}

goos() {
  case "$(uname -s)" in
    Darwin) printf 'darwin\n' ;;
    Linux) printf 'linux\n' ;;
    *) fail "unsupported operating system: $(uname -s). On Windows, run install.ps1." "不支持的操作系统：$(uname -s)。Windows 请运行 install.ps1。" ;;
  esac
}

goarch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64\n' ;;
    arm64|aarch64) printf 'arm64\n' ;;
    *) fail "unsupported architecture: $(uname -m)" "不支持的架构：$(uname -m)" ;;
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

strip_slash() {
  v=$1
  while [ -n "$v" ] && [ "$v" != "${v%/}" ]; do
    v=${v%/}
  done
  printf '%s\n' "$v"
}

is_github_base() {
  case "$1" in
    https://github.com|https://github.com/*|http://github.com|http://github.com/*)
      return 0
      ;;
  esac
  return 1
}

# asset_url KIND BASE NAME
# GitHub:  https://github.com/REPO/releases/latest/download/NAME
#          https://github.com/REPO/releases/download/vVERSION/NAME
# Mirror:  BASE/releases/latest/NAME
#          BASE/releases/vVERSION/NAME
asset_url() {
  kind=$1
  base=$2
  asset=$3
  if [ "$kind" = github ]; then
    if [ -n "$VERSION" ]; then
      printf 'https://github.com/%s/releases/download/v%s/%s\n' "$REPO" "$VERSION" "$asset"
    else
      printf 'https://github.com/%s/releases/latest/download/%s\n' "$REPO" "$asset"
    fi
    return 0
  fi
  base=$(strip_slash "$base")
  if [ -n "$VERSION" ]; then
    printf '%s/releases/v%s/%s\n' "$base" "$VERSION" "$asset"
  else
    printf '%s/releases/latest/%s\n' "$base" "$asset"
  fi
}

# Official repo only: deeprouter.top is not a mirror of forks.
# DEEPROUTER_DOWNLOAD_BASE wins, then the baked-in mirror default, then GitHub.
resolve_download_sources() {
  override=$(strip_slash "${DEEPROUTER_DOWNLOAD_BASE:-}")
  builtin=$(strip_slash "$BUILTIN_DOWNLOAD_BASE")
  mirror=""
  if [ "$REPO" = "$OFFICIAL_REPO" ]; then
    mirror=$KNOWN_MIRROR
  fi
  PRIMARY_KIND=""
  PRIMARY_BASE=""
  PRIMARY_LABEL=""
  SECONDARY_KIND=""
  SECONDARY_BASE=""
  SECONDARY_LABEL=""
  if [ -n "$override" ]; then
    if is_github_base "$override"; then
      PRIMARY_KIND=github
      PRIMARY_BASE=https://github.com
      PRIMARY_LABEL=GitHub
      if [ -n "$mirror" ]; then
        SECONDARY_KIND=mirror
        SECONDARY_BASE=$mirror
        SECONDARY_LABEL=mirror
      fi
    else
      PRIMARY_KIND=mirror
      PRIMARY_BASE=$override
      PRIMARY_LABEL=mirror
      SECONDARY_KIND=github
      SECONDARY_BASE=https://github.com
      SECONDARY_LABEL=GitHub
    fi
    return 0
  fi
  if [ -n "$builtin" ] && [ -n "$mirror" ]; then
    PRIMARY_KIND=mirror
    PRIMARY_BASE=$builtin
    PRIMARY_LABEL=mirror
    SECONDARY_KIND=github
    SECONDARY_BASE=https://github.com
    SECONDARY_LABEL=GitHub
    return 0
  fi
  PRIMARY_KIND=github
  PRIMARY_BASE=https://github.com
  PRIMARY_LABEL=GitHub
  if [ -n "$mirror" ]; then
    SECONDARY_KIND=mirror
    SECONDARY_BASE=$mirror
    SECONDARY_LABEL=mirror
  fi
}

# fetch_url URL DEST MAX_SECONDS
# 0 success, 3 HTTP 404, 1 anything else. 404 is "not published".
# A timeout or a refused connection is not 404.
fetch_url() {
  fu_url=$1
  fu_out=$2
  fu_max=$3
  rm -f "$fu_out"
  if command -v curl >/dev/null 2>&1; then
    fu_code=""
    fu_status=0
    # Space-separated values. Ubuntu curl 8.5 rejects a timeout glued on with '='.
    fu_code=$(curl -fsSL --connect-timeout 5 --max-time "$fu_max" -o "$fu_out" -w '%{http_code}' "$fu_url" 2>/dev/null) || fu_status=$?
    if [ "$fu_status" -eq 0 ]; then
      return 0
    fi
    case "$fu_code" in
      404) return 3 ;;
    esac
    return 1
  fi
  if command -v wget >/dev/null 2>&1; then
    fu_log=$(mktemp)
    fu_status=0
    wget -O "$fu_out" -T "$fu_max" --server-response "$fu_url" >"$fu_log" 2>&1 || fu_status=$?
    if [ "$fu_status" -eq 0 ]; then
      rm -f "$fu_log"
      return 0
    fi
    if grep -E -q 'HTTP/[^ ]+ 404' "$fu_log"; then
      rm -f "$fu_log"
      return 3
    fi
    rm -f "$fu_log"
    return 1
  fi
  fail "curl or wget is required to download a release" "下载发布版本需要 curl 或 wget"
}

# checksum_unusable is true for an HTML body or a file with no sha256 lines.
checksum_unusable() {
  sums=$1
  sample=$(dd if="$sums" bs=2048 count=1 2>/dev/null || true)
  if printf '%s\n' "$sample" | grep -q -i -E '<!DOCTYPE[[:space:]]+html|<html[[:space:]>]|<html>'; then
    return 0
  fi
  parsed=$(awk '
    /^[0-9a-fA-F]{64}[[:space:]]+\*?[^[:space:]]+/ { n++ }
    END { print n + 0 }
  ' "$sums")
  [ "$parsed" -eq 0 ]
}

build_from_source() {
  src=$1
  command -v go >/dev/null 2>&1 || fail "Go is not installed, so the binary cannot be built from source" "没有安装 Go，无法从源码构建"
  mkdir -p "$PREFIX"
  dest="$PREFIX/deeprouter"
  ver=$(source_version "$src")
  say "Building deeprouter from $src" "正在从 $src 构建 deeprouter"
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
  name="deeprouter-${os}-${arch}"
  dest="$PREFIX/deeprouter"
  resolve_download_sources
  tmp=$(mktemp)
  sums=$(mktemp)
  ok=0
  if fetch_release "$PRIMARY_KIND" "$PRIMARY_BASE" "$PRIMARY_LABEL" 45 "$SECONDARY_BASE" "$SECONDARY_LABEL"; then
    ok=1
  elif [ -n "$SECONDARY_KIND" ] && fetch_release "$SECONDARY_KIND" "$SECONDARY_BASE" "$SECONDARY_LABEL" 120 "" ""; then
    ok=1
  fi
  if [ "$ok" -ne 1 ]; then
    rm -f "$tmp" "$sums"
    fail "download failed. Clone the repo and run sh install.sh, or set DEEPROUTER_SETUP_REPO." "下载失败。可以克隆仓库后运行 sh install.sh，或设置 DEEPROUTER_SETUP_REPO。"
  fi
  mkdir -p "$PREFIX"
  mv "$tmp" "$dest"
  rm -f "$sums"
  chmod 755 "$dest"
  INSTALLED=$dest
}

# say_trying_next reports a failed source when fetch_release has a fallback.
say_trying_next() {
  if [ -n "$next_label" ]; then
    say "$label download failed. Trying $next_base ($next_label)." "下载失败（$(shown_label "$label")），正在尝试 ${next_base}（$(shown_label "$next_label")）。"
  fi
}

# fetch_release KIND BASE LABEL MAX_SECONDS NEXT_BASE NEXT_LABEL
# A source succeeds when the binary is saved and either checksums.txt
# matches or checksums.txt is HTTP 404 (not published). HTML, a checksum
# mismatch, or a connection error fails the source.
fetch_release() {
  kind=$1
  base=$2
  label=$3
  max=$4
  next_base=$5
  next_label=$6
  url=$(asset_url "$kind" "$base" "$name")
  sum_url=$(asset_url "$kind" "$base" checksums.txt)
  say "Downloading $name from $url ($label)" "正在从 $url 下载 ${name}（$(shown_label "$label")）"
  status=0
  fetch_url "$url" "$tmp" "$max" || status=$?
  if [ "$status" -ne 0 ]; then
    rm -f "$tmp"
    say_trying_next
    return 1
  fi
  sum_status=0
  fetch_url "$sum_url" "$sums" "$max" || sum_status=$?
  if [ "$sum_status" -eq 3 ]; then
    if [ -n "$VERSION" ]; then
      say "checksums.txt was not published for v${VERSION}; skipped verification" "v${VERSION} 没有发布 checksums.txt，已跳过校验。"
    else
      say "checksums.txt was not published for the latest release; skipped verification" "最新版本没有发布 checksums.txt，已跳过校验。"
    fi
    return 0
  fi
  if [ "$sum_status" -ne 0 ]; then
    rm -f "$tmp"
    say_trying_next
    return 1
  fi
  if ! verify_checksum "$tmp" "$name" "$sums"; then
    rm -f "$tmp"
    say_trying_next
    return 1
  fi
  return 0
}

verify_checksum() {
  file=$1
  asset=$2
  sums=$3
  if checksum_unusable "$sums"; then
    say "checksums.txt looks like HTML or has no checksum lines. The download was blocked or rewritten by a proxy." "checksums.txt 看起来是 HTML，或里面没有校验行。下载被代理拦截或改写了。"
    return 2
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "$file" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    got=$(shasum -a 256 "$file" | awk '{print $1}')
  else
    say "sha256sum is not installed; skipped checksum verification" "没有安装 sha256sum，已跳过校验和核对。"
    return 0
  fi
  want=$(awk -v name="$asset" '
    {
      hash = $1
      file = $2
      sub(/^\*/, "", file)
      if (file == name) { print hash; exit }
    }
  ' "$sums")
  got=$(printf '%s' "$got" | tr 'A-F' 'a-f')
  want=$(printf '%s' "$want" | tr 'A-F' 'a-f')
  if [ -z "$want" ]; then
    say "checksums.txt has no entry for $asset" "checksums.txt 里没有 $asset 的记录"
    return 2
  fi
  if [ "$got" != "$want" ]; then
    say "checksum mismatch for $asset" "$asset 的校验和不匹配"
    return 2
  fi
  say "Checksum verified." "校验和已核对。"
}

src=""
if src=$(discover_source); then
  :
else
  src=""
fi

if [ "$FROM_SOURCE" -eq 1 ]; then
  if [ -z "$src" ]; then
    fail "--from-source needs the repository checkout (go.mod next to install.sh)" "--from-source 需要仓库源码（install.sh 旁边要有 go.mod）"
  fi
  build_from_source "$src"
elif [ -n "$src" ] && command -v go >/dev/null 2>&1; then
  build_from_source "$src"
else
  download_release
fi

# write_compat_shim replaces a previous deeprouter-setup binary in PREFIX
# with a script that runs deeprouter. The command was renamed; the shim keeps
# existing scripts working.
write_compat_shim() {
  shim="$PREFIX/deeprouter-setup"
  if [ -d "$shim" ]; then
    return 0
  fi
  cat > "$shim" <<EOF
#!/bin/sh
exec "$PREFIX/deeprouter" "\$@"
EOF
  chmod 755 "$shim"
}

finish_install() {
  installed=$(absolute_path "$1")
  write_compat_shim
  say "Installed deeprouter to $installed" "已安装 deeprouter 到 $installed"
  say "The previous command name deeprouter-setup now runs deeprouter." "原来的命令名 deeprouter-setup 现在会运行 deeprouter。"
  on_path=0
  case ":$PATH:" in
    *":$PREFIX:"*) on_path=1 ;;
  esac
  if [ "$on_path" -eq 1 ]; then
    say "$PREFIX is on PATH." "$PREFIX 已在 PATH 中。"
  else
    if on_default_prefix; then
      line=$(path_line)
    else
      line="export PATH=\"$PREFIX:\$PATH\""
    fi
    say "$PREFIX is not on PATH. Add it with:" "$PREFIX 不在 PATH 中。请加入："
    say "  $line"
    if [ "$NO_PATH" -eq 0 ] && on_default_prefix; then
      add_path_line "$(shell_rc)" "$line"
    fi
  fi
  if [ -n "$PROVIDER" ]; then
    export DEEPROUTER_PROVIDER="$PROVIDER"
  fi
  if [ "${DEEPROUTER_NO_SETUP:-}" = "1" ] || ! tty_ready; then
    say ""
    say "Next, run:" "接下来请运行："
    if [ -n "${DEEPROUTER_PROVIDER:-}" ]; then
      say "  DEEPROUTER_PROVIDER=$DEEPROUTER_PROVIDER $installed"
    else
      say "  $installed"
    fi
    return 0
  fi
  say ""
  say "Starting $installed" "正在启动 $installed"
  # curl | sh feeds this script on stdin. The wizard has to read the
  # terminal, or it sees EOF and treats the session as non-interactive.
  exec "$installed" </dev/tty
}

# tty_ready is true when this process has a controlling terminal.
# Opening /dev/tty fails without one. That failure is a skip, not an error.
# stderr is redirected before /dev/tty is opened so a missing terminal does
# not print "cannot open /dev/tty".
tty_ready() {
  [ -t 0 ] >/dev/null 2>&1 </dev/tty
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
    say "PATH is already recorded in $rc" "PATH 已经写在 $rc 里"
    return 0
  fi
  mkdir -p "$(dirname "$rc")" 2>/dev/null || {
    say "Could not update $rc. Add this line yourself:" "无法更新 ${rc}。请自己加上这一行："
    say "  $line"
    return 0
  }
  if [ -f "$rc" ] && [ -s "$rc" ]; then
    printf '\n# deeprouter-setup: PATH\n%s\n' "$line" >> "$rc" || {
      say "Could not update $rc. Add this line yourself:" "无法更新 ${rc}。请自己加上这一行："
      say "  $line"
      return 0
    }
  else
    printf '# deeprouter-setup: PATH\n%s\n' "$line" >> "$rc" || {
      say "Could not update $rc. Add this line yourself:" "无法更新 ${rc}。请自己加上这一行："
      say "  $line"
      return 0
    }
  fi
  say "Added $PREFIX to PATH in $rc" "已把 $PREFIX 加入 $rc 的 PATH"
  say "Open a new terminal so the change takes effect." "请打开一个新终端，改动才会生效。"
}

finish_install "$INSTALLED"
