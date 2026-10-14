//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import "testing"

func TestRV64ReferenceCallsIgnoreAggregateTypeDeclarations(t *testing.T) {
	c := &Compiled{
		Types: []DefinedTypeDescriptor{
			{RecGroup: 0, Final: true, Kind: CompositeTypeStruct},
			{RecGroup: 0, Final: true, Kind: CompositeTypeArray},
			{RecGroup: 1, Final: true, Kind: CompositeTypeFunction},
		},
		Funcs:                     []FuncSig{{HasTypeIndex: true, TypeIndex: 2}},
		requiredFeatures:          CoreFeatureTypedFunctionReferences,
		nativeReferenceCallABI:    true,
		nativeReferenceControlABI: true,
		nativeContextTrapABI:      true,
		nativeTableContextABI:     true,
		nativeTableReferenceABI:   true,
		registerABIDisabled:       true,
	}
	if err := railshotValidateReferenceCalls(c); err != nil {
		t.Fatalf("aggregate declaration entered the call/control validator: %v", err)
	}

	c.Types[2].Final = false
	if err := railshotValidateReferenceCalls(c); err == nil {
		t.Fatal("non-final function declaration retained the call/control witness")
	}

	c.Types[2].Final = true
	c.Types[2].RecGroup = 0
	if err := railshotValidateReferenceCalls(c); err == nil {
		t.Fatal("function declaration in a recursive group retained the call/control witness")
	}
}
