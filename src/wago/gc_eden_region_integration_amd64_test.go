//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"reflect"
	"strings"
	"testing"
)

// Keep all runtime admission rules intact. The only oracle difference is Eden
// partitioning; neither Tiny nor the stress nursery override admits regions.
func edenIntegrationConfig(shift uint8) GCConfig {
	return GCConfig{NurseryBytes: 1024, SurvivorBytes: 1024, NurseryRegionShift: shift,
		CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
}

// The private metadata observation is deliberate: allocation charges alone can
// also describe native handleless allocations and cannot qualify real regions.
// Read reflection avoids unsafe pointers or a production diagnostics API.
func edenIntegrationAssert(t *testing.T, in *Instance, shift uint8) {
	t.Helper()
	if in.gc == nil {
		t.Fatal("missing collector")
	}
	regions := reflect.ValueOf(in.gc).Elem().FieldByName("edenRegions")
	if shift == 0 {
		if regions.Len() != 0 {
			t.Fatal("baseline unexpectedly enabled regions")
		}
	} else {
		if regions.Len() != 16 {
			t.Fatalf("region directory length = %d, want 16", regions.Len())
		}
		used := uint64(0)
		for i := 0; i < regions.Len(); i++ {
			used += regions.Index(i).FieldByName("bump").Uint()
		}
		if used == 0 {
			t.Fatal("no payload allocated in region storage")
		}
	}
	stats := in.gc.Stats()
	if stats.Allocations == 0 || stats.FullCollections == 0 {
		t.Fatalf("allocation/collection not exercised: %+v", stats)
	}
}

func TestGCEdenRegionIntegrationImmutableGlobal(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("regionShift=%d", shift), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			providerCode, err := Compile(cfg, gcSharedGlobalProviderModule(false))
			if err != nil {
				t.Fatal(err)
			}
			defer providerCode.Close()
			consumerCode, err := Compile(cfg, gcSharedImmutableGlobalConsumerModule(false))
			if err != nil {
				t.Fatal(err)
			}
			defer consumerCode.Close()
			if providerCode.genericGCFrameRoots() == nil || consumerCode.genericGCFrameRoots() == nil {
				t.Fatal("immutable GC-global modules lost exact native root admission")
			}

			store := newReferenceStore(false)
			defer store.closeRuntime()
			gcConfig := edenIntegrationConfig(shift)
			provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: store})
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			global, err := provider.ExportedGlobalObject("g")
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: testImports("provider.g", global)})
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			if provider.gc == nil || provider.gc != consumer.gc || !store.ownsGCCollector(provider.gc) {
				t.Fatal("immutable GC-global instances do not share one Runtime collector domain")
			}
			for i := 0; i < 20; i++ {
				if got, callErr := consumer.Invoke("read"); callErr != nil || !reflect.DeepEqual(got, []uint64{42}) {
					t.Fatalf("consumer read %d = %v, %v", i, got, callErr)
				}
			}
			edenIntegrationAssert(t, consumer, shift)
			if err := provider.Close(); err != nil {
				t.Fatal(err)
			}
			if got, callErr := consumer.Invoke("read"); callErr != nil || !reflect.DeepEqual(got, []uint64{42}) {
				t.Fatalf("retained global after provider close = %v, %v", got, callErr)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}

		})
	}
}

