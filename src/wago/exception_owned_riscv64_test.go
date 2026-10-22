//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

var ownedExceptionTestPackets sync.Map // *Instance -> arena-backed native packet
func ownedExceptionPacketForTest(in *Instance) []byte {
	packet, ok := ownedExceptionTestPackets.Load(in)
	if !ok {
		panic("owned exception fixture has no native packet")
	}
	return packet.([]byte)
}

func ownedExceptionTestInstance(t testing.TB, profile gc.Profile, params []ValueTypeDescriptor) (*Instance, []byte) {
	t.Helper()
	leaf, err := gc.NewStructDesc(0, []gc.StorageKind{gc.StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	collector, err := gc.NewCollector(gc.Config{Profile: profile, NurseryBytes: 4096, TinyHeapBytes: 64 << 10, VerifyAfterCollect: true}, []gc.TypeDesc{leaf})
	if err != nil {
		t.Fatal(err)
	}
	c := &Compiled{Types: []DefinedTypeDescriptor{{Kind: CompositeTypeFunction, Params: params}}, memoryDir: &compiledMemoryDirectory{ehTags: []compiledTagDef{{TypeIndex: 0}}}}
	c.setOwnedExceptionReferences(true)
	in := &Instance{c: c, gc: collector, refStore: newReferenceStore(true)}
	nativePacket, err := coreruntime.NewArena(ownedExceptionPayloadBytes)
	if err != nil {
		t.Fatal(err)
	}
	ownedExceptionTestPackets.Store(in, nativePacket.Alloc(ownedExceptionPayloadBytes))
	t.Cleanup(func() { ownedExceptionTestPackets.Delete(in); nativePacket.Close() })

	directory := make([]byte, 8)
	binary.LittleEndian.PutUint64(directory, newNativeExceptionTag(c, 0, 0))
	t.Cleanup(func() { releaseNativeExceptionTags(c, directory); collector.Close() })
	return in, directory
}

func ownedExceptionPublicModule() []byte {
	makeBody := []byte{0x02, byte(wasm.HeapExn), 0x1f, 0x40, 1, byte(wasm.CatchAllRef), 0, 0x20, 0, 0x08, 0, 0x0b, 0x00, 0x0b, 0x0b}
	getBody := []byte{0x02, 0x7f, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0, 0x20, 0, 0x0a, 0x0b, 0x00, 0x0b, 0x0b}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), []byte{0x60, 1, 0x7f, 1, 0x63, 0x69}, []byte{0x60, 1, 0x63, 0x69, 1, 0x7f})), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))), wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("make", 0, 0), wasmtest.ExportEntry("get", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(makeBody), wasmtest.Code(getBody))))
}
func TestRV64OwnedExceptionPublicReturnAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		cfg := rv64I31Config()
		features := cfg.frontendFeatures()
		features.ExceptionHandling = true
		features.ExceptionReferences = true
		c, err := compileWithFrontendFeatures(cfg, ownedExceptionPublicModule(), features)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if !c.needsOwnedExceptionReferences() {
			t.Fatal("owned mode missing")
		}
		if artifact {
			blob, err := c.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			defer loaded.Close()
			c = loaded
			if !c.needsOwnedExceptionReferences() {
				t.Fatal("artifact lost owned mode")
			}
		}
		in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 512, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		first, err := in.Invoke("make", uint64(123))
		if err != nil || len(first) != 1 {
			t.Fatalf("make first %v,%v", first, err)
		}
		firstToken := first[0]
		second, err := in.Invoke("make", uint64(456))
		if err != nil || len(second) != 1 {
			t.Fatalf("make second %v,%v", second, err)
		}
		secondToken := second[0]
		for i := 0; i < 20; i++ {
			got, err := in.Invoke("get", firstToken)
			if err != nil || len(got) != 1 || got[0] != 123 {
				t.Fatalf("retained first %v,%v", got, err)
			}
			got, err = in.Invoke("get", secondToken)
			if err != nil || len(got) != 1 || got[0] != 456 {
				t.Fatalf("retained second %v,%v", got, err)
			}
		}
		if err = in.ReleaseExnRef(ExnRef{token: firstToken}); err != nil {
			t.Fatal(err)
		}
		if err = in.ReleaseExnRef(ExnRef{token: secondToken}); err != nil {
			t.Fatal(err)
		}
	}
}
func allocOwnedExceptionForTest(in *Instance, packet []byte) gc.Ref {
	native := ownedExceptionPacketForTest(in)
	copy(native, packet)
	var result [1]uint64
	in.dispatchGCExceptionHelper(0, gcExceptionAlloc, 0, []uint64{uint64(uintptr(unsafe.Pointer(&native[0])))}, result[:], nil)
	runtime.KeepAlive(packet)
	return gc.Ref(result[0])
}
func TestRV64OwnedExceptionLifetimeAndReclamation(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		t.Run(map[gc.Profile]string{gc.ProfileThroughput: "moving", gc.ProfileTiny: "tiny"}[profile], func(t *testing.T) {
			in, directory := ownedExceptionTestInstance(t, profile, nil)
			tagID := binary.LittleEndian.Uint64(directory)
			packet := make([]byte, ownedExceptionPayloadBytes)
			copy(packet, directory)
			binary.LittleEndian.PutUint64(packet[8:], 0x123456789abcdef0)
			first := allocOwnedExceptionForTest(in, packet)
			slot := in.gc.NewGlobalSlot(first)
			binary.LittleEndian.PutUint64(packet[8:], 0xfedcba9876543210)
			second := allocOwnedExceptionForTest(in, packet)
			if first == second {
				t.Fatal("each catch must allocate a distinct live object")
			}
			value, _ := ownedExceptionCollectors.Load(in.gc)
			owned := value.(*ownedExceptionCollector)
			for i := 0; i < 500; i++ {
				_ = allocOwnedExceptionForTest(in, packet)
				if i%25 == 0 {
					if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
						t.Fatal(err)
					}
					drainExceptionReclaims(in.gc)
				}
			}
			if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			drainExceptionReclaims(in.gc)
			payload, err := in.gc.StructPayloadReadOnly(first, owned.typeID)
			if err != nil || binary.LittleEndian.Uint64(payload[8:]) != 0x123456789abcdef0 {
				t.Fatalf("retained payload %x, %v", payload, err)
			}
			owned.mu.Lock()
			retained := len(owned.records)
			owned.mu.Unlock()
			if retained != 1 {
				t.Fatalf("weak records=%d, want1", retained)
			}
			// A closed producing instance cannot remove a still-live tag identity.
			releaseNativeExceptionTags(in.c, directory)
			clear(directory)
			if tag := retainExceptionTag(binary.LittleEndian.Uint64(payload)); tag == nil {
				t.Fatal("live exception lost closed tag")
			} else {
				releaseExceptionTag(tag)
			}
			if err := in.gc.SetGlobalSlot(slot, gc.Null()); err != nil {
				t.Fatal(err)
			}
			if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			drainExceptionReclaims(in.gc)
			owned.mu.Lock()
			retained = len(owned.records)
			owned.mu.Unlock()
			if retained != 0 {
				t.Fatalf("unreachable exception records=%d", retained)
			}
			exceptionTags.Lock()
			remainingTag := exceptionTags.byID[tagID]
			exceptionTags.Unlock()
			if remainingTag != nil {
				t.Fatal("dead exception retained closed tag metadata")
			}
		})
	}
}
func TestRV64OwnedExceptionCollectorPayload(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		param := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
		in, directory := ownedExceptionTestInstance(t, profile, []ValueTypeDescriptor{param})
		leaf, err := in.gc.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		packet := make([]byte, ownedExceptionPayloadBytes)
		copy(packet, directory)
		binary.LittleEndian.PutUint64(packet[72:], uint64(leaf))
		ref := allocOwnedExceptionForTest(in, packet)
		slot := in.gc.NewGlobalSlot(ref)
		for i := 0; i < 100; i++ {
			if _, err = in.gc.NewStructDefault(0); err != nil {
				t.Fatal(err)
			}
		}
		if err = in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		drainExceptionReclaims(in.gc)
		value, _ := ownedExceptionCollectors.Load(in.gc)
		owned := value.(*ownedExceptionCollector)
		payload, err := in.gc.StructPayloadReadOnly(ref, owned.typeID)
		if err != nil {
			t.Fatal(err)
		}
		retained := gc.Ref(binary.LittleEndian.Uint32(payload[72:]))
		if _, err = in.gc.ObjectType(retained); err != nil {
			t.Fatalf("GC payload lost: %v", err)
		}
		if err = in.gc.SetGlobalSlot(slot, gc.Null()); err != nil {
			t.Fatal(err)
		}
		if err = in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		drainExceptionReclaims(in.gc)
	}
}
func TestRV64OwnedExceptionExternrefPinReclaimed(t *testing.T) {
	param := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}, Nullable: true}}
	in, directory := ownedExceptionTestInstance(t, gc.ProfileThroughput, []ValueTypeDescriptor{param})
	token, err := in.refStore.issueExternref(new(int))
	if err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, ownedExceptionPayloadBytes)
	copy(packet, directory)
	binary.LittleEndian.PutUint64(packet[8:], token)
	ref := allocOwnedExceptionForTest(in, packet)
	slot := in.gc.NewGlobalSlot(ref)
	if in.refStore.releaseExternref(token) {
		t.Fatal("live exception did not pin externref")
	}
	if err = in.gc.SetGlobalSlot(slot, gc.Null()); err != nil {
		t.Fatal(err)
	}
	if err = in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	drainExceptionReclaims(in.gc)
	if !in.refStore.releaseExternref(token) {
		t.Fatal("dead exception retained externref")
	}
}

