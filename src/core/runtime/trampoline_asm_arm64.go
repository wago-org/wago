// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && arm64 && !tinygo

package runtime

// enterNative is implemented in trampoline_arm64.s for the standard Go
// toolchain. It switches to the engine's foreign stack, enters the arm64
// WasmWrapper with X0=serArgs, X1=linMem, X2=trap, X3=results, then restores the
// Go context.
func enterNativeRaw(code, serArgs, linMem, trap, results, foreignStackTop uintptr)

func (e *Engine) initNativeEntry() error { return nil }

// resumeNative is implemented in resume_arm64.s. It restores the wasm state
// parked by hostCallStub and returns to the instruction after the host CALL.
func resumeNativeRaw(ctrl, foreignStackTop uintptr)