func TestGCEdenRegionIntegrationMutableGlobal(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("regionShift=%d", shift), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			providerCode, err := Compile(cfg, gcSharedGlobalProviderModule(true))
			if err != nil {
				t.Fatal(err)
			}
			defer providerCode.Close()
			consumerCode, err := Compile(cfg, gcSharedImmutableGlobalConsumerModule(true))
			if err != nil {
				t.Fatal(err)
			}
			defer consumerCode.Close()
			if !providerCode.sharedGCPersistentDomainSafe() || !consumerCode.sharedGCPersistentDomainSafe() || providerCode.genericGCFrameRoots() == nil || consumerCode.genericGCFrameRoots() == nil {
				t.Fatal("mutable GC-global modules lost shared-global root admission")
			}
			store := newReferenceStore(false)
			defer store.closeRuntime()
			gcConfig := edenIntegrationConfig(shift)
			provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: store})
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			global, err := provider.ExportedGlobalObject("g")
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: testImports("provider.g", global)})
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			if provider.gc == nil || provider.gc != consumer.gc {
				t.Fatal("mutable GC-global aliases do not share one collector")
			}
			for i := 0; i < 20; i++ {
				if got, callErr := consumer.Invoke("read"); callErr != nil || !reflect.DeepEqual(got, []uint64{77}) {
					t.Fatalf("mutable consumer read %d = %v, %v", i, got, callErr)
				}
				if got, callErr := provider.Invoke("read"); callErr != nil || !reflect.DeepEqual(got, []uint64{77}) {
					t.Fatalf("provider alias read %d = %v, %v", i, got, callErr)
				}
			}
			edenIntegrationAssert(t, provider, shift)
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if got, callErr := provider.Invoke("read"); callErr != nil || !reflect.DeepEqual(got, []uint64{77}) {
				t.Fatalf("provider after importer close = %v, %v", got, callErr)
			}

		})
	}
}

func TestGCEdenRegionIntegrationRetainedToken(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("regionShift=%d", shift), func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGenericPublicTokenModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			store := newReferenceStore(false)
			defer store.closeRuntime()
			config := edenIntegrationConfig(shift)
			first, err := instantiateCore(compiled, InstantiateOptions{GC: config, store: store})
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			second, err := instantiateCore(compiled, InstantiateOptions{GC: config, store: store})
			if err != nil {
				first.Close()
				t.Fatal(err)
			}
			defer second.Close()
			if first.gc == nil || first.gc != second.gc || !store.ownsGCCollector(first.gc) {
				t.Fatal("generic token instances do not share one Runtime collector domain")
			}
			bits, err := first.Invoke("new_struct")
			if err != nil || len(bits) != 1 {
				first.Close()
				t.Fatalf("first new_struct = %v, %v", bits, err)
			}
			ref := ValueOf(ValAnyRef, bits[0]).GCRef()
			token := ValueGCRef(ref).Bits()
			if err := second.ReleaseGCRef(ref); err == nil || !strings.Contains(err.Error(), "different producer") {
				first.Close()
				t.Fatalf("cross-producer release = %v", err)
			}
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := second.Invoke("read_struct", token); err != nil || len(got) != 1 || got[0] != 42 {
				t.Fatalf("cross-instance token ingress after producer close = %v, %v", got, err)
			}
			secondBits, err := second.Invoke("new_array")
			if err != nil || len(secondBits) != 1 {
				t.Fatalf("second new_array after producer close = %v, %v", secondBits, err)
			}
			if err := second.ReleaseGCRef(ValueOf(ValAnyRef, secondBits[0]).GCRef()); err != nil {
				t.Fatal(err)
			}
			edenIntegrationAssert(t, second, shift)
			if err := first.ReleaseGCRef(ref); err != nil {
				t.Fatalf("release retained producer token: %v", err)
			}

		})
	}
}

func TestGCEdenRegionIntegrationBorrowExclusion(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("regionShift=%d", shift), func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), guestStorageGCCollectModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()

			in, err := Instantiate(compiled, InstantiateOptions{GC: edenIntegrationConfig(shift)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("roundtrip", 7); err != nil || len(got) != 1 || uint32(got[0]) != 7 {
				t.Fatalf("roundtrip = %v, %v; want [7], nil", got, err)
			}

			edenIntegrationAssert(t, in, shift)
			caller := in.beginHostCallScopeReservedWithID(newInvocationID(), nil)
			defer caller.scope.end(caller.generation, caller.parentGeneration)
			if err := caller.WithGuestStorage(func(GuestStorage) error {
				before := in.gc.Stats().FullCollections
				collectErr := in.CollectGC()
				if after := in.gc.Stats().FullCollections; after != before {
					return &guestStorageTestError{"rejected borrow collection changed counter"}
				}
				if !errors.Is(collectErr, ErrPermissionDenied) {
					if collectErr != nil {
						return collectErr
					}
					return &guestStorageTestError{"collection during borrow was not rejected"}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := in.CollectGC(); err != nil {
				t.Fatalf("collection after guest-storage borrow: %v", err)
			}

		})
	}
}

func TestGCEdenRegionIntegrationIndirectTail(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("regionShift=%d", shift), func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcTailIndirectFrameRootModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			plan := compiled.genericGCFrameRoots()
			if plan == nil || len(plan.callsites) != 0 {
				t.Fatalf("tail-indirect root map = %+v, want no retained caller callsite", plan)
			}
			in, err := Instantiate(compiled, InstantiateOptions{GC: edenIntegrationConfig(shift)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("run", 66); err != nil || !reflect.DeepEqual(got, []uint64{66}) {
				t.Fatalf("run = %v, %v; want [66]", got, err)
			}

			edenIntegrationAssert(t, in, shift)

		})
	}
}

