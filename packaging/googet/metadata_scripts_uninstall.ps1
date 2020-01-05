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
if ($path -like "*${install_dir}*") {
  Set-ItemProperty $machine_env -Name 'Path' -Value $path.Replace(";$install_dir", '')
}

& schtasks /delete /tn GCEStartup /f
