//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"testing"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestRV64ManagedFunctionExceptionPayloadRetention(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		t.Run(map[gc.Profile]string{gc.ProfileThroughput: "throughput", gc.ProfileTiny: "tiny"}[profile], func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			c := rv64TableStageCompile(t, "managed-function-exception", rv64LocalMutableReferenceImportModule(false), false)
			defer c.Close()
			imports := NewImports()
			imports.Function("env", "target", slotHostFunc(func(HostModule, []uint64, []uint64) {}))
			producer := rv64NativeTokenInstantiate(t, rt, c, imports)
			defer producer.Close()
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("local function descriptor unavailable")
			}
			in, tags := ownedExceptionTestInstance(t, profile, []ValueTypeDescriptor{{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapFunc}}}})
			in.refStore, in.jm = producer.refStore, producer.jm
			packet := ownedExceptionPacketForTest(in)
			binary.LittleEndian.PutUint64(packet, binary.LittleEndian.Uint64(tags))
			binary.LittleEndian.PutUint64(packet[8:], descriptor)
			results := make([]uint64, 1)
			in.dispatchGCExceptionHelper(0, gcExceptionAlloc, 0, []uint64{uint64(uintptr(unsafe.Pointer(&packet[0])))}, results, nil)
			ref := gc.Ref(uint32(results[0]))
			slot := in.gc.NewGlobalSlot(ref)
			producer.Close()
			if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			drainExceptionReclaims(in.gc)
			drainManagedFunctionReclaims(in.gc)
			if !producer.hasPhysicalResources() {
				t.Fatal("live exception lost closed producer")
			}
			state, ok := managedFunctionCollectors.Load(in.gc)
			if !ok || len(state.(*managedFunctionCollector).envs) != 1 {
				t.Fatal("exception has no managed function Env")
			}
			actual, err := in.gc.ObjectType(ref)
			if err != nil {
				t.Fatal(err)
			}
			owned := in.ownedExceptionCollector(nil)
			if actual == owned.typeID || !managedExceptionPayloadType(in.gc, actual) {
				t.Fatal("function-bearing exception has no private Env fields")
			}
			payload, err := in.gc.StructPayloadReadOnly(ref, actual)
			if err != nil {
				t.Fatal(err)
			}
			if len(payload) != 152 || binary.LittleEndian.Uint64(payload[8:]) != descriptor {
				t.Fatalf("payload bytes=%d raw descriptor=%#x", len(payload), binary.LittleEndian.Uint64(payload[8:]))
			}
			in.dispatchGCExceptionHelper(0, gcExceptionPayload, 0, []uint64{uint64(ref)}, results, nil)
			if results[0] != uint64(uintptr(unsafe.Pointer(&payload[0]))) || binary.LittleEndian.Uint64(payload[8:]) != descriptor {
				t.Fatal("throw_ref payload adapter changed descriptor")
			}
			if err := in.gc.SetGlobalSlot(slot, gc.Null()); err != nil {
				t.Fatal(err)
			}
			if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			drainExceptionReclaims(in.gc)
			drainManagedFunctionReclaims(in.gc)
			if producer.hasPhysicalResources() || len(state.(*managedFunctionCollector).envs) != 0 || len(owned.records) != 0 {
				t.Fatal("dropped exception retained function producer or metadata")
			}
		})
	}
}
