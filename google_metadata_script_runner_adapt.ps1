# Copyright 2025 Google Inc. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

<#
  .SYNOPSIS
    Metadata Script Runner Adapt.

  .DESCRIPTION
    This script wraps compatibility logic of guest agent's startup script
    runner. If compat manager is present run it, otherwise launch the
    known service binary.

   .EXAMPLE
    .\google_metadata_script_runner_adapt.ps1 <startup|shutdown|specialize>
#>

#requires -version 3.0

param (
    [Parameter(Position=0)]
    [string]$phase
)

$script:gce_install_dir = 'C:\Program Files\Google\Compute Engine'
$script:orig_runner = "$script:gce_install_dir\metadata_scripts\GCEMetadataScripts.exe"
$script:metadata_script_loc = $script:orig_runner
$script:compatRunner = "$script:gce_install_dir\metadata_scripts\GCECompatMetadataScripts.exe"
$script:runnerV2 = "$script:gce_install_dir\agent\GCEMetadataScriptRunner.exe"

if (Test-Path $script:runnerV2) {
    $script:metadata_script_loc = $script:runnerV2
}

if (Test-Path $script:compatRunner) {
    $script:metadata_script_loc = $script:compatRunner
}

Write-Host "Launching metadata scripts from $script:metadata_script_loc for $phase"
# Call startup script during sysprep specialize phase.
& $script:metadata_script_loc $phase