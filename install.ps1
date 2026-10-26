# Install deeprouter on Windows.
#
# PowerShell:
#   irm https://raw.githubusercontent.com/zfg5118/deeprouter-setup/main/install.ps1 | iex
#
# cmd:
#   powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/zfg5118/deeprouter-setup/main/install.ps1 | iex"
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
#   -Version | --version
# iex (the one-liner) ignores arguments and reads the environment instead.
# DEEPROUTER_NO_PATH=1 skips the user PATH update.
# DEEPROUTER_SETUP_FROM_SOURCE=1 builds from a checkout.
# DEEPROUTER_SETUP_VERSION without a leading v pins a release.
# DEEPROUTER_SETUP_REPO=owner/name selects a fork.

$script:InstallerRoot = $PSScriptRoot
$script:InstallerCommandPath = $PSCommandPath
$script:InstallerArgs = @($args)
$script:InstallerDotSourced = ($MyInvocation.InvocationName -eq '.')

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
        Write-Host "Installed deeprouter to $dest"
        Write-Host "The previous command name deeprouter-setup now runs deeprouter."
        if (-not $opts.NoPath) {
            Add-UserPathEntry -Dir $prefix
        }

        if ($env:DEEPROUTER_NO_SETUP -eq '1' -or -not (Test-ConsoleInput)) {
            Write-Host ''
            Write-Host 'Next, run:'
            Write-Host "  $dest"
            return
        }
        Write-Host ''
        Write-Host "Starting $dest"
        & $dest
        if ($null -ne $LASTEXITCODE -and $LASTEXITCODE -ne 0) {
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
        if ($arg -eq '-Version' -or $arg -eq '--version') { $opts.ShowVersion = $true; continue }
        if ($arg -eq '-h' -or $arg -eq '-Help' -or $arg -eq '--help') { $opts.ShowHelp = $true; continue }
        throw "unknown argument: $arg"
    }
    return $opts
}

function Write-InstallHelp {
    Write-Output @'
Usage: install.ps1 [-Prefix DIR] [-FromSource] [-NoPath] [-Version]

Installs the deeprouter binary on Windows.

  -Prefix DIR      Destination directory
                   (default: %LOCALAPPDATA%\Programs\deeprouter)
  -FromSource      Build with the local Go toolchain instead of downloading
  -NoPath          Do not add the destination directory to the user PATH
  -Version         Print the installer version and exit

PowerShell:
  irm https://raw.githubusercontent.com/zfg5118/deeprouter-setup/main/install.ps1 | iex

cmd:
  powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/zfg5118/deeprouter-setup/main/install.ps1 | iex"

DEEPROUTER_NO_SETUP=1 skips the automatic setup. A console is required for
the interactive prompts; there is no /dev/tty on Windows.
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
    Write-Host "Building deeprouter from $Source"
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

function Get-ReleaseAssetUrl([string]$Repo, [string]$Name) {
    if ($env:DEEPROUTER_SETUP_VERSION) {
        return "https://github.com/$Repo/releases/download/v$($env:DEEPROUTER_SETUP_VERSION)/$Name"
    }
    return "https://github.com/$Repo/releases/latest/download/$Name"
}

function Download-Release([string]$Dest) {
    $repo = if ($env:DEEPROUTER_SETUP_REPO) { $env:DEEPROUTER_SETUP_REPO } else { 'zfg5118/deeprouter-setup' }
    $name = "deeprouter-windows-$(Get-GoArch).exe"
    $url = Get-ReleaseAssetUrl -Repo $repo -Name $name
    $sumUrl = Get-ReleaseAssetUrl -Repo $repo -Name 'checksums.txt'
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("deeprouter-" + [guid]::NewGuid().ToString('n') + '.exe')
    Write-Host "Downloading $url"
    try {
        try {
            Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing
        } catch {
            throw "download failed. Clone the repo and run install.ps1, or set DEEPROUTER_SETUP_REPO. $($_.Exception.Message)"
        }
        $sums = $null
        try {
            $sums = (Invoke-WebRequest -Uri $sumUrl -UseBasicParsing).Content
        } catch {
            if ($env:DEEPROUTER_SETUP_VERSION) {
                Write-Host "checksums.txt was not published for v$($env:DEEPROUTER_SETUP_VERSION); skipped verification"
            } else {
                Write-Host 'checksums.txt was not published for the latest release; skipped verification'
            }
        }
        if ($null -ne $sums) {
            Confirm-Checksum -File $tmp -Name $name -Sums $sums
        }
        $parent = Split-Path -Parent $Dest
        if ($parent) { New-Item -ItemType Directory -Force -Path $parent | Out-Null }
        Move-Item -Force -Path $tmp -Destination $Dest
        $tmp = $null
    } finally {
        if ($tmp -and (Test-Path $tmp)) { Remove-Item -Force $tmp }
    }
}

function Confirm-Checksum([string]$File, [string]$Name, [string]$Sums) {
    $want = $null
    foreach ($line in ($Sums -split '\r?\n')) {
        $trimmed = $line.Trim()
        if ($trimmed -eq '') { continue }
        if ($trimmed -match '^([0-9a-fA-F]{64})\s+\*?(\S+)$') {
            if ($Matches[2] -eq $Name) {
                $want = $Matches[1].ToLower()
                break
            }
        }
    }
    if (-not $want) { throw "checksums.txt has no entry for $Name" }
    $got = (Get-FileHash -Algorithm SHA256 -Path $File).Hash.ToLower()
    if ($got -ne $want) { throw "checksum mismatch for $Name" }
    Write-Host 'Checksum verified.'
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
            Write-Host "Added $full to the user PATH."
            Write-Host 'Open a new terminal so the change takes effect.'
        } else {
            Write-Host "$full is already on the user PATH."
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
        Write-Host 'Saved the user PATH. Open a new terminal if this window does not see it yet.'
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
