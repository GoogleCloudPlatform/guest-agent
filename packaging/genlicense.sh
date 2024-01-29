#!/bin/sh
# Run ./packaging/genlicense.sh from repo root to generate THIRD_PARTY_LICENSES directory
set -e

echo "Clean existing licenses"
rm -rf THIRD_PARTY_LICENSES
mkdir THIRD_PARTY_LICENSES

echo "Generate linux licenses"
GOOS=linux go-licenses save --save_path ./linux_licenses/ ./... --force
echo "Merge linux licenses"
for file in $(find ./linux_licenses -type f); do
        mkdir -p $(dirname $file | sed 's/linux_licenses/THIRD_PARTY_LICENSES/')
        cp -f $file $(echo $file | sed 's/linux_licenses/THIRD_PARTY_LICENSES/')
done
echo "Generate windows licenses"
GOOS=windows go-licenses save --save_path ./windows_licenses/ ./... --force
echo "Merge windows licenses"
for file in $(find ./windows_licenses -type f); do
        mkdir -p $(dirname $file | sed 's/windows_licenses/THIRD_PARTY_LICENSES/')
        cp -f $file $(echo $file | sed 's/windows_licenses/THIRD_PARTY_LICENSES/')
done

rm -rf ./linux_licenses ./windows_licenses
