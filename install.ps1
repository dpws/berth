<#
.SYNOPSIS
    Installs berth-clipd, the clipboard agent, on Windows.

.DESCRIPTION
    irm https://raw.githubusercontent.com/dpws/berth/main/install.ps1 | iex

    berth itself needs a pty and tmux, so it does not run on Windows - install
    it on the machine you SSH into. What belongs here is berth-clipd, which
    serves this machine's clipboard so ctrl+y over there pastes a screenshot
    from here.

    Downloads the latest release, installs to your local app data, starts it at
    login, and checks that it answers. No administrator rights are needed and
    nothing is written outside your user profile.

    To pass options through a piped install, wrap it in a script block:

        & ([scriptblock]::Create((irm https://raw.githubusercontent.com/dpws/berth/main/install.ps1))) -Port 9000

.PARAMETER Version
    Release tag to install. Defaults to the latest release.

.PARAMETER Source
    Install from a local .exe instead of downloading.

.PARAMETER Port
    Port to listen on. Must match clip_agent_url in berth's config.

.PARAMETER Token
    Shared secret, required if Address is not loopback. Set the same value as
    clip_agent_token in berth's config.

.PARAMETER Address
    Listen address. Leave as loopback and forward the port over SSH; on a LAN
    or VPN address this serves your clipboard to anything that can reach it.

.PARAMETER NoStartup
    Install without running it at login.

.PARAMETER Uninstall
    Remove the agent and its startup entry.
#>
#Requires -Version 5.1
[CmdletBinding()]
param(
    [string]$Version    = "latest",
    [string]$Source     = "",
    [string]$InstallDir = "$env:LOCALAPPDATA\berth",
    [string]$Address    = "127.0.0.1",
    [int]$Port          = 8377,
    [string]$Token      = "",
    [switch]$NoStartup,
    [switch]$Uninstall
)

$ErrorActionPreference = "Stop"
# PowerShell 5.1 still defaults to TLS 1.0, which GitHub refuses.
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo         = "dpws/berth"
$exeName      = "berth-clipd.exe"
$silentName   = "berth-clipd-silent.exe"
$startupDir   = [Environment]::GetFolderPath("Startup")
$shortcutPath = Join-Path $startupDir "berth-clipd.lnk"

function Write-Step { param([string]$Message) Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Ok   { param([string]$Message) Write-Host "    $Message" -ForegroundColor Green }
function Write-Warn { param([string]$Message) Write-Host "    $Message" -ForegroundColor Yellow }

function Stop-Agent {
    # Stop-Process only asks; Windows keeps the executable locked until the
    # process has actually gone, and an install that copied straight after
    # would fail with "being used by another process".
    Get-Process -Name "berth-clipd", "berth-clipd-silent" -ErrorAction SilentlyContinue |
        ForEach-Object {
            Write-Ok "stopping pid $($_.Id)"
            Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
            Wait-Process -Id $_.Id -Timeout 10 -ErrorAction SilentlyContinue
        }
}

function Copy-Agent {
    param([string]$Source, [string]$Target)

    # Even once the process is gone the file can stay locked for a moment -
    # antivirus and search indexers both do this - so a few retries turn a
    # failed install into a slightly slower one.
    for ($attempt = 1; $attempt -le 10; $attempt++) {
        try {
            Copy-Item $Source $Target -Force -ErrorAction Stop
            return
        } catch {
            # Deliberately not catching [System.IO.IOException] by type:
            # -ErrorAction Stop wraps cmdlet errors, and the wrapper does not
            # reliably match in Windows PowerShell 5.1. The real error is
            # carried into the final message so a genuine failure - no
            # permission, no disk - is not disguised as a lock.
            if ($attempt -eq 10) {
                throw "could not write $Target after several attempts: " +
                      "$($_.Exception.Message) If berth-clipd is still " +
                      "running, close it and run this again."
            }
            Start-Sleep -Milliseconds 300
        }
    }
}

if ($Uninstall) {
    Write-Step "Removing berth-clipd"
    Stop-Agent
    if (Test-Path $shortcutPath) { Remove-Item $shortcutPath -Force; Write-Ok "removed the startup entry" }
    if (Test-Path $InstallDir)   { Remove-Item $InstallDir -Recurse -Force; Write-Ok "removed $InstallDir" }
    Write-Host "`nDone. Nothing else was touched." -ForegroundColor Green
    return
}

# ---------------------------------------------------------------- platform

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default { throw "unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

Write-Step "Installing berth-clipd"
Write-Ok "platform: windows/$arch"

# ---------------------------------------------------------------- obtain

$temp = Join-Path ([IO.Path]::GetTempPath()) ("berth-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $temp | Out-Null

try {
    if (-not $Source) {
        # A script run through `irm | iex` has no path of its own, so only look
        # beside it when there is a beside.
        if ($PSCommandPath) {
            foreach ($candidate in @($silentName, $exeName)) {
                $try = Join-Path (Split-Path -Parent $PSCommandPath) $candidate
                if (Test-Path $try) { $Source = $try; break }
            }
        }
    }

    if (-not $Source) {
        if ($Version -eq "latest") {
            Write-Step "Finding the latest release"
            try {
                $release = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest" -UseBasicParsing
                $Version = $release.tag_name
            } catch {
                throw "no published release found for $repo. Build it from source instead: make clipd-windows"
            }
        }
        Write-Ok "version:  $Version"

        $zipName = "berth-clipd_${Version}_windows_${arch}.zip"
        $baseUrl = "https://github.com/$repo/releases/download/$Version"
        $zipPath = Join-Path $temp $zipName

        Write-Step "Downloading"
        try {
            Invoke-WebRequest "$baseUrl/$zipName" -OutFile $zipPath -UseBasicParsing
        } catch {
            throw "no build for windows/$arch in $Version"
        }
        Write-Ok $zipName

        Write-Step "Verifying"
        try {
            $sumsPath = Join-Path $temp "checksums.txt"
            Invoke-WebRequest "$baseUrl/checksums.txt" -OutFile $sumsPath -UseBasicParsing
            $line = Select-String -Path $sumsPath -Pattern ([regex]::Escape($zipName)) | Select-Object -First 1
            if (-not $line) {
                Write-Warn "$zipName is not listed in checksums.txt"
            } else {
                $want = ($line.Line -split '\s+')[0]
                $got  = (Get-FileHash $zipPath -Algorithm SHA256).Hash.ToLower()
                if ($want.ToLower() -ne $got) {
                    throw "checksum mismatch for ${zipName}: expected $want, got $got. Do not use this download."
                }
                Write-Ok "sha256 ok"
            }
        } catch [System.Net.WebException] {
            Write-Warn "no checksums.txt published for $Version"
        }

        Expand-Archive -Path $zipPath -DestinationPath $temp -Force
        foreach ($candidate in @($silentName, $exeName)) {
            $try = Join-Path $temp $candidate
            if (Test-Path $try) { $Source = $try; break }
        }
    }

    if (-not $Source -or -not (Test-Path $Source)) { throw "could not find $exeName to install" }

    # ------------------------------------------------------------ safety

    if ($Address -notin @("127.0.0.1", "localhost", "::1") -and -not $Token) {
        throw @"
Refusing to install listening on $Address without a token.

That address is reachable beyond this machine, and the agent serves your
clipboard to anyone who can connect. Either keep the default loopback address
and forward the port over SSH, or pass -Token <secret> and set the same value
as clip_agent_token in berth's config.
"@
    }

    # ------------------------------------------------------------ install

    Write-Step "Installing to $InstallDir"
    Stop-Agent
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    $target = Join-Path $InstallDir $exeName
    Copy-Agent -Source $Source -Target $target
    Write-Ok "installed $target"

    $agentArgs = "-addr $Address`:$Port"
    if ($Token) { $agentArgs += " -token $Token" }

    if (-not $NoStartup) {
        Write-Step "Running at login"
        $shell    = New-Object -ComObject WScript.Shell
        $shortcut = $shell.CreateShortcut($shortcutPath)
        $shortcut.TargetPath       = $target
        $shortcut.Arguments        = $agentArgs
        $shortcut.WorkingDirectory = $InstallDir
        $shortcut.Description      = "Serves the clipboard to a remote berth"
        $shortcut.Save()
        Write-Ok "added $shortcutPath"
    }

    Write-Step "Starting it now"
    Start-Process -FilePath $target -ArgumentList $agentArgs -WindowStyle Hidden
    Start-Sleep -Milliseconds 800

    # ------------------------------------------------------------ verify

    Write-Step "Checking that it answers"
    try {
        $health = Invoke-WebRequest "http://${Address}:$Port/health" -UseBasicParsing -TimeoutSec 5
        Write-Ok $health.Content.Trim()
    } catch {
        throw "the agent did not answer on http://${Address}:$Port/health. Run it in a console to see why:`n    & `"$target`" $agentArgs"
    }
} finally {
    Remove-Item $temp -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host @"

Installed. On the machine running berth, connect with the port forwarded:

    ssh -R ${Port}:localhost:$Port you@yourbox

or add this to your ~/.ssh/config here so every connection carries it:

    Host yourbox
      HostName <address>
      RemoteForward $Port localhost:$Port

Then press ctrl+y in berth. Copy an image first - Win+Shift+S, or Ctrl+C on a
file in Explorer, both work.

Uninstall:
    & ([scriptblock]::Create((irm https://raw.githubusercontent.com/$repo/main/install.ps1))) -Uninstall
"@ -ForegroundColor Green
