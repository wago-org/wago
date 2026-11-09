// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && wago_regalloccheck

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

// Joining workers precedes releasing facts, including scratch retained by a
// custom lowering callback. This scope starts after public option validation.
func compileSourceModuleWith(m *wasm.Module, opts CompileOptions, validatedGCStores bool) (*encoder.CompiledModule, error) {
	defer codegen.CloseSourceContext(opts.Codegen, m)
	cm, err := compileModuleWithValidation(m, opts, validatedGCStores)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep && cm != nil && cm.CodeImage != nil {
			_ = cm.CodeImage.Close()
		}
	}()
	if err := checkSourceCallsFinal(m, opts, cm); err != nil {
		return nil, err
	}
	checkSourceIntegerFinal(m, opts, cm)
	keep = true
	return cm, nil
}
