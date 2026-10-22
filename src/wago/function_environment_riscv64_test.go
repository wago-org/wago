//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestRV64ManagedFunctionFrameRetainsClosedProducer(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		t.Run(map[gc.Profile]string{gc.ProfileThroughput: "throughput", gc.ProfileTiny: "tiny"}[profile], func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			c := rv64TableStageCompile(t, "managed-function-frame", rv64LocalMutableReferenceImportModule(false), false)
			defer c.Close()
			imports := NewImports()
			imports.Function("env", "target", slotHostFunc(func(HostModule, []uint64, []uint64) {}))
			producer := rv64NativeTokenInstantiate(t, rt, c, imports)
			defer producer.Close()
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("local function descriptor unavailable")
			}
			in, _ := ownedExceptionTestInstance(t, profile, nil)
			in.refStore, in.jm = producer.refStore, producer.jm
			state := in.managedFunctionCollector(nil)
			ref, err := state.environmentForDescriptor(producer, descriptor, gc.EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			frame := make([]byte, 16)
			var pin runtime.Pinner
			pin.Pin(&frame[0])
			defer pin.Unpin()
			binary.LittleEndian.PutUint64(frame[8:], descriptor)
			roots := gcNativeFrameRoots{owner: in, base: uintptr(unsafe.Pointer(&frame[0])), offsets: []uint32{shared.GCFrameFuncRefRootFlag | 8}, frameBytes: 16,
				frameLayout: gcNativeFrameLayoutRISCV64, codeBase: 1, codeBytes: 1, allowExternalReturn: true}
			producer.Close()
			if !producer.hasPhysicalResources() {
				t.Fatal("Env lost closed producer before collection")
			}
			if err := in.gc.CollectFull(&roots); err != nil {
				t.Fatal(err)
			}
			drainManagedFunctionReclaims(in.gc)
			if state.envs[ref] == nil || !producer.hasPhysicalResources() {
				t.Fatal("raw function frame slot lost closed producer")
			}
			if got := binary.LittleEndian.Uint64(frame[8:]); got != descriptor {
				t.Fatalf("raw descriptor rewritten: got %#x want %#x", got, descriptor)
			}
			binary.LittleEndian.PutUint64(frame[8:], 0)
			if err := in.gc.CollectFull(&roots); err != nil {
				t.Fatal(err)
			}
			drainManagedFunctionReclaims(in.gc)
			if len(state.envs) != 0 || len(state.byDescriptor) != 0 || producer.hasPhysicalResources() {
				t.Fatal("cleared function frame slot retained producer")
			}
		})
	}
}

func TestRV64ManagedFunctionEnvPinAndReclaim(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		t.Run(map[gc.Profile]string{gc.ProfileThroughput: "throughput", gc.ProfileTiny: "tiny"}[profile], func(t *testing.T) {
			in, _ := ownedExceptionTestInstance(t, profile, nil)
			exceptions := in.ownedExceptionCollector(nil)
			state := in.managedFunctionCollector(nil)
			ref, err := state.newManagedFunctionEnv(in, gc.EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			if in.resourceRefs != 1 {
				t.Fatalf("producer roots=%d", in.resourceRefs)
			}
			again, err := state.newManagedFunctionEnv(in, gc.EmptyRoots{})
			if err != nil || ref != again || in.resourceRefs != 1 {
				t.Fatalf("cached Env=%v roots=%d err=%v", again, in.resourceRefs, err)
			}
			if allocs := testing.AllocsPerRun(1000, func() {
				if got, err := state.newManagedFunctionEnv(in, gc.EmptyRoots{}); err != nil || got != ref {
					panic("cached Env changed")
				}
			}); allocs != 0 {
				t.Fatalf("cached Env lookup allocations=%v", allocs)
			}
			slot := in.gc.NewGlobalSlot(ref)
			if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			drainManagedFunctionReclaims(in.gc)
			if in.resourceRefs != 1 || len(state.envs) != 1 {
				t.Fatal("rooted Env lost physical producer")
			}
			if err := in.gc.SetGlobalSlot(slot, gc.Null()); err != nil {
				t.Fatal(err)
			}
			if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			drainManagedFunctionReclaims(in.gc)
			if in.resourceRefs != 0 || len(state.envs) != 0 || len(state.byOwner) != 0 || len(state.graph.members) != 0 {
				t.Fatalf("reclaimed roots=%d envs=%d owners=%d graph=%d", in.resourceRefs, len(state.envs), len(state.byOwner), len(state.graph.members))
			}
			if value, ok := ownedExceptionCollectors.Load(in.gc); !ok || value != exceptions {
				t.Fatal("Env observer replaced exception observer")
			}
			in.gc.Close()
			if _, ok := managedFunctionCollectors.Load(in.gc); ok {
				t.Fatal("closed collector sidecar retained")
			}
			if _, ok := managedFunctionGraphs.Load(in.refStore); ok {
				t.Fatal("empty graph retained")
			}
		})
	}
}
