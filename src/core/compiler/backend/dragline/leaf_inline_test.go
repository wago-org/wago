package dragline

import (
	"bytes"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtimeabi "github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Tests of the native call ABI must retain an actual call after optimization.
func nonInlinableLeafBody(body []byte) []byte {
	return append(bytes.Repeat([]byte{0x01}, leafInlineMaxBody), body...)
}

func TestCompilerInlinesLeafCallsWithMemoryAndLocals(t *testing.T) {
	// A leaf reads memory, adds its zero-initialized local, and returns early.
	leaf := []byte{1, 1, 0x7f, 0x20, 0, 0x28, 2, 0, 0x20, 1, 0x6a, 0x0f, 0x0b}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(leaf))), leaf...), wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x20, 0, 0x10, 0, 0x6a, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	original, err := railssa.BuildStackFunc(m, 1)
	if err != nil {
		t.Fatal(err)
	}
	var scratch railssa.StackFunc
	fn, err := buildCompilerFunc(m, 1, &scratch)
	if err != nil {
		t.Fatal(err)
	}
	calls, loads := 0, 0
	callOffsets := map[uint32]bool{}
	for _, in := range original.Instrs {
		if in.Kind == wasm.InstrCall {
			callOffsets[in.Offset] = true
		}
	}
	for _, in := range fn.Structured.Instrs {
		if in.Kind == wasm.InstrCall {
			calls++
		}
		if in.Kind == wasm.InstrI32Load {
			loads++
			if !callOffsets[in.Offset] {
				t.Errorf("inlined load offset %d is not an original call site", in.Offset)
			}
		}
	}
	if calls != 0 || loads != 2 {
		t.Fatalf("inlined calls=%d loads=%d, want 0 and 2", calls, loads)
	}
	if len(fn.Structured.Locals) <= len(original.Locals) {
		t.Fatal("callee local bindings were not added")
	}
	if len(m.Code[1].BodyBytes) != 10 {
		t.Fatal("inlining changed the input module")
	}
}

func TestLeafInlineBudgetsAndLocalReuse(t *testing.T) {
	leaf := []byte{1, 1, 0x7f, 0x20, 0, 0x20, 1, 0x6a, 0x0b}
	caller := make([]byte, 0)
	for site := 0; site < leafInlineMaxSites+3; site++ {
		caller = append(caller, 0x20, 0, 0x10, 0, 0x1a)
	}
	caller = append(caller, 0x20, 0, 0x0b)
	m := leafInlineTestModule(t, leaf, caller)
	stack, err := railssa.BuildStackFunc(m, 1)
	if err != nil {
		t.Fatal(err)
	}
	inlined, _ := boundedLeafInlineModule(m, 1, stack)
	if inlined == nil {
		t.Fatal("eligible caller was not expanded")
	}
	if err := wasm.ValidateModule(inlined); err != nil {
		t.Fatal(err)
	}
	got, err := railssa.BuildStackFunc(inlined, 1)
	if err != nil {
		t.Fatal(err)
	}
	calls, resets := 0, 0
	for _, in := range got.Instrs {
		if in.Kind == wasm.InstrCall {
			calls++
		}
		if in.Kind == wasm.InstrLocalSet && in.U32() == 2 {
			resets++
		}
	}
	if calls != 3 || resets != leafInlineMaxSites || len(got.Locals) != 3 {
		t.Fatalf("calls=%d resets=%d locals=%d", calls, resets, len(got.Locals))
	}
	if len(inlined.Code[1].BodyBytes)-len(m.Code[1].BodyBytes) > leafInlineMaxGrowth {
		t.Fatal("byte growth budget exceeded")
	}
}

func TestLeafInlineRejectsCallsAndGrowingMemory(t *testing.T) {
	for _, body := range [][]byte{
		{0, 0x20, 0, 0x10, 0, 0x0b}, // recursive direct call
		{0, 0x20, 0, 0x40, 0, 0x0b}, // memory.grow may allocate/reenter
		append([]byte{0}, nonInlinableLeafBody([]byte{0x20, 0, 0x0b})...),
	} {
		m := leafInlineTestModule(t, body, []byte{0x20, 0, 0x10, 0, 0x0b})
		if leafInlineCallee(m, 0) != nil {
			t.Fatal("ineligible leaf admitted")
		}
	}
}

