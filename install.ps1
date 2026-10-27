# Install deeprouter on Windows.
#
# PowerShell:
#   irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex
#
# cmd:
#   powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex"
#
# The binary is installed to %LOCALAPPDATA%\Programs\deeprouter\deeprouter.exe.
# A deeprouter-setup.cmd shim in that directory runs deeprouter, and a previous
# %LOCALAPPDATA%\Programs\deeprouter-setup install is removed from PATH.
# (override with DEEPROUTER_SETUP_PREFIX) and that directory is added to the
# user PATH. Setup then runs in this console unless DEEPROUTER_NO_SETUP=1.
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
$script:InstallerDotSourced = ($MyInvocation.InvocationName -eq '.')

try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
} catch {
}

function Test-EnglishUI {
    $lang = $env:LC_ALL
    if (-not $lang) { $lang = $env:LANG }
    if (-not $lang) { return $false }
    return $lang -match '^(?i)en([._-]|$)'
}

function Say-Install([string]$English, [string]$Chinese) {
    if ((Test-EnglishUI) -or -not $Chinese) {
        Write-Host $English
    } else {
        Write-Host $Chinese
    }
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
                throw 'LOCALAPPDATA is not set'
            }
            $prefix = Join-Path $env:LOCALAPPDATA 'Programs\deeprouter'
        }
        New-Item -ItemType Directory -Force -Path $prefix | Out-Null
        $dest = Join-Path $prefix 'deeprouter.exe'

        $source = Find-InstallSource
        if ($opts.FromSource -and -not $source) {
            throw '--from-source needs the repository checkout (go.mod next to install.ps1)'
        }
        if ($source -and (Get-Command go -ErrorAction SilentlyContinue)) {
            Build-FromSource -Source $source -Dest $dest
        } elseif ($opts.FromSource) {
            throw 'Go is not installed, so the binary cannot be built from source'
        } else {
            if (-not (Test-WindowsOS)) {
                throw 'install.ps1 downloads the Windows binary. On macOS and Linux, run sh install.sh.'
            }
            Download-Release -Dest $dest
        }

        Install-CompatShim -Dir $prefix -Target $dest
        Remove-OldInstall -NewExe $dest
        Say-Install "Installed deeprouter to $dest" "已安装 deeprouter 到 $dest"
        Say-Install "The previous command name deeprouter-setup now runs deeprouter." "原来的命令名 deeprouter-setup 现在会运行 deeprouter。"
        if (-not $opts.NoPath) {
            Add-UserPathEntry -Dir $prefix
        }

        if ($opts.Provider) {
            $env:DEEPROUTER_PROVIDER = [string]$opts.Provider
        }
        if ($env:DEEPROUTER_NO_SETUP -eq '1' -or -not (Test-ConsoleInput)) {
            Write-Host ''
            Say-Install 'Next, run:' '接下来请运行：'
            if ($env:DEEPROUTER_PROVIDER) {
                Write-Host "  `$env:DEEPROUTER_PROVIDER='$env:DEEPROUTER_PROVIDER'; & '$dest'"
            } else {
                Write-Host "  $dest"
            }
            return
        }
        Write-Host ''
        Say-Install "Starting $dest" "正在启动 $dest"
        & $dest
        if ($null -ne $LASTEXITCODE -and $LASTEXITCODE -ne 0) {
            if ($LASTEXITCODE -eq 2) {
                exit 2
            }
            throw "deeprouter exited with code $LASTEXITCODE"
        }
    } finally {
        $ErrorActionPreference = $previousErrorAction
        $ProgressPreference = $previousProgress
    }
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
    $argv = @($script:InstallerArgs)
    for ($i = 0; $i -lt $argv.Count; $i++) {
        $arg = [string]$argv[$i]
        if ($arg -eq '-Prefix' -or $arg -eq '--prefix') {
            $i++
            if ($i -ge $argv.Count -or -not $argv[$i]) { throw '--prefix needs a directory' }
            $opts.Prefix = [string]$argv[$i]
            continue
        }
        if ($arg -eq '-NoPath' -or $arg -eq '--no-path') { $opts.NoPath = $true; continue }
        if ($arg -eq '-FromSource' -or $arg -eq '--from-source') { $opts.FromSource = $true; continue }
        if ($arg -eq '-Provider' -or $arg -eq '--provider') {
            $i++
            if ($i -ge $argv.Count -or -not $argv[$i]) { throw '--provider needs a name' }
            $opts.Provider = [string]$argv[$i]
            continue
        }
        if ($arg -like '--provider=*') {
            $opts.Provider = $arg.Substring(11)
            if (-not $opts.Provider) { throw '--provider needs a name' }
            continue
        }
        if ($arg -eq '-Version' -or $arg -eq '--version') { $opts.ShowVersion = $true; continue }
        if ($arg -eq '-h' -or $arg -eq '-Help' -or $arg -eq '--help') { $opts.ShowHelp = $true; continue }
        throw "unknown argument: $arg"
    }
    return $opts
}

