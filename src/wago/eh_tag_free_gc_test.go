//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The consumer imports a function table, but no tag. After unwinding the producer,
// only its catch_all_ref slot owns the producer's freshly allocated payload.
func TestGCCatchAllRefWithoutTags(t *testing.T) {
	structType := []byte{0x5f, 0x01, 0x7f, 0x00} // (struct (field i32))
	voidType := wasmtest.FuncType(nil, nil)
	providerBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, voidType, []byte{0x60, 0x01, 0x64, 0x00, 0x00})),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x01})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("throwers", byte(wasm.ExternTable), 0))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x2a, 0xfb, 0x00, 0x00, 0x08, 0x00, 0x0b}))),
	)
	funcImport := func(name string) []byte {
		return append(append(wasmtest.Name("env"), wasmtest.Name(name)...), 0x00, 0x01)
	}
	// (func (export "run")
	//   (drop (struct.new 0 (i32.const 0)))
	//   (block (result exnref)
	//     (try_table (catch_all_ref 0) (call_indirect (type 1) (i32.const 0))) (unreachable))
	//   (call 0) (drop))
	consumerBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, voidType)),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("throwers")...), 0x01, 0x70, 0x00, 0x01), funcImport("collect"))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, 0xfb, 0x00, 0x00, 0x1a,
			0x02, 0x69, 0x1f, 0x40, 0x01, 0x03, 0x00, 0x41, 0x00, 0x11, 0x01, 0x00, 0x0b, 0x00, 0x0b,
			0x10, 0x00, 0x1a, 0x0b,
		}))),
	)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerCode, err := Compile(cfg, providerBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, consumerBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	if status := consumerCode.GCNativeRootAdmission(); !status.Required || !status.Exact {
		t.Fatalf("consumer root admission = %+v", status)
	}
	store := newReferenceStore(false)
	defer store.closeRuntime()
	gcConfig := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 32, VerifyAfterCollect: true}
	provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	throwers, err := provider.ExportedTable("throwers")
	if err != nil {
		t.Fatal(err)
	}
	collections := 0
	collect := func(host HostModule, _, _ []uint64) {
		if err := host.(GCHostModule).CollectGC(); err != nil {
			t.Error(err)
			return
		}
		collections++
		if live := provider.gc.Stats().LiveObjects; live != 1 {
			t.Errorf("live payload objects after collection = %d, want 1", live)
		}
	}
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: testImports("env.throwers", throwers, "env.collect", collect)})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if provider.gc == nil || provider.gc != consumer.gc {
		t.Fatal("instances must share a collector")
	}
	if _, err := consumer.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if collections != 1 {
		t.Fatalf("collections = %d, want 1", collections)
	}
	if err := consumer.CollectGC(); err != nil {
		t.Fatal(err)
	}
	if live := consumer.gc.Stats().LiveObjects; live != 0 {
		t.Fatalf("live objects after caught exception is dropped = %d, want 0", live)
	}
}
