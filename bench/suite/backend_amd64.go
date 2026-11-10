// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package wagobench

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func benchCompileModule(m *wasm.Module) (*benchCompiledModule, error) {
	return benchCompileModuleWorkers(m, 1)
}

func benchCompileModuleWorkers(m *wasm.Module, workers int) (*benchCompiledModule, error) {
	cm, err := railshot.CompileModuleWith(m, railshot.CompileOptions{Workers: workers})
	if err != nil {
		return nil, err
	}
	return &benchCompiledModule{Code: cm.Code, Entry: cm.Entry, image: cm.CodeImage}, nil
}

func benchCompileModuleCompact(m *wasm.Module) (*benchCompiledModule, error) {
	cm, err := railshot.CompileModuleWith(m, railshot.CompileOptions{Workers: 1, CompactNative: true})
	if err != nil {
		return nil, err
	}
	return &benchCompiledModule{Code: cm.Code, Entry: cm.Entry, image: cm.CodeImage}, nil
}
