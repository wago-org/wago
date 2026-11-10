#!/usr/bin/env bash
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#
# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Wago authors and contributors

set -euo pipefail

output_dir="$(cd "$(dirname "$0")" && pwd)"
curl -fsSL 'https://github.com/holzschu/a-Shell-commands/releases/download/0.1/ctags.wasm' -o "$output_dir/ctags.wasm"
got="$(shasum -a 256 "$output_dir/ctags.wasm" | cut -d ' ' -f 1)"
want=efc2a28f937f081c3a8f3b5518343bc4505b62881758c1d35a054f07e437c413
if [[ "$got" != "$want" ]]; then
  echo "ctags.wasm SHA-256 $got, want $want" >&2
  exit 1
fi
