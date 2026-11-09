//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func loopLocalLeaseFixture(t testing.TB) *wasm.Module {
	// A high-index accumulator and induction local exercise the absence of
	// function-wide merge snapshots. The identity call runs after publication.
	b := []byte{2, 67, 0x7f, 5, 0x7e, 0x20, 0, 0x21, 3, 0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0,
		0x03, 0x40, 0x20, 70, 0x20, 70, 0x20, 3, 0xad, 0x7c, 0x22, 70, 0x85, 0x21, 70,
		0x20, 3, 0x41, 1, 0x6b, 0x22, 3, 0x0d, 0, 0x0b, 0x0b, 0x20, 70, 0x10, 1, 0x0b}
	m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I64}, body: b},
		funcDef{params: []wasm.ValType{wasm.I64}, results: []wasm.ValType{wasm.I64}, body: []byte{0, 0x20, 0, 0x0b}})
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestLoopLocalLeaseMemoryAndTrapPrefix(t *testing.T) {
	old := loopLocalLeaseEnabled
	defer func() { loopLocalLeaseEnabled = old }()
	b := []byte{2, 67, 0x7f, 6, 0x7e, 0x20, 0, 0x21, 3, 0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40}
	for i := byte(0); i < 6; i++ {
		if i == 1 {
			b = append(b, 0x20, 1)
		} else {
			b = append(b, 0x41)
			b = append(b, wasmtest.SLEB32(int32(i)*8)...)
		}
		b = append(b, 0x20, 69+i, 0x20, 3, 0xad, 0x7c, 0x22, 69+i, 0x37, 0, 0)
	}
	b = append(b, 0x20, 3, 0x41, 1, 0x6b, 0x22, 3, 0x0d, 0, 0x0b, 0x0b)
	for i := byte(0); i < 6; i++ {
		b = append(b, 0x41)
		b = append(b, wasmtest.SLEB32(64+int32(i)*8)...)
		b = append(b, 0x20, 69+i, 0x37, 0, 0)
	}
	b = append(b, 0x41, 0, 0x0b)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, on := range []bool{false, true} {
		loopLocalLeaseEnabled = on
		for _, compact := range []bool{false, true} {
			for _, n := range []uint64{0, 1, 2, 3, 7, 16, 127} {
				for _, address := range []uint64{8, 65536} {
					want := make([]byte, 65536)
					traps := n != 0 && address == 65536
					if traps {
						binary.LittleEndian.PutUint64(want, n)
					} else {
						v := n * (n + 1) / 2
						for i := 0; i < 6; i++ {
							binary.LittleEndian.PutUint64(want[i*8:], v)
							binary.LittleEndian.PutUint64(want[64+i*8:], v)
						}
					}
					got, err := runScalarUnrollMemory(t, m, CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"wide-reuse-pins": false, "interval-region-pins": false}}, n, address)
					if (err != nil) != traps || !bytes.Equal(got, want) {
						t.Fatalf("on%v compact%v n%d address%d err%v expectedTrap%v memoryEqual%v", on, compact, n, address, err, traps, bytes.Equal(got, want))
					}
				}
			}
		}
	}
}

func TestLoopLocalLeaseProbeBounds(t *testing.T) {
	old := loopLocalLeaseEnabled
	loopLocalLeaseEnabled = true
	defer func() { loopLocalLeaseEnabled = old }()
	m := loopLocalLeaseFixture(t)
	f := fn{nLocals: 73, localType: make([]machineType, 73), classifier: wasm.NewModuleInstructionClassifier(m, true)}
	for i := range f.localType {
		f.localType[i] = mtI32
	}
	body := []byte{0x20, 3, 0x20, 3, 0x6a, 0x21, 3, 0x20, 3, 0x0d, 0, 0x0b}
	allocs := testing.AllocsPerRun(100, func() {
		f.loopLocalLeaseWork = 0
		r := wasm.ReaderFrom(body)
		plan, ok := f.planLoopLocalLease(&r)
		if !ok || plan.count != 1 || plan.local[0] != 3 || r.Offset() != 0 {
			panic("invalid probe")
		}
	})
	if allocs != 0 {
		t.Fatalf("probe allocates %v", allocs)
	}
	for _, prefix := range [][]byte{{0x02, 0x40}, {0x03, 0x40}, {0x04, 0x40}, {0x0c, 0}, {0x10, 1}, {0x11, 0, 0}, {0x25, 0}, {0xfc, 10, 0, 0}, {0xfd, 0}, {0xfe, 0}, {0xfb, 0}, bytes.Repeat([]byte{1}, 129)} {
		f.loopLocalLeaseWork = 0
		r := wasm.ReaderFrom(append(append([]byte{}, prefix...), body...))
		if _, ok := f.planLoopLocalLease(&r); ok || r.Offset() != 0 {
			t.Fatalf("admitted barrier %x", prefix)
		}
	}
	f.loopLocalLeaseWork = loopLocalLeaseWorkLimit - 1
	r := wasm.ReaderFrom(body)
	if _, ok := f.planLoopLocalLease(&r); ok || f.loopLocalLeaseWork != loopLocalLeaseWorkLimit || r.Offset() != 0 {
		t.Fatal("work budget not enforced")
	}
}

func TestLoopLocalLeaseExactExecution(t *testing.T) {
	old := loopLocalLeaseEnabled
	defer func() { loopLocalLeaseEnabled = old }()
	for _, on := range []bool{false, true} {
		loopLocalLeaseEnabled = on
		for _, compact := range []bool{false, true} {
			for _, n := range []uint64{0, 1, 2, 3, 7, 16, 127, 4097} {
				got, err := runArm64WrapperWithOptions(t, loopLocalLeaseFixture(t), CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"inline": false}}, n)
				// Capture the old accumulator before tee overwrites its register.
				want := uint64(0)
				for counter := n; counter != 0; counter-- {
					want ^= want + counter
				}
				if err != nil || got != want {
					t.Fatalf("on%v compact%v n%d got%d want%d err%v", on, compact, n, got, want, err)
				}
			}
		}
	}
}

func TestLoopLocalLeaseAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := loopLocalLeaseEnabled
	defer func() { loopLocalLeaseEnabled = old }()
	for _, on := range []bool{false, true} {
		loopLocalLeaseEnabled = on
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(loopLocalLeaseFixture(t), CompileOptions{Stats: stats, Optimizations: map[string]bool{"inline": false}})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		if hit := stats.Funcs[0].Peephole["loop-local-lease"]; (hit != 0) != on {
			t.Fatalf("on%v hit%d", on, hit)
		}
	}
}
