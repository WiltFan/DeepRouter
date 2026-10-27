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
    $hash = (Get-FileHash -Algorithm SHA256 -Path $dummy).Hash.ToLower()
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
