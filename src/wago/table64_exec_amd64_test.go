//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func boundedTable64Module(max uint64) []byte {
	table := []byte{0x70, 0x05}
	table = append(table, uleb64(2)...)
	table = append(table, uleb64(max)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(4))),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 0),
			wasmtest.ExportEntry("clear", 0, 1),
			wasmtest.ExportEntry("is_null", 0, 2),
			wasmtest.ExportEntry("grow", 0, 3),
			wasmtest.ExportEntry("fill", 0, 4),
			wasmtest.ExportEntry("table", 1, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfc, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x70, 0x26, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd2, 0x00, 0x20, 0x01, 0xfc, 0x11, 0x00, 0x0b}),
		)),
	)
}

func table32GetSetGrowSizeFillModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x02, 0x04})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfc, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x70, 0x26, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x70, 0x20, 0x01, 0xfc, 0x11, 0x00, 0x0b}),
		)),
	)
}

func compileStagedTable64(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Table64 = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func TestStagedTable64LocalGetSetSizeAndProductRoundTrip(t *testing.T) {
	module := boundedTable64Module(4)
	if _, err := Compile(nil, module); err == nil || !strings.Contains(err.Error(), "table64") {
		t.Fatalf("public table64 compile error = %v", err)
	}
	compiled, err := compileStagedTable64(module)
	if err != nil {
		t.Fatalf("compile staged table64: %v", err)
	}
	defer compiled.Close()
	if !compiled.requiredFeatures.IsEnabled(CoreFeatureTable64) {
		t.Fatalf("table64 required features = %s", compiled.requiredFeatures)
	}
	if !compiled.HasTable || !compiled.TableAddr64 || compiled.TableSize != 2 || compiled.TableMax != 4 || !compiled.TableHasMax {
		t.Fatalf("compiled table64 shape = present %v addr64 %v size/max %d/%d hasMax %v", compiled.HasTable, compiled.TableAddr64, compiled.TableSize, compiled.TableMax, compiled.TableHasMax)
	}
	meta := (&Module{c: compiled}).Metadata()
	if len(meta.Tables) != 1 || !meta.Tables[0].Addr64 || meta.Tables[0].Min != 2 || meta.Tables[0].Max != 4 || !meta.Tables[0].HasMax || !reflect.DeepEqual(meta.Tables[0].Exports, []string{"table"}) {
		t.Fatalf("table64 metadata = %#v", meta.Tables)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal table64: %v", err)
	}
	if blob[4] != 26 {
		t.Fatalf("table64 codec version = %d, want 26", blob[4])
	}
	var public Compiled
	if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
		t.Fatalf("public table64 codec load error = %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("decode table64 metadata: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true // private execution proof; codec never serializes admission
	loaded.hasTableExportMetadata = true
	if !loaded.requiredFeatures.IsEnabled(CoreFeatureTable64) || !loaded.TableAddr64 || !reflect.DeepEqual((&Module{c: &loaded}).Metadata().Tables, meta.Tables) {
		t.Fatalf("table64 codec metadata = %#v, want %#v", (&Module{c: &loaded}).Metadata().Tables, meta.Tables)
	}
	if _, err := Capture(compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "tables cannot be snapshotted") {
		t.Fatalf("table64 snapshot error = %v", err)
	}

	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate table64: %v", err)
	}
	defer in.Close()
	if got, err := in.Invoke("size"); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("table64.size = %v, err=%v", got, err)
	}
	for _, index := range []uint64{0, 1} {
		if got, err := in.Invoke("is_null", index); err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("table64.get(%d) null = %v, err=%v", index, got, err)
		}
		if _, err := in.Invoke("clear", index); err != nil {
			t.Fatalf("table64.set(%d): %v", index, err)
		}
	}
	for _, index := range []uint64{2, 1 << 32, ^uint64(0)} {
		if _, err := in.Invoke("is_null", index); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
			t.Fatalf("table64.get(%d) error = %v", index, err)
		}
		if _, err := in.Invoke("clear", index); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
			t.Fatalf("table64.set(%d) error = %v", index, err)
		}
	}
	if got, err := in.Invoke("is_null", 1); err != nil || got[0] != 1 {
		t.Fatalf("table64 state changed after traps = %v, err=%v", got, err)
	}
	if _, err := in.Invoke("fill", 0, 2); err != nil {
		t.Fatalf("table64.fill full range: %v", err)
	}
	for _, index := range []uint64{0, 1} {
		if got, err := in.Invoke("is_null", index); err != nil || got[0] != 0 {
			t.Fatalf("table64.fill entry %d null = %v, err=%v", index, got, err)
		}
	}
	if _, err := in.Invoke("clear", 1); err != nil {
		t.Fatalf("clear table64 fill sentinel: %v", err)
	}
	if _, err := in.Invoke("fill", 2, 0); err != nil {
		t.Fatalf("zero-length table64.fill at boundary: %v", err)
	}
	for _, args := range [][2]uint64{{1, 2}, {1 << 32, 0}, {0, 1 << 32}, {^uint64(0), 2}} {
		if _, err := in.Invoke("fill", args[0], args[1]); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
			t.Fatalf("table64.fill(%d,%d) error = %v", args[0], args[1], err)
		}
		if got, err := in.Invoke("is_null", 1); err != nil || got[0] != 1 {
			t.Fatalf("trapping table64.fill changed sentinel = %v, err=%v", got, err)
		}
	}
	if got, err := in.Invoke("is_null", 0); err != nil || got[0] != 0 {
		t.Fatalf("trapping table64.fill changed prior entry = %v, err=%v", got, err)
	}
	if _, err := in.Invoke("clear", 0); err != nil {
		t.Fatalf("clear table64 fill entry: %v", err)
	}
	if got, err := in.Invoke("grow", 1); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("table64.grow(1) = %v, err=%v, want [2]", got, err)
	}
	if got, err := in.Invoke("size"); err != nil || got[0] != 3 {
		t.Fatalf("table64 size after grow = %v, err=%v", got, err)
	}
	if got, err := in.Invoke("is_null", 2); err != nil || got[0] != 1 {
		t.Fatalf("grown table64 entry = %v, err=%v", got, err)
	}
	if got, err := in.Invoke("grow", 0); err != nil || got[0] != 3 {
		t.Fatalf("table64.grow(0) = %v, err=%v", got, err)
	}
	if got, err := in.Invoke("grow", 1); err != nil || got[0] != 3 {
		t.Fatalf("table64 grow to maximum = %v, err=%v", got, err)
	}
	for _, delta := range []uint64{1, 1 << 32, ^uint64(0)} {
		if got, err := in.Invoke("grow", delta); err != nil || len(got) != 1 || got[0] != ^uint64(0) {
			t.Fatalf("table64.grow(%d) = %v, err=%v, want [-1]", delta, got, err)
		}
		if got, err := in.Invoke("size"); err != nil || got[0] != 4 {
			t.Fatalf("failed table64 grow changed size = %v, err=%v", got, err)
		}
	}

	loadedIn, err := instantiateCore(&loaded, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate codec-reloaded table64: %v", err)
	}
	defer loadedIn.Close()
	if _, err := loadedIn.Invoke("fill", 0, 2); err != nil {
		t.Fatalf("codec-reloaded table64.fill: %v", err)
	}
	if got, err := loadedIn.Invoke("is_null", 1); err != nil || got[0] != 0 {
		t.Fatalf("codec-reloaded table64.fill entry = %v, err=%v", got, err)
	}
	if got, err := loadedIn.Invoke("grow", 2); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("codec-reloaded table64.grow = %v, err=%v", got, err)
	}
	if got, err := loadedIn.Invoke("grow", 1<<32); err != nil || len(got) != 1 || got[0] != ^uint64(0) {
		t.Fatalf("codec-reloaded high-delta table64.grow = %v, err=%v", got, err)
	}
}

