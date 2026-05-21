$ErrorActionPreference = 'Stop'

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot '..\..')
$backendScript = Join-Path $repoRoot 'deploy\scripts\run_backend_windows.ps1'
$livekitScript = Join-Path $repoRoot 'deploy\scripts\run_livekit_windows.ps1'

Start-Process powershell.exe -ArgumentList @('-NoExit', '-ExecutionPolicy', 'Bypass', '-File', $livekitScript)
Start-Sleep -Seconds 2
Start-Process powershell.exe -ArgumentList @('-NoExit', '-ExecutionPolicy', 'Bypass', '-File', $backendScript)

Write-Host 'Started LiveKit and EchoRift in separate PowerShell windows.'
Write-Host 'Open http://127.0.0.1:8080'
