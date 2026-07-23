package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

type instructionTestExt struct {
	nativeCalls *int
	wide        bool
	fail        bool
}

type instructionRecipeExt struct {
	name          string
	input         []int32
	output        int32
	lower         InstructionLowerer
	eval          func([]uint32) uint32
	portableCalls *int
}

type instructionMachineExt struct {
	name          string
	input         []int32
	output        []int32
	lowering      *AMD64InstructionLowering
	portableCalls *int
}

func (instructionMachineExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.instruction-machine-code", Version: "1.0.0", Stability: Experimental}
}

func (e instructionMachineExt) Register(r *Registry) error {
	r.Capability(CapCompilerCodegen)
	return r.Compiler().Instruction(InstructionSpec{
		Module: "wago:instr/machine", Name: e.name, Input: e.input, Output: e.output, AMD64: e.lowering,
		Handler: func(_ InstructionContext, args []Bits) ([]Bits, error) {
			if e.portableCalls != nil {
				*e.portableCalls++
			}
			if len(e.output) == 0 {
				return nil, nil
			}
			out, _ := BitsFromUint32(e.output[0], args[0].Uint32())
			return []Bits{out}, nil
		},
	})
}

func (instructionRecipeExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.instruction-recipes", Version: "1.0.0", Stability: Experimental}
}

func (e instructionRecipeExt) Register(r *Registry) error {
	r.Capability(CapCompilerCodegen)
	return r.Compiler().Instruction(InstructionSpec{
		Module: "wago:instr/recipe", Name: e.name, Input: e.input, Output: []int32{e.output},
		Handler: func(_ InstructionContext, args []Bits) ([]Bits, error) {
			if e.portableCalls != nil {
				*e.portableCalls++
			}
			raw := make([]uint32, len(args))
			for i := range args {
				raw[i] = args[i].Uint32()
			}
			out, _ := BitsFromUint32(e.output, e.eval(raw))
			return []Bits{out}, nil
		},
		Lower: e.lower,
	})
}

func (instructionTestExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.instructions", Version: "1.0.0", Stability: Experimental}
}
func (e instructionTestExt) Register(r *Registry) error {
	r.Capability(CapCompilerCodegen)
	if e.wide {
		if err := r.Compiler().Instruction(InstructionSpec{Module: "wago:instr/test", Name: "u64.make", Input: []int32{32, 32}, Output: []int32{64}, Handler: func(_ InstructionContext, args []Bits) ([]Bits, error) {
			raw := make([]byte, 8)
			binary.LittleEndian.PutUint32(raw, args[0].Uint32())
			binary.LittleEndian.PutUint32(raw[4:], args[1].Uint32())
			v, _ := NewBits(64, raw)
			return []Bits{v}, nil
		}}); err != nil {
			return err
		}
		return r.Compiler().Instruction(InstructionSpec{Module: "wago:instr/test", Name: "u64.split", Input: []int32{64}, Output: []int32{32, 32}, Handler: func(_ InstructionContext, args []Bits) ([]Bits, error) {
			raw := args[0].Bytes()
			lo, _ := BitsFromUint32(32, binary.LittleEndian.Uint32(raw))
			hi, _ := BitsFromUint32(32, binary.LittleEndian.Uint32(raw[4:]))
			return []Bits{lo, hi}, nil
		}})
	}
	var lower InstructionLowerer
	if !e.fail {
		lower = func(ctx LoweringContext) error { ctx.Output(0, ctx.Add(ctx.Input(0), ctx.Input(1))); return nil }
	}
	return r.Compiler().Instruction(InstructionSpec{
		Module: "wago:instr/example.int", Name: "i4.add", Input: []int32{4, 4}, Output: []int32{4},
		Handler: func(_ InstructionContext, args []Bits) ([]Bits, error) {
			if e.fail {
				return nil, errors.New("deliberate failure")
			}
			if e.nativeCalls != nil {
				*e.nativeCalls++
			}
			v, _ := BitsFromUint32(4, args[0].Uint32()+args[1].Uint32())
			return []Bits{v}, nil
		},
		Lower: lower,
	})
}

func instructionFuncImport(module, name string, typeIndex uint32) []byte {
	out := append(wasmtest.Name(module), wasmtest.Name(name)...)
	out = append(out, 0)
	return append(out, wasmtest.ULEB(typeIndex)...)
}

