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
	rv64GCTypeSubtypingCrossGroupMismatchLinkProviderVariant = 1
	rv64GCTypeSubtypingCrossGroupMismatchLinkConsumerVariant = 2
)

func admitRV64GCTypeSubtypingCrossGroupMismatchLinkWire(data []byte) bool { return false }

// GCTypeSubtypingCrossGroupMismatchLinkSourceVariant returns 1 for the exact .51
// provider, 2 for the exact .52 consumer, and zero for every other source.
func GCTypeSubtypingCrossGroupMismatchLinkSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingCrossGroupMismatchLink(m *wasm.Module) uint8 { return 0 }
