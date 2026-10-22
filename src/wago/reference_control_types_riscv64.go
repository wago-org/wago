//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

// A declared function type can describe an internal block/loop instead of a
// callable function. Actual FuncSig bindings and native call/tail sites retain
// their independent numeric ABI checks.
func rv64DeclaredControlSignature(c *Compiled, typ *DefinedTypeDescriptor) (bool, error) {
	reference, _, err := rv64DeclaredControlSignatureProfile(c, typ, false)
	return reference, err
}

func rv64DeclaredControlSignatureProfile(c *Compiled, typ *DefinedTypeDescriptor, declarative bool) (bool, bool, error) {
	limit := 256
	vector := c.requiredFeatures.IsEnabled(CoreFeatureSIMD)
	if vector {
		limit = 128
	}
	if typ.Kind != CompositeTypeFunction || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.HasDescriptor || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) || len(typ.Params) > limit || len(typ.Results) > limit {
		return false, false, fmt.Errorf("RV64 plain bounded declared function type required")
	}
	reference, typed := false, false
	for _, values := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
		for _, v := range values {
			if v.Kind <= ValueTypeV128 {
				if v.Ref != (ReferenceTypeDescriptor{}) || v.Kind == ValueTypeV128 && !vector {
					return false, false, fmt.Errorf("RV64 numeric declared carrier metadata/profile")
				}
				continue
			}
			if v.Kind != ValueTypeReference || v.Ref.Exact || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
				if v.Kind == ValueTypeReference && v.Ref.Exact {
					return false, false, fmt.Errorf("%w: RV64 internal declared reference carrier", railshot.ErrUnsupported)
				}
				return false, false, fmt.Errorf("RV64 internal declared reference carrier")
			}
			reference = true
			heap := v.Ref.Heap
			if !v.Ref.Nullable || heap.Defined {
				typed = true
				if !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) || !declarative && !c.nativeReferenceControlABI {
					return false, false, fmt.Errorf("RV64 typed control declaration requires retained witness")
				}
			}
			if !heap.Defined {
				mutableVariant := rv64MutableFuncrefGlobalLocalCallCodeVariant(c)
				mutableCarrier := mutableVariant >= 19 && mutableVariant <= 28 && len(c.Funcs) == 6 && rv64MutableReferenceCarrierTarget(c, 0)
				primitiveExtern := heap.Abstract == AbstractHeapExtern && v.Ref.Nullable && c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes)
				executionExtern := heap.Abstract == AbstractHeapExtern && v.Ref.Nullable && (rv64Core3RefIsNullExecution(c) || rv64Core3RefIsNullExecutionCodecPrefix(c))
				declarationExtern := heap.Abstract == AbstractHeapExtern && v.Ref.Nullable && (rv64Core3RefDeclarationMetadata(c) || rv64Core3ReferenceDeclarationMetadata(c))
				declarationNonNullExtern := heap.Abstract == AbstractHeapExtern && !v.Ref.Nullable && (rv64Core3RefDeclarationMetadata(c) || rv64Core3RefAsNonNullDeclarationMetadata(c) || rv64Core3RefIsNullDeclarationMetadata(c))
				publicCallExtern := heap.Abstract == AbstractHeapExtern && v.Ref.Nullable && rv64PublicReferenceCallBoundaryWithPlan(c, false) && rv64PublicReferenceCarrierSignature(c.Funcs[0])
				publicHostExtern := heap.Abstract == AbstractHeapExtern && v.Ref.Nullable && rv64PublicHostReferenceBoundaryWithPlan(c, false)
				publicDynamicHostExtern := heap.Abstract == AbstractHeapExtern && v.Ref.Nullable && rv64PublicDynamicHostReferenceBoundaryWithPlan(c, false)
				mutableExtern := heap.Abstract == AbstractHeapExtern && v.Ref.Nullable && mutableCarrier
				if heap.TypeIndex != 0 || heap.Abstract != AbstractHeapFunc && !primitiveExtern && !executionExtern && !declarationExtern && !declarationNonNullExtern && !publicCallExtern && !publicHostExtern && !publicDynamicHostExtern && !mutableExtern {
					return false, false, fmt.Errorf("RV64 internal abstract function reference required")
				}
				continue
			}
			if heap.Abstract != 0 || uint64(heap.TypeIndex) >= uint64(len(c.Types)) {
				return false, false, fmt.Errorf("RV64 control function heap index")
			}
			target := &c.Types[heap.TypeIndex]
			if target.Kind != CompositeTypeFunction || !target.Final || len(target.Supers) != 0 || target.HasDescribes || target.HasDescriptor || len(target.Fields) != 0 || target.Array != (FieldTypeDescriptor{}) || len(target.Params) > limit || len(target.Results) > limit {
				return false, false, fmt.Errorf("RV64 indexed heap requires plain bounded numeric callable")
			}
			mutableVariant := rv64MutableFuncrefGlobalLocalCallCodeVariant(c)
			if heap.TypeIndex == 0 && (rv64PublicReferenceCallBoundaryWithPlan(c, false) && rv64PublicReferenceCarrierSignature(c.Funcs[0]) || mutableVariant >= 19 && mutableVariant <= 28 && len(c.Funcs) == 6 && rv64MutableReferenceCarrierTarget(c, 0)) {
				continue
			}
			// Do not recurse through another reference-bearing control declaration.
			for _, list := range [][]ValueTypeDescriptor{target.Params, target.Results} {
				for _, t := range list {
					if t.Kind > ValueTypeV128 || t.Ref != (ReferenceTypeDescriptor{}) || t.Kind == ValueTypeV128 && !vector {
						return false, false, fmt.Errorf("RV64 indexed heap requires numeric callable carriers")
					}
				}
			}
		}
	}
	return reference, typed, nil
}

func rv64ValidateDeclaredControlTypes(c *Compiled) error {
	if compiledFunctionOnlyTypeGraph(c) {
		return nil
	}
	if rv64PrimitiveReferenceContract(c) {
		return nil // Every exact function carrier was checked as one bounded graph.
	}
	for i := range c.Types {
		// GC aggregate declarations share the module type index space with
		// callable declarations. They are validated by the rooted-GC receipt;
		// only function declarations participate in the call/control ABI.
		if c.Types[i].Kind != CompositeTypeFunction {
			continue
		}
		if _, err := rv64DeclaredControlSignature(c, &c.Types[i]); err != nil {
			return err
		}
	}
	return nil
}

func rv64ValidateDeclarativeControlTypes(c *Compiled) (bool, error) {
	if compiledFunctionOnlyTypeGraph(c) || rv64PrimitiveReferenceDeclarations(c) {
		return rv64PrimitiveUsesTypedSignatures(c), nil
	}
	typed := false
	for i := range c.Types {
		if c.Types[i].Kind != CompositeTypeFunction {
			if (c.Types[i].Kind != CompositeTypeStruct && c.Types[i].Kind != CompositeTypeArray) || !c.requiredFeatures.IsEnabled(CoreFeatureGC) {
				return false, fmt.Errorf("RV64 declared aggregate type requires its GC feature")
			}
			continue
		}
		_, witness, err := rv64DeclaredControlSignatureProfile(c, &c.Types[i], true)
		if err != nil {
			return false, err
		}
		typed = typed || witness
	}
	return typed, nil
}
