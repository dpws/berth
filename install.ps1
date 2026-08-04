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

.PARAMETER NoTerminalFix
    Skip the offer to teach Windows Terminal that shift+enter means a new line.

.PARAMETER Yes
    Answer yes to that offer without asking, for an unattended install.

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
    [switch]$NoTerminalFix,
    [switch]$Yes,
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

# ------------------------------------------------- windows terminal

# Windows Terminal cannot tell shift+enter from enter on its own: it does not
# speak the keyboard protocol that would carry the difference, so an agent on
# the other end of the SSH connection receives a plain return and sends the
# half-written prompt. Nothing on the remote machine can recover a distinction
# that was never transmitted.
#
# What it can do is be told to send something else for that key. An escape
# followed by a return is what Claude Code and Codex both read as a line break,
# and it is what berth sends for the keys it can already see.
#
# alt+enter is not an answer here, whatever it is elsewhere: Windows Terminal
# keeps that one for going fullscreen.
# Built by interpolation rather than by adding a [char] to a string: in
# PowerShell the left operand decides the arithmetic, and [char] + string tries
# to make a char of the string rather than concatenating.
$wtNewlineInput = "$([char]27)`r"
$wtNewlineKeys  = "shift+enter"

function Get-TerminalSettings {
    # Store, Preview and unpackaged installs each keep it somewhere different,
    # and someone can have more than one.
    $candidates = @(
        "$env:LOCALAPPDATA\Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState\settings.json",
        "$env:LOCALAPPDATA\Packages\Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe\LocalState\settings.json",
        "$env:LOCALAPPDATA\Microsoft\Windows Terminal\settings.json"
    )
    $candidates | Where-Object { Test-Path $_ }
}

