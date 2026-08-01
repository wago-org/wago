//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func gcTableSnapshotModule() []byte {
	// (struct (field (mut (ref null 0))) (field (mut i32)))
	structType := []byte{0x5f, 0x02, 0x63, 0x00, 0x01, 0x7f, 0x01}
	warmType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	getType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	resultType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	tableType := []byte{0x63, 0x00, 0x01, 0x01, 0x03} // (table 1 3 (ref null 0))

	warm := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0xfb, 0x01, 0x00, 0x21, 0x01, // struct.new_default 0; local.set 1
		0x20, 0x01, 0x20, 0x01, 0xfb, 0x05, 0x00, 0x00, // self-cycle in field 0
		0x20, 0x01, 0x20, 0x00, 0xfb, 0x05, 0x00, 0x01, // value in field 1
		0x41, 0x00, 0x20, 0x01, 0x26, 0x00, // table.set 0 0
		0xd0, 0x00, 0x41, 0x01, 0xfc, 0x0f, 0x00, 0x1a, // table.grow 0 by one null slot
		0x41, 0x01, 0x20, 0x01, 0x26, 0x00, // table.set 0 1 to the same object
		0xd0, 0x00, 0x41, 0x09, 0xfb, 0x00, 0x00, 0x1a, // collect-capable allocation after publication
		0x0b,
	}
	get := []byte{0x00, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x02, 0x00, 0x01, 0x0b}
	same := []byte{0x00, 0x41, 0x00, 0x25, 0x00, 0x41, 0x01, 0x25, 0x00, 0xd3, 0x0b}
	size := []byte{0x00, 0xfc, 0x10, 0x00, 0x0b}
	churn := []byte{
		0x00,
		0xd0, 0x00, 0x41, 0x0b, 0xfb, 0x00, 0x00, 0x1a, // allocate and discard
		0x41, 0x01, 0x25, 0x00, 0xfb, 0x02, 0x00, 0x01,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, warmType, getType, resultType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(3), wasmtest.ULEB(3))),
		wasmtest.Section(4, wasmtest.Vec(tableType)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("warm", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("get", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("same", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("size", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("churn", byte(wasm.ExternFunc), 4),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(warm))), warm...),
			append(wasmtest.ULEB(uint32(len(get))), get...),
			append(wasmtest.ULEB(uint32(len(same))), same...),
			append(wasmtest.ULEB(uint32(len(size))), size...),
			append(wasmtest.ULEB(uint32(len(churn))), churn...),
		)),
	)
}

func gcMultiTableSnapshotModule() []byte {
	struct0 := []byte{0x5f, 0x01, 0x7f, 0x01}
	struct1 := []byte{0x5f, 0x02, 0x63, 0x00, 0x01, 0x7f, 0x01}
	warmType := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil)
	readType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	resultType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	table0 := []byte{0x63, 0x00, 0x01, 0x01, 0x02}
	table1 := []byte{0x63, 0x01, 0x01, 0x01, 0x03}
	warm := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x00,
		0x22, 0x02, 0x26, 0x00,
		0xd0, 0x01, 0x41, 0x01, 0xfc, 0x0f, 0x01, 0x1a,
		0x41, 0x01,
		0x20, 0x02, 0x20, 0x01, 0xfb, 0x00, 0x01,
		0x26, 0x01,
		0xd0, 0x00, 0x21, 0x02,
		0x41, 0x09, 0xfb, 0x00, 0x00, 0x1a,
		0x0b,
	}
	read0 := []byte{0x00, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	read1 := []byte{0x00, 0x20, 0x00, 0x25, 0x01, 0xfb, 0x02, 0x01, 0x01, 0x0b}
	same := []byte{0x00, 0x41, 0x00, 0x25, 0x00, 0x41, 0x01, 0x25, 0x01, 0xfb, 0x02, 0x01, 0x00, 0xd3, 0x0b}
	size0 := []byte{0x00, 0xfc, 0x10, 0x00, 0x0b}
	size1 := []byte{0x00, 0xfc, 0x10, 0x01, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(struct0, struct1, warmType, readType, resultType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(3), wasmtest.ULEB(4), wasmtest.ULEB(4), wasmtest.ULEB(4))),
		wasmtest.Section(4, wasmtest.Vec(table0, table1)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("warm", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("read0", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("read1", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("same", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("size0", byte(wasm.ExternFunc), 4),
			wasmtest.ExportEntry("size1", byte(wasm.ExternFunc), 5),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(warm))), warm...),
			append(wasmtest.ULEB(uint32(len(read0))), read0...),
			append(wasmtest.ULEB(uint32(len(read1))), read1...),
			append(wasmtest.ULEB(uint32(len(same))), same...),
			append(wasmtest.ULEB(uint32(len(size0))), size0...),
			append(wasmtest.ULEB(uint32(len(size1))), size1...),
		)),
	)
}

