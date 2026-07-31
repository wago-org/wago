//go:build linux && amd64

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func gcLiveSnapshotCycleModule() []byte {
	// (struct (field (mut (ref null 0))) (field (mut i32)))
	structType := []byte{0x5f, 0x02, 0x63, 0x00, 0x01, 0x7f, 0x01}
	warmType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	getType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	global := []byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b}
	warm := []byte{0x01, 0x01, 0x63, 0x00,
		0xfb, 0x01, 0x00, 0x21, 0x01,
		0x20, 0x01, 0x20, 0x01, 0xfb, 0x05, 0x00, 0x00,
		0x20, 0x01, 0x20, 0x00, 0xfb, 0x05, 0x00, 0x01,
		0x20, 0x01, 0x24, 0x00,
		0x20, 0x01, 0x24, 0x01,
		0x0b}
	get := []byte{0x00, 0x23, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xfb, 0x02, 0x00, 0x01, 0x0b}
	same := []byte{0x00, 0x23, 0x00, 0x23, 0x01, 0xd3, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, warmType, getType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(global, global)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("warm", 0, 0),
			wasmtest.ExportEntry("get", 0, 1),
			wasmtest.ExportEntry("same", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(warm))), warm...),
			append(wasmtest.ULEB(uint32(len(get))), get...),
			append(wasmtest.ULEB(uint32(len(same))), same...),
		)),
	)
}

func TestGCWarmSnapshotPreservesCyclesAndSharing(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), gcLiveSnapshotCycleModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	snapshot, err := Capture(compiled, SnapshotOptions{
		Kind: SnapshotWarm, WarmFunc: "warm", WarmArgs: []uint64{0x12345678},
		GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 64, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.gcObjects) != 1 || len(snapshot.gcGlobalRefs) != 2 || snapshot.gcGlobalRefs[0] != snapshot.gcGlobalRefs[1] {
		t.Fatalf("captured GC graph objects/roots = %d/%v", len(snapshot.gcObjects), snapshot.gcGlobalRefs)
	}
	blob, err := snapshot.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if len(blob) < 5 || blob[4] != 4 {
		t.Fatalf("snapshot version = %v, want 4", blob[:5])
	}
	loaded, err := LoadSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Module().Close()
	for _, source := range []Instantiable{snapshot, loaded} {
		in, err := Instantiate(source)
		if err != nil {
			t.Fatal(err)
		}
		got, getErr := in.Invoke("get")
		if getErr != nil || !reflect.DeepEqual(got, []uint64{0x12345678}) {
			in.Close()
			t.Fatalf("restored cyclic value = %v, %v", got, getErr)
		}
		got, sameErr := in.Invoke("same")
		if sameErr != nil || !reflect.DeepEqual(got, []uint64{1}) {
			in.Close()
			t.Fatalf("restored sharing = %v, %v", got, sameErr)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
