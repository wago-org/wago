//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/tests/wasmtest"
)

func exactGCRefFactModule(t testing.TB, controlBoundary bool) *wasm.Module {
	t.Helper()
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0xfb, 0x01, 0x00, // struct.new_default 0
		0x21, 0x00, // local.set 0
	}
	if controlBoundary {
		body = append(body, 0x02, 0x40, 0x0b) // empty block conservatively clears facts
	}
	body = append(body,
		0x20, 0x00,
		0xfb, 0x16, 0x00, // ref.cast (ref 0)
		0xd1, // ref.is_null => 0
		0x0b,
	)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("decode exact-ref-fact module: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate exact-ref-fact module: %v", err)
	}
	return m
}

func enableGCRefFacts(t *testing.T) {
	t.Helper()
	before := exactGCRefFactsEnabled
	exactGCRefFactsEnabled = true
	t.Cleanup(func() { exactGCRefFactsEnabled = before })
}

func TestFinalGCParameterFactsResolveRecursiveGroupIndex(t *testing.T) {
	enableGCRefFacts(t)
	recParam := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0, Rec: true}), false))
	m := &wasm.Module{Types: []wasm.RecType{
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}},
		{SubTypes: []wasm.SubType{
			{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct}},
			{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{recParam}}},
		}},
	}}
	f := fn{m: m, localGCRefFacts: make([]codegen.GCRefFact, 1)}
	f.seedFinalGCParameterTypes([]wasm.ValType{recParam}, 1, 2)
	if got, ok := f.localGCRefFacts[0].ExactType(); !ok || got != 1 {
		t.Fatalf("recursive final parameter fact = %d,%v, want flattened type 1", got, ok)
	}
}

func TestNullableFinalGCParameterRetainsNonNullCast(t *testing.T) {
	enableGCRefFacts(t)
	compile := func(nullableCast bool) *CodegenStats {
		t.Helper()
		cast := byte(0x16) // ref.cast (ref 0)
		if nullableCast {
			cast = 0x17 // ref.cast (ref null 0)
		}
		body := []byte{
			0x00,       // no locals
			0x20, 0x00, // local.get 0
			0xfb, cast, 0x00, // ref.cast 0
			0xd1, // ref.is_null
			0x0b,
		}
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x00},
				[]byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x7f}, // (ref null 0) -> i32
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		)
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatalf("decode nullable final parameter module: %v", err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatalf("validate nullable final parameter module: %v", err)
		}
		var stats ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, Stats: &stats}); err != nil {
			t.Fatalf("compile nullable final parameter module: %v", err)
		}
		return stats.Funcs[0]
	}

	nonNull := compile(false)
	if got := nonNull.Peephole["gc-ref-cast-elide"]; got != 0 {
		t.Fatalf("nullable parameter elided non-null cast %d times", got)
	}
	if got := nonNull.Calls["gcnative"]; got != 1 {
		t.Fatalf("nullable parameter non-null cast calls = %d, want 1", got)
	}

	nullable := compile(true)
	if got := nullable.Peephole["gc-ref-cast-elide"]; got != 1 {
		t.Fatalf("nullable parameter nullable cast elisions = %d, want 1", got)
	}
	if got := nullable.Calls["gcnative"]; got != 0 {
		t.Fatalf("nullable parameter nullable cast calls = %d, want 0", got)
	}
}

func TestGCHeapClassMatchTruthTable(t *testing.T) {
	enableGCRefFacts(t)
	targets := []wasm.AbsHeapType{wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapFunc, wasm.HeapExtern}
	for _, tc := range []struct {
		name   string
		source codegen.GCHeapClass
		want   string // 1=true, 0=false, ?=unknown in target order above
	}{
		{name: "unknown", source: codegen.GCHeapUnknown, want: "???????"},
		{name: "any-upper-bound", source: codegen.GCHeapAny, want: "1????00"},
		{name: "eq-upper-bound", source: codegen.GCHeapEq, want: "11???00"},
		{name: "i31-exact-family", source: codegen.GCHeapI31, want: "1110000"},
		{name: "struct-exact-family", source: codegen.GCHeapStruct, want: "1101000"},
		{name: "array-exact-family", source: codegen.GCHeapArray, want: "1100100"},
		{name: "func-exact-family", source: codegen.GCHeapFunc, want: "0000010"},
		{name: "extern-exact-family", source: codegen.GCHeapExtern, want: "0000001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i, target := range targets {
				match, known := gcHeapClassMatches(tc.source, target)
				switch tc.want[i] {
				case '1':
					if !known || !match {
						t.Fatalf("target %v = %v/%v, want true/known", target, match, known)
					}
				case '0':
					if !known || match {
						t.Fatalf("target %v = %v/%v, want false/known", target, match, known)
					}
				case '?':
					if known {
						t.Fatalf("target %v = %v/%v, want unknown", target, match, known)
					}
				}
			}
			for _, target := range []wasm.AbsHeapType{wasm.HeapNone, wasm.HeapNoFunc, wasm.HeapNoExtern} {
				if match, known := gcHeapClassMatches(tc.source, target); !known || match {
					t.Fatalf("bottom target %v = %v/%v, want false/known", target, match, known)
				}
			}
		})
	}
}

