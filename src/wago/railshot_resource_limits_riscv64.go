// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !wago_standalone && !tinygo && !wago_target_tinygo

package wago

func railshotConfigureResourceLimits(cfg *RuntimeConfig, opts *railshotCompileOptions) {
	opts.MaxFunctionLocals = cfg.maxFunctionLocals
	opts.MaxModuleBytes = cfg.maxModuleBytes
	opts.MaxNativeCodeBytes = cfg.maxNativeCodeBytes
	opts.MaxCompiledMetadataBytes = normalizeSnapshotLimit(cfg.maxCompiledMetadataBytes)
}