func TestGCEdenRegionIntegrationExceptions(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("regionShift=%d", shift), func(t *testing.T) {
			data, err := hex.DecodeString(gcExceptionPayloadWasm)
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			if status := compiled.GCNativeRootAdmission(); !status.Required || !status.Exact {
				t.Fatalf("root admission = %+v", status)
			}
			in, err := Instantiate(compiled, InstantiateOptions{GC: edenIntegrationConfig(shift)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for export, want := range map[string]uint64{"direct": 42, "held": 7, "mixed": 105} {
				got, err := in.Invoke(export)
				if err != nil || len(got) != 1 || uint32(got[0]) != uint32(want) {
					t.Fatalf("%s = %v, %v; want %d", export, got, err, want)
				}
				if stats := in.gc.Stats(); stats.FullCollections == 0 {
					t.Fatalf("%s ran without a collection", export)
				}
			}

			edenIntegrationAssert(t, in, shift)

		})
	}
}

func TestGCEdenRegionIntegrationForeignToken(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("regionShift=%d", shift), func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGenericPublicTokenModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			producer, err := Instantiate(compiled, InstantiateOptions{GC: edenIntegrationConfig(shift)})
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			foreign, err := Instantiate(compiled, InstantiateOptions{GC: edenIntegrationConfig(shift)})
			if err != nil {
				t.Fatal(err)
			}
			defer foreign.Close()
			bits, err := producer.Invoke("new_struct")
			if err != nil || len(bits) != 1 {
				t.Fatalf("issue token = %v, %v", bits, err)
			}
			ref := ValueOf(ValAnyRef, bits[0]).GCRef()
			foreignBits, err := foreign.Invoke("new_array")
			if err != nil || len(foreignBits) != 1 {
				t.Fatalf("issue foreign token = %v, %v", foreignBits, err)
			}
			foreignRef := ValueOf(ValAnyRef, foreignBits[0]).GCRef()
			if _, err := foreign.Invoke("read_struct", ValueGCRef(ref).Bits()); err == nil || !strings.Contains(err.Error(), "stale GC reference token") {
				t.Fatalf("foreign-store GC token ingress = %v", err)
			}
			if err := foreign.ReleaseGCRef(foreignRef); err != nil {
				t.Fatal(err)
			}
			if err := producer.ReleaseGCRef(ref); err != nil {
				t.Fatal(err)
			}

			edenIntegrationAssert(t, producer, shift)

			edenIntegrationAssert(t, foreign, shift)

		})
	}
}

