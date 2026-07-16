//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func stagedGCRefCastBytes(t testing.TB, class stagedGCRefCastClass) []byte {
	t.Helper()
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/ref_cast", &script)
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
			pin, ok := stagedGCRefCastLeaderPinFor(latest, cmd.Line)
			if ok && pin.Class == class {
				return append([]byte(nil), latest...)
			}
		}
	}
	t.Fatalf("official %s gc/ref_cast leader not found", class)
	return nil
}

func compileStagedGCRefCastProduct(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCStructProducts = true
	features.GCI31Products = true
	if product, ok := stagedGCStructExecutionProduct(data); ok && product == stagedGCStructRefCastAbstract {
		features.GCArrayProducts = true
	}
	return compileWithFrontendFeatures(cfg, data, features)
}

func expectGCRefCastTrap(t testing.TB, in *Instance, field string, index uint64, code TrapCode) {
	t.Helper()
	got, err := in.Invoke(field, index)
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != code {
		t.Fatalf("%s(%d) = %v, %v; want trap %v", field, index, got, err, code)
	}
}

func TestStagedGCRefCastProductBoundaryLifecycle(t *testing.T) {
	abstract := stagedGCRefCastBytes(t, stagedGCRefCastAbstract)
	concrete := stagedGCRefCastBytes(t, stagedGCRefCastConcrete)
	for class, data := range map[stagedGCRefCastClass][]byte{stagedGCRefCastAbstract: abstract, stagedGCRefCastConcrete: concrete} {
		if _, err := Compile(NewRuntimeConfig(), data); err == nil {
			t.Fatalf("public Compile admitted staged %s gc/ref_cast product", class)
		}
		guardCfg := NewRuntimeConfig()
		guardCfg.boundsChecks = BoundsChecksSignalsBased
		features := guardCfg.frontendFeatures()
		features.TypedFunctionReferences = true
		features.GCStructProducts = true
		features.GCArrayProducts = true
		features.GCI31Products = true
		if _, err := compileWithFrontendFeatures(guardCfg, data, features); err == nil || !strings.Contains(err.Error(), "signals-based") {
			t.Fatalf("%s guard compile gate = %v", class, err)
		}
	}

	abstractCompiled, err := compileStagedGCRefCastProduct(abstract)
	if err != nil {
		t.Fatal(err)
	}
	defer abstractCompiled.Close()
	if abstractCompiled.stagedGCStructProduct() != stagedGCStructRefCastAbstract || !abstractCompiled.usesGCStructHelpers() || !abstractCompiled.usesGCArrayHelpers() {
		t.Fatalf("abstract product/helper admission = %v/%v/%v", abstractCompiled.stagedGCStructProduct(), abstractCompiled.usesGCStructHelpers(), abstractCompiled.usesGCArrayHelpers())
	}
	abstractInstance, err := instantiateCore(abstractCompiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 48, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}})
	if err != nil {
		t.Fatal(err)
	}
	state := abstractInstance.existingGCRefTestTableState()
	if state == nil || state.TableCount != 1 || state.Count != 10 || state.Conversion == nil || abstractInstance.refStore == nil {
		t.Fatalf("abstract ref.cast state = %+v store=%p", state, abstractInstance.refStore)
	}
	extern, err := abstractInstance.NewExternRef("cast-foreign")
	if err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 100; iteration++ {
		if got, err := abstractInstance.Invoke("init", ValueExternRef(extern).Bits()); err != nil || len(got) != 0 {
			t.Fatalf("abstract init iteration %d = %v, %v", iteration, got, err)
		}
	}

	if got, err := abstractInstance.Invoke("ref_cast_non_null", 1); err != nil || len(got) != 0 {
		t.Fatalf("non-null i31 cast = %v, %v", got, err)
	}
	expectGCRefCastTrap(t, abstractInstance, "ref_cast_non_null", 0, TrapNullReference)
	if got, err := abstractInstance.Invoke("ref_cast_null", 0); err != nil || len(got) != 0 {
		t.Fatalf("nullable null casts = %v, %v", got, err)
	}
	expectGCRefCastTrap(t, abstractInstance, "ref_cast_null", 4, TrapCastFailure)
	if got, err := abstractInstance.Invoke("ref_cast_i31", 1); err != nil || len(got) != 0 {
		t.Fatalf("i31 identity casts = %v, %v", got, err)
	}
	expectGCRefCastTrap(t, abstractInstance, "ref_cast_i31", 2, TrapCastFailure)
	if got, err := abstractInstance.Invoke("ref_cast_struct", 2); err != nil || len(got) != 0 {
		t.Fatalf("struct identity casts = %v, %v", got, err)
	}
	if got, err := abstractInstance.Invoke("ref_cast_array", 3); err != nil || len(got) != 0 {
		t.Fatalf("array identity casts = %v, %v", got, err)
	}

	desc := state.Descriptor
	for _, tc := range []struct {
		index  int
		target gc.RefTestTarget
	}{
		{0, gc.RefTestTarget{Kind: gc.RefTestAny, Nullable: true}},
		{1, gc.RefTestTarget{Kind: gc.RefTestI31}},
		{2, gc.RefTestTarget{Kind: gc.RefTestStruct}},
		{3, gc.RefTestTarget{Kind: gc.RefTestArray}},
		{4, gc.RefTestTarget{Kind: gc.RefTestAny}},
	} {
		word := binary.LittleEndian.Uint64(desc[8+tc.index*8:])
		got, err := state.refCast(abstractInstance.gc, word, tc.target)
		if err != nil || got != word {
			t.Fatalf("abstract compact cast slot %d = %#x, %v; want original %#x", tc.index, got, err, word)
		}
	}
	if _, err := state.refCast(abstractInstance.gc, binary.LittleEndian.Uint64(desc[8+2*8:]), gc.RefTestTarget{Kind: gc.RefTestArray}); !errors.Is(err, gc.ErrCastFailure) {
		t.Fatalf("abstract mismatched compact cast = %v, want cast failure", err)
	}
	if _, err := state.refCast(abstractInstance.gc, 0xfedcba9876543210, gc.RefTestTarget{Kind: gc.RefTestAny}); err == nil || errors.Is(err, gc.ErrCastFailure) {
		t.Fatalf("forged foreign-any cast = %v, want ownership rejection", err)
	}
	if err := abstractInstance.gc.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if live := abstractInstance.gc.Stats().LiveObjects; live != 2 {
		t.Fatalf("abstract ref.cast live objects = %d, want struct+array roots", live)
	}

	abstractBlob, err := marshalCompiled(abstractCompiled)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(abstractCompiled, SnapshotOptions{}); err == nil {
		t.Fatal("snapshot admitted abstract gc/ref_cast product")
	}
	if err := abstractInstance.Close(); err != nil {
		t.Fatal(err)
	}
	if state.Conversion.count != 0 || !state.Conversion.closed {
		t.Fatalf("closed abstract ref.cast retained conversions: count=%d closed=%v", state.Conversion.count, state.Conversion.closed)
	}

	concreteCompiled, err := compileStagedGCRefCastProduct(concrete)
	if err != nil {
		t.Fatal(err)
	}
	defer concreteCompiled.Close()
	if concreteCompiled.stagedGCStructProduct() != stagedGCStructRefCastConcrete || !concreteCompiled.usesGCStructHelpers() || concreteCompiled.usesGCArrayHelpers() {
		t.Fatalf("concrete product/helper admission = %v/%v/%v", concreteCompiled.stagedGCStructProduct(), concreteCompiled.usesGCStructHelpers(), concreteCompiled.usesGCArrayHelpers())
	}
	concreteInstance, err := instantiateCore(concreteCompiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}})
	if err != nil {
		t.Fatal(err)
	}
	concreteState := concreteInstance.existingGCRefTestTableState()
	if concreteState == nil || concreteState.Count != 20 || concreteState.CanonicalType == nil || concreteState.Conversion != nil {
		t.Fatalf("concrete ref.cast state = %+v", concreteState)
	}
	for iteration := 0; iteration < 20; iteration++ {
		for _, field := range []string{"test-sub", "test-canon"} {
			if got, err := concreteInstance.Invoke(field); err != nil || len(got) != 0 {
				t.Fatalf("concrete %s iteration %d = %v, %v", field, iteration, got, err)
			}
		}
	}
	word := binary.LittleEndian.Uint64(concreteState.Descriptor[8+1*8:])
	if got, err := concreteState.refCast(concreteInstance.gc, word, gc.RefTestTarget{Kind: gc.RefTestDefined, Type: 2}); err != nil || got != word {
		t.Fatalf("canonical concrete cast = %#x, %v; want original %#x", got, err, word)
	}
	if err := concreteInstance.gc.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if live := concreteInstance.gc.Stats().LiveObjects; live != 8 {
		t.Fatalf("concrete ref.cast live objects = %d, want eight table roots", live)
	}
	concreteBlob, err := marshalCompiled(concreteCompiled)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(concreteCompiled, SnapshotOptions{}); err == nil {
		t.Fatal("snapshot admitted concrete gc/ref_cast product")
	}
	if err := concreteInstance.Close(); err != nil {
		t.Fatal(err)
	}

	for class, tc := range map[stagedGCRefCastClass]struct {
		blob []byte
	}{stagedGCRefCastAbstract: {abstractBlob}, stagedGCRefCastConcrete: {concreteBlob}} {
		var loaded Compiled
		if err := unmarshalCompiled(&loaded, tc.blob[5:]); err != nil {
			t.Fatal(err)
		}
		if loaded.stagedGCStructProduct() != 0 || loaded.usesGCStructHelpers() || loaded.usesGCArrayHelpers() {
			loaded.Close()
			t.Fatalf("codec inherited %s gc/ref_cast admission", class)
		}
		if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
			loaded.Close()
			t.Fatalf("codec-loaded %s gc/ref_cast instantiate = %v", class, err)
		}
		loaded.Close()
	}

	t.Logf("gc/ref_cast products: abstract wasm=%d code=%d codec=%d; concrete wasm=%d code=%d codec=%d; tableState=%d conversionState=%d plugin=%d", len(abstract), len(abstractCompiled.Code), len(abstractBlob), len(concrete), len(concreteCompiled.Code), len(concreteBlob), unsafe.Sizeof(gcRefTestTableState{}), unsafe.Sizeof(gcExternConversionState{}), unsafe.Sizeof(instancePluginState{}))
}

func BenchmarkStagedGCRefCastStableI31(b *testing.B) {
	c, err := compileStagedGCRefCastProduct(stagedGCRefCastBytes(b, stagedGCRefCastAbstract))
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
		got, err := in.Invoke("ref_cast_i31", 1)
		if err != nil || len(got) != 0 {
			b.Fatalf("ref_cast_i31(1) = %v, %v", got, err)
		}
	}
}
