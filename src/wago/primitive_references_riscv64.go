//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// The canonical execution contract binds primitive carrier declarations and
// code layout. It creates no collector or reference-valued descriptor ABI.
func railshotPrimitiveReferenceContract(c *Compiled) bool {
	return rv64PrimitiveReferenceContract(c)
}

func rv64PrimitiveReferenceContract(c *Compiled) bool {
	if c != nil {
		if memo := c.loadValidateMemo(); memo != nil && memo.executionView() == c {
			if indexes := c.loadCompileIndexes(); indexes != nil && indexes.railshotTail.primitiveReferences && indexes.railshotTail.executionContract != nil && indexes.railshotTail.executionContract.verified.Load() == c {
				return true
			}
		}
	}
	return rv64PrimitiveReferenceDeclarations(c) && rv64SourceContractValid(c)
}

// Declaration classification is also needed while encoding the metadata that
// the execution digest will bind. It grants no permission to execute code.
// Runtime entry must use rv64PrimitiveReferenceContract instead.
func railshotPrimitiveReferenceDeclarations(c *Compiled) bool {
	return rv64PrimitiveReferenceDeclarations(c)
}

func rv64PrimitiveReferenceDeclarations(c *Compiled) bool {
	if c == nil || c.needsNativeGCABI() || !c.nativeContextTrapABI || !c.nativeTableContextABI || c.tableCount() != 0 && !c.nativeTableReferenceABI || !c.registerABIDisabled || len(c.Funcs) > railshot.MaxPublicCodeBytes/16 || len(c.Types) > railshot.MaxPublicCodeBytes/8 {
		return false
	}
	for _, typ := range c.Types {
		if typ.HasDescribes || typ.HasDescriptor {
			return false
		}
		if typ.Kind == CompositeTypeStruct || typ.Kind == CompositeTypeArray {
			continue // Inert graph members; full declaration validation is separate.
		}
		if typ.Kind != CompositeTypeFunction || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
			return false
		}
		for _, values := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
			for _, value := range values {
				if value.Kind <= ValueTypeV128 {
					if value.Ref != (ReferenceTypeDescriptor{}) || value.Kind == ValueTypeV128 && !c.requiredFeatures.IsEnabled(CoreFeatureSIMD) {
						return false
					}
					continue
				}
				if !rv64PrimitiveReferenceValue(c, value) {
					return false
				}
			}
		}
	}
	for _, sig := range c.importFuncSigs {
		for _, values := range [][]ValType{sig.Params, sig.Results} {
			for _, value := range values {
				if value != ValI32 && value != ValI64 && value != ValF32 && value != ValF64 && value != ValV128 && value != ValFuncRef && value != ValExternRef {
					return false
				}
			}
		}
	}
	for index, global := range c.Globals {
		if global.Type == ValFuncRef || global.Type == ValExternRef {
			exact, err := c.globalExactType(index)
			if err != nil || !rv64PrimitiveReferenceValue(c, exact) {
				return false
			}
		} else if isGCRefValType(global.Type) {
			return false
		}
	}
	return true
}

func rv64PrimitiveReferenceValue(c *Compiled, value ValueTypeDescriptor) bool {
	if value.Kind != ValueTypeReference || value.Ref.Exact || c.requiredFeatures&(CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC) == 0 || (!value.Ref.Nullable || value.Ref.Heap.Defined) && !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) {
		return false
	}
	heap := value.Ref.Heap
	if heap.Defined {
		return heap.Abstract == 0 && uint64(heap.TypeIndex) < uint64(len(c.Types)) && c.Types[heap.TypeIndex].Kind == CompositeTypeFunction
	}
	return heap.TypeIndex == 0 && (heap.Abstract == AbstractHeapFunc || heap.Abstract == AbstractHeapExtern || heap.Abstract == AbstractHeapNoFunc || heap.Abstract == AbstractHeapNoExtern)
}