func TestExactGCReferenceFactUsesCanonicalTypeEquivalence(t *testing.T) {
	enableGCRefFacts(t)
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(
		[]byte{0x5f, 0x00},
		[]byte{0x5f, 0x00},
	)))
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	f := fn{m: m}
	fact := codegen.ExactGCRefFact(1, 1, codegen.GCHeapStruct)
	if matched, known := f.gcRefFactMatchesTarget(fact, 0, false, true); !known || !matched {
		t.Fatalf("equivalent exact type match = %v/%v, want true/known", matched, known)
	}
}

func TestStructuredGCReferenceFactIntersectionAndLoopSubset(t *testing.T) {
	enableGCRefFacts(t)
	left := codegen.ExactGCRefFact(3, 11, codegen.GCHeapArray).
		WithFreshness(codegen.GCFreshUnpublished).
		WithKnownArrayLength(7)
	f := fn{localGCRefFacts: []codegen.GCRefFact{left, left}}
	joined := f.snapshotGCRefFacts()
	f.localGCRefFacts[0] = codegen.ExactGCRefFact(4, 12, codegen.GCHeapArray).WithKnownArrayLength(9)
	f.mergeGCRefFactsInto(&joined)
	if _, exact := joined[0].ExactType(); exact || joined[0].Identity() != 0 || joined[0].HeapClass() != codegen.GCHeapArray {
		t.Fatalf("contradictory join retained exact identity: %+v", joined[0])
	}
	if _, known := joined[0].KnownArrayLength(); known {
		t.Fatalf("contradictory join retained array length: %+v", joined[0])
	}
	f.installGCRefFacts([]codegen.GCRefFact{left, left})
	f.invalidateLoopModifiedGCRefFacts(map[uint32]bool{0: true})
	if !f.localGCRefFacts[0].IsZero() || f.localGCRefFacts[1].IsZero() || f.localGCRefFacts[1].Freshness() != codegen.GCPublished {
		t.Fatalf("loop subset invalidation/publication = %+v", f.localGCRefFacts)
	}
	f.freeGCRefFactBuf(joined)
}

func TestLoopHeaderClearsMutableFieldForwarding(t *testing.T) {
	enableGCRefFacts(t)
	f := fn{
		localGCRefFacts: []codegen.GCRefFact{
			codegen.ExactGCRefFact(0, 1, codegen.GCHeapStruct).WithFreshness(codegen.GCFreshUnpublished),
			{},
		},
		gcLastField: gcStructFieldFact{valid: true, fromStore: true, local: -1, resultLocal: -1, identity: 1},
	}
	f.invalidateLoopModifiedGCRefFacts(nil)
	if f.gcLastField.valid {
		t.Fatal("mutable constructor field forwarding survived loop header")
	}
	if got := f.localGCRefFacts[0].Freshness(); got != codegen.GCPublished {
		t.Fatalf("loop-invariant freshness = %v, want published", got)
	}

	f.gcLastField = gcStructFieldFact{valid: true, immutable: true, local: 0, resultLocal: 1}
	f.invalidateLoopModifiedGCRefFacts(nil)
	if !f.gcLastField.valid {
		t.Fatal("loop-invariant immutable field forwarding was discarded")
	}
	f.invalidateLoopModifiedGCRefFacts(map[uint32]bool{1: true})
	if f.gcLastField.valid {
		t.Fatal("immutable field forwarding survived result-local mutation")
	}
}

