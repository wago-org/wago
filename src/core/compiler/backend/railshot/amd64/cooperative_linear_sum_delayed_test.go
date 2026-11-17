//go:build amd64 && linux && wago_experimental_cooperative_linear_sum && wago_experimental_bounded_vector_polls

package amd64

import (
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/runtime"
	"testing"
)

// Reuse the independently tested poll-prefix trampoline. Only the test binary
// is patched; original TEST/trap branches and all guest instructions remain.
func TestCooperativeLinearSumDelayedCheckpoints(t *testing.T) {
	cm, err := CompileModuleWith(linearSumLoopModuleAMD64(t), CompileOptions{Interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	original := append([]byte(nil), cm.Code...)
	sites := boundedPollInjectionSites(original)
	// Function entry, original header, first scalar, group, scalar remainder.
	if len(sites) != 5 {
		t.Fatalf("unrolled checkpoints not emitted: %v", sites)
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
	args, results, trap, count := arena.Alloc(64), arena.Alloc(64), arena.Alloc(runtime.TrapBufferBytes), arena.Alloc(8)
	cases := []struct {
		site, visit int
		n           uint64
	}{
		{0, 1, 0}, {1, 1, 0}, {2, 1, 1}, // pre-entry, zero loop, terminal first scalar
		{3, 1, 5}, {3, 2, 1 + 3*linearSumPollLoads}, {3, 2, 1 + 2*linearSumPollLoads}, {3, 3, 1 + 3*linearSumPollLoads}, // final groups after prior polls
		{4, 1, 2}, {4, 2, 3}, {4, 3, 4}, {4, 3, 8}, // final scalar tails
		{1, 2, 8193}, {1, 3, 1<<32 - 1}, // failed full-range guard retains scalar polling
	}
	// Every tail residue must retain the final group poll. In particular, a
	// remaining&63 mask would incorrectly skip it for residues one to three.
	for tail := uint64(0); tail < 4; tail++ {
		cases = append(cases, struct {
			site, visit int
			n           uint64
		}{3, 2, 1 + 2*linearSumPollLoads + tail})
	}
	// Outlined group poll is emitted after the tail, but logical test sites
	// remain first/group/tail. All original assertions still execute.
	physicalSite := func(site int) int {
		if linearSumPollOutline && linearSumPollLoads > 4 {
			if site == 3 {
				return 4
			}
			if site == 4 {
				return 3
			}
		}
		return site
	}
	for _, tc := range cases {
		clear(trap)
		clear(count)
		clear(results)
		binary.LittleEndian.PutUint64(args, tc.n)
		patched := delayedPairPollCode(t, original, physicalSite(tc.site), count, tc.visit)
		mapping, entry, e := runtime.MapCode(patched)
		if e != nil {
			t.Fatal(e)
		}
		e = eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results)
		runtime.Unmap(mapping)
		var te *runtime.TrapError
		if !errors.As(e, &te) || te.Code != runtime.TrapInterrupted {
			t.Fatalf("site=%d visit=%d n=%d: %v", tc.site, tc.visit, tc.n, e)
		}
		if got := binary.LittleEndian.Uint32(count); got != uint32(tc.visit) {
			t.Fatalf("visit=%d want=%d", got, tc.visit)
		}
	}
	// Non-activating injection counts every dynamic group poll, independently
	// of code-site counts, across both partial first periods and scalar tails.
	for _, n := range []uint64{5, 6, 7, 8, 63, 64, 65, 66, 67, 68, 129, 130, 131, 132, 257} {
		clear(trap)
		clear(count)
		binary.LittleEndian.PutUint64(args, n)
		patched := delayedPairPollCode(t, original, physicalSite(3), count, 10000)
		mapping, entry, e := runtime.MapCode(patched)
		if e != nil {
			t.Fatal(e)
		}
		e = eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results)
		runtime.Unmap(mapping)
		if e != nil {
			t.Fatalf("n=%d: %v", n, e)
		}
		groups := (n - 1) / 4
		period := uint64(linearSumPollLoads / 4)
		want := (groups + period - 1) / period
		if got := binary.LittleEndian.Uint32(count); uint64(got) != want {
			t.Fatalf("n=%d group polls=%d want=%d", n, got, want)
		}
		if binary.LittleEndian.Uint64(results) != 0 {
			t.Fatalf("n=%d corrupted sum", n)
		}
	}
	// Successful call after every interrupted frame has unwound.
	clear(trap)
	binary.LittleEndian.PutUint64(args, 9)
	mapping, entry, err := runtime.MapCode(original)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Unmap(mapping)
	if err = eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint64(results) != 0 {
		t.Fatal("post-interruption state corrupted")
	}
}
