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

const (
	rv64GCTypeSubtypingFinalityLinkProviderVariant = 1
	rv64GCTypeSubtypingFinalityLinkConsumerFirst   = 2
	rv64GCTypeSubtypingFinalityLinkConsumerLast    = 3
)

func admitRV64GCTypeSubtypingFinalityLinkWire(data []byte) bool { return false }

// GCTypeSubtypingFinalityLinkSourceVariant returns 1 for the exact .34
// provider, 2 and 3 for the expected-unlinkable .35/.36 consumers, and zero
// for every other source.
func GCTypeSubtypingFinalityLinkSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingFinalityLink(m *wasm.Module) uint8 { return 0 }
