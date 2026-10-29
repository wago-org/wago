//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"slices"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64RootedCollectorCallableRequiresClosedSourceProof(t *testing.T) {
	cfg := rv64RootedScalarConfig()
	cfg.railshotState.tailProducts = true
	compiled, err := cfg.Compile(rv64PublicRootedCollectorReferenceCallModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64RootedClosedCallableFunction(view, 0) || railshotNonCallableFunction(view, 0) {
		t.Fatal("fresh closed collector target has no callable row")
	}
	for name, mutate := range map[string]func(*Compiled){
		"signature-key": func(candidate *Compiled) {
			candidate.FuncTypeID = slices.Clone(candidate.FuncTypeID)
			candidate.FuncTypeID[0] ^= 1
		},
		"code": func(candidate *Compiled) {
			candidate.code = slices.Clone(candidate.code)
			candidate.code[0] ^= 1
		},
		"table-export":            func(candidate *Compiled) { candidate.tableExports = map[string]int{"open": 0} },
		"reference-global-import": func(candidate *Compiled) { candidate.GlobalImports = []GlobalImportDef{{Type: ValAnyRef}} },
		"reference-export-param": func(candidate *Compiled) {
			candidate.Funcs = slices.Clone(candidate.Funcs)
			candidate.Funcs[1].Params = []ValType{ValAnyRef}
		},
		"no-native-abi": func(candidate *Compiled) {
			candidate.nativeReferenceCallABI = false
			candidate.codeCache.setNativeGCABIVersion(0)
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			cache, memo := *candidate.codeCache, *candidate.validateMemo
			candidate.codeCache, candidate.validateMemo = &cache, &memo
			mutate(candidate)
			if rv64RootedClosedCallableFunction(candidate, 0) {
				t.Fatal("changed collector target retained a callable row")
			}
		})
	}
}

