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
	serArgs, results, trap := ar.Alloc(16), ar.Alloc(16), ar.Alloc(coreruntime.TrapBufferBytes)
	binary.LittleEndian.PutUint32(serArgs, arg)
	err = eng.Call(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results)
	return binary.LittleEndian.Uint32(results), err
}

func TestLoopPrecheckScannerConsumesMixedMemoryWidthImmediatesArm64(t *testing.T) {
	m := &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}, {Limits: wasm.Limits{Min: 1, Addr64: true}}}}
	body := []byte{
		0x20, 0x00, // local.get 0
		0xfd, 0x00, 0x44, 0x01, 0x80, 0x80, 0x80, 0x80, 0x10, // v128.load memory 1, offset 2^32
		0x1a,       // drop
		0x41, 0x00, // i32.const 0
		0x40, 0x00, // memory.grow 0
		0x1a,       // drop
		0x10, 0x00, // call 0 after the wide immediate
		0x0b,
	}
	set, grow, call, nested, table := scanLoopBody(wasm.NewReader(body), m)
	if len(set) != 0 || !grow || !call || nested || table {
		t.Fatalf("mixed-width loop facts = set=%v grow=%v call=%v nested=%v table=%v", set, grow, call, nested, table)
	}
	cands, n, grow := scanLoopHoistable(wasm.NewReader(body), m)
	if len(cands) != 0 || n != 0 || !grow {
		t.Fatalf("mixed-width loop scan = cands=%v n=%d grow=%v", cands, n, grow)
	}
}

func TestLoopBodyScannerCompactsModifiedLocalsArm64(t *testing.T) {
	body := []byte{0x21, 0x03, 0x22, 0x01, 0x21, 0x03, 0x0b}
	classifier := wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)
	got, _, _, _, _ := scanLoopBodyWithClassifier(wasm.NewReader(body), classifier, []uint16{9})
	want := []uint16{9, 1, 3}
	if len(got) != len(want) {
		t.Fatalf("modified locals = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("modified locals = %v, want %v", got, want)
		}
	}
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
