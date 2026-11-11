// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_regalloccheck

package codegen

import "github.com/wago-org/wago/src/core/compiler/wasm"

const SourceChecks = false

// Options are shared code-generation dependencies selected by the caller after
// frontend validation and runtime configuration normalization.
type Options struct {
	Runtime RuntimeABI
	Heap    HeapABI
	Module  ModuleInfo
}

type SourceContext struct{}

func SourceOptions(opts Options, _ *wasm.Module, _ *wasm.ValidatedModuleAnalysis, _ wasm.ValidationFeatures) Options {
	return opts
}

func AttachSourceContext(*Options, *wasm.Module, *wasm.ValidatedModuleAnalysis, wasm.ValidationFeatures) bool {
	return false
}
func SourceContextFor(Options, *wasm.Module) *SourceContext { return nil }
func ValidatedSourceContext(*SourceContext, *wasm.Module) (*wasm.ValidatedModuleAnalysis, wasm.ValidationFeatures, bool) {
	return nil, wasm.ValidationFeatures{}, false
}
func CloseSourceContext(Options, *wasm.Module) {}
