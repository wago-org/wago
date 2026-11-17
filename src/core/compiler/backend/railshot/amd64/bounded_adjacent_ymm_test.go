//go:build amd64 && linux && wago_experimental_bounded_vector_polls && wago_experimental_adjacent_ymm

package amd64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	goruntime "runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/runtime"
)

func TestGemmYMMDelayedPollInterruption(t *testing.T) {
	// The tag is opt-in; require an actual wide minimal fixture, not fallback.
	oldWide := regionWideAdjacentEnabled
	regionWideAdjacentEnabled = true
	defer func() { regionWideAdjacentEnabled = oldWide }()
	old, force := regionAdjacentEnabled, regionLoopTestFast
	defer func() { regionAdjacentEnabled, regionLoopTestFast = old, force }()
	regionAdjacentEnabled, regionLoopTestFast = true, false
	for _, features := range []shared.AMD64Features{shared.AMD64AVX, shared.AMD64ModernBaseline} {
		t.Run(fmt.Sprint(features), func(t *testing.T) {
			cm, err := CompileModuleWith(gemmAdjacentPollFixture(t), CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: features})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if !bytes.Contains(cm.Code, []byte{0xc5, 0xf8, 0x77}) {
				t.Fatal("YMM exit missing VZeroUpper; no wide path")
			}
			if len(boundedPollInjectionSites(cm.Code)) != 4 {
				t.Fatal("actual GEMM packed path absent")
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
			args, results, trap, counter := arena.Alloc(256), arena.Alloc(256), arena.Alloc(runtime.TrapBufferBytes), arena.Alloc(8)
			normal, entry, err := runtime.MapCode(cm.Code)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Unmap(normal)
			for _, n := range []int{1, 2, 3, 2*boundedVectorPollGroups - 1, 2 * boundedVectorPollGroups, 2*boundedVectorPollGroups + 1, 4 * boundedVectorPollGroups, 4*boundedVectorPollGroups + 1, 6 * boundedVectorPollGroups, 35} {
				groups := n / 2
				polls := (groups + boundedVectorPollGroups - 1) / boundedVectorPollGroups
				type point struct{ site, target, completed, polls int }
				var points []point
				first := groups % boundedVectorPollGroups
				if first == 0 {
					first = boundedVectorPollGroups
				}
				if polls > 0 {
					points = append(points, point{3, polls, 2 * (first + (polls-1)*boundedVectorPollGroups), polls})
				}
				if polls > 2 {
					points = append(points, point{3, 2, 2 * (first + boundedVectorPollGroups), polls})
				}
				if n%2 != 0 {
					points = append(points, point{2, 1, n - 1, 1})
				}
				for _, tc := range points {
					target := tc.target
					polls := tc.polls
					t.Run(fmt.Sprintf("n%d-site%d-visit%d", n, tc.site, target), func(t *testing.T) {
						clear(counter)
						clear(trap)
						code := delayedPairPollCode(t, cm.Code, tc.site, counter, target)
						mapped, patchedEntry, err := runtime.MapCode(code)
						if err != nil {
							t.Fatal(err)
						}
						defer runtime.Unmap(mapped)
						lin := jm.LinearMemory()
						init := func() {
							clear(lin)
							binary.LittleEndian.PutUint64(lin[8192:], math.Float64bits(2))
							for i := 0; i < 2*n; i++ {
								binary.LittleEndian.PutUint64(lin[4096+i*8:], math.Float64bits(float64(i+1)))
								binary.LittleEndian.PutUint64(lin[128+i*8:], math.Float64bits(1))
							}
						}
						init()
						expected := append([]byte(nil), lin...)
						completed := tc.completed
						for i := 0; i < 2*completed; i++ {
							binary.LittleEndian.PutUint64(expected[128+i*8:], math.Float64bits(float64(i+1)*3+1))
						}
						for i, v := range []uint64{128, 8192, 4096, 0, uint64(n) * 16} {
							binary.LittleEndian.PutUint64(args[i*8:], v)
						}
						err = eng.Call(patchedEntry+uintptr(cm.Entry[0]), args, lin, trap, results)
						goruntime.KeepAlive(counter)
						var te *runtime.TrapError
						if !errors.As(err, &te) || te.Code != runtime.TrapInterrupted {
							t.Fatalf("want delayed interruption: %v", err)
						}
						if binary.LittleEndian.Uint32(counter) != uint32(target) {
							t.Fatalf("wrong poll visit count: %d", binary.LittleEndian.Uint32(counter))
						}
						if len(te.Frames) == 0 || te.Frames[0].FunctionIndex != 0 || te.Frames[0].HasProgramCounter {
							t.Fatalf("wrong trap origin: %+v", te.Frames)
						}
						if !bytes.Equal(lin, expected) {
							t.Fatalf("not exact %d-source-iteration prefix", completed)
						}
						// Same trampoline with unreachable activation proves successful checks,
						// scratch reload, counter flags and balanced stack through normal return.
						clear(counter)
						clear(trap)
						init()
						passCode := delayedPairPollCode(t, cm.Code, tc.site, counter, polls+1)
						pass, passEntry, err := runtime.MapCode(passCode)
						if err != nil {
							t.Fatal(err)
						}
						defer runtime.Unmap(pass)
						if err = eng.Call(passEntry+uintptr(cm.Entry[0]), args, lin, trap, results); err != nil {
							t.Fatal("successful-poll path failed", err)
						}
						goruntime.KeepAlive(counter)
						if binary.LittleEndian.Uint32(counter) != uint32(polls) || binary.LittleEndian.Uint64(results) != uint64(n*16) {
							t.Fatal("wrong final poll count or local counter")
						}
						got := append([]byte(nil), lin...)
						clear(trap)
						init()
						if err = eng.Call(entry+uintptr(cm.Entry[0]), args, lin, trap, results); err != nil {
							t.Fatal("engine reuse failed", err)
						}
						if !bytes.Equal(got, lin) {
							t.Fatal("instrumentation changed completed memory")
						}
					})
				}
			}
		})
	}
}

