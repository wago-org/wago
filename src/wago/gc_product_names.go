// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

func stagedGCArrayProductName(p stagedGCArrayProduct) string {
	switch p {
	case stagedGCArrayProductDeclarations:
		return "declarations"
	case stagedGCArrayProductBindings:
		return "bindings"
	case stagedGCArrayProductNumericDefault:
		return "numeric-default"
	case stagedGCArrayProductNumericFixed:
		return "numeric-fixed"
	case stagedGCArrayProductPackedData:
		return "packed-data"
	case stagedGCArrayProductReferenceElements:
		return "reference-elements"
	case stagedGCArrayProductNullDereference:
		return "null-dereference"
	case stagedGCArrayProductNumericLocal:
		return "numeric-local-helper"
	case stagedGCArrayProductNumericUniform:
		return "numeric-uniform-helper"
	case stagedGCArrayProductNumericFixedLocal:
		return "numeric-fixed-local-helper"
	case stagedGCArrayProductBulkFill:
		return "bulk-fill"
	case stagedGCArrayProductBulkCopy:
		return "bulk-copy"
	case stagedGCArrayProductInitData:
		return "init-data"
	case stagedGCArrayProductInitElem:
		return "init-elem"
	case stagedGCArrayProductNewData:
		return "new-data"
	case stagedGCArrayProductNewElem:
		return "new-elem"
	case stagedGCArrayProductGeneric:
		return "generic"
	default:
		return "unknown"
	}
}