func TestDisabledGCReferenceFactsDoNotAllocateSnapshots(t *testing.T) {
	saved := exactGCRefFactsEnabled
	exactGCRefFactsEnabled = false
	defer func() { exactGCRefFactsEnabled = saved }()
	f := fn{localGCRefFacts: make([]codegen.GCRefFact, 1024)}
	if got := f.snapshotGCRefFacts(); got != nil {
		t.Fatalf("disabled snapshot = %v, want nil", got)
	}
	if allocs := testing.AllocsPerRun(100, func() {
		if got := f.snapshotGCRefFacts(); got != nil {
			panic("disabled fact snapshot")
		}
	}); allocs != 0 {
		t.Fatalf("disabled fact snapshot allocations = %v, want 0", allocs)
	}
}

func TestExactGCReferenceFactsElideProvenCast(t *testing.T) {
	enableGCRefFacts(t)
	compile := func(m *wasm.Module) *CodegenStats {
		var stats ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, Stats: &stats}); err != nil {
			t.Fatalf("compile exact-ref-fact module: %v", err)
		}
		return stats.Funcs[0]
	}

	saved := exactGCRefFactsEnabled
	defer func() { exactGCRefFactsEnabled = saved }()

	exactGCRefFactsEnabled = true
	on := compile(exactGCRefFactModule(t, false))
	if got := on.Peephole["gc-ref-cast-elide"]; got != 1 {
		t.Fatalf("gc-ref-cast-elide = %d, want 1 (all: %v)", got, on.Peephole)
	}
	if got := on.Calls["gcnative"]; got != 0 {
		t.Fatalf("native cast calls = %d, want 0", got)
	}

	exactGCRefFactsEnabled = false
	off := compile(exactGCRefFactModule(t, false))
	if got := off.Peephole["gc-ref-cast-elide"]; got != 0 {
		t.Fatalf("disabled gc-ref-cast-elide = %d, want 0", got)
	}
	if got := off.Calls["gcnative"]; got != 1 {
		t.Fatalf("disabled native cast calls = %d, want 1", got)
	}

	exactGCRefFactsEnabled = true
	boundary := compile(exactGCRefFactModule(t, true))
	if got := boundary.Peephole["gc-ref-cast-elide"]; got != 1 {
		t.Fatalf("fact did not survive identical empty-block paths: gc-ref-cast-elide = %d", got)
	}
	if got := boundary.Calls["gcnative"]; got != 0 {
		t.Fatalf("structured-merge native cast calls = %d, want 0", got)
	}
}

func TestTeeSpillElisionPreservesGCReferenceFacts(t *testing.T) {
	enableGCRefFacts(t)
	const n = 20
	body := []byte{
		0x02,             // two local declaration groups
		0x01, 0x63, 0x00, // one (ref null 0) local
		n, wasm.MustEncodeValType(wasm.I32), // twenty i32 locals
		0xfb, 0x01, 0x00, // struct.new_default 0
		0x22, 0x00, // local.tee 0; keep the exact reference live
	}
	for i := byte(1); i <= n; i++ {
		body = append(body, 0x41, i, 0x22, i) // i32.const i; local.tee i
	}
	for range n {
		body = append(body, 0x1a) // drop the integer tee results
	}
	body = append(body,
		0xfb, 0x16, 0x00, // ref.cast (ref 0)
		0xd1, // ref.is_null => 0
		0x0b,
	)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("decode GC tee pressure module: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate GC tee pressure module: %v", err)
	}

	saved := teeSpillElideEnabled
	teeSpillElideEnabled = true
	defer func() { teeSpillElideEnabled = saved }()
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, Stats: &stats}); err != nil {
		t.Fatalf("compile GC tee pressure module: %v", err)
	}
	fn := stats.Funcs[0]
	if got := fn.Peephole["tee-spill-elide"]; got == 0 {
		t.Fatal("integer tee spill elision was not active under GC reference pressure")
	}
	if got := fn.Peephole["gc-ref-cast-elide"]; got != 1 {
		t.Fatalf("GC reference fact lost across pressure: cast elisions = %d, want 1", got)
	}
}

func TestTeeSpillElisionDoesNotReuseGCReferenceHome(t *testing.T) {
	enableGCRefFacts(t)
	savedFacts, savedTee := exactGCRefFactsEnabled, teeSpillElideEnabled
	exactGCRefFactsEnabled, teeSpillElideEnabled = true, true
	defer func() { exactGCRefFactsEnabled, teeSpillElideEnabled = savedFacts, savedTee }()

	stats := &CodegenStats{}
	f := fn{a: &encoderamd64.Asm{}, s: newStack(), stats: stats}
	e := f.pushValue(storage{kind: stReg, typ: mtI64, reg: RAX, gcRoot: true})
	f.regUser[RAX] = e
	want := codegen.ExactGCRefFact(3, 11, codegen.GCHeapArray).
		WithNullability(codegen.GCKnownNonNull).
		WithKnownArrayLength(17)
	putGCRefFact(&e.st, want)

	f.spill(e)

	if e.st.kind != stSlot {
		t.Fatalf("GC reference spill storage = %v, want spill slot", e.st.kind)
	}
	if got := gcRefFact(e); got != want {
		t.Fatalf("GC reference fact after spill = %+v, want %+v", got, want)
	}
	if stats.Spills != 1 {
		t.Fatalf("GC reference spills = %d, want 1", stats.Spills)
	}
	if got := stats.Peephole["tee-spill-elide"]; got != 0 {
		t.Fatalf("GC reference used integer tee spill home %d times", got)
	}
}

