// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_no_gc

package wago

type compiledGCProductState struct{}

func (*compiledCodeCache) setGCProducts(compiledGCProducts) {}

func (*compiledCodeCache) hasGCObjectProducts() bool { return false }

func (*Compiled) stagedGCTypeSubtypingProduct() stagedGCTypeSubtypingProduct { return 0 }
func (*Compiled) stagedGCStructProduct() stagedGCStructProduct               { return 0 }
func (*Compiled) stagedGCArrayProduct() stagedGCArrayProduct                 { return 0 }
func (*Compiled) stagedGCI31Product() stagedGCI31Product                     { return 0 }

func (*Compiled) usesGCStructHelpers() bool          { return false }
func (*Compiled) usesGCArrayHelpers() bool           { return false }
func (*Compiled) usesGenericGCExecution() bool       { return false }
func (*Compiled) collectorFreeGCArrayMetadata() bool { return false }
