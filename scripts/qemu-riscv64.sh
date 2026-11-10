#!/usr/bin/env bash
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#
# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Wago authors and contributors

set -euo pipefail

# Run the RV64G baseline without optional bit-manipulation or vector extensions.
export WAGO_RV64_TEST_RUNNER=qemu
exec "${WAGO_RV64_QEMU:-qemu-riscv64}" \
  -cpu rv64,zba=false,zbb=false,zbc=false,zbs=false,v=false "$@"
