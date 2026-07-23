package plugins

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/machinecode"
)

func portableAdd(_ InstructionContext, args []Bits) ([]Bits, error) {
	sum, err := BitsFromUint32(4, args[0].Uint32()+args[1].Uint32())
	return []Bits{sum}, err
}

func TestPrepareCustomTypeSupportsStandardCarriers(t *testing.T) {
	for _, carrier := range []WasmType{WasmI32, WasmI64, WasmF32, WasmF64, WasmV128, WasmFuncRef, WasmExternRef} {
		typ, err := PrepareCustomType(CustomTypeSpec{Name: "example.value", Size: 16, Carrier: carrier})
		if err != nil {
			t.Fatalf("carrier %d: %v", carrier, err)
		}
		if typ.Name() != "example.value" || typ.Size() != 16 || typ.Carrier() != carrier || !typ.Valid() {
			t.Fatalf("carrier %d produced %+v", carrier, typ)
		}
	}
	for _, spec := range []CustomTypeSpec{
		{Size: 16, Carrier: WasmI32},
		{Name: "bad", Size: 15, Carrier: WasmI32},
		{Name: "bad", Size: 16, Carrier: WasmType(255)},
	} {
		if _, err := PrepareCustomType(spec); err == nil {
			t.Fatalf("invalid custom type accepted: %+v", spec)
		}
	}
}

func TestPrepareBuildsNativeInstruction(t *testing.T) {
	input := []int32{4, 4}
	output := []int32{4}
	def, err := Prepare(InstructionSpec{
		Module:  "example",
		Name:    "i4.add",
		Input:   input,
		Output:  output,
		Handler: portableAdd,
		Lower: func(ctx LoweringContext) error {
			ctx.Output(0, ctx.Add(ctx.Input(0), ctx.Input(1)))
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Registration owns its contract; later caller mutations cannot change it.
	input[0], output[0] = 32, 32
	if def.Spec.Input[0] != 4 || def.Spec.Output[0] != 4 {
		t.Fatalf("Prepare did not detach widths: input=%v output=%v", def.Spec.Input, def.Spec.Output)
	}

	native, ok := def.Native()
	if !ok {
		t.Fatal("4-bit add recipe should lower natively")
	}
	if native.ResultWidth != 4 || native.Output != 2 || !native.StackCompatible {
		t.Fatalf("unexpected native lowering: %+v", native)
	}
	if len(native.Nodes) != 3 ||
		native.Nodes[0].Op != InstructionInput ||
		native.Nodes[1].Op != InstructionInput ||
		native.Nodes[2].Op != InstructionAdd {
		t.Fatalf("unexpected native nodes: %+v", native.Nodes)
	}
}

func TestPrepareBuildsIndependentTargetLowerings(t *testing.T) {
	amd64 := &machinecode.AMD64Lowering{
		Compatibility: machinecode.AMD64CompatibilityManaged,
		Managed:       func(machinecode.AMD64ManagedContext) error { return nil },
	}
	arm64 := &machinecode.ARM64Lowering{
		Compatibility: machinecode.ARM64CompatibilityManaged,
		Managed:       func(machinecode.ARM64ManagedContext) error { return nil },
	}
	def, err := Prepare(InstructionSpec{
		Module:  "example",
		Name:    "raw",
		Input:   []int32{32},
		Handler: func(InstructionContext, []Bits) ([]Bits, error) { return nil, nil },
		AMD64:   amd64,
		ARM64:   arm64,
	})
	if err != nil {
		t.Fatal(err)
	}
	amd64.Compatibility = 0
	arm64.Compatibility = 0
	native, ok := def.Native()
	if !ok || native.AMD64 == nil || native.ARM64 == nil {
		t.Fatal("target declarations should produce native instruction lowerings")
	}
	if native.AMD64.Compatibility != machinecode.AMD64CompatibilityManaged ||
		native.ARM64.Compatibility != machinecode.ARM64CompatibilityManaged {
		t.Fatalf("Prepare did not detach target declarations: %+v", native)
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
		Custom: &CustomSignature{Inputs: inputs, Output: &output},
		AMD64: &machinecode.AMD64Lowering{
			Compatibility: machinecode.AMD64CompatibilityFullAccess,
			Emit:          func(machinecode.AMD64Context) error { return nil },
		},
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

func TestPrepareRejectsInvalidDefinitions(t *testing.T) {
	vector, err := PrepareCustomType(CustomTypeSpec{Name: "example.v256", Size: 32, Carrier: WasmExternRef})
	if err != nil {
		t.Fatal(err)
	}
	customAMD64 := &machinecode.AMD64Lowering{
		Compatibility: machinecode.AMD64CompatibilityFullAccess,
		Emit:          func(machinecode.AMD64Context) error { return nil },
	}
	tests := []struct {
		name string
		spec InstructionSpec
		want string
	}{
		{
			name: "missing handler",
			spec: InstructionSpec{Module: "example", Name: "bad"},
			want: "requires Handler",
		},
		{
			name: "invalid width",
			spec: InstructionSpec{
				Module: "example", Name: "bad", Input: []int32{0},
				Handler: func(InstructionContext, []Bits) ([]Bits, error) { return nil, nil },
			},
			want: "non-positive width",
		},
		{
			name: "unset recipe output",
			spec: InstructionSpec{
				Module: "example", Name: "bad", Input: []int32{4}, Output: []int32{4},
				Handler: func(InstructionContext, []Bits) ([]Bits, error) { return nil, nil },
				Lower:   func(LoweringContext) error { return nil },
			},
			want: "did not set output",
		},
		{
			name: "custom metadata length",
			spec: InstructionSpec{
				Module: "example", Name: "bad", Input: []int32{256},
				Custom: &CustomSignature{}, AMD64: customAMD64,
			},
			want: "custom input metadata has 0 entries, want 1",
		},
		{
			name: "custom width mismatch",
			spec: InstructionSpec{
				Module: "example", Name: "bad", Input: []int32{128},
				Custom: &CustomSignature{Inputs: []CustomType{vector}},
				AMD64:  customAMD64,
			},
			want: "is 128 bits",
		},
		{
			name: "custom handler",
			spec: InstructionSpec{
				Module: "example", Name: "bad", Input: []int32{256},
				Custom:  &CustomSignature{Inputs: []CustomType{vector}},
				Handler: func(InstructionContext, []Bits) ([]Bits, error) { return nil, nil },
				AMD64:   customAMD64,
			},
			want: "native-only and forbid Handler",
		},
		{
			name: "custom without lowering",
			spec: InstructionSpec{
				Module: "example", Name: "bad", Input: []int32{256},
				Custom: &CustomSignature{Inputs: []CustomType{vector}},
			},
			want: "require a native lowering",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Prepare(tc.spec)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Prepare error = %v, want substring %q", err, tc.want)
			}
		})
	}
}
