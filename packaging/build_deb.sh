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

DEBIAN_FRONTEND=noninteractive
dpkg_working_dir="/tmp/debpackage"

. packaging/common.sh

# Install dependencies.
# golang-go is installed for dh_helpers; we build with custom go below
apt-get update && apt-get -y install debhelper devscripts build-essential dh-golang dh-systemd golang-go

# Ensure deps are met
dpkg-checkbuilddeps packaging/debian/control

echo "Installing go"
install_go

# Pull go deps
$GO mod download

echo "Building package"
[[ -d $dpkg_working_dir ]] && rm -rf $dpkg_working_dir
mkdir $dpkg_working_dir
tar czvf $dpkg_working_dir/${PKGNAME}_${VERSION}.orig.tar.gz --exclude .git \
  --exclude packaging --transform "s/^\./${PKGNAME}-${VERSION}/" .

working_dir=${PWD}
cd $dpkg_working_dir
tar xzvf ${PKGNAME}_${VERSION}.orig.tar.gz

cd ${PKGNAME}-${VERSION}

cp -r ${working_dir}/packaging/debian ./
cp -r ${working_dir}/*.service ./debian/

debuild -e "VERSION=${VERSION}" -us -uc
