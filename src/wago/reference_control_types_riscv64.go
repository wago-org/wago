//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import "fmt"

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
				executionExtern := heap.Abstract == AbstractHeapExtern && v.Ref.Nullable && (rv64Core3RefIsNullExecution(c) || rv64Core3RefIsNullExecutionCodecPrefix(c))
				declarationExtern := heap.Abstract == AbstractHeapExtern && v.Ref.Nullable && rv64Core3ReferenceDeclarationMetadata(c)
				declarationNonNullExtern := heap.Abstract == AbstractHeapExtern && !v.Ref.Nullable && (rv64Core3RefAsNonNullDeclarationMetadata(c) || rv64Core3RefIsNullDeclarationMetadata(c))
				publicCallExtern := heap.Abstract == AbstractHeapExtern && v.Ref.Nullable && rv64PublicReferenceCallBoundaryWithPlan(c, false) && rv64PublicReferenceCarrierSignature(c.Funcs[0])
				publicHostExtern := heap.Abstract == AbstractHeapExtern && v.Ref.Nullable && rv64PublicHostReferenceBoundaryWithPlan(c, false)
				if heap.TypeIndex != 0 || heap.Abstract != AbstractHeapFunc && !executionExtern && !declarationExtern && !declarationNonNullExtern && !publicCallExtern && !publicHostExtern {
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
			if heap.TypeIndex == 0 && rv64PublicReferenceCallBoundaryWithPlan(c, false) && rv64PublicReferenceCarrierSignature(c.Funcs[0]) {
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
	for i := range c.Types {
		if i > 0 && c.Types[i-1].RecGroup == c.Types[i].RecGroup {
			return fmt.Errorf("RV64 recursive control type group is unsupported")
		}
		if _, err := rv64DeclaredControlSignature(c, &c.Types[i]); err != nil {
			return err
		}
	}
	return nil
}

func rv64ValidateDeclarativeControlTypes(c *Compiled) (bool, error) {
	typed := false
	for i := range c.Types {
		if i > 0 && c.Types[i-1].RecGroup == c.Types[i].RecGroup {
			return false, fmt.Errorf("RV64 recursive control type group is unsupported")
		}
		_, witness, err := rv64DeclaredControlSignatureProfile(c, &c.Types[i], true)
		if err != nil {
			return false, err
		}
		typed = typed || witness
	}
	return typed, nil
}