func rv64PrimitiveUsesTypedSignatures(c *Compiled) bool {
	for _, typ := range c.Types {
		if !typ.Final || len(typ.Supers) != 0 {
			return true
		}
		for _, values := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
			for _, value := range values {
				if value.Kind == ValueTypeReference && (!value.Ref.Nullable || value.Ref.Heap.Defined || value.Ref.Heap.Abstract != AbstractHeapFunc && value.Ref.Heap.Abstract != AbstractHeapExtern) {
					return true
				}
			}
		}
	}
	for index := 1; index < len(c.Types); index++ {
		if c.Types[index].RecGroup == c.Types[index-1].RecGroup {
			return true
		}
	}
	return false
}

// Rebuild the complete bounded declaration graph during cold admission. Inert
// aggregate members still contribute to a recursive function's exact identity;
// their presence grants no aggregate execution permission.
func rv64ValidatePrimitiveFunctionKeys(c *Compiled, output ...*[]uint64) error {
	if c == nil || len(c.Types) == 0 || len(c.Types) > railshot.MaxPublicCodeBytes/8 {
		return fmt.Errorf("RV64 function type graph size")
	}
	if c.NumImports < 0 || len(c.importFuncSigs) != c.NumImports || len(c.FuncTypeID) != c.NumImports+len(c.Funcs) {
		return fmt.Errorf("RV64 function graph binding count")
	}
	if err := validateDefinedTypeDescriptors(c.Types); err != nil {
		return err
	}
	module, err := rv64PrimitiveDeclaredGraph(c)
	if err != nil {
		return err
	}
	if err := wasm.ValidateModule(module); err != nil {
		return err
	}
	keys := make([]uint64, len(c.Types))
	for index, typ := range c.Types {
		if typ.Kind != CompositeTypeFunction {
			continue
		}
		key, ok := module.StructuralTypeKeyChecked(uint32(index))
		if !ok {
			return fmt.Errorf("RV64 function graph declared type key")
		}
		keys[index] = key
	}
	for index, expected := range c.FuncTypeID {
		var sig FuncSig
		if index < c.NumImports {
			sig = c.importFuncSigs[index]
		} else {
			sig = c.Funcs[index-c.NumImports]
		}
		if !sig.HasTypeIndex {
			return fmt.Errorf("RV64 function graph binding is missing")
		}
		if err := validateFuncSignature(sig, c.Types); err != nil {
			return fmt.Errorf("RV64 function graph binding: %w", err)
		}
		if keys[sig.TypeIndex] != expected {
			return fmt.Errorf("RV64 function graph signature key differs from exact type")
		}
	}
	if len(output) != 0 && output[0] != nil {
		*output[0] = keys
	}
	return nil
}

func rv64PrimitiveMutableFunctionGlobal(c *Compiled, index uint64) bool {
	if c == nil || index >= uint64(len(c.Globals)) || !rv64PrimitiveReferenceContract(c) {
		return false
	}
	global := c.Globals[index]
	if global.Type != ValFuncRef || !global.Mutable {
		return false
	}
	exact, err := c.globalExactType(int(index))
	return err == nil && rv64PrimitiveReferenceValue(c, exact)
}

func rv64PrimitiveFunctionGlobalStores(c *Compiled) bool {
	if c == nil || c.hasNoFunctionCode() || !rv64PrimitiveReferenceContract(c) {
		return false
	}
	for _, global := range c.Globals {
		if global.Type == ValFuncRef && global.Mutable {
			return true
		}
	}
	return false
}

func rv64PrimitiveGuestGlobalTarget(source *Instance, target *Global) bool {
	if source == nil || target == nil || !rv64PrimitiveFunctionGlobalStores(source.c) {
		return false
	}
	for index, global := range source.globalCells {
		if global == target && rv64PrimitiveMutableFunctionGlobal(source.c, uint64(index)) {
			return true
		}
	}
	return false
}
