//go:build amd64 || arm64

package plugins

import (
	"strings"
	"testing"
)

func TestPrepareSnapshotsCurrentTargetLowering(t *testing.T) {
	lowering := testManagedLowering()
	def, err := Prepare(InstructionSpec{
		Module:  "example",
		Name:    "raw",
		Input:   []int32{32},
		Handler: func(InstructionContext, []Bits) ([]Bits, error) { return nil, nil },
		Codegen: lowering,
	})
	if err != nil {
		t.Fatal(err)
	}
	invalidateTestLowering(lowering)
	native, ok := def.Native()
	if !ok || native.Codegen == nil {
		t.Fatal("target declaration should produce a native instruction lowering")
	}
	if !testLoweringIsManaged(native.Codegen) {
		t.Fatalf("Prepare did not detach target declarations: %+v", native)
	}
}

func TestPrepareRejectsOtherArchitecture(t *testing.T) {
	_, err := Prepare(InstructionSpec{
		Module:  "example",
		Name:    "wrong-target",
		Input:   []int32{32},
		Handler: func(InstructionContext, []Bits) ([]Bits, error) { return nil, nil },
		Codegen: testWrongTargetLowering(),
	})
	if err == nil || !strings.Contains(err.Error(), "codegen in an ") {
		t.Fatalf("wrong-target lowering error = %v", err)
	}
}

func TestPrepareBuildsCustomInstruction(t *testing.T) {
	vector, err := PrepareCustomType(CustomTypeSpec{Name: "example.v256", Size: 32, Carrier: WasmExternRef})
	if err != nil {
		t.Fatal(err)
	}
	output := vector
	inputs := []CustomType{vector, vector}
	def, err := Prepare(InstructionSpec{
		Module: "example", Name: "v256.xor",
		Custom:  &CustomSignature{Inputs: inputs, Output: &output},
		Codegen: testFullAccessLowering(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Registration snapshots both the signature slices and pointed-to result.
	inputs[0] = CustomType{}
	output = CustomType{}
	native, ok := def.Native()
	if !ok {
		t.Fatal("custom instruction should have a native lowering")
	}
	if len(native.CustomInputs) != 2 || native.CustomInputs[0].Name() != "example.v256" ||
		native.CustomOutput == nil || native.CustomOutput.Name() != "example.v256" {
		t.Fatalf("custom signature was not detached: %+v", native)
	}
	if native.ResultWidth != 256 || native.StackCompatible {
		t.Fatalf("unexpected custom lowering: %+v", native)
	}
	if got := def.Spec.Input; len(got) != 2 || got[0] != 256 || got[1] != 256 {
		t.Fatalf("derived custom input widths=%v", got)
	}
}
