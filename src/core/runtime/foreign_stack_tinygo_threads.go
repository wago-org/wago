// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build tinygo && scheduler.threads && ((linux && amd64) || ((linux || darwin) && arm64))

package runtime

import "unsafe"

// These hooks require scripts/patch-tinygo-042.sh. An unpatched toolchain fails
// to link rather than silently running with an unsafe conservative stack scan.
// Registration covers the mapping's entire lifetime, including parked host
// calls, reentry, prepared entries, trap unwinding and interrupted transitions.
// The engine lease must exclude Close from every active/parked native call.
//
//go:linkname registerTinyGoForeignStack runtime.wagoRegisterForeignStack
func registerTinyGoForeignStack(low, high, hostSP uintptr) unsafe.Pointer

//go:linkname unregisterTinyGoForeignStack runtime.wagoUnregisterForeignStack
func unregisterTinyGoForeignStack(token unsafe.Pointer)
