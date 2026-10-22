//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// Immediate-reference execution uses the ordinary framed ABI and compact i31
// values. The native image and every retained initializer are bound together by
// the trusted artifact contract; old images retain their independent checks.
func rv64ImmediateReferenceContract(c *Compiled) bool {
	if c == nil || c.tableImportCount() != 0 || c.stagedGCI31Product() == 0 || c.nativeGCABIRequirement() != 0 || c.genericGCFrameRoots() != nil || c.usesGCStructHelpers() || c.usesGCArrayHelpers() || !c.requiredFeatures.IsEnabled(CoreFeatureGC) || !rv64ArtifactContractValid(c) {
		return false
	}
	for _, desc := range c.GCTypeDescs {
		if desc.Kind != gc.KindFunc {
			return false
		}
	}
	carrier := func(t ValType) bool {
		return t == ValI32 || t == ValI64 || t == ValF32 || t == ValF64 || t == ValI31Ref
	}
	for _, typ := range c.Types {
		if typ.Kind != CompositeTypeFunction || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.HasDescriptor {
			return false
		}
		for _, list := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
			for _, v := range list {
				abi, ok := v.ABIType(c.Types)
				if !ok || !carrier(abi) {
					return false
				}
			}
		}
	}
	for _, sig := range c.importFuncSigs {
		for _, list := range [][]ValType{sig.Params, sig.Results} {
			for _, v := range list {
				if !carrier(v) || v == ValI31Ref {
					return false
				}
			}
		}
	}
	for _, g := range c.GlobalImports {
		if !carrier(g.Type) || g.Type == ValI31Ref {
			return false
		}
	}
	for i, g := range c.Globals {
		if !isGCRefValType(g.Type) {
			continue
		}
		if g.Type != ValAnyRef && g.Type != ValI31Ref {
			return false
		}
		exact, err := c.globalExactType(i)
		if err != nil || !rv64ImmediateCollectorValue(exact) {
			return false
		}
	}
	for _, index := range c.GlobalExports {
		if index < 0 || index >= len(c.Globals) || isGCRefValType(c.Globals[index].Type) && c.Globals[index].Type != ValI31Ref {
			return false
		}
	}
	for _, index := range c.tableExports {
		if index < 0 || index >= c.tableCount() || c.tableElementType(index) != ValI31Ref {
			return false
		}
	}
	for i := 0; i < c.tableCount(); i++ {
		exact, err := c.tableExactType(i)
		if err != nil || !rv64ImmediateCollectorValue(exact) {
			return false
		}
	}
	return true
}

// General collector-free i31 execution can retain unused aggregate type
// declarations. Its live GC carriers remain confined to compact i31 values;
// function references use the separate callable descriptor lifetime.
func rv64GeneralI31Contract(c *Compiled) bool {
	if c == nil || c.stagedGCI31Product() != stagedGCI31ProductCore || c.nativeGCABIRequirement() != 0 || c.genericGCFrameRoots() != nil ||
		c.usesGCStructHelpers() || c.usesGCArrayHelpers() || !c.requiredFeatures.IsEnabled(CoreFeatureGC) || !rv64ArtifactContractValid(c) {
		return false
	}
	carrier := func(t ValType) bool {
		return t == ValI32 || t == ValI64 || t == ValF32 || t == ValF64 || t == ValI31Ref || t == ValFuncRef
	}
	for _, typ := range c.Types {
		switch typ.Kind {
		case CompositeTypeFunction:
			for _, list := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
				for _, value := range list {
					abi, ok := value.ABIType(c.Types)
					if !ok || !carrier(abi) {
						return false
					}
				}
			}
		case CompositeTypeStruct, CompositeTypeArray:
		default:
			return false
		}
	}
	for _, sig := range c.importFuncSigs {
		for _, list := range [][]ValType{sig.Params, sig.Results} {
			for _, value := range list {
				if !carrier(value) || value == ValI31Ref {
					return false
				}
			}
		}
	}
	for _, global := range c.GlobalImports {
		if !carrier(global.Type) || global.Type == ValI31Ref {
			return false
		}
	}
	for i, global := range c.Globals {
		if !isGCRefValType(global.Type) {
			continue
		}
		exact, err := c.globalExactType(i)
		if err != nil || !rv64GeneralI31Value(exact) {
			return false
		}
	}
	for i := 0; i < c.tableCount(); i++ {
		exact, err := c.tableExactType(i)
		if err != nil || !rv64GeneralI31Value(exact) {
			return false
		}
	}
	return true
}

func rv64GeneralI31Value(value ValueTypeDescriptor) bool {
	if immediateI31ReferenceType(value) {
		return true
	}
	return value.Kind == ValueTypeReference && !value.Ref.Exact && !value.Ref.Heap.Defined && value.Ref.Heap.TypeIndex == 0 &&
		(value.Ref.Heap.Abstract == AbstractHeapFunc || value.Ref.Heap.Abstract == AbstractHeapNoFunc)
}

func rv64ImmediateCollectorValue(v ValueTypeDescriptor) bool {
	if v.Kind != ValueTypeReference || v.Ref.Exact || v.Ref.Heap.Defined || v.Ref.Heap.TypeIndex != 0 {
		return false
	}
	switch v.Ref.Heap.Abstract {
	case AbstractHeapNone, AbstractHeapAny, AbstractHeapEq, AbstractHeapI31, AbstractHeapStruct, AbstractHeapArray:
		return true
	}
	return false
}
