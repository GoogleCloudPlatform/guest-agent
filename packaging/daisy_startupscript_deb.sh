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

URL="http://metadata/computeMetadata/v1/instance/attributes"
GCS_PATH=$(curl -f -H Metadata-Flavor:Google ${URL}/daisy-outs-path)
SRC_PATH=$(curl -f -H Metadata-Flavor:Google ${URL}/daisy-sources-path)
BASE_REPO=$(curl -f -H Metadata-Flavor:Google ${URL}/base-repo)
REPO=$(curl -f -H Metadata-Flavor:Google ${URL}/repo)
PULL_REF=$(curl -f -H Metadata-Flavor:Google ${URL}/pull-ref)

DEBIAN_FRONTEND=noninteractive

echo "Started build..."

gsutil cp "${SRC_PATH}/common.sh" ./

. common.sh

apt-get -y update
apt-get install -y git-core

git_checkout "$BASE_REPO" "$REPO" "$PULL_REF"

./packaging/build_deb.sh
gsutil cp /tmp/debpackage/google-guest-agent*.deb "${GCS_PATH}/"

echo "Package build success: built `echo /tmp/debpackage/*.deb|xargs -n1 basename`"
