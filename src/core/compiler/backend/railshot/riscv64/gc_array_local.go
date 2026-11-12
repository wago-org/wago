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

func admitRV64GCArrayLocalWire(data []byte) bool { return false }

// AdmitGCArrayLocalSource is the wire-provenance half of the exact numeric
// array.new_default/get/set/len checkpoint.
func AdmitGCArrayLocalSource(data []byte) bool { return false }

func admitRV64GCArrayLocal(m *wasm.Module) bool { return false }
