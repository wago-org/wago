//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

func stagedGCBrOnCastBytes(t testing.TB, base string, class stagedGCBrOnCastClass) []byte {
	t.Helper()
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, base, &script)
	var latest []byte
	for _, cmd := range script.Commands {
		switch cmd.Type {
		case "module_definition":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				t.Fatal(err)
			}
			latest = data
		case "module_instance":
			pin, ok := stagedGCBrOnCastLeaderPinFor(base, latest, cmd.Line)
			if ok && pin.Class == class {
				return append([]byte(nil), latest...)
			}
		}
	}
	t.Fatalf("official %s %s leader not found", base, class)
	return nil
}

func stagedGCBrOnCastProductFor(base string, class stagedGCBrOnCastClass) stagedGCStructProduct {
	if base == "gc/br_on_cast" {
		switch class {
		case stagedGCBrOnCastAbstract:
			return stagedGCStructBrOnCastAbstract
		case stagedGCBrOnCastConcrete:
			return stagedGCStructBrOnCastConcrete
		case stagedGCBrOnCastNullability:
			return stagedGCStructBrOnCastNullability
		}
	}
	switch class {
	case stagedGCBrOnCastAbstract:
		return stagedGCStructBrOnCastFailAbstract
	case stagedGCBrOnCastConcrete:
		return stagedGCStructBrOnCastFailConcrete
	case stagedGCBrOnCastNullability:
		return stagedGCStructBrOnCastFailNullability
	default:
		return 0
	}
}

func compileStagedGCBrOnCastProduct(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCStructProducts = true
	if product, ok := stagedGCStructExecutionProduct(data); ok && (product == stagedGCStructBrOnCastAbstract || product == stagedGCStructBrOnCastFailAbstract) {
		features.GCArrayProducts = true
		features.GCI31Products = true
	}
	return compileWithFrontendFeatures(cfg, data, features)
}

func invokeBranchI32(t testing.TB, in *Instance, field string, want uint32, args ...uint64) {
	t.Helper()
	got, err := in.Invoke(field, args...)
	if err != nil || len(got) != 1 || uint32(got[0]) != want {
		t.Fatalf("%s%v = %v, %v; want i32 %d", field, args, got, err, want)
	}
}

