//go:build amd64 && linux && wago_experimental_bounded_vector_polls

package amd64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/wago-org/wago/src/core/runtime"
	"math"

	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAdjacentPairPollsDeterministicInterruption(t *testing.T) {
	// This existing fixture qualifies the XMM policy; YMM has separate tests.
	oldWide := regionWideAdjacentEnabled
	regionWideAdjacentEnabled = false
	defer func() { regionWideAdjacentEnabled = oldWide }()
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		t.Run(fmt.Sprint(features), func(t *testing.T) { testAdjacentPairInterruption(t, features) })
	}
}

func testAdjacentPairInterruption(t *testing.T, features shared.AMD64Features) {
	old, force := regionAdjacentEnabled, regionLoopTestFast
	defer func() { regionAdjacentEnabled, regionLoopTestFast = old, force }()
	regionAdjacentEnabled, regionLoopTestFast = true, false
	cm, err := CompileModuleWith(adjacentLoopFixture(t, 0xa0), CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: features})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	original := append([]byte(nil), cm.Code...)
	sites := boundedPollInjectionSites(original)
	// Entry, ordinary header, checked remainder, and packed-group check.
	if len(sites) != 4 {
		t.Fatalf("expected four bounded polls, got %v", sites)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	args, results, trap := arena.Alloc(256), arena.Alloc(256), arena.Alloc(runtime.TrapBufferBytes)
	if uint32(uintptr(unsafe.Pointer(&trap[0]))) == 0 {
		t.Fatal("trap pointer unsuitable for deterministic nonzero injection")
	}
	type interruptCase struct {
		name               string
		site, n, completed int
		src                uint64
	}
	cases := []interruptCase{{"entry", 0, 8, 0, 4096}, {"initial-header", 1, 8, 0, 4096}, {"alias-fallback", 2, 8, 0, 132}, {"oob-fallback", 2, 8, 0, 65528}, {"wrap-fallback", 2, 8, 0, 0xfffffff8}}
	for _, n := range []int{1, boundedVectorPollGroups - 1, boundedVectorPollGroups, boundedVectorPollGroups + 1, 2*boundedVectorPollGroups - 1, 2 * boundedVectorPollGroups, 2*boundedVectorPollGroups + 1, 35} {
		first := n % boundedVectorPollGroups
		if first == 0 {
			first = boundedVectorPollGroups
		}
		cases = append(cases, interruptCase{fmt.Sprintf("pair-batch-%d", n), 3, n, first, 4096})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := append([]byte(nil), original...)
			code[sites[tc.site]] = 0x89
			mapped, entry, err := runtime.MapCode(code)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Unmap(mapped)
			lin := jm.LinearMemory()
			clear(lin)
			for i := 0; i < 2*tc.n && tc.src+uint64(i*8)+8 <= uint64(len(lin)); i++ {
				binary.LittleEndian.PutUint64(lin[int(tc.src)+i*8:], math.Float64bits(float64(i+1)))
			}
			for i := 0; i < 2*tc.n; i++ {
				binary.LittleEndian.PutUint64(lin[8192+i*8:], math.Float64bits(3))
			}
			expected := append([]byte(nil), lin...)
			for i := 0; i < 2*tc.completed; i++ {
				binary.LittleEndian.PutUint64(expected[128+i*8:], math.Float64bits(float64(i+1)+3))
			}
			for i, v := range []uint64{128, tc.src, 8192, 0, uint64(tc.n)} {
				binary.LittleEndian.PutUint64(args[i*8:], v)
			}
			err = eng.Call(entry+uintptr(cm.Entry[0]), args, lin, trap, results)
			var te *runtime.TrapError
			if !errors.As(err, &te) || te.Code != runtime.TrapInterrupted {
				t.Fatalf("want interruption: %v", err)
			}
			// Existing grouped interrupt stubs deliberately discard site-specific PCs.
			if tc.site != 0 {
				if len(te.Frames) == 0 || te.Frames[0].FunctionIndex != 0 || te.Frames[0].HasProgramCounter {
					t.Fatalf("wrong grouped interruption origin: %+v", te.Frames)
				}
			}
			if !bytes.Equal(lin, expected) {
				t.Fatalf("writes are not the expected %d-iteration prefix", tc.completed)
			}
			// The same engine and job must remain usable after cold unwinding.
			normal, normalEntry, err := runtime.MapCode(original)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Unmap(normal)
			clear(trap)
			clear(lin)
			for i := 0; i < 2*tc.n && tc.src+uint64(i*8)+8 <= uint64(len(lin)); i++ {
				binary.LittleEndian.PutUint64(lin[int(tc.src)+i*8:], math.Float64bits(float64(i+1)))
			}
			// Reuse checks a valid invocation even when the interrupted input was
			// deliberately OOB/wrapping; the original input must still trap normally.
			binary.LittleEndian.PutUint64(args[8:], 4096)
			if err = eng.Call(normalEntry+uintptr(cm.Entry[0]), args, lin, trap, results); err != nil {
				t.Fatalf("post-interrupt reuse: %v", err)
			}
		})
	}
}

