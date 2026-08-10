//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
	"github.com/wago-org/wago/tests/wasmtest"
)

func domainSnapshotConfig() *RuntimeConfig {
	return NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit)
}

func TestDomainSnapshotV4Footprint(t *testing.T) {
	if got := unsafe.Sizeof(DomainSnapshot{}); got != 120 {
		t.Fatalf("DomainSnapshot size = %d, want 120", got)
	}
	if got := unsafe.Sizeof(domainSnapshotMember{}); got != 104 {
		t.Fatalf("domainSnapshotMember size = %d, want 104", got)
	}
}

func domainSnapshotLocalEHModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	tagType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	tag := []byte{0x00, 0x01}
	body := []byte{0x02, 0x01, 0x63, 0x00, 0x01, 0x7f,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0x02, 0x7f,
		0x1f, 0x40, 0x01, 0x00, 0x00, 0x00,
		0x41, 0x07, 0x08, 0x00,
		0x0b, 0x41, 0x00, 0x0b, 0x1a,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, tagType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(13, wasmtest.Vec(tag)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestDomainSnapshotRestoresLocalExceptionHandling(t *testing.T) {
	compiled, err := Compile(domainSnapshotConfig(), domainSnapshotLocalEHModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	store := newReferenceStore(false)
	profile := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}
	in, err := instantiateCore(compiled, InstantiateOptions{GC: profile, store: store})
	if err != nil {
		store.closeRuntime()
		t.Fatal(err)
	}
	if got, err := in.Invoke("run", 42); err != nil || !reflect.DeepEqual(got, []uint64{42}) {
		in.Close()
		store.closeRuntime()
		t.Fatalf("pre-snapshot EH run = %v, %v", got, err)
	}
	snapshot, err := CaptureDomain(in)
	if err != nil {
		in.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	blob, err := snapshot.MarshalBinary()
	if err != nil {
		in.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	store.closeRuntime()
	loaded, err := LoadDomainSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()
	defer rt.Close()
	restored, err := loaded.Instantiate(rt)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 {
		t.Fatalf("restored EH members = %d, want 1", len(restored))
	}
	defer restored[0].Close()
	if got, err := restored[0].Invoke("run", 77); err != nil || !reflect.DeepEqual(got, []uint64{77}) {
		t.Fatalf("restored EH run = %v, %v", got, err)
	}
}

func snapshotMemory64Module(t testing.TB) []byte {
	t.Helper()
	const moduleHex = "0061736d01000000010d0360000060017e017f6000017e030403000102050401050102071603047761726d0000047265616400010473697a6500020a23031400420140001a4284800441f8acd191013602000b070020002802000b04003f000b"
	module, err := hex.DecodeString(moduleHex)
	if err != nil {
		t.Fatal(err)
	}
	return module
}

func TestMemory64SnapshotRoundTrip(t *testing.T) {
	compiled, err := Compile(domainSnapshotConfig(), snapshotMemory64Module(t))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	snapshot, err := Capture(compiled, SnapshotOptions{Kind: SnapshotWarm, WarmFunc: "warm"})
	if err != nil {
		t.Fatal(err)
	}
	corrupt := *snapshot
	corrupt.memories = append([]memorySnap(nil), snapshot.memories...)
	corrupt.memories[0].pages = 3
	if _, err := corrupt.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "exceeds declared maximum") {
		t.Fatalf("memory64 oversized snapshot error = %v", err)
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
	if !loaded.Module().memoryDef(0).Addr64 {
		t.Fatal("snapshot codec lost memory64 address form")
	}
	for name, source := range map[string]Instantiable{"memory": snapshot, "blob": loaded} {
		t.Run(name, func(t *testing.T) {
			in, err := Instantiate(source)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("size"); err != nil || !reflect.DeepEqual(got, []uint64{2}) {
				t.Fatalf("restored memory64 size = %v, %v", got, err)
			}
			if got, err := in.Invoke("read", uint64(65540)); err != nil || !reflect.DeepEqual(got, []uint64{0x12345678}) {
				t.Fatalf("restored memory64 read = %v, %v", got, err)
			}
		})
	}
}

func BenchmarkMemory64SnapshotInstantiate(b *testing.B) {
	compiled, err := Compile(domainSnapshotConfig(), snapshotMemory64Module(b))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	snapshot, err := Capture(compiled, SnapshotOptions{Kind: SnapshotWarm, WarmFunc: "warm"})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := Instantiate(snapshot)
		if err != nil {
			b.Fatal(err)
		}
		if err := in.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestDomainSnapshotRestoresInternalMemory64Alias(t *testing.T) {
	cfg := domainSnapshotConfig()
	providerCode, err := Compile(cfg, domainSnapshotMemoryProviderModuleWithAddr64(true))
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, domainSnapshotMemoryConsumerModuleWithAddr64(true))
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	store := newReferenceStore(false)
	profile := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}
	provider, err := instantiateCore(providerCode, InstantiateOptions{GC: profile, store: store})
	if err != nil {
		store.closeRuntime()
		t.Fatal(err)
	}
	retain, err := provider.ExportedFunc("retain")
	if err != nil {
		provider.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	table, err := provider.ExportedTable("t")
	if err != nil {
		provider.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	memory, err := provider.ExportedMemory("mem")
	if err != nil {
		provider.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	global, err := provider.ExportedGlobalObject("g")
	if err != nil {
		provider.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	tag, err := provider.ExportedTag("tag")
	if err != nil {
		provider.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: profile, store: store, Imports: Imports{"provider.retain": retain, "provider.t": table, "provider.mem": memory, "provider.g": global, "provider.tag": tag}})
	if err != nil {
		provider.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	copy(memory.Bytes()[19:27], "memory64")
	snapshot, err := CaptureDomain(consumer, provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.marshalBinaryVersion(2); err == nil || !strings.Contains(err.Error(), "memory64 state") {
		t.Fatalf("WGDN v2 memory64 downgrade error = %v", err)
	}
	corrupt := *snapshot
	corrupt.members = append([]domainSnapshotMember(nil), snapshot.members...)
	state := *corrupt.members[0].state
	state.memories = append([]memorySnap(nil), state.memories...)
	state.memories[0].image = append([]byte(nil), state.memories[0].image...)
	state.memories[0].image[19] ^= 1
	corrupt.members[0].state = &state
	if _, err := corrupt.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "memory import") || !strings.Contains(err.Error(), "alias state") {
		t.Fatalf("corrupt memory64 alias error = %v", err)
	}
	blob, err := snapshot.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	store.closeRuntime()
	loaded, err := LoadDomainSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()
	defer rt.Close()
	restored, err := loaded.Instantiate(rt)
	if err != nil {
		t.Fatal(err)
	}
	defer restored[0].Close()
	defer restored[1].Close()
	consumer, provider = restored[0], restored[1]
	consumerMemory, _ := consumer.instanceMemoryAt(0)
	providerMemory, _ := provider.instanceMemoryAt(0)
	pages := 0
	if providerMemory != nil {
		pages = len(providerMemory.Bytes()) / 65536
	}
	if consumerMemory == nil || consumerMemory != providerMemory || pages != 1 || string(providerMemory.Bytes()[19:27]) != "memory64" {
		t.Fatalf("restored memory64 alias/pages/image = %p/%p/%d/%q", consumerMemory, providerMemory, pages, providerMemory.Bytes()[19:27])
	}
}

func domainSnapshotMemoryProviderModule() []byte {
	return domainSnapshotMemoryProviderModuleWithAddr64(false)
}

func domainSnapshotMemoryProviderModuleWithAddr64(addr64 bool) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	refCallType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x63, 0x00}
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	tagType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	tableType := []byte{0x63, 0x00, 0x01, 0x01, 0x02}
	global := []byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b}
	body := []byte{0x01, 0x01, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b,
		0x20, 0x00, 0x0b}
	memoryType := []byte{0x01, 0x01, 0x02}
	if addr64 {
		memoryType = []byte{0x05, 0x01, 0x02}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, refCallType, runType, tagType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec(tableType)),
		wasmtest.Section(5, wasmtest.Vec(memoryType)),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x03})),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("retain", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("t", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("mem", byte(wasm.ExternMem), 0),
			wasmtest.ExportEntry("g", byte(wasm.ExternGlobal), 0),
			wasmtest.ExportEntry("tag", byte(wasm.ExternTag), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func domainSnapshotMemoryConsumerModule() []byte {
	return domainSnapshotMemoryConsumerModuleWithAddr64(false)
}

func domainSnapshotMemoryConsumerModuleWithAddr64(addr64 bool) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	refCallType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x63, 0x00}
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	tagType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	tableType := []byte{0x63, 0x00, 0x01, 0x01, 0x02}
	funcImport := append(append(wasmtest.Name("provider"), wasmtest.Name("retain")...), byte(wasm.ExternFunc))
	funcImport = append(funcImport, wasmtest.ULEB(1)...)
	tableImport := append(append(wasmtest.Name("provider"), wasmtest.Name("t")...), byte(wasm.ExternTable))
	tableImport = append(tableImport, tableType...)
	memoryType := []byte{0x01, 0x01, 0x02}
	if addr64 {
		memoryType = []byte{0x05, 0x01, 0x02}
	}
	memoryImport := append(append(wasmtest.Name("provider"), wasmtest.Name("mem")...), byte(wasm.ExternMem))
	memoryImport = append(memoryImport, memoryType...)
	globalImport := append(append(wasmtest.Name("provider"), wasmtest.Name("g")...), byte(wasm.ExternGlobal), 0x63, 0x00, 0x01)
	tagImport := append(append(wasmtest.Name("provider"), wasmtest.Name("tag")...), byte(wasm.ExternTag), 0x00, 0x03)
	body := []byte{0x01, 0x01, 0x63, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x24, 0x00,
		0x41, 0x00,
		0x20, 0x00, 0x41, 0x01, 0x6a, 0xfb, 0x00, 0x00,
		0x22, 0x01, 0x26, 0x00,
		0x20, 0x01, 0x10, 0x00, 0x1a,
		0xd0, 0x00, 0x21, 0x01,
		0x41, 0x09, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x41, 0x00, 0x25, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x6a, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, refCallType, runType, tagType)),
		wasmtest.Section(2, wasmtest.Vec(funcImport, tableImport, memoryImport, globalImport, tagImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("tag", byte(wasm.ExternTag), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestDomainSnapshotRestoresInternalMemoryAlias(t *testing.T) {
	cfg := domainSnapshotConfig()
	providerCode, err := Compile(cfg, domainSnapshotMemoryProviderModule())
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, domainSnapshotMemoryConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	if providerCode.genericGCFrameRoots() == nil || consumerCode.genericGCFrameRoots() == nil {
		t.Fatal("memory-linked domain modules lost exact roots")
	}
	store := newReferenceStore(false)
	profile := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}
	provider, err := instantiateCore(providerCode, InstantiateOptions{GC: profile, store: store})
	if err != nil {
		store.closeRuntime()
		t.Fatal(err)
	}
	retain, _ := provider.ExportedFunc("retain")
	table, _ := provider.ExportedTable("t")
	memory, err := provider.ExportedMemory("mem")
	if err != nil {
		provider.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	global, _ := provider.ExportedGlobalObject("g")
	tag, err := provider.ExportedTag("tag")
	if err != nil {
		provider.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	copy(memory.Bytes()[19:25], "domain")
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: profile, store: store, Imports: Imports{"provider.retain": retain, "provider.t": table, "provider.mem": memory, "provider.g": global, "provider.tag": tag}})
	if err != nil {
		provider.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	snapshot, err := CaptureDomain(consumer, provider)
	if err != nil {
		consumer.Close()
		provider.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	corrupt := *snapshot
	corrupt.members = append([]domainSnapshotMember(nil), snapshot.members...)
	state := *corrupt.members[0].state
	state.memories = append([]memorySnap(nil), state.memories...)
	state.memories[0].image = append([]byte(nil), state.memories[0].image...)
	state.memories[0].image[19] ^= 1
	corrupt.members[0].state = &state
	if _, err := corrupt.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "memory import") || !strings.Contains(err.Error(), "alias state") {
		t.Fatalf("corrupt memory alias error = %v", err)
	}
	blob, err := snapshot.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	store.closeRuntime()
	loaded, err := LoadDomainSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()
	defer rt.Close()
	restored, err := loaded.Instantiate(rt)
	if err != nil {
		t.Fatal(err)
	}
	defer restored[0].Close()
	defer restored[1].Close()
	if restored[0].memory != restored[1].memory {
		t.Fatal("restored imported memory does not alias its domain owner")
	}
	consumerTag, err := restored[0].ExportedTag("tag")
	if err != nil {
		t.Fatal(err)
	}
	providerTag, err := restored[1].ExportedTag("tag")
	if err != nil {
		t.Fatal(err)
	}
	if consumerTag != providerTag {
		t.Fatal("restored imported exception tag does not alias its domain owner")
	}
	if got := string(restored[0].memory.Bytes()[19:25]); got != "domain" {
		t.Fatalf("restored shared memory bytes = %q, want domain", got)
	}
	if got, err := restored[0].Invoke("run", 12); err != nil || !reflect.DeepEqual(got, []uint64{25}) {
		t.Fatalf("restored memory consumer run = %v, %v", got, err)
	}
}

func domainSnapshotDroppedElementModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x01, 0x01, 0x63, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0xfc, 0x0d, 0x00,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00,
		0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, wasmtest.Vec(tableTestPassiveElem(0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestDomainSnapshotRestoresLiveAndDroppedPassiveElements(t *testing.T) {
	compiled, err := Compile(domainSnapshotConfig(), domainSnapshotDroppedElementModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.genericGCFrameRoots() == nil {
		t.Fatal("dropped-element module lost exact roots")
	}
	profile := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}
	store := newReferenceStore(false)
	in, err := instantiateCore(compiled, InstantiateOptions{GC: profile, store: store})
	if err != nil {
		store.closeRuntime()
		t.Fatal(err)
	}
	if lens := capturePassiveElemLens(in); !reflect.DeepEqual(lens, []uint32{1}) {
		in.Close()
		store.closeRuntime()
		t.Fatalf("initial passive element lengths = %v", lens)
	}
	liveSnapshot, err := CaptureDomain(in)
	if err != nil {
		in.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	legacyV2, err := liveSnapshot.marshalBinaryVersion(2)
	if err != nil {
		t.Fatal(err)
	}
	legacyLoaded, err := LoadDomainSnapshot(legacyV2)
	if err != nil {
		t.Fatalf("load WGDN v2 passive element state: %v", err)
	}
	if got := snapshotPassiveElemLens(legacyLoaded.members[0].state); !reflect.DeepEqual(got, []uint32{1}) {
		t.Fatalf("WGDN v2 passive element lengths = %v", got)
	}
	liveBlob, err := liveSnapshot.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	liveLoaded, err := LoadDomainSnapshot(liveBlob)
	if err != nil {
		t.Fatal(err)
	}
	liveRT := NewRuntime()
	liveRestored, err := liveLoaded.Instantiate(liveRT)
	if err != nil {
		liveRT.Close()
		t.Fatal(err)
	}
	if lens := capturePassiveElemLens(liveRestored[0]); !reflect.DeepEqual(lens, []uint32{1}) {
		liveRestored[0].Close()
		liveRT.Close()
		t.Fatalf("restored live passive element lengths = %v", lens)
	}
	if got, err := liveRestored[0].Invoke("run", 44); err != nil || !reflect.DeepEqual(got, []uint64{44}) {
		liveRestored[0].Close()
		liveRT.Close()
		t.Fatalf("restored live-element run = %v, %v", got, err)
	}
	if lens := capturePassiveElemLens(liveRestored[0]); !reflect.DeepEqual(lens, []uint32{0}) {
		liveRestored[0].Close()
		liveRT.Close()
		t.Fatalf("restored live element did not drop: %v", lens)
	}
	if err := liveRestored[0].Close(); err != nil {
		t.Fatal(err)
	}
	if err := liveRT.Close(); err != nil {
		t.Fatal(err)
	}

	if got, err := in.Invoke("run", 55); err != nil || !reflect.DeepEqual(got, []uint64{55}) {
		in.Close()
		store.closeRuntime()
		t.Fatalf("drop run = %v, %v", got, err)
	}
	if lens := capturePassiveElemLens(in); !reflect.DeepEqual(lens, []uint32{0}) {
		in.Close()
		store.closeRuntime()
		t.Fatalf("captured passive element lengths = %v", lens)
	}
	snapshot, err := CaptureDomain(in)
	if err != nil {
		in.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	blob, err := snapshot.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if blob[len(domainSnapshotMagic)] != domainSnapshotVersion {
		t.Fatalf("domain snapshot version = %d, want %d", blob[len(domainSnapshotMagic)], domainSnapshotVersion)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	store.closeRuntime()
	loaded, err := LoadDomainSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()
	defer rt.Close()
	restored, err := loaded.Instantiate(rt)
	if err != nil {
		t.Fatal(err)
	}
	defer restored[0].Close()
	if lens := capturePassiveElemLens(restored[0]); !reflect.DeepEqual(lens, []uint32{0}) {
		t.Fatalf("restored passive element lengths = %v", lens)
	}
	if got, err := restored[0].Invoke("run", 77); err != nil || !reflect.DeepEqual(got, []uint64{77}) {
		t.Fatalf("restored drop run = %v, %v", got, err)
	}
}

func domainSnapshotGlobalElementModule(t testing.TB, object bool) []byte {
	t.Helper()
	moduleHex := "0061736d010000000108025e6c016000017f03020101060a01646c0041db00fb1c0b0708010472656164000009080105646c0123000b0a1301110041004101fb0a00004100fb0b00fb1d0b0017046e616d65040401000161070401000167080401000165"
	if object {
		moduleHex = "0061736d01000000010d035f017f005e6300016000017f0303020202060b0164000041cd00fb00000b07130301670300047265616400000473616d6500010908010564000123000b0a2d02180101640141004101fb0a010022004100fb0b01fb0200000b120041004101fb0a01004100fb0b012300d30b0022046e616d650206010001000161040702000173010161070401000167080401000165"
	}
	module, err := hex.DecodeString(moduleHex)
	if err != nil {
		t.Fatal(err)
	}
	return module
}

func domainSnapshotImportedGlobalElementModules(t testing.TB) (provider, consumer []byte) {
	t.Helper()
	const providerHex = "0061736d01000000010e035f017f006000017f60000164000303020102060b0164000041cd00fb00000b0713030167030004726561640000046d616b6500010a120208002300fb0200000b07004101fb00000b000b046e616d65040401000173"
	const consumerHex = "0061736d01000000010d035f017f005e6300016000017f0210010870726f76696465720167036400000303020202070f02047265616400000473616d6500010908010564000123000b0a2802130041004101fb0a01004100fb0b01fb0200000b120041004101fb0a01004100fb0b012300d30b0014046e616d65040702000173010161080401000165"
	var err error
	provider, err = hex.DecodeString(providerHex)
	if err != nil {
		t.Fatal(err)
	}
	consumer, err = hex.DecodeString(consumerHex)
	if err != nil {
		t.Fatal(err)
	}
	return provider, consumer
}

func TestDomainSnapshotRestoresGlobalDependentPassiveElements(t *testing.T) {
	for _, tc := range []struct {
		name   string
		object bool
		want   uint64
	}{
		{name: "i31", want: 91},
		{name: "object", object: true, want: 77},
	} {
		for _, profile := range []GCProfile{GCProfileTiny, GCProfileThroughput} {
			profileName := map[GCProfile]string{GCProfileTiny: "tiny", GCProfileThroughput: "throughput"}[profile]
			t.Run(tc.name+"/"+profileName, func(t *testing.T) {
				compiled, err := Compile(domainSnapshotConfig(), domainSnapshotGlobalElementModule(t, tc.object))
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				store := newReferenceStore(false)
				gcConfig := GCConfig{Profile: profile, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}
				in, err := instantiateCore(compiled, InstantiateOptions{GC: gcConfig, store: store})
				if err != nil {
					store.closeRuntime()
					t.Fatal(err)
				}
				if got, err := in.Invoke("read"); err != nil || !reflect.DeepEqual(got, []uint64{tc.want}) {
					t.Fatalf("pre-capture global element read = %v, %v", got, err)
				}
				if tc.object {
					if got, err := in.Invoke("same"); err != nil || !reflect.DeepEqual(got, []uint64{1}) {
						t.Fatalf("pre-capture global element identity = %v, %v", got, err)
					}
				}
				snapshot, err := CaptureDomain(in)
				if err != nil {
					t.Fatal(err)
				}
				if len(snapshot.members[0].elementRoots) != 1 || len(snapshot.members[0].elementRoots[0]) != 1 {
					t.Fatalf("captured passive roots = %#v", snapshot.members[0].elementRoots)
				}
				if _, err := snapshot.marshalBinaryVersion(2); err == nil || !strings.Contains(err.Error(), "passive element roots") {
					t.Fatalf("WGDN v2 global-element downgrade error = %v", err)
				}
				corrupt := *snapshot
				corrupt.members = append([]domainSnapshotMember(nil), snapshot.members...)
				corrupt.members[0].elementRoots = [][]gcSnapshotRef{{}}
				if _, err := corrupt.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "root length mismatch") {
					t.Fatalf("malformed passive root error = %v", err)
				}
				blob, err := snapshot.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
				store.closeRuntime()
				loaded, err := LoadDomainSnapshot(blob)
				if err != nil {
					t.Fatal(err)
				}
				rt := NewRuntime()
				restored, err := loaded.Instantiate(rt)
				if err != nil {
					rt.Close()
					t.Fatal(err)
				}
				if got, err := restored[0].Invoke("read"); err != nil || !reflect.DeepEqual(got, []uint64{tc.want}) {
					t.Fatalf("restored global element read = %v, %v", got, err)
				}
				if tc.object {
					if got, err := restored[0].Invoke("same"); err != nil || !reflect.DeepEqual(got, []uint64{1}) {
						t.Fatalf("restored global element identity = %v, %v", got, err)
					}
				}
				if err := restored[0].Close(); err != nil {
					t.Fatal(err)
				}
				if err := rt.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestDomainSnapshotRestoresImportedGlobalDependentPassiveElement(t *testing.T) {
	providerModule, consumerModule := domainSnapshotImportedGlobalElementModules(t)
	cfg := domainSnapshotConfig()
	providerCode, err := Compile(cfg, providerModule)
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, consumerModule)
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	if consumerCode.genericGCFrameRoots() == nil {
		t.Fatal("imported global element consumer lost exact native roots")
	}
	store := newReferenceStore(false)
	gcConfig := GCConfig{Profile: GCProfileThroughput, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}
	provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: store})
	if err != nil {
		store.closeRuntime()
		t.Fatal(err)
	}
	global, err := provider.ExportedGlobalObject("g")
	if err != nil {
		provider.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: Imports{"provider.g": global}})
	if err != nil {
		provider.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	snapshot, err := CaptureDomain(consumer, provider)
	if err != nil {
		consumer.Close()
		provider.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	root := snapshot.members[0].elementRoots[0][0]
	if root != snapshot.members[0].globalRefs[0] || root != snapshot.members[1].globalRefs[0] {
		t.Fatalf("imported global passive identity = %+v / %+v / %+v", root, snapshot.members[0].globalRefs[0], snapshot.members[1].globalRefs[0])
	}
	blob, err := snapshot.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	store.closeRuntime()
	loaded, err := LoadDomainSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()
	defer rt.Close()
	restored, err := loaded.Instantiate(rt)
	if err != nil {
		t.Fatal(err)
	}
	defer restored[0].Close()
	defer restored[1].Close()
	if got, err := restored[0].Invoke("read"); err != nil || !reflect.DeepEqual(got, []uint64{77}) {
		t.Fatalf("restored imported global element read = %v, %v", got, err)
	}
	if got, err := restored[0].Invoke("same"); err != nil || !reflect.DeepEqual(got, []uint64{1}) {
		t.Fatalf("restored imported global element identity = %v, %v", got, err)
	}
}

func BenchmarkDomainSnapshotGlobalDependentElementInstantiate(b *testing.B) {
	compiled, err := Compile(domainSnapshotConfig(), domainSnapshotGlobalElementModule(b, true))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	store := newReferenceStore(false)
	in, err := instantiateCore(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileThroughput}, store: store})
	if err != nil {
		store.closeRuntime()
		b.Fatal(err)
	}
	snapshot, err := CaptureDomain(in)
	if err != nil {
		in.Close()
		store.closeRuntime()
		b.Fatal(err)
	}
	if err := in.Close(); err != nil {
		b.Fatal(err)
	}
	store.closeRuntime()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rt := NewRuntime()
		restored, err := snapshot.Instantiate(rt)
		if err != nil {
			rt.Close()
			b.Fatal(err)
		}
		if err := restored[0].Close(); err != nil {
			rt.Close()
			b.Fatal(err)
		}
		if err := rt.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestDomainSnapshotRestoresLivePassiveI31ElementPayload(t *testing.T) {
	const moduleHex = "0061736d01000000018980808000025e6c006000027f7f0382808080000101079b80808000011761727261792d6e65772d656c656d2d636f6e74656e74730000099c8080800001056c0441aa01fb1c0b41bb01fb1c0b41cc01fb1c0b41dd01fb1c0b0aa78080800001a1808080000101640041014102fb0a0000210020004100fb0b00fb1e20004101fb0b00fb1e0b"
	module, err := hex.DecodeString(moduleHex)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(domainSnapshotConfig(), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.genericGCFrameRoots() == nil {
		t.Fatalf("live i31 module lost exact roots: product=%s generic=%v", compiled.stagedGCArrayProduct(), compiled.usesGenericGCExecution())
	}
	store := newReferenceStore(false)
	profile := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}
	in, err := instantiateCore(compiled, InstantiateOptions{GC: profile, store: store})
	if err != nil {
		store.closeRuntime()
		t.Fatal(err)
	}
	if lens := capturePassiveElemLens(in); !reflect.DeepEqual(lens, []uint32{4}) {
		in.Close()
		store.closeRuntime()
		t.Fatalf("live i31 element lengths = %v", lens)
	}
	snapshot, err := CaptureDomain(in)
	if err != nil {
		in.Close()
		store.closeRuntime()
		t.Fatal(err)
	}
	blob, err := snapshot.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	store.closeRuntime()
	loaded, err := LoadDomainSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()
	defer rt.Close()
	restored, err := loaded.Instantiate(rt)
	if err != nil {
		t.Fatal(err)
	}
	defer restored[0].Close()
	got, err := restored[0].Invoke("array-new-elem-contents")
	if err != nil || !reflect.DeepEqual(got, []uint64{187, 204}) {
		t.Fatalf("restored live i31 element payload = %v, %v; want [187 204]", got, err)
	}
	if lens := capturePassiveElemLens(restored[0]); !reflect.DeepEqual(lens, []uint32{4}) {
		t.Fatalf("array.new_elem consumed passive segment: %v", lens)
	}
}

func TestDomainSnapshotLivePassiveElementAdmissionRejectsOpaqueOrMalformedValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		ref  ValType
		init RefInit
		want string
	}{
		{name: "externref", ref: ValExternRef, init: RefInit{FuncIndex: 1}, want: "opaque externref"},
		{name: "invalid i31", ref: ValI31Ref, init: RefInit{FuncIndex: 2}, want: "invalid i31"},
		{name: "foreign function", ref: ValFuncRef, init: RefInit{FuncIndex: 1}, want: "unavailable function"},
		{name: "reference global", ref: ValFuncRef, init: RefInit{HasGlobal: true}, want: "reference global"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := &Compiled{passiveElems: []ElemInit{{Mode: ElemModePassive, RefType: tc.ref, Values: []RefInit{tc.init}}}}
			if err := validateDomainPassiveElements(compiled, []uint32{1}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("live passive admission error = %v, want %q", err, tc.want)
			}
			if err := validateDomainPassiveElements(compiled, []uint32{0}); err != nil {
				t.Fatalf("dropped passive element rejected: %v", err)
			}
		})
	}
}

func TestDomainSnapshotRestoresSharedGCGraphAndAliases(t *testing.T) {
	cfg := domainSnapshotConfig()
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
	legacy, err := snapshot.marshalBinaryVersion(1)
	if err != nil {
		t.Fatal(err)
	}
	if legacy[len(domainSnapshotMagic)] != 1 {
		t.Fatalf("legacy domain snapshot version = %d, want 1", legacy[len(domainSnapshotMagic)])
	}
	legacyLoaded, err := LoadDomainSnapshot(legacy)
	if err != nil {
		t.Fatalf("load WGDN v1 compatibility blob: %v", err)
	}
	for _, member := range legacyLoaded.members {
		defer member.state.c.Close()
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
	badConfig[len(domainSnapshotMagic)+1+3+12*4] = 2
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
	compiled, err := Compile(domainSnapshotConfig(), gcGenericPublicTokenModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	store := newReferenceStore(false)
	in, err := instantiateCore(compiled, InstantiateOptions{store: store})
	if err != nil {
		t.Fatal(err)
	}
	values, err := in.Call(context.Background(), "new_struct")
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
