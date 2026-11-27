// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

// Import-only relays retain a mapped cache and native import thunks, but have
// no executable guest span. Owner-graph validation may accept that empty view;
// tail certificates still require the nonempty NativeTailCodeView contract.
func rv64PublishedCodeView(in *Instance, allowEmpty bool) ([]byte, error) {
	if in == nil {
		return nil, fmt.Errorf("%w: RV64 published image has no instance", railshot.ErrUnsupported)
	}
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.resourcesClosed || in.c == nil || in.jm == nil || in.ar == nil || in.base == 0 || in.nativeContext == 0 || in.resourceRefs == 0 && in.invocationState.Load()&instanceInvocationCount == 0 {
		return nil, fmt.Errorf("%w: RV64 published image has no physical instance lease", railshot.ErrUnsupported)
	}
	cc := in.c.loadCodeCache()
	if cc == nil {
		return nil, fmt.Errorf("%w: RV64 published image has no code cache", railshot.ErrUnsupported)
	}
	cc.mu.Lock()
	defer cc.mu.Unlock()
	b := in.c.code
	indexes := in.c.loadCompileIndexes()
	// Closing the execution snapshot itself clears its readable code slice,
	// while the particular instance's cache reference keeps the RX mapping live.
	// Recover only that mapping's recorded logical span, never page padding.
	if cc.closed && b == nil && indexes != nil {
		n := indexes.railshotTail.mappedCodeBytes
		if n > 0 && n <= len(cc.mem) {
			b = cc.mem[:n:n]
		}
	}
	// Logical Close is allowed: this particular instance still owns its job,
	// arena/context and code reference. A shared cache's refs alone cannot prove
	// that those resources survive for this instance.
	if !cc.sealed || cc.refs <= 0 || cc.base != in.base || indexes == nil || len(b) != indexes.railshotTail.mappedCodeBytes || len(b) > len(cc.mem) || offHeapSlicePtr(cc.mem) != cc.base {
		return nil, fmt.Errorf("%w: RV64 published image differs from sealed code cache", railshot.ErrUnsupported)
	}
	if len(b) == 0 {
		if allowEmpty && in.c.NumImports > 0 && len(in.c.Funcs) == 0 && len(in.c.Entry) == 0 && len(in.c.InternalEntry) == 0 && !in.c.nativeReferenceFunctionABI && !in.c.nativeReferenceCallABI && !in.c.nativeReferenceControlABI && rv64NativeTailModule(in.c) == nil {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: RV64 owner has no executable guest span", railshot.ErrUnsupported)
	}
	if offHeapSlicePtr(b) != cc.base {
		return nil, fmt.Errorf("%w: RV64 published code has a different base", railshot.ErrUnsupported)
	}
	return b[:len(b):len(b)], nil
}