// A live outer object remains solely in a suspended native frame while nested
// guest execution collects, and remains valid after a trapped nested entry.
func TestGCEdenRegionIntegrationReentry(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("regionShift=%d", shift), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcHostReentryFrameRootModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			var in *Instance
			active, calls := 0, 0
			trap := false
			host := CallerHostCallFunc(func(caller Caller, call HostCall) {
				active++
				calls++
				defer func() { active-- }()
				if active == 3 {
					if trap {
						panic(HostTrap{Err: errors.New("region integration trap")})
					}
					got, err := in.InvokeFromHost(context.Background(), caller, "inner")
					if err != nil || len(got) != 1 || got[0] != 0 {
						panic(fmt.Sprintf("inner=%v, %v", got, err))
					}
				} else {
					want := uint64(active)
					got, err := in.InvokeFromHost(context.Background(), caller, "outer", want)
					if err != nil {
						panic(HostTrap{Err: err})
					}
					if len(got) != 1 || got[0] != want {
						panic(fmt.Sprintf("nested outer=%v", got))
					}
				}
				call.ResultSlots()[0] = 0
			})
			in, err = Instantiate(c, InstantiateOptions{GC: edenIntegrationConfig(shift), Imports: testImports("env.reenter", host)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for _, fail := range []bool{false, true, false} {
				trap = fail
				calls = 0
				got, err := in.Invoke("outer", 91)
				if fail {
					if err == nil || !strings.Contains(err.Error(), "region integration trap") {
						t.Fatalf("trap=%v", err)
					}
				} else if err != nil || len(got) != 1 || got[0] != 91 {
					t.Fatalf("outer=%v, %v", got, err)
				}
				if active != 0 || calls != 3 {
					t.Fatalf("active=%d calls=%d", active, calls)
				}
				state := in.publicGCState()
				state.mu.Lock()
				pending := state.hostActivationCount
				state.mu.Unlock()
				if pending != 0 {
					t.Fatalf("leaked suspended activations: %d", pending)
				}
				assertHostReentryDepthsEmpty(t)
			}
			edenIntegrationAssert(t, in, shift)
		})
	}
}

func TestGCEdenRegionIntegrationTables(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("regionShift=%d", shift), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			pc, err := Compile(cfg, gcSharedMultiTableModule(false))
			if err != nil {
				t.Fatal(err)
			}
			defer pc.Close()
			cc, err := Compile(cfg, gcSharedMultiTableModule(true))
			if err != nil {
				t.Fatal(err)
			}
			defer cc.Close()
			store := newReferenceStore(false)
			defer store.closeRuntime()
			opts := InstantiateOptions{GC: edenIntegrationConfig(shift), store: store}
			p, err := instantiateCore(pc, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			a, err := p.ExportedTable("t0")
			if err != nil {
				t.Fatal(err)
			}
			b, err := p.ExportedTable("t1")
			if err != nil {
				t.Fatal(err)
			}
			opts.Imports = testImports("provider.t0", a, "provider.t1", b)
			c, err := instantiateCore(cc, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if p.gc != c.gc {
				t.Fatal("collectors differ")
			}
			check := func(in *Instance, name string, want uint64, args ...uint64) {
				t.Helper()
				got, err := in.Invoke(name, args...)
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("%s=%v,%v want %d", name, got, err, want)
				}
			}
			check(c, "set0", 11, 0, 11)
			check(c, "set1", 22, 0, 22)
			check(c, "grow1", 1, 1)
			check(c, "set1", 33, 1, 33)
			for i := 0; i < 8; i++ {
				check(c, "set0", uint64(40+i), 0, uint64(40+i))
				check(p, "read0", uint64(40+i), 0)
				check(p, "read1", 33, 1)
			}
			edenIntegrationAssert(t, c, shift)
			if err := p.Close(); err != nil {
				t.Fatal(err)
			}
			if err := c.CollectGC(); err != nil {
				t.Fatal(err)
			}
			check(c, "read1", 22, 0)
			check(c, "read1", 33, 1)
		})
	}
}

