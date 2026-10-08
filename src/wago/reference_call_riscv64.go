//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import "fmt"

// The persisted execution marker is separate from general typed-reference
// permission. Every possible declared call fits the counted root arenas, and
// only immutable numeric framed descriptors belong to this bounded profile.
func railshotValidateReferenceCalls(c *Compiled) error {
	if !c.nativeReferenceCallABI {
		return nil
	}
	if !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.NumImports+len(c.Funcs) == 0 {
		return fmt.Errorf("RV64 call_ref descriptor/native ABI contract")
	}
	if (c.needsFuncRefDescs() || len(c.FuncTypeID) != 0) && len(c.FuncTypeID) != c.NumImports+len(c.Funcs) {
		return fmt.Errorf("RV64 call_ref indexed descriptor extent")
	}
	for _, d := range c.Types {
		if d.Kind != CompositeTypeFunction || !d.Final || len(d.Supers) != 0 || d.HasDescribes || d.HasDescriptor {
			return fmt.Errorf("RV64 call_ref requires plain final function signatures")
		}
		for _, values := range [][]ValueTypeDescriptor{d.Params, d.Results} {
			if _, err := rv64IndirectSignatureSlots(values); err != nil {
				return err
			}
			for _, v := range values {
				if v.Ref != (ReferenceTypeDescriptor{}) {
					return fmt.Errorf("RV64 call_ref requires numeric value descriptors")
				}
			}
		}
	}
	return nil
}
