// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && !windows

package wagobench

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func compileWorkerDiagnosticBackend(mod *wasm.Module, workers int) error {
	compiled, err := railshot.CompileModuleWith(mod, railshot.CompileOptions{Workers: workers})
	if err != nil {
		return err
	}
	if compiled.CodeImage != nil {
		return compiled.CodeImage.Close()
	}
	return nil
}
