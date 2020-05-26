#  Copyright 2017 Google Inc. All Rights Reserved.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.

$name = 'GCEAgent'
$path = '"C:\Program Files\Google\Compute Engine\agent\GCEWindowsAgent.exe"'
$display_name = 'Google Compute Engine Agent'
$description = 'Google Compute Engine Agent'
$initial_config = @'
# GCE Instance Configuration

# For details on what can be configured, see:
# https://cloud.google.com/compute/docs/instances/windows/creating-managing-windows-instances#configure-windows-features

# [accountManager]
# disable=false

# [addressManager]
# disable=false
'@

function Set-ServiceConfig {
  # Restart service after 1s, then 2s. Reset error counter after 60s.
  sc.exe failure $name reset= 60 actions= restart/1000/restart/2000
  # Set dependency and delayed start
  cmd.exe /c "sc.exe config ${name} depend= `"samss`" start= delayed-auto binpath= \`"${path}\`""
  # Create trigger to start the service on first IP address
  sc.exe triggerinfo $name start/networkon
}

try {

  if (-not (Get-Service $name -ErrorAction SilentlyContinue)) {
    New-Service -Name $name `
                -DisplayName $display_name `
                -BinaryPathName $path `
                -StartupType Automatic `
                -Description $description
  } 
  else {
    Set-Service -Name $name `
                -DisplayName $display_name `
                -Description $description
  }

  Set-ServiceConfig

  $config = "${env:ProgramFiles}\Google\Compute Engine\instance_configs.cfg"
  if (-not (Test-Path $config)) {
    $initial_config | Set-Content -Path $config -Encoding ASCII
  }

  Restart-Service $name -Verbose
}
catch {
  Write-Output $_.InvocationInfo.PositionMessage
  Write-Output "Install failed: $($_.Exception.Message)"
  exit 1
}
