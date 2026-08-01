//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func TestDomainSnapshotRestoresSharedGCGraphAndAliases(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerCode, err := Compile(cfg, gcCrossInstancePersistentProviderModule())
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, gcCrossInstancePersistentConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()

	store := newReferenceStore(false)
	gcConfig := GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}
	provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: store})
	if err != nil {
		t.Fatal(err)
	}
	retain, _ := provider.ExportedFunc("retain")
	table, _ := provider.ExportedTable("t")
	global, _ := provider.ExportedGlobalObject("g")
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: Imports{"provider.retain": retain, "provider.t": table, "provider.g": global}})
	if err != nil {
		provider.Close()
		t.Fatal(err)
	}
	if got, err := consumer.Invoke("run", 20); err != nil || !reflect.DeepEqual(got, []uint64{41}) {
		consumer.Close()
		provider.Close()
		t.Fatalf("warm run = %v, %v", got, err)
	}

	snapshot, err := CaptureDomain(provider, consumer)
	if err != nil {
		consumer.Close()
		provider.Close()
		t.Fatal(err)
	}
	blob, err := snapshot.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	again, err := snapshot.MarshalBinary()
	if err != nil || !bytes.Equal(blob, again) {
		t.Fatalf("domain snapshot encoding is nondeterministic: equal=%v err=%v", bytes.Equal(blob, again), err)
	}
	loaded, err := LoadDomainSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	corrupt, err := LoadDomainSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	corrupt.members[1].globalRefs[0] = gcSnapshotRef{}
	if _, err := corrupt.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "alias state") {
		t.Fatalf("corrupt global alias error = %v", err)
	}
	for _, cut := range []int{0, 1, len(domainSnapshotMagic), len(blob) / 4, len(blob) / 2, len(blob) - 1} {
		if cut < 0 || cut >= len(blob) {
			continue
		}
		if _, err := LoadDomainSnapshot(blob[:cut]); err == nil {
			t.Fatalf("truncated domain snapshot length %d accepted", cut)
		}
	}
	if !reflect.DeepEqual(loaded.gc, gcConfig) {
		t.Fatalf("loaded GC config = %+v, want %+v", loaded.gc, gcConfig)
	}
	path := t.TempDir() + "/domain.wgsn"
	if err := snapshot.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	fromFile, err := ReadDomainSnapshotFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fileBlob, err := fromFile.MarshalBinary()
	if err != nil || !bytes.Equal(fileBlob, blob) {
		t.Fatalf("domain snapshot file round trip equal=%v err=%v", bytes.Equal(fileBlob, blob), err)
	}
	snapshot = loaded
	if _, err := LoadDomainSnapshot(append(append([]byte(nil), blob...), 0)); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing domain snapshot error = %v", err)
	}
	badConfig := append([]byte(nil), blob...)
	badConfig[len(domainSnapshotMagic)+1+3+10*4] = 2
	if _, err := LoadDomainSnapshot(badConfig); err == nil || !strings.Contains(err.Error(), "boolean") {
		t.Fatalf("invalid domain GC config error = %v", err)
	}
	if _, err := CaptureDomain(provider); err == nil || !strings.Contains(err.Error(), "incomplete") {
		consumer.Close()
		provider.Close()
		t.Fatalf("partial capture error = %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	store.closeRuntime()

	failing, err := LoadDomainSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	failing.gc = GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 16}
	failedRuntime := NewRuntime()
	if instances, err := failing.Instantiate(failedRuntime); err == nil || instances != nil {
		t.Fatalf("allocation-failed domain restore = %v, %v", instances, err)
	}
	failedRuntime.refStore.mu.Lock()
	failedLive := failedRuntime.refStore.liveInstances
	failedDomains := failedRuntime.refStore.gcDomains
	failedRuntime.refStore.mu.Unlock()
	if failedLive != 0 || failedDomains != nil {
		t.Fatalf("allocation-failed restore published live=%d domains=%p", failedLive, failedDomains)
	}
	_ = failedRuntime.Close()

	occupied := NewRuntime()
	occupiedInstance, err := instantiateCore(providerCode, InstantiateOptions{store: occupied.refStore, runtime: occupied})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Instantiate(occupied); err == nil || !strings.Contains(err.Error(), "without a live GC domain") {
		t.Fatalf("occupied Runtime restore error = %v", err)
	}
	_ = occupiedInstance.Close()
	_ = occupied.Close()

	rt := NewRuntime()
	defer rt.Close()
	restored, err := snapshot.Instantiate(rt)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 2 || restored[0].gc == nil || restored[0].gc != restored[1].gc {
		t.Fatalf("restored domain = %#v", restored)
	}
	defer restored[1].Close()
	defer restored[0].Close()
	if restored[0].globalCells[0] != restored[1].globalCells[0] {
		t.Fatal("restored GC global alias identity was not preserved")
	}
	providerTable := restored[0].tableDescriptor(0)
	consumerTable := restored[1].tableDescriptor(0)
	if len(providerTable) == 0 || len(consumerTable) == 0 || &providerTable[0] != &consumerTable[0] {
		t.Fatal("restored GC table alias identity was not preserved")
	}
	globalRef := gcRefFromCell(t, restored[0].globalCells[0])
	globalValue, err := restored[0].gc.StructGet(globalRef, 0)
	if err != nil || globalValue.Bits != 20 {
		t.Fatalf("restored global object = %+v, %v", globalValue, err)
	}
	tableRef := gcRefFromBits(t, binary.LittleEndian.Uint64(providerTable[8:]))
	tableValue, err := restored[0].gc.StructGet(tableRef, 0)
	if err != nil || tableValue.Bits != 21 {
		t.Fatalf("restored table object = %+v, %v", tableValue, err)
	}
	if got, err := restored[1].Invoke("run", 31); err != nil || !reflect.DeepEqual(got, []uint64{63}) {
		t.Fatalf("restored run = %v, %v", got, err)
	}
}

func gcRefFromCell(t *testing.T, global *Global) gc.Ref {
	t.Helper()
	return gcRefFromBits(t, readGlobalObject(global, global.Type))
}

func gcRefFromBits(t *testing.T, bits uint64) gc.Ref {
	t.Helper()
	ref := gc.Ref(uint32(bits))
	if bits != uint64(ref) || !ref.IsObj() {
		t.Fatalf("invalid compact GC reference %#x", bits)
	}
	return ref
}

func TestDomainSnapshotRejectsLiveTokensAndRestoresTransactionally(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGenericPublicTokenModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	store := newReferenceStore(false)
	in, err := instantiateCore(compiled, InstantiateOptions{store: store})
	if err != nil {
		t.Fatal(err)
	}
	values, err := in.Call(t.Context(), "new_struct")
	if err != nil {
		t.Fatal(err)
	}
	token := values[0].GCRef()
	if _, err := CaptureDomain(in); err == nil || !strings.Contains(err.Error(), "live public GC reference tokens") {
		t.Fatalf("live-token capture error = %v", err)
	}
	if err := in.ReleaseGCRef(token); err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureDomain(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	store.closeRuntime()

	snapshot.members[0].imports = append(snapshot.members[0].imports, domainSnapshotImport{key: "bad", kind: domainImportFunction, member: 1})
	rt := NewRuntime()
	defer rt.Close()
	if restored, err := snapshot.Instantiate(rt); err == nil || restored != nil {
		t.Fatalf("malformed domain restore = %v, %v", restored, err)
	}
	rt.refStore.mu.Lock()
	live := rt.refStore.liveInstances
	rt.refStore.mu.Unlock()
	if live != 0 {
		t.Fatalf("failed domain restore published %d live instances", live)
	}
}
