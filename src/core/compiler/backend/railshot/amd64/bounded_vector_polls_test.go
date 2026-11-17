//go:build amd64 && (linux || darwin || windows) && wago_experimental_bounded_vector_polls

package amd64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime"
	"math"
	"sort"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestBoundedVectorPollsMemoryOracle(t *testing.T) {
	old, force := regionLoopEnabled, regionLoopTestFast
	defer func() { regionLoopEnabled, regionLoopTestFast = old, force }()
	regionLoopTestFast = false
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for _, n := range []uint64{1, 2, 3, 4, 5, 8, 17, 2*boundedVectorPollGroups - 1, 2 * boundedVectorPollGroups, 2*boundedVectorPollGroups + 1, 4*boundedVectorPollGroups - 1, 4 * boundedVectorPollGroups, 4*boundedVectorPollGroups + 1, 8*boundedVectorPollGroups - 1, 8 * boundedVectorPollGroups, 8*boundedVectorPollGroups + 1} {
			for _, src := range []uint64{128, 4096, 132} {
				var want uint64
				var expected []byte
				for _, fast := range []bool{false, true} {
					regionLoopEnabled = fast
					init := func(mem []byte) {
						for i := uint64(0); i < n; i++ {
							binary.LittleEndian.PutUint64(mem[src+i*8:], math.Float64bits(float64(i+1)))
						}
					}
					got, mem, err := runMemAmd64WithOptions(t, vectorMapFixture(t), CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: features}, init, 128, src, 0, n, math.Float64bits(2), math.Float64bits(1.5))
					if err != nil {
						t.Fatal(features, n, src, fast, err)
					}
					if !fast {
						want, expected = got, mem
					} else if got != want || !bytes.Equal(mem, expected) {
						t.Fatal("bounded loop oracle mismatch", features, n, src)
					}
				}
			}
		}
	}
}

// Injection changes only a poll's MOV load to a same-width MOV store. The
// scratch register still contains the trap-cell pointer, whose nonzero low word
// is published then tested. The original cold stub canonicalizes interruption.
// No timing race, host callback, extra branch, or altered source site is used.
func boundedPollInjectionSites(code []byte) []int {
	var sites []int
	for reg := Reg(0); reg < 16; reg++ {
		a := &encoderamd64.Asm{}
		a.Load64(reg, RBX, -offTrapCellPtr)
		load := a.Len()
		a.Load32(reg, reg, 0)
		a.TestSelf(reg, false)
		for at := 0; at < len(code); {
			found := bytes.Index(code[at:], a.B)
			if found < 0 {
				break
			}
			start := at + found
			opcode := start + load
			if code[opcode] >= 0x40 && code[opcode] <= 0x4f {
				opcode++
			}
			if code[opcode] != 0x8b {
				panic("unexpected poll encoding")
			}
			sites = append(sites, opcode)
			at = start + len(a.B)
		}
	}
	sort.Ints(sites)
	return sites
}

func TestBoundedVectorPollsDeterministicInterruption(t *testing.T) {
	// No parallel subtests: existing region feature switches are process-global.
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		t.Run(fmt.Sprint(features), func(t *testing.T) { testBatchedVectorInterruption(t, features) })
	}
}

func testBatchedVectorInterruption(t *testing.T, features shared.AMD64Features) {
	old, force := regionLoopEnabled, regionLoopTestFast
	defer func() { regionLoopEnabled, regionLoopTestFast = old, force }()
	regionLoopEnabled, regionLoopTestFast = true, false
	cm, err := CompileModuleWith(vectorMapFixture(t), CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: features})
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
	lanes := 2
	if features&shared.AMD64AVX != 0 {
		lanes = 4
	}
	type interruptCase struct {
		name               string
		site, n, completed int
		src                uint64
	}
	cases := []interruptCase{
		{"entry", 0, lanes * boundedVectorPollGroups, 0, 4096},
		{"initial-header", 1, lanes * boundedVectorPollGroups, 0, 4096},
		{"alias-fallback", 2, lanes * boundedVectorPollGroups, 0, 132},
	}
	for _, groups := range []int{1, boundedVectorPollGroups - 1, boundedVectorPollGroups, boundedVectorPollGroups + 1, 2*boundedVectorPollGroups - 1, 2 * boundedVectorPollGroups, 2*boundedVectorPollGroups + 1} {
		first := groups % boundedVectorPollGroups
		if first == 0 {
			first = boundedVectorPollGroups
		}
		cases = append(cases, interruptCase{fmt.Sprintf("batch-%d-groups", groups), 3, groups * lanes, first * lanes, 4096})
	}
	if lanes == 4 {
		// Tail entry happens after the final batch poll and home reconciliation.
		cases = append(cases, interruptCase{"tail", 2, lanes*boundedVectorPollGroups + 1, lanes * boundedVectorPollGroups, 4096})
	} else {
		// The existing pair path rejects odd counts into the checked body.
		cases = append(cases, interruptCase{"odd-fallback", 2, lanes*boundedVectorPollGroups + 1, 0, 4096})
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
			for i := 0; i < tc.n; i++ {
				binary.LittleEndian.PutUint64(lin[int(tc.src)+i*8:], math.Float64bits(float64(i+1)))
			}
			expected := append([]byte(nil), lin...)
			for i := 0; i < tc.completed; i++ {
				binary.LittleEndian.PutUint64(expected[128+i*8:], math.Float64bits(float64(i+1)*2+1.5))
			}
			for i, v := range []uint64{128, tc.src, 0, uint64(tc.n), math.Float64bits(2), math.Float64bits(1.5)} {
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
			for i := 0; i < tc.n; i++ {
				binary.LittleEndian.PutUint64(lin[int(tc.src)+i*8:], math.Float64bits(float64(i+1)))
			}
			if err = eng.Call(normalEntry+uintptr(cm.Entry[0]), args, lin, trap, results); err != nil {
				t.Fatalf("post-interrupt reuse: %v", err)
			}
		})
	}
}
