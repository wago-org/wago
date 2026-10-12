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

func rv64MutableFuncrefMoveModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "source", wasm.FuncRef, true),
			wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
		)),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("move", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x24, 1, 0xd0, 0x70, 0x24, 0, 0x0b}))),
	)
}

func TestRV64PublicMutableFuncrefGlobalMove(t *testing.T) {
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

			sourceCode := rv64TableStageCompile(t, "mutable-funcref-move-source", rv64MutableFuncrefStoreSelfModule(), artifact)
			sourceModule, err := rt.Module(sourceCode)
			if err != nil {
				t.Fatal(err)
			}
			sourceOwner, err := rt.Instantiate(context.Background(), sourceModule, WithImports(NewImports().Global("env", "cell", source)))
			sourceModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = sourceOwner.Invoke("store"); err != nil {
				t.Fatal("seed source", err)
			}
			sourceDescriptor := readGlobalObject(source, ValFuncRef)

			targetCode := rv64TableStageCompile(t, "mutable-funcref-move-target", rv64MutableFuncrefStoreSelfModule(), artifact)
			targetModule, err := rt.Module(targetCode)
			if err != nil {
				t.Fatal(err)
			}
			targetOwner, err := rt.Instantiate(context.Background(), targetModule, WithImports(NewImports().Global("env", "cell", target)))
			targetModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = targetOwner.Invoke("store"); err != nil {
				t.Fatal("seed target", err)
			}
			targetDescriptor := readGlobalObject(target, ValFuncRef)
			if sourceDescriptor == 0 || targetDescriptor == 0 || sourceDescriptor == targetDescriptor {
				t.Fatal("move fixture descriptors are not distinct", sourceDescriptor, targetDescriptor)
			}

			compiled := rv64TableStageCompile(t, "mutable-funcref-move", rv64MutableFuncrefMoveModule(), artifact)
			view := compiled.executionView()
			if !rv64MutableFuncrefGlobalMove(view) || !view.usesGuestGlobalStoreHelper() {
				t.Fatalf("mutable funcref move witness missing: code=%d/%x globals=%+v imports=%+v exports=%v validation=%v", len(view.code), sha256.Sum256(view.code), view.Globals, view.GlobalImports, view.Exports, railshotValidateCompiled(view))
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

			aliasBefore := sourceOwner.referenceLifetime().snapshot()
			if _, err = alias.Invoke("move"); err != nil {
				t.Fatal("aliased guest move", err)
			}
			aliasAfter := sourceOwner.referenceLifetime().snapshot()
			if got := readGlobalObject(source, ValFuncRef); got != 0 || aliasBefore.ResourceRoots == 0 || aliasAfter.ResourceRoots+1 != aliasBefore.ResourceRoots {
				t.Fatal("aliased move did not clear one owned cell", got, aliasBefore, aliasAfter)
			}
			if err = alias.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err = sourceOwner.Invoke("store"); err != nil {
				t.Fatal("reseed source", err)
			}

			sourceBefore := sourceOwner.referenceLifetime().snapshot()
			targetBefore := targetOwner.referenceLifetime().snapshot()
			if _, err = consumer.Invoke("move"); err != nil {
				t.Fatal("guest move", err)
			}
			if got := readGlobalObject(source, ValFuncRef); got != 0 {
				t.Fatal("guest move did not clear source", got)
			}
			if got := readGlobalObject(target, ValFuncRef); got != sourceDescriptor {
				t.Fatal("guest move target descriptor", got, sourceDescriptor)
			}
			sourceAfter := sourceOwner.referenceLifetime().snapshot()
			targetAfter := targetOwner.referenceLifetime().snapshot()
			if sourceAfter.ResourceRoots != sourceBefore.ResourceRoots || targetBefore.ResourceRoots == 0 || targetAfter.ResourceRoots+1 != targetBefore.ResourceRoots {
				t.Fatal("guest move did not hand off roots exactly", sourceBefore, sourceAfter, targetBefore, targetAfter)
			}
			target.owner.mu.Lock()
			state, retained := target.owner.retained[sourceOwner]
			precise := retained && state != nil && state.precise && len(state.proxyDescriptors) == 0 && len(target.owner.retained) == 1
			target.owner.mu.Unlock()
			if !precise {
				t.Fatal("guest move did not publish one precise target root")
			}

			beforeNullMove := sourceOwner.referenceLifetime().snapshot()
			if _, err = consumer.Invoke("move"); err != nil {
				t.Fatal("guest null move", err)
			}
			afterNullMove := sourceOwner.referenceLifetime().snapshot()
			if readGlobalObject(source, ValFuncRef) != 0 || readGlobalObject(target, ValFuncRef) != 0 || beforeNullMove.ResourceRoots == 0 || afterNullMove.ResourceRoots+1 != beforeNullMove.ResourceRoots {
				t.Fatal("guest null move did not clear target", beforeNullMove, afterNullMove)
			}
			if _, err = sourceOwner.Invoke("store"); err != nil {
				t.Fatal("final source seed", err)
			}
			if _, err = consumer.Invoke("move"); err != nil {
				t.Fatal("final guest move", err)
			}

			if err = targetOwner.Close(); err != nil {
				t.Fatal(err)
			}
			if err = targetCode.Close(); err != nil {
				t.Fatal(err)
			}
			retiredTarget := targetOwner.referenceLifetime().snapshot()
			if !retiredTarget.LogicallyClosed || retiredTarget.PhysicalResources || retiredTarget.ResourceRoots != 0 {
				t.Fatal("overwritten target owner did not retire", retiredTarget)
			}
			if err = sourceOwner.Close(); err != nil {
				t.Fatal(err)
			}
			if err = sourceCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err = consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err = compiled.Close(); err != nil {
				t.Fatal(err)
			}
			parked := sourceOwner.referenceLifetime().snapshot()
			if !parked.LogicallyClosed || !parked.PhysicalResources || parked.ResourceRoots != 1 {
				t.Fatal("move target did not retain closed source owner", parked)
			}
			if err = target.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
				t.Fatal("clear move target", err)
			}
			retiredSource := sourceOwner.referenceLifetime().snapshot()
			if !retiredSource.LogicallyClosed || retiredSource.PhysicalResources || retiredSource.ResourceRoots != 0 {
				t.Fatal("move target clear did not retire source owner", retiredSource)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalMoveAdmission(t *testing.T) {
	compiled := rv64TableStageCompile(t, "mutable-funcref-move-admission", rv64MutableFuncrefMoveModule(), false)
	defer compiled.Close()
	if !rv64MutableFuncrefGlobalMove(compiled.executionView()) {
		view := compiled.executionView()
		t.Fatalf("base mutable funcref move witness rejected: code=%d/%x globals=%+v imports=%+v exports=%v validation=%v", len(view.code), sha256.Sum256(view.code), view.Globals, view.GlobalImports, view.Exports, railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"code":             func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"function-export":  func(v *Compiled) { v.Exports = map[string]int{"other": 0} },
		"global-export":    func(v *Compiled) { v.GlobalExports = map[string]int{"target": 1} },
		"import-name":      func(v *Compiled) { v.GlobalImports[1].Name = "other" },
		"immutable":        func(v *Compiled) { v.GlobalImports[0].Mutable = false; v.Globals[0].Mutable = false },
		"descriptor":       func(v *Compiled) { v.NeedsFuncRefDescs = true },
		"dynamic-escape":   func(v *Compiled) { v.dynamicFuncrefEscape = true },
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
				t.Fatal("hostile mutable move artifact admitted", err)
			}
		})
	}
}
