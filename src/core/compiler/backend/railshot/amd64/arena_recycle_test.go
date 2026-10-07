//go:build linux && amd64

package amd64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRecycleEmptyOperandArenaPreservesLiveOwners(t *testing.T) {
	for _, kind := range []string{"control-prefix", "integer-register", "float-register"} {
		t.Run(kind, func(t *testing.T) {
			sc := newScratchWithStackCap(defaultStackArenaCap)
			f := &sc.fnState
			f.sc, f.s = sc, sc.stack
			f.ctrl = []ctrlFrame{{}}
			node := f.s.alloc()
			switch kind {
			case "control-prefix":
				f.ctrl[0].height = 1
				f.nonzeroCtrlHeights = 1
			case "integer-register":
				f.regUser[0] = node
			case "float-register":
				f.fregUser[0] = node
			}
			f.recycleEmptyOperandArena()
			if len(f.s.chunks[0]) != 2 {
				t.Fatal("rewound arena with a live owner")
			}
		})
	}
}

func TestRecycleEmptyOperandArenaClearsTransientHandles(t *testing.T) {
	sc := newScratchWithStackCap(defaultStackArenaCap)
	f := &sc.fnState
	f.sc, f.s = sc, sc.stack
	f.ctrl = []ctrlFrame{{}, {}, {}}
	node := f.s.alloc()
	sc.transient.tmpRoots = []*elem{node}
	sc.transient.tmpBelow = []*elem{node}
	sc.transient.tmpDeferred = []deferredArg{{root: node}}
	f.transient = sc.transient
	sc.transient.tmpRoots, sc.transient.tmpBelow, sc.transient.tmpDeferred = nil, nil, nil
	// Force backing growth after transferring ownership, as compilation does.
	f.tmpRoots = append(f.tmpRoots, node)
	f.tmpBelow = append(f.tmpBelow, node)
	f.tmpDeferred = append(f.tmpDeferred, deferredArg{root: node})
	f.tmpRootsWritten, f.tmpBelowWritten, f.tmpDeferredWritten = 2, 2, 2
	f.recycleEmptyOperandArena()
	if len(f.s.chunks[0]) != 1 || f.s.head.next != f.s.head || f.s.head.prev != f.s.head {
		t.Fatal("did not restore empty sentinel")
	}
	for i := range f.tmpRoots {
		if f.tmpRoots[i] != nil || f.tmpBelow[i] != nil || f.tmpDeferred[i].root != nil {
			t.Fatal("retained current function node handles after backing growth")
		}
	}
}

func TestNestedStatementArenaBoundedByLiveDemand(t *testing.T) {
	if profileEnabled {
		t.Skip("source profiling preserves node identities")
	}
	requireCompilerDiagnostics(t)
	const statements = 10000
	body := []byte{0, 0x02, 0x40, 0x03, 0x40} // no locals; block; loop
	for range statements {
		body = append(body, 0x41, 0, 0x01, 0x1a)
	}
	body = append(body, 0x0b, 0x0b, 0x0b)
	m := benchDecodeValidateModule(t, benchModuleBytes([]benchFuncDef{{body: body}}, false))
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	assertArenaRecycled(t, &stats)
	want := uint64(defaultStackArenaCap) * uint64(unsafe.Sizeof(elem{}))
	if stats.Compile.NodeScratchPeak > want {
		t.Fatalf("node peak %d exceeds live-demand bound %d", stats.Compile.NodeScratchPeak, want)
	}
}

// The nop prevents adjacent literal/drop folding from bypassing node allocation.
func recycleStatements(body []byte) []byte {
	for range 600 {
		body = append(body, 0x41, 0, 0x01, 0x1a)
	}
	return body
}

