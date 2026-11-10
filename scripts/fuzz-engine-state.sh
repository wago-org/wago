#!/bin/sh
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#
# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Wago authors and contributors

set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
starshine_wasm=${STARSHINE_FFI_WASM:-"$repo_dir/tests/fuzz/engine/starshine-ffi.wasm"}
worker_dir="$repo_dir/.tmp/engine-state"
worker="$worker_dir/railshot-worker"

if [ ! -f "$starshine_wasm" ]; then
	echo "engine-state fuzz: Starshine FFI not found: $starshine_wasm" >&2
	echo "Restore the tracked artifact or set STARSHINE_FFI_WASM to another build" >&2
	exit 1
fi

mkdir -p "$worker_dir"
cd "$repo_dir"
go build -tags=wago_regalloccheck -o "$worker" ./tests/fuzz/engine/worker
exec node scripts/fuzz-engine-state.mjs \
	--worker "$worker" \
	--starshine "$starshine_wasm" \
	"$@"
