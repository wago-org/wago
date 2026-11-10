// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package arm64 holds the AArch64-specific half of the railshot single-pass
// backend — the twin of railshot/amd64. It provides the same surface (condense
// engine / instruction selection, linear-memory + bounds lowering, scalar-float
// and NEON v128 lowering, calling convention, table/global ops, magic-division)
// but lowered to AArch64 and driving the arm64 instruction encoder
// (src/core/encoder/arm64).
//
// Because AArch64 is load/store (no memory operands folded into ALU ops), has
// csel/cset/cbz instead of cmov/setcc/Jcc, and materializes constants via
// movz/movk + bitmask immediates, the instruction-selection code diverges from
// amd64 even though it consumes the same neutral core (operand stack, allocator,
// hints, control-flow reconciliation) from the parent railshot package.
//
// Status: scaffolding. Codegen is built out in phases (int/mem/control → calls →
// float → NEON) gated on the wasm spec suite under qemu-user. See the arm64-port
// plan.
package arm64
