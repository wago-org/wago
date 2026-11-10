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

func admitRV64GCTypeSubtypingRefTestSingleWire(data []byte) bool { return false }

// AdmitGCTypeSubtypingRefTestSingleSource returns the exact-wire provenance
// witness for the admitted recursive ref.test products.
func AdmitGCTypeSubtypingRefTestSingleSource(data []byte) bool { return false }

// GCTypeSubtypingRefTestSingleSourceVariant returns the exact source identity:
// 1 for .20 through 9 for .28, and zero otherwise.
func GCTypeSubtypingRefTestSingleSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingRefTestSingleVariant(m *wasm.Module) uint8 { return 0 }

func admitRV64GCTypeSubtypingRefTestSingle(m *wasm.Module) bool { return false }
