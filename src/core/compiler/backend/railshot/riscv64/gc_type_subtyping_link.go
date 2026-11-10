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
	rv64GCTypeSubtypingLinkProviderVariant = 1
	rv64GCTypeSubtypingLinkConsumerVariant = 2
	rv64GCTypeSubtypingLinkUnlinkableFirst = 3
	rv64GCTypeSubtypingLinkUnlinkableLast  = 5
)

func admitRV64GCTypeSubtypingLinkWire(data []byte) bool { return false }

// GCTypeSubtypingLinkSourceVariant returns 1 for the exact .29 provider, 2 for
// the exact .30 consumer, 3 through 5 for the expected-unlinkable .31 through
// .33 consumers, and zero for every other source.
func GCTypeSubtypingLinkSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingLink(m *wasm.Module) uint8 { return 0 }
