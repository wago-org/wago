// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_no_gc

package wago

func (p stagedGCArrayProduct) String() string { return stagedGCArrayProductName(p) }

func (p stagedGCStructProduct) String() string {
	switch p {
	case stagedGCStructGeneric:
		return "generic"
	case stagedGCStructDeclarations:
		return "declarations"
	case stagedGCStructBindings:
		return "bindings"
	case stagedGCStructNamedGets:
		return "named-field-get"
	case stagedGCStructBasic:
		return "basic-new-get-set"
	case stagedGCStructNullDereference:
		return "null-dereference"
	case stagedGCStructPacked:
		return "packed-fields"
	case stagedGCStructNumericLocal:
		return "numeric-local-helper"
	case stagedGCStructNumericGlobals:
		return "numeric-global-roots"
	case stagedGCStructRefTestTable:
		return "struct-table-ref-test"
	case stagedGCStructRefTestConcrete:
		return "official-concrete-ref-test"
	case stagedGCStructRefTestAbstract:
		return "official-abstract-ref-test"
	case stagedGCStructExtern:
		return "official-extern-conversion"
	case stagedGCStructRefEq:
		return "official-reference-equality"
	case stagedGCStructRefCastAbstract:
		return "official-abstract-reference-cast"
	case stagedGCStructRefCastConcrete:
		return "official-concrete-reference-cast"
	case stagedGCStructBrOnCastAbstract:
		return "official-abstract-branch-on-cast"
	case stagedGCStructBrOnCastConcrete:
		return "official-concrete-branch-on-cast"
	case stagedGCStructBrOnCastNullability:
		return "official-nullability-branch-on-cast"
	case stagedGCStructBrOnCastFailAbstract:
		return "official-abstract-branch-on-cast-fail"
	case stagedGCStructBrOnCastFailConcrete:
		return "official-concrete-branch-on-cast-fail"
	case stagedGCStructBrOnCastFailNullability:
		return "official-nullability-branch-on-cast-fail"
	default:
		return "unknown"
	}
}

func (p stagedGCI31Product) String() string {
	switch p {
	case stagedGCI31ProductCore:
		return "core"
	case stagedGCI31ProductTable:
		return "i31-table"
	case stagedGCI31ProductTableGlobalInitializer:
		return "table-global-initializer"
	case stagedGCI31ProductGlobalInitializer:
		return "global-global-initializer"
	case stagedGCI31ProductAnyGlobal:
		return "anyref-global"
	case stagedGCI31ProductAnyTable:
		return "anyref-table"
	case stagedGCI31ProductRefTest:
		return "ref-test"
	case stagedGCI31ProductRefEqPilot:
		return "ref-eq-pilot"
	default:
		return "unknown"
	}
}
