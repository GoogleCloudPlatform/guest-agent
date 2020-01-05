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

$install_dir = "${env:ProgramFiles}\Google\Compute Engine\metadata_scripts"
$machine_env = 'HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment'

$path = (Get-ItemProperty $machine_env).Path
if ($path -notlike "*${install_dir}*") {
  Set-ItemProperty $machine_env -Name 'Path' -Value ($path + ";${install_dir}")
}

$run_startup_scripts = "${install_dir}\run_startup_scripts.cmd"
$service = New-Object -ComObject("Schedule.Service")
$service.Connect()
$task = $service.NewTask(0)
$task.Settings.Enabled = $true
$task.Settings.AllowDemandStart = $true
$task.Settings.Priority = 5
$action = $task.Actions.Create(0)
$action.Path = "`"$run_startup_scripts`""
$trigger = $task.Triggers.Create(8)
$folder = $service.GetFolder('\')
$folder.RegisterTaskDefinition('GCEStartup',$task,6,'System',$null,5) | Out-Null

$gpt_ini = "${env:SystemRoot}\System32\GroupPolicy\gpt.ini"
$scripts_ini = "${env:SystemRoot}\System32\GroupPolicy\Machine\Scripts\scripts.ini"
if ((Test-Path $gpt_ini) -or (Test-Path $scripts_ini)) {
  return
}

New-Item -Type Directory -Path "${env:SystemRoot}\System32\GroupPolicy\Machine\Scripts" -ErrorAction SilentlyContinue

@'
[General]
gPCMachineExtensionNames= [{42B5FAAE-6536-11D2-AE5A-0000F87571E3}{40B6664F-4972-11D1-A7CA-0000F87571E3}]
Version=1
'@ | Set-Content -Path $gpt_ini -Encoding ASCII

@'
[Shutdown]
0CmdLine=C:\Program Files\Google\Compute Engine\metadata_scripts\run_shutdown_scripts.cmd
0Parameters=
'@ | Set-Content -Path $scripts_ini -Encoding ASCII