func TestGCWarmSnapshotPreservesMultipleHeterogeneousLocalTables(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), gcMultiTableSnapshotModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, tc := range []struct {
		name string
		gc   GCConfig
	}{
		{name: "throughput", gc: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}},
		{name: "tiny", gc: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, TinyStepBudget: 1, VerifyAfterCollect: true, StressBarriers: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := SnapshotOptions{Kind: SnapshotWarm, WarmFunc: "warm", WarmArgs: []uint64{41, 83}, GC: tc.gc}
			snapshot, err := Capture(compiled, opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.gcTableRefs) != 0 || len(snapshot.gcTableRoots) != 2 || len(snapshot.gcTableRoots[0]) != 1 || len(snapshot.gcTableRoots[1]) != 2 || len(snapshot.gcObjects) != 2 {
				t.Fatalf("captured multi-table roots/objects = %v/%v/%d", snapshot.gcTableRefs, snapshot.gcTableRoots, len(snapshot.gcObjects))
			}
			if snapshot.gcTableRoots[0][0] != (gcSnapshotRef{kind: gcSnapshotRefObject, value: 1}) || snapshot.gcTableRoots[1][0] != (gcSnapshotRef{}) || snapshot.gcTableRoots[1][1] != (gcSnapshotRef{kind: gcSnapshotRefObject, value: 2}) {
				t.Fatalf("captured multi-table root identity = %v", snapshot.gcTableRoots)
			}
			if got := snapshot.gcObjects[1].values[0].ref; got != snapshot.gcTableRoots[0][0] {
				t.Fatalf("cross-table shared object = %v, want %v", got, snapshot.gcTableRoots[0][0])
			}
			blob, err := snapshot.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			if len(blob) < 5 || blob[4] != snapshotVersion {
				t.Fatalf("multi-table snapshot version = %v, want %d", blob[:5], snapshotVersion)
			}
			repeated, err := Capture(compiled, opts)
			if err != nil {
				t.Fatal(err)
			}
			repeatedBlob, err := repeated.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(blob, repeatedBlob) {
				t.Fatal("repeated multi-table capture was not deterministic")
			}
			loaded, err := LoadSnapshot(blob)
			if err != nil {
				t.Fatal(err)
			}
			defer loaded.Module().Close()
			roundTripBlob, err := loaded.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(blob, roundTripBlob) {
				t.Fatal("snapshot-v6 codec round-trip changed the encoded graph")
			}
			if _, err := LoadSnapshot(blob[:len(blob)-1]); err == nil {
				t.Fatal("truncated snapshot-v6 blob loaded")
			}
			if _, err := LoadSnapshot(append(append([]byte(nil), blob...), 0)); err == nil || !strings.Contains(err.Error(), "trailing snapshot bytes") {
				t.Fatalf("snapshot-v6 trailing byte error = %v", err)
			}
			for _, source := range []Instantiable{snapshot, loaded} {
				in, err := Instantiate(source)
				if err != nil {
					t.Fatal(err)
				}
				for name, want := range map[string][]uint64{"size0": {1}, "size1": {2}, "same": {1}} {
					got, callErr := in.Invoke(name)
					if callErr != nil || !reflect.DeepEqual(got, want) {
						in.Close()
						t.Fatalf("restored %s = %v, %v; want %v", name, got, callErr, want)
					}
				}
				for _, read := range []struct {
					name string
					arg  uint64
					want uint64
				}{{"read0", 0, 41}, {"read1", 1, 83}} {
					got, callErr := in.Invoke(read.name, read.arg)
					if callErr != nil || !reflect.DeepEqual(got, []uint64{read.want}) {
						in.Close()
						t.Fatalf("restored %s = %v, %v; want %d", read.name, got, callErr, read.want)
					}
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestGCSnapshotRejectsGCReferenceFunctionImportDomain(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), gcCrossInstancePersistentConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if _, err := Capture(compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "GC-reference function imports require whole-domain capture ownership") {
		t.Fatalf("Capture error = %v, want whole-domain function-import rejection", err)
	}
}

func TestGCWarmSnapshotRejectsMalformedMultipleLocalTableRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), gcMultiTableSnapshotModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	valid, err := Capture(compiled, SnapshotOptions{Kind: SnapshotWarm, WarmFunc: "warm", WarmArgs: []uint64{41, 83}})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func(*Snapshot)
		want string
	}{
		{name: "table count", edit: func(s *Snapshot) { s.gcTableRoots = s.gcTableRoots[:1] }, want: "does not match module table count"},
		{name: "below second minimum", edit: func(s *Snapshot) { s.gcTableRoots[1] = nil }, want: "table 1 root count 0 is below declared minimum"},
		{name: "wrong heterogeneous type", edit: func(s *Snapshot) { s.gcTableRoots[1][1] = s.gcTableRoots[0][0] }, want: "table 1 slot 1: reference does not match declared structural type"},
		{name: "unreachable second object", edit: func(s *Snapshot) { s.gcTableRoots[1][1] = gcSnapshotRef{} }, want: "object 2 is unreachable from snapshot roots"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := *valid
			candidate.gcGlobalRefs = append([]gcSnapshotRef(nil), valid.gcGlobalRefs...)
			candidate.gcTableRoots = make([][]gcSnapshotRef, len(valid.gcTableRoots))
			for i := range valid.gcTableRoots {
				candidate.gcTableRoots[i] = append([]gcSnapshotRef(nil), valid.gcTableRoots[i]...)
			}
			candidate.gcObjects = append([]gcObjectSnapshot(nil), valid.gcObjects...)
			tc.edit(&candidate)
			if _, err := candidate.MarshalBinary(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("MarshalBinary error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestGCWarmSnapshotPreservesLocalTableRootsGrowthCyclesAndSharing(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), gcTableSnapshotModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, tc := range []struct {
		name string
		gc   GCConfig
	}{
		{name: "throughput", gc: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}},
		{name: "tiny", gc: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, TinyStepBudget: 1, VerifyAfterCollect: true, StressBarriers: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := Capture(compiled, SnapshotOptions{Kind: SnapshotWarm, WarmFunc: "warm", WarmArgs: []uint64{0x12345678}, GC: tc.gc})
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.gcGlobalRefs) != 0 || len(snapshot.gcObjects) != 1 || len(snapshot.gcTableRefs) != 2 || snapshot.gcTableRefs[0] != snapshot.gcTableRefs[1] {
				t.Fatalf("captured GC globals/table/objects = %v/%v/%d", snapshot.gcGlobalRefs, snapshot.gcTableRefs, len(snapshot.gcObjects))
			}
			blob, err := snapshot.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			repeated, err := Capture(compiled, SnapshotOptions{Kind: SnapshotWarm, WarmFunc: "warm", WarmArgs: []uint64{0x12345678}, GC: tc.gc})
			if err != nil {
				t.Fatal(err)
			}
			repeatedBlob, err := repeated.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(blob, repeatedBlob) {
				t.Fatal("repeated table-root capture was not deterministic")
			}
			if len(blob) < 5 || blob[4] != 5 {
				t.Fatalf("single-table snapshot version = %v, want 5", blob[:5])
			}
			loaded, err := LoadSnapshot(blob)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := LoadSnapshot(append(append([]byte(nil), blob...), 0)); err == nil || !strings.Contains(err.Error(), "trailing snapshot bytes") {
				t.Fatalf("trailing snapshot byte error = %v", err)
			}
			defer loaded.Module().Close()
			for _, source := range []Instantiable{snapshot, loaded} {
				in, err := Instantiate(source)
				if err != nil {
					t.Fatal(err)
				}
				for name, want := range map[string][]uint64{
					"size":  {2},
					"same":  {1},
					"churn": {0x12345678},
				} {
					got, callErr := in.Invoke(name)
					if callErr != nil || !reflect.DeepEqual(got, want) {
						in.Close()
						t.Fatalf("restored %s = %v, %v; want %v", name, got, callErr, want)
					}
				}
				for i := uint64(0); i < 2; i++ {
					got, callErr := in.Invoke("get", i)
					if callErr != nil || !reflect.DeepEqual(got, []uint64{0x12345678}) {
						in.Close()
						t.Fatalf("restored table slot %d = %v, %v", i, got, callErr)
					}
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestGCInitSnapshotPreservesLocalTableLengthAndNullRoot(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), gcTableSnapshotModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	snapshot, err := Capture(compiled, SnapshotOptions{Kind: SnapshotInit})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.gcTableRefs) != 1 || snapshot.gcTableRefs[0] != (gcSnapshotRef{}) || len(snapshot.gcObjects) != 0 {
		t.Fatalf("init table roots/objects = %v/%d", snapshot.gcTableRefs, len(snapshot.gcObjects))
	}
	blob, err := snapshot.MarshalBinary()
	if err != nil {
		t.Fatal(err)
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
		got, callErr := in.Invoke("size")
		if callErr != nil || !reflect.DeepEqual(got, []uint64{1}) {
			in.Close()
			t.Fatalf("restored init table size = %v, %v", got, callErr)
		}
		desc := in.tableDescriptor(0)
		if len(desc) < 16 || desc[8] != 0 || desc[9] != 0 || desc[10] != 0 || desc[11] != 0 {
			in.Close()
			t.Fatalf("restored init table descriptor = %v", desc)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGCTableSnapshotRestoreFailureRollsBackRuntimeDomain(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), gcTableSnapshotModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	base, err := Capture(compiled, SnapshotOptions{Kind: SnapshotWarm, WarmFunc: "warm", WarmArgs: []uint64{1}})
	if err != nil {
		t.Fatal(err)
	}
	candidate := *base
	candidate.gcGlobalRefs = append([]gcSnapshotRef(nil), base.gcGlobalRefs...)
	candidate.gcTableRefs = append([]gcSnapshotRef(nil), base.gcTableRefs...)
	candidate.gcObjects = make([]gcObjectSnapshot, 16)
	for i := range candidate.gcObjects {
		candidate.gcObjects[i] = base.gcObjects[0]
		candidate.gcObjects[i].values = append([]gcSnapshotValue(nil), base.gcObjects[0].values...)
		if i+1 < len(candidate.gcObjects) {
			candidate.gcObjects[i].values[0].ref = gcSnapshotRef{kind: gcSnapshotRefObject, value: uint32(i + 2)}
		} else {
			candidate.gcObjects[i].values[0].ref = gcSnapshotRef{}
		}
		candidate.gcObjects[i].values[1].bits = uint64(i + 1)
	}
	candidate.gcTableRefs[0] = gcSnapshotRef{kind: gcSnapshotRefObject, value: 1}
	candidate.gcTableRefs[1] = candidate.gcTableRefs[0]
	if _, err := candidate.MarshalBinary(); err != nil {
		t.Fatalf("expanded snapshot validation: %v", err)
	}

	store := newReferenceStore(false)
	defer store.closeRuntime()
	tiny := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, TinyStepBudget: 1, VerifyAfterCollect: true, StressBarriers: true}
	for attempt := 0; attempt < 3; attempt++ {
		if in, err := instantiateCore(compiled, InstantiateOptions{GC: tiny, store: store, restore: &candidate}); err == nil {
			in.Close()
			t.Fatalf("near-capacity restore attempt %d unexpectedly succeeded", attempt)
		} else if !strings.Contains(err.Error(), "allocate object") {
			t.Fatalf("near-capacity restore attempt %d error = %v", attempt, err)
		}
		store.mu.Lock()
		liveInstances, liveObjects, domains := store.liveInstances, store.liveObjects, store.gcDomains
		store.mu.Unlock()
		if liveInstances != 0 || liveObjects != 0 || domains != nil {
			t.Fatalf("failed restore attempt %d leaked store state: instances=%d objects=%d domains=%p", attempt, liveInstances, liveObjects, domains)
		}
	}

	in, err := instantiateCore(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 2048, TinyBlockBytes: 16, VerifyAfterCollect: true, StressBarriers: true}, store: store, restore: &candidate})
	if err != nil {
		t.Fatalf("restore after rollback: %v", err)
	}
	got, callErr := in.Invoke("churn")
	if callErr != nil || !reflect.DeepEqual(got, []uint64{1}) {
		in.Close()
		t.Fatalf("restored graph after rollback = %v, %v", got, callErr)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGCWarmSnapshotRejectsMalformedLocalTableRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), gcTableSnapshotModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	valid, err := Capture(compiled, SnapshotOptions{Kind: SnapshotWarm, WarmFunc: "warm", WarmArgs: []uint64{7}})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func(*Snapshot)
		want string
	}{
		{name: "below minimum", edit: func(s *Snapshot) { s.gcTableRefs = nil }, want: "below declared minimum"},
		{name: "above capacity", edit: func(s *Snapshot) { s.gcTableRefs = append(s.gcTableRefs, gcSnapshotRef{}, gcSnapshotRef{}) }, want: "exceeds runtime capacity"},
		{name: "wrong root kind", edit: func(s *Snapshot) { s.gcTableRefs[0].kind = 0xff }, want: "unknown GC reference kind"},
		{name: "wrong dynamic type", edit: func(s *Snapshot) { s.gcTableRefs[0] = gcSnapshotRef{kind: gcSnapshotRefI31, value: 1} }, want: "does not match declared structural type"},
		{name: "unreachable object", edit: func(s *Snapshot) { s.gcTableRefs[0] = gcSnapshotRef{}; s.gcTableRefs[1] = gcSnapshotRef{} }, want: "unreachable from snapshot roots"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := *valid
			candidate.gcGlobalRefs = append([]gcSnapshotRef(nil), valid.gcGlobalRefs...)
			candidate.gcTableRefs = append([]gcSnapshotRef(nil), valid.gcTableRefs...)
			candidate.gcObjects = append([]gcObjectSnapshot(nil), valid.gcObjects...)
			tc.edit(&candidate)
			if _, err := candidate.MarshalBinary(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("MarshalBinary error = %v, want %q", err, tc.want)
			}
		})
	}
}
