# Install deeprouter on Windows.
#
# PowerShell:
#   irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex
#
# cmd:
#   powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex"
#
# The binary is installed to %LOCALAPPDATA%\Programs\deeprouter\deeprouter.exe
# (override with DEEPROUTER_SETUP_PREFIX) and that directory is added to the
# user PATH. A deeprouter-setup.cmd shim in that directory runs deeprouter, and
# a previous %LOCALAPPDATA%\Programs\deeprouter-setup install is removed from
# PATH. Setup then runs in this console unless DEEPROUTER_NO_SETUP=1.
# This script does not use /dev/tty.
#
# When executed as a file, optional arguments are:
#   -Prefix DIR | --prefix DIR
#   -NoPath | --no-path
#   -FromSource | --from-source
#   -Provider NAME | --provider NAME
#   -Version | --version
# DEEPROUTER_PROVIDER selects deeprouter, wolfai, or linkmesh when -Provider
# is omitted. The value is passed to the setup that follows.
# iex (the one-liner) ignores arguments and reads the environment instead.
# DEEPROUTER_NO_PATH=1 skips the user PATH update.
# DEEPROUTER_SETUP_FROM_SOURCE=1 builds from a checkout.
# DEEPROUTER_SETUP_VERSION without a leading v pins a release.
# DEEPROUTER_SETUP_REPO=owner/name selects a fork.
# DEEPROUTER_DOWNLOAD_BASE overrides the first download host. A GitHub host
# uses GitHub release URLs. Any other host uses
# BASE/releases/<version-or-latest>/<asset>, including checksums.txt.
# The GitHub-hosted script tries GitHub first, then https://deeprouter.top
# for zfg5118/deeprouter. The mirror-hosted script tries the mirror first.
#
# Messages are Simplified Chinese unless DEEPROUTER_LANG, LC_ALL, or LANG
# selects English, the same rule deeprouter uses.
# Keep this file ASCII. Windows PowerShell 5.1 reads a script file without a
# BOM in the ANSI code page, and irm | iex keeps a BOM as a stray character
# that fails as a command. Chinese text is therefore written as \uXXXX
# escapes, with backslashes doubled, and decoded by [regex]::Unescape.
# To escape a line:  -join ([char[]]$text | ForEach-Object { '\u{0:x4}' -f [int]$_ })

# mirror-layout.sh rewrites the next line to 'https://deeprouter.top' in the
# copy uploaded to the mirror. Empty means this GitHub-hosted script tries
# GitHub first. Keep the quotes.
$script:BuiltinDownloadBase = ''
$script:KnownMirror = 'https://deeprouter.top'
$script:OfficialRepo = 'zfg5118/deeprouter'
$script:ChecksumNotPublished = $false

$script:InstallerRoot = $PSScriptRoot
$script:InstallerCommandPath = $PSCommandPath
$script:InstallerArgs = @($args)
# Under iex, $PSCommandPath is empty while $MyInvocation is the caller's, so a
# dot-sourced caller does not make this text count as dot-sourced.
$script:InstallerDotSourced = ([bool]$PSCommandPath -and $MyInvocation.InvocationName -eq '.')

# GitHub needs TLS 1.2, which older Windows PowerShell does not offer by default.
# The protocols that are already enabled stay enabled.
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
} catch {
}

function Test-EnglishUI {
    $choice = ([string]$env:DEEPROUTER_LANG).Trim().ToLower()
    if (@('en', 'en-us', 'en_us', 'en-gb', 'en_gb') -contains $choice) { return $true }
    if (@('zh', 'zh-cn', 'zh_cn', 'zh-hans') -contains $choice) { return $false }
    $lang = ([string]$env:LC_ALL).Trim()
    if (-not $lang) { $lang = ([string]$env:LANG).Trim() }
    return $lang -match '^(?i)en([._@-]|$)'
}

# Get-InstallText returns English as given, or the escaped Chinese template
# formatted with the remaining arguments.
function Get-InstallText([string]$English, [string]$Chinese) {
    if ((Test-EnglishUI) -or -not $Chinese) { return $English }
    return ([regex]::Unescape($Chinese) -f $args)
}

function Say-Install([string]$English, [string]$Chinese) {
    Write-Host (Get-InstallText $English $Chinese @args)
}

function Get-SourceLabel([string]$Label) {
    if ($Label -eq 'mirror') { return (Get-InstallText 'mirror' '\u955c\u50cf') }
    return $Label
}

# Format-RunCommand prints a command that runs Path in PowerShell.
function Format-RunCommand([string]$Path) {
    $quoted = "'" + $Path.Replace("'", "''") + "'"
    if ($env:DEEPROUTER_PROVIDER) {
        return "`$env:DEEPROUTER_PROVIDER='" + ([string]$env:DEEPROUTER_PROVIDER).Replace("'", "''") + "'; & $quoted"
    }
    if ($Path -match "[\s'&;()]") { return "& $quoted" }
    return $Path
}

