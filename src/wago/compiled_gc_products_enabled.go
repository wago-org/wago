// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_no_gc

package wago

type compiledGCProductState struct{ compiledGCProducts }

func (cc *compiledCodeCache) setGCProducts(products compiledGCProducts) {
	cc.compiledGCProducts = products
	if products != (compiledGCProducts{}) {
		cc.stagedFeatures |= CoreFeatureGC
	}
	if products.gcArrayProduct.metadataOnly() {
		cc.gcMetadataFlags |= compiledGCMetadataCollectorFreeArray
	}
}

func (cc *compiledCodeCache) hasGCObjectProducts() bool {
	return cc.gcStructProduct != 0 || cc.gcArrayProduct != 0
}

func (c *Compiled) stagedGCTypeSubtypingProduct() stagedGCTypeSubtypingProduct {
	cc := c.loadCodeCache()
	if cc == nil {
		return 0
	}
	return cc.gcTypeSubtypingProduct
}

func (c *Compiled) usesGCStructHelpers() bool {
	return c != nil && c.stagedGCStructProduct().requiresHelpers()
}

func (c *Compiled) usesGCArrayHelpers() bool {
	return c != nil && gcArrayHelpersRequired(c.stagedGCArrayProduct(), c.stagedGCStructProduct())
}

func (c *Compiled) collectorFreeGCArrayMetadata() bool {
	cc := c.loadCodeCache()
	return cc != nil && cc.gcMetadataFlags&compiledGCMetadataCollectorFreeArray != 0
}

func (c *Compiled) stagedGCStructProduct() stagedGCStructProduct {
	cc := c.loadCodeCache()
	if cc == nil {
		return 0
	}
	return cc.gcStructProduct
}

func (c *Compiled) stagedGCArrayProduct() stagedGCArrayProduct {
	cc := c.loadCodeCache()
	if cc == nil {
		return 0
	}
	return cc.gcArrayProduct
}

func (c *Compiled) usesGenericGCExecution() bool {
	if c == nil {
		return false
	}
	arrayProduct := c.stagedGCArrayProduct()
	return c.stagedGCStructProduct() == stagedGCStructGeneric || arrayProduct == stagedGCArrayProductNewData || arrayProduct == stagedGCArrayProductNewElem || arrayProduct == stagedGCArrayProductGeneric
}

func (c *Compiled) stagedGCI31Product() stagedGCI31Product {
	cc := c.loadCodeCache()
	if cc == nil {
		return 0
	}
	return cc.gcI31Product
}
