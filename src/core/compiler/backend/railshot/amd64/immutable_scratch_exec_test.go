//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/amd64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

const immutableScratchA uint64 = 0x123456789abcdef
const immutableScratchB uint64 = 0x23456789abcdef1

// The seed and declared i64 locals remain unchanged through every iteration.
// Their pins occupy the earlier cache candidates: the memory case leaves R11
// and RDI/RSI, while the table case leaves R9/R10/R11 before scratch exclusions.
func immutableScratchInlineModule(t *testing.T, table bool) (*wasm.Module, int) {
	t.Helper()
	immutableLocals := 2
	params := []wasm.ValType{wasm.I64, wasm.I32}
	if table {
		immutableLocals = 3
		// Eight incoming GP args keep R9/R10/R11 out of the local-pin pool.
		// R12-R15 are occupied, so the first cache candidate would be R9
		// without the table/original-call scratch exclusion.
		params = append(params, wasm.I64, wasm.I64, wasm.I64, wasm.I64, wasm.I64, wasm.I64)
	}
	accumulator := byte(len(params))
	body := []byte{1, byte(immutableLocals + 1), 0x7e} // accumulator + immutable locals
	for i := 0; i < immutableLocals; i++ {
		body = append(body, 0x20, 0, 0x42, byte(i+1), 0x7c, 0x21, accumulator+byte(i+1))
	}
	body = append(body, 0x03, 0x40) // loop
	addTerms := func(constant uint64) {
		for i := 0; i < immutableLocals; i++ {
			body = append(body, 0x20, accumulator, 0x20, accumulator+byte(i+1), 0x42)
			body = append(body, wasmtest.SLEB64(int64(constant))...)
			body = append(body, 0x7e, 0x7c, 0x21, accumulator)
		}
	}
	addTerms(immutableScratchA)
	body = append(body, 0x10, 1) // the sole call, inlined inside the loop
	addTerms(immutableScratchB)
	body = append(body, 0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0, 0x0b, 0x20, accumulator)
	for i := 0; i < immutableLocals; i++ {
		body = append(body, 0x20, accumulator+byte(i+1), 0x7c)
	}
	body = append(body, 0x0b)
	helper := []byte{0x41, 32, 0x41, 0, 0x41, 16, 0xfc, 10, 0, 0, 0x0b}
	storage := wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})) // one memory page
	if table {
		helper = []byte{0x41, 4, 0x41, 0, 0x41, 2, 0xfc, 14, 0, 0, 0x0b}
		storage = wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 8}))
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(params, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		storage,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(body))), body...), wasmtest.Code(helper),
		)),
	)
	m, err := wasm.DecodeModule(module)
	if err != nil {
		t.Fatal(err)
	}
	return m, immutableLocals
}

func compileImmutableScratchInline(t *testing.T, m *wasm.Module, table bool) *enc.CompiledModule {
	t.Helper()
	policy := currentCodegenPolicy()
	hints, sidecar, _, err := computeModuleHints(m, 0, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	h := sidecar.viewAt(hints[0], 0)
	if !h.flags.has(hintHasCall) || !h.flags.has(hintHasLoop) || h.flags.has(hintUsesBulkMem|hintMutatesTable) || h.loopIntConsts == nil || h.loopIntConsts.count != 2 {
		t.Fatalf("caller hints do not exercise original-call cache admission: flags=%x constants=%v", h.flags, h.loopIntConsts)
	}
	targets := buildInlineTargets(m, hints, policy)
	sc := newScratch()
	sc.policy = policy
	sc.amd64Features = shared.AMD64ModernBaseline
	var functionStats CodegenStats
	code, relocs, _, err := compileFunc(m, nil, 0, true, false, true, false, false,
		nil, &h, computeImmutableTableHints(m, hints, policy), nil, false, coreruntime.MaxHostArity,
		false, false, false, nil, nil, &functionStats, targets, sc)
	if err != nil {
		t.Fatal(err)
	}
	f := &sc.fnState
	if f.scalarSummary.Eligible || f.makesCalls || len(relocs) != 0 {
		t.Fatalf("caller must use established fallback with all calls inlined: shared=%t calls=%t relocs=%d", f.scalarSummary.Eligible, f.makesCalls, len(relocs))
	}
	busy := maskOf(R12, R13, R14, R9, R10)
	wantCaches := []intConstReg{{bits: int64(immutableScratchA), reg: R11}}
	if table {
		busy = maskOf(R12, R13, R14, R15)
		wantCaches = []intConstReg{{bits: int64(immutableScratchA), reg: R10}, {bits: int64(immutableScratchB), reg: R11}}
	}
	if f.pinnedLocalMask&busy != busy || f.pinnedLocalMask.has(R11) || f.pinnedLocalMask.has(RDI) || f.pinnedLocalMask.has(RSI) {
		t.Fatalf("fixture lost register pressure: pinned=%x want busy=%x", f.pinnedLocalMask, busy)
	}
	if table && (f.pinnedLocalMask.has(R9) || f.pinnedLocalMask.has(R10)) {
		t.Fatal("incoming-argument fixture unexpectedly pinned table/cache scratch")
	}
	if int(f.iconstN) != len(wantCaches) {
		t.Fatalf("immutable cache admission = %v, want %v", f.iconsts[:f.iconstN], wantCaches)
	}
	for i, want := range wantCaches {
		if f.iconsts[i] != want {
			t.Fatalf("immutable cache %d = %v, want %v", i, f.iconsts[i], want)
		}
	}
	if diagnosticsEnabled && (functionStats.Calls["inline"] != 1 || functionStats.Peephole["all-calls-inlined"] != 1 || functionStats.Peephole["wide-loop-int-const"] != len(wantCaches)) {
		t.Fatalf("missing inline/cache diagnostic evidence: calls=%v decisions=%v", functionStats.Calls, functionStats.Peephole)
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: optionalTestStats(&stats), AMD64Features: shared.AMD64ModernBaseline, AMD64FeaturesSet: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
	})
	// Tie the inspected production function state to the full module that runs.
	if cm.Entry[0]+len(code) > len(cm.Code) || !bytes.Equal(code, cm.Code[cm.Entry[0]:cm.Entry[0]+len(code)]) {
		t.Fatal("inspected caller differs from the executed module")
	}
	if diagnosticsEnabled && (stats.Funcs[0].SharedScalar || stats.Funcs[0].Calls["inline"] != 1 || stats.Funcs[0].PinnedLocals != functionStats.PinnedLocals) {
		t.Fatal("module compilation did not preserve inspected fallback/inlining/pins")
	}
	return cm
}

