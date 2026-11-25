# Copyright 2026 Contextual, Inc. https://agentrq.com
# This notice may not be modified or removed.
# SPDX-License-Identifier: Apache-2.0
#
# AgentRQ desktop installer and updater for Windows.
#
#   irm https://agentrq.com/install.ps1 | iex
#
# The same command installs and updates: it resolves the newest GitHub Release,
# downloads the setup program, checks it against the SHA-256 digest GitHub
# records for the asset, and runs it silently. Both setup programs install for
# the current user, so no administrator rights are needed.
#
# By default this installs the Electron app from agentrq/agentrq; -Tauri
# installs the Tauri app from agentrq/agentrq-desktop instead.
#
# Piped in with `irm | iex`, this script cannot see command-line flags. Set the
# equivalent environment variable first:
#
#   $env:AGENTRQ_TAURI = '1'; irm https://agentrq.com/install.ps1 | iex
#
# Run as a file, ordinary parameters work:
#
#   .\install.ps1 -Tauri -Version v0.19.4 -Quit -Force
#
# Flags / environment variables:
#
#   -Tauri           / AGENTRQ_TAURI     install the Tauri app instead of the Electron one
#   -Version <tag>   / AGENTRQ_VERSION   install a specific release, e.g. v0.19.4
#   -Quit            / AGENTRQ_QUIT      close the running app instead of refusing
#   -Force           / AGENTRQ_FORCE     reinstall even when the installed version already matches
#   -Help
#
# GITHUB_TOKEN, if set, is used for the API call.

param(
    [switch]$Tauri = [bool]$env:AGENTRQ_TAURI,
    [string]$Version = $env:AGENTRQ_VERSION,
    [switch]$Quit = [bool]$env:AGENTRQ_QUIT,
    [switch]$Force = [bool]$env:AGENTRQ_FORCE,
    [Alias('h')]
    [switch]$Help
)

$ErrorActionPreference = 'Stop'
# Invoke-WebRequest's progress bar slows a large download by an order of
# magnitude on Windows PowerShell 5.1.
$ProgressPreference = 'SilentlyContinue'

# A throw, never `exit`: under `irm | iex` this runs in the caller's own
# session, and `exit` would close their PowerShell window.
function Die([string]$Message) { throw "error: $Message" }
function Say([string]$Message) { Write-Host $Message }
function Note([string]$Message) { Write-Host "  $Message" }

