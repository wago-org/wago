// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && wago_standalone

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime/hostthunk"
)

type railshotCompiledModule = encoder.CompiledModule

func railshotCompileModuleWith(*wasm.Module, railshotCompileOptions) (*railshotCompiledModule, error) {
	return nil, fmt.Errorf("source compilation is unavailable in a standalone executable")
}

func railshotHostIndirectThunk(importIdx uint32) []byte {
	return hostthunk.Indirect(importIdx)
}

func railshotHostIndirectSyncThunk(importIdx uint32, paramSlots, resultSlots int) []byte {
	return hostthunk.IndirectSync(importIdx, paramSlots, resultSlots)
}

func railshotHostIndirectOwnedSyncThunk(importIdx uint32, paramSlots, resultSlots int) []byte {
	return hostthunk.IndirectOwnedSync(importIdx, paramSlots, resultSlots)
}
