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

if (Get-Service GCEAgent -ErrorAction SilentlyContinue) {
    Stop-Service GCEAgent -Verbose
    & sc.exe delete GCEAgent
}

$compat_manager = 'GCEWindowsCompatManager'
$name = 'GCEAgentManager'
$cleanup_exe = "C:\Program Files\Google\Compute Engine\agent\ggactl_plugin.exe"

# Stop and Delete compat manager.
if (Get-Service $compat_manager -ErrorAction SilentlyContinue) {
    Stop-Service $compat_manager -Verbose
    & sc.exe delete $compat_manager
}

# Stop Guest Agent Manager, cleanup all plugins (if present) and delete the service.
if (Get-Service $name -ErrorAction SilentlyContinue) {
    Stop-Service $name -Verbose
    & $cleanup_exe coreplugin stop
    & $cleanup_exe dynamic-cleanup
    & sc.exe delete $name
}