func gcReferenceFactStats(t *testing.T, composite, body []byte) *CodegenStats {
	t.Helper()
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			composite,
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("decode GC fact module: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate GC fact module: %v", err)
	}
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats}); err != nil {
		t.Fatalf("compile GC fact module: %v", err)
	}
	return stats.Funcs[0]
}

func TestGCNullAndTypeTestsFoldFromStructuredFacts(t *testing.T) {
	enableGCRefFacts(t)
	saved := exactGCRefFactsEnabled
	defer func() { exactGCRefFactsEnabled = saved }()
	exactGCRefFactsEnabled = true

	nullTest := gcReferenceFactStats(t, []byte{0x5f, 0x00}, []byte{
		0x00,       // no locals
		0xd0, 0x6e, // ref.null any
		0xd1, // ref.is_null
		0x0b,
	})
	if got := nullTest.Peephole["gc-null-check-elide"]; got != 1 {
		t.Fatalf("gc-null-check-elide = %d, want 1 (all: %v)", got, nullTest.Peephole)
	}

	refTest := gcReferenceFactStats(t, []byte{0x5f, 0x00}, []byte{
		0x00,       // no locals
		0xd0, 0x6e, // ref.null any
		0xfb, 0x14, 0x6b, // ref.test (ref struct)
		0x0b,
	})
	if got := refTest.Peephole["gc-ref-test-fold"]; got != 1 {
		t.Fatalf("gc-ref-test-fold = %d, want 1 (all: %v)", got, refTest.Peephole)
	}

	exactGCRefFactsEnabled = false
	disabled := gcReferenceFactStats(t, []byte{0x5f, 0x00}, []byte{
		0x00, 0xd0, 0x6e, 0xd1, 0x0b,
	})
	if got := disabled.Peephole["gc-null-check-elide"]; got != 0 {
		t.Fatalf("disabled gc-null-check-elide = %d", got)
	}
}

func gcResolveReuseStats(t *testing.T, composite, funcType, body []byte) *CodegenStats {
	t.Helper()
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(composite, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("decode GC resolve module: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate GC resolve module: %v", err)
	}
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats}); err != nil {
		t.Fatalf("compile GC resolve module: %v", err)
	}
	return stats.Funcs[0]
}