func TestStagedTable64GatesAndTable32CodeStability(t *testing.T) {
	unbounded := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x04, 0x01})))
	if _, err := compileStagedTable64(unbounded); err == nil || !strings.Contains(err.Error(), "explicit maximum") {
		t.Fatalf("unbounded table64 error = %v", err)
	}
	if _, err := compileStagedTable64(boundedTable64Module(16385)); err == nil || !strings.Contains(err.Error(), "16384") {
		t.Fatalf("oversized table64 error = %v", err)
	}
	imported := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	imported = append(imported, byte(wasm.ExternTable), 0x70, 0x05, 0x01, 0x02)
	if _, err := compileStagedTable64(wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(imported)))); err == nil || !strings.Contains(err.Error(), "exactly one local table") {
		t.Fatalf("imported table64 error = %v", err)
	}
	table := []byte{0x70, 0x05, 0x01, 0x02}
	if _, err := compileStagedTable64(wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(table, table)))); err == nil || !strings.Contains(err.Error(), "exactly one local table") {
		t.Fatalf("multiple table64 error = %v", err)
	}
	externref := []byte{0x6f, 0x05, 0x01, 0x02}
	if _, err := compileStagedTable64(wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(externref)))); err == nil || !strings.Contains(err.Error(), "funcref") {
		t.Fatalf("externref table64 error = %v", err)
	}
	cfg := NewRuntimeConfig()
	cfg.boundsChecks = BoundsChecksSignalsBased
	features := cfg.frontendFeatures()
	features.Table64 = true
	if _, err := compileWithFrontendFeatures(cfg, boundedTable64Module(4), features); err == nil || !strings.Contains(err.Error(), "signals-based") {
		t.Fatalf("guard table64 error = %v", err)
	}

	ordinary := table32GetSetGrowSizeFillModule()
	base, err := Compile(nil, ordinary)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	stageCfg := NewRuntimeConfig()
	stageFeatures := stageCfg.frontendFeatures()
	stageFeatures.Table64 = true
	staged, err := compileWithFrontendFeatures(stageCfg, ordinary, stageFeatures)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if !bytes.Equal(base.Code, staged.Code) {
		t.Fatal("enabling staged table64 changed table32 code bytes")
	}
}

func BenchmarkStagedTable64FillZero(b *testing.B) {
	compiled, err := compileStagedTable64(boundedTable64Module(4))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("fill", 2, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedTable64GrowZero(b *testing.B) {
	compiled, err := compileStagedTable64(boundedTable64Module(4))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := in.Invoke("grow", 0); err != nil || len(got) != 1 || got[0] != 2 {
			b.Fatalf("table64.grow(0) = %v, err=%v", got, err)
		}
	}
}

func BenchmarkStagedTable64Size(b *testing.B) {
	compiled, err := compileStagedTable64(boundedTable64Module(4))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("size"); err != nil {
			b.Fatal(err)
		}
	}
}
