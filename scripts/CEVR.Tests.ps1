# Tests for CEVR.ps1's port-bind gate (Wait-PortListening). Dot-sources the script
# (the execution guard keeps it from launching echovr) and exercises the gate the
# way the monitor loop uses it. Run: pwsh -File scripts/CEVR.Tests.ps1
. "$PSScriptRoot/CEVR.ps1"

$fail = 0
function Check($name, $cond) {
    if ($cond) { Write-Host "PASS  $name" }
    else { Write-Host "FAIL  $name" -ForegroundColor Red; $script:fail++ }
}

# BAC: a port with a listener bound is detected as healthy (echovr bound its httpport).
$listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
$listener.Start()
$boundPort = $listener.LocalEndpoint.Port
$sw = [System.Diagnostics.Stopwatch]::StartNew()
$up = Wait-PortListening -Port $boundPort -TimeoutSec 5
$sw.Stop()
Check "detects a LISTENING port as healthy" ($up -eq $true)
Check "returns promptly when already up (<1s)" ($sw.Elapsed.TotalSeconds -lt 1)
$listener.Stop()

# BAC: a port with nothing listening times out to failure (the cycling case — but
# now it reports failure deliberately instead of the loop guessing).
$free = 6799  # unlikely to be bound in test env
$sw2 = [System.Diagnostics.Stopwatch]::StartNew()
$down = Wait-PortListening -Port $free -TimeoutSec 2 -PollMs 200
$sw2.Stop()
Check "times out to FAILURE on a dead port" ($down -eq $false)
Check "honors the timeout (>=2s, <4s)" ($sw2.Elapsed.TotalSeconds -ge 2 -and $sw2.Elapsed.TotalSeconds -lt 4)

# BAC: if the process already exited, bail immediately (don't wait the full timeout).
$dead = Start-Process -FilePath (Get-Process -Id $PID).Path -ArgumentList '-NoProfile','-Command','exit 0' -PassThru
$dead | Wait-Process
$sw3 = [System.Diagnostics.Stopwatch]::StartNew()
$res = Wait-PortListening -Port $free -TimeoutSec 10 -Process $dead
$sw3.Stop()
Check "returns fast when process already exited (<2s, not full 10s)" ($res -eq $false -and $sw3.Elapsed.TotalSeconds -lt 2)

if ($fail -gt 0) { Write-Host "`n$fail test(s) FAILED" -ForegroundColor Red; exit 1 }
Write-Host "`nAll tests passed" -ForegroundColor Green
