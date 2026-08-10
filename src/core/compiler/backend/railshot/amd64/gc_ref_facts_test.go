//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
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

func TestFinalGCParameterFactsResolveRecursiveGroupIndex(t *testing.T) {
	recParam := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0, Rec: true}), false))
	m := &wasm.Module{Types: []wasm.RecType{
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}},
		{SubTypes: []wasm.SubType{
			{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct}},
			{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{recParam}}},
		}},
	}}
	f := fn{m: m, localGCRefFacts: make([]shared.GCRefFact, 1)}
	f.seedFinalGCParameterTypes([]wasm.ValType{recParam}, 1, 2)
	if got, ok := f.localGCRefFacts[0].ExactType(); !ok || got != 1 {
		t.Fatalf("recursive final parameter fact = %d,%v, want flattened type 1", got, ok)
	}
}

func TestNullableFinalGCParameterRetainsNonNullCast(t *testing.T) {
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

func TestStructuredGCReferenceFactIntersectionAndLoopSubset(t *testing.T) {
	left := shared.ExactGCRefFact(3, 11, shared.GCHeapArray).
		WithFreshness(shared.GCFreshUnpublished).
		WithKnownArrayLength(7)
	f := fn{localGCRefFacts: []shared.GCRefFact{left, left}}
	joined := f.snapshotGCRefFacts()
	f.localGCRefFacts[0] = shared.ExactGCRefFact(4, 12, shared.GCHeapArray).WithKnownArrayLength(9)
	f.mergeGCRefFactsInto(&joined)
	if _, exact := joined[0].ExactType(); exact || joined[0].Identity() != 0 || joined[0].HeapClass() != shared.GCHeapArray {
		t.Fatalf("contradictory join retained exact identity: %+v", joined[0])
	}
	if _, known := joined[0].KnownArrayLength(); known {
		t.Fatalf("contradictory join retained array length: %+v", joined[0])
	}
	f.installGCRefFacts([]shared.GCRefFact{left, left})
	f.invalidateLoopModifiedGCRefFacts(map[uint32]bool{0: true})
	if !f.localGCRefFacts[0].IsZero() || f.localGCRefFacts[1].IsZero() {
		t.Fatalf("loop subset invalidation = %+v", f.localGCRefFacts)
	}
	f.freeGCRefFactBuf(joined)
}

func TestDisabledGCReferenceFactsDoNotAllocateSnapshots(t *testing.T) {
	saved := exactGCRefFactsEnabled
	exactGCRefFactsEnabled = false
	defer func() { exactGCRefFactsEnabled = saved }()
	f := fn{localGCRefFacts: make([]shared.GCRefFact, 1024)}
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

func TestGCReferenceFactLoadOpportunityCounters(t *testing.T) {
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