// Exact inner GEMM shape: two adjacent updates, advancing offset by 16.
func gemmAdjacentPollFixture(t *testing.T) *wasm.Module {
	b := []byte{1, 2, 0x7f, 3, 0x40}
	b = append(b, 0x20, 0, 0x20, 3, 0x6a, 0x22, 5)
	for lane := byte(0); lane < 2; lane++ {
		if lane != 0 {
			b = append(b, 0x20, 5, 0x41, 8, 0x6a, 0x22, 5)
		}
		b = append(b, 0x20, 1, 0x2b, 0, 0, 0x44, 0, 0, 0, 0, 0, 0, 0xf8, 0x3f, 0xa2)
		if lane == 0 {
			b = append(b, 0x20, 3, 0x20, 2, 0x6a, 0x22, 6)
		} else {
			b = append(b, 0x20, 6, 0x41, 8, 0x6a)
		}
		b = append(b, 0x2b, 0, 0, 0xa2, 0x20, 5, 0x2b, 0, 0, 0xa0, 0x39, 0, 0)
	}
	b = append(b, 0x20, 3, 0x41, 16, 0x6a, 0x22, 3, 0x20, 4, 0x47, 0x0d, 0, 0xb, 0x20, 3, 0xad, 0xb)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, b)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}
func TestGemmAdjacentPollAdmission(t *testing.T) {
	m := gemmAdjacentPollFixture(t)
	b := m.Code[0].BodyBytes
	at := bytes.Index(b, []byte{3, 0x40})
	r := wasm.NewReader(b[at+2:])
	var p regionLoopPlan
	if !inspectRegionLoop(*r, []machineType{mtI32, mtI32, mtI32, mtI32, mtI32, mtI32, mtI32}, wasm.NewModuleInstructionClassifier(m, true), &p) {
		t.Fatal("GEMM shape rejected")
	}
	if p.independentLanes() || p.scalarMemoryRecurrence() {
		t.Fatal("wrong GEMM classification")
	}
	if !p.packAdjacentOutputs() || p.storeN != 1 || !p.contiguousAdjacentIterations() || p.iterationsPerVector() != 1 {
		t.Fatal("GEMM pair proof rejected")
	}
}
func TestGemmAdjacentPollExactResultsAndFallback(t *testing.T) {
	// This existing fixture qualifies the XMM policy; YMM has separate tests.
	oldWide := regionWideAdjacentEnabled
	regionWideAdjacentEnabled = false
	defer func() { regionWideAdjacentEnabled = oldWide }()
	old, force := regionAdjacentEnabled, regionLoopTestFast
	defer func() { regionAdjacentEnabled, regionLoopTestFast = old, force }()
	regionLoopTestFast = false
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for _, n := range []uint64{1, 3, 4, 5, 7, 8, 9, 35} {
			for _, src := range []uint64{128, 132, 4096, 65528, 0xfffffff8} {
				var want uint64
				var expected []byte
				var wantTrap runtime.TrapCode
				for _, enabled := range []bool{false, true} {
					regionAdjacentEnabled = enabled
					init := func(mem []byte) {
						for i := 0; i < 2*int(n); i++ {
							binary.LittleEndian.PutUint64(mem[128+i*8:], math.Float64bits(float64(i+1)))
							if src+uint64(i*8)+8 <= uint64(len(mem)) {
								binary.LittleEndian.PutUint64(mem[int(src)+i*8:], math.Float64bits(float64(i+2)))
							}
						}
						binary.LittleEndian.PutUint64(mem[8192:], math.Float64bits(2))
					}
					got, mem, err := runMemAmd64WithOptions(t, gemmAdjacentPollFixture(t), CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: features}, init, 128, 8192, src, 0, n*16)
					var code runtime.TrapCode
					var te *runtime.TrapError
					if err != nil {
						if !errors.As(err, &te) {
							t.Fatal(err)
						}
						code = te.Code
					}
					if !enabled {
						want, expected, wantTrap = got, append([]byte(nil), mem...), code
					} else if got != want || code != wantTrap || !bytes.Equal(mem, expected) {
						t.Fatal("GEMM result/trap/prefix differs", features, n, src)
					}
				}
			}
		}
	}
}