function Write-InstallHelp {
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
    Say-Install "Building deeprouter from $Source" "正在从 $Source 构建 deeprouter"
    $previous = $env:CGO_ENABLED
    $hadPrevious = Test-Path Env:CGO_ENABLED
    Push-Location $Source
    try {
        $env:CGO_ENABLED = '0'
        & go build -trimpath -ldflags "-s -w -X main.version=$ver" -o $Dest .
        if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
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
        default { throw "unsupported architecture: $arch" }
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
# DEEPROUTER_DOWNLOAD_BASE names a host explicitly.
function Get-DownloadSources([string]$Repo) {
    $override = Strip-TrailingSlash $env:DEEPROUTER_DOWNLOAD_BASE
    $builtin = Strip-TrailingSlash $script:BuiltinDownloadBase
    $mirror = ''
    if ($Repo -eq $script:OfficialRepo) { $mirror = $script:KnownMirror }
    $sources = @()
    if ($override) {
        if (Test-GitHubBase $override) {
            $sources += @{ Kind = 'github'; Base = 'https://github.com'; Label = 'GitHub'; Timeout = 20 }
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
    $sources += @{ Kind = 'github'; Base = 'https://github.com'; Label = 'GitHub'; Timeout = 20 }
    if ($mirror) { $sources += @{ Kind = 'mirror'; Base = $mirror; Label = 'mirror'; Timeout = 120 } }
    return $sources
}

function Download-Release([string]$Dest) {
    $repo = if ($env:DEEPROUTER_SETUP_REPO) { $env:DEEPROUTER_SETUP_REPO } else { 'zfg5118/deeprouter' }
    $name = "deeprouter-windows-$(Get-GoArch).exe"
    $sources = @(Get-DownloadSources -Repo $repo)
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("deeprouter-" + [guid]::NewGuid().ToString('n') + '.exe')
    $lastError = 'download failed. Clone the repo and run install.ps1, or set DEEPROUTER_SETUP_REPO.'
    try {
        for ($i = 0; $i -lt $sources.Count; $i++) {
            $source = $sources[$i]
            $url = Get-AssetUrl -Kind $source.Kind -Base $source.Base -Repo $repo -Name $name
            $sumUrl = Get-AssetUrl -Kind $source.Kind -Base $source.Base -Repo $repo -Name 'checksums.txt'
            Say-Install "Downloading $name from $url ($($source.Label))" "正在从 $url 下载 $name（$($source.Label)）"
            if (Test-Path $tmp) { Remove-Item -Force $tmp }
            try {
                Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing -TimeoutSec $source.Timeout
            } catch {
                $lastError = "$($source.Label) download failed. $($_.Exception.Message)"
                if (($i + 1) -lt $sources.Count) {
                    $next = $sources[$i + 1]
                    Say-Install "$($source.Label) download failed. Trying $($next.Base) ($($next.Label))." "$($source.Label) 下载失败，正在尝试 $($next.Base)（$($next.Label)）。"
                }
                continue
            }
            $sums = $null
            try {
                $sums = Get-ChecksumText -Url $sumUrl -TimeoutSec $source.Timeout
            } catch {
                $lastError = "$($source.Label) download failed. $($_.Exception.Message)"
                if (($i + 1) -lt $sources.Count) {
                    $next = $sources[$i + 1]
                    Say-Install "$($source.Label) download failed. Trying $($next.Base) ($($next.Label))." "$($source.Label) 下载失败，正在尝试 $($next.Base)（$($next.Label)）。"
                }
                continue
            }
            if ($script:ChecksumNotPublished) {
                if ($env:DEEPROUTER_SETUP_VERSION) {
                    Say-Install "checksums.txt was not published for v$($env:DEEPROUTER_SETUP_VERSION); skipped verification" "v$($env:DEEPROUTER_SETUP_VERSION) 没有发布 checksums.txt，已跳过校验。"
                } else {
                    Say-Install 'checksums.txt was not published for the latest release; skipped verification' '最新版本没有发布 checksums.txt，已跳过校验。'
                }
            } else {
                try {
                    # HTML or a checksum mismatch fails this source. A 404 above skips verification.
                    Confirm-Checksum -File $tmp -Name $name -Sums $sums
                } catch {
                    $lastError = $_.Exception.Message
                    if (($i + 1) -lt $sources.Count) {
                        $next = $sources[$i + 1]
                        Say-Install "$($source.Label) download failed. Trying $($next.Base) ($($next.Label))." "$($source.Label) 下载失败，正在尝试 $($next.Base)（$($next.Label)）。"
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
        throw 'checksums.txt looks like HTML, not a checksum list. The download was blocked or rewritten by a proxy.'
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
        throw 'checksums.txt contained no checksum lines. The download was blocked or rewritten by a proxy.'
    }
    if (-not $want) { throw "checksums.txt has no entry for $Name" }
    $got = Get-ContentSHA256 -Path $File
    if ($got -ne $want) { throw "checksum mismatch for $Name" }
    Say-Install 'Checksum verified.' '校验和已核对。'
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
            Say-Install "Added $full to the user PATH." "已把 $full 加入用户 PATH。"
            Say-Install 'Open a new terminal so the change takes effect.' '请打开一个新终端，改动才会生效。'
        } else {
            Say-Install "$full is already on the user PATH." "$full 已在用户 PATH 中。"
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
        Say-Install 'Saved the user PATH. Open a new terminal if this window does not see it yet.' '已保存用户 PATH。如果当前窗口还看不到，请打开一个新终端。'
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
    try {
        Install-DeepRouter
    } catch {
        Write-Host "install.ps1: $($_.Exception.Message)"
        if ($script:InstallerCommandPath) { exit 1 }
    }
}
