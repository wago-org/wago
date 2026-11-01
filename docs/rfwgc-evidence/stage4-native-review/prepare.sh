#!/usr/bin/env bash
set -euo pipefail
here=$(cd -- "$(dirname -- "$0")" && pwd)
repo=$(cd "$here/../../.." && pwd)
# Do not overwrite an existing export or mix concurrent production into it.
mkdir "$here/snapshot" "$here/baseline"
git -C "$repo" archive 98939b4dd875efbcda6dd53ba769ad25dec2b4f1 | tar -x -C "$here/snapshot"
git -C "$repo" archive 5b31c81ec0c74695a90bc8d36754b92845b59f14 | tar -x -C "$here/baseline"
for file in src/core/runtime/gc/native/native_review_stage4_test.go src/wago/native_review_stage4_amd64_test.go src/wago/native_review_stage4_reentry_amd64_test.go; do
 cp "$repo/$file" "$here/snapshot/$file"
done
