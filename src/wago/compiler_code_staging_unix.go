// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64 && (((darwin || linux) && !tinygo) || ((darwin || linux) && tinygo && wago_tinygo_finalizers && (gc.conservative || gc.precise)))

package wago

import coreruntime "github.com/wago-org/wago/src/core/runtime"

// wago_tinygo_finalizers is for TinyGo SDKs that implement SetFinalizer for
// the block-based collectors. Compiled.Close is optional, so deferred native
// storage needs the same finalizer ownership as executable mappings.
const compilerCodeStaging = true

type compilerCodeStage struct {
	codeStaging []byte
}

func (c *Compiled) retainCodeStaging(mapping []byte) {
	c.ensureCompileIndexes().codeStaging = mapping
}

func (c *Compiled) releaseCodeStagingLocked() error {
	indexes := c.loadCompileIndexes()
	if indexes == nil || indexes.codeStaging == nil {
		return nil
	}
	if err := coreruntime.Unmap(indexes.codeStaging); err != nil {
		return err
	}
	indexes.codeStaging = nil
	return nil
}

func (c *Compiled) stagingCodeMapping() []byte {
	if indexes := c.loadCompileIndexes(); indexes != nil {
		return indexes.codeStaging
	}
	return nil
}

func (c *Compiled) takeCodeStagingLocked() []byte {
	if indexes := c.loadCompileIndexes(); indexes != nil {
		mem := indexes.codeStaging
		indexes.codeStaging = nil
		return mem
	}
	return nil
}
