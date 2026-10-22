//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64MutableFuncrefStoreSelfModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("store", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}))),
	)
}

func TestRV64PublicMutableFuncrefGlobalStoreSelf(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			shared, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
			if err != nil {
				t.Fatal(err)
			}
			defer shared.Close()
			compiled := rv64TableStageCompile(t, "mutable-funcref-store-self", rv64MutableFuncrefStoreSelfModule(), artifact)
			view := compiled.executionView()
			if !(rv64MutableFuncrefGlobalStoreSelf(view) || rv64PrimitiveReferenceContract(view)) || !view.usesGuestGlobalStoreHelper() {
				t.Fatalf("mutable funcref self-store witness missing: code=%d/%x desc=%t escape=%t refs=%t/%t/%t sync=%d validation=%v", len(view.code), sha256.Sum256(view.code), view.NeedsFuncRefDescs, view.dynamicFuncrefEscape, view.nativeReferenceFunctionABI, view.nativeReferenceCallABI, view.nativeReferenceControlABI, view.syncHostSlots, railshotValidateCompiled(view))
			}
			module, err := rt.Module(compiled)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), module, WithImports(NewImports().Global("env", "cell", shared)))
			module.Close()
			if err != nil {
				t.Fatal(err)
			}
			descriptor, ok := consumer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("consumer function zero has no descriptor")
			}
			if _, ok = rv64PublishedDescriptor(consumer, descriptor); !ok {
				t.Fatal("consumer function zero descriptor was not published")
			}
			before := consumer.referenceLifetime().snapshot()
			if err = shared.setGuestFuncrefDescriptor(consumer, descriptor+8); err == nil {
				t.Fatal("misaligned guest descriptor admitted")
			}
			if got := readGlobalObject(shared, ValFuncRef); got != 0 || consumer.referenceLifetime().snapshot() != before {
				t.Fatal("rejected descriptor changed global state or ownership", got, before, consumer.referenceLifetime().snapshot())
			}
			for iteration := 0; iteration < 2; iteration++ {
				if _, err = consumer.Invoke("store"); err != nil {
					t.Fatal("guest self-store", iteration, err)
				}
				if got := readGlobalObject(shared, ValFuncRef); got != descriptor {
					t.Fatalf("guest self-store descriptor = %#x, want %#x", got, descriptor)
				}
				after := consumer.referenceLifetime().snapshot()
				if after.ResourceRoots != before.ResourceRoots+1 || !after.PhysicalResources || after.LogicallyClosed {
					t.Fatal("guest self-store descriptor root drifted", before, after)
				}
				shared.owner.mu.Lock()
				state, retained := shared.owner.retained[consumer]
				precise := retained && state != nil && state.precise && len(state.proxyDescriptors) == 0 && len(shared.owner.retained) == 1
				shared.owner.mu.Unlock()
				if !precise {
					t.Fatal("guest self-store did not publish one precise owner root")
				}
			}
			if err = consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err = compiled.Close(); err != nil {
				t.Fatal(err)
			}
			parked := consumer.referenceLifetime().snapshot()
			if !parked.LogicallyClosed || !parked.PhysicalResources || parked.ResourceRoots != 1 {
				t.Fatal("global did not retain the closed descriptor owner", parked)
			}
			if _, ok = rv64PublishedDescriptor(consumer, descriptor); !ok {
				t.Fatal("retained descriptor was unpublished before overwrite")
			}
			if err = shared.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
				t.Fatal("host clear after owner close", err)
			}
			if got := readGlobalObject(shared, ValFuncRef); got != 0 {
				t.Fatal("host clear did not publish null", got)
			}
			retired := consumer.referenceLifetime().snapshot()
			if !retired.LogicallyClosed || retired.PhysicalResources || retired.ResourceRoots != 0 {
				t.Fatal("global clear did not retire the descriptor owner", retired)
			}
			shared.owner.mu.Lock()
			retained := len(shared.owner.retained)
			shared.owner.mu.Unlock()
			if retained != 0 {
				t.Fatal("global clear retained a stale descriptor owner", retained)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalStoreSelfAdmission(t *testing.T) {
	compiled := rv64TableStageCompile(t, "mutable-funcref-store-self-admission", rv64MutableFuncrefStoreSelfModule(), true)
	defer compiled.Close()
	if !(rv64MutableFuncrefGlobalStoreSelf(compiled.executionView()) || rv64PrimitiveReferenceContract(compiled.executionView())) {
		t.Fatal("base mutable funcref self-store witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"code":            func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"function-export": func(v *Compiled) { v.Exports = map[string]int{"other": 0} },
		"global-export":   func(v *Compiled) { v.GlobalExports = map[string]int{"other": 0} },
		"immutable":       func(v *Compiled) { v.GlobalImports[0].Mutable = false; v.Globals[0].Mutable = false },
		"descriptor":      func(v *Compiled) { v.NeedsFuncRefDescs = false },
		"dynamic-escape":  func(v *Compiled) { v.dynamicFuncrefEscape = true },
		"signature": func(v *Compiled) {
			v.Funcs = slices.Clone(v.Funcs)
			v.Funcs[0].Results = []ValType{ValI32}
		},
		"function-type-id": func(v *Compiled) { v.FuncTypeID = slices.Clone(v.FuncTypeID); v.FuncTypeID[0] ^= 1 },
		"type-descriptor":  func(v *Compiled) { v.Types = slices.Clone(v.Types); v.Types[0].Final = false },
		"gc-descriptor":    func(v *Compiled) { v.GCTypeDescs = slices.Clone(v.GCTypeDescs); v.GCTypeDescs[0].Final = false },
		"entry":            func(v *Compiled) { v.Entry = []int{4} },
		"feature":          func(v *Compiled) { v.requiredFeatures &^= CoreFeatureMutableGlobal },
		"abi":              func(v *Compiled) { v.nativeTableReferenceABI = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *compiled.executionView()
			bad.GlobalImports = slices.Clone(bad.GlobalImports)
			bad.Globals = slices.Clone(bad.Globals)
			bad.Exports = maps.Clone(bad.Exports)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			mutate(&bad)
			if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile mutable self-store artifact admitted", err)
			}
		})
	}
}
