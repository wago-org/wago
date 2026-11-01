#!/usr/bin/env bash
set -euo pipefail
revision=$1
export_root=$(realpath "$2")
for version in v1 v2; do
 path=tests/conformance/spec-$version
 pin=$(git ls-tree "$revision" "$path" | awk '{print $3}')
 if [ ! -d "$export_root/$path/.git" ]; then
   git clone --no-checkout "$PWD/$path" "$export_root/$path"
 fi
 git -C "$export_root/$path" checkout --detach "$pin"
done