func TestGemmYMMCheckedTailFallback(t *testing.T) {
	oldWide, oldTail := regionWideAdjacentEnabled, regionWideCheckedTailEnabled
	defer func() { regionWideAdjacentEnabled, regionWideCheckedTailEnabled = oldWide, oldTail }()
	regionWideAdjacentEnabled = true
	for _, checked := range []bool{false, true} {
		regionWideCheckedTailEnabled = checked
		m := gemmAdjacentPollFixture(t)
		opts := CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: shared.AMD64AVX}
		cm, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		wide := bytes.Contains(cm.Code, []byte{0xc5, 0xf8, 0x77})
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
		if wide != checked {
			t.Fatalf("checked=%v wide=%v", checked, wide)
		}
		for _, n := range []uint64{1, 2, 3, 35} {
			init := func(mem []byte) {
				binary.LittleEndian.PutUint64(mem[8192:], math.Float64bits(2))
				for i := uint64(0); i < 2*n; i++ {
					binary.LittleEndian.PutUint64(mem[4096+i*8:], math.Float64bits(float64(i+1)))
					binary.LittleEndian.PutUint64(mem[128+i*8:], math.Float64bits(1))
				}
			}
			got, mem, err := runMemAmd64WithOptions(t, m, opts, init, 128, 8192, 4096, 0, n*16)
			if err != nil || got != n*16 {
				t.Fatalf("checked=%v n=%d result=%d err=%v", checked, n, got, err)
			}
			for i := uint64(0); i < 2*n; i++ {
				if binary.LittleEndian.Uint64(mem[128+i*8:]) != math.Float64bits(float64(i+1)*3+1) {
					t.Fatal("wrong output", checked, n, i)
				}
			}
		}
	}
}
