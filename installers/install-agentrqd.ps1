# Copyright 2026 Contextual, Inc. https://agentrq.com
# This notice may not be modified or removed.
# SPDX-License-Identifier: Apache-2.0
#
# AgentRQ daemon (agentrqd) installer and updater for Windows.
#
#   irm https://agentrq.com/install-agentrqd.ps1 | iex
#
# It resolves the newest GitHub Release, downloads the archive matching this
# machine, verifies it against the published SHA-256 checksums, and puts
# agentrqd.exe on your PATH. Running it again updates in place.
#
# It installs and nothing else. It does not enrol the machine, does not start
# anything, and does not install a service -- enrolling is where you decide
# which account may run agents here, and that is a decision to make deliberately
# with a code in front of you, not a side effect of an install.
#
# Why the checksum is mandatory here, unlike the desktop installer:
#
#   This binary takes commands from an AgentRQ account and runs them on this
#   machine. That is the whole point of it, and it is also why a download this
#   script cannot vouch for must never reach your disk. Every release publishes
#   checksums.txt, so a missing or unverifiable checksum means something is
#   wrong -- with the release or with the network -- and the right answer is to
#   stop. There is deliberately no flag to skip it.
#
# It never runs the daemon, and never installs it as Administrator. agentrqd
# refuses to run as Administrator on purpose: an agent should be able to do
# what you can do and no more. An installer that reached for elevation to work
# around that would be removing the only thing keeping it true.
#
# Piped in with `irm | iex`, this script cannot see command-line flags -- there
# is nothing to attach them to. Set the equivalent environment variable first:
#
#   $env:AGENTRQD_VERSION = '0.7.1'; irm https://agentrq.com/install-agentrqd.ps1 | iex
#
# Run as a file, ordinary parameters work:
#
#   .\install-agentrqd.ps1 -Version 0.7.1 -Dir C:\tools\agentrqd -Force
#
# Flags / environment variables:
#
#   -Version <tag>   / AGENTRQD_VERSION       install a specific release, e.g. 0.7.1 or v0.7.1
#   -Dir <path>      / AGENTRQD_INSTALL_DIR   install location (default $env:LOCALAPPDATA\Programs)
#   -Force           / AGENTRQD_FORCE         reinstall even when the installed version already matches
#   -Help                                     this
#
# GITHUB_TOKEN, if set, is used for the API call -- only useful on a shared IP
# that has exhausted the unauthenticated rate limit.

param(
    [string]$Version = $env:AGENTRQD_VERSION,
    [string]$Dir = $env:AGENTRQD_INSTALL_DIR,
    [switch]$Force = [bool]$env:AGENTRQD_FORCE,
    [Alias('h')]
    [switch]$Help
)

$ErrorActionPreference = 'Stop'

$Repo = 'agentrq/agentrq'
$Api = "https://api.github.com/repos/$Repo/releases"
$ReleasesPage = "https://github.com/$Repo/releases"
$Docs = 'https://agentrq.com/docs/daemon'

function Say([string]$Message) { Write-Host $Message }
function Note([string]$Message) { Write-Host "  $Message" }
function Die([string]$Message) {
    Write-Host "error: $Message" -ForegroundColor Red
    exit 1
}

if ($Help) {
    Write-Host @'
AgentRQ daemon installer and updater.

  install-agentrqd.ps1 [-Version <tag>] [-Dir <path>] [-Force]

Installs agentrqd on this machine and verifies it against the checksums
published with the release. It does not enrol the machine or start anything;
it prints what to run next.

  -Version <tag>   install a specific release, e.g. 0.7.1 or v0.7.1
  -Dir <path>      install location (default $env:LOCALAPPDATA\Programs)
  -Force           reinstall even when the installed version already matches
  -Help            this

Environment: AGENTRQD_VERSION, AGENTRQD_INSTALL_DIR, AGENTRQD_FORCE, GITHUB_TOKEN

Piped in (irm | iex) does not accept flags directly -- set the environment
variable first:
  $env:AGENTRQD_VERSION = '0.7.1'; irm https://agentrq.com/install-agentrqd.ps1 | iex
'@
    exit 0
}

