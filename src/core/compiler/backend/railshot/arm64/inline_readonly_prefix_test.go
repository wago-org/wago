//go:build arm64

package arm64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestInlineReadonlyCallerBlockPreservesUnreachableCallee(t *testing.T) {
	caller := []byte{0, 0x20, 0, 0x02, 0x7f, 0x20, 0, 0x10, 1, 0x6a, 0x0b, 0x6a, 0x0b}
	helper := []byte{0, 0x03, 0x40, 0x0b, 0x00, 0x0b}
	m := modFuncs(t,
		funcDef{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, caller},
		funcDef{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}, helper})
	for _, enabled := range []bool{false, true} {
		_, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"inline-integer-tuples": enabled}}, 7)
		var trap *rt.TrapError
		if !errors.As(err, &trap) || trap.Code != rt.TrapUnreachable {
			t.Fatalf("enabled=%v valid module should trap unreachable, got %v", enabled, err)
		}
	}
}

func TestInlineReadonlyPrefixAcrossCallerBlockAndAliasedStores(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, block, constant, add, mul := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x6c)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, block, constant, add, mul = wasm.I64, 0x7e, 0x42, 0x7c, 0x7e
			fold = func(x uint64) uint64 { return x }
		}
		// The scalar caller block retains a prefix below the tuple splice. Both
		// result assignments then overwrite locals read by that retained prefix.
		caller := []byte{0, 0x20, 0, 0x02, block, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1,
			0x21, 1, 0x21, 0, 0x20, 0, constant, 3, mul, 0x20, 1, constant, 5, mul, add, 0x0b, add, 0x0b}
		helper := []byte{0, 0x03, 0x40, 0x0b, 0x20, 2, 0x04, 0x40,
			0x20, 0, constant, 1, add, 0x21, 0, 0x0b, 0x20, 0, 0x20, 1, 0x0b}
		params := []wasm.ValType{typ, typ, wasm.I32}
		m := modFuncs(t, funcDef{params, []wasm.ValType{typ}, caller}, funcDef{params, []wasm.ValType{typ, typ}, helper})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, input := range [][2]uint64{{7, 13}, {0xdeadbeef80000000, 0xcafebabeffffffff}, {^uint64(0), 0x8000000000000001}} {
					for _, flag := range []uint64{0, 1, 0xdeadbeef00000000, ^uint64(0)} {
						first := fold(input[0])
						if uint32(flag) != 0 {
							first = fold(first + 1)
						}
						want := fold(fold(input[0]) + 3*first + 5*fold(input[1]))
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline-integer-tuples": enabled}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, input[0], input[1], flag)
						if err != nil || fold(got) != want {
							t.Fatalf("wide=%v enabled=%v workers=%d input=%x flag=%x got=%x want=%x err=%v", wide, enabled, workers, input, flag, got, want, err)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tuple-readonly-prefix"] != 0) != (enabled && !profileEnabled) {
							t.Fatalf("wide=%v enabled=%v stats=%v", wide, enabled, stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}

func TestInlineReadonlyPrefixKeepsArenaNodesAndOrder(t *testing.T) {
	f := gpClassEmitter()
	f.inlineDepth, f.localBase, f.nLocals = 1, 2, 2
	f.locals = []localDef{{reg: X19, state: lsReg}, {reg: X20, state: lsReg}}
	f.pinnedLocalMask = maskOf(X19, X20)
	first := f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: X19})
	second := f.pushValue(storage{kind: stConst, typ: mtI64, cval: 7})
	prefix := f.detachInlineReadonlyPrefix()
	if prefix.count != 2 || f.depth() != 0 {
		t.Fatal("readonly prefix was not detached")
	}
	// Inner control rebuilds its stack model and may grow the arena. Neither
	// operation may recycle a node retained by the caller's hidden prefix.
	for range 4096 {
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 13})
	}
	f.setDepthTypes([]machineType{mtI32, mtI64})
	results := append([]*elem(nil), f.rootsBottomToTop()...)
	f.restoreInlineReadonlyPrefix(prefix)
	roots := f.rootsBottomToTop()
	if len(roots) != 4 || roots[0] != first || roots[1] != second || roots[2] != results[0] || roots[3] != results[1] {
		t.Fatal("restoration lost caller nodes or reordered results")
	}
	if first.st.kind != stLocalReg || first.st.reg != X19 || second.st.cval != 7 {
		t.Fatal("inner stack allocation overwrote the retained prefix")
	}
}

func TestInlineReadonlyPrefixFailsClosed(t *testing.T) {
	for _, variant := range []string{"owned", "slot", "float", "gc", "eh", "private", "unpinned", "spilled", "lazy", "nested", "polled", "regional", "selection", "streaming", "large", "deferred"} {
		f := gpClassEmitter()
		f.inlineDepth, f.localBase, f.nLocals = 1, 1, 1
		f.locals = []localDef{{reg: X19, state: lsReg}}
		f.pinnedLocalMask = maskOf(X19)
		value := f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: X19})
		switch variant {
		case "owned":
			value.st.kind = stReg
		case "slot":
			value.st.kind = stSlot
		case "float":
			value.st.typ = mtF32
		case "gc":
			value.st.setGCRoot(true)
		case "eh":
			value.st.setEHRoot(true)
		case "private":
			f.localBase = 0
		case "unpinned":
			f.pinnedLocalMask = 0
		case "spilled":
			f.locals[0].state = lsMem
		case "lazy":
			f.locals[0].state = lsConstZero
		case "nested":
			f.inlineDepth = 2
		case "polled":
			f.interruptible = true
		case "regional":
			f.intervalControl = true
		case "selection":
			f.selectGroupEnd = 1
		case "streaming":
			f.streamingReduction = true
		case "large":
			for range 4 {
				f.pushValue(storage{kind: stConst, typ: mtI32})
			}
		case "deferred":
			value.setElemKind(ekDeferred)
		}
		first, last := f.s.head.next, f.s.head.prev
		if prefix := f.detachInlineReadonlyPrefix(); prefix.count != 0 || f.s.head.next != first || f.s.head.prev != last {
			t.Fatalf("variant=%s admitted or modified an unsupported prefix", variant)
		}
	}
}