func TestStagedGCBrOnCastProductBoundaryLifecycle(t *testing.T) {
	bases := []string{"gc/br_on_cast", "gc/br_on_cast_fail"}
	classes := []stagedGCBrOnCastClass{stagedGCBrOnCastAbstract, stagedGCBrOnCastConcrete, stagedGCBrOnCastNullability}
	for _, base := range bases {
		for _, class := range classes {
			data := stagedGCBrOnCastBytes(t, base, class)
			if _, err := Compile(NewRuntimeConfig(), data); err == nil {
				t.Fatalf("public Compile admitted staged %s %s product", base, class)
			}
			guardCfg := NewRuntimeConfig()
			guardCfg.boundsChecks = BoundsChecksSignalsBased
			features := guardCfg.frontendFeatures()
			features.TypedFunctionReferences = true
			features.GCStructProducts = true
			features.GCArrayProducts = class == stagedGCBrOnCastAbstract
			features.GCI31Products = class == stagedGCBrOnCastAbstract
			if _, err := compileWithFrontendFeatures(guardCfg, data, features); err == nil || !strings.Contains(err.Error(), "signals-based") {
				t.Fatalf("%s %s guard compile gate = %v", base, class, err)
			}
		}
	}

	var blobs [][]byte
	for _, base := range bases {
		data := stagedGCBrOnCastBytes(t, base, stagedGCBrOnCastAbstract)
		c, err := compileStagedGCBrOnCastProduct(data)
		if err != nil {
			t.Fatal(err)
		}
		wantProduct := stagedGCBrOnCastProductFor(base, stagedGCBrOnCastAbstract)
		if c.stagedGCStructProduct() != wantProduct || !c.usesGCStructHelpers() || !c.usesGCArrayHelpers() {
			c.Close()
			t.Fatalf("%s abstract product/helper admission = %v/%v/%v", base, c.stagedGCStructProduct(), c.usesGCStructHelpers(), c.usesGCArrayHelpers())
		}
		in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 72, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}})
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		state := in.existingGCRefTestTableState()
		if state == nil || state.TableCount != 1 || state.Count != 10 || state.Conversion == nil {
			in.Close()
			c.Close()
			t.Fatalf("%s abstract state = %+v", base, state)
		}
		extern, err := in.NewExternRef("branch-cast")
		if err != nil {
			in.Close()
			c.Close()
			t.Fatal(err)
		}
		for iteration := 0; iteration < 100; iteration++ {
			if got, err := in.Invoke("init", ValueExternRef(extern).Bits()); err != nil || len(got) != 0 {
				in.Close()
				c.Close()
				t.Fatalf("%s init iteration %d = %v, %v", base, iteration, got, err)
			}
		}
		if base == "gc/br_on_cast" {
			invokeBranchI32(t, in, "br_on_i31", 7, 1)
			invokeBranchI32(t, in, "br_on_struct", 6, 2)
			invokeBranchI32(t, in, "br_on_array", 3, 3)
			invokeBranchI32(t, in, "br_on_i31", ^uint32(0), 4)
		} else {
			invokeBranchI32(t, in, "br_on_non_i31", 7, 1)
			invokeBranchI32(t, in, "br_on_non_struct", 6, 2)
			invokeBranchI32(t, in, "br_on_non_array", 3, 3)
			invokeBranchI32(t, in, "br_on_non_i31", ^uint32(0), 4)
		}
		invokeBranchI32(t, in, "null-diff", 1, 0)
		invokeBranchI32(t, in, "null-diff", 0, 1)
		if err := in.gc.CollectFull(nil); err != nil {
			t.Fatal(err)
		}
		if live := in.gc.Stats().LiveObjects; live != 2 {
			t.Fatalf("%s abstract live objects = %d, want two table roots", base, live)
		}
		blob, err := marshalCompiled(c)
		if err != nil {
			t.Fatal(err)
		}
		blobs = append(blobs, blob)
		if _, err := Capture(c, SnapshotOptions{}); err == nil {
			t.Fatalf("snapshot admitted %s abstract branch-cast product", base)
		}
		t.Logf("%s abstract: wasm=%d code=%d codec=%d", base, len(data), len(c.Code), len(blob))
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
		if state.Conversion.count != 0 || !state.Conversion.closed {
			t.Fatalf("%s close retained conversion state", base)
		}
		c.Close()
	}

	for _, base := range bases {
		data := stagedGCBrOnCastBytes(t, base, stagedGCBrOnCastConcrete)
		c, err := compileStagedGCBrOnCastProduct(data)
		if err != nil {
			t.Fatal(err)
		}
		wantProduct := stagedGCBrOnCastProductFor(base, stagedGCBrOnCastConcrete)
		if c.stagedGCStructProduct() != wantProduct || !c.usesGCStructHelpers() || c.usesGCArrayHelpers() {
			c.Close()
			t.Fatalf("%s concrete product/helper admission = %v/%v/%v", base, c.stagedGCStructProduct(), c.usesGCStructHelpers(), c.usesGCArrayHelpers())
		}
		in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}})
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		state := in.existingGCRefTestTableState()
		if state == nil || state.Count != 20 || state.CanonicalType == nil || state.Conversion != nil {
			in.Close()
			c.Close()
			t.Fatalf("%s concrete state = %+v", base, state)
		}
		for iteration := 0; iteration < 20; iteration++ {
			for _, field := range []string{"test-sub", "test-canon"} {
				if got, err := in.Invoke(field); err != nil || len(got) != 0 {
					in.Close()
					c.Close()
					t.Fatalf("%s %s iteration %d = %v, %v", base, field, iteration, got, err)
				}
			}
		}
		if err := in.gc.CollectFull(nil); err != nil {
			t.Fatal(err)
		}
		if live := in.gc.Stats().LiveObjects; live != 8 {
			t.Fatalf("%s concrete live objects = %d, want eight table roots", base, live)
		}
		blob, err := marshalCompiled(c)
		if err != nil {
			t.Fatal(err)
		}
		blobs = append(blobs, blob)
		if _, err := Capture(c, SnapshotOptions{}); err == nil {
			t.Fatalf("snapshot admitted %s concrete branch-cast product", base)
		}
		t.Logf("%s concrete: wasm=%d code=%d codec=%d", base, len(data), len(c.Code), len(blob))
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
		c.Close()
	}

	for _, base := range bases {
		data := stagedGCBrOnCastBytes(t, base, stagedGCBrOnCastNullability)
		c, err := compileStagedGCBrOnCastProduct(data)
		if err != nil {
			t.Fatal(err)
		}
		if c.stagedGCStructProduct() != stagedGCBrOnCastProductFor(base, stagedGCBrOnCastNullability) || !c.usesGCStructHelpers() {
			c.Close()
			t.Fatalf("%s nullability product/helper admission = %v/%v", base, c.stagedGCStructProduct(), c.usesGCStructHelpers())
		}
		in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 8}})
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		if in.gc == nil || in.existingGCRefTestTableState() != nil {
			in.Close()
			c.Close()
			t.Fatalf("%s nullability collector/table state = %p/%p", base, in.gc, in.existingGCRefTestTableState())
		}
		blob, err := marshalCompiled(c)
		if err != nil {
			t.Fatal(err)
		}
		blobs = append(blobs, blob)
		t.Logf("%s nullability: wasm=%d code=%d codec=%d", base, len(data), len(c.Code), len(blob))
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
		c.Close()
	}

	for i, blob := range blobs {
		var loaded Compiled
		if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
			t.Fatal(err)
		}
		if loaded.stagedGCStructProduct() != 0 || loaded.usesGCStructHelpers() || loaded.usesGCArrayHelpers() {
			loaded.Close()
			t.Fatalf("codec inherited branch-cast admission for blob %d", i)
		}
		if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
			loaded.Close()
			t.Fatalf("codec-loaded branch-cast blob %d instantiate = %v", i, err)
		}
		loaded.Close()
	}

	t.Logf("branch-cast sidecars: tableState=%d conversionState=%d plugin=%d", unsafe.Sizeof(gcRefTestTableState{}), unsafe.Sizeof(gcExternConversionState{}), unsafe.Sizeof(instancePluginState{}))
}

func BenchmarkStagedGCBrOnCastStableI31(b *testing.B) {
	data := stagedGCBrOnCastBytes(b, "gc/br_on_cast", stagedGCBrOnCastAbstract)
	c, err := compileStagedGCBrOnCastProduct(data)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	extern, err := in.NewExternRef("benchmark")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := in.Invoke("init", ValueExternRef(extern).Bits()); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("br_on_i31", 1)
		if err != nil || len(got) != 1 || uint32(got[0]) != 7 {
			b.Fatalf("br_on_i31(1) = %v, %v", got, err)
		}
	}
}
