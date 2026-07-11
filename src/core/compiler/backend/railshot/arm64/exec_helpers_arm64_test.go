//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// runArm64Wrapper compiles function 0, runs it through the real arm64 runtime via
// the wrapper (serialized-args) ABI, and returns the raw 64-bit result word plus
// any trap error. The wrapper path takes any argument count, unlike the register
// ABI's Call2/Call3 helpers, so it is the general N-arg executor these regression
// suites need. args are written as 8-byte little-endian words, matching the
// serialized-args layout the wrapper prologue expects (mirrors amd64 runAmd64u).
func runArm64Wrapper(t *testing.T, m *wasm.Module, args ...uint64) (uint64, error) {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("arm64 compile: %v", err)
	}
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
	ar, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	defer coreruntime.Unmap(code)

	serArgs := ar.Alloc(256)
	results := ar.Alloc(256)
	trap := ar.Alloc(8)
	for i, a := range args {
		binary.LittleEndian.PutUint64(serArgs[i*8:], a)
	}
	err = eng.Call(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results)
	return binary.LittleEndian.Uint64(results), err
}

// runArm64u runs function 0 and returns its 64-bit result, failing the test on a
// trap. Mirrors amd64's runAmd64u.
func runArm64u(t *testing.T, m *wasm.Module, args ...uint64) uint64 {
	t.Helper()
	got, err := runArm64Wrapper(t, m, args...)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	return got
}

// runArm64 runs function 0 with int32 args and returns the low 32 bits of its
// result as an int32. Mirrors amd64's runAmd64.
func runArm64(t *testing.T, m *wasm.Module, args ...int32) int32 {
	t.Helper()
	uargs := make([]uint64, len(args))
	for i, a := range args {
		uargs[i] = uint64(uint32(a))
	}
	return int32(uint32(runArm64u(t, m, uargs...)))
}
