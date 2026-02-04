#  Copyright 2017 Google LLC
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      https://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.

$compat_manager = 'GCEWindowsCompatManager'
$name = 'GCEAgentManager'
$cleanup_exe = "C:\Program Files\Google\Compute Engine\agent\ggactl_plugin.exe"

function Invoke-CommandWithTimeout {
    param(
        [string]$FilePath,
        [string[]]$ArgumentList,
        [int]$TimeoutSeconds = 10
    )
    try {
        $p = Start-Process -FilePath $FilePath -ArgumentList $ArgumentList -PassThru -NoNewWindow
        try {
            Wait-Process -InputObject $p -Timeout $TimeoutSeconds -ErrorAction Stop
        } catch [System.TimeoutException] {
            Write-Warning "Command '$FilePath' with args '$ArgumentList' timed out after $TimeoutSeconds seconds. Killing..."
            Stop-Process -InputObject $p -Force -ErrorAction SilentlyContinue
        }
    } catch {
        Write-Warning "Failed to run or wait for '$FilePath': $_"
    }
}

function Remove-ServiceSafely {
    param (
        [string]$ServiceName
    )
    $service = Get-Service $ServiceName -ErrorAction SilentlyContinue
    if ($service) {
        if ($service.Status -ne 'Stopped') {
            try {
                Stop-Service $ServiceName -Force -ErrorAction Stop
            } catch {
                Write-Warning "Failed to stop service $ServiceName : $_"
            }
        }
        try {
            $output = & sc.exe delete $ServiceName 2>&1
            if ($LASTEXITCODE -ne 0) {
                 Write-Warning "sc.exe delete failed for $ServiceName : $output"
            }
        } catch {
            Write-Warning "Failed to delete service $ServiceName : $_"
        }
    }
}

# Stop and delete GCEAgent.
Remove-ServiceSafely -ServiceName 'GCEAgent'

# Stop and Delete compat manager.
Remove-ServiceSafely -ServiceName $compat_manager

# Stop Guest Agent Manager, cleanup all plugins (if present) and delete the service.
# We attempt cleanup even if the service is missing, just in case.
if (Test-Path $cleanup_exe) {
    Invoke-CommandWithTimeout -FilePath $cleanup_exe -ArgumentList "coreplugin","stop"
    Invoke-CommandWithTimeout -FilePath $cleanup_exe -ArgumentList "dynamic-cleanup"
}

Remove-ServiceSafely -ServiceName $name

# Fallback cleanup for runtime data directory
$runtimeDataDir = "C:\ProgramData\Google\Compute Engine\google-guest-agent"
if (Test-Path $runtimeDataDir) {
    try {
        Write-Verbose "Removing runtime data directory: $runtimeDataDir"
        Remove-Item -Path $runtimeDataDir -Recurse -Force -ErrorAction SilentlyContinue
    } catch {
        Write-Warning "Failed to remove runtime data directory: $_"
    }
}

