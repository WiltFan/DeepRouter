package e2e

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Fixture hashes served as checksums.txt. Neither matches the driver's dummy file.
var (
	fixtureAmd64Hash = strings.Repeat("a1", 32)
	fixtureArm64Hash = strings.Repeat("b2", 32)
)

// TestInstallPS1ChecksumParsing drives install.ps1's checksum helpers.
// Invoke-WebRequest .Content is a byte slice when checksums.txt is served as
// application/octet-stream. The helpers must find deeprouter-windows-amd64.exe
// from those bytes, from a UTF-8 file read the same way -OutFile leaves it,
// and from a loopback fixture checksums.txt fetched by the real Get-ChecksumText.
// PowerShell ignores testgate.DenyPublicDial, so the driver wraps
// Invoke-WebRequest to refuse non-loopback URLs, and the child's proxy
// variables point at a deny proxy that fails the test if anything reaches it.
func TestInstallPS1ChecksumParsing(t *testing.T) {
	ps := findPowerShell(t)
	root := moduleRoot(t)
	installer := filepath.Join(root, "install.ps1")
	driver := filepath.Join(t.TempDir(), "checksum-driver.ps1")
	if err := os.WriteFile(driver, []byte(installPS1ChecksumDriver), 0o644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	served := map[string]int{}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		served[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/releases/latest/download/checksums.txt":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte(fixtureAmd64Hash + "  deeprouter-windows-amd64.exe\n" +
				fixtureArm64Hash + "  deeprouter-windows-arm64.exe\n" +
				strings.Repeat("c3", 32) + "  deeprouter-linux-amd64\n"))
		case "/html/checksums.txt":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!DOCTYPE html>\n<html><body>proxy login</body></html>\n"))
		case "/empty/checksums.txt":
			w.Header().Set("Content-Type", "application/octet-stream")
		case "/broken/checksums.txt":
			http.Error(w, "upstream failed", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fixture.Close()

	proxy, leaked := denyProxy(t)
	cmd := exec.Command(ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", driver,
		installer, fixture.URL, fixtureAmd64Hash, fixtureArm64Hash)
	cmd.Env = withDenyProxy(os.Environ(), proxy)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("checksum driver: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "checksum parsing ok") {
		t.Fatalf("checksum driver did not finish:\n%s", out)
	}
	if hosts := leaked(); len(hosts) > 0 {
		t.Fatalf("PowerShell sent public requests through the deny proxy: %v\n%s", hosts, out)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, p := range []string{"/releases/latest/download/checksums.txt", "/missing/checksums.txt", "/html/checksums.txt", "/empty/checksums.txt", "/broken/checksums.txt"} {
		if served[p] == 0 {
			t.Errorf("fixture %s was never requested; served %v", p, served)
		}
	}
}

// TestInstallPS1PublishedChecksums checks the real latest release. It needs a
// published release and network access, so it only runs when
// DEEPROUTER_LIVE_RELEASE_TEST=1. A 404 is a failure here, not a skip.
func TestInstallPS1PublishedChecksums(t *testing.T) {
	if os.Getenv("DEEPROUTER_LIVE_RELEASE_TEST") != "1" {
		t.Skip("set DEEPROUTER_LIVE_RELEASE_TEST=1 to check the published release")
	}
	ps := findPowerShell(t)
	installer := filepath.Join(moduleRoot(t), "install.ps1")
	driver := filepath.Join(t.TempDir(), "published-driver.ps1")
	if err := os.WriteFile(driver, []byte(installPS1PublishedDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", driver, installer,
		"https://github.com/WiltFan/DeepRouter/releases/latest/download/checksums.txt")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("published checksums: %v\n%s", err, out)
	}
}

// denyProxy answers every request with 403 and records its host. The child's
// proxy variables point here so a public request from pwsh 7 cannot leave the
// box. Windows PowerShell 5.1 ignores these variables; the driver's
// Invoke-WebRequest wrapper covers it.
func denyProxy(t *testing.T) (url string, leaked func() []string) {
	t.Helper()
	var mu sync.Mutex
	var hosts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hosts = append(hosts, r.Method+" "+r.Host)
		mu.Unlock()
		http.Error(w, "public request denied in tests", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), hosts...)
	}
}

func withDenyProxy(base []string, proxy string) []string {
	keys := []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY"}
	out := make([]string, 0, len(base)+len(keys))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, k := range keys {
			if strings.EqualFold(name, k) {
				drop = true
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	for _, k := range keys[:3] {
		out = append(out, k+"="+proxy)
		if runtime.GOOS != "windows" {
			out = append(out, strings.ToLower(k)+"="+proxy)
		}
	}
	out = append(out, "NO_PROXY=127.0.0.1,localhost")
	if runtime.GOOS != "windows" {
		out = append(out, "no_proxy=127.0.0.1,localhost")
	}
	return out
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
param(
    [Parameter(Mandatory = $true)][string]$Installer,
    [Parameter(Mandatory = $true)][string]$Base,
    [Parameter(Mandatory = $true)][string]$Amd64,
    [Parameter(Mandatory = $true)][string]$Arm64
)
$ErrorActionPreference = 'Stop'
. $Installer

# Leak tripwire: Get-ChecksumText resolves Invoke-WebRequest to this function,
# which refuses anything but loopback and then calls the real cmdlet.
$script:LoopbackRequests = 0
function Invoke-WebRequest {
    [CmdletBinding()]
    param([Uri]$Uri, [string]$OutFile, [switch]$UseBasicParsing, [int]$TimeoutSec)
    if (-not $Uri.IsLoopback) { throw "public request blocked: $Uri" }
    $script:LoopbackRequests++
    Microsoft.PowerShell.Utility\Invoke-WebRequest @PSBoundParameters
}

function Test-ArchEntries($Text) {
    $t = [string]$Text
    return (($t -match '(?m)^[0-9a-fA-F]{64}\s+\*?deeprouter-windows-amd64\.exe\s*$') -and
        ($t -match '(?m)^[0-9a-fA-F]{64}\s+\*?deeprouter-windows-arm64\.exe\s*$'))
}

function Assert-BadSums($Sums, [string]$Label) {
    try {
        Confirm-Checksum -File $dummy -Name 'deeprouter-windows-amd64.exe' -Sums $Sums
    } catch {
        $msg = $_.Exception.Message
        if ($msg -like 'checksums.txt has no entry for *') { throw "$Label was reported as a missing name: $msg" }
        if ($msg -notlike '*proxy*') { throw }
        return
    }
    throw "$Label passed Confirm-Checksum"
}

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

    # Fetch through the real Get-ChecksumText, but only from the loopback fixture.
    $good = Get-ChecksumText -Url ($Base + '/releases/latest/download/checksums.txt')
    if ($script:ChecksumNotPublished) { throw 'fixture checksums.txt was reported as not published' }
    if (-not (Test-ArchEntries $good)) {
        throw ("fixture checksums.txt is missing a Windows entry:" + [char]10 + $good)
    }
    if ($good -notmatch ('(?m)^' + $Amd64 + '\s+deeprouter-windows-amd64\.exe\s*$')) { throw "amd64 hash not parsed: $good" }
    if ($good -notmatch ('(?m)^' + $Arm64 + '\s+deeprouter-windows-arm64\.exe\s*$')) { throw "arm64 hash not parsed: $good" }
    try {
        Confirm-Checksum -File $dummy -Name 'deeprouter-windows-amd64.exe' -Sums $good
        throw 'expected the fixture hash to reject the dummy file'
    } catch {
        if ($_.Exception.Message -ne 'checksum mismatch for deeprouter-windows-amd64.exe') { throw }
    }

    # A missing asset is "not published", never a parsed checksum list.
    $missing = Get-ChecksumText -Url ($Base + '/missing/checksums.txt')
    if (-not $script:ChecksumNotPublished) { throw '404 was not reported as not published' }
    if ($missing -ne '' -or (Test-ArchEntries $missing)) { throw "404 returned checksum text: $missing" }
    Assert-BadSums $missing '404'

    $html = Get-ChecksumText -Url ($Base + '/html/checksums.txt')
    if ($script:ChecksumNotPublished) { throw 'HTML page was reported as not published' }
    if (Test-ArchEntries $html) { throw "HTML page passed the arch checks: $html" }
    Assert-BadSums $html 'HTML'

    $empty = $null
    try { $empty = Get-ChecksumText -Url ($Base + '/empty/checksums.txt') } catch { $empty = $null }
    if ($script:ChecksumNotPublished) { throw 'empty body was reported as not published' }
    if (Test-ArchEntries $empty) { throw 'empty body passed the arch checks' }
    Assert-BadSums $empty 'empty'

    $broken = $null
    try {
        $broken = Get-ChecksumText -Url ($Base + '/broken/checksums.txt')
    } catch {
        $broken = 'threw'
    }
    if ($broken -ne 'threw') { throw "HTTP 500 did not throw: $broken" }
    if ($script:ChecksumNotPublished) { throw 'HTTP 500 was reported as not published' }

    # The tripwire must sit in front of the real download path.
    try {
        [void](Get-ChecksumText -Url 'https://deeprouter-leak.example/checksums.txt')
        throw 'public checksum URL was not blocked'
    } catch {
        if ($_.Exception.Message -notlike 'public request blocked:*') { throw }
    }
    if ($script:LoopbackRequests -ne 5) { throw "expected 5 loopback downloads, got $($script:LoopbackRequests)" }
    Write-Output 'checksum parsing ok'
} finally {
    if (Test-Path $dummy) { Remove-Item -Force $dummy }
}
`

const installPS1PublishedDriver = `
param([Parameter(Mandatory = $true)][string]$Installer, [Parameter(Mandatory = $true)][string]$Url)
$ErrorActionPreference = 'Stop'
. $Installer
$sums = Get-ChecksumText -Url $Url
if ($script:ChecksumNotPublished) { throw "no published checksums.txt at $Url" }
foreach ($name in @('deeprouter-windows-amd64.exe', 'deeprouter-windows-arm64.exe')) {
    if ($sums -notmatch ('(?m)^[0-9a-fA-F]{64}\s+\*?' + [regex]::Escape($name) + '\s*$')) {
        throw ("published checksums.txt has no $name entry:" + [char]10 + $sums)
    }
}
Write-Output 'published checksums ok'
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
