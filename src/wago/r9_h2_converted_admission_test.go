//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"reflect"
	"testing"
)

// Check ownership admission before constructing any instance or publishing an
// aggregate. This is not permission to execute a missing ownership path.
func TestR9H2ConvertedAggregateAdmission(t *testing.T) {
	imp := append(append(wasmtest.Name("p"), wasmtest.Name("slot")...), 3, 0x6f, 1)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x70, 1}, []byte{0x60, 0, 0})),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("publish", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0xd2, 0, 0xfb, 0, 0, 0xfb, 0x1b, 0x24, 0, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
	if err != nil {
		t.Fatalf("STOP converted aggregate compile admission: %v", err)
	}
	defer compiled.Close()
	t.Logf("product=%d generic=%t conversion=%t managed=%t frameRoots=%t sharedSafe=%t", compiled.stagedGCStructProduct(), compiled.usesGenericGCExecution(), compiled.stagedGCStructProduct().requiresExternConversion(), compiledManagedFunctionReachability(compiled), compiled.genericGCFrameRoots() != nil, compiled.sharedGCPersistentDomainSafe())
	if a := compiled.GCNativeRootAdmission(); !a.Exact || a.Safepoints == 0 {
		t.Fatalf("missing exact allocation maps: %+v", a)
	}
	if err := validateCompiledGCFrameRoots(compiled, compiled.genericGCFrameRoots()); err != nil {
		t.Fatal(err)
	}
	if !compiledManagedFunctionReachability(compiled) {
		t.Fatal("STOP converted function aggregate is outside H6 managed ownership admission; no instantiation/publication/access follows")
	}
}

func TestR9H2ConvertedAggregateAdmissionControls(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field byte
		body  []byte
	}{
		{"numeric", 0x7f, []byte{0x41, 1, 0xfb, 0, 0, 0xfb, 0x1b, 0x1a, 0x0b}},
		{"unused-function-type", 0x70, []byte{0xd0, 0x6e, 0xfb, 0x1b, 0x1a, 0x0b}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, tc.field, 1}, []byte{0x60, 0, 0})),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tc.body))),
			)
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			if compiledManagedFunctionReachability(compiled) {
				t.Fatal("nonfunction/nonallocating control activated function ownership")
			}
		})
	}
}

var r9H2StoppedStores []*referenceStore

// This bounded local-owner control never retires a publisher with a foreign
// container receipt. It qualifies allocating converted function aggregates and
// exact frame maps while the conversion owner is alive.
func TestR9H2ConvertedAggregateCollectingLocalOwner(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("region=%d", shift), func(t *testing.T) {
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x70, 1}, []byte{0x60, 0, 0})),
				wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1}, []byte{1})),
				wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("clear", 0, 2))),
				wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0xd2, 0, 0xfb, 0, 0, 0xfb, 0x1b, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd0, 0x6f, 0x24, 0, 0x0b}))),
			)
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if !compiledManagedFunctionReachability(c) || !c.GCNativeRootAdmission().Exact {
				t.Fatal("STOP missing ownership/frame admission")
			}
			in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, NurseryRegionShift: shift, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			r9H2StoppedStores = append(r9H2StoppedStores, in.refStore)
			if !in.hasPhysicalResources() {
				t.Fatal("STOP missing producer before collecting constructor")
			}
			if _, err = in.Invoke("run"); err != nil {
				t.Fatal(err)
			}
			conversion := in.existingGCExternConversionState()
			if conversion == nil || conversion.closed || conversion.count != 1 {
				t.Fatal("STOP missing live converted identity")
			}
			entry := conversion.entries[0]
			if !entry.hasRoot || !entry.ref.IsObj() {
				t.Fatal("STOP missing checked root")
			}
			root, err := in.gc.CheckedTableSlot(entry.rootSlot)
			if err != nil || root != entry.ref {
				t.Fatal("STOP wrong checked root")
			}
			state, ok := managedFunctionCollectors.Load(in.gc)
			if !ok || len(state.(*managedFunctionCollector).envs) != 1 {
				t.Fatal("STOP missing function environment")
			}
			if got := reflect.ValueOf(in.gc).Elem().FieldByName("edenRegions").Len() != 0; got != (shift != 0) {
				t.Fatal("STOP wrong actual region policy")
			}
			if err = in.CollectGC(); err != nil {
				t.Fatal(err)
			}
			if !in.hasPhysicalResources() || len(state.(*managedFunctionCollector).envs) != 1 {
				t.Fatal("STOP collection lost producer")
			}
			if stats := in.gc.Stats(); stats.MinorCollections+stats.FullCollections == 0 {
				t.Fatal("no collection")
			}
			// Consume the local conversion through the transactional receipt API before
			// closing its owner; no unimplemented surviving-provider path is exercised.
			if err = conversion.replaceExtern(0, entry.externWord); err != nil {
				t.Fatal(err)
			}
			if _, err = in.Invoke("clear"); err != nil {
				t.Fatal(err)
			}
			if err = conversion.replaceExtern(entry.externWord, 0); err != nil {
				t.Fatal(err)
			}
			if err = in.CollectGC(); err != nil {
				t.Fatal(err)
			}
			if err = in.Close(); err != nil {
				t.Fatal(err)
			}
			if in.hasPhysicalResources() {
				t.Fatal("local owner retained after final clear")
			}
		})
	}
}