function Install-DeepRouter {
    $previousErrorAction = $ErrorActionPreference
    $previousProgress = $ProgressPreference
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    try {
        $opts = Get-InstallOptions
        if ($opts.ShowVersion) {
            if ($env:DEEPROUTER_SETUP_VERSION) {
                Write-Output $env:DEEPROUTER_SETUP_VERSION
            } else {
                Write-Output 'latest'
            }
            return
        }
        if ($opts.ShowHelp) {
            Write-InstallHelp
            return
        }

        $prefix = $opts.Prefix
        if (-not $prefix) {
            if (-not $env:LOCALAPPDATA) {
                throw (Get-InstallText 'LOCALAPPDATA is not set' '\u6ca1\u6709\u8bbe\u7f6e LOCALAPPDATA')
            }
            $prefix = Join-Path $env:LOCALAPPDATA 'Programs\deeprouter'
        }
        New-Item -ItemType Directory -Force -Path $prefix | Out-Null
        $dest = Join-Path $prefix 'deeprouter.exe'

        $source = Find-InstallSource
        if ($opts.FromSource -and -not $source) {
            throw (Get-InstallText '--from-source needs the repository checkout (go.mod next to install.ps1)' '--from-source \u9700\u8981\u4ed3\u5e93\u6e90\u7801\uff08install.ps1 \u65c1\u8fb9\u8981\u6709 go.mod\uff09')
        }
        if ($source -and (Get-Command go -ErrorAction SilentlyContinue)) {
            Build-FromSource -Source $source -Dest $dest
        } elseif ($opts.FromSource) {
            throw (Get-InstallText 'Go is not installed, so the binary cannot be built from source' '\u6ca1\u6709\u5b89\u88c5 Go\uff0c\u65e0\u6cd5\u4ece\u6e90\u7801\u6784\u5efa')
        } else {
            if (-not (Test-WindowsOS)) {
                throw (Get-InstallText 'install.ps1 downloads the Windows binary. On macOS and Linux, run sh install.sh.' 'install.ps1 \u4e0b\u8f7d\u7684\u662f Windows \u7248\u672c\u3002macOS \u548c Linux \u8bf7\u8fd0\u884c sh install.sh\u3002')
            }
            Download-Release -Dest $dest
        }

        Install-CompatShim -Dir $prefix -Target $dest
        Remove-OldInstall -NewExe $dest
        Say-Install "Installed deeprouter to $dest" '\u5df2\u5b89\u88c5 deeprouter \u5230 {0}' $dest
        Say-Install 'The previous command name deeprouter-setup now runs deeprouter.' '\u539f\u6765\u7684\u547d\u4ee4\u540d deeprouter-setup \u73b0\u5728\u4f1a\u8fd0\u884c deeprouter\u3002'
        if (-not $opts.NoPath) {
            Add-UserPathEntry -Dir $prefix
        }

        if ($opts.Provider) {
            $env:DEEPROUTER_PROVIDER = [string]$opts.Provider
        }
        if ($env:DEEPROUTER_NO_SETUP -eq '1' -or -not (Test-ConsoleInput)) {
            Write-Host ''
            Say-Install 'Next, run:' '\u63a5\u4e0b\u6765\u8bf7\u8fd0\u884c\uff1a'
            Write-Host ('  ' + (Format-RunCommand $dest))
            return
        }
        Write-Host ''
        Say-Install "Starting $dest" '\u6b63\u5728\u542f\u52a8 {0}' $dest
        & $dest
        $script:InstallExitCode = Get-SetupExitCode -Code $LASTEXITCODE -Dest $dest
    } finally {
        $ErrorActionPreference = $previousErrorAction
        $ProgressPreference = $previousProgress
    }
}

# Get-SetupExitCode reports a setup that did not finish. deeprouter has already
# explained a cancel (2) or Ctrl+C (130), so those add nothing here.
function Get-SetupExitCode($Code, [string]$Dest) {
    if ($null -eq $Code -or $Code -eq 0) { return 0 }
    if ($Code -ne 2 -and $Code -ne 130) {
        Say-Install "deeprouter did not finish setup (exit code $Code). Run it again:" 'deeprouter \u6ca1\u6709\u5b8c\u6210\u914d\u7f6e\uff08\u9000\u51fa\u7801 {0}\uff09\u3002\u53ef\u4ee5\u91cd\u65b0\u8fd0\u884c\uff1a' $Code
        Write-Host ('  ' + (Format-RunCommand $Dest))
    }
    return [int]$Code
}

