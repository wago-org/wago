//go:build linux && amd64

package wago

import (
	"reflect"
	"strings"
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

func cloneGCSnapshotForTest(s *Snapshot) *Snapshot {
	clone := *s
	clone.memories = append([]memorySnap(nil), s.memories...)
	clone.memory = append([]byte(nil), s.memory...)
	clone.globals = append([]globalSnap(nil), s.globals...)
	clone.passiveDataLens = append([]uint32(nil), s.passiveDataLens...)
	clone.gcGlobalRefs = append([]gcSnapshotRef(nil), s.gcGlobalRefs...)
	clone.gcObjects = make([]gcObjectSnapshot, len(s.gcObjects))
	for i, object := range s.gcObjects {
		clone.gcObjects[i] = object
		clone.gcObjects[i].values = append([]gcSnapshotValue(nil), object.values...)
	}
	return &clone
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

func TestGCWarmSnapshotRejectsMalformedGraphs(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), gcLiveSnapshotCycleModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	valid, err := Capture(compiled, SnapshotOptions{Kind: SnapshotWarm, WarmFunc: "warm", WarmArgs: []uint64{7}})
	if err != nil {
		t.Fatal(err)
	}
	if len(valid.gcObjects) != 1 || len(valid.gcObjects[0].values) != 2 {
		t.Fatalf("unexpected valid graph shape: %+v", valid.gcObjects)
	}

	cases := []struct {
		name string
		edit func(*Snapshot)
		want string
	}{
		{name: "unknown root kind", edit: func(s *Snapshot) { s.gcGlobalRefs[0].kind = 0xff }, want: "unknown GC reference kind"},
		{name: "object root out of range", edit: func(s *Snapshot) { s.gcGlobalRefs[0].value = 2 }, want: "exceeds object count"},
		{name: "null root payload", edit: func(s *Snapshot) { s.gcGlobalRefs[0] = gcSnapshotRef{value: 1} }, want: "null reference has payload"},
		{name: "wrong value count", edit: func(s *Snapshot) { s.gcObjects[0].values = s.gcObjects[0].values[:1] }, want: "value count"},
		{name: "wrong storage kind", edit: func(s *Snapshot) { s.gcObjects[0].values[1].kind = s.gcObjects[0].values[0].kind }, want: "does not match layout kind"},
		{name: "reference payload on numeric field", edit: func(s *Snapshot) { s.gcObjects[0].values[1].ref = gcSnapshotRef{kind: gcSnapshotRefObject, value: 1} }, want: "reference payload for non-reference storage"},
		{name: "unreachable object", edit: func(s *Snapshot) { s.gcObjects = append(s.gcObjects, s.gcObjects[0]) }, want: "object 2 is unreachable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := cloneGCSnapshotForTest(valid)
			tc.edit(candidate)
			if _, err := candidate.MarshalBinary(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("MarshalBinary error = %v, want %q", err, tc.want)
			}
		})
	}
}