func BenchmarkRV64OwnedExceptionNumericCapture(b *testing.B) {
	in, directory := ownedExceptionTestInstance(b, gc.ProfileThroughput, nil)
	packet := ownedExceptionPacketForTest(in)
	copy(packet, directory)
	binary.LittleEndian.PutUint64(packet[8:], 123)
	var args, results [1]uint64
	args[0] = uint64(uintptr(unsafe.Pointer(&packet[0])))
	for i := 0; i < 256; i++ {
		in.dispatchGCExceptionHelper(0, gcExceptionAlloc, 0, args[:], results[:], nil)
	}
	if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
		b.Fatal(err)
	}
	drainExceptionReclaims(in.gc)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in.dispatchGCExceptionHelper(0, gcExceptionAlloc, 0, args[:], results[:], nil)
		if i%128 == 127 {
			if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				b.Fatal(err)
			}
			drainExceptionReclaims(in.gc)
		}
	}
	b.StopTimer()
	b.ReportMetric(160, "native-B/capture")
	b.ReportMetric(float64(unsafe.Sizeof(ownedExceptionRecord{})), "weak-record-B")
	runtime.KeepAlive(packet)
}

func BenchmarkRV64OwnedExceptionPublicThrowRef(b *testing.B) {
	cfg := rv64I31Config()
	features := cfg.frontendFeatures()
	features.ExceptionHandling = true
	features.ExceptionReferences = true
	c, err := compileWithFrontendFeatures(cfg, ownedExceptionPublicModule(), features)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	values, err := in.Invoke("make", uint64(123))
	if err != nil {
		b.Fatal(err)
	}
	token := values[0]
	defer func() {
		if err := in.ReleaseGCRef(GCRef{token: token}); err != nil {
			b.Fatal(err)
		}
	}()
	for i := 0; i < 16; i++ {
		if _, err = in.Invoke("get", token); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		values, err = in.Invoke("get", token)
		if err != nil || values[0] != 123 {
			b.Fatalf("throw_ref=%v,%v", values, err)
		}
	}
}

