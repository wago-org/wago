//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func exactGCRefFactModule(t *testing.T, controlBoundary bool) *wasm.Module {
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
	if got := boundary.Peephole["gc-ref-cast-elide"]; got != 0 {
		t.Fatalf("fact crossed control boundary: gc-ref-cast-elide = %d", got)
	}
	if got := boundary.Calls["gcnative"]; got != 1 {
		t.Fatalf("control-boundary native cast calls = %d, want 1", got)
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
	compile := func(m *wasm.Module, shared bool) (int, ModuleStats) {
		savedShared, savedReuse := gcSharedStubsEnabled, gcResolveReuseEnabled
		gcSharedStubsEnabled, gcResolveReuseEnabled = shared, false
		defer func() { gcSharedStubsEnabled, gcResolveReuseEnabled = savedShared, savedReuse }()
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		defer cm.CodeImage.Close()
		return len(cm.Code), stats
	}

	oneOnBytes, oneOn := compile(module(1), true)
	oneOffBytes, _ := compile(module(1), false)
	if oneOn.GCSharedStubs != 0 || oneOn.GCSharedStubCallSites != 0 || oneOnBytes != oneOffBytes {
		t.Fatalf("one-site crossover emitted shared resolver: bytes=%d/%d stats=%+v", oneOnBytes, oneOffBytes, oneOn)
	}

	const sites = 8
	onBytes, on := compile(module(sites), true)
	offBytes, off := compile(module(sites), false)
	if on.GCSharedStubs != 1 || on.GCSharedStubCallSites != sites || on.GCSharedStubBytes == 0 {
		t.Fatalf("shared resolver stats = bodies %d calls %d bytes %d", on.GCSharedStubs, on.GCSharedStubCallSites, on.GCSharedStubBytes)
	}
	if off.GCSharedStubs != 0 || off.GCSharedStubCallSites != 0 || off.GCSharedStubBytes != 0 {
		t.Fatalf("disabled shared resolver stats = bodies %d calls %d bytes %d", off.GCSharedStubs, off.GCSharedStubCallSites, off.GCSharedStubBytes)
	}
	if onBytes >= offBytes {
		t.Fatalf("shared resolver code = %d bytes, inline = %d; want shared smaller", onBytes, offBytes)
	}
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
	if got := array.Peephole["gc-array-len-repeat"]; got != 1 {
		t.Fatalf("gc-array-len-repeat = %d, want 1 (all: %v)", got, array.Peephole)
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
	if got := setGet.Peephole["gc-struct-set-get"]; got != 1 {
		t.Fatalf("gc-struct-set-get = %d, want 1 (all: %v)", got, setGet.Peephole)
	}
}
