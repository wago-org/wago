//go:build amd64

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"reflect"
	"testing"
	"unsafe"
)

func TestParallelHintOwnerRetainsFullWidthIndexes(t *testing.T) {
	owner := parallelHintOwner{worker: ^uint32(0), event: ^uint32(0)}
	if unsafe.Sizeof(owner) != 8 || owner.worker != ^uint32(0) || owner.event != ^uint32(0) {
		t.Fatalf("owner representation: size=%d value=%+v", unsafe.Sizeof(owner), owner)
	}
}

func TestParallelLocalScratchGrowsPastReserve(t *testing.T) {
	params := make([]wasm.ValType, 103)
	for i := range params {
		params[i] = wasm.I64
	}
	defs := make([]funcDef, 8)
	for i := range defs {
		defs[i] = funcDef{params: params, results: []wasm.ValType{wasm.I64}, body: []byte{0, 0x20, 102, 0x0b}}
	}
	m := modFuncs(t, defs...)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	serial, err := CompileModuleWith(m, CompileOptions{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, workers := range []int{2, 4, 8} {
		parallel, err := CompileModuleWith(m, CompileOptions{Workers: workers})
		if err != nil {
			t.Fatalf("workers %d: %v", workers, err)
		}
		if !bytes.Equal(parallel.Code, serial.Code) || !reflect.DeepEqual(parallel.Entry, serial.Entry) || !reflect.DeepEqual(parallel.InternalEntry, serial.InternalEntry) || !reflect.DeepEqual(parallel.DirectPrepared, serial.DirectPrepared) {
			t.Fatalf("workers %d: large-local code or entry metadata differs", workers)
		}
	}
}

func TestParallelHintScratchExclusiveAndGrowable(t *testing.T) {
	states := newParallelHintWorkers(&wasm.Module{}, 32, 2)
	for i := range states {
		state := &states[i]
		state.elig.reset()
		frame := state.elig.push()
		state.elig.add(frame, uint32(i))
		state.globals.Add(uint32(i), int64(i+1))
	}
	if got := states[0].elig.globalsIn(0); len(got) != 1 || got[0] != 0 {
		t.Fatalf("first worker: %v", got)
	}
	if got := states[1].elig.globalsIn(0); len(got) != 1 || got[0] != 1 {
		t.Fatalf("second worker: %v", got)
	}
	if cap(states[0].elig.marks) != 32 {
		t.Fatal("dense marks are not capacity-bounded")
	}
	// Grow past both inline capacities without changing another worker.
	for n := 0; n < 9; n++ {
		frame := states[0].elig.push()
		for g := uint32(0); g < 16; g++ {
			states[0].elig.add(frame, g)
		}
	}
	if len(states[0].elig.frames) != 10 || len(states[0].elig.globalsIn(9)) != 16 {
		t.Fatal("scope scratch did not grow")
	}
	if got := states[1].elig.globalsIn(0); len(got) != 1 || got[0] != 1 {
		t.Fatalf("growth changed other worker: %v", got)
	}
	if got := states[1].globals.AppendTo(nil); len(got) != 1 || got[0].Score != 2 {
		t.Fatalf("dense storage alias: %+v", got)
	}
	if allocs := testing.AllocsPerRun(100, func() {
		states[0].elig.reset()
		frame := states[0].elig.push()
		states[0].elig.add(frame, 3)
		states[0].globals.Reset(32)
		states[0].globals.Add(3, 1)
	}); allocs != 0 {
		t.Fatalf("reused scope allocations: %g", allocs)
	}
}

func TestParallelHintRetainedScratchIsExclusiveAndGrowable(t *testing.T) {
	states := newParallelHintWorkers(&wasm.Module{}, 32, 2)
	for i := range states {
		if len(states[i].retained) != 0 || cap(states[i].retained) != 8 {
			t.Fatalf("worker %d retained scratch: len=%d cap=%d, want 0/8", i, len(states[i].retained), cap(states[i].retained))
		}
		states[i].globals.Add(uint32(i), int64(i+1))
		states[i].retained = states[i].globals.AppendTo(states[i].retained)
	}
	states[0].retained[0].Score = 99
	if states[1].retained[0].Score != 2 {
		t.Fatal("inline retained buffers alias")
	}
	states[0].globals.Reset(32)
	for i := uint32(0); i < 32; i++ {
		states[0].globals.Add(i, 1)
	}
	states[0].retained = states[0].globals.AppendTo(states[0].retained)
	if len(states[0].retained) != 33 || states[0].retained[0].Score != 99 {
		t.Fatal("retained growth lost prior records")
	}
	if len(states[1].retained) != 1 || states[1].retained[0].Score != 2 {
		t.Fatal("retained growth changed another worker")
	}
	if got := testing.AllocsPerRun(100, func() {
		state := &states[1]
		state.retained = state.retained[:0]
		state.globals.Reset(32)
		for i := uint32(0); i < 8; i++ {
			state.globals.Add(i, 1)
		}
		state.retained = state.globals.AppendTo(state.retained)
	}); got != 0 {
		t.Fatalf("small retained span allocations = %g, want 0", got)
	}
}

func TestParallelLocalScratchCapacityIsBounded(t *testing.T) {
	for _, n := range []int{0, 1, 32, 64, 65, 103, 65535} {
		hints := []funcHints{{localCount: uint16(n)}}
		got := parallelLocalScratchCapacity(hints, inlineTargetTable{}, []bool{false})
		if want := min(n, 64); got != want {
			t.Fatalf("locals %d: reserve %d, want %d", n, got, want)
		}
		sc := &scratch{}
		sc.reserveLocalScratch(got)
		if cap(sc.fnState.localType) != got || cap(sc.fnState.localSlot) != got || cap(sc.fnState.locals) != got {
			t.Fatalf("locals %d: inconsistent scratch capacities", n)
		}
	}
}
