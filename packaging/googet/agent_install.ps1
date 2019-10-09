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

if (-not (Get-Service 'GCEAgent' -ErrorAction SilentlyContinue)) {
  New-Service -Name 'GCEAgent' -BinaryPathName '"C:\Program Files\Google\Compute Engine\agent\GCEWindowsAgent.exe"' -StartupType Automatic -Description 'Google Compute Engine Agent'
}

$config = "${env:ProgramFiles}\Google\Compute Engine\instance_configs.cfg"
if (-not (Test-Path $config)) {
  @'
# GCE Instance Configuration

# For details on what can be configured, see:
# https://cloud.google.com/compute/docs/instances/windows/creating-managing-windows-instances#configure-windows-features

# [accountManager]
# disable=false

# [addressManager]
# disable=false
'@ | Set-Content -Path $config -Encoding ASCII
}

Restart-Service GCEAgent -Verbose
