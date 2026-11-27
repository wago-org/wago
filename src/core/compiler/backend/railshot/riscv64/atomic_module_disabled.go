// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo && wago_no_threads

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func atomicHelperSlots(*wasm.Module) int { return 0 }

func appendAtomicHelpers(code []byte, _ []directCall) ([]byte, uint8, error) { return code, 0, nil }

func moduleThreadedMemory(*wasm.Module) bool { return false }