function Install-AgentRQDesktop {
    if ($Help) {
        Write-Host @'
AgentRQ desktop installer and updater, for Windows.

  install.ps1 [-Tauri] [-Version <tag>] [-Quit] [-Force]

  -Tauri           install the Tauri app instead of the Electron one
  -Version <tag>   install a specific release, e.g. v0.19.4
  -Quit            close the running app instead of refusing
  -Force           reinstall even when the installed version already matches
  -Help            this

Environment: AGENTRQ_TAURI, AGENTRQ_VERSION, AGENTRQ_QUIT, AGENTRQ_FORCE, GITHUB_TOKEN

Piped in (irm | iex) does not accept flags directly -- set the environment
variable first:
  $env:AGENTRQ_TAURI = '1'; irm https://agentrq.com/install.ps1 | iex
'@
        return
    }

    if ($Tauri) {
        # The Tauri build is released from its own repository, one setup
        # program per architecture: AgentRQ-Tauri-<version>-<arch>.exe.
        $repo = 'agentrq/agentrq-desktop'
        $product = 'AgentRQ Tauri'
        $processName = 'agentrq-tauri'
        $updateCmd = "`$env:AGENTRQ_TAURI = '1'; irm https://agentrq.com/install.ps1 | iex"
        # Windows on Arm runs the x64 build under emulation until an arm64 one ships.
        switch ($env:PROCESSOR_ARCHITECTURE) {
            'AMD64' { $patterns = @('-x64\.exe$') }
            'ARM64' { $patterns = @('-arm64\.exe$', '-x64\.exe$') }
            default { Die "unsupported architecture: $($env:PROCESSOR_ARCHITECTURE). See https://github.com/$repo/releases" }
        }
        # Its uninstall entry is named after the product alone.
        $isEntry = { param($name) $name -eq 'AgentRQ Tauri' }
    }
    else {
        # One setup program carries both x64 and arm64: AgentRQ-Setup-<version>.exe.
        $repo = 'agentrq/agentrq'
        $product = 'AgentRQ'
        $processName = 'AgentRQ'
        $updateCmd = 'irm https://agentrq.com/install.ps1 | iex'
        $patterns = @('^AgentRQ[-. ]Setup[-. ].*\.exe$')
        # electron-builder names its uninstall entry "<product> <version>".
        $isEntry = { param($name) $name -eq 'AgentRQ' -or $name -match '^AgentRQ [0-9]' }
    }
    $api = "https://api.github.com/repos/$repo/releases"
    $releasesPage = "https://github.com/$repo/releases"

    # The setup program's own uninstall entry, under the current user.
    function Get-InstalledVersion {
        $keys = Get-ChildItem 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall' -ErrorAction SilentlyContinue
        foreach ($key in $keys) {
            $entry = Get-ItemProperty $key.PSPath -ErrorAction SilentlyContinue
            if ($entry -and $entry.DisplayName -and (& $isEntry $entry.DisplayName)) { return [string]$entry.DisplayVersion }
        }
        return $null
    }

    # ----------------------------------------------------------- the release

    $headers = @{ 'Accept' = 'application/vnd.github+json' }
    if ($env:GITHUB_TOKEN) { $headers['Authorization'] = "Bearer $($env:GITHUB_TOKEN)" }

    if ($Version) {
        $tagName = if ($Version.StartsWith('v')) { $Version } else { "v$Version" }
        $apiUrl = "$api/tags/$tagName"
    }
    else {
        $apiUrl = "$api/latest"
    }

    Say "Looking up the $product release..."
    try {
        $release = Invoke-RestMethod -Uri $apiUrl -Headers $headers -UseBasicParsing
    }
    catch {
        Die "could not fetch $apiUrl -- no such release, or GitHub is unreachable"
    }
    $tag = $release.tag_name
    if (-not $tag) { Die "no published release found at $apiUrl" }
    $version = $tag -replace '^v', ''

    $asset = $null
    foreach ($pattern in $patterns) {
        $asset = $release.assets | Where-Object { $_.name -match $pattern } | Select-Object -First 1
        if ($asset) { break }
    }
    if (-not $asset) { Die "release $tag has no Windows setup program for $product. See $releasesPage" }
    $filename = $asset.name

    # --------------------------------------------------- already up to date?

    if (-not $Force) {
        $installed = Get-InstalledVersion
        if ($installed -and $installed -eq $version) {
            Say "$product $version is already installed. Nothing to do."
            Note 'Reinstall anyway with -Force.'
            return
        }
    }

    # The setup program cannot replace files a running app holds open.
    $running = Get-Process -Name $processName -ErrorAction SilentlyContinue
    if ($running) {
        if (-not $Quit) {
            Die "$product is running. Quit it and run this again, or set `$env:AGENTRQ_QUIT = '1' (or pass -Quit) to have the installer close it."
        }
        Say "Quitting $product..."
        $running | Stop-Process -Force -ErrorAction SilentlyContinue
        $running | Wait-Process -Timeout 15 -ErrorAction SilentlyContinue
        if (Get-Process -Name $processName -ErrorAction SilentlyContinue) {
            Die "$product did not quit. Close it manually and run this again."
        }
    }

    # ------------------------------------------------------------- download

    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "agentrq-install-$([System.IO.Path]::GetRandomFileName())"
    New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null

    try {
        $downloadPath = Join-Path $tmpDir $filename
        Say "Downloading $filename..."
        try {
            Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $downloadPath -UseBasicParsing
        }
        catch {
            Die "download failed: $($asset.browser_download_url)"
        }

        # GitHub records each asset's SHA-256 as "sha256:<hex>".
        if ($asset.digest -and $asset.digest -match '^sha256:([0-9a-fA-F]{64})$') {
            $expected = $Matches[1].ToLower()
            $actual = (Get-FileHash -Path $downloadPath -Algorithm SHA256).Hash.ToLower()
            if ($actual -ne $expected) {
                Die "checksum mismatch for $filename -- the download is corrupt or has been tampered with. Nothing was installed."
            }
            Note 'Checksum verified.'
        }
        else {
            Note "GitHub lists no checksum for $filename; skipping verification."
        }

        # ------------------------------------------------------------ install

        Say "Installing $product $version..."
        $setup = Start-Process -FilePath $downloadPath -ArgumentList '/S' -Wait -PassThru
        if ($setup.ExitCode -ne 0) {
            Die "the setup program exited with code $($setup.ExitCode)"
        }
    }
    finally {
        Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
    }

    $installed = Get-InstalledVersion
    if ($installed -ne $version) {
        Die "the setup program finished, but $product $version is not listed as installed (found: $installed)"
    }

    Say ""
    Say "$product $version installed for this user."
    Note "Launch it from the Start menu: $product"
    Say ""
    Say 'Update later by running the same command:'
    Say "  $updateCmd"
}

Install-AgentRQDesktop
