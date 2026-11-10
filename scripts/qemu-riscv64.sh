#!/usr/bin/env bash
set -euo pipefail

# Run the RV64G baseline without optional bit-manipulation or vector extensions.
export WAGO_RV64_TEST_RUNNER=qemu
exec "${WAGO_RV64_QEMU:-qemu-riscv64}" \
  -cpu rv64,zba=false,zbb=false,zbc=false,zbs=false,v=false "$@"