function Get-InstallOptions {
    $opts = @{
        Prefix      = $env:DEEPROUTER_SETUP_PREFIX
        NoPath      = ($env:DEEPROUTER_NO_PATH -eq '1')
        FromSource  = ($env:DEEPROUTER_SETUP_FROM_SOURCE -eq '1')
        Provider    = $env:DEEPROUTER_PROVIDER
        ShowVersion = $false
        ShowHelp    = $false
    }
    if (-not $script:InstallerCommandPath) {
        return $opts
    }
    $needsPrefix = Get-InstallText '--prefix needs a directory' '--prefix \u9700\u8981\u4e00\u4e2a\u76ee\u5f55'
    $needsProvider = Get-InstallText '--provider needs a name' '--provider \u9700\u8981\u4e00\u4e2a\u540d\u79f0'
    $argv = @($script:InstallerArgs)
    for ($i = 0; $i -lt $argv.Count; $i++) {
        $arg = [string]$argv[$i]
        if ($arg -eq '-Prefix' -or $arg -eq '--prefix') {
            $i++
            if ($i -ge $argv.Count -or -not $argv[$i]) { throw $needsPrefix }
            $opts.Prefix = [string]$argv[$i]
            continue
        }
        if ($arg -like '--prefix=*') {
            $opts.Prefix = $arg.Substring(9)
            if (-not $opts.Prefix) { throw $needsPrefix }
            continue
        }
        if ($arg -eq '-NoPath' -or $arg -eq '--no-path') { $opts.NoPath = $true; continue }
        if ($arg -eq '-FromSource' -or $arg -eq '--from-source') { $opts.FromSource = $true; continue }
        if ($arg -eq '-Provider' -or $arg -eq '--provider') {
            $i++
            if ($i -ge $argv.Count -or -not $argv[$i]) { throw $needsProvider }
            $opts.Provider = [string]$argv[$i]
            continue
        }
        if ($arg -like '--provider=*') {
            $opts.Provider = $arg.Substring(11)
            if (-not $opts.Provider) { throw $needsProvider }
            continue
        }
        if ($arg -eq '-Version' -or $arg -eq '--version') { $opts.ShowVersion = $true; continue }
        if ($arg -eq '-h' -or $arg -eq '-Help' -or $arg -eq '--help') { $opts.ShowHelp = $true; continue }
        throw (Get-InstallText "unknown argument: $arg" '\u672a\u77e5\u53c2\u6570\uff1a{0}' $arg)
    }
    return $opts
}