func TestCustomInstructionI4AddLowersNatively(t *testing.T) {
	calls := 0
	rt := NewRuntime()
	if err := rt.Use(instructionTestExt{nativeCalls: &calls}); err != nil {
		t.Fatal(err)
	}
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/example.int", "i4.add", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x0b}))),
	)
	mod, err := rt.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	if len(mod.c.customInstructions) != 1 {
		t.Fatalf("native custom lowerings=%d, want 1", len(mod.c.customInstructions))
	}
	for _, lowering := range mod.c.customInstructions {
		if !lowering.StackCompatible {
			t.Fatal("linear i4.add recipe did not select the zero-copy stack path")
		}
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := in.Invoke("add", I32(15), I32(3))
	if err != nil {
		t.Fatal(err)
	}
	if got := AsI32(out[0]); got != 2 {
		t.Fatalf("i4.add(15,3)=%d, want 2", got)
	}
	if calls != 0 {
		t.Fatalf("portable handler called %d time(s), want native lowering", calls)
	}
}

func TestCustomInstructionRichRecipesLowerNatively(t *testing.T) {
	signed4 := func(v uint32) int32 { return int32(int8(uint8(v<<4))) >> 4 }
	comparisons := []struct {
		name  string
		lower func(LoweringContext, LowerValue, LowerValue) LowerValue
		eval  func(uint32, uint32) bool
	}{
		{"eq", func(c LoweringContext, a, b LowerValue) LowerValue { return c.Eq(a, b) }, func(a, b uint32) bool { return a == b }},
		{"ne", func(c LoweringContext, a, b LowerValue) LowerValue { return c.Ne(a, b) }, func(a, b uint32) bool { return a != b }},
		{"lt_u", func(c LoweringContext, a, b LowerValue) LowerValue { return c.LtU(a, b) }, func(a, b uint32) bool { return a < b }},
		{"le_u", func(c LoweringContext, a, b LowerValue) LowerValue { return c.LeU(a, b) }, func(a, b uint32) bool { return a <= b }},
		{"gt_u", func(c LoweringContext, a, b LowerValue) LowerValue { return c.GtU(a, b) }, func(a, b uint32) bool { return a > b }},
		{"ge_u", func(c LoweringContext, a, b LowerValue) LowerValue { return c.GeU(a, b) }, func(a, b uint32) bool { return a >= b }},
		{"lt_s", func(c LoweringContext, a, b LowerValue) LowerValue { return c.LtS(a, b) }, func(a, b uint32) bool { return signed4(a) < signed4(b) }},
		{"le_s", func(c LoweringContext, a, b LowerValue) LowerValue { return c.LeS(a, b) }, func(a, b uint32) bool { return signed4(a) <= signed4(b) }},
		{"gt_s", func(c LoweringContext, a, b LowerValue) LowerValue { return c.GtS(a, b) }, func(a, b uint32) bool { return signed4(a) > signed4(b) }},
		{"ge_s", func(c LoweringContext, a, b LowerValue) LowerValue { return c.GeS(a, b) }, func(a, b uint32) bool { return signed4(a) >= signed4(b) }},
	}
	for _, tc := range comparisons {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			ext := instructionRecipeExt{name: tc.name, input: []int32{4, 4}, output: 1, portableCalls: &calls}
			ext.lower = func(c LoweringContext) error {
				c.Output(0, tc.lower(c, c.Input(0), c.Input(1)))
				return nil
			}
			ext.eval = func(v []uint32) uint32 {
				if tc.eval(v[0], v[1]) {
					return 1
				}
				return 0
			}
			in := instantiateInstructionRecipe(t, ext)
			defer in.Close()
			for a := uint32(0); a < 16; a++ {
				for b := uint32(0); b < 16; b++ {
					out, err := in.Invoke("run", I32(int32(a|0xffff0000)), I32(int32(b|0xaaaa0000)))
					if err != nil {
						t.Fatal(err)
					}
					if got, want := uint32(out[0]), ext.eval([]uint32{a, b}); got != want {
						t.Fatalf("%s(%d,%d)=%d, want %d", tc.name, a, b, got, want)
					}
				}
			}
			if calls != 0 {
				t.Fatalf("portable handler called %d time(s)", calls)
			}
		})
	}

	shifts := []struct {
		name  string
		lower func(LoweringContext, LowerValue, LowerValue) LowerValue
		eval  func(uint32, uint32) uint32
	}{
		{"shl", func(c LoweringContext, a, b LowerValue) LowerValue { return c.Shl(a, b) }, func(a, b uint32) uint32 { return (a << (b % 4)) & 15 }},
		{"shr_u", func(c LoweringContext, a, b LowerValue) LowerValue { return c.ShrU(a, b) }, func(a, b uint32) uint32 { return a >> (b % 4) }},
		{"shr_s", func(c LoweringContext, a, b LowerValue) LowerValue { return c.ShrS(a, b) }, func(a, b uint32) uint32 { return uint32(signed4(a)>>(b%4)) & 15 }},
	}
	for _, tc := range shifts {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			ext := instructionRecipeExt{name: tc.name, input: []int32{4, 4}, output: 4, portableCalls: &calls, eval: func(v []uint32) uint32 { return tc.eval(v[0], v[1]) }}
			ext.lower = func(c LoweringContext) error {
				c.Output(0, tc.lower(c, c.Input(0), c.Input(1)))
				return nil
			}
			in := instantiateInstructionRecipe(t, ext)
			defer in.Close()
			for a := uint32(0); a < 16; a++ {
				for b := uint32(0); b < 16; b++ {
					out, err := in.Invoke("run", I32(int32(a)), I32(int32(b)))
					if err != nil {
						t.Fatal(err)
					}
					if got, want := uint32(out[0]), ext.eval([]uint32{a, b}); got != want {
						t.Fatalf("%s(%d,%d)=%d, want %d", tc.name, a, b, got, want)
					}
				}
			}
			if calls != 0 {
				t.Fatalf("portable handler called %d time(s)", calls)
			}
		})
	}

	t.Run("is-zero", func(t *testing.T) {
		calls := 0
		ext := instructionRecipeExt{name: "is_zero", input: []int32{4}, output: 1, portableCalls: &calls}
		ext.lower = func(c LoweringContext) error {
			c.Output(0, c.IsZero(c.Input(0)))
			return nil
		}
		ext.eval = func(v []uint32) uint32 {
			if v[0] == 0 {
				return 1
			}
			return 0
		}
		in := instantiateInstructionRecipe(t, ext)
		defer in.Close()
		for value := uint32(0); value < 16; value++ {
			out, err := in.Invoke("run", I32(int32(value|0xffff0000)))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := uint32(out[0]), ext.eval([]uint32{value}); got != want {
				t.Fatalf("is-zero(%d)=%d, want %d", value, got, want)
			}
		}
		if calls != 0 {
			t.Fatalf("portable handler called %d time(s)", calls)
		}
	})

	t.Run("dag-select", func(t *testing.T) {
		calls := 0
		ext := instructionRecipeExt{name: "dag.select", input: []int32{4, 4, 4}, output: 4, portableCalls: &calls}
		ext.lower = func(c LoweringContext) error {
			a, b, shift := c.Input(0), c.Input(1), c.Input(2)
			c.Output(0, c.Select(c.Add(a, a), c.ShrU(b, shift), c.LtS(a, b)))
			return nil
		}
		ext.eval = func(v []uint32) uint32 {
			if signed4(v[0]) < signed4(v[1]) {
				return (v[0] + v[0]) & 15
			}
			return v[1] >> (v[2] % 4)
		}
		in := instantiateInstructionRecipe(t, ext)
		defer in.Close()
		for a := uint32(0); a < 16; a++ {
			for b := uint32(0); b < 16; b++ {
				for shift := uint32(0); shift < 4; shift++ {
					out, err := in.Invoke("run", I32(int32(a)), I32(int32(b)), I32(int32(shift)))
					if err != nil {
						t.Fatal(err)
					}
					if got, want := uint32(out[0]), ext.eval([]uint32{a, b, shift}); got != want {
						t.Fatalf("dag.select(%d,%d,%d)=%d, want %d", a, b, shift, got, want)
					}
				}
			}
		}
		if calls != 0 {
			t.Fatalf("portable handler called %d time(s)", calls)
		}
	})
}

