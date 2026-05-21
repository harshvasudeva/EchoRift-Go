$ErrorActionPreference = 'Stop'

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot '..\..')
$outExe = Join-Path $repoRoot 'bin\echorift.exe'
$buildScript = Join-Path $PSScriptRoot 'build_sign_windows.ps1'

if (!(Test-Path $outExe)) {
  & powershell.exe -ExecutionPolicy Bypass -File $buildScript
}

$signature = Get-AuthenticodeSignature $outExe
if ($signature.Status -ne 'Valid') {
  & powershell.exe -ExecutionPolicy Bypass -File $buildScript
}

& $outExe
