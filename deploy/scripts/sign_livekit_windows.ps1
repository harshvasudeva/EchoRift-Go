$ErrorActionPreference = 'Stop'

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot '..\..')
$livekitExe = Join-Path $repoRoot 'tools\livekit\livekit-server.exe'
$certSubject = 'CN=EchoRift Local Dev'

if (!(Test-Path $livekitExe)) {
  throw "LiveKit executable not found at $livekitExe"
}

$cert = Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert |
  Where-Object { $_.Subject -eq $certSubject } |
  Sort-Object NotAfter -Descending |
  Select-Object -First 1

if (!$cert) {
  throw 'EchoRift Local Dev signing certificate not found. Run build_sign_windows.ps1 first.'
}

$signature = Set-AuthenticodeSignature -FilePath $livekitExe -Certificate $cert -HashAlgorithm SHA256
$signature | Format-List Status,StatusMessage,Path

if ($signature.Status -ne 'Valid') {
  throw "LiveKit signing failed with status $($signature.Status): $($signature.StatusMessage)"
}
