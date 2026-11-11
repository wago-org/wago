// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCTypeSubtypingDeclarationsWire(data []byte) bool { return false }

// AdmitGCTypeSubtypingDeclarationsSource reports whether data is the exact
// initial declaration-only gc/type-subtyping product.
func AdmitGCTypeSubtypingDeclarationsSource(data []byte) bool { return false }

// GCTypeSubtypingDeclarationsSourceVariant returns the exact source variant:
// 1 through 6 for the corresponding pinned declaration module, or zero for
// every other shape.
func GCTypeSubtypingDeclarationsSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingDeclarations(m *wasm.Module) uint8 { return 0 }
