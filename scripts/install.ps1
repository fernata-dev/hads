# hads installer - Windows 10/11 (x64 + ARM64), PowerShell 5.1 or 7+
#   irm https://raw.githubusercontent.com/fernata-dev/hads/main/scripts/install.ps1 | iex
# env: HADS_VERSION (e.g. v1.0.0), HADS_SETUP_ARGS (e.g. "--yes"), HADS_DL_URL (mirror)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$repo = if ($env:HADS_REPO) { $env:HADS_REPO } else { 'fernata-dev/hads' }
$dl = "https://github.com/$repo/releases/latest/download"
if ($env:HADS_VERSION) { $dl = "https://github.com/$repo/releases/download/$($env:HADS_VERSION)" }
if ($env:HADS_DL_URL) { $dl = $env:HADS_DL_URL }
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { 'arm64' } else { 'amd64' }
$dir = Join-Path $env:LOCALAPPDATA 'hads\bin'
$exe = Join-Path $dir 'hads.exe'
$name = "hads-windows-$arch.exe"

Write-Host ''
Write-Host '  * hads' -ForegroundColor Magenta -NoNewline
Write-Host "  - the terminal that knows what you'll type next" -ForegroundColor DarkGray
Write-Host ''
Write-Host "  > downloading $name"

New-Item -ItemType Directory -Force -Path $dir | Out-Null
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("hads-" + [guid]::NewGuid().ToString() + ".exe")
Invoke-WebRequest -Uri "$dl/$name" -OutFile $tmp -UseBasicParsing
$sums = (Invoke-WebRequest -Uri "$dl/checksums.txt" -UseBasicParsing).Content
if ($sums -is [byte[]]) { $sums = [Text.Encoding]::UTF8.GetString($sums) }

$want = $null
foreach ($line in ($sums -split "`n")) {
  $parts = $line.Trim() -split '\s+'
  if ($parts.Count -ge 2 -and $parts[1] -eq $name) { $want = $parts[0].ToLower() }
}
$got = (Get-FileHash -Path $tmp -Algorithm SHA256).Hash.ToLower()
if (-not $want -or $want -ne $got) {
  Remove-Item $tmp -Force
  throw "hads: checksum mismatch - refusing to install"
}
Write-Host '  + sha-256 verified' -ForegroundColor Green

if (Test-Path $exe) { try { & $exe stop *> $null } catch {} ; Start-Sleep -Milliseconds 300 }
Move-Item -Force -Path $tmp -Destination $exe
Unblock-File -Path $exe -ErrorAction SilentlyContinue
Write-Host "  + installed $exe" -ForegroundColor Green

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not (($userPath -split ';') -contains $dir)) {
  [Environment]::SetEnvironmentVariable('Path', ($dir + ';' + $userPath).TrimEnd(';'), 'User')
  Write-Host '  + added to your PATH' -ForegroundColor Green
}
$env:Path = "$dir;$env:Path"

$setupArgs = @('setup')
if ($env:HADS_SETUP_ARGS) { $setupArgs += ($env:HADS_SETUP_ARGS -split '\s+') }
& $exe @setupArgs