func TestCustomInstructionPluginMachineCode(t *testing.T) {
	t.Run("managed", func(t *testing.T) {
		calls := 0
		ext := instructionMachineExt{name: "i4.identity", input: []int32{4}, output: []int32{4}, portableCalls: &calls}
		ext.lowering = &AMD64InstructionLowering{Compatibility: AMD64CompatibilityManaged, Managed: func(ctx AMD64ManagedLoweringContext) error {
			value, err := ctx.InputI32(0)
			if err != nil {
				return err
			}
			return ctx.OutputI32(value)
		}}
		in := instantiateMachineInstruction(t, ext)
		defer in.Close()
		out, err := in.Invoke("run", I32(-1))
		if err != nil {
			t.Fatal(err)
		}
		if got := uint32(out[0]); got != 15 {
			t.Fatalf("managed i4.identity=%d, want 15", got)
		}
		if calls != 0 {
			t.Fatalf("portable handler called %d time(s)", calls)
		}
	})

	t.Run("encoder", func(t *testing.T) {
		calls := 0
		ext := instructionMachineExt{name: "i4.add", input: []int32{4, 4}, output: []int32{4}, portableCalls: &calls}
		ext.lowering = &AMD64InstructionLowering{Compatibility: AMD64CompatibilityFullAccess, Emit: func(ctx AMD64LoweringContext) error {
			a, err := ctx.InputI32(0)
			if err != nil {
				return err
			}
			b, err := ctx.InputI32(1)
			if err != nil {
				return err
			}
			ctx.Encoder().Add32(a, b)
			ctx.Release(b)
			return ctx.OutputI32(a)
		}}
		in := instantiateMachineInstruction(t, ext)
		defer in.Close()
		out, err := in.Invoke("run", I32(-1), I32(0x12340003))
		if err != nil {
			t.Fatal(err)
		}
		if got := uint32(out[0]); got != 2 {
			t.Fatalf("machine i4.add=%d, want 2", got)
		}
		if calls != 0 {
			t.Fatalf("portable handler called %d time(s)", calls)
		}
	})

	t.Run("arbitrary-bytes", func(t *testing.T) {
		calls := 0
		ext := instructionMachineExt{name: "raw.constant", output: []int32{32}, portableCalls: &calls}
		ext.lowering = &AMD64InstructionLowering{Compatibility: AMD64CompatibilityFullAccess, Emit: func(ctx AMD64LoweringContext) error {
			const rax = 0
			if err := ctx.ReserveGP(rax); err != nil {
				return err
			}
			// mov eax, 0x78563412. Encoder().B deliberately permits arbitrary bytes.
			ctx.Encoder().B = append(ctx.Encoder().B, 0xb8, 0x12, 0x34, 0x56, 0x78)
			return ctx.OutputI32(rax)
		}}
		in := instantiateMachineInstruction(t, ext)
		defer in.Close()
		out, err := in.Invoke("run")
		if err != nil {
			t.Fatal(err)
		}
		if got := uint32(out[0]); got != 0x78563412 {
			t.Fatalf("raw constant=%#x", got)
		}
		if calls != 0 {
			t.Fatalf("portable handler called %d time(s)", calls)
		}
	})

	t.Run("feature-declaration", func(t *testing.T) {
		ext := instructionMachineExt{name: "avx2.marker", output: []int32{32}}
		ext.lowering = &AMD64InstructionLowering{Compatibility: AMD64CompatibilityFullAccess, Features: AMD64FeatureAVX2, Emit: func(ctx AMD64LoweringContext) error {
			r := ctx.AllocGP()
			ctx.Encoder().MovImm32(r, 1)
			return ctx.OutputI32(r)
		}}
		rt, mod := compileMachineInstruction(t, ext)
		_ = rt
		if !mod.Compiled().RequiresAVX2() {
			t.Fatal("plugin-declared AVX2 requirement was not propagated")
		}
	})

	t.Run("mode-validation", func(t *testing.T) {
		ext := instructionMachineExt{name: "bad.mode", output: []int32{32}}
		ext.lowering = &AMD64InstructionLowering{Compatibility: AMD64CompatibilityManaged, Emit: func(AMD64LoweringContext) error { return nil }}
		rt := NewRuntime()
		if err := rt.Use(ext); err == nil || !strings.Contains(err.Error(), "requires Managed and forbids Emit") {
			t.Fatalf("mode validation error=%v", err)
		}
	})
}