func TestGemmPairPollsDeterministicInterruption(t *testing.T) {
	// This existing fixture qualifies the XMM policy; YMM has separate tests.
	oldWide := regionWideAdjacentEnabled
	regionWideAdjacentEnabled = false
	defer func() { regionWideAdjacentEnabled = oldWide }()
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		t.Run(fmt.Sprint(features), func(t *testing.T) { testGemmPairInterruption(t, features) })
	}
}
func testGemmPairInterruption(t *testing.T, features shared.AMD64Features) {
	old, force := regionAdjacentEnabled, regionLoopTestFast
	defer func() { regionAdjacentEnabled, regionLoopTestFast = old, force }()
	regionAdjacentEnabled, regionLoopTestFast = true, false
	cm, err := CompileModuleWith(gemmAdjacentPollFixture(t), CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: features})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	original := append([]byte(nil), cm.Code...)
	sites := boundedPollInjectionSites(original)
	// Entry, ordinary header, checked remainder, and packed-group check.
	if len(sites) != 4 {
		t.Fatalf("expected four bounded polls, got %v", sites)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	args, results, trap := arena.Alloc(256), arena.Alloc(256), arena.Alloc(runtime.TrapBufferBytes)
	if uint32(uintptr(unsafe.Pointer(&trap[0]))) == 0 {
		t.Fatal("trap pointer unsuitable for deterministic nonzero injection")
	}
	type interruptCase struct {
		name               string
		site, n, completed int
		src                uint64
	}
	cases := []interruptCase{{"entry", 0, 8, 0, 4096}, {"initial-header", 1, 8, 0, 4096}, {"alias-fallback", 2, 8, 0, 132}, {"oob-fallback", 2, 8, 0, 65528}, {"wrap-fallback", 2, 8, 0, 0xfffffff8}}
	for _, n := range []int{1, boundedVectorPollGroups - 1, boundedVectorPollGroups, boundedVectorPollGroups + 1, 2*boundedVectorPollGroups - 1, 2 * boundedVectorPollGroups, 2*boundedVectorPollGroups + 1, 35} {
		first := n % boundedVectorPollGroups
		if first == 0 {
			first = boundedVectorPollGroups
		}
		cases = append(cases, interruptCase{fmt.Sprintf("pair-batch-%d", n), 3, n, first, 4096})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := append([]byte(nil), original...)
			code[sites[tc.site]] = 0x89
			mapped, entry, err := runtime.MapCode(code)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Unmap(mapped)
			lin := jm.LinearMemory()
			clear(lin)
			for i := 0; i < 2*tc.n && tc.src+uint64(i*8)+8 <= uint64(len(lin)); i++ {
				binary.LittleEndian.PutUint64(lin[int(tc.src)+i*8:], math.Float64bits(float64(i+1)))
			}
			binary.LittleEndian.PutUint64(lin[8192:], math.Float64bits(2))
			expected := append([]byte(nil), lin...)
			for i := 0; i < 2*tc.completed; i++ {
				binary.LittleEndian.PutUint64(expected[128+i*8:], math.Float64bits(float64(i+1)*3+math.Float64frombits(binary.LittleEndian.Uint64(expected[128+i*8:]))))
			}
			for i, v := range []uint64{128, 8192, tc.src, 0, uint64(tc.n) * 16} {
				binary.LittleEndian.PutUint64(args[i*8:], v)
			}
			err = eng.Call(entry+uintptr(cm.Entry[0]), args, lin, trap, results)
			var te *runtime.TrapError
			if !errors.As(err, &te) || te.Code != runtime.TrapInterrupted {
				t.Fatalf("want interruption: %v", err)
			}
			// Existing grouped interrupt stubs deliberately discard site-specific PCs.
			if tc.site != 0 {
				if len(te.Frames) == 0 || te.Frames[0].FunctionIndex != 0 || te.Frames[0].HasProgramCounter {
					t.Fatalf("wrong grouped interruption origin: %+v", te.Frames)
				}
			}
			if !bytes.Equal(lin, expected) {
				t.Fatalf("writes are not the expected %d-iteration prefix", tc.completed)
			}
			// The same engine and job must remain usable after cold unwinding.
			normal, normalEntry, err := runtime.MapCode(original)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Unmap(normal)
			clear(trap)
			clear(lin)
			for i := 0; i < 2*tc.n && tc.src+uint64(i*8)+8 <= uint64(len(lin)); i++ {
				binary.LittleEndian.PutUint64(lin[int(tc.src)+i*8:], math.Float64bits(float64(i+1)))
			}
			// Reuse checks a valid invocation even when the interrupted input was
			// deliberately OOB/wrapping; the original input must still trap normally.
			binary.LittleEndian.PutUint64(args[16:], 4096)
			if err = eng.Call(normalEntry+uintptr(cm.Entry[0]), args, lin, trap, results); err != nil {
				t.Fatalf("post-interrupt reuse: %v", err)
			}
		})
	}
}

