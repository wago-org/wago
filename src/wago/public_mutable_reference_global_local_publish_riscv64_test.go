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

func rv64MutableFuncrefLocalPublishModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{4, 0, 0, 0, 0}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("set_local", 0, 1),
			wasmtest.ExportEntry("clear_local", 0, 2),
			wasmtest.ExportEntry("publish", 0, 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 1, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x24, 1, 0x0b}),
			wasmtest.Code([]byte{0x23, 1, 0x24, 0, 0x0b}),
		)),
	)
}

func TestRV64PublicMutableFuncrefGlobalLocalPublish(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			target, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
			if err != nil {
				t.Fatal(err)
			}
			defer target.Close()
			compiled := rv64TableStageCompile(t, "mutable-funcref-local-publish", rv64MutableFuncrefLocalPublishModule(), artifact)
			view := compiled.executionView()
			t.Logf("local-publish finalized image: code=%d digest=%x entries=%v/%v", len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			if !rv64MutableFuncrefGlobalLocalPublish(view) || !view.usesGuestGlobalStoreHelper() {
				t.Fatalf("mutable funcref local-publish witness missing: code=%d/%x globals=%+v imports=%+v exports=%v validation=%v", len(view.code), sha256.Sum256(view.code), view.Globals, view.GlobalImports, view.Exports, railshotValidateCompiled(view))
			}
			module, err := rt.Module(compiled)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), module, WithImports(NewImports().Global("env", "target", target)))
			module.Close()
			if err != nil {
				t.Fatal(err)
			}
			descriptor, ok := consumer.localFuncrefDescriptor(0)
			if !ok || descriptor == 0 || readGlobalObject(consumer.globalCells[1], ValFuncRef) != 0 {
				t.Fatal("local mutable cell did not start null", descriptor, ok)
			}
			before := consumer.referenceLifetime().snapshot()
			if _, err = consumer.Invoke("publish"); err != nil || readGlobalObject(target, ValFuncRef) != 0 {
				t.Fatal("initial null publish", err)
			}
			if _, err = consumer.Invoke("set_local"); err != nil {
				t.Fatal("set private local", err)
			}
			if got := readGlobalObject(consumer.globalCells[1], ValFuncRef); got != descriptor {
				t.Fatalf("private local descriptor = %#x, want %#x", got, descriptor)
			}
			for iteration := 0; iteration < 2; iteration++ {
				if _, err = consumer.Invoke("publish"); err != nil {
					t.Fatal("publish private local", iteration, err)
				}
				if got := readGlobalObject(target, ValFuncRef); got != descriptor {
					t.Fatalf("published descriptor = %#x, want %#x", got, descriptor)
				}
				after := consumer.referenceLifetime().snapshot()
				if after.ResourceRoots != before.ResourceRoots+1 || !after.PhysicalResources || after.LogicallyClosed {
					t.Fatal("published descriptor root drifted", before, after)
				}
			}
			if _, err = consumer.Invoke("clear_local"); err != nil {
				t.Fatal("clear private local", err)
			}
			if readGlobalObject(consumer.globalCells[1], ValFuncRef) != 0 || readGlobalObject(target, ValFuncRef) != descriptor {
				t.Fatal("private clear unexpectedly changed published target")
			}
			if _, err = consumer.Invoke("publish"); err != nil {
				t.Fatal("publish private null", err)
			}
			if readGlobalObject(target, ValFuncRef) != 0 {
				t.Fatal("published null did not clear target")
			}
			if after := consumer.referenceLifetime().snapshot(); after.ResourceRoots != before.ResourceRoots || !after.PhysicalResources || after.LogicallyClosed {
				t.Fatal("null publish did not release target root", before, after)
			}
			if _, err = consumer.Invoke("set_local"); err != nil {
				t.Fatal(err)
			}
			if _, err = consumer.Invoke("publish"); err != nil {
				t.Fatal(err)
			}
			if err = consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err = compiled.Close(); err != nil {
				t.Fatal(err)
			}
			parked := consumer.referenceLifetime().snapshot()
			if !parked.LogicallyClosed || !parked.PhysicalResources || parked.ResourceRoots != before.ResourceRoots+1 {
				t.Fatal("target did not retain closed local-publish owner", parked)
			}
			if err = target.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
				t.Fatal("clear target after owner close", err)
			}
			retired := consumer.referenceLifetime().snapshot()
			if !retired.LogicallyClosed || retired.PhysicalResources || retired.ResourceRoots != 0 {
				t.Fatal("target clear did not retire local-publish owner", retired)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalPublishAdmission(t *testing.T) {
	compiled := rv64TableStageCompile(t, "mutable-funcref-local-publish-admission", rv64MutableFuncrefLocalPublishModule(), false)
	defer compiled.Close()
	if !rv64MutableFuncrefGlobalLocalPublish(compiled.executionView()) {
		t.Fatal("base mutable funcref local-publish witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"function-export":  func(v *Compiled) { v.Exports = map[string]int{"other": 0} },
		"global-export":    func(v *Compiled) { v.GlobalExports = map[string]int{"cell": 1} },
		"import-name":      func(v *Compiled) { v.GlobalImports[0].Name = "other" },
		"import-immutable": func(v *Compiled) { v.GlobalImports[0].Mutable = false; v.Globals[0].Mutable = false },
		"local-immutable":  func(v *Compiled) { v.Globals[1].Mutable = false },
		"local-nonnull":    func(v *Compiled) { v.Globals[1].HasInitFunc = true },
		"descriptor":       func(v *Compiled) { v.NeedsFuncRefDescs = false },
		"dynamic-escape":   func(v *Compiled) { v.dynamicFuncrefEscape = true },
		"signature":        func(v *Compiled) { v.Funcs = slices.Clone(v.Funcs); v.Funcs[3].Results = []ValType{ValI32} },
		"function-type-id": func(v *Compiled) { v.FuncTypeID = slices.Clone(v.FuncTypeID); v.FuncTypeID[2] ^= 1 },
		"type-descriptor":  func(v *Compiled) { v.Types = slices.Clone(v.Types); v.Types[0].Final = false },
		"gc-descriptor":    func(v *Compiled) { v.GCTypeDescs = slices.Clone(v.GCTypeDescs); v.GCTypeDescs[0].Final = false },
		"entry":            func(v *Compiled) { v.Entry = slices.Clone(v.Entry); v.Entry[3] += 4 },
		"internal-entry":   func(v *Compiled) { v.InternalEntry = slices.Clone(v.InternalEntry); v.InternalEntry[3] += 4 },
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
				t.Fatal("hostile mutable local-publish artifact admitted", err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalPublishBinding(t *testing.T) {
	compiled := rv64TableStageCompile(t, "mutable-funcref-local-publish-binding", rv64MutableFuncrefLocalPublishModule(), true)
	defer compiled.Close()
	rt := NewRuntime()
	defer rt.Close()
	immutable, err := rt.NewFuncRefGlobal(NullFuncRef(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer immutable.Close()
	foreign := NewRuntime()
	defer foreign.Close()
	foreignGlobal, err := foreign.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer foreignGlobal.Close()
	closed, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	for name, global := range map[string]*Global{"immutable": immutable, "foreign-runtime": foreignGlobal, "closed": closed} {
		t.Run(name, func(t *testing.T) {
			module, err := rt.Module(compiled)
			if err != nil {
				t.Fatal(err)
			}
			defer module.Close()
			if in, err := rt.Instantiate(context.Background(), module, WithImports(NewImports().Global("env", "target", global))); err == nil || in != nil {
				if in != nil {
					in.Close()
				}
				t.Fatal("invalid local-publish binding admitted", err)
			}
			if !rv64MutableFuncrefGlobalLocalPublish(compiled.executionView()) {
				t.Fatal("failed binding altered local-publish code product")
			}
		})
	}
}
