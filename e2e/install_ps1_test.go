package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestInstallPS1ChecksumParsing drives install.ps1's checksum helpers.
// Invoke-WebRequest .Content is a byte slice when checksums.txt is served as
// application/octet-stream. The helpers must find deeprouter-windows-amd64.exe
// from those bytes, from a UTF-8 file read the same way -OutFile leaves it,
// and from the published checksum list.
func TestInstallPS1ChecksumParsing(t *testing.T) {
	ps := findPowerShell(t)
	root := moduleRoot(t)
	installer := filepath.Join(root, "install.ps1")
	driver := filepath.Join(t.TempDir(), "checksum-driver.ps1")
	if err := os.WriteFile(driver, []byte(installPS1ChecksumDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", driver, installer)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("checksum driver: %v\n%s", err, out)
	}
}

func TestInstallPS1DownloadSources(t *testing.T) {
	ps := findPowerShell(t)
	root := moduleRoot(t)
	installer := filepath.Join(root, "install.ps1")
	driver := filepath.Join(t.TempDir(), "source-driver.ps1")
	if err := os.WriteFile(driver, []byte(installPS1SourceDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", driver, installer)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("download sources: %v\n%s", err, out)
	}
}

func findPowerShell(t *testing.T) string {
	t.Helper()
	// Windows PowerShell 5.1 is powershell.exe. It is the host that turns an
	// octet-stream body into a byte array. PowerShell 7 is the fallback.
	names := []string{"pwsh"}
	if runtime.GOOS == "windows" {
		names = []string{"powershell.exe", "powershell", "pwsh.exe", "pwsh"}
	}
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("PowerShell is not installed")
	return ""
}

const installPS1ChecksumDriver = `
param([Parameter(Mandatory = $true)][string]$Installer)
$ErrorActionPreference = 'Stop'
. $Installer

$dummy = Join-Path ([System.IO.Path]::GetTempPath()) ("deeprouter-dummy-" + [guid]::NewGuid().ToString('n'))
Set-Content -Encoding Ascii -Path $dummy -Value 'not-the-binary'
try {
    $hash = Get-ContentSHA256 -Path $dummy
    $other = ('ab' * 32)
    $nl = [string]([char]13) + [string]([char]10)
    $text = $hash + "  deeprouter-windows-amd64.exe" + $nl + $nl + $other + " *deeprouter-windows-arm64.exe" + $nl
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($text)
    $withBom = New-Object byte[] ($bytes.Length + 3)
    $withBom[0] = 0xEF
    $withBom[1] = 0xBB
    $withBom[2] = 0xBF
    [System.Array]::Copy($bytes, 0, $withBom, 3, $bytes.Length)

    Confirm-Checksum -File $dummy -Name 'deeprouter-windows-amd64.exe' -Sums $bytes
    Confirm-Checksum -File $dummy -Name 'deeprouter-windows-amd64.exe' -Sums $withBom
    try {
        Confirm-Checksum -File $dummy -Name 'deeprouter-windows-arm64.exe' -Sums $bytes
        throw 'expected the starred arm64 line to be found'
    } catch {
        if ($_.Exception.Message -ne 'checksum mismatch for deeprouter-windows-arm64.exe') { throw }
    }

    $saved = Join-Path ([System.IO.Path]::GetTempPath()) ("deeprouter-sums-" + [guid]::NewGuid().ToString('n') + '.txt')
    [System.IO.File]::WriteAllBytes($saved, $bytes)
    try {
        $fromFile = Read-ChecksumText -Path $saved
        Confirm-Checksum -File $dummy -Name 'deeprouter-windows-amd64.exe' -Sums $fromFile
    } finally {
        Remove-Item -Force $saved
    }

    foreach ($bad in @(
        '<html><body>gateway</body></html>',
        ("<!DOCTYPE html>" + [char]10 + "<title>proxy</title>"),
        ("not a checksum list" + [char]10)
    )) {
        try {
            Confirm-Checksum -File $dummy -Name 'deeprouter-windows-amd64.exe' -Sums $bad
            throw "expected a bad-download error for: $bad"
        } catch {
            $msg = $_.Exception.Message
            if ($msg -like 'checksums.txt has no entry for *') {
                throw "bad download was reported as a missing name: $msg"
            }
            if ($msg -notlike '*proxy*') { throw }
        }
    }

    try {
        Confirm-Checksum -File $dummy -Name 'deeprouter-windows-amd64.exe' -Sums ($other + "  other.exe" + [char]10)
        throw 'expected a missing-name error'
    } catch {
        $msg = $_.Exception.Message
        if ($msg -ne 'checksums.txt has no entry for deeprouter-windows-amd64.exe') { throw }
    }

    $live = Get-ChecksumText -Url 'https://github.com/zfg5118/deeprouter/releases/latest/download/checksums.txt'
    if ($live -notmatch '(?m)^[0-9a-fA-F]{64}\s+\*?deeprouter-windows-amd64\.exe\s*$') {
        throw ("published checksums.txt has no deeprouter-windows-amd64.exe entry:" + [char]10 + $live)
    }
    if ($live -notmatch '(?m)^[0-9a-fA-F]{64}\s+\*?deeprouter-windows-arm64\.exe\s*$') {
        throw ("published checksums.txt has no deeprouter-windows-arm64.exe entry:" + [char]10 + $live)
    }
    try {
        Confirm-Checksum -File $dummy -Name 'deeprouter-windows-amd64.exe' -Sums $live
        throw 'expected the published hash to reject the dummy file'
    } catch {
        if ($_.Exception.Message -ne 'checksum mismatch for deeprouter-windows-amd64.exe') { throw }
    }
    Write-Output 'checksum parsing ok'
} finally {
    if (Test-Path $dummy) { Remove-Item -Force $dummy }
}
`

const installPS1SourceDriver = `
param([Parameter(Mandatory = $true)][string]$Installer)
$ErrorActionPreference = 'Stop'
. $Installer

function Clear-InstallEnv([string]$Name) {
    if (Test-Path "Env:$Name") { Remove-Item "Env:$Name" }
}
Clear-InstallEnv 'DEEPROUTER_DOWNLOAD_BASE'
Clear-InstallEnv 'DEEPROUTER_SETUP_VERSION'
Clear-InstallEnv 'DEEPROUTER_SETUP_REPO'
$script:BuiltinDownloadBase = ''

$sources = @(Get-DownloadSources -Repo 'zfg5118/deeprouter')
if ($sources.Count -ne 2) { throw "official count $($sources.Count)" }
if ($sources[0].Label -ne 'GitHub' -or $sources[0].Timeout -ne 20) { throw 'GitHub is not first' }
if ($sources[1].Base -ne 'https://deeprouter.top' -or $sources[1].Label -ne 'mirror' -or $sources[1].Timeout -ne 120) { throw 'mirror is not second' }
$url = Get-AssetUrl -Kind 'mirror' -Base 'https://deeprouter.top/' -Repo 'zfg5118/deeprouter' -Name 'checksums.txt'
if ($url -ne 'https://deeprouter.top/releases/latest/checksums.txt') { throw $url }
$env:DEEPROUTER_SETUP_VERSION = '0.3.3'
$url = Get-AssetUrl -Kind 'github' -Base 'https://github.com' -Repo 'zfg5118/deeprouter' -Name 'deeprouter-windows-amd64.exe'
if ($url -ne 'https://github.com/zfg5118/deeprouter/releases/download/v0.3.3/deeprouter-windows-amd64.exe') { throw $url }
$url = Get-AssetUrl -Kind 'mirror' -Base 'https://deeprouter.top' -Repo 'zfg5118/deeprouter' -Name 'deeprouter-windows-amd64.exe'
if ($url -ne 'https://deeprouter.top/releases/v0.3.3/deeprouter-windows-amd64.exe') { throw $url }

Clear-InstallEnv 'DEEPROUTER_SETUP_VERSION'
$fork = @(Get-DownloadSources -Repo 'other/fork')
if ($fork.Count -ne 1 -or $fork[0].Label -ne 'GitHub') { throw "fork $($fork.Count)" }

$script:BuiltinDownloadBase = 'https://deeprouter.top'
$bakedFork = @(Get-DownloadSources -Repo 'other/fork')
if ($bakedFork.Count -ne 1) { throw 'fork used the baked mirror' }
$baked = @(Get-DownloadSources -Repo 'zfg5118/deeprouter')
if ($baked.Count -ne 2 -or $baked[0].Label -ne 'mirror' -or $baked[0].Base -ne 'https://deeprouter.top') { throw 'baked mirror is not first' }

$env:DEEPROUTER_DOWNLOAD_BASE = 'https://example.test/mirror/'
$custom = @(Get-DownloadSources -Repo 'other/fork')
if ($custom.Count -ne 2 -or $custom[0].Base -ne 'https://example.test/mirror' -or $custom[1].Label -ne 'GitHub') { throw "custom $($custom[0].Base)" }
$env:DEEPROUTER_DOWNLOAD_BASE = 'https://github.com/zfg5118/deeprouter'
$asGitHub = @(Get-DownloadSources -Repo 'zfg5118/deeprouter')
if ($asGitHub[0].Label -ne 'GitHub' -or $asGitHub[1].Base -ne 'https://deeprouter.top') { throw 'github base did not fall back to the mirror' }
Write-Output 'download sources ok'
`
