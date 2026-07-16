//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func stagedGCRefTestAbstractBytes(t testing.TB) []byte {
	t.Helper()
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/ref_test", &script)
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
			pin, ok := stagedGCRefTestLeaderPinFor(latest, cmd.Line)
			if ok && pin.Class == stagedGCRefTestAbstract {
				return append([]byte(nil), latest...)
			}
		}
	}
	t.Fatal("official abstract gc/ref_test leader not found")
	return nil
}

func TestStagedGCRefTestAbstractMixedTableLifecycle(t *testing.T) {
	data := stagedGCRefTestAbstractBytes(t)
	if _, err := Compile(NewRuntimeConfig(), data); err == nil {
		t.Fatal("public Compile admitted staged abstract ref.test product")
	}
	c, err := compileStagedGCRefTestAccounting(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.stagedGCStructProduct() != stagedGCStructRefTestAbstract || !c.usesGCStructHelpers() || !c.usesGCArrayHelpers() {
		t.Fatalf("product/helper admission = %v/%v/%v", c.stagedGCStructProduct(), c.usesGCStructHelpers(), c.usesGCArrayHelpers())
	}
	in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 96, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}})
	if err != nil {
		t.Fatal(err)
	}
	state := in.existingGCRefTestTableState()
	if state == nil || state.TableCount != 3 || state.Count != 10 || state.Conversion == nil || in.refStore == nil {
		t.Fatalf("mixed table state = %+v store=%p", state, in.refStore)
	}
	extern, err := in.NewExternRef("official-extern")
	if err != nil {
		t.Fatal(err)
	}
	token := ValueExternRef(extern).Bits()
	for iteration := 0; iteration < 100; iteration++ {
		if got, err := in.Invoke("init", token); err != nil || len(got) != 0 {
			t.Fatalf("init iteration %d = %v, %v", iteration, got, err)
		}
	}

	any := state.Descriptors[0]
	if got := binary.LittleEndian.Uint64(any[8+3*8:]); got != uint64(gc.I31New(7)) {
		t.Fatalf("any table i31 = %#x, want %#x", got, gc.I31New(7))
	}
	for _, index := range []int{4, 5} {
		word := binary.LittleEndian.Uint64(any[8+index*8:])
		root, err := in.gc.CheckedTableSlot(state.Slots[index])
		if err != nil || word != uint64(root) || !root.IsObj() {
			t.Fatalf("any table object %d = %#x root=%#x err=%v", index, word, root, err)
		}
	}
	foreignAny := binary.LittleEndian.Uint64(any[8+6*8:])
	if foreign, err := state.Conversion.isForeignAny(foreignAny); err != nil || !foreign {
		t.Fatalf("converted foreign any = %#x, %v/%v", foreignAny, foreign, err)
	}
	if rooted, err := in.gc.CheckedTableSlot(state.Slots[6]); err != nil || !rooted.IsNull() {
		t.Fatalf("foreign any collector slot = %#x, %v; want null", rooted, err)
	}

	fun := state.Descriptors[1]
	if descriptor := binary.LittleEndian.Uint64(fun[8+2*32+24:]); descriptor == 0 {
		t.Fatal("funcref table did not retain local function identity")
	}
	externTable := state.Descriptors[2]
	if got := binary.LittleEndian.Uint64(externTable[8+2*8:]); got != token {
		t.Fatalf("extern table public token = %#x, want %#x", got, token)
	}
	for _, index := range []int{3, 4} {
		word := binary.LittleEndian.Uint64(externTable[8+index*8:])
		if word == 0 || word == token {
			t.Fatalf("extern table converted identity %d = %#x", index, word)
		}
	}
	if state.Conversion.count != 3 {
		t.Fatalf("conversion entries after repeated init = %d, want foreign+i31+object", state.Conversion.count)
	}
	if err := in.gc.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if live := in.gc.Stats().LiveObjects; live != 3 {
		t.Fatalf("mixed table live objects = %d, want any struct/array plus converted struct", live)
	}

	before := binary.LittleEndian.Uint64(externTable[8+4*8:])
	if err := state.setTable(in.gc, 2, 4, 0xfedcba9876543210); err == nil || !strings.Contains(err.Error(), "externref token") {
		t.Fatalf("forged extern table set = %v", err)
	}
	if after := binary.LittleEndian.Uint64(externTable[8+4*8:]); after != before {
		t.Fatalf("rejected extern table set mutated %#x -> %#x", before, after)
	}
	if err := state.setTable(in.gc, 2, 10, 0); err == nil || !strings.Contains(err.Error(), "out of bounds") {
		t.Fatalf("out-of-bounds extern table set = %v", err)
	}

	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := in.ExternRefValue(extern); ok {
		t.Fatal("closed private mixed product retained externref token")
	}
	if state.Conversion.count != 0 || !state.Conversion.closed {
		t.Fatalf("closed mixed product retained conversions: count=%d closed=%v", state.Conversion.count, state.Conversion.closed)
	}

	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.stagedGCStructProduct() != 0 || loaded.usesGCStructHelpers() || loaded.usesGCArrayHelpers() {
		t.Fatal("codec inherited mixed ref.test admission")
	}
	if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
		t.Fatalf("codec-loaded mixed product instantiate = %v", err)
	}
	if _, err := Capture(c, SnapshotOptions{}); err == nil {
		t.Fatal("snapshot admitted mixed ref.test product")
	}

	t.Logf("abstract ref.test product: wasm=%d code=%d codec=%d tableState=%d conversionState=%d plugin=%d", len(data), len(c.Code), len(blob), unsafe.Sizeof(gcRefTestTableState{}), unsafe.Sizeof(gcExternConversionState{}), unsafe.Sizeof(instancePluginState{}))
}

func BenchmarkStagedGCRefTestAbstractActions(b *testing.B) {
	data := stagedGCRefTestAbstractBytes(b)
	c, err := compileStagedGCRefTestAccounting(data)
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
		got, err := in.Invoke("ref_test_any", 6)
		if err != nil || len(got) != 1 || got[0] != 2 {
			b.Fatalf("ref_test_any(6) = %v, %v", got, err)
		}
	}
}
