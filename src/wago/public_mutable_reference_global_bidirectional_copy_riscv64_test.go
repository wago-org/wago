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

func rv64MutableFuncrefBidirectionalCopyModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "source", wasm.FuncRef, true),
			wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
		)),
		wasmtest.Section(3, []byte{2, 0, 0}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("copy_to_target", 0, 0),
			wasmtest.ExportEntry("copy_to_source", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0, 0x24, 1, 0x0b}),
			wasmtest.Code([]byte{0x23, 1, 0x24, 0, 0x0b}),
		)),
	)
}

func TestRV64PublicMutableFuncrefGlobalBidirectionalCopy(t *testing.T) {
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

			producerCode := rv64TableStageCompile(t, "mutable-funcref-bidirectional-producer", rv64MutableFuncrefStoreSelfModule(), artifact)
			producerModule, err := rt.Module(producerCode)
			if err != nil {
				t.Fatal(err)
			}
			sourceOwner, err := rt.Instantiate(context.Background(), producerModule, WithImports(NewImports().Global("env", "cell", source)))
			if err != nil {
				producerModule.Close()
				t.Fatal(err)
			}
			targetOwner, err := rt.Instantiate(context.Background(), producerModule, WithImports(NewImports().Global("env", "cell", target)))
			producerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = sourceOwner.Invoke("store"); err != nil {
				t.Fatal("seed source", err)
			}
			if _, err = targetOwner.Invoke("store"); err != nil {
				t.Fatal("seed target", err)
			}
			sourceDescriptor := readGlobalObject(source, ValFuncRef)
			targetDescriptor := readGlobalObject(target, ValFuncRef)
			if sourceDescriptor == 0 || targetDescriptor == 0 || sourceDescriptor == targetDescriptor {
				t.Fatal("bidirectional fixture descriptors are not distinct", sourceDescriptor, targetDescriptor)
			}

			compiled := rv64TableStageCompile(t, "mutable-funcref-bidirectional-copy", rv64MutableFuncrefBidirectionalCopyModule(), artifact)
			view := compiled.executionView()
			if !rv64MutableFuncrefGlobalBidirectionalCopy(view) || !view.usesGuestGlobalStoreHelper() {
				t.Fatalf("bidirectional witness missing: code=%d/%x entries=%v/%v globals=%+v imports=%+v exports=%v validation=%v", len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.Globals, view.GlobalImports, view.Exports, railshotValidateCompiled(view))
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
			beforeForgedIndex := sourceOwner.referenceLifetime().snapshot()
			forgedIndexPanicked := false
			func() {
				defer func() {
					forgedIndexPanicked = recover() != nil
				}()
				consumer.dispatchGuestGlobalStore(hostFuncRefDispatchBit, []uint64{sourceDescriptor, 2}, nil)
			}()
			if !forgedIndexPanicked {
				t.Fatal("forged indexed guest store did not panic")
			}
			if readGlobalObject(source, ValFuncRef) != sourceDescriptor || readGlobalObject(target, ValFuncRef) != targetDescriptor || sourceOwner.referenceLifetime().snapshot() != beforeForgedIndex {
				t.Fatal("forged indexed guest store changed values or roots")
			}

			sourceBefore := sourceOwner.referenceLifetime().snapshot()
			targetBefore := targetOwner.referenceLifetime().snapshot()
			if _, err = consumer.Invoke("copy_to_target"); err != nil {
				t.Fatal("forward copy", err)
			}
			if got := readGlobalObject(target, ValFuncRef); got != sourceDescriptor {
				t.Fatal("forward copy target descriptor", got, sourceDescriptor)
			}
			sourceAfter := sourceOwner.referenceLifetime().snapshot()
			targetAfter := targetOwner.referenceLifetime().snapshot()
			if sourceAfter.ResourceRoots != sourceBefore.ResourceRoots+1 || targetBefore.ResourceRoots == 0 || targetAfter.ResourceRoots+1 != targetBefore.ResourceRoots {
				t.Fatal("forward copy root transfer drifted", sourceBefore, sourceAfter, targetBefore, targetAfter)
			}
			if _, err = consumer.Invoke("copy_to_target"); err != nil || sourceOwner.referenceLifetime().snapshot() != sourceAfter {
				t.Fatal("repeated forward copy changed roots", err, sourceAfter, sourceOwner.referenceLifetime().snapshot())
			}

			if _, err = targetOwner.Invoke("store"); err != nil {
				t.Fatal("reseed target", err)
			}
			sourceBefore = sourceOwner.referenceLifetime().snapshot()
			targetBefore = targetOwner.referenceLifetime().snapshot()
			if _, err = consumer.Invoke("copy_to_source"); err != nil {
				t.Fatal("reverse copy", err)
			}
			if got := readGlobalObject(source, ValFuncRef); got != targetDescriptor {
				t.Fatal("reverse copy source descriptor", got, targetDescriptor)
			}
			sourceAfter = sourceOwner.referenceLifetime().snapshot()
			targetAfter = targetOwner.referenceLifetime().snapshot()
			if sourceBefore.ResourceRoots == 0 || sourceAfter.ResourceRoots+1 != sourceBefore.ResourceRoots || targetAfter.ResourceRoots != targetBefore.ResourceRoots+1 {
				t.Fatal("reverse copy root transfer drifted", sourceBefore, sourceAfter, targetBefore, targetAfter)
			}
			aliasBefore := targetOwner.referenceLifetime().snapshot()
			if _, err = alias.Invoke("copy_to_target"); err != nil {
				t.Fatal("aliased forward copy", err)
			}
			if _, err = alias.Invoke("copy_to_source"); err != nil {
				t.Fatal("aliased reverse copy", err)
			}
			if got := targetOwner.referenceLifetime().snapshot(); got != aliasBefore || readGlobalObject(source, ValFuncRef) != targetDescriptor {
				t.Fatal("aliased bidirectional copy changed value or roots", aliasBefore, got)
			}
			if err = alias.Close(); err != nil {
				t.Fatal(err)
			}

			if err = source.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
				t.Fatal("clear source", err)
			}
			beforeNull := targetOwner.referenceLifetime().snapshot()
			if _, err = consumer.Invoke("copy_to_target"); err != nil {
				t.Fatal("forward null copy", err)
			}
			afterNull := targetOwner.referenceLifetime().snapshot()
			if readGlobalObject(target, ValFuncRef) != 0 || beforeNull.ResourceRoots == 0 || afterNull.ResourceRoots+1 != beforeNull.ResourceRoots {
				t.Fatal("forward null copy did not retire target", beforeNull, afterNull)
			}
			if _, err = consumer.Invoke("copy_to_source"); err != nil || readGlobalObject(source, ValFuncRef) != 0 {
				t.Fatal("reverse null copy changed null state", err)
			}

			if _, err = sourceOwner.Invoke("store"); err != nil {
				t.Fatal("final source seed", err)
			}
			if _, err = consumer.Invoke("copy_to_target"); err != nil {
				t.Fatal("final forward copy", err)
			}
			if err = source.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
				t.Fatal("clear final source", err)
			}
			if err = targetOwner.Close(); err != nil {
				t.Fatal(err)
			}
			if err = sourceOwner.Close(); err != nil {
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
			parked := sourceOwner.referenceLifetime().snapshot()
			if !parked.LogicallyClosed || !parked.PhysicalResources || parked.ResourceRoots != 1 {
				t.Fatal("bidirectional target did not retain closed source owner", parked)
			}
			if err = target.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
				t.Fatal("clear retained target", err)
			}
			retired := sourceOwner.referenceLifetime().snapshot()
			if !retired.LogicallyClosed || retired.PhysicalResources || retired.ResourceRoots != 0 {
				t.Fatal("retained target clear did not retire source owner", retired)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalBidirectionalCopyAdmission(t *testing.T) {
	compiled := rv64TableStageCompile(t, "mutable-funcref-bidirectional-copy-admission", rv64MutableFuncrefBidirectionalCopyModule(), false)
	defer compiled.Close()
	if !rv64MutableFuncrefGlobalBidirectionalCopy(compiled.executionView()) {
		view := compiled.executionView()
		t.Fatalf("base bidirectional copy witness rejected: code=%d/%x entries=%v/%v validation=%v", len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"code":          func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"first-export":  func(v *Compiled) { v.Exports = map[string]int{"other": 0, "copy_to_source": 1} },
		"second-export": func(v *Compiled) { v.Exports = map[string]int{"copy_to_target": 0, "other": 1} },
		"global-export": func(v *Compiled) { v.GlobalExports = map[string]int{"target": 1} },
		"import-name":   func(v *Compiled) { v.GlobalImports[1].Name = "other" },
		"immutable":     func(v *Compiled) { v.GlobalImports[0].Mutable = false; v.Globals[0].Mutable = false },
		"second-signature": func(v *Compiled) {
			v.Funcs = slices.Clone(v.Funcs)
			v.Funcs[1].Results = []ValType{ValI32}
		},
		"function-type-id": func(v *Compiled) { v.FuncTypeID = slices.Clone(v.FuncTypeID); v.FuncTypeID[1] ^= 1 },
		"entry":            func(v *Compiled) { v.Entry = []int{0, 676} },
		"internal-entry":   func(v *Compiled) { v.InternalEntry = []int{0, 676} },
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
				t.Fatal("hostile bidirectional-copy artifact admitted", err)
			}
		})
	}
}
