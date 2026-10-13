<#
.SYNOPSIS
  Fetch the zashboard build this binary serves into backend/internal/web/dist.

.DESCRIPTION
  The panel is zashboard (https://github.com/Zephyruso/zashboard, MIT), used as
  its published build rather than rebuilt from source: it is a large Vue app
  with its own toolchain, and pinning a release plus its checksum makes the
  panel a fixed input to this repo instead of a moving dependency.

  Output lands in backend/internal/web/dist, which is gitignored and embedded by
  go:embed. Re-run it to move to a new panel version: change the two constants
  below together, since the checksum is what makes the pin mean anything.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$Version = 'v3.29.1'
# dist-no-fonts: system fonts only. The font-bundled builds are 3-5x larger for
# a dashboard that renders fine either way.
$Sha256 = '21371cd111b6b3d87774f3ea3ddeacdcb0f6aff8690a16d652daa3ea6e3b0145'

$RepoRoot = Split-Path -Parent $PSScriptRoot
$Dest = Join-Path $RepoRoot 'backend/internal/web/dist'
$Url = "https://github.com/Zephyruso/zashboard/releases/download/$Version/dist-no-fonts.zip"

$Work = Join-Path ([System.IO.Path]::GetTempPath()) ("zashboard-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $Work -Force | Out-Null

try {
  $Zip = Join-Path $Work 'panel.zip'

  Write-Host "fetch-zashboard: downloading $Version"
  Invoke-WebRequest -Uri $Url -OutFile $Zip -UseBasicParsing

  Write-Host 'fetch-zashboard: verifying checksum'
  $Actual = (Get-FileHash -Path $Zip -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($Actual -ne $Sha256) {
    throw "checksum mismatch: expected $Sha256, got $Actual"
  }

  Expand-Archive -Path $Zip -DestinationPath $Work -Force
  $Extracted = Join-Path $Work 'dist'
  if (-not (Test-Path (Join-Path $Extracted 'index.html'))) {
    throw 'archive did not contain dist/index.html'
  }

  # Replace the tree wholesale so a file dropped between panel versions does not
  # linger and get embedded. .gitkeep is restored so a fresh checkout still has
  # the directory the placeholder exists for.
  if (Test-Path $Dest) {
    Get-ChildItem -Path $Dest -Force | Remove-Item -Recurse -Force
  } else {
    New-Item -ItemType Directory -Path $Dest -Force | Out-Null
  }
  Copy-Item -Path (Join-Path $Extracted '*') -Destination $Dest -Recurse -Force
  New-Item -ItemType File -Path (Join-Path $Dest '.gitkeep') -Force | Out-Null

  Write-Host "fetch-zashboard: panel $Version is in backend/internal/web/dist"
} finally {
  Remove-Item -Path $Work -Recurse -Force -ErrorAction SilentlyContinue
}