func TestGCEdenRegionIntegrationTableRollback(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("regionShift=%d", shift), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			providerCode, err := Compile(cfg, gcSharedMultiTableModule(false))
			if err != nil {
				t.Fatal(err)
			}
			defer providerCode.Close()
			consumerCode, err := Compile(cfg, gcSharedMultiTableModule(true))
			if err != nil {
				t.Fatal(err)
			}
			defer consumerCode.Close()
			storeA, storeB := newReferenceStore(false), newReferenceStore(false)
			defer storeA.closeRuntime()
			defer storeB.closeRuntime()
			gcConfig := edenIntegrationConfig(shift)
			providerA, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: storeA})
			if err != nil {
				t.Fatal(err)
			}
			defer providerA.Close()
			providerB, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: storeB})
			if err != nil {
				t.Fatal(err)
			}
			defer providerB.Close()
			table0, err := providerA.ExportedTable("t0")
			if err != nil {
				t.Fatal(err)
			}
			table1, err := providerB.ExportedTable("t1")
			if err != nil {
				t.Fatal(err)
			}
			if in, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: storeA, Imports: testImports("provider.t0", table0, "provider.t1", table1)}); err == nil {
				in.Close()
				t.Fatal("cross-domain second table import unexpectedly succeeded")
			} else if got := err.Error(); !strings.Contains(got, "same Runtime GC domain") {
				t.Fatalf("cross-domain second table error = %v", err)
			}
			for i, table := range []*Table{table0, table1} {
				table.owner.mu.Lock()
				importers := table.owner.importers
				table.owner.mu.Unlock()
				if importers != 0 {
					t.Fatalf("table %d retained %d importer roots after rollback", i, importers)
				}
			}

			for _, in := range []*Instance{providerA, providerB} {
				got, err := in.Invoke("set0", 0, 59)
				if err != nil || len(got) != 1 || got[0] != 59 {
					t.Fatalf("post rollback set=%v,%v", got, err)
				}
				edenIntegrationAssert(t, in, shift)
			}

		})
	}
}

// Registration adds distinct managed types to an already executing shared
// domain. Both the old and new instance must retain their canonical mappings.
func TestGCEdenRegionIntegrationMetadataGrowth(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("regionShift=%d", shift), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			pc, err := Compile(cfg, gcSharedGlobalProviderModule(false))
			if err != nil {
				t.Fatal(err)
			}
			defer pc.Close()
			store := newReferenceStore(false)
			defer store.closeRuntime()
			p, err := instantiateCore(pc, InstantiateOptions{GC: edenIntegrationConfig(shift), store: store})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			global, err := p.ExportedGlobalObject("g")
			if err != nil {
				t.Fatal(err)
			}
			check := func(in *Instance) {
				t.Helper()
				got, err := in.Invoke("read")
				if err != nil || len(got) != 1 || got[0] != 42 {
					t.Fatalf("read=%v,%v", got, err)
				}
			}
			check(p) // Publish and exercise the old native views before registration.
			initial := reflect.ValueOf(p.gc).Elem().FieldByName("types").Len()
			previous := initial
			var instances []*Instance
			for width := 2; width <= 16; width++ {
				types := [][]byte{{0x5f, 1, 0x7f, 1}}
				extra := []byte{0x5f, byte(width)}
				for j := 0; j < width; j++ {
					extra = append(extra, 0x7f, 1)
				}
				types = append(types, extra, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))
				imp := append(append(wasmtest.Name("provider"), wasmtest.Name("g")...), byte(wasm.ExternGlobal), 0x64, 0, 0)
				// Exercise the newly registered type as well as the original imported root.
				code := []byte{0, 0xfb, 1, 1, 0x1a, 0x41, 9, 0xfb, 0, 0, 0x1a, 0x23, 0, 0xfb, 2, 0, 0, 0x0b}
				data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{2})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("read", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))))
				c, err := Compile(cfg, data)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := instantiateCore(c, InstantiateOptions{GC: edenIntegrationConfig(shift), store: store, Imports: testImports("provider.g", global)})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if in.gc != p.gc {
					t.Fatal("registration escaped shared collector")
				}
				instances = append(instances, in)
				current := reflect.ValueOf(p.gc).Elem().FieldByName("types").Len()
				if current <= previous {
					t.Fatalf("type metadata did not grow: %d -> %d", previous, current)
				}
				previous = current
				oldID, ok := p.gcDomainType(0)
				if !ok {
					t.Fatal("old type mapping missing")
				}
				newID, ok := in.gcDomainType(0)
				if !ok || newID != oldID {
					t.Fatalf("canonical identity changed: %d -> %d", oldID, newID)
				}
				check(in)
				check(p)
				edenIntegrationAssert(t, in, shift)
			}
			for _, in := range instances {
				check(in)
			}
			check(p)
			if previous < initial+15 {
				t.Fatalf("insufficient growth: %d -> %d", initial, previous)
			}
		})
	}
}
