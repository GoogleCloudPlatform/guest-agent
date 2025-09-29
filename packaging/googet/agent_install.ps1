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

$name = 'GCEAgent'
$path = '"C:\Program Files\Google\Compute Engine\agent\GCEWindowsAgent.exe"'
$display_name = 'Google Compute Engine Agent'
$description = 'Google Compute Engine Agent'

$manager_name = 'GCEAgentManager'
$manager_path = '"C:\Program Files\Google\Compute Engine\agent\GCEWindowsAgentManager.exe"'
$manager_display_name = 'Google Compute Engine Agent Manager'
$manager_description = 'Google Compute Engine Agent Manager'

$compat_manager = 'GCEWindowsCompatManager'
$compat_path = '"C:\Program Files\Google\Compute Engine\agent\GCEWindowsCompatManager.exe"'
$compat_display_name = 'Google Compute Engine Compat Manager'
$compat_description = 'Google Compute Engine Compat Manager'

$core_enabled = "C:\ProgramData\Google\Compute Engine\google-guest-agent\core-plugin-enabled"

$initial_config = @'
# GCE Instance Configuration

# For details on what can be configured, see:
# https://cloud.google.com/compute/docs/instances/windows/creating-managing-windows-instances#configure-windows-features

# [accountManager]
# disable=false

# [addressManager]
# disable=false
'@

function Set-ServiceConfig($service_name, $service_binary) {
  # Restart service after 1s, then 2s. Reset error counter after 60s.
  sc.exe failure $service_name reset= 60 actions= restart/1000/restart/2000
  # Set dependency and delayed start
  cmd.exe /c "sc.exe config ${service_name} depend= `"samss`" start= delayed-auto binpath= \`"${service_binary}\`""
  # Create trigger to start the service on first IP address
  sc.exe triggerinfo $service_name start/networkon
}

function Set-New-Service($service_name, $service_display_name, $service_desc, $service_binary) {
  if (-not (Get-Service $service_name -ErrorAction SilentlyContinue)) {
    New-Service -Name $service_name `
                -DisplayName $service_display_name `
                -BinaryPathName $service_binary `
                -StartupType Automatic `
                -Description $service_desc
  } else {
    Set-Service -Name $service_name `
                -DisplayName $service_display_name `
                -Description $service_desc
  }
}

try {
  # This is to safeguard from installing agent manager using placeholder file
  $install_manager = $false
  if (Test-Path ($manager_path -replace '"', "")) {
    $contains = Select-String -Path ($manager_path -replace '"', "") -Pattern "This is a placeholder file"
    if ($contains -eq $null) {
      $install_manager = $true
    }
  }

  # Guest Agent service
  Set-New-Service $name $display_name $description $path
  Set-ServiceConfig $name $path

  # Guest Agent Manager and Compat Manager service
  if ($install_manager) {
    Set-New-Service $compat_manager $compat_display_name $compat_description $compat_path
    Set-ServiceConfig $compat_manager $compat_path

    Set-New-Service $manager_name $manager_display_name $manager_description $manager_path
    Set-ServiceConfig $manager_name $manager_path
  } else {
    if (Get-Service $compat_manager -ErrorAction SilentlyContinue) {
      Stop-Service $compat_manager
      & sc.exe delete $compat_manager
    }

    if (Get-Service $manager_name -ErrorAction SilentlyContinue) {
      Stop-Service $manager_name
      & sc.exe delete $manager_name
    }
  }

  $config = "${env:ProgramFiles}\Google\Compute Engine\instance_configs.cfg"
  if (-not (Test-Path $config)) {
    $initial_config | Set-Content -Path $config -Encoding ASCII
  }

  if ($install_manager) {
    # If core plugin is disabled, honor the setting and restart non-plugin based agent.
    if (Get-Content -Path $core_enabled -ErrorAction SilentlyContinue | Select-String -Pattern "false" -Quiet) {
      Restart-Service $name -Verbose
    }
    else {
      & sc.exe config $name start=disabled
      Stop-Service $name
    }
    
    Restart-Service $compat_manager -Verbose
    Restart-Service $manager_name -Verbose
  } else {
     Restart-Service $name -Verbose
  }
}
catch {
  Write-Output $_.InvocationInfo.PositionMessage
  Write-Output "Install failed: $($_.Exception.Message)"
  exit 1
}
