//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

func stagedGCExternBytes(t testing.TB) []byte {
	t.Helper()
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/extern", &script)
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
			if _, ok := stagedGCExternLeaderPinFor(latest, cmd.Line); ok {
				return append([]byte(nil), latest...)
			}
		}
	}
	t.Fatal("official gc/extern leader not found")
	return nil
}

func TestStagedGCExternProductBoundaryLifecycle(t *testing.T) {
	data := stagedGCExternBytes(t)
	m, err := corewasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := corewasm.ValidateModule(m); err == nil || !strings.Contains(err.Error(), "constant expression required") {
		t.Fatalf("default validation of GC conversion constants = %v", err)
	}
	if err := corewasm.ValidateModuleWithFeatures(m, corewasm.ValidationFeatures{GCConstExpr: true}); err != nil {
		t.Fatalf("staged GC conversion constant validation: %v", err)
	}
	if _, err := Compile(NewRuntimeConfig(), data); err == nil {
		t.Fatal("public Compile admitted staged gc/extern product")
	}
	guardCfg := NewRuntimeConfig()
	guardCfg.boundsChecks = BoundsChecksSignalsBased
	guardFeatures := guardCfg.frontendFeatures()
	guardFeatures.TypedFunctionReferences = true
	guardFeatures.GCStructProducts = true
	guardFeatures.GCArrayProducts = true
	guardFeatures.GCI31Products = true
	if _, err := compileWithFrontendFeatures(guardCfg, data, guardFeatures); err == nil || !strings.Contains(err.Error(), "signals-based") {
		t.Fatalf("guard compile gate = %v", err)
	}
	c, err := compileStagedGCExternAccounting(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.stagedGCStructProduct() != stagedGCStructExtern || !c.usesGCStructHelpers() || !c.usesGCArrayHelpers() {
		t.Fatalf("product/helper admission = %v/%v/%v", c.stagedGCStructProduct(), c.usesGCStructHelpers(), c.usesGCArrayHelpers())
	}
	in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 48, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}})
	if err != nil {
		t.Fatal(err)
	}
	state := in.existingGCRefTestTableState()
	if state == nil || state.TableCount != 1 || state.Count != 10 || state.Conversion == nil || in.refStore == nil {
		t.Fatalf("extern product state = %+v store=%p", state, in.refStore)
	}
	foreign0, err := in.NewExternRef("foreign-0")
	if err != nil {
		t.Fatal(err)
	}
	token0 := ValueExternRef(foreign0).Bits()
	for iteration := 0; iteration < 100; iteration++ {
		if got, err := in.Invoke("init", token0); err != nil || len(got) != 0 {
			t.Fatalf("init iteration %d = %v, %v", iteration, got, err)
		}
	}
	desc := state.Descriptor
	if got := binary.LittleEndian.Uint64(desc[8+1*8:]); got != uint64(gc.I31New(7)) {
		t.Fatalf("any table i31 = %#x, want %#x", got, gc.I31New(7))
	}
	for _, index := range []int{2, 3} {
		word := binary.LittleEndian.Uint64(desc[8+index*8:])
		root, err := in.gc.CheckedTableSlot(state.Slots[index])
		if err != nil || word != uint64(root) || !root.IsObj() {
			t.Fatalf("any table object %d = %#x root=%#x err=%v", index, word, root, err)
		}
	}
	foreignAny := binary.LittleEndian.Uint64(desc[8+4*8:])
	if ok, err := state.Conversion.isForeignAny(foreignAny); err != nil || !ok {
		t.Fatalf("table foreign any = %#x, %v/%v", foreignAny, ok, err)
	}
	if err := in.gc.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if live := in.gc.Stats().LiveObjects; live != 2 {
		t.Fatalf("extern table live objects = %d, want struct+array", live)
	}

	foreign1, err := in.NewExternRef("foreign-1")
	if err != nil {
		t.Fatal(err)
	}
	token1 := ValueExternRef(foreign1).Bits()
	if got, err := in.Invoke("internalize", token1); err != nil || len(got) != 1 || got[0] != token1 {
		t.Fatalf("internalize foreign = %v, %v; want stable public any token %#x", got, err, token1)
	}
	if got, err := in.Invoke("internalize", 0); err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("internalize null = %v, %v", got, err)
	}
	foreign2, err := in.NewExternRef("foreign-2")
	if err != nil {
		t.Fatal(err)
	}
	token2 := ValueExternRef(foreign2).Bits()
	if got, err := in.Invoke("externalize", token2); err != nil || len(got) != 1 || got[0] != token2 {
		t.Fatalf("externalize host any = %v, %v; want %#x", got, err, token2)
	}
	if got, err := in.Invoke("externalize", 0); err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("externalize null = %v, %v", got, err)
	}

	var publicExtern [4]uint64
	var publicAny [4]uint64
	for i, index := range []uint64{1, 2, 3, 4} {
		ext, err := in.Invoke("externalize-i", index)
		if err != nil || len(ext) != 1 || ext[0] == 0 || ext[0]>>32 == 0 {
			t.Fatalf("externalize-i(%d) = %v, %v", index, ext, err)
		}
		externWord := ext[0]
		any, err := in.Invoke("externalize-ii", index)
		if err != nil || len(any) != 1 || any[0] == 0 || any[0]>>32 == 0 {
			t.Fatalf("externalize-ii(%d) = %v, %v", index, any, err)
		}
		anyWord := any[0]
		publicExtern[i], publicAny[i] = externWord, anyWord
		if roundTrip, err := in.Invoke("internalize", externWord); err != nil || len(roundTrip) != 1 || roundTrip[0] != anyWord {
			t.Fatalf("public round trip %d = %v, %v; want %#x", index, roundTrip, err, anyWord)
		}
	}
	if publicExtern[0] == publicAny[0] || publicExtern[1] == publicAny[1] {
		t.Fatal("public extern and any token categories collided")
	}
	if state.Conversion.count != 6 {
		t.Fatalf("conversion entries = %d, want three foreign plus i31/struct/array", state.Conversion.count)
	}
	beforeCount := state.Conversion.count
	if _, err := in.Invoke("externalize", 0xfedcba9876543210); err == nil || !strings.Contains(err.Error(), "invalid anyref token") {
		t.Fatalf("forged public any ingress = %v", err)
	}
	if _, err := in.Invoke("internalize", 0xfedcba9876543210); err == nil || !strings.Contains(err.Error(), "invalid externref token") {
		t.Fatalf("forged public extern ingress = %v", err)
	}
	if state.Conversion.count != beforeCount {
		t.Fatalf("rejected public ingress changed conversion count %d -> %d", beforeCount, state.Conversion.count)
	}

	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(c, SnapshotOptions{}); err == nil {
		t.Fatal("snapshot admitted gc/extern product")
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if state.Conversion.count != 0 || !state.Conversion.closed {
		t.Fatalf("closed extern product retained conversions: count=%d closed=%v", state.Conversion.count, state.Conversion.closed)
	}
	if _, err := in.Invoke("internalize", token0); err == nil {
		t.Fatal("closed extern product remained callable")
	}

	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.stagedGCStructProduct() != 0 || loaded.usesGCStructHelpers() || loaded.usesGCArrayHelpers() {
		t.Fatal("codec inherited gc/extern admission")
	}
	if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
		t.Fatalf("codec-loaded gc/extern instantiate = %v", err)
	}

	t.Logf("gc/extern product: wasm=%d code=%d codec=%d tableState=%d conversionState=%d entry=%d plugin=%d", len(data), len(c.Code), len(blob), unsafe.Sizeof(gcRefTestTableState{}), unsafe.Sizeof(gcExternConversionState{}), unsafe.Sizeof(gcExternConversionEntry{}), unsafe.Sizeof(instancePluginState{}))
}

func BenchmarkStagedGCExternStableRoundTrip(b *testing.B) {
	data := stagedGCExternBytes(b)
	c, err := compileStagedGCExternAccounting(data)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init", 0); err != nil {
		b.Fatal(err)
	}
	ext, err := in.Invoke("externalize-i", 2)
	if err != nil || len(ext) != 1 {
		b.Fatalf("warm externalize-i = %v, %v", ext, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("internalize", ext[0])
		if err != nil || len(got) != 1 || got[0] == 0 {
			b.Fatalf("internalize = %v, %v", got, err)
		}
	}
}
