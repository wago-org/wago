#!/usr/bin/env bash
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#
# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Wago authors and contributors

set -euo pipefail

# age v1.3.2, commit b74dce4cdbe35b5e5f66c06d9612b72f89028758.
source_dir="${1:?pass a clean age checkout at the pinned revision}"
output_dir="$(cd "$(dirname "$0")" && pwd)"
if [[ "$(git -C "$source_dir" rev-parse HEAD)" != b74dce4cdbe35b5e5f66c06d9612b72f89028758 ]]; then
  echo 'age checkout is not at the pinned revision' >&2
  exit 1
fi
if [[ -n "$(git -C "$source_dir" status --porcelain)" ]]; then
  echo 'age checkout must be clean' >&2
  exit 1
fi
if [[ "$(cd "$source_dir" && go version)" != 'go version go1.27.0 '* ]]; then
  echo 'build with Go 1.27.0' >&2
  exit 1
fi
(
  cd "$source_dir"
  CGO_ENABLED=0 GOOS=wasip1 GOARCH=wasm GOFLAGS=-mod=readonly \
    go build -trimpath -o "$output_dir/age.wasm" ./cmd/age
  CGO_ENABLED=0 GOOS=wasip1 GOARCH=wasm GOFLAGS=-mod=readonly \
    go build -trimpath -o "$output_dir/age-keygen.wasm" ./cmd/age-keygen
)
shasum -a 256 "$output_dir/age.wasm" "$output_dir/age-keygen.wasm"