# Remove-JsonComments takes the // and /* */ out of a settings file so it can be
# parsed. Windows Terminal writes comments into the file it ships, and neither
# PowerShell 5.1 nor 7 will parse JSON containing them.
#
# It steps through the text a character at a time rather than using a regular
# expression, because a // inside a string - "https://..." is in every profile
# that has an icon or a source - is not a comment, and a regular expression that
# knew the difference would be harder to be sure of than this loop.
function Remove-JsonComments {
    param([string]$Text)

    $out      = New-Object System.Text.StringBuilder
    $inString = $false
    $escaped  = $false
    $i        = 0

    while ($i -lt $Text.Length) {
        $c = $Text[$i]

        if ($inString) {
            [void]$out.Append($c)
            if ($escaped)       { $escaped = $false }
            elseif ($c -eq '\') { $escaped = $true }
            elseif ($c -eq '"') { $inString = $false }
            $i++
            continue
        }

        if ($c -eq '"') { $inString = $true; [void]$out.Append($c); $i++; continue }

        if ($c -eq '/' -and $i + 1 -lt $Text.Length) {
            if ($Text[$i + 1] -eq '/') {
                while ($i -lt $Text.Length -and $Text[$i] -ne "`n") { $i++ }
                continue
            }
            if ($Text[$i + 1] -eq '*') {
                $i += 2
                while ($i + 1 -lt $Text.Length -and -not ($Text[$i] -eq '*' -and $Text[$i + 1] -eq '/')) { $i++ }
                $i += 2
                continue
            }
        }

        [void]$out.Append($c)
        $i++
    }
    $out.ToString()
}

function Set-TerminalNewline {
    param([string]$Path)

    $raw = Get-Content $Path -Raw
    try {
        $settings = Remove-JsonComments $raw | ConvertFrom-Json
    } catch {
        Write-Warn "could not read $Path as JSON, leaving it alone"
        return $false
    }

    # Newer Windows Terminal calls the list "actions"; older ones "keybindings".
    # Whichever this file already uses is the one to add to, so the file is not
    # left saying the same thing in two places.
    $listName = if ($null -ne $settings.actions) { "actions" }
                elseif ($null -ne $settings.keybindings) { "keybindings" }
                else { "actions" }

    $existing = @()
    if ($null -ne $settings.$listName) { $existing = @($settings.$listName) }

    foreach ($entry in $existing) {
        if ($entry.keys -eq $wtNewlineKeys) {
            # Something is already bound to it. Replacing someone's own binding
            # is not this script's call, so say so and leave it.
            Write-Warn "$wtNewlineKeys is already bound in $Path, leaving it alone"
            return $false
        }
    }

    $binding = [pscustomobject]@{
        command = [pscustomobject]@{ action = "sendInput"; input = $wtNewlineInput }
        keys    = $wtNewlineKeys
    }

    Copy-Item $Path "$Path.berth-bak" -Force
    $settings | Add-Member -NotePropertyName $listName -NotePropertyValue (@($existing) + $binding) -Force

    # Depth has to be generous: profiles, schemes and their nested objects go
    # several levels down, and ConvertTo-Json silently truncates past its
    # default of 2, which would throw away most of the file.
    $settings | ConvertTo-Json -Depth 100 | Set-Content $Path -Encoding UTF8
    Write-Ok "added $wtNewlineKeys to $Path"
    return $true
}


# Remove-TerminalNewline takes out only the binding this script added: same key,
# same input. A binding someone wrote themselves for that key, or changed the
# input of, is theirs and is left where it is.
function Remove-TerminalNewline {
    param([string]$Path)

    $raw = Get-Content $Path -Raw
    try {
        $settings = Remove-JsonComments $raw | ConvertFrom-Json
    } catch {
        return $false
    }

    $listName = if ($null -ne $settings.actions) { "actions" }
                elseif ($null -ne $settings.keybindings) { "keybindings" }
                else { return $false }

    $kept = @(@($settings.$listName) | Where-Object {
        -not ($_.keys -eq $wtNewlineKeys -and $_.command.action -eq "sendInput" -and $_.command.input -eq $wtNewlineInput)
    })
    if ($kept.Count -eq @($settings.$listName).Count) { return $false }

    Copy-Item $Path "$Path.berth-bak" -Force
    $settings | Add-Member -NotePropertyName $listName -NotePropertyValue $kept -Force
    $settings | ConvertTo-Json -Depth 100 | Set-Content $Path -Encoding UTF8
    Write-Ok "removed $wtNewlineKeys from $Path"
    return $true
}

if ($Uninstall) {
    Write-Step "Removing berth-clipd"
    Stop-Agent
    if (Test-Path $shortcutPath) { Remove-Item $shortcutPath -Force; Write-Ok "removed the startup entry" }
    if (Test-Path $InstallDir)   { Remove-Item $InstallDir -Recurse -Force; Write-Ok "removed $InstallDir" }

    # The key binding is the one thing this script leaves outside its own
    # directory, so uninstalling has to take it back out rather than claim
    # nothing else was touched.
    $removed = 0
    foreach ($f in @(Get-TerminalSettings)) {
        if (Remove-TerminalNewline $f) { $removed++ }
    }
    if ($removed -eq 0) { Write-Host "`nDone. Nothing else was touched." -ForegroundColor Green }
    else { Write-Host "`nDone. Restart Windows Terminal for the key to go back to what it was." -ForegroundColor Green }
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



if (-not $NoTerminalFix) {
    $found = @(Get-TerminalSettings)
    if ($found.Count -gt 0) {
        Write-Step "Windows Terminal"
        Write-Host "    berth can teach Windows Terminal that shift+enter means a new line"
        Write-Host "    rather than sending your half-written prompt. Without it, use ctrl+j."
        Write-Host "    Files that would change, each copied to <file>.berth-bak first:"
        foreach ($f in $found) { Write-Host "      $f" }
        Write-Warn "comments and formatting in those files are not preserved"

        # An unattended install has nobody to ask. Read-Host would sit there
        # waiting for a console that is not listening, so a run with no
        # interactive host takes the answer it would take from silence: no.
        $answer = "y"
        if (-not $Yes) {
            if ([Environment]::UserInteractive) {
                $answer = Read-Host "    Do it? [y/N]"
            } else {
                $answer = "n"
                Write-Warn "not running interactively, so leaving it; pass -Yes to do it anyway"
            }
        }
        if ($answer -match '^(y|yes)$') {
            foreach ($f in $found) { [void](Set-TerminalNewline $f) }
            Write-Ok "restart Windows Terminal for it to take effect"
        } else {
            Write-Ok "left alone; ctrl+j gives you a new line without changing anything"
        }
    }
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