func TestGCResolvedHandleReuseAndInvalidation(t *testing.T) {
	enableGCRefFacts(t)
	refTo0I32 := []byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f}
	refTo0TwoI32 := []byte{0x60, 0x01, 0x64, 0x00, 0x02, 0x7f, 0x7f}
	structBody := []byte{
		0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x6a, 0x0b,
	}
	stats := gcResolveReuseStats(t, []byte{0x5f, 0x01, 0x7f, 0x00}, refTo0I32, structBody)
	if stats.GCHandleResolutions != 1 || stats.GCHandleResolutionReuse != 1 {
		t.Fatalf("straight-line resolutions = %d reused = %d, want 1/1", stats.GCHandleResolutions, stats.GCHandleResolutionReuse)
	}

	arrayBody := []byte{
		0x00,
		0x20, 0x00, 0xfb, 0x0f,
		0x20, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x00,
		0x0b,
	}
	stats = gcResolveReuseStats(t, []byte{0x5e, 0x7f, 0x00}, refTo0TwoI32, arrayBody)
	if stats.GCHandleResolutions != 1 || stats.GCHandleResolutionReuse != 1 {
		t.Fatalf("array len/get resolutions = %d reused = %d, want 1/1 (calls=%v peep=%v)", stats.GCHandleResolutions, stats.GCHandleResolutionReuse, stats.Calls, stats.Peephole)
	}

	savedDead := deadGCNewEnabled
	deadGCNewEnabled = false
	defer func() { deadGCNewEnabled = savedDead }()
	allocationBody := []byte{
		0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x1a,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	stats = gcResolveReuseStats(t, []byte{0x5f, 0x01, 0x7f, 0x00}, refTo0I32, allocationBody)
	if stats.GCHandleResolutions != 2 || stats.GCHandleResolutionReuse != 0 {
		t.Fatalf("allocation-boundary resolutions = %d reused = %d, want 2/0", stats.GCHandleResolutions, stats.GCHandleResolutionReuse)
	}

	controlBody := []byte{
		0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x1a,
		0x02, 0x40, 0x0b,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	stats = gcResolveReuseStats(t, []byte{0x5f, 0x01, 0x7f, 0x00}, refTo0I32, controlBody)
	if stats.GCHandleResolutions != 2 || stats.GCHandleResolutionReuse != 0 {
		t.Fatalf("control-boundary resolutions = %d reused = %d, want 2/0", stats.GCHandleResolutions, stats.GCHandleResolutionReuse)
	}
}

func TestModuleSharedGCResolverStubReducesDenseSites(t *testing.T) {
	enableGCRefFacts(t)
	module := func(sites int) *wasm.Module {
		body := []byte{0x00}
		for range sites {
			body = append(body, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x1a)
		}
		body = append(body, 0x41, 0x00, 0x0b)
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x01, 0x7f, 0x00},
				[]byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f},
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		)
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	distinctModule := func(sites int) *wasm.Module {
		body := []byte{0x00}
		funcType := []byte{0x60, byte(sites)}
		for i := 0; i < sites; i++ {
			funcType = append(funcType, 0x64, 0x00)
			body = append(body, 0x20, byte(i), 0xfb, 0x02, 0x00, 0x00)
		}
		funcType = append(funcType, 0x01, 0x7f)
		for i := 1; i < sites; i++ {
			body = append(body, 0x6a)
		}
		body = append(body, 0x0b)
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0x01, 0x7f, 0x00}, funcType)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		)
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	compile := func(m *wasm.Module, shared, reuse bool) (int, ModuleStats) {
		savedShared, savedReuse := gcSharedStubsEnabled, gcResolveReuseEnabled
		gcSharedStubsEnabled, gcResolveReuseEnabled = shared, reuse
		defer func() { gcSharedStubsEnabled, gcResolveReuseEnabled = savedShared, savedReuse }()
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		defer cm.CodeImage.Close()
		return len(cm.Code), stats
	}

	oneOnBytes, oneOn := compile(module(1), true, false)
	oneOffBytes, _ := compile(module(1), false, false)
	if oneOn.GCSharedStubs != 0 || oneOn.GCSharedStubCallSites != 0 || oneOnBytes != oneOffBytes {
		t.Fatalf("one-site crossover emitted shared resolver: bytes=%d/%d stats=%+v", oneOnBytes, oneOffBytes, oneOn)
	}

	// One direct scalar access plus one final reference-field access has two GC
	// opcodes but only one module-resolver callsite. The conservative hint scan
	// must not cross the measured two-real-site threshold.
	mixedBody := []byte{
		0x00,
		0x20, 0x01, 0xfb, 0x02, 0x01, 0x00, 0x1a,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	mixedData := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			[]byte{0x5f, 0x01, 0x6e, 0x00},
			[]byte{0x60, 0x02, 0x64, 0x00, 0x64, 0x01, 0x01, 0x7f},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(mixedBody))), mixedBody...))),
	)
	mixed, err := wasm.DecodeModule(mixedData)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(mixed); err != nil {
		t.Fatal(err)
	}
	mixedOnBytes, mixedOn := compile(mixed, true, false)
	mixedOffBytes, _ := compile(mixed, false, false)
	if mixedOn.GCSharedStubs != 0 || mixedOn.GCSharedStubCallSites != 0 || mixedOnBytes != mixedOffBytes {
		t.Fatalf("one-real-site mixed module emitted shared resolver: bytes=%d/%d stats=%+v", mixedOnBytes, mixedOffBytes, mixedOn)
	}

	const sites = 8
	onBytes, on := compile(module(sites), true, false)
	offBytes, off := compile(module(sites), false, false)
	if on.GCSharedStubs != 1 || on.GCSharedStubCallSites != sites || on.GCSharedStubBytes == 0 {
		t.Fatalf("shared resolver stats = bodies %d calls %d bytes %d", on.GCSharedStubs, on.GCSharedStubCallSites, on.GCSharedStubBytes)
	}
	if off.GCSharedStubs != 0 || off.GCSharedStubCallSites != 0 || off.GCSharedStubBytes != 0 {
		t.Fatalf("disabled shared resolver stats = bodies %d calls %d bytes %d", off.GCSharedStubs, off.GCSharedStubCallSites, off.GCSharedStubBytes)
	}
	if onBytes >= offBytes {
		t.Fatalf("shared resolver code = %d bytes, inline = %d; want shared smaller", onBytes, offBytes)
	}

	reuseSharedBytes, reuseShared := compile(module(sites), true, true)
	reuseInlineBytes, _ := compile(module(sites), false, true)
	if reuseShared.GCSharedStubs != 0 || reuseShared.GCSharedStubCallSites != 0 || reuseSharedBytes != reuseInlineBytes {
		t.Fatalf("one-function reuse emitted code-growing shared island: bytes=%d/%d stats=%+v", reuseSharedBytes, reuseInlineBytes, reuseShared)
	}

	distinctSharedBytes, distinctShared := compile(distinctModule(sites), true, true)
	distinctInlineBytes, _ := compile(distinctModule(sites), false, true)
	if distinctShared.GCSharedStubs != 1 || distinctShared.GCSharedStubCallSites != sites || distinctSharedBytes >= distinctInlineBytes {
		t.Fatalf("distinct one-function sites did not select shared island: bytes=%d/%d stats=%+v", distinctSharedBytes, distinctInlineBytes, distinctShared)
	}

	// ModuleStats is a reusable sink; a later sparse compile must not retain the
	// prior module's island attribution.
	savedShared, savedReuse := gcSharedStubsEnabled, gcResolveReuseEnabled
	gcSharedStubsEnabled, gcResolveReuseEnabled = true, false
	defer func() { gcSharedStubsEnabled, gcResolveReuseEnabled = savedShared, savedReuse }()
	var reused ModuleStats
	dense, err := CompileModuleWith(module(sites), CompileOptions{GCStructHelpers: true, Stats: &reused})
	if err != nil {
		t.Fatal(err)
	}
	if dense.CodeImage != nil {
		_ = dense.CodeImage.Close()
	}
	if reused.GCSharedStubs != 1 {
		t.Fatalf("dense reusable stats = %+v", reused)
	}
	sparse, err := CompileModuleWith(module(1), CompileOptions{GCStructHelpers: true, Stats: &reused})
	if err != nil {
		t.Fatal(err)
	}
	if sparse.CodeImage != nil {
		_ = sparse.CodeImage.Close()
	}
	if reused.GCSharedStubBytes != 0 || reused.GCSharedStubs != 0 || reused.GCSharedStubCallSites != 0 {
		t.Fatalf("reused sparse stats retained shared island = %+v", reused)
	}
}

func TestGCImmutableLoadCacheSurvivesUnrelatedMutableEffects(t *testing.T) {
	enableGCRefFacts(t)
	f := fn{
		gcLastField: gcStructFieldFact{valid: true, immutable: true, local: 0, resultLocal: 1, identity: 7},
		gcResolved:  gcResolvedObject{valid: true, reg: R12},
		pinned:      maskOf(R12),
	}
	f.invalidateGCMutableLoadFacts()
	if !f.gcLastField.valid {
		t.Fatal("immutable field cache was cleared by unrelated mutable effect")
	}
	if f.gcResolved.valid || f.pinned.has(R12) {
		t.Fatal("raw resolved address survived mutable effect")
	}
	f.invalidateGCLoadFactsForLocal(1)
	if f.gcLastField.valid {
		t.Fatal("immutable field cache survived cached-result local replacement")
	}

	f.gcLastField = gcStructFieldFact{valid: true, local: 0, resultLocal: 1}
	f.invalidateGCMutableLoadFacts()
	if f.gcLastField.valid {
		t.Fatal("mutable field cache survived unknown mutable effect")
	}
}

func TestGCReferenceFactEliminatesRepeatedLoadsViaBoundedResultLocal(t *testing.T) {
	enableGCRefFacts(t)
	savedFacts, savedLoads := exactGCRefFactsEnabled, gcLoadForwardingEnabled
	defer func() { exactGCRefFactsEnabled, gcLoadForwardingEnabled = savedFacts, savedLoads }()
	exactGCRefFactsEnabled, gcLoadForwardingEnabled = true, true
	refTo0I32 := []byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f}
	arrayBody := []byte{
		0x01, 0x01, 0x7f, // one i32 result-cache local; parameter is local 0
		0x20, 0x00, 0xfb, 0x0f, 0x21, 0x01,
		0x20, 0x00, 0xfb, 0x0f,
		0x0b,
	}
	array := gcResolveReuseStats(t, []byte{0x5e, 0x7f, 0x01}, refTo0I32, arrayBody)
	if got := array.Peephole["gc-array-len-repeat-elide"]; got != 1 {
		t.Fatalf("gc-array-len-repeat-elide = %d, want 1 (all: %v)", got, array.Peephole)
	}
	if array.GCHandleResolutions != 1 {
		t.Fatalf("repeated array.len resolutions = %d, want 1", array.GCHandleResolutions)
	}

	structBody := []byte{
		0x01, 0x01, 0x7f,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x21, 0x01,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	strukt := gcResolveReuseStats(t, []byte{0x5f, 0x01, 0x7f, 0x00}, refTo0I32, structBody)
	if got := strukt.Peephole["gc-struct-get-repeat-elide"]; got != 1 {
		t.Fatalf("gc-struct-get-repeat-elide = %d, want 1 (all: %v)", got, strukt.Peephole)
	}
	if strukt.GCHandleResolutions != 1 {
		t.Fatalf("repeated immutable struct.get resolutions = %d, want 1", strukt.GCHandleResolutions)
	}

	gcLoadForwardingEnabled = false
	disabled := gcResolveReuseStats(t, []byte{0x5e, 0x7f, 0x01}, refTo0I32, arrayBody)
	if got := disabled.Peephole["gc-array-len-repeat-elide"]; got != 0 {
		t.Fatalf("disabled gc-array-len-repeat-elide = %d", got)
	}
	if array.CodeBytes >= disabled.CodeBytes {
		t.Fatalf("array.len forwarding code = %d bytes, disabled = %d; want smaller", array.CodeBytes, disabled.CodeBytes)
	}
	t.Logf("bounded array.len forwarding code bytes: enabled=%d disabled=%d", array.CodeBytes, disabled.CodeBytes)
}

