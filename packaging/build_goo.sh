#!/bin/bash
# Copyright 2019 Google Inc. All Rights Reserved.
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

set -e

. packaging/common.sh

echo "Installing go"
install_go

# Pull go deps
$GO mod download

# Install dependencies.
$GO get github.com/google/googet/v2/goopack

echo "Building package"
goopack -var:version=${VERSION} packaging/googet/google-compute-engine-windows.goospec
