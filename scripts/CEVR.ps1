#Requires -Version 5.1

function Test-PortAvailable {
    param([int]$Port)
    try {
        $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, $Port)
        $listener.Start()
        $listener.Stop()
        return $true
    } catch {
        return $false
    }
}

function Get-AvailableTcpPort {
    param([int]$StartPort = 6721)

    $port = $StartPort
    $tempPath = [System.IO.Path]::GetTempPath()

    # Add a sanity limit to prevent an infinite loop
    for ($i = 0; $i -lt 1000; $i++) {
        $portFile = Join-Path $tempPath "cevr_ps1.port.$port"
        if (Test-PortAvailable -Port $port) {
            if (-not (Test-Path $portFile)) {
                # Reserve the port by creating the file with this script's PID
                $PID | Set-Content -Path $portFile
                return @{Port = $port; PortFile = $portFile }
            }
            else {
                # File exists, check if the PID is still alive
                $filePid = Get-Content $portFile
                if (-not (Get-Process -Id $filePid -ErrorAction SilentlyContinue)) {
                    Write-Verbose "Removing stale port file: $portFile"
                    Remove-Item $portFile -Force
                    # Retry this same port immediately
                    continue
                }
            }
        }
        $port++
    }
    # If no port is found after 1000 tries, throw an error.
    throw "Could not find an available TCP port after 1000 attempts."
}

function Wait-PortListening {
    # Poll until something is LISTENING on $Port (i.e. echovr has bound its
    # -httpport), or until the process dies / the timeout elapses. Returns $true
    # only if the port actually came up. This is the gate that was missing: without
    # it, a slow-to-bind echovr looks dead and the monitor cycles new instances on
    # top of it. Uses a plain TcpClient connect so it needs no extra modules (PS 5.1).
    param(
        [Parameter(Mandatory = $true)][int]$Port,
        [int]$TimeoutSec = 60,
        [int]$PollMs = 500,
        [System.Diagnostics.Process]$Process
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSec)
    while ((Get-Date) -lt $deadline) {
        # If echovr already exited, stop waiting — it never got healthy.
        if ($null -ne $Process -and $Process.HasExited) { return $false }
        try {
            $client = [System.Net.Sockets.TcpClient]::new()
            $client.Connect([System.Net.IPAddress]::Loopback, $Port)
            $client.Close()
            return $true
        } catch {
            Start-Sleep -Milliseconds $PollMs
        }
    }
    return $false
}

function Start-MonitoredProcess {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,

        [string]$ArgumentList,

        [int]$InitialRestartDelaySec = 3,

        [int]$MaxRestartDelaySec = 60,

        # How long to wait for echovr to bind its -httpport before declaring the
        # launch failed. echovr loads ~569 DLLs, so give it real headroom.
        [int]$PortBindTimeoutSec = 90
    )

    $currentDelay = $InitialRestartDelaySec

    while ($true) {
        $portInfo = $null
        try {
            # Find and reserve a port for this session
            $portInfo = Get-AvailableTcpPort
            $argumentsWithPort = "$ArgumentList -httpport $($portInfo.Port)"

            Write-Verbose "Starting process: $FilePath $argumentsWithPort"

            $process = Start-Process -FilePath $FilePath -ArgumentList $argumentsWithPort -PassThru
            $process.PriorityClass = [System.Diagnostics.ProcessPriorityClass]::High

            # Gate: wait for echovr to actually BIND its http port before treating it
            # as healthy. Without this the loop can't tell "still starting up" from
            # "dead", so it cycles fresh instances on top of a slow-to-bind one
            # (the 2-3 churning). If the port never comes up (or echovr dies during
            # startup), kill it and back off instead of hammering.
            if (-not (Wait-PortListening -Port $portInfo.Port -TimeoutSec $PortBindTimeoutSec -Process $process)) {
                Write-Warning "echovr did not bind http port $($portInfo.Port) within $PortBindTimeoutSec s. Killing and backing off $currentDelay s."
                if (-not $process.HasExited) {
                    Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
                }
                Start-Sleep -Seconds $currentDelay
                $currentDelay = [System.Math]::Min($currentDelay * 2, $MaxRestartDelaySec)
                continue
            }
            Write-Verbose "echovr bound http port $($portInfo.Port); healthy. Serving."
            # A healthy bind resets the backoff — this was a good launch.
            $currentDelay = $InitialRestartDelaySec

            # Wait for the process to exit
            $process | Wait-Process

            # If the process exited quickly, it may have crashed.
            # A process running less than 15 seconds is considered a fast failure.
            $runDuration = (Get-Date) - $process.StartTime
            if ($runDuration.TotalSeconds -lt 15) {
                Write-Warning "Process exited quickly. Increasing restart delay to $currentDelay seconds."
                Start-Sleep -Seconds $currentDelay
                # Double the delay for the next failure, up to the max
                $currentDelay = [System.Math]::Min($currentDelay * 2, $MaxRestartDelaySec)
            } else {
                # Reset delay after a successful run
                $currentDelay = $InitialRestartDelaySec
                Write-Verbose "Process exited normally. Restarting after $InitialRestartDelaySec seconds."
                Start-Sleep -Seconds $InitialRestartDelaySec
            }

        }
        catch {
            Write-Error "An error occurred in the monitoring loop: $_"
            Start-Sleep -Seconds $currentDelay
            $currentDelay = [System.Math]::Min($currentDelay * 2, $MaxRestartDelaySec)
        }
        finally {
            # Ensure the port file is cleaned up even if errors occur
            if ($null -ne $portInfo.PortFile -and (Test-Path $portInfo.PortFile)) {
                Write-Verbose "Cleaning up port file: $($portInfo.PortFile)"
                Remove-Item $portInfo.PortFile -Force
            }
        }
    }
}

# --- Script Execution Starts Here ---
# Guard so the file can be dot-sourced by tests without launching the server.
# When dot-sourced, $MyInvocation.InvocationName is '.'; when run directly it isn't.
if ($MyInvocation.InvocationName -ne '.') {
    $exePath = "C:\echovr\ready-at-dawn-echo-arena\bin\win10\echovr.exe"
    $baseArgs = "-noovr -server -headless -fixedtimestep -noaudio -timestep 180 -exitonerror"

    # Call the main function with the configured parameters
    Start-MonitoredProcess -FilePath $exePath -ArgumentList $baseArgs -Verbose
}