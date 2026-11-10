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
	rv64GCTypeSubtypingExtendedRecursiveLinkProviderVariant = 1
	rv64GCTypeSubtypingExtendedRecursiveLinkConsumerVariant = 2
)

func admitRV64GCTypeSubtypingExtendedRecursiveLinkWire(data []byte) bool { return false }

// GCTypeSubtypingExtendedRecursiveLinkSourceVariant returns 1 for the exact
// .49 provider, 2 for the exact .50 consumer, and zero otherwise.
func GCTypeSubtypingExtendedRecursiveLinkSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingExtendedRecursiveLink(m *wasm.Module) uint8 { return 0 }
