$ErrorActionPreference = 'Stop'

$certSubject = 'CN=EchoRift Local Dev'
$repoRoot = Resolve-Path (Join-Path $PSScriptRoot '..\..')
$backendDir = Join-Path $repoRoot 'backend'
$webDir = Join-Path $repoRoot 'apps\web'
$webDist = Join-Path $webDir 'dist'
$embedDist = Join-Path $repoRoot 'backend\internal\web\dist'
$binDir = Join-Path $repoRoot 'bin'
$outExe = Join-Path $binDir 'echorift.exe'
$certExportPath = Join-Path $env:TEMP 'EchoRiftLocalDev.cer'
$skipSigning = $env:ECHORIFT_SKIP_SIGNING -eq '1'

New-Item -ItemType Directory -Force $binDir | Out-Null

function Get-GoCommand {
  $goCommand = Get-Command go -ErrorAction SilentlyContinue
  if ($goCommand) {
    return $goCommand.Source
  }

  $fallbacks = @(
    'C:\Program Files\Go\bin\go.exe',
    'C:\Program Files (x86)\Go\bin\go.exe'
  )

  foreach ($fallback in $fallbacks) {
    if (Test-Path $fallback) {
      return $fallback
    }
  }

  throw 'go was not found in PATH or the standard Windows install locations.'
}

function Get-OrCreate-CodeSigningCert {
  $cert = Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert |
    Where-Object { $_.Subject -eq $certSubject } |
    Sort-Object NotAfter -Descending |
    Select-Object -First 1

  if (!$cert) {
    Write-Host 'Creating self-signed EchoRift local development code-signing certificate...'
    $cert = New-SelfSignedCertificate `
      -Type CodeSigningCert `
      -Subject $certSubject `
      -KeyAlgorithm RSA `
      -KeyLength 3072 `
      -HashAlgorithm SHA256 `
      -CertStoreLocation 'Cert:\CurrentUser\My' `
      -NotAfter (Get-Date).AddYears(3)
  }

  Export-Certificate -Cert $cert -FilePath $certExportPath -Force | Out-Null
  Import-Certificate -FilePath $certExportPath -CertStoreLocation 'Cert:\CurrentUser\Root' | Out-Null
  Import-Certificate -FilePath $certExportPath -CertStoreLocation 'Cert:\CurrentUser\TrustedPublisher' | Out-Null

  return $cert
}

$go = Get-GoCommand

Write-Host "Using Go: $go"
& $go version

Write-Host 'Building web client for embedded single-exe release...'
& corepack pnpm --filter '@echorift/web' build
if ($LASTEXITCODE -ne 0) {
  throw 'Web build failed.'
}

if (!(Test-Path $webDist)) {
  throw "Web dist directory not found at $webDist"
}

if (Test-Path $embedDist) {
  Remove-Item $embedDist -Recurse -Force
}
New-Item -ItemType Directory -Force $embedDist | Out-Null
Copy-Item -Path (Join-Path $webDist '*') -Destination $embedDist -Recurse -Force

Write-Host "Building backend + embedded web to $outExe"
& $go -C $backendDir build -trimpath -o $outExe .\cmd\echorift
if ($LASTEXITCODE -ne 0) {
  throw 'Go build failed.'
}

if ($skipSigning) {
  Write-Host "Unsigned executable ready: $outExe"
  return
}

$cert = Get-OrCreate-CodeSigningCert

Write-Host 'Signing backend executable...'
$signature = Set-AuthenticodeSignature `
  -FilePath $outExe `
  -Certificate $cert `
  -HashAlgorithm SHA256 `
  -TimestampServer 'http://timestamp.digicert.com'

if ($signature.Status -ne 'Valid') {
  Write-Warning "Timestamped signing status was $($signature.Status). Retrying without timestamp server..."
  $signature = Set-AuthenticodeSignature `
    -FilePath $outExe `
    -Certificate $cert `
    -HashAlgorithm SHA256
}

$verification = Get-AuthenticodeSignature $outExe
$verification | Format-List Status,StatusMessage,SignerCertificate,Path

if ($verification.Status -ne 'Valid') {
  throw "Signature verification failed with status: $($verification.Status). Message: $($verification.StatusMessage)"
}

Write-Host "Signed executable ready: $outExe"
