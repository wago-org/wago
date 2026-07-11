//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

var readLoopBodyArm64 = []byte{
	0x01, 0x02, 0x7f,
	0x03, 0x40,
	0x20, 0x02, 0x20, 0x00, 0x28, 0x02, 0x00, 0x6a, 0x21, 0x02,
	0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01,
	0x20, 0x01, 0x41, 0x04, 0x48, 0x0d, 0x00,
	0x0b, 0x20, 0x02, 0x0b,
}

func runArm64WrapperMem(t *testing.T, m *wasm.Module, arg uint32, init func([]byte)) (uint32, error) {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
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
	if init != nil {
		init(jm.CurrentBytes())
	}
	ar, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(code)
	serArgs, results, trap := ar.Alloc(16), ar.Alloc(16), ar.Alloc(8)
	binary.LittleEndian.PutUint32(serArgs, arg)
	err = eng.Call(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results)
	return binary.LittleEndian.Uint32(results), err
}

func TestLoopPrecheckExecAndSlowTrapArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modMem(t, 1, i32, i32, readLoopBodyArm64)
	savedEn, savedK := loopPrecheckEnabled, loopPrecheckMinChecks
	loopPrecheckEnabled, loopPrecheckMinChecks = true, 1
	defer func() { loopPrecheckEnabled, loopPrecheckMinChecks = savedEn, savedK }()

	got, err := runArm64WrapperMem(t, m, 16, func(mem []byte) { binary.LittleEndian.PutUint32(mem[16:], 7) })
	if err != nil || got != 28 {
		t.Fatalf("fast path: got=%d err=%v, want 28", got, err)
	}
	if _, err := runArm64WrapperMem(t, m, 100000, nil); err == nil {
		t.Fatal("out-of-bounds slow path did not trap")
	}

	var ms ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
		t.Fatal(err)
	}
	if ms.Funcs[0].Peephole["loop-precheck"] == 0 {
		t.Fatalf("loop was not versioned: %v", ms.Funcs[0].Peephole)
	}
}

func TestLoopPrecheckBenefitGateArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modMem(t, 1, i32, i32, readLoopBodyArm64)
	savedEn, savedK := loopPrecheckEnabled, loopPrecheckMinChecks
	defer func() { loopPrecheckEnabled, loopPrecheckMinChecks = savedEn, savedK }()
	loopPrecheckEnabled = true

	for _, tc := range []struct {
		min  int
		want bool
	}{{3, false}, {1, true}} {
		loopPrecheckMinChecks = tc.min
		var ms ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
			t.Fatal(err)
		}
		got := ms.Funcs[0].Peephole["loop-precheck"] != 0
		if got != tc.want {
			t.Fatalf("min=%d versioned=%v, want %v", tc.min, got, tc.want)
		}
	}
}