func TestRV64OwnedExceptionMemoryFootprint(t *testing.T) {
	const live = 4096
	measure := func(exceptions bool) int64 {
		in, directory := ownedExceptionTestInstance(t, gc.ProfileThroughput, nil)
		packet := make([]byte, ownedExceptionPayloadBytes)
		copy(packet, directory)
		ownedState := in.ownedExceptionCollector(nil)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for i := 0; i < live; i++ {
			var ref gc.Ref
			if exceptions {
				ref = allocOwnedExceptionForTest(in, packet)
			} else {
				var err error
				ref, err = in.gc.NewStructDefaultWithRoots(ownedState.typeID, gc.EmptyRoots{})
				if err != nil {
					t.Fatal(err)
				}
			}
			in.gc.NewGlobalSlot(ref)
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(in)
		bytes := int64(after.HeapAlloc) - int64(before.HeapAlloc)
		in.gc.Close()
		return bytes
	}
	baseline := measure(false)
	owned := measure(true)
	t.Logf("live=%d baseline collector+roots Go=%dB owned collector+roots+weakmap Go=%dB extra=%.2fB/live; object=160 nativeB, inline weakrecord=%dB; Compiled=%dB RV64coldstate=%dB", live, baseline, owned, float64(owned-baseline)/live, unsafe.Sizeof(ownedExceptionRecord{}), unsafe.Sizeof(Compiled{}), unsafe.Sizeof(railshotTailCompiledState{}))
}

func TestRV64OwnedExceptionTagRollback(t *testing.T) {
	cfg := rv64I31Config()
	features := cfg.frontendFeatures()
	features.ExceptionHandling = true
	features.ExceptionReferences = true
	c, err := compileWithFrontendFeatures(cfg, ownedExceptionPublicModule(), features)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	count := func() int { exceptionTags.Lock(); defer exceptionTags.Unlock(); return len(exceptionTags.byID) }
	before := count()

	for _, panics := range []bool{false, true} {
		var in *Instance
		var err error
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			in, err = instantiateCore(c, InstantiateOptions{afterCreate: func(*Instance) error {
				if panics {
					panic("owned exception rollback")
				}
				return fmt.Errorf("owned exception rollback")
			}})
		}()
		if in != nil || err == nil && recovered == nil {
			t.Fatalf("late rollback=%v,%v,%v", in, err, recovered)
		}
		if got := count(); got != before {
			t.Fatalf("tag registry after rollback=%d want%d", got, before)
		}
	}
}
