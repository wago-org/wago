//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

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

func rv64MutableFuncrefCopyModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "source", wasm.FuncRef, true),
			wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
		)),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x24, 1, 0x0b}))),
	)
}

func TestRV64PublicMutableFuncrefGlobalCopy(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			source, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			target, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
			if err != nil {
				t.Fatal(err)
			}
			defer target.Close()
			producerCode := rv64TableStageCompile(t, "mutable-funcref-copy-producer", rv64MutableFuncrefStoreSelfModule(), artifact)
			producerModule, err := rt.Module(producerCode)
			if err != nil {
				t.Fatal(err)
			}
			producer, err := rt.Instantiate(context.Background(), producerModule, WithImports(NewImports().Global("env", "cell", source)))
			producerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = producer.Invoke("store"); err != nil {
				t.Fatal("seed source", err)
			}
			descriptor := readGlobalObject(source, ValFuncRef)
			if _, ok := rv64PublishedDescriptor(producer, descriptor); !ok {
				t.Fatal("source descriptor is not fully published")
			}
			compiled := rv64TableStageCompile(t, "mutable-funcref-copy", rv64MutableFuncrefCopyModule(), artifact)
			view := compiled.executionView()
			if !rv64MutableFuncrefGlobalCopy(view) || !view.usesGuestGlobalStoreHelper() {
				t.Fatalf("mutable funcref copy witness missing: code=%d/%x globals=%+v imports=%+v exports=%v values=%+v desc=%t validation=%v", len(view.code), sha256.Sum256(view.code), view.Globals, view.GlobalImports, view.Exports, view.ValueTypes, view.NeedsFuncRefDescs, railshotValidateCompiled(view))
			}
			module, err := rt.Module(compiled)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), module, WithImports(NewImports().Global("env", "source", source).Global("env", "target", target)))
			if err != nil {
				module.Close()
				t.Fatal(err)
			}
			alias, err := rt.Instantiate(context.Background(), module, WithImports(NewImports().Global("env", "source", source).Global("env", "target", source)))
			module.Close()
			if err != nil {
				t.Fatal(err)
			}
			aliasBefore := producer.referenceLifetime().snapshot()
			if _, err = alias.Invoke("copy"); err != nil {
				t.Fatal("aliased guest copy", err)
			}
			if got := readGlobalObject(source, ValFuncRef); got != descriptor || producer.referenceLifetime().snapshot() != aliasBefore {
				t.Fatal("aliased guest copy changed value or root count", got, descriptor, aliasBefore, producer.referenceLifetime().snapshot())
			}
			if err = alias.Close(); err != nil {
				t.Fatal(err)
			}
			before := producer.referenceLifetime().snapshot()
			if err = target.copyGuestFuncrefDescriptor(consumer, descriptor+8); err == nil {
				t.Fatal("misaligned copied descriptor admitted")
			}
			if got := readGlobalObject(target, ValFuncRef); got != 0 || producer.referenceLifetime().snapshot() != before {
				t.Fatal("rejected copied descriptor changed target or ownership", got, before, producer.referenceLifetime().snapshot())
			}
			for iteration := 0; iteration < 2; iteration++ {
				if _, err = consumer.Invoke("copy"); err != nil {
					t.Fatal("guest copy", iteration, err)
				}
				if got := readGlobalObject(target, ValFuncRef); got != descriptor {
					t.Fatalf("guest copy descriptor = %#x, want %#x", got, descriptor)
				}
				after := producer.referenceLifetime().snapshot()
				if after.ResourceRoots != before.ResourceRoots+1 || !after.PhysicalResources || after.LogicallyClosed {
					t.Fatal("guest copy descriptor root drifted", before, after)
				}
				target.owner.mu.Lock()
				state, retained := target.owner.retained[producer]
				precise := retained && state != nil && state.precise && len(state.proxyDescriptors) == 0 && len(target.owner.retained) == 1
				target.owner.mu.Unlock()
				if !precise {
					t.Fatal("guest copy did not publish one precise owner root")
				}
			}
			if err = source.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
				t.Fatal("clear source", err)
			}
			beforeNullCopy := producer.referenceLifetime().snapshot()
			if _, err = consumer.Invoke("copy"); err != nil {
				t.Fatal("guest null copy", err)
			}
			if got := readGlobalObject(target, ValFuncRef); got != 0 {
				t.Fatal("guest null copy did not clear target", got)
			}
			afterNullCopy := producer.referenceLifetime().snapshot()
			if beforeNullCopy.ResourceRoots == 0 || afterNullCopy.ResourceRoots+1 != beforeNullCopy.ResourceRoots {
				t.Fatal("guest null copy did not retire one target root", beforeNullCopy, afterNullCopy)
			}
			target.owner.mu.Lock()
			retainedAfterNull := len(target.owner.retained)
			target.owner.mu.Unlock()
			if retainedAfterNull != 0 {
				t.Fatal("guest null copy retained a stale target root", retainedAfterNull)
			}
			if _, err = producer.Invoke("store"); err != nil {
				t.Fatal("reseed source", err)
			}
			if _, err = consumer.Invoke("copy"); err != nil {
				t.Fatal("recopy after null", err)
			}
			if err = source.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
				t.Fatal("clear reseeded source", err)
			}
			if err = producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err = producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err = consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err = compiled.Close(); err != nil {
				t.Fatal(err)
			}
			parked := producer.referenceLifetime().snapshot()
			if !parked.LogicallyClosed || !parked.PhysicalResources || parked.ResourceRoots != 1 {
				t.Fatal("target did not retain the closed descriptor owner", parked)
			}
			if _, ok := rv64PublishedDescriptor(producer, descriptor); !ok {
				t.Fatal("copied descriptor was unpublished before target overwrite")
			}
			if err = target.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
				t.Fatal("clear target", err)
			}
			retired := producer.referenceLifetime().snapshot()
			if !retired.LogicallyClosed || retired.PhysicalResources || retired.ResourceRoots != 0 {
				t.Fatal("target clear did not retire copied descriptor owner", retired)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalCopyAdmission(t *testing.T) {
	compiled := rv64TableStageCompile(t, "mutable-funcref-copy-admission", rv64MutableFuncrefCopyModule(), false)
	defer compiled.Close()
	if !rv64MutableFuncrefGlobalCopy(compiled.executionView()) {
		view := compiled.executionView()
		t.Fatalf("base mutable funcref copy witness rejected: code=%d/%x globals=%+v imports=%+v exports=%v values=%+v validation=%v", len(view.code), sha256.Sum256(view.code), view.Globals, view.GlobalImports, view.Exports, view.ValueTypes, railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"code":                func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"function-export":     func(v *Compiled) { v.Exports = map[string]int{"other": 0} },
		"extra-global-export": func(v *Compiled) { v.GlobalExports = map[string]int{"source": 0} },
		"import-name":         func(v *Compiled) { v.GlobalImports[0].Name = "other" },
		"immutable":           func(v *Compiled) { v.GlobalImports[1].Mutable = false; v.Globals[1].Mutable = false },
		"descriptor":          func(v *Compiled) { v.NeedsFuncRefDescs = true },
		"dynamic-escape":      func(v *Compiled) { v.dynamicFuncrefEscape = true },
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
				t.Fatal("hostile mutable copy artifact admitted", err)
			}
		})
	}
}