func TestGCConstructorKnownBoundsElideLogicalArrayChecks(t *testing.T) {
	enableGCRefFacts(t)
	savedFacts, savedBounds := exactGCRefFactsEnabled, gcKnownArrayBoundsEnabled
	defer func() { exactGCRefFactsEnabled, gcKnownArrayBoundsEnabled = savedFacts, savedBounds }()
	exactGCRefFactsEnabled, gcKnownArrayBoundsEnabled = true, true
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0x20, 0x00, 0x41, 0x04, 0xfb, 0x06, 0x00, 0x21, 0x01,
		0x20, 0x01, 0x41, 0x02, 0x41, 0x09, 0xfb, 0x0e, 0x00,
		0x20, 0x01, 0x41, 0x02, 0xfb, 0x0b, 0x00,
		0x0b,
	}
	on := gcResolveReuseStats(t, []byte{0x5e, 0x7f, 0x01}, []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}, body)
	if got := on.Peephole["gc-array-known-bounds"]; got != 2 {
		t.Fatalf("gc-array-known-bounds = %d, want 2 (all: %v)", got, on.Peephole)
	}
	if got := on.Peephole["gc-array-const-index"]; got != 2 {
		t.Fatalf("gc-array-const-index = %d, want 2", got)
	}
	gcKnownArrayBoundsEnabled = false
	off := gcResolveReuseStats(t, []byte{0x5e, 0x7f, 0x01}, []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}, body)
	if got := off.Peephole["gc-array-known-bounds"]; got != 0 {
		t.Fatalf("disabled gc-array-known-bounds = %d", got)
	}
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("known-bounds code = %d bytes, disabled = %d; want smaller", on.CodeBytes, off.CodeBytes)
	}
	t.Logf("known array bounds code bytes: enabled=%d disabled=%d", on.CodeBytes, off.CodeBytes)
}

