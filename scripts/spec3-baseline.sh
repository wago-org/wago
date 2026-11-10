#!/bin/sh
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#
# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Wago authors and contributors

set -u

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
out=${1:-"$repo/tests/conformance/baselines/spec-v3-baseline.json"}
mkdir -p "$repo/.validation"
log="$repo/.validation/spec3-baseline.log"

set +e
just --justfile "$repo/justfile" test spec v3 >"$log" 2>&1
status=$?
set -e

python3 "$repo/scripts/spec3-baseline.py" "$log" "$out" --exit-code "$status"
cat "$log"
echo "spec3-baseline: wrote $out (just test spec v3 exit $status)" >&2
exit "$status"
