//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// TestCallExec compiles a 2-function module where f(x) = g(x) + 1 and g(x) = 2x,
// then executes f through its runtime wrapper — exercising an
// inter-function call (BL relocation + reg-ABI arg/result passing).
func TestCallExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		// func 0 (f): local.get 0; call 1; i32.const 1; i32.add
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x41, 0x01, 0x6a, 0x0b}},
		// func 1 (g): local.get 0; local.get 0; i32.add  (= 2x)
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x20, 0x00, 0x6a, 0x0b}},
	)
	for _, x := range []uint64{5, 0, 100} {
		if got := runArm64u(t, m, x); uint32(got) != uint32(2*x+1) {
			t.Fatalf("f(%d) = %d, want %d", x, uint32(got), 2*x+1)
		}
	}
}

func TestLeafCallResultStaysInX0(t *testing.T) {
	oldInline := inlineEnabled
	inlineEnabled = false
	t.Cleanup(func() { inlineEnabled = oldInline })
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		// The result feeds arithmetic, rather than local.set fusion, so it should
		// remain allocator-owned in X0 after the pin-preserving leaf returns.
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x41, 0x01, 0x6a, 0x0b}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x41, 0x02, 0x6c, 0x0b}},
	)
	s := compileWithStats(t, m, false).Funcs[0]
	if got := s.Peephole["call-result-x0"]; got != 1 {
		t.Fatalf("call-result-x0 = %d, want 1 (all: %v)", got, s.Peephole)
	}
}

func TestMixedCallWrapperArgsExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		// f(x) = trunc(g(float(x), x)); both call operands are register-resident.
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0xb7, 0x20, 0x00, 0x10, 0x01, 0xaa, 0x0b}},
		// g(a, b) = a + float(b).
		funcDef{[]wasm.ValType{wasm.F64, wasm.I32}, []wasm.ValType{wasm.F64}, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0xb7, 0xa0, 0x0b}},
	)
	stats := compileWithStats(t, m, false).Funcs[0]
	if got := stats.Calls["mixed"]; got != 1 {
		t.Fatalf("mixed register calls = %d, want 1 (all: %v)", got, stats.Calls)
	}
	for _, x := range []uint64{0, 1, 7, 100} {
		if got := runArm64u(t, m, x); uint32(got) != uint32(2*x) {
			t.Fatalf("f(%d) = %d, want %d", x, uint32(got), 2*x)
		}
	}
}

func TestMixedCallPreservesBelowOperandsAndConstants(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		// Keep x below the two call arguments. The f64 constant is loaded directly
		// into V0 while the below operand is canonicalized independently.
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x14, 0x40, 0x41, 0x03, 0x10, 0x01, 0xaa, 0x6a, 0x0b}},
		funcDef{[]wasm.ValType{wasm.F64, wasm.I32}, []wasm.ValType{wasm.F64}, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0xb7, 0xa0, 0x0b}},
	)
	// Call-bearing internal entries read the stack fence from the module context.
	// Enter through the runtime wrapper so X26 is initialized rather than inherited
	// from Go's closure register, whose value varies with the preceding tests.
	opts := CompileOptions{Optimizations: map[string]bool{"stack-fence": true}}
	for _, x := range []uint64{0, 1, 17} {
		got, err := runArm64WrapperWithOptions(t, m, opts, x)
		if err != nil || uint32(got) != uint32(x+8) {
			t.Fatalf("f(%d) = %d, %v; want %d", x, uint32(got), err, x+8)
		}
	}
}

func TestTwoIntegerResultRegisterCall(t *testing.T) {
	oldInline := inlineEnabled
	inlineEnabled = false
	t.Cleanup(func() { inlineEnabled = oldInline })
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		// f(x) = sum(g(x)); g returns x and x+1 in X0/X1.
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x6a, 0x0b}},
		funcDef{i32, []wasm.ValType{wasm.I32, wasm.I32}, []byte{0x00, 0x20, 0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
	)
	stats := compileWithStats(t, m, false).Funcs[0]
	if got := stats.Calls["regabi"]; got != 1 {
		t.Fatalf("regabi calls = %d, want 1 (all: %v)", got, stats.Calls)
	}
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// The caller has a stack-fence load through its module context. Enter via
	// the real wrapper/Engine contract instead of an uninitialized Call2 X26.
	// Its internal call to g still returns both values in X0/X1, as asserted
	// by the regabi census above.
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	code, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(code)
	args, results, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
	jm.SetStackFence(eng.StackLimit())
	for _, x := range []uint64{0, 1, 7, 100} {
		binary.LittleEndian.PutUint64(args, x)
		if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint64(results); uint32(got) != uint32(2*x+1) {
			t.Fatalf("f(%d) = %d, want %d", x, uint32(got), 2*x+1)
		}
	}
	// Prove that the same generated caller actually consumes the supplied
	// fence and returns through the checked trap path, rather than disabling it.
	jm.SetStackFence(^uintptr(0))
	err = eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results)
	var trapped *coreruntime.TrapError
	if !errors.As(err, &trapped) || trapped.Code != coreruntime.TrapStackFenceBreached {
		t.Fatalf("raised stack fence = %v, want stack-fence trap", err)
	}
	jm.SetStackFence(eng.StackLimit())
	if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint64(results); uint32(got) != 201 {
		t.Fatalf("post-trap result = %d, want 201", got)
	}
}
