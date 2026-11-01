//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The only function edge is () -> (): collector identity cannot come from its
// signature. The tag-free case must discover and retain the foreign payload.
func TestGCScalarExceptionTransfer(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		for _, catchAll := range []bool{false, true} {
			t.Run(fmt.Sprintf("region=%d/catch_all_ref=%t", shift, catchAll), func(t *testing.T) {
				types := [][]byte{{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, nil), {0x60, 1, 0x64, 0, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), {0x60, 0, 1, 0x64, 0}}
				providerBytes := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(types...)),
					wasmtest.Section(3, []byte{1, 1}),
					wasmtest.Section(13, []byte{1, 0, 2}),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("throw", 0, 0), wasmtest.ExportEntry("tag", byte(wasm.ExternTag), 0))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0xfb, 0, 0, 0x08, 0, 0x0b}))),
				)
				imports := [][]byte{append(append(wasmtest.Name("env"), wasmtest.Name("throw")...), 0, 1), append(append(wasmtest.Name("env"), wasmtest.Name("collect")...), 0, 1)}
				body := []byte{0x02, 4, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0, 0x10, 0, 0x0b, 0x00, 0x0b, 0x10, 1, 0xfb, 2, 0, 0, 0x0b}
				if catchAll {
					// Drop the operand alias after local.tee, collect with only the retained
					// exception record live, then consume the local alias.
					body = []byte{0x02, 0x69, 0x1f, 0x40, 1, byte(wasm.CatchAllRef), 0, 0x10, 0, 0x0b, 0x00, 0x0b, 0x22, 0, 0x1a, 0x10, 1, 0x20, 0, 0x1a, 0x41, 42, 0x0b}
				} else {
					imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("tag")...), byte(wasm.ExternTag), 0, 2))
				}
				body = append([]byte{0x41, 0, 0xfb, 0, 0, 0x1a}, body...)
				locals := []byte{0}
				if catchAll {
					locals = []byte{1, 1, 0x69}
				}
				fn := append(locals, body...)
				consumerBytes := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 3}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(fn))), fn...))))
				cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
				pc, err := Compile(cfg, providerBytes)
				if err != nil {
					t.Fatal(err)
				}
				defer pc.Close()
				cc, err := Compile(cfg, consumerBytes)
				if err != nil {
					t.Fatal(err)
				}
				defer cc.Close()
				if admission := cc.GCNativeRootAdmission(); !admission.Required || !admission.Exact || admission.MaximumRoots == 0 {
					t.Fatalf("payload absent from exact root plan: %+v", admission)
				}
				store := newReferenceStore(false)
				defer store.closeRuntime()
				settings := GCConfig{NurseryBytes: 1024, SurvivorBytes: 1024, NurseryRegionShift: shift, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
				provider, err := instantiateCore(pc, InstantiateOptions{GC: settings, store: store})
				if err != nil {
					t.Fatal(err)
				}
				defer provider.Close()
				thrower, err := provider.ExportedFunc("throw")
				if err != nil {
					t.Fatal(err)
				}
				tag, err := provider.ExportedTag("tag")
				if err != nil {
					t.Fatal(err)
				}
				collections := 0
				trap := false
				collect := func(host HostModule, _, _ []uint64) {
					if err := host.(GCHostModule).CollectGC(); err != nil {
						t.Error(err)
						return
					}
					collections++
					if live := provider.gc.Stats().LiveObjects; live != 1 {
						t.Errorf("live payloads=%d, want 1", live)
					}
					if trap {
						panic(HostTrap{Err: errors.New("rooting transfer trap")})
					}
				}
				bindings := testImports("env.throw", thrower, "env.collect", collect)
				if !catchAll {
					bindings.Tag("env", "tag", tag)
				}
				consumer, err := instantiateCore(cc, InstantiateOptions{GC: settings, store: store, Imports: bindings})
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				if provider.gc == nil || provider.gc != consumer.gc {
					t.Fatal("scalar EH edge lost collector identity")
				}
				if enabled := reflect.ValueOf(consumer.gc).Elem().FieldByName("edenRegions").Len() != 0; enabled != (shift != 0) {
					t.Fatal("incorrect actual allocation policy")
				}
				for i := 0; i < 3; i++ {
					trap = i == 1
					before := consumer.gc.Stats().FullCollections
					got, err := consumer.Invoke("run")
					if trap {
						if err == nil {
							t.Fatal("missing host trap")
						}
					} else if err != nil || len(got) != 1 || got[0] != 42 {
						t.Fatalf("run=%v,%v", got, err)
					}
					if consumer.gc.Stats().FullCollections <= before {
						t.Fatal("no collection")
					}
					if state := consumer.existingPublicGCState(); state != nil && state.hostActivationCount != 0 {
						t.Fatal("leaked host activation")
					}
					if err := consumer.CollectGC(); err != nil {
						t.Fatal(err)
					}
					if live := consumer.gc.Stats().LiveObjects; live != 0 {
						t.Fatalf("dropped/trapped payloads=%d", live)
					}
				}
				if collections != 3 {
					t.Fatalf("collections=%d", collections)
				}
			})
		}
	}
}

func TestGCScalarExceptionRejectsNonEHProducer(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	types := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil)))
	pc, err := Compile(cfg, wasmtest.Module(types, wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b})))))
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("call")...), 0, 0)
	cc, err := Compile(cfg, wasmtest.Module(types, wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x02, 0x69, 0x1f, 0x40, 1, byte(wasm.CatchAllRef), 0, 0x10, 0, 0x0b, 0x00, 0x0b, 0x1a, 0x0b})))))
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	store := newReferenceStore(false)
	defer store.closeRuntime()
	provider, err := instantiateCore(pc, InstantiateOptions{store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	fn, err := provider.ExportedFunc("call")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := instantiateCore(cc, InstantiateOptions{store: store, Imports: testImports("env.call", fn)})
	if consumer != nil {
		consumer.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "exception-enabled producer") {
		t.Fatalf("scalar non-EH producer admission=%v", err)
	}
}