func TestLeafInlineByteGrowthBudget(t *testing.T) {
	leaf := append([]byte{0}, bytes.Repeat([]byte{0x01}, 300)...)
	leaf = append(leaf, 0x20, 0, 0x0b)
	var caller []byte
	for site := 0; site < leafInlineMaxSites; site++ {
		caller = append(caller, 0x20, 0, 0x10, 0, 0x1a)
	}
	caller = append(caller, 0x20, 0, 0x0b)
	m := leafInlineTestModule(t, leaf, caller)
	stack, err := railssa.BuildStackFunc(m, 1)
	if err != nil {
		t.Fatal(err)
	}
	inlined, _ := boundedLeafInlineModule(m, 1, stack)
	if inlined == nil {
		t.Fatal("no expansion")
	}
	if err := wasm.ValidateModule(inlined); err != nil {
		t.Fatal(err)
	}
	growth := len(inlined.Code[1].BodyBytes) - len(m.Code[1].BodyBytes)
	if growth > leafInlineMaxGrowth || growth < leafInlineMaxGrowth-leafInlineMaxBody {
		t.Fatalf("growth=%d, expected the byte budget to be binding", growth)
	}
}

func TestLeafInlineTrapArtifactUsesCallerOffset(t *testing.T) {
	leaf, caller := []byte{0, 0x20, 0, 0x28, 2, 0, 0x0b}, []byte{0x20, 0, 0x10, 0, 0x0b}
	m := leafInlineTestModule(t, leaf, caller)
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	input := corecompiler.Input{
		Module: m, Source: leafInlineTestSource(leaf, caller),
		Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsExplicit,
		Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision}, ConfigurationFingerprint: [32]byte{1},
	}
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	if _, err := (Compiler{FunctionCache: cache}).Compile(input); err != nil {
		t.Fatal(err)
	}
	dependencies, ok := functionArtifactDependencies(input, m, cache)
	if !ok {
		t.Fatal("no dependencies")
	}
	identity, err := functionArtifactIdentity(input, m, 1, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	artifact, hit, err := cache.Get(identity)
	if err != nil || !hit {
		t.Fatalf("artifact hit=%v err=%v", hit, err)
	}
	if len(artifact.Relocations) != 0 || len(artifact.Traps) != 1 || artifact.Traps[0].WasmOffset != 2 {
		t.Fatalf("inlined artifact relocations=%v traps=%v; want a trap at caller offset 2", artifact.Relocations, artifact.Traps)
	}
}

func TestLeafInlineCalleeChangesInvalidateCallerArtifact(t *testing.T) {
	module := func(value byte) *wasm.Module {
		return leafInlineTestModule(t, []byte{0, 0x20, 0, 0x41, value, 0x6a, 0x0b}, []byte{0x20, 0, 0x10, 0, 0x0b})
	}
	before, after := module(1), module(2)
	// The source bytes need only provide the common module-section identity;
	// callee contracts consume the actual decoded bodies independently.
	source := leafInlineTestSource([]byte{0, 0x20, 0, 0x41, 1, 0x6a, 0x0b}, []byte{0x20, 0, 0x10, 0, 0x0b})
	input := corecompiler.Input{Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision}, ConfigurationFingerprint: [32]byte{1}}
	cache := corecompiler.NewFunctionArtifactCache(1)
	a, ok := functionArtifactDependencies(input, before, cache)
	if !ok {
		t.Fatal("before dependencies unavailable")
	}
	b, ok := functionArtifactDependencies(input, after, cache)
	if !ok || a.callee[1] == b.callee[1] {
		t.Fatal("caller identity ignored copied callee body")
	}
}

func leafInlineTestModule(t *testing.T, leaf, caller []byte) *wasm.Module {
	t.Helper()
	source := leafInlineTestSource(leaf, caller)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func leafInlineTestSource(leaf, caller []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(leaf))), leaf...), wasmtest.Code(caller))),
	)
}
