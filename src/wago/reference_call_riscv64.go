//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import "fmt"

// The persisted call/non-null witnesses are separate from general typed-
// reference permission. Both operate on retained internal function references;
// only calls require declared-type transport arenas. Reference-valued external
// signatures and non-function descriptors remain outside this bounded profile.
func railshotValidateReferenceCalls(c *Compiled) error {
	if !c.nativeReferenceCallABI && !c.nativeReferenceNonNullABI {
		return nil
	}
	if !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || len(c.Funcs) == 0 {
		return fmt.Errorf("RV64 typed-reference descriptor/native ABI contract")
	}
	if (c.needsFuncRefDescs() || len(c.FuncTypeID) != 0) && len(c.FuncTypeID) != c.NumImports+len(c.Funcs) {
		return fmt.Errorf("RV64 typed-reference indexed descriptor extent")
	}
	for _, d := range c.Types {
		if d.Kind != CompositeTypeFunction || !d.Final || len(d.Supers) != 0 || d.HasDescribes || d.HasDescriptor {
			return fmt.Errorf("RV64 typed-reference requires plain final function signatures")
		}
		for _, values := range [][]ValueTypeDescriptor{d.Params, d.Results} {
			if _, err := rv64IndirectSignatureSlots(values); err != nil {
				return err
			}
			for _, v := range values {
				if v.Ref != (ReferenceTypeDescriptor{}) {
					return fmt.Errorf("RV64 typed-reference requires numeric value descriptors")
				}
			}
		}
	}
	return nil
}