func TestRV64RootedCollectorCallableLookupAllocation(t *testing.T) {
	cfg := rv64RootedScalarConfig()
	cfg.railshotState.tailProducts = true
	compiled, err := cfg.Compile(rv64PublicRootedCollectorReferenceCallModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	allocs := testing.AllocsPerRun(1000, func() {
		if railshotNonCallableFunction(view, 0) {
			panic("fresh closed collector target is not callable")
		}
	})
	t.Logf("warm callable lookup: %.0f allocations/run; Compiled=%d memo=%d indexes=%d RV64-state=%d bytes", allocs, unsafe.Sizeof(Compiled{}), unsafe.Sizeof(validateMemo{}), unsafe.Sizeof(compiledCacheIndexes{}), unsafe.Sizeof(railshotTailCompiledState{}))
	if allocs != 0 {
		t.Fatalf("warm callable lookup allocations=%v, want 0", allocs)
	}
	mask := view.loadCompileIndexes().railshotTail.rootedClosedCallables
	t.Logf("callable bitmap: %d bytes for %d local functions", len(mask)*8, len(view.Funcs))
	if len(mask) != (len(view.Funcs)+63)/64 {
		t.Fatal("callable bitmap does not match the local function bound")
	}
}

func TestRV64RootedCollectorCallableSnapshotRequiresSourceProof(t *testing.T) {
	cfg := rv64RootedScalarConfig()
	cfg.railshotState.tailProducts = true
	compiled, err := cfg.Compile(rv64PublicRootedCollectorReferenceCallModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	// A mutable copy still has shared cold indexes. It must authenticate its
	// own metadata and must not obtain the snapshot's cached positive result.
	mutable := cloneCompiledMetadata(view)
	mutable.code = slices.Clone(mutable.code)
	mutable.code[0] ^= 1
	if rv64RootedClosedCallableFunction(mutable, 0) {
		t.Fatal("mutable copy used the published snapshot's classification")
	}
	// A decode without process-local compiler proof has no callable mask,
	// even if its exact signatures and code match a valid compiled module.
	proofless := cloneCompiledExecutionMetadata(view)
	proofless.validateMemo = &validateMemo{}
	if err := railshotInitializeExecutionSnapshot(proofless); err != nil {
		t.Fatal(err)
	}
	proofless.validateMemo.execution = proofless
	if rv64RootedClosedCallableFunction(proofless, 0) || proofless.loadCompileIndexes() != nil {
		t.Fatal("proofless snapshot inherited a callable classification")
	}
}

func TestRV64RootedCollectorCallableSnapshotQuota(t *testing.T) {
	cfg := rv64RootedScalarConfig()
	cfg.railshotState.tailProducts = true
	compiled, err := cfg.Compile(rv64PublicRootedCollectorReferenceCallModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	withCache, err := snapshotMetadataBytes(view, 0)
	if err != nil {
		t.Fatal(err)
	}
	withoutCache := cloneCompiledMetadata(view)
	withoutCache.nativeReferenceCallABI = false
	memo := *withoutCache.validateMemo
	withoutCache.validateMemo = &memo
	withoutCache.validateMemo.gcFrameRoots = nil
	base, err := snapshotMetadataBytes(withoutCache, 0)
	if err != nil {
		t.Fatal(err)
	}
	cacheBytes := uint64(railshotExecutionSnapshotCacheBytes(view))
	primitiveBytes := uint64(0)
	if compiled.rv64ExecutionContract() != nil {
		primitiveBytes = uint64(unsafe.Sizeof(uint64(0)))
	}
	collectorBytes := cacheBytes - primitiveBytes
	if collectorBytes != 8 || withCache-base != 2*collectorBytes {
		t.Fatalf("snapshot collector cache=%d primitive state=%d budget delta=%d, want one collector word with allocation rounding", collectorBytes, primitiveBytes, withCache-base)
	}
	if _, err := compiled.freezeExecution(withCache - 1); err == nil {
		t.Fatal("snapshot quota omitted callable-cache bytes")
	}
	if snapshot, err := compiled.freezeExecution(withCache); err != nil || snapshot != view {
		t.Fatalf("exact snapshot quota=%d failed: %v", withCache, err)
	}
}

func BenchmarkRV64RootedCollectorCallableLookup(b *testing.B) {
	cfg := rv64RootedScalarConfig()
	cfg.railshotState.tailProducts = true
	compiled, err := cfg.Compile(rv64PublicRootedCollectorReferenceCallModule())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if railshotNonCallableFunction(view, 0) {
			b.Fatal("fresh closed collector target is not callable")
		}
	}
}

// Ordinary local calls carry collector references under the exact GC ABI. They
// do not require the separate dynamic reference-call transport.
func TestRV64RootedCollectorOrdinaryLocalCallable(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			[]byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x63, 0x00},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 9, 0xfb, 0, 0, 0x1a, 0x20, 0, 0x0b}),
			append(wasmtest.ULEB(30), []byte{1, 1, 0x63, 0, 0x20, 0, 0xfb, 0, 0, 0x21, 1, 0x20, 1, 0x20, 1, 0x10, 0, 0x1a, 0xfb, 2, 0, 0, 0x20, 1, 0xfb, 2, 0, 0, 0x6a, 0x0b}...),
		)),
	)
	c, err := NewRuntimeConfig().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.nativeReferenceCallABI {
		t.Fatal("ordinary-call fixture selected dynamic reference-call ABI")
	}
	if !rv64RootedClosedCallableFunction(c.executionView(), 0) {
		t.Fatal("ordinary collector target has no callable row")
	}
	for _, name := range []string{"abi", "roots"} {
		candidate := cloneCompiledMetadata(c.executionView())
		cache, memo := *candidate.codeCache, *candidate.validateMemo
		candidate.codeCache, candidate.validateMemo = &cache, &memo
		candidate.nativeReferenceCallABI = false
		if name == "abi" {
			candidate.codeCache.setNativeGCABIVersion(0)
		} else {
			candidate.validateMemo.gcFrameRoots = nil
		}
		if rv64RootedClosedCallableFunction(candidate, 0) {
			t.Fatalf("missing %s retained callable row", name)
		}
	}

	for _, candidate := range []*Compiled{c} {
		for _, profile := range []corergc.Profile{corergc.ProfileThroughput, corergc.ProfileTiny} {
			in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: profile, CollectEveryAlloc: true, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			got, err := in.Invoke("run", 21)
			in.Close()
			if err != nil || len(got) != 1 || got[0] != 42 {
				t.Fatalf("ordinary collector call=%v, %v", got, err)
			}
		}
	}
}