// Force-fast inserts a trap at the fallback entry. Passing the enabled run
// therefore proves actual emitted GEMM packing, not only planner acceptance.
func TestGemmPairEmittedPackingAndFloatEdges(t *testing.T) {
	// This existing fixture qualifies the XMM policy; YMM has separate tests.
	oldWide := regionWideAdjacentEnabled
	regionWideAdjacentEnabled = false
	defer func() { regionWideAdjacentEnabled = oldWide }()
	old, force := regionAdjacentEnabled, regionLoopTestFast
	defer func() { regionAdjacentEnabled, regionLoopTestFast = old, force }()
	type values struct {
		a, b, c uint64
		nan     bool
	}
	cases := []values{
		{0x3ff0000000000001, 0x3fefffffffffffff, 0xbff8000000000000, false},
		{0x8000000000000000, 0x3ff0000000000000, 0x8000000000000000, false},
		{0, 0x3ff0000000000000, 0x8000000000000000, false},
		{0x0010000000000001, 0x3fe0000000000000, 1, false},
		{0x7ff8000000001234, 0x3ff0000000000000, 0, true},
		{0x3ff0000000000000, 0x7ff8000000004321, 0, true},
	}
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		regionAdjacentEnabled, regionLoopTestFast = true, true
		cm, err := CompileModuleWith(gemmAdjacentPollFixture(t), CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: features})
		if err != nil {
			t.Fatal(err)
		}
		// This single-loop fixture must contain a distinct packed-batch poll
		// in addition to entry, initial header and checked fallback. Thus even
		// a rejected optimizer with no diagnostic fallback cannot pass silently.
		sites := boundedPollInjectionSites(cm.Code)
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
		if len(sites) != 4 {
			t.Fatalf("GEMM packed-batch emission absent: %v", sites)
		}
		for _, n := range []uint64{1, boundedVectorPollGroups, boundedVectorPollGroups + 1, 35} {
			for _, v := range cases {
				var want uint64
				var expected []byte
				for _, fast := range []bool{false, true} {
					regionAdjacentEnabled, regionLoopTestFast = fast, fast
					init := func(mem []byte) {
						binary.LittleEndian.PutUint64(mem[8192:], v.a)
						for i := uint64(0); i < 2*n; i++ {
							binary.LittleEndian.PutUint64(mem[4096+i*8:], v.b)
							binary.LittleEndian.PutUint64(mem[128+i*8:], v.c)
						}
					}
					got, mem, err := runMemAmd64WithOptions(t, gemmAdjacentPollFixture(t), CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: features}, init, 128, 8192, 4096, 0, n*16)
					if err != nil {
						t.Fatalf("GEMM packed emission/fallback failure: fast=%v features=%v n=%v: %v", fast, features, n, err)
					}
					if !fast {
						want, expected = got, mem
						continue
					}
					if got != want {
						t.Fatal("GEMM final counter mismatch")
					}
					for i := uint64(0); i < 2*n; i++ {
						off := 128 + i*8
						x := binary.LittleEndian.Uint64(mem[off:])
						y := binary.LittleEndian.Uint64(expected[off:])
						if v.nan {
							// Payload/sign selection is not fixed by Wasm arithmetic NaN rules.
							// These inputs contain noncanonical quiet NaNs: require arithmetic NaNs.
							const arithmeticNaN = uint64(0x7ff8000000000000)
							if x&arithmeticNaN != arithmeticNaN || y&arithmeticNaN != arithmeticNaN {
								t.Fatal("not an arithmetic NaN", x, y)
							}
							binary.LittleEndian.PutUint64(mem[off:], y)
						} else if x != y {
							t.Fatalf("FP evaluation/order/zero-sign differs: %x != %x", x, y)
						}
					}
					if !bytes.Equal(mem, expected) {
						t.Fatal("unexpected memory outside paired outputs")
					}
				}
			}
		}
	}
}