func TestGCExactFinalSubtypeSpecializesOpenStructGet(t *testing.T) {
	enableGCRefFacts(t)
	body := []byte{
		0x01, 0x01, 0x63, 0x01, // one (ref null 1) local
		0xfb, 0x01, 0x01, 0x21, 0x00, // struct.new_default 1; local.set 0
		0x20, 0x00,
		0xfb, 0x02, 0x00, 0x00, // struct.get open type 0 field 0
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x01},       // open type 0
			[]byte{0x4f, 0x01, 0x00, 0x5f, 0x01, 0x7f, 0x01}, // final type 1 <: 0
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	compile := func() *CodegenStats {
		t.Helper()
		var stats ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, Stats: &stats}); err != nil {
			t.Fatal(err)
		}
		return stats.Funcs[0]
	}
	got := compile()
	if got.Peephole["gc-nonfinal-struct-get-specialize"] != 1 || got.GCHandleResolutions != 1 {
		t.Fatalf("specialized open struct.get stats = %+v", got)
	}
	if got.Calls["hostsync"] != 1 {
		t.Fatalf("specialized open struct.get hostsync calls = %d, want constructor only", got.Calls["hostsync"])
	}

	exactGCRefFactsEnabled = false
	fallback := compile()
	if fallback.Peephole["gc-nonfinal-struct-get-specialize"] != 0 || fallback.GCHandleResolutions != 0 {
		t.Fatalf("disabled open struct.get specialization stats = %+v", fallback)
	}
	if fallback.Calls["hostsync"] != 2 {
		t.Fatalf("disabled open struct.get hostsync calls = %d, want constructor plus get", fallback.Calls["hostsync"])
	}
}

func TestGCReferenceFactLoadOpportunityCounters(t *testing.T) {
	enableGCRefFacts(t)
	arrayBody := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0x41, 0x02, 0xfb, 0x07, 0x00, 0x21, 0x00, // array.new_default 0 -> local 0
		0x20, 0x00, 0xfb, 0x16, 0x00, 0xfb, 0x0f, 0x1a,
		0x20, 0x00, 0xfb, 0x16, 0x00, 0xfb, 0x0f, 0x1a,
		0x41, 0x00, 0x0b,
	}
	array := gcReferenceFactStats(t, []byte{0x5e, 0x7f, 0x01}, arrayBody)
	if got := array.Peephole["gc-array-len-elide"]; got != 2 {
		t.Fatalf("gc-array-len-elide = %d, want 2 (all: %v)", got, array.Peephole)
	}
	if got := array.Peephole["gc-known-array-len"]; got != 2 {
		t.Fatalf("gc-known-array-len = %d, want 2", got)
	}

	structBody := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0xfb, 0x01, 0x00, 0x21, 0x00, // struct.new_default 0 -> local 0
		0x20, 0x00, 0xfb, 0x16, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0xfb, 0x16, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x1a,
		0x41, 0x00, 0x0b,
	}
	// final struct with one immutable nullable anyref field.
	strukt := gcReferenceFactStats(t, []byte{0x5f, 0x01, 0x6e, 0x00}, structBody)
	if got := strukt.Peephole["gc-struct-get-repeat"]; got != 1 {
		t.Fatalf("gc-struct-get-repeat = %d, want 1 (all: %v)", got, strukt.Peephole)
	}
	if got := strukt.Peephole["gc-known-struct-get"]; got != 2 {
		t.Fatalf("gc-known-struct-get = %d, want 2", got)
	}

	setGetBody := []byte{
		0x01, 0x01, 0x63, 0x00,
		0xfb, 0x01, 0x00, 0x21, 0x00,
		0x20, 0x00, 0xfb, 0x16, 0x00, 0xd0, 0x6e, 0xfb, 0x05, 0x00, 0x00,
		0x20, 0x00, 0xfb, 0x16, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x1a,
		0x41, 0x00, 0x0b,
	}
	setGet := gcReferenceFactStats(t, []byte{0x5f, 0x01, 0x6e, 0x01}, setGetBody)
	if got := setGet.Peephole["gc-struct-set-get-forward"]; got != 1 {
		t.Fatalf("gc-struct-set-get-forward = %d, want 1 (all: %v)", got, setGet.Peephole)
	}
	if got := setGet.Peephole["gc-barrier-none"]; got != 1 {
		t.Fatalf("gc-barrier-none = %d, want 1 (all: %v)", got, setGet.Peephole)
	}
	if got := setGet.Peephole["gc-barrier-elide"]; got != 1 {
		t.Fatalf("gc-barrier-elide = %d, want 1 (all: %v)", got, setGet.Peephole)
	}

	fillBody := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x04, 0xfb, 0x07, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x00, 0xd0, 0x6e, 0x41, 0x04, 0xfb, 0x10, 0x00,
		0x41, 0x00, 0x0b,
	}
	fill := gcReferenceFactStats(t, []byte{0x5e, 0x6e, 0x01}, fillBody)
	if got := fill.Peephole["gc-bulk-barrier-elide"]; got != 1 {
		t.Fatalf("gc-bulk-barrier-elide = %d, want 1 (all: %v)", got, fill.Peephole)
	}
}
