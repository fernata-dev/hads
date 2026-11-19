# hads - predictive shell for PowerShell  (https://hads.sh)
#   Ctrl+Space   fill the predicted command      Alt+N   cycle alternatives
#   ? <request>  natural-language mode (if enabled)
if ($global:__HadsLoaded) { return }
$global:__HadsLoaded = $true
$global:__HadsBin = __HADS_BIN__
$global:__HadsSess = "pwsh-$PID"
$global:__HadsLastId = (Get-History -Count 1).Id
$global:__HadsList = @()
$global:__HadsIdx = -1
$global:__HadsCur = $null

function global:__HadsB64([string]$s) { [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes([string]$s)) }
function global:__HadsUn64([string]$s) { [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($s)) }
function global:__HadsCwd { try { (Get-Location).ProviderPath } catch { $HOME } }
function global:__HadsPredict([string]$buf, [int]$limit) {
  $out = & $global:__HadsBin predict --shell pwsh --sess $global:__HadsSess --limit $limit --out-b64 "--cwd-b64=$(__HadsB64 (__HadsCwd))" "--buffer-b64=$(__HadsB64 $buf)" 2>$null
  @($out | Where-Object { $_ } | ForEach-Object { __HadsUn64 $_ })
}

$global:__HadsOrigPrompt = $function:prompt
function global:prompt {
  $ok = $?
  $code = $global:LASTEXITCODE
  try {
    $h = Get-History -Count 1
    if ($h -and $h.Id -ne $global:__HadsLastId) {
      $global:__HadsLastId = $h.Id
      $ec = 0
      if (-not $ok) {
        $ec = 1
        if ($code) { $ec = $code }
        if ($Error.Count -gt 0 -and "$($Error[0].FullyQualifiedErrorId)" -like 'CommandNotFoundException*') { $ec = 127 }
      }
      & $global:__HadsBin record --shell pwsh --sess $global:__HadsSess --exit $ec "--cwd-b64=$(__HadsB64 (__HadsCwd))" "--cmd-b64=$(__HadsB64 $h.CommandLine)" 2>$null | Out-Null
    }
  } catch {}
  $global:LASTEXITCODE = $code
  if (-not $ok) { Write-Error 'hads' -ErrorAction Ignore }
  & $global:__HadsOrigPrompt
}

if (-not (Get-Command ask -ErrorAction SilentlyContinue)) {
  function global:ask { & $global:__HadsBin ask ($args -join ' ') }
}

if (Get-Module PSReadLine -ErrorAction SilentlyContinue) {
  Set-PSReadLineKeyHandler -Chord 'Ctrl+Spacebar' -BriefDescription 'HadsFill' -Description 'Fill the command hads predicts' -ScriptBlock {
    $line = $null; $cursor = $null
    [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)
    $s = @(__HadsPredict $line 1)
    if ($s.Count -gt 0 -and $s[0]) {
      [Microsoft.PowerShell.PSConsoleReadLine]::Replace(0, $line.Length, $s[0])
      [Microsoft.PowerShell.PSConsoleReadLine]::SetCursorPosition($s[0].Length)
      $global:__HadsCur = $s[0]
    }
  }
  Set-PSReadLineKeyHandler -Chord 'Alt+n' -BriefDescription 'HadsCycle' -Description 'Cycle hads predictions' -ScriptBlock {
    $line = $null; $cursor = $null
    [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)
    if ($line -ne $global:__HadsCur) {
      $global:__HadsList = @(__HadsPredict $line 6)
      $global:__HadsIdx = -1
    }
    if ($global:__HadsList.Count -eq 0) { return }
    $global:__HadsIdx = ($global:__HadsIdx + 1) % $global:__HadsList.Count
    $s = $global:__HadsList[$global:__HadsIdx]
    [Microsoft.PowerShell.PSConsoleReadLine]::Replace(0, $line.Length, $s)
    [Microsoft.PowerShell.PSConsoleReadLine]::SetCursorPosition($s.Length)
    $global:__HadsCur = $s
  }
  Set-PSReadLineKeyHandler -Key Enter -BriefDescription 'HadsEnter' -Description 'Accept line; "? request" runs hads ask' -ScriptBlock {
    $line = $null; $cursor = $null
    [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)
    $q = [string][char]0x061F
    $bin = 'hads'
    if (-not (Get-Command hads -ErrorAction SilentlyContinue)) { $bin = "& '" + ($global:__HadsBin -replace "'", "''") + "'" }
    $req = $null
    if ($line.StartsWith('? ') -or $line.StartsWith($q + ' ')) {
      $req = $line.Substring(2).Trim()
    } elseif ($line.Trim()) {
      # a plain-language request typed as a command (Arabic, or an English sentence)
      $first = ($line.Trim() -split '\s+')[0]
      if ($first -notmatch '[\\/]' -and -not (Get-Command $first -ErrorAction SilentlyContinue)) {
        & $global:__HadsBin cnf --b64 (__HadsB64 $line) 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) { $req = $line.Trim() }
      }
    }
    if ($req) {
      [Microsoft.PowerShell.PSConsoleReadLine]::Replace(0, $line.Length, "$bin ask '" + ($req -replace "'", "''") + "'")
    }
    [Microsoft.PowerShell.PSConsoleReadLine]::AcceptLine()
  }
}
