//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64MutableFuncrefStateModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("cell", 3, 0))),
	)
}

func TestRV64PublicMutableFuncrefStateImport(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			producerCode := rv64TableStageCompile(t, "mutable-funcref-state-producer", rv64NativeTokenModule(false, true, true, false), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			value, err := producer.GlobalValue("reference")
			if err != nil || value.Type() != ValFuncRef || value.FuncRef().IsNull() {
				t.Fatal("producer token", value, err)
			}

			shared, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
			if err != nil {
				t.Fatal(err)
			}
			defer shared.Close()
			compiled := rv64TableStageCompile(t, "mutable-funcref-state-consumer", rv64MutableFuncrefStateModule(), artifact)
			defer compiled.Close()
			if !rv64StateOnlyMutableFuncrefImport(compiled.executionView()) {
				t.Fatalf("mutable funcref state witness missing: imports=%#v globals=%#v exports=%#v values=%#v features=%#x structural=%#x", compiled.GlobalImports, compiled.Globals, compiled.GlobalExports, compiled.ValueTypes, compiled.requiredFeatures, compiledStructuralRequiredFeatures(compiled))
			}
			rv64CheckStateOnlyCode(t, compiled, 0)
			module, err := rt.Module(compiled)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), module, WithImports(NewImports().Global("env", "cell", shared)))
			module.Close()
			if err != nil {
				t.Fatal(err)
			}
			rv64CheckStateOnlyCode(t, compiled, 1)
			exported, err := consumer.ExportedGlobalObject("cell")
			if err != nil || exported != shared || shared.owner.importers != 1 {
				t.Fatal("re-export identity", exported == shared, shared.owner.importers, err)
			}
			if got, err := consumer.GlobalValue("cell"); err != nil || !got.FuncRef().IsNull() {
				t.Fatal("initial null", got, err)
			}
			if err := exported.SetValue(value); err != nil {
				t.Fatal("set through re-export", err)
			}
			shared.owner.mu.Lock()
			retained := len(shared.owner.retained)
			shared.owner.mu.Unlock()
			if retained != 1 {
				t.Fatal("descriptor owner retention", retained)
			}
			if got, err := shared.GetValue(); err != nil || got != value {
				t.Fatal("shared mutation visibility", got, err)
			}
			if got, err := consumer.GlobalValue("cell"); err != nil || got != value {
				t.Fatal("consumer mutation visibility", got, err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal("logical producer close", err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal("producer code close", err)
			}
			if got, err := consumer.GlobalValue("cell"); err != nil || got != value {
				t.Fatal("retained value after producer close", got, err)
			}
			if err := shared.Close(); err == nil || !strings.Contains(err.Error(), "live importer") {
				t.Fatal("global close did not observe consumer", err)
			}
			if err := shared.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
				t.Fatal("clear shared value", err)
			}
			shared.owner.mu.Lock()
			retained = len(shared.owner.retained)
			shared.owner.mu.Unlock()
			if retained != 0 {
				t.Fatal("cleared descriptor owner retention", retained)
			}
			if got, err := consumer.GlobalValue("cell"); err != nil || !got.FuncRef().IsNull() {
				t.Fatal("cleared consumer value", got, err)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal("consumer close", err)
			}
			if shared.owner.importers != 0 {
				t.Fatal("consumer attachment leaked", shared.owner.importers)
			}
			if err := shared.Close(); err != nil {
				t.Fatal("global close", err)
			}
			rv64CheckStateOnlyCode(t, compiled, 0)
		})
	}
}

func TestRV64PublicMutableFuncrefStateAdmission(t *testing.T) {
	compiled := rv64TableStageCompile(t, "mutable-funcref-state-admission", rv64MutableFuncrefStateModule(), true)
	defer compiled.Close()
	if !rv64StateOnlyMutableFuncrefImport(compiled.executionView()) {
		t.Fatalf("base mutable funcref state witness rejected: imports=%#v globals=%#v exports=%#v values=%#v features=%#x structural=%#x types=%d funcs=%d funcids=%d code=%d", compiled.GlobalImports, compiled.Globals, compiled.GlobalExports, compiled.ValueTypes, compiled.requiredFeatures, compiledStructuralRequiredFeatures(compiled), len(compiled.Types), len(compiled.Funcs), len(compiled.FuncTypeID), len(compiled.code))
	}
	for name, mutate := range map[string]func(*Compiled){
		"immutable-import": func(v *Compiled) { v.GlobalImports[0].Mutable = false },
		"immutable-cell":   func(v *Compiled) { v.Globals[0].Mutable = false },
		"missing-export":   func(v *Compiled) { v.GlobalExports = nil },
		"extra-global":     func(v *Compiled) { v.Globals = append(v.Globals, GlobalDef{Type: ValFuncRef, Mutable: true}) },
		"initializer":      func(v *Compiled) { v.Globals[0].Bits = 1 },
		"missing-feature":  func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *compiled.executionView()
			bad.Globals = slices.Clone(bad.Globals)
			bad.GlobalImports = slices.Clone(bad.GlobalImports)
			bad.GlobalExports = map[string]int{"cell": 0}
			mutate(&bad)
			if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("invalid mutable state artifact admitted", err)
			}
			blob, err := marshalCompiled(&bad)
			if err == nil && name != "missing-feature" {
				loaded, loadErr := LoadTrustedArtifact(blob)
				if loaded != nil {
					loaded.Close()
				}
				if loadErr == nil {
					t.Fatal("invalid mutable state artifact loaded")
				}
			}
		})
	}
}

func TestRV64PublicMutableFuncrefStateBinding(t *testing.T) {
	compiled := rv64TableStageCompile(t, "mutable-funcref-state-binding", rv64MutableFuncrefStateModule(), true)
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
			if in, err := rt.Instantiate(context.Background(), module, WithImports(NewImports().Global("env", "cell", global))); err == nil || in != nil {
				if in != nil {
					in.Close()
				}
				t.Fatal("invalid binding admitted", err)
			}
			rv64CheckStateOnlyCode(t, compiled, 0)
		})
	}
}
