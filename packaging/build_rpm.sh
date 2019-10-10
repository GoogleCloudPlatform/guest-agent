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

rpm_working_dir=/tmp/rpmpackage/

. packaging/common.sh

# Install dependencies.
yum -y install rpmdevtools

echo "Installing go"
install_go

# Pull go deps
$GO mod download

rm -rf /tmp/rpmpackage
mkdir -p ${rpm_working_dir}/{SOURCES,SPECS}

cp packaging/${PKGNAME}.spec ${rpm_working_dir}/SPECS/

tar czvf ${rpm_working_dir}/SOURCES/${PKGNAME}_${VERSION}.orig.tar.gz \
  --exclude .git --exclude packaging --transform "s/^\./${PKGNAME}-${VERSION}/" .

rpmbuild --define "_topdir ${rpm_working_dir}/" --define "_version ${VERSION}" \
  --define "_go ${GO}" --define "_arch x86_64" --define "_gopath ${GOPATH}" \
  -ba ${rpm_working_dir}/SPECS/${PKGNAME}.spec