# The daemon refuses to run as Administrator, so installing it while elevated
# would put an install only an elevated shell can update, to run something
# that will then decline to start. Said plainly here rather than discovered
# later.
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($identity)
if ($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Die "do not run this as Administrator. agentrqd refuses to run as Administrator, so an agent can only do what you can. Run it from a regular, non-elevated PowerShell window."
}

# ---------------------------------------------------------------- the machine

$goos = 'windows'
switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { $goarch = 'amd64' }
    'ARM64' { $goarch = 'arm64' }
    default { Die "unsupported architecture: $($env:PROCESSOR_ARCHITECTURE). See $ReleasesPage" }
}

# --------------------------------------------------------------- the release

$headers = @{ 'Accept' = 'application/vnd.github+json' }
if ($env:GITHUB_TOKEN) { $headers['Authorization'] = "Bearer $($env:GITHUB_TOKEN)" }

# Distinguishes "no such release" (404) from a real network problem across
# both Windows PowerShell (WebException) and PowerShell 7 (HttpResponseException).
function Get-StatusCode($Exception) {
    if ($Exception.Response -and $Exception.Response.StatusCode) {
        return [int]$Exception.Response.StatusCode
    }
    return $null
}

$release = $null
$tag = $null
$version = $null
$asset = $null

# Which release to install is decided by the asset, not by the tag -- see the
# equivalent comment in install-agentrqd.sh. The daemon used to ship on its
# own series, tagged agentrqd-v<version>; since 0.7.0 everything releases
# together under the plain v<version> tag, and not every v<version> tag
# carries a daemon archive at all.
if ($Version) {
    # Accept whatever somebody has to hand: agentrqd-v0.7.0, v0.7.0 or 0.7.0.
    $bare = $Version -replace '^agentrqd-', '' -replace '^v', ''
    foreach ($candidate in @("v$bare", "agentrqd-v$bare")) {
        Say "Looking up agentrqd $candidate..."
        try {
            $release = Invoke-RestMethod -Uri "$Api/tags/$candidate" -Headers $headers -UseBasicParsing
            $tag = $candidate
            break
        }
        catch {
            if ((Get-StatusCode $_.Exception) -eq 404) { continue }
            Die "could not reach the GitHub release API. Is the network up?"
        }
    }
    if (-not $release) {
        Die "no release tagged v$bare or agentrqd-v$bare. Published versions are listed at $ReleasesPage"
    }
    $version = $bare
    $filename = "agentrqd_${version}_${goos}_${goarch}.zip"
    $asset = $release.assets | Where-Object { $_.name -eq $filename } | Select-Object -First 1
    if (-not $asset) {
        Die "release $tag has no $goos/$goarch archive ($filename). See $ReleasesPage"
    }
}
else {
    Say "Looking up the latest agentrqd release..."
    try {
        $releases = Invoke-RestMethod -Uri "$($Api)?per_page=30" -Headers $headers -UseBasicParsing
    }
    catch {
        Die "could not reach the GitHub release API. Is the network up?"
    }
    # Newest first, which is the order the API returns them in.
    $pattern = "^agentrqd_[0-9][^/]*_${goos}_${goarch}\.zip$"
    foreach ($candidateRelease in $releases) {
        $candidateAsset = $candidateRelease.assets | Where-Object { $_.name -match $pattern } | Select-Object -First 1
        if ($candidateAsset) {
            $release = $candidateRelease
            $asset = $candidateAsset
            break
        }
    }
    if (-not $asset) {
        Die "no agentrqd $goos/$goarch archive in the latest 30 releases. See $ReleasesPage"
    }
    $tag = $release.tag_name
    $filename = $asset.name
    # goreleaser's name_template: agentrqd_<version>_<os>_<arch>.zip
    $version = ($filename -replace '^agentrqd_', '') -split '_' | Select-Object -First 1
}

