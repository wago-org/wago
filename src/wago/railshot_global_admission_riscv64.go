// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"

type rv64GlobalAdmission struct {
	primitiveReferences bool
	freshRootedGC       bool
	immediateReferences bool
	typedI31            bool
	exactAnyI31         bool
	mutableImports      bool
	mutableLocalCall    bool
}

func rv64GlobalImportError(c *Compiled, primitiveReferences, freshRootedGC, mutableImports bool) string {
	vectorProfile := c.requiredFeatures.IsEnabled(CoreFeatureSIMD)
	if len(c.GlobalImports) > len(c.Globals) {
		return "global import index shape"
	}
	for i, global := range c.GlobalImports {
		funcref := global.Type == ValFuncRef && (primitiveReferences || !global.Mutable || mutableImports) && (primitiveReferences || c.tableCount() <= 1)
		externRef := (primitiveReferences || freshRootedGC && c.compactExternFrameRoots()) && global.Type == ValExternRef
		collectorRef := freshRootedGC && isGCRefValType(global.Type)
		reference := funcref || externRef || collectorRef
		if funcref || externRef {
			plain, _ := valueTypeDescriptorFromValType(global.Type)
			exact, err := exactValueType(global.Type, global.HasValueType, global.ValueTypeIndex, c.ValueTypes, c.Types)
			if err != nil || exact != plain && !(primitiveReferences && rv64PrimitiveReferenceValue(c, exact)) {
				return "reference global import exact type"
			}
		} else if collectorRef {
			exact, err := exactValueType(global.Type, global.HasValueType, global.ValueTypeIndex, c.ValueTypes, c.Types)
			if err != nil || exact.Kind != ValueTypeReference {
				return "collector-reference global import exact type"
			}
		}
		if (!reference && global.Type != ValI32 && global.Type != ValI64 && global.Type != ValF32 && global.Type != ValF64 && (!vectorProfile || global.Type != ValV128)) || global.Type != c.Globals[i].Type || global.Mutable != c.Globals[i].Mutable {
			return "global import type/mutability shape"
		}
	}
	return ""
}

