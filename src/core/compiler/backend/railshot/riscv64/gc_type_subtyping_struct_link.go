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
	rv64GCTypeSubtypingStructLinkProviderVariant = 1
	rv64GCTypeSubtypingStructLinkConsumerVariant = 2
)

func admitRV64GCTypeSubtypingStructLinkWire(data []byte) bool { return false }

// GCTypeSubtypingStructLinkSourceVariant returns 1 for the exact .37
// provider, 2 for the exact .38 consumer, and zero for every other source.
func GCTypeSubtypingStructLinkSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingStructLink(m *wasm.Module) uint8 { return 0 }