# ------------------------------------------------------- where it is going

$destDir = $Dir
if (-not $destDir) {
    $destDir = Join-Path $env:LOCALAPPDATA 'Programs'
}
$exePath = Join-Path $destDir 'agentrqd.exe'

# Already current? Re-downloading a release somebody already has is noise, and
# on a slow link it is a surprising amount of it.
if (-not $Force -and (Test-Path $exePath)) {
    $current = & $exePath version 2>$null | Select-Object -First 1
    if ($current -and $current.ToString().Contains($version)) {
        Say "agentrqd $version is already installed at $exePath."
        Note "Reinstall anyway with -Force."
        exit 0
    }
}

# ------------------------------------------------------------- the download

$checksumsAsset = $release.assets | Where-Object { $_.name -eq 'checksums.txt' } | Select-Object -First 1
if (-not $checksumsAsset) {
    Die "release $tag publishes no checksums.txt, so $filename cannot be verified. Nothing was installed."
}

$tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "agentrqd-install-$([System.IO.Path]::GetRandomFileName())"
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

    # Mandatory, and fail-closed. See the note at the top: this binary runs
    # commands on this machine, so an archive we cannot vouch for does not get
    # unpacked, let alone installed.
    $checksumsPath = Join-Path $tmpDir 'checksums.txt'
    try {
        Invoke-WebRequest -Uri $checksumsAsset.browser_download_url -OutFile $checksumsPath -UseBasicParsing
    }
    catch {
        Die "could not download the checksums for $tag. Nothing was installed."
    }

    $expectedLine = Get-Content $checksumsPath | Where-Object { $_ -match [regex]::Escape($filename) } | Select-Object -First 1
    if (-not $expectedLine) {
        Die "checksums.txt for $tag does not list $filename. Nothing was installed."
    }
    $expected = ($expectedLine -split '\s+')[0].ToLower()
    $actual = (Get-FileHash -Path $downloadPath -Algorithm SHA256).Hash.ToLower()
    if ($actual -ne $expected) {
        Die "checksum mismatch for $filename -- the download is corrupt or has been tampered with. Nothing was installed."
    }
    Note "Checksum verified."

    # ---------------------------------------------------------------- install

    Expand-Archive -Path $downloadPath -DestinationPath $tmpDir -Force
    $exeSource = Join-Path $tmpDir 'agentrqd.exe'
    if (-not (Test-Path $exeSource)) {
        Die "$filename did not contain an agentrqd.exe binary"
    }

    New-Item -ItemType Directory -Path $destDir -Force | Out-Null
    Say "Installing to $exePath..."
    Copy-Item -Path $exeSource -Destination $exePath -Force
}
finally {
    Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
}

# ------------------------------------------------------------------- done

Say ""
Say "Installed agentrqd $version to $exePath"

# Windows has no user directory that is already on PATH the way ~/.local/bin
# or /usr/local/bin might be, so this adds the install directory itself --
# unlike the shell installer, there is exactly one place to put it (the User
# scope in the registry), so there is no ambiguity about which profile to edit.
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$pathEntries = @()
if ($userPath) { $pathEntries = $userPath -split ';' }
if ($pathEntries -notcontains $destDir) {
    $newUserPath = if ($userPath) { "$userPath;$destDir" } else { $destDir }
    [Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')
    $env:Path = "$env:Path;$destDir"
    Say ""
    Note "$destDir was added to your PATH. Open a new terminal for it to take effect there;"
    Note "it already works in this one."
}

Say ""
Say "Next: enrol this machine against your account."
Note "Machines -> Add machine in AgentRQ gives you a short code. Then run:"
Note "  agentrqd enroll --server https://your-agentrq-server --code ABCD-EFGH"
Note ""
Note "Enrolling lets anyone who can sign in to that account run commands on"
Note "this machine, as you. Run it here, on the machine itself."
Note ""
Note "Then start it in the foreground to check it connects:"
Note "  agentrqd serve"
Note ""
Note "Full guide: $Docs"
