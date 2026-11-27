// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_no_gc

package wago

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Initializers are populated by source compilation and owned by the compiled memory directory.
type compiledGCInitializers struct {
	structGlobals []gcStructGlobalInit
	arrayGlobals  []gcArrayGlobalInit
	arrayElement  *gcArrayElementInit
}

func (m *compiledGCInitializers) gcStructGlobals() []gcStructGlobalInit { return m.structGlobals }

func (m *compiledGCInitializers) gcArrayGlobals() []gcArrayGlobalInit { return m.arrayGlobals }

func (m *compiledGCInitializers) gcArrayElement() *gcArrayElementInit { return m.arrayElement }

func (m *compiledGCInitializers) hasGCInitializers() bool {
	return len(m.structGlobals) != 0 || len(m.arrayGlobals) != 0 || m.arrayElement != nil
}

func (c *Compiled) compileGCInitializers(m *wasm.Module, gcStructProduct stagedGCStructProduct, gcArrayProduct stagedGCArrayProduct) error {
	if gcStructProduct != 0 && gcStructProduct != stagedGCStructGeneric {
		gcGlobals, err := stagedGCStructGlobalInitializers(m)
		if err != nil {
			return wrapContextError("compile: staged GC struct globals", err)
		}
		c.memoryDir.structGlobals = gcGlobals
	}
	if gcArrayProduct != 0 {
		if gcArrayProduct != stagedGCArrayProductNewData && gcArrayProduct != stagedGCArrayProductNewElem && gcArrayProduct != stagedGCArrayProductGeneric {
			gcGlobals, err := stagedGCArrayGlobalInitializers(m, gcArrayProduct)
			if err != nil {
				return wrapContextError("compile: staged GC array globals", err)
			}
			c.memoryDir.arrayGlobals = gcGlobals
		}
		if gcArrayProduct == stagedGCArrayProductReferenceElements {
			init, err := stagedGCArrayElementInitializer(m)
			if err != nil {
				return wrapContextError("compile: staged GC array element segment", err)
			}
			c.memoryDir.arrayElement = init
		}
	}
	return nil
}