func instantiateMachineInstruction(t *testing.T, ext instructionMachineExt) *Instance {
	t.Helper()
	rt, mod := compileMachineInstruction(t, ext)
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func compileMachineInstruction(t *testing.T, ext instructionMachineExt) (*Runtime, *Module) {
	t.Helper()
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatal(err)
	}
	params := make([]wasm.ValType, len(ext.input))
	for i := range params {
		params[i] = wasm.I32
	}
	results := []wasm.ValType(nil)
	if len(ext.output) != 0 {
		results = []wasm.ValType{wasm.I32}
	}
	sig := wasmtest.FuncType(params, results)
	body := make([]byte, 0, len(params)*2+3)
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x10, 0x00, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/machine", ext.name, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	mod, err := rt.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	return rt, mod
}

func instantiateInstructionRecipe(t *testing.T, ext instructionRecipeExt) *Instance {
	t.Helper()
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatal(err)
	}
	params := make([]wasm.ValType, len(ext.input))
	for i := range params {
		params[i] = wasm.I32
	}
	sig := wasmtest.FuncType(params, []wasm.ValType{wasm.I32})
	body := make([]byte, 0, len(params)*2+3)
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x10, 0x00, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/recipe", ext.name, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	mod, err := rt.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	if len(mod.c.customInstructions) != 1 {
		t.Fatalf("native custom lowerings=%d, want 1", len(mod.c.customInstructions))
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func TestCustomInstructionWideValuesAndMultiResults(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Use(instructionTestExt{wide: true}); err != nil {
		t.Fatal(err)
	}
	t0 := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	t1 := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	t2 := t0
	t3 := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	t4 := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32})
	imports := wasmtest.Vec(
		instructionFuncImport("wago:instr/test", "u64.make", 0), instructionFuncImport("wago:instr/test", "u64.split", 1),
		instructionFuncImport("wago:abi", "result.get", 2), instructionFuncImport("wago:abi", "result.drop", 3), instructionFuncImport("wago:abi", "value.drop", 3),
	)
	body := []byte{
		0x01, 0x02, 0x7f, // one local run containing two i32 locals
		0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x22, 0x02, 0x10, 0x01, 0x22, 0x03,
		0x41, 0x00, 0x10, 0x02,
		0x20, 0x03, 0x41, 0x01, 0x10, 0x02,
		0x20, 0x03, 0x10, 0x03, 0x20, 0x02, 0x10, 0x04, 0x0b,
	}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(t0, t1, t2, t3, t4)), wasmtest.Section(2, imports), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(4))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("roundtrip", 0, 5))), wasmtest.Section(10, wasmtest.Vec(code)))
	mod, err := rt.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := in.Invoke("roundtrip", I32(-1985229329), I32(0x01234567))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || uint32(out[0]) != 0x89abcdef || uint32(out[1]) != 0x01234567 {
		t.Fatalf("roundtrip=%#v", out)
	}
	if len(in.instructionState.values) != 0 || len(in.instructionState.packs) != 0 {
		t.Fatalf("instruction handles leaked: %d values, %d packs", len(in.instructionState.values), len(in.instructionState.packs))
	}
}

func TestCustomInstructionSignatureAndHandlerErrors(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Use(instructionTestExt{}); err != nil {
		t.Fatal(err)
	}
	bad := wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32}, []wasm.ValType{wasm.I32})
	module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(bad)), wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/example.int", "i4.add", 0))))
	if _, err := rt.Compile(module); err == nil {
		t.Fatal("expected signature error")
	}

	v, err := NewBits(4, []byte{0xff})
	if err != nil {
		t.Fatal(err)
	}
	if v.Uint32() != 15 {
		t.Fatalf("canonical bits=%d", v.Uint32())
	}
	if _, err := NewBits(0, nil); err == nil {
		t.Fatal("expected invalid width")
	}

	failing := NewRuntime()
	if err := failing.Use(instructionTestExt{fail: true}); err != nil {
		t.Fatal(err)
	}
	good := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	callModule := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(good)), wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/example.int", "i4.add", 0))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x0b}))))
	mod, err := failing.Compile(callModule)
	if err != nil {
		t.Fatal(err)
	}
	in, err := failing.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err = in.Invoke("call", I32(1), I32(2)); err == nil || !strings.Contains(err.Error(), "deliberate failure") {
		t.Fatalf("handler error=%v", err)
	}
}