function Write-InstallHelp {
    if (-not (Test-EnglishUI)) {
        Write-Output ([regex]::Unescape(@'
\u7528\u6cd5\uff1ainstall.ps1 [-Prefix \u76ee\u5f55] [-FromSource] [-NoPath] [-Provider \u540d\u79f0] [-Version]

\u5728 Windows \u4e0a\u5b89\u88c5 deeprouter \u7a0b\u5e8f\u3002

  -Prefix \u76ee\u5f55     \u5b89\u88c5\u76ee\u5f55
                   \uff08\u9ed8\u8ba4\uff1a%LOCALAPPDATA%\\Programs\\deeprouter\uff09
  -FromSource      \u7528\u672c\u673a\u7684 Go \u4ece\u6e90\u7801\u6784\u5efa\uff0c\u4e0d\u4e0b\u8f7d
  -NoPath          \u4e0d\u628a\u5b89\u88c5\u76ee\u5f55\u52a0\u5165\u7528\u6237 PATH
  -Provider \u540d\u79f0   deeprouter\u3001wolfai \u6216 linkmesh\u3002\u4f5c\u4e3a DEEPROUTER_PROVIDER
                   \u4f20\u7ed9\u968f\u540e\u7684\u914d\u7f6e\u3002\u4e0d\u52a0\u8fd9\u4e2a\u53c2\u6570\u65f6\uff0c\u6cbf\u7528\u8fd9\u4e2a\u73af\u5883\u53d8\u91cf\u3002
  -Version         \u6253\u5370\u5b89\u88c5\u811a\u672c\u7684\u7248\u672c\u540e\u9000\u51fa

PowerShell\uff1a
  irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex

cmd\uff1a
  powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex"

GitHub \u8bbf\u95ee\u6162\u65f6\uff0c\u6539\u7528\u955c\u50cf\uff1a
  irm https://deeprouter.top/install.ps1 | iex

DEEPROUTER_NO_SETUP=1 \u8df3\u8fc7\u81ea\u52a8\u914d\u7f6e\u3002\u4ea4\u4e92\u63d0\u793a\u9700\u8981\u63a7\u5236\u53f0\uff1bWindows \u4e0a\u6ca1\u6709
/dev/tty\u3002DEEPROUTER_DOWNLOAD_BASE \u6307\u5b9a\u5148\u8bd5\u54ea\u4e2a\u4e0b\u8f7d\u5730\u5740\u3002GitHub \u4e0a\u7684\u8fd9\u4efd
\u811a\u672c\u5148\u8bd5 GitHub\uff0c\u5b98\u65b9\u4ed3\u5e93\u518d\u8bd5 https://deeprouter.top\u3002
'@))
        return
    }
    Write-Output @'
Usage: install.ps1 [-Prefix DIR] [-FromSource] [-NoPath] [-Provider NAME] [-Version]

Installs the deeprouter binary on Windows.

  -Prefix DIR      Destination directory
                   (default: %LOCALAPPDATA%\Programs\deeprouter)
  -FromSource      Build with the local Go toolchain instead of downloading
  -NoPath          Do not add the destination directory to the user PATH
  -Provider NAME   deeprouter, wolfai, or linkmesh. Passed to setup as
                   DEEPROUTER_PROVIDER. The environment variable is used
                   when this flag is omitted.
  -Version         Print the installer version and exit

PowerShell:
  irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex

cmd:
  powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex"

If GitHub is slow, use the mirror instead:
  irm https://deeprouter.top/install.ps1 | iex

DEEPROUTER_NO_SETUP=1 skips the automatic setup. A console is required for
the interactive prompts; there is no /dev/tty on Windows.
DEEPROUTER_DOWNLOAD_BASE overrides the first download host. The GitHub copy
tries GitHub, then https://deeprouter.top for the official repository.
'@
}

function Test-WindowsOS {
    if ($env:OS -eq 'Windows_NT') { return $true }
    if (Test-Path -Path 'variable:IsWindows') { return [bool]$IsWindows }
    return $false
}

function Find-InstallSource {
    if ($env:DEEPROUTER_SETUP_SOURCE -and (Test-Path (Join-Path $env:DEEPROUTER_SETUP_SOURCE 'go.mod'))) {
        return (Resolve-Path $env:DEEPROUTER_SETUP_SOURCE).Path
    }
    if ($script:InstallerRoot -and (Test-Path (Join-Path $script:InstallerRoot 'go.mod'))) {
        return $script:InstallerRoot
    }
    return $null
}

function Get-SourceVersion([string]$Source) {
    if ($env:DEEPROUTER_SETUP_VERSION) { return $env:DEEPROUTER_SETUP_VERSION }
    if (Get-Command git -ErrorAction SilentlyContinue) {
        $desc = & git -C $Source describe --tags 2>$null
        if ($LASTEXITCODE -eq 0 -and $desc) { return $desc.Trim() }
    }
    return 'dev'
}

function Build-FromSource([string]$Source, [string]$Dest) {
    $ver = Get-SourceVersion -Source $Source
    Say-Install "Building deeprouter from $Source" '\u6b63\u5728\u4ece {0} \u6784\u5efa deeprouter' $Source
    $previous = $env:CGO_ENABLED
    $hadPrevious = Test-Path Env:CGO_ENABLED
    Push-Location $Source
    try {
        $env:CGO_ENABLED = '0'
        & go build -trimpath -ldflags "-s -w -X main.version=$ver" -o $Dest .
        if ($LASTEXITCODE -ne 0) { throw (Get-InstallText 'go build failed' 'go build \u5931\u8d25') }
    } finally {
        Pop-Location
        if ($hadPrevious) { $env:CGO_ENABLED = $previous } else { Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue }
    }
}

function Get-GoArch {
    $arch = $env:PROCESSOR_ARCHITEW6432
    if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
    switch -Regex ($arch) {
        '^(AMD64|x64)$' { return 'amd64' }
        '^ARM64$' { return 'arm64' }
        default { throw (Get-InstallText "unsupported architecture: $arch" '\u4e0d\u652f\u6301\u7684\u67b6\u6784\uff1a{0}' $arch) }
    }
}

function Strip-TrailingSlash([string]$Value) {
    if (-not $Value) { return '' }
    return $Value.TrimEnd('/')
}

function Test-GitHubBase([string]$Base) {
    if (-not $Base) { return $false }
    return $Base -match '(?i)^https?://github\.com(/|$)'
}

function Get-AssetUrl([string]$Kind, [string]$Base, [string]$Repo, [string]$Name) {
    $version = $env:DEEPROUTER_SETUP_VERSION
    if ($Kind -eq 'github') {
        if ($version) {
            return "https://github.com/$Repo/releases/download/v$version/$Name"
        }
        return "https://github.com/$Repo/releases/latest/download/$Name"
    }
    $root = Strip-TrailingSlash $Base
    if ($version) {
        return "$root/releases/v$version/$Name"
    }
    return "$root/releases/latest/$Name"
}

# Get-DownloadSources returns GitHub first unless a mirror base is selected.
# deeprouter.top is only used for the official repository, unless
# DEEPROUTER_DOWNLOAD_BASE names a host explicitly. The first host gets 45
# seconds and the fallback 120, the same limits as install.sh.
function Get-DownloadSources([string]$Repo) {
    $override = Strip-TrailingSlash $env:DEEPROUTER_DOWNLOAD_BASE
    $builtin = Strip-TrailingSlash $script:BuiltinDownloadBase
    $mirror = ''
    if ($Repo -eq $script:OfficialRepo) { $mirror = $script:KnownMirror }
    $sources = @()
    if ($override) {
        if (Test-GitHubBase $override) {
            $sources += @{ Kind = 'github'; Base = 'https://github.com'; Label = 'GitHub'; Timeout = 45 }
            if ($mirror) { $sources += @{ Kind = 'mirror'; Base = $mirror; Label = 'mirror'; Timeout = 120 } }
        } else {
            $sources += @{ Kind = 'mirror'; Base = $override; Label = 'mirror'; Timeout = 45 }
            $sources += @{ Kind = 'github'; Base = 'https://github.com'; Label = 'GitHub'; Timeout = 120 }
        }
        return $sources
    }
    if ($builtin -and $mirror) {
        $sources += @{ Kind = 'mirror'; Base = $builtin; Label = 'mirror'; Timeout = 45 }
        $sources += @{ Kind = 'github'; Base = 'https://github.com'; Label = 'GitHub'; Timeout = 120 }
        return $sources
    }
    $sources += @{ Kind = 'github'; Base = 'https://github.com'; Label = 'GitHub'; Timeout = 45 }
    if ($mirror) { $sources += @{ Kind = 'mirror'; Base = $mirror; Label = 'mirror'; Timeout = 120 } }
    return $sources
}

function Say-TryingNext($Source, $Next) {
    $label = Get-SourceLabel $Source.Label
    $nextLabel = Get-SourceLabel $Next.Label
    Say-Install "$($Source.Label) download failed. Trying $($Next.Base) ($($Next.Label))." '\u4e0b\u8f7d\u5931\u8d25\uff08{0}\uff09\uff0c\u6b63\u5728\u5c1d\u8bd5 {1}\uff08{2}\uff09\u3002' $label $Next.Base $nextLabel
}

function Get-DownloadError($Source, $ErrorRecord) {
    $detail = $ErrorRecord.Exception.Message
    return (Get-InstallText "$($Source.Label) download failed. $detail" '\u4e0b\u8f7d\u5931\u8d25\uff08{0}\uff09\u3002{1}' (Get-SourceLabel $Source.Label) $detail)
}

function Download-Release([string]$Dest) {
    $repo = if ($env:DEEPROUTER_SETUP_REPO) { $env:DEEPROUTER_SETUP_REPO } else { 'zfg5118/deeprouter' }
    $name = "deeprouter-windows-$(Get-GoArch).exe"
    $sources = @(Get-DownloadSources -Repo $repo)
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("deeprouter-" + [guid]::NewGuid().ToString('n') + '.exe')
    $lastError = Get-InstallText 'download failed. Clone the repo and run install.ps1, or set DEEPROUTER_SETUP_REPO.' '\u4e0b\u8f7d\u5931\u8d25\u3002\u53ef\u4ee5\u514b\u9686\u4ed3\u5e93\u540e\u8fd0\u884c install.ps1\uff0c\u6216\u8bbe\u7f6e DEEPROUTER_SETUP_REPO\u3002'
    try {
        for ($i = 0; $i -lt $sources.Count; $i++) {
            $source = $sources[$i]
            $next = $null
            if (($i + 1) -lt $sources.Count) { $next = $sources[$i + 1] }
            $url = Get-AssetUrl -Kind $source.Kind -Base $source.Base -Repo $repo -Name $name
            $sumUrl = Get-AssetUrl -Kind $source.Kind -Base $source.Base -Repo $repo -Name 'checksums.txt'
            Say-Install "Downloading $name from $url ($($source.Label))" '\u6b63\u5728\u4ece {0} \u4e0b\u8f7d {1}\uff08{2}\uff09' $url $name (Get-SourceLabel $source.Label)
            if (Test-Path $tmp) { Remove-Item -Force $tmp }
            try {
                Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing -TimeoutSec $source.Timeout
            } catch {
                $lastError = Get-DownloadError $source $_
                if ($next) { Say-TryingNext $source $next }
                continue
            }
            $sums = $null
            try {
                $sums = Get-ChecksumText -Url $sumUrl -TimeoutSec $source.Timeout
            } catch {
                $lastError = Get-DownloadError $source $_
                if ($next) { Say-TryingNext $source $next }
                continue
            }
            if ($script:ChecksumNotPublished) {
                if ($env:DEEPROUTER_SETUP_VERSION) {
                    Say-Install "checksums.txt was not published for v$($env:DEEPROUTER_SETUP_VERSION); skipped verification" 'v{0} \u6ca1\u6709\u53d1\u5e03 checksums.txt\uff0c\u5df2\u8df3\u8fc7\u6821\u9a8c\u3002' $env:DEEPROUTER_SETUP_VERSION
                } else {
                    Say-Install 'checksums.txt was not published for the latest release; skipped verification' '\u6700\u65b0\u7248\u672c\u6ca1\u6709\u53d1\u5e03 checksums.txt\uff0c\u5df2\u8df3\u8fc7\u6821\u9a8c\u3002'
                }
            } else {
                try {
                    # HTML or a checksum mismatch fails this source. A 404 above skips verification.
                    Confirm-Checksum -File $tmp -Name $name -Sums $sums
                } catch {
                    $lastError = $_.Exception.Message
                    if ($next) {
                        Write-Host $lastError
                        Say-TryingNext $source $next
                    }
                    continue
                }
            }
            $parent = Split-Path -Parent $Dest
            if ($parent) { New-Item -ItemType Directory -Force -Path $parent | Out-Null }
            Move-Item -Force -Path $tmp -Destination $Dest
            $tmp = $null
            return
        }
        throw $lastError
    } finally {
        if ($tmp -and (Test-Path $tmp)) { Remove-Item -Force $tmp }
    }
}

# Get-ChecksumText downloads checksums.txt to a temp file and reads it as UTF-8.
# GitHub serves release assets, including checksums.txt, as application/octet-stream
# after the redirect to release-assets.githubusercontent.com. For that content type,
# (Invoke-WebRequest -UseBasicParsing).Content is a [byte[]] on Windows PowerShell 5.1
# and on PowerShell 7. Binding that array to [string] joins the bytes as
# space-separated decimals ("102 53 55 55 ..."), so a real "HASH  name" line never
# matches and the installer reports "checksums.txt has no entry" for a file that
# is in the list. -OutFile keeps the raw bytes, the same way the binary is downloaded.
function Get-WebErrorStatus($ErrorRecord) {
    $candidates = @()
    if ($ErrorRecord.Exception) { $candidates += $ErrorRecord.Exception }
    if ($ErrorRecord.Exception -and $ErrorRecord.Exception.InnerException) { $candidates += $ErrorRecord.Exception.InnerException }
    foreach ($ex in $candidates) {
        if ($ex.PSObject.Properties['Response'] -and $ex.Response -and $ex.Response.PSObject.Properties['StatusCode']) {
            try { return [int]$ex.Response.StatusCode } catch { }
        }
        if ($ex.PSObject.Properties['StatusCode'] -and $null -ne $ex.StatusCode) {
            try { return [int]$ex.StatusCode } catch { }
        }
    }
    return 0
}

function Get-ChecksumText([string]$Url, [int]$TimeoutSec = 120) {
    $previous = $ProgressPreference
    $ProgressPreference = 'SilentlyContinue'
    $script:ChecksumNotPublished = $false
    $path = Join-Path ([System.IO.Path]::GetTempPath()) ("deeprouter-" + [guid]::NewGuid().ToString('n') + '.checksums.txt')
    try {
        try {
            Invoke-WebRequest -Uri $Url -OutFile $path -UseBasicParsing -TimeoutSec $TimeoutSec
        } catch {
            # 404 means the asset was not published, so verification is skipped.
            # A timeout or a connection error is thrown and the caller tries the next host.
            if ((Get-WebErrorStatus $_) -eq 404) {
                $script:ChecksumNotPublished = $true
                return ''
            }
            throw
        }
        return (Read-ChecksumText -Path $path)
    } finally {
        $ProgressPreference = $previous
        if (Test-Path $path) { Remove-Item -Force $path }
    }
}

function Read-ChecksumText([string]$Path) {
    $bytes = [System.IO.File]::ReadAllBytes($Path)
    return (ConvertTo-ChecksumText -Value $bytes)
}

# ConvertTo-ChecksumText accepts either the UTF-8 file bytes or a string.
# A [byte[]] is decoded explicitly so an octet-stream .Content value still
# parses if a caller passes it through.
function ConvertTo-ChecksumText($Value) {
    $text = $Value
    if ($Value -is [byte[]]) {
        $text = [System.Text.Encoding]::UTF8.GetString($Value)
    } else {
        $text = [string]$Value
    }
    if ([string]::IsNullOrEmpty($text)) { return '' }
    return $text.TrimStart([char]0xFEFF)
}

# SHA256 via .NET. Some Windows PowerShell hosts on current GitHub runners
# do not expose the Get-FileHash cmdlet until a profile loads it.
function Get-ContentSHA256([string]$Path) {
    $sha = [System.Security.Cryptography.SHA256]::Create()
    $stream = [System.IO.File]::OpenRead($Path)
    try {
        $bytes = $sha.ComputeHash($stream)
    } finally {
        $stream.Dispose()
        $sha.Dispose()
    }
    return ([System.BitConverter]::ToString($bytes)).Replace('-', '').ToLower()
}

function Confirm-Checksum([string]$File, [string]$Name, $Sums) {
    $text = ConvertTo-ChecksumText -Value $Sums
    $sample = $text
    if ($sample.Length -gt 2048) { $sample = $sample.Substring(0, 2048) }
    if ($sample -match '(?i)<!DOCTYPE\s+html|<html[\s>]') {
        throw (Get-InstallText 'checksums.txt looks like HTML, not a checksum list. The download was blocked or rewritten by a proxy.' 'checksums.txt \u770b\u8d77\u6765\u662f HTML\uff0c\u4e0d\u662f\u6821\u9a8c\u548c\u5217\u8868\u3002\u4e0b\u8f7d\u88ab\u4ee3\u7406\u62e6\u622a\u6216\u6539\u5199\u4e86\u3002')
    }
    $want = $null
    $parsed = 0
    foreach ($line in ($text -split '\r?\n')) {
        $trimmed = $line.Trim()
        if ($trimmed -eq '') { continue }
        # sha256sum writes "HASH  name" or "HASH *name" (binary mode).
        if ($trimmed -match '^([0-9a-fA-F]{64})\s+\*?(\S+)$') {
            $parsed++
            if ($Matches[2] -eq $Name -and -not $want) {
                $want = $Matches[1].ToLower()
            }
        }
    }
    if ($parsed -eq 0) {
        throw (Get-InstallText 'checksums.txt contained no checksum lines. The download was blocked or rewritten by a proxy.' 'checksums.txt \u91cc\u6ca1\u6709\u6821\u9a8c\u884c\u3002\u4e0b\u8f7d\u88ab\u4ee3\u7406\u62e6\u622a\u6216\u6539\u5199\u4e86\u3002')
    }
    if (-not $want) { throw (Get-InstallText "checksums.txt has no entry for $Name" 'checksums.txt \u91cc\u6ca1\u6709 {0} \u7684\u8bb0\u5f55' $Name) }
    $got = Get-ContentSHA256 -Path $File
    if ($got -ne $want) { throw (Get-InstallText "checksum mismatch for $Name" '{0} \u7684\u6821\u9a8c\u548c\u4e0d\u5339\u914d' $Name) }
    Say-Install 'Checksum verified.' '\u6821\u9a8c\u548c\u5df2\u6838\u5bf9\u3002'
}

function Install-CompatShim([string]$Dir, [string]$Target) {
    $shim = Join-Path $Dir 'deeprouter-setup.cmd'
    Set-Content -Encoding ASCII -Path $shim -Value "@echo off`r`n`"$Target`" %*`r`n"
}

# Remove-OldInstall deletes the previous deeprouter-setup.exe and drops that
# directory from the user PATH. A cmd shim is left there so an old PATH entry
# still launches deeprouter until the user opens a new terminal.
function Remove-OldInstall([string]$NewExe) {
    if (-not $env:LOCALAPPDATA) { return }
    $old = Join-Path $env:LOCALAPPDATA 'Programs\deeprouter-setup'
    if (-not (Test-Path $old)) { return }
    $oldFull = [System.IO.Path]::GetFullPath($old).TrimEnd('\')
    $newFull = [System.IO.Path]::GetFullPath((Split-Path -Parent $NewExe)).TrimEnd('\')
    $oldExe = Join-Path $old 'deeprouter-setup.exe'
    if (Test-Path $oldExe) { Remove-Item -Force $oldExe }
    if ($oldFull -eq $newFull) { return }
    $shim = Join-Path $old 'deeprouter-setup.cmd'
    Set-Content -Encoding ASCII -Path $shim -Value "@echo off`r`n`"$NewExe`" %*`r`n"
    Remove-UserPathEntry -Dir $oldFull
}

function Remove-UserPathEntry([string]$Dir) {
    $full = [System.IO.Path]::GetFullPath($Dir).TrimEnd('\')
    $hive = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    if ($null -eq $hive) { return }
    try {
        $kind = [Microsoft.Win32.RegistryValueKind]::ExpandString
        $raw = ''
        try {
            $kind = $hive.GetValueKind('Path')
            $raw = [string]$hive.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        } catch {
            return
        }
        $kept = New-Object System.Collections.Generic.List[string]
        $changed = $false
        foreach ($part in ($raw -split ';')) {
            if (-not $part) { continue }
            $expanded = [Environment]::ExpandEnvironmentVariables($part).TrimEnd('\')
            if ($expanded -ieq $full -or $part.TrimEnd('\') -ieq $full) {
                $changed = $true
                continue
            }
            $kept.Add($part)
        }
        if ($changed) {
            $hive.SetValue('Path', ($kept -join ';'), $kind)
            Publish-EnvironmentChange
        }
    } finally {
        $hive.Close()
    }
}

# Add-UserPathEntry appends Dir to HKCU\Environment Path and keeps the
# existing registry type so a REG_EXPAND_SZ value is not flattened.
function Add-UserPathEntry([string]$Dir) {
    $full = [System.IO.Path]::GetFullPath($Dir).TrimEnd('\')
    $hive = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    if ($null -eq $hive) {
        $hive = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment')
    }
    try {
        $kind = [Microsoft.Win32.RegistryValueKind]::ExpandString
        $raw = ''
        try {
            $kind = $hive.GetValueKind('Path')
            $raw = [string]$hive.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        } catch {
            $kind = [Microsoft.Win32.RegistryValueKind]::ExpandString
            $raw = ''
        }
        $found = $false
        if ($raw) {
            foreach ($part in ($raw -split ';')) {
                if (-not $part) { continue }
                $expanded = [Environment]::ExpandEnvironmentVariables($part).TrimEnd('\')
                if ($expanded -ieq $full -or $part.TrimEnd('\') -ieq $full) {
                    $found = $true
                    break
                }
            }
        }
        if (-not $found) {
            $updated = if ([string]::IsNullOrEmpty($raw)) { $full } else { $raw.TrimEnd(';') + ';' + $full }
            $hive.SetValue('Path', $updated, $kind)
            Publish-EnvironmentChange
            Say-Install "Added $full to the user PATH." '\u5df2\u628a {0} \u52a0\u5165\u7528\u6237 PATH\u3002' $full
            Say-Install 'Open a new terminal so the change takes effect.' '\u8bf7\u6253\u5f00\u4e00\u4e2a\u65b0\u7ec8\u7aef\uff0c\u6539\u52a8\u624d\u4f1a\u751f\u6548\u3002'
        } else {
            Say-Install "$full is already on the user PATH." '{0} \u5df2\u5728\u7528\u6237 PATH \u4e2d\u3002' $full
        }
    } finally {
        $hive.Close()
    }
    $present = $false
    foreach ($part in ($env:Path -split ';')) {
        if ($part.TrimEnd('\') -ieq $full) { $present = $true; break }
    }
    if (-not $present) {
        if ($env:Path) { $env:Path = $full + ';' + $env:Path } else { $env:Path = $full }
    }
}

function Publish-EnvironmentChange {
    try {
        if (-not ('Native.EnvBroadcast' -as [type])) {
            Add-Type -Namespace Native -Name EnvBroadcast -MemberDefinition @'
[System.Runtime.InteropServices.DllImport("user32.dll", SetLastError=true, CharSet=System.Runtime.InteropServices.CharSet.Unicode)]
public static extern System.IntPtr SendMessageTimeout(System.IntPtr hWnd, int Msg, System.UIntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out System.UIntPtr result);
'@
        }
        $result = [UIntPtr]::Zero
        [void][Native.EnvBroadcast]::SendMessageTimeout([IntPtr]0xffff, 0x001A, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
    } catch {
        Say-Install 'Saved the user PATH. Open a new terminal if this window does not see it yet.' '\u5df2\u4fdd\u5b58\u7528\u6237 PATH\u3002\u5982\u679c\u5f53\u524d\u7a97\u53e3\u8fd8\u770b\u4e0d\u5230\uff0c\u8bf7\u6253\u5f00\u4e00\u4e2a\u65b0\u7ec8\u7aef\u3002'
    }
}

function Test-ConsoleInput {
    try {
        return -not [Console]::IsInputRedirected
    } catch {
        return $false
    }
}

if (-not $script:InstallerDotSourced) {
    $script:InstallExitCode = 0
    try {
        Install-DeepRouter
    } catch {
        $message = $_.Exception.Message
        Say-Install "install.ps1: $message" 'install.ps1\uff1a{0}' $message
        $script:InstallExitCode = 1
    }
    # irm | iex runs in the caller's session, where exit would close the window.
    if ($script:InstallerCommandPath) {
        exit $script:InstallExitCode
    }
}