func TestImmutableIntegerCacheAcrossInlinedBulkLoop(t *testing.T) {
	savedWide, savedInline, savedCallFree := wideLoopIntConstEnabled, inlineEnabled, inlineCallFreeHintsEnabled
	wideLoopIntConstEnabled, inlineEnabled, inlineCallFreeHintsEnabled = true, true, true
	t.Cleanup(func() {
		wideLoopIntConstEnabled, inlineEnabled, inlineCallFreeHintsEnabled = savedWide, savedInline, savedCallFree
	})
	for _, table := range []bool{false, true} {
		name := "memory.copy"
		if table {
			name = "table.copy"
		}
		t.Run(name, func(t *testing.T) {
			m, locals := immutableScratchInlineModule(t, table)
			cm := compileImmutableScratchInline(t, m, table)
			for _, seed := range []uint64{3, 1 << 63, ^uint64(0)} {
				for _, iterations := range []uint64{1, 2, 7} {
					got := executeImmutableScratchInline(t, m, cm, table, seed, iterations)
					sum := uint64(locals)*seed + uint64(locals*(locals+1)/2)
					want := iterations*sum*(immutableScratchA+immutableScratchB) + sum
					if got != want {
						t.Fatalf("seed=%x iterations=%d got=%x want=%x", seed, iterations, got, want)
					}
				}
			}
		})
	}
}

func executeImmutableScratchInline(t *testing.T, m *wasm.Module, cm *enc.CompiledModule, table bool, seed, iterations uint64) uint64 {
	t.Helper()
	engine, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	jm, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	image, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(image)
	ft, _ := m.LocalFuncType(0)
	args, result, trap := arena.Alloc(8*len(ft.Params)), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
	binary.LittleEndian.PutUint64(args, seed)
	binary.LittleEndian.PutUint64(args[8:], iterations)
	storage := jm.LinearMemory()[:64]
	for i := range storage {
		storage[i] = byte(i + 1)
	}
	want := append([]byte(nil), storage...)
	copy(want[32:48], want[:16])
	if table {
		storage = arena.Alloc(8 + 8*coreruntime.TableEntryBytes)
		binary.LittleEndian.PutUint32(storage, 8)
		binary.LittleEndian.PutUint32(storage[4:], 8)
		descriptors := arena.Alloc(2 * coreruntime.FuncRefDescBytes)
		context := uint64(uintptr(unsafe.Pointer(&descriptors[0])))
		canonical := descriptors[coreruntime.FuncRefDescBytes:]
		binary.LittleEndian.PutUint64(canonical[coreruntime.TableEntryCodePtrOffset:], uint64(entry)+uint64(cm.Entry[1]))
		binary.LittleEndian.PutUint64(canonical[coreruntime.TableEntrySigKeyOffset:], m.StructuralTypeKey(1))
		binary.LittleEndian.PutUint64(canonical[coreruntime.TableEntryHomeLinMemOffset:], uint64(jm.LinMemBase()))
		binary.LittleEndian.PutUint64(canonical[coreruntime.TableEntryRefSlotOffset:], uint64(uintptr(unsafe.Pointer(&canonical[0]))))
		binary.LittleEndian.PutUint64(canonical[coreruntime.FuncRefContextOffset:], context)
		copy(storage[8:], canonical[:coreruntime.TableEntryBytes]) // helper reference followed by null
		jm.SetFuncRefDesc(uintptr(unsafe.Pointer(&descriptors[0])))
		jm.SetTablePtr(uintptr(unsafe.Pointer(&storage[0])))
		want = append([]byte(nil), storage...)
		copy(want[8+4*coreruntime.TableEntryBytes:], want[8:8+2*coreruntime.TableEntryBytes])
	}
	if err := engine.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, result); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storage, want) {
		t.Fatalf("bulk contents = %x, want %x", storage, want)
	}
	return binary.LittleEndian.Uint64(result)
}
