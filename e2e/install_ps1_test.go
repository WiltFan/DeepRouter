package e2e

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// TestInstallPS1UnderIexAndAsAFile covers how install.ps1 ends. irm | iex runs
// the text in the caller's session, so a failed install reports itself and
// returns instead of exiting that window. Run as a file, the same failure
// exits 1. A cancelled setup (2) or Ctrl+C (130) adds no line of its own.
func TestInstallPS1UnderIexAndAsAFile(t *testing.T) {
	ps := findPowerShell(t)
	root := moduleRoot(t)
	installer := filepath.Join(root, "install.ps1")
	driver := filepath.Join(t.TempDir(), "ending-driver.ps1")
	if err := os.WriteFile(driver, []byte(installPS1EndingDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", driver, installer)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ending driver: %v\n%s", err, out)
	}

	dir := t.TempDir()
	body, err := os.ReadFile(installer)
	if err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(dir, "install.ps1")
	if err := os.WriteFile(copied, body, 0o644); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", copied, "-FromSource", "-Prefix", filepath.Join(dir, "bin"))
	run.Env = envWithout(os.Environ(), "DEEPROUTER_SETUP_SOURCE")
	got, err := run.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("file run: want exit 1, got %v\n%s", err, got)
	}
	if !strings.Contains(string(got), "install.ps1: --from-source needs the repository checkout") {
		t.Fatalf("file run output:\n%s", got)
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

    $live = Get-ChecksumText -Url 'https://github.com/WiltFan/DeepRouter/releases/latest/download/checksums.txt'
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

$sources = @(Get-DownloadSources -Repo 'WiltFan/DeepRouter')
if ($sources.Count -ne 2) { throw "official count $($sources.Count)" }
if ($sources[0].Label -ne 'GitHub' -or $sources[0].Timeout -ne 45) { throw 'GitHub is not first' }
if ($sources[1].Base -ne 'https://deeprouter.top' -or $sources[1].Label -ne 'mirror' -or $sources[1].Timeout -ne 120) { throw 'mirror is not second' }
$url = Get-AssetUrl -Kind 'mirror' -Base 'https://deeprouter.top/' -Repo 'WiltFan/DeepRouter' -Name 'checksums.txt'
if ($url -ne 'https://deeprouter.top/releases/latest/checksums.txt') { throw $url }
$env:DEEPROUTER_SETUP_VERSION = '0.3.3'
$url = Get-AssetUrl -Kind 'github' -Base 'https://github.com' -Repo 'WiltFan/DeepRouter' -Name 'deeprouter-windows-amd64.exe'
if ($url -ne 'https://github.com/WiltFan/DeepRouter/releases/download/v0.3.3/deeprouter-windows-amd64.exe') { throw $url }
$url = Get-AssetUrl -Kind 'mirror' -Base 'https://deeprouter.top' -Repo 'WiltFan/DeepRouter' -Name 'deeprouter-windows-amd64.exe'
if ($url -ne 'https://deeprouter.top/releases/v0.3.3/deeprouter-windows-amd64.exe') { throw $url }

Clear-InstallEnv 'DEEPROUTER_SETUP_VERSION'
$fork = @(Get-DownloadSources -Repo 'other/fork')
if ($fork.Count -ne 1 -or $fork[0].Label -ne 'GitHub') { throw "fork $($fork.Count)" }

$script:BuiltinDownloadBase = 'https://deeprouter.top'
$bakedFork = @(Get-DownloadSources -Repo 'other/fork')
if ($bakedFork.Count -ne 1) { throw 'fork used the baked mirror' }
$baked = @(Get-DownloadSources -Repo 'WiltFan/DeepRouter')
if ($baked.Count -ne 2 -or $baked[0].Label -ne 'mirror' -or $baked[0].Base -ne 'https://deeprouter.top' -or $baked[0].Timeout -ne 45) { throw 'baked mirror is not first' }

$env:DEEPROUTER_DOWNLOAD_BASE = 'https://example.test/mirror/'
$custom = @(Get-DownloadSources -Repo 'other/fork')
if ($custom.Count -ne 2 -or $custom[0].Base -ne 'https://example.test/mirror' -or $custom[0].Timeout -ne 45 -or $custom[1].Label -ne 'GitHub') { throw "custom $($custom[0].Base)" }
$env:DEEPROUTER_DOWNLOAD_BASE = 'https://github.com/WiltFan/DeepRouter'
$asGitHub = @(Get-DownloadSources -Repo 'WiltFan/DeepRouter')
if ($asGitHub[0].Label -ne 'GitHub' -or $asGitHub[1].Base -ne 'https://deeprouter.top') { throw 'github base did not fall back to the mirror' }
Write-Output 'download sources ok'
`

const installPS1EndingDriver = `
param([Parameter(Mandatory = $true)][string]$Installer)
$ErrorActionPreference = 'Stop'

# irm hands iex the decoded text without stripping a BOM, so read it the same way.
$text = [System.Text.Encoding]::UTF8.GetString([System.IO.File]::ReadAllBytes($Installer))
$prefix = Join-Path ([System.IO.Path]::GetTempPath()) ('deeprouter-iex-' + [guid]::NewGuid().ToString('n'))
$env:DEEPROUTER_SETUP_FROM_SOURCE = '1'
$env:DEEPROUTER_SETUP_PREFIX = $prefix
if (Test-Path Env:DEEPROUTER_SETUP_SOURCE) { Remove-Item Env:DEEPROUTER_SETUP_SOURCE }
try {
    $ErrorActionPreference = 'Continue'
    # Piped like irm | iex. A redirect on Invoke-Expression itself would not
    # capture errors raised by the text it runs.
    $out = (& { $text | Invoke-Expression } *>&1 | Out-String)
    $ErrorActionPreference = 'Stop'
} finally {
    Remove-Item Env:DEEPROUTER_SETUP_FROM_SOURCE
    Remove-Item Env:DEEPROUTER_SETUP_PREFIX
    if (Test-Path $prefix) { Remove-Item -Recurse -Force $prefix }
}
if ($out -like '*is not recognized*') { throw "iex ran a stray command: $out" }
if ($out -notlike '*install.ps1: --from-source needs the repository checkout*') { throw "iex output: $out" }

. $Installer
foreach ($code in @(2, 130)) {
    $lines = @(Get-SetupExitCode -Code $code -Dest 'C:\deeprouter\deeprouter.exe' 6>&1)
    if ($lines.Count -ne 1 -or [int]$lines[0] -ne $code) { throw "exit $code printed: $($lines -join ' | ')" }
}
$lines = @(Get-SetupExitCode -Code 1 -Dest 'C:\Program Files\deeprouter\deeprouter.exe' 6>&1)
if ([int]$lines[-1] -ne 1) { throw "exit 1 returned $($lines[-1])" }
$shown = ($lines[0..($lines.Count - 2)] | ForEach-Object { [string]$_ }) -join [char]10
if ($shown -notlike '*did not finish setup (exit code 1)*') { throw "exit 1 message: $shown" }
if ($shown -notlike "*& 'C:\Program Files\deeprouter\deeprouter.exe'*") { throw "exit 1 command: $shown" }
$lines = @(Get-SetupExitCode -Code 0 -Dest 'C:\deeprouter\deeprouter.exe' 6>&1)
if ($lines.Count -ne 1 -or [int]$lines[0] -ne 0) { throw 'exit 0 printed a line' }

$source = [System.IO.File]::ReadAllText($Installer)
$templates = 0
foreach ($m in [regex]::Matches($source, "'([^'\r\n]*\\u[0-9a-fA-F]{4}[^'\r\n]*)'")) {
    [void][regex]::Unescape($m.Groups[1].Value)
    $templates++
}
if ($templates -lt 30) { throw "found $templates escaped Chinese strings" }

$env:LC_ALL = 'C'
$env:LANG = 'C'
if (Test-Path Env:DEEPROUTER_LANG) { Remove-Item Env:DEEPROUTER_LANG }
$next = Get-InstallText 'Next, run:' '\u63a5\u4e0b\u6765\u8bf7\u8fd0\u884c\uff1a'
if ([int]$next[0] -ne 0x63a5) { throw "the C locale did not select Chinese: $next" }
$help = (Write-InstallHelp | Out-String)
if ($help.IndexOf([char]0x7528) -lt 0 -or $help.IndexOf('%LOCALAPPDATA%\Programs\deeprouter') -lt 0) { throw "Chinese help: $help" }
$env:DEEPROUTER_LANG = 'en'
if ((Get-InstallText 'Next, run:' '\u63a5') -ne 'Next, run:') { throw 'DEEPROUTER_LANG=en did not select English' }
Remove-Item Env:DEEPROUTER_LANG
$env:LC_ALL = 'en_GB.UTF-8'
if ((Get-InstallText 'Next, run:' '\u63a5') -ne 'Next, run:') { throw 'en_GB did not select English' }
Write-Output 'ending ok'
`
