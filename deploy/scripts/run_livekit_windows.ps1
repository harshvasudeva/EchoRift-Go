$ErrorActionPreference = 'Stop'

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot '..\..')
$configPath = Join-Path $repoRoot 'deploy\livekit\livekit.local.yaml'
if (!(Test-Path $configPath)) {
  $configPath = Join-Path $repoRoot 'deploy\livekit\livekit.example.yaml'
}

$livekit = Get-Command livekit-server -ErrorAction SilentlyContinue
if (!$livekit) {
  $localPath = Join-Path $repoRoot 'tools\livekit\livekit-server.exe'
  if (Test-Path $localPath) {
    $livekitPath = $localPath
  } else {
    throw "livekit-server not found. Download livekit-server.exe from https://github.com/livekit/livekit/releases and either put it on PATH or at $localPath"
  }
} else {
  $livekitPath = $livekit.Source
}

Write-Host "Starting LiveKit: $livekitPath"
Write-Host "Config: $configPath"
& $livekitPath --config $configPath
