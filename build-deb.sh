#!/bin/env bash
#
# This script wraps the steps required to locally build a deb package
# based on the in tree debian packaging (see invocation example bellow).
#
# After the build is finished the script will print out where the final
# artifact was generated.
#
# Invocation:
# VERSION=<version> RELEASE=<release> ./build-deb.sh
#

BUILD_DIR=$(mktemp -d)
PKGNAME="google-guest-agent"

if [ -z "${VERSION}" ]; then
   echo "VERSION environment variable is not set"
   exit 1
fi

if [ -z "${RELEASE}" ]; then
   echo "RELEASE environment variable is not set"
   exit 1
fi

TARBALL="${PKGNAME}_${VERSION}.orig.tar.gz"

echo "Creating tarball: ${TARBALL}"
tar czvf "${BUILD_DIR}/${TARBALL}" --exclude .git --exclude packaging \
  --transform "s/^\./${PKGNAME}-${VERSION}/" .

tar -C "$BUILD_DIR" -xzvf "${BUILD_DIR}/${TARBALL}"
PKGDIR="${BUILD_DIR}/${PKGNAME}-${VERSION}"

cp -r packaging/debian "${BUILD_DIR}/${PKGNAME}-${VERSION}/"

cd "${BUILD_DIR}/${PKGNAME}-${VERSION}"

# We generate this to enable auto-versioning.
[[ -f debian/changelog ]] && rm debian/changelog

dch --create -M -v 1:${VERSION}-${RELEASE} --package $PKGNAME -D stable \
  "Debian packaging for ${PKGNAME}"

DEB_BUILD_OPTIONS="noautodbgsym nocheck" debuild -e "VERSION=${VERSION}" -e "RELEASE=${RELEASE}" -us -uc

echo "Package built at: ${BUILD_DIR}"
