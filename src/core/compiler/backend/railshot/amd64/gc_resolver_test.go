//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcNativeAttributionModule(t testing.TB, controlBoundary bool) *wasm.Module {
	t.Helper()
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0xfb, 0x01, 0x00, // struct.new_default 0
		0x21, 0x00, // local.set 0
	}
	if controlBoundary {
		body = append(body, 0x02, 0x40, 0x0b)
	}
	body = append(body,
		0x20, 0x00,
		0xfb, 0x16, 0x00, // ref.cast (ref 0)
		0xd1, // ref.is_null
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
		t.Fatalf("decode GC attribution module: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate GC attribution module: %v", err)
	}
	return m
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
		t.Fatalf("array len/get resolutions = %d reused = %d, want 1/1", stats.GCHandleResolutions, stats.GCHandleResolutionReuse)
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

	oneOnBytes, oneOn := compile(gcResolverDensityModule(t, 1), true, false)
	oneOffBytes, _ := compile(gcResolverDensityModule(t, 1), false, false)
	if oneOn.GCSharedStubs != 0 || oneOn.GCSharedStubCallSites != 0 || oneOnBytes != oneOffBytes {
		t.Fatalf("one-site crossover emitted shared resolver: bytes=%d/%d stats=%+v", oneOnBytes, oneOffBytes, oneOn)
	}

	const sites = 8
	onBytes, on := compile(gcResolverDensityModule(t, sites), true, false)
	offBytes, off := compile(gcResolverDensityModule(t, sites), false, false)
	if on.GCSharedStubs != 1 || on.GCSharedStubCallSites != sites || on.GCSharedStubBytes == 0 {
		t.Fatalf("shared resolver stats = bodies %d calls %d bytes %d", on.GCSharedStubs, on.GCSharedStubCallSites, on.GCSharedStubBytes)
	}
	if off.GCSharedStubs != 0 || off.GCSharedStubCallSites != 0 || off.GCSharedStubBytes != 0 {
		t.Fatalf("disabled shared resolver stats = bodies %d calls %d bytes %d", off.GCSharedStubs, off.GCSharedStubCallSites, off.GCSharedStubBytes)
	}
	if onBytes >= offBytes {
		t.Fatalf("shared resolver code = %d bytes, inline = %d; want shared smaller", onBytes, offBytes)
	}

	reuseSharedBytes, reuseShared := compile(gcResolverDensityModule(t, sites), true, true)
	reuseInlineBytes, _ := compile(gcResolverDensityModule(t, sites), false, true)
	if reuseShared.GCSharedStubs != 0 || reuseShared.GCSharedStubCallSites != 0 || reuseSharedBytes != reuseInlineBytes {
		t.Fatalf("one-function reuse emitted code-growing shared island: bytes=%d/%d stats=%+v", reuseSharedBytes, reuseInlineBytes, reuseShared)
	}
	if reuseShared.Compile.FunctionAttempts != 1 {
		t.Fatalf("one-function reuse attempts = %d, want 1", reuseShared.Compile.FunctionAttempts)
	}

	distinctSharedBytes, distinctShared := compile(gcDistinctResolverModule(t, sites), true, true)
	distinctInlineBytes, _ := compile(gcDistinctResolverModule(t, sites), false, true)
	if distinctShared.GCSharedStubs != 1 || distinctShared.GCSharedStubCallSites != sites-1 || distinctSharedBytes >= distinctInlineBytes {
		t.Fatalf("distinct one-function sites did not select shared island: bytes=%d/%d stats=%+v", distinctSharedBytes, distinctInlineBytes, distinctShared)
	}
	if distinctShared.Compile.FunctionAttempts != 1 {
		t.Fatalf("distinct one-function attempts = %d, want 1", distinctShared.Compile.FunctionAttempts)
	}
}