func TestRecycledArenaNestedBranchExecution(t *testing.T) {
	body := []byte{0, 0x02, 0x7f, 0x20, 0, 0x04, 0x7f} // block(result i32); if(result i32)
	body = recycleStatements(body)
	body = append(body, 0x41, 17, 0x0c, 1, 0x05) // 17; br outer; else
	body = recycleStatements(body)
	body = append(body, 0x41, 23, 0x0b, 0x0b, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	assertModuleArenaRecycled(t, m)
	for _, tc := range []struct{ arg, want uint64 }{{0, 23}, {1, 17}} {
		if got := runAmd64u(t, m, tc.arg); got != tc.want {
			t.Fatalf("arg %d: got %d, want %d", tc.arg, got, tc.want)
		}
	}
}

func TestRecycledArenaBlockParametersAndResults(t *testing.T) {
	// Type0 is (i32)->i32, also the block signature. Consume its parameter
	// into a local, reclaim while its height is zero, then return that local.
	body := []byte{1, 1, 0x7f, 0x20, 0, 0x02, 0, 0x21, 1}
	body = recycleStatements(body)
	body = append(body, 0x20, 1, 0x0b, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	assertModuleArenaRecycled(t, m)
	if got := runAmd64u(t, m, 42); got != 42 {
		t.Fatalf("got %d, want42", got)
	}
}

func assertArenaRecycled(t *testing.T, stats *ModuleStats) {
	t.Helper()
	if diagnosticsEnabled && !profileEnabled && stats.Funcs[0].Peephole["operand-arena-recycle"] == 0 {
		t.Fatal("production driver did not recycle operand arena")
	}
}

func assertModuleArenaRecycled(t *testing.T, m *wasm.Module) {
	t.Helper()
	if !diagnosticsEnabled || profileEnabled {
		return
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	assertArenaRecycled(t, &stats)
}

// A wide prior use leaves handles beyond the current length. Clean that written
// prefix once, then retain the capacity without visiting it at each checkpoint.
func TestRecycleArenaClearsWrittenPrefixOnce(t *testing.T) {
	sc := newScratchWithStackCap(defaultStackArenaCap)
	f := &sc.fnState
	f.sc, f.s = sc, sc.stack
	node := f.s.alloc()
	f.tmpRoots = make([]*elem, 1, 4096)
	f.tmpBelow = make([]*elem, 1, 4096)
	f.tmpDeferred = make([]deferredArg, 0, 4096)
	f.tmpRoots[:4096][4095] = node
	f.tmpBelow[:4096][4095] = node
	f.tmpDeferred[:4096][4095].root = node
	f.tmpRootsWritten, f.tmpBelowWritten, f.tmpDeferredWritten = 4096, 4096, 4096
	f.recycleEmptyOperandArena()
	if f.tmpRoots[:4096][4095] != nil || f.tmpBelow[:4096][4095] != nil || f.tmpDeferred[:4096][4095].root != nil {
		t.Fatal("retained stale handles beyond current length")
	}
	if f.tmpRootsWritten != 0 || f.tmpBelowWritten != 0 || f.tmpDeferredWritten != 0 {
		t.Fatal("cleanup did not reset written prefixes")
	}
	// A later shallow use dirties only one slot, despite retained wide buffers.
	node = f.s.alloc()
	f.tmpRoots[0], f.tmpBelow[0] = node, node
	f.tmpDeferred[:1][0].root = node
	f.tmpRootsWritten, f.tmpBelowWritten, f.tmpDeferredWritten = 1, 1, 1
	f.recycleEmptyOperandArena()
	if f.tmpRoots[0] != nil || f.tmpBelow[0] != nil || f.tmpDeferred[:1][0].root != nil {
		t.Fatal("retained shallow-use handles")
	}
}

func TestRecyclerControlEligibilityTracksPushAndEnd(t *testing.T) {
	sc := newScratchWithStackCap(defaultStackArenaCap)
	f := &sc.fnState
	f.sc, f.s, f.a = sc, sc.stack, sc.asm
	f.ctrl = []ctrlFrame{{kind: cfFunc}}
	f.unreachable = true // end the synthetic frames without emitting operands
	for _, height := range []int{2, 0, 1} {
		f.pushCtrl(&ctrlFrame{kind: cfBlock, height: height})
	}
	if f.nonzeroCtrlHeights != 2 {
		t.Fatalf("got %d prefix owners, want 2", f.nonzeroCtrlHeights)
	}
	for _, want := range []int{1, 1, 0} {
		if err := f.opEnd(); err != nil {
			t.Fatal(err)
		}
		if f.nonzeroCtrlHeights != want {
			t.Fatalf("got %d prefix owners after end, want %d", f.nonzeroCtrlHeights, want)
		}
	}
}