func rv64GlobalDeclarationError(c *Compiled, legacyGC *rv64LegacyGCProducts, profile rv64GlobalAdmission) string {
	vectorProfile := c.requiredFeatures.IsEnabled(CoreFeatureSIMD)
	var localMutableReferences bool
	if referenceSignaturesBuilt {
		var err error
		localMutableReferences, err = rv64ValidateLocalMutableReferences(c)
		if err != nil {
			return err.Error()
		}
	}
	hasReferenceGlobal := false
	for i, global := range c.Globals {
		if profile.freshRootedGC && (isGCRefValType(global.Type) || global.Type == ValFuncRef || global.Type == ValExternRef) {
			continue // The generic receipt pins exact nullable aggregate roots.
		}
		if legacyGC.typeSubtypingRefFuncGlobals {
			continue // The exact metadata digest pins every indexed ref.func global.
		}
		if legacyGC.extern && i < 2 {
			continue // The exact product pins both null conversion globals.
		}
		if legacyGC.structPacked && i < 2 {
			continue // The exact product pins both rooted non-null struct initializers.
		}
		if legacyGC.structBasic && i < 2 {
			continue // The exact product pins both rooted non-null struct initializers.
		}
		if legacyGC.structNumericGlobals && i < 2 {
			continue // The exact state-only product pins both rooted non-null struct initializers.
		}
		if legacyGC.arrayFixedOfficial && i == 0 || legacyGC.arrayDefaultOfficial && i < 2 || legacyGC.arrayInitElem && i < 2 || legacyGC.arrayInitData && i < 3 || legacyGC.arrayFill && i < 2 || legacyGC.arrayCopy && i < 2 {
			continue // The exact product pins this rooted non-null array initializer.
		}
		if wasmGCBuilt && profile.immediateReferences && isGCRefValType(global.Type) {
			exact, err := c.globalExactType(i)
			if err != nil || !rv64ImmediateCollectorValue(exact) || global.HasInitFunc || global.InitFunc != 0 || global.V128 != (V128{}) || global.Bits>>32 != 0 || global.Bits != 0 && global.Bits&1 == 0 {
				return "immediate reference global metadata"
			}
			if len(global.InitExpr) != 0 {
				if i < len(c.GlobalImports) || global.HasInitGlobal || c.validateGCConstExpr(global.InitExpr, exact, i) != nil {
					return "immediate reference global initializer"
				}
			} else if global.HasInitGlobal && !rv64InitializerReferenceGlobal(c, global.InitGlobal, exact, i) {
				return "immediate reference global source"
			}
			continue
		}
		if profile.typedI31 && (global.Type == ValI31Ref || profile.exactAnyI31 && global.Type == ValAnyRef) {
			continue // The exact product witness pins both globals and their initial values.
		}
		if global.Type == ValExternRef {
			if profile.primitiveReferences {
				exact, err := c.globalExactType(i)
				if err != nil || !rv64PrimitiveReferenceValue(c, exact) || global.HasInitFunc || global.InitFunc != 0 || global.Bits != 0 || global.V128 != (V128{}) || len(global.InitExpr) != 0 || i < len(c.GlobalImports) && global.HasInitGlobal || global.HasInitGlobal && !rv64InitializerGlobal(c, global.InitGlobal, ValExternRef, i) {
					return "plain external global initializer/type metadata"
				}
				continue
			}
			return "externref globals require the exact state-only null directory"
		}
		if global.Type == ValFuncRef {
			hasReferenceGlobal = true
			plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
			exact, err := c.globalExactType(i)
			typedIndex, typed := rv64PrivateIndexedFunctionType(c, exact)
			callTyped := profile.mutableLocalCall && i == 0 && exact.Kind == ValueTypeReference && exact.Ref.Nullable && !exact.Ref.Exact && exact.Ref.Heap.Defined && exact.Ref.Heap.Abstract == 0 && exact.Ref.Heap.TypeIndex == 0
			mutableQualified := profile.primitiveReferences || localMutableReferences && i >= len(c.GlobalImports) || (profile.mutableImports) && i < len(c.GlobalImports)
			primitiveTyped := profile.primitiveReferences && rv64PrimitiveReferenceValue(c, exact)
			if err != nil || exact != plain && !typed && !callTyped && !primitiveTyped || global.Mutable && !mutableQualified || len(global.InitExpr) != 0 || global.Bits != 0 || global.V128 != (V128{}) || c.tableCount() > 1 && !profile.primitiveReferences || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || global.HasInitFunc && (uint64(global.InitFunc) >= uint64(c.NumImports+len(c.Funcs)) || !c.needsFuncRefDescs()) {
				return "qualified reference global and retained references required"
			}
			if typed && !callTyped && !primitiveTyped {
				local := int(global.InitFunc) - c.NumImports
				if i < len(c.GlobalImports) || global.Mutable || c.NumImports != 0 || c.tableCount() != 0 || c.memoryCount() != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || c.HasStart || !global.HasInitFunc || global.HasInitGlobal || global.InitGlobal != 0 || local < 0 || local >= len(c.Funcs) || !c.Funcs[local].HasTypeIndex || c.Funcs[local].TypeIndex != typedIndex || !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) {
					return "private indexed reference global exact target"
				}
			}
			if i < len(c.GlobalImports) && (global.HasInitFunc || global.HasInitGlobal) {
				return "reference import initializer"
			}
			if global.HasInitGlobal && (global.HasInitFunc || !rv64InitializerGlobal(c, global.InitGlobal, ValFuncRef, i)) {
				return "reference initializer requires an earlier immutable global"
			}
			continue
		}
		if (global.Type != ValI32 && global.Type != ValI64 && global.Type != ValF32 && global.Type != ValF64 && (!vectorProfile || global.Type != ValV128)) || global.HasInitFunc {
			return "global initializer lowering is pending"
		}
		if len(global.InitExpr) != 0 {
			if i < len(c.GlobalImports) || global.HasInitGlobal {
				return "conflicting global initializer"
			}
			if err := rv64ValidateIntegerInitializer(c, global.InitExpr, global.Type, i); err != nil {
				return err.Error()
			}
		}
		if global.HasInitGlobal && (i < len(c.GlobalImports) || !rv64InitializerGlobal(c, global.InitGlobal, global.Type, i)) {
			return "global initializer requires an available immutable global of the exact type"
		}
	}
	if hasReferenceGlobal {
		if len(c.GlobalExports) > railshot.MaxModuleExports {
			return "reference-global export count bound"
		}
		for _, index := range c.GlobalExports {
			if index < 0 || index >= len(c.Globals) {
				return "reference global export index"
			}

		}
	}
	return ""
}
