$ErrorActionPreference = 'Stop'

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot '..\..')
$buildScript = Join-Path $PSScriptRoot 'build_sign_windows.ps1'
$backendExe = Join-Path $repoRoot 'bin\echorift.exe'
$logPath = Join-Path $repoRoot 'backend\smoke.log'
$errPath = Join-Path $repoRoot 'backend\smoke.err.log'

if (!(Test-Path $backendExe)) {
  & powershell.exe -ExecutionPolicy Bypass -File $buildScript
}

$signature = Get-AuthenticodeSignature $backendExe
if ($signature.Status -ne 'Valid') {
  & powershell.exe -ExecutionPolicy Bypass -File $buildScript
}

if (Test-Path $logPath) {
  Remove-Item $logPath -Force
}
if (Test-Path $errPath) {
  Remove-Item $errPath -Force
}

$process = Start-Process -FilePath $backendExe -WorkingDirectory $repoRoot -RedirectStandardOutput $logPath -RedirectStandardError $errPath -PassThru
try {
  Start-Sleep -Seconds 3

  $session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
  $email = "smoke-$([Guid]::NewGuid().ToString('N').Substring(0, 8))@example.com"
  $password = "smoke-$([Guid]::NewGuid().ToString('N'))"

  $signupBody = @{
    email = $email
    password = $password
    display_name = 'Smoke Test'
    device_name = 'PowerShell Smoke'
    platform = 'cli'
  } | ConvertTo-Json

  $signup = Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:8080/api/v1/auth/signup' -ContentType 'application/json' -Body $signupBody -WebSession $session
  $token = $signup.access_token

  $workspaceBody = @{ name = 'Smoke Workspace' } | ConvertTo-Json
  $workspace = Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:8080/api/v1/workspaces' -ContentType 'application/json' -Headers @{ Authorization = "Bearer $token" } -Body $workspaceBody -WebSession $session
  $workspaceID = $workspace.workspace.id

  $channels = Invoke-RestMethod -Method Get -Uri "http://127.0.0.1:8080/api/v1/workspaces/$workspaceID/channels" -Headers @{ Authorization = "Bearer $token" } -WebSession $session
  $textChannel = $channels.channels | Where-Object { $_.type -eq 'text' } | Select-Object -First 1
  $channelID = $textChannel.id

  $messageBody = @{ body = 'hello from smoke test' } | ConvertTo-Json
  $message = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:8080/api/v1/workspaces/$workspaceID/channels/$channelID/messages" -ContentType 'application/json' -Headers @{ Authorization = "Bearer $token" } -Body $messageBody -WebSession $session

  $messages = Invoke-RestMethod -Method Get -Uri "http://127.0.0.1:8080/api/v1/workspaces/$workspaceID/channels/$channelID/messages" -Headers @{ Authorization = "Bearer $token" } -WebSession $session

  [PSCustomObject]@{
    ok = $true
    email = $email
    user_id = $signup.user.id
    workspace_id = $workspaceID
    channel_id = $channelID
    message_id = $message.message.id
    message_count = $messages.messages.Count
  } | ConvertTo-Json
}
finally {
  if ($process -and !$process.HasExited) {
    Stop-Process -Id $process.Id -Force
  }
  if (Test-Path $logPath) {
    Get-Content $logPath
  }
  if (Test-Path $errPath) {
    Get-Content $errPath
  }
}
