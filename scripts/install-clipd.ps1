<#
.SYNOPSIS
    Installs berth-clipd, which serves this machine's clipboard to a
    berth running on another machine.

.DESCRIPTION
    Copies the binary into your local app data, optionally starts it at login,
    and checks that it answers. Nothing is written outside your user profile
    and no administrator rights are needed.

    The agent listens on loopback only. To reach it from the machine running
    berth, forward the port when you connect:

        ssh -R 8377:localhost:8377 you@yourbox

.PARAMETER Source
    Path to berth-clipd.exe. Defaults to the folder this script is in.

.PARAMETER Port
    Port to listen on. Must match clip_agent_url in berth's config.

.PARAMETER Token
    Shared secret. Only needed if you change -Address away from loopback;
    set the same value as clip_agent_token in berth's config.

.PARAMETER Address
    Listen address. Leave as loopback unless you know what you are doing:
    on a LAN or VPN address this hands your clipboard to anything that can
    reach the port, so a Token is then required.

.PARAMETER NoStartup
    Install without running it at login.

.PARAMETER Uninstall
    Remove the agent and its startup entry.

.EXAMPLE
    .\install-clipd.ps1

.EXAMPLE
    .\install-clipd.ps1 -Port 9000 -NoStartup
#>
#Requires -Version 5.1
[CmdletBinding()]
param(
    [string]$Source     = "",
    [string]$InstallDir = "$env:LOCALAPPDATA\berth",
    [string]$Address    = "127.0.0.1",
    [int]$Port          = 8377,
    [string]$Token      = "",
    [switch]$NoStartup,
    [switch]$Uninstall
)

$ErrorActionPreference = "Stop"

$exeName      = "berth-clipd.exe"
$silentName   = "berth-clipd-silent.exe"
$startupDir   = [Environment]::GetFolderPath("Startup")
$shortcutPath = Join-Path $startupDir "berth-clipd.lnk"

function Write-Step { param([string]$Message) Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Ok   { param([string]$Message) Write-Host "    $Message" -ForegroundColor Green }
function Write-Warn { param([string]$Message) Write-Host "    $Message" -ForegroundColor Yellow }

function Stop-Agent {
    Get-Process -Name "berth-clipd", "berth-clipd-silent" -ErrorAction SilentlyContinue |
        ForEach-Object {
            Write-Ok "stopping pid $($_.Id)"
            Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
        }
}

if ($Uninstall) {
    Write-Step "Removing berth-clipd"
    Stop-Agent
    if (Test-Path $shortcutPath) {
        Remove-Item $shortcutPath -Force
        Write-Ok "removed the startup entry"
    }
    if (Test-Path $InstallDir) {
        Remove-Item $InstallDir -Recurse -Force
        Write-Ok "removed $InstallDir"
    }
    Write-Host "`nDone. Nothing else was touched." -ForegroundColor Green
    return
}

# ---------------------------------------------------------------- locate

if (-not $Source) {
    $scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
    foreach ($candidate in @($silentName, $exeName)) {
        $try = Join-Path $scriptDir $candidate
        if (Test-Path $try) { $Source = $try; break }
    }
}
if (-not $Source -or -not (Test-Path $Source)) {
    Write-Error @"
Cannot find berth-clipd.exe.

Build it on the machine that has the berth source:

    make clipd-windows-gui

then copy dist\berth-clipd-silent.exe next to this script, or pass
-Source <path>.
"@
}

# ---------------------------------------------------------------- safety

if ($Address -ne "127.0.0.1" -and $Address -ne "localhost" -and $Address -ne "::1") {
    if (-not $Token) {
        Write-Error @"
Refusing to install listening on $Address without a token.

That address is reachable beyond this machine, and the agent serves your
clipboard to anyone who can connect. Either keep the default loopback address
and forward the port over SSH, or pass -Token <secret> and set the same value
as clip_agent_token in berth's config.
"@
    }
    Write-Warn "listening on $Address - make sure your firewall expects this"
}

# ---------------------------------------------------------------- install

Write-Step "Installing to $InstallDir"
Stop-Agent
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

$target = Join-Path $InstallDir $exeName
Copy-Item $Source $target -Force
Write-Ok "copied $(Split-Path -Leaf $Source)"

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

# ---------------------------------------------------------------- verify

Write-Step "Checking that it answers"
$healthUrl = "http://${Address}:$Port/health"
try {
    $response = Invoke-WebRequest -Uri $healthUrl -UseBasicParsing -TimeoutSec 5
    Write-Ok $response.Content.Trim()
} catch {
    Write-Error @"
The agent did not answer on $healthUrl.

Try running it in a console to see why:

    & "$target" $agentArgs
"@
}

$imageUrl = "http://${Address}:$Port/image"
try {
    $headers = @{}
    if ($Token) { $headers["X-Berth-Token"] = $Token }
    $image = Invoke-WebRequest -Uri $imageUrl -UseBasicParsing -TimeoutSec 20 -Headers $headers
    if ($image.StatusCode -eq 204) {
        Write-Ok "clipboard reachable, currently holds no image"
    } else {
        Write-Ok "clipboard reachable, serving $($image.RawContentLength) bytes"
    }
} catch {
    Write-Warn "clipboard read failed: $($_.Exception.Message)"
    Write-Warn "copy an image and retry $imageUrl to see the real error"
}

# ---------------------------------------------------------------- next steps

Write-Host @"

Installed. On the machine running berth, connect with the port forwarded:

    ssh -R ${Port}:localhost:$Port you@yourbox

or add this to your ~/.ssh/config here so every connection carries it:

    Host yourbox
      HostName <address>
      RemoteForward $Port localhost:$Port

Then press ctrl+y in berth. Copy an image first - Win+Shift+S, or
Ctrl+C on a file in Explorer, both work.

Uninstall with:  .\install-clipd.ps1 -Uninstall
"@ -ForegroundColor Green
