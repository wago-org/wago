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

func rv64MutableFuncrefLocalTransferModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "source", wasm.FuncRef, true),
			wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
		)),
		wasmtest.Section(3, []byte{3, 0, 0, 0}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("capture", 0, 0),
			wasmtest.ExportEntry("publish", 0, 1),
			wasmtest.ExportEntry("clear_local", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0, 0x24, 2, 0x0b}),
			wasmtest.Code([]byte{0x23, 2, 0x24, 1, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x24, 2, 0x0b}),
		)),
	)
}

func TestRV64PublicMutableFuncrefGlobalLocalTransfer(t *testing.T) {
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

			producerCode := rv64TableStageCompile(t, "mutable-funcref-local-transfer-producer", rv64MutableFuncrefStoreSelfModule(), artifact)
			producerModule, err := rt.Module(producerCode)
			if err != nil {
				t.Fatal(err)
			}
			producerA, err := rt.Instantiate(context.Background(), producerModule, WithImports(NewImports().Global("env", "cell", source)))
			if err != nil {
				producerModule.Close()
				t.Fatal(err)
			}
			producerB, err := rt.Instantiate(context.Background(), producerModule, WithImports(NewImports().Global("env", "cell", source)))
			producerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = producerA.Invoke("store"); err != nil {
				t.Fatal("seed source", err)
			}
			descriptorA := readGlobalObject(source, ValFuncRef)
			if descriptorA == 0 {
				t.Fatal("producer did not seed source")
			}

			compiled := rv64TableStageCompile(t, "mutable-funcref-local-transfer", rv64MutableFuncrefLocalTransferModule(), artifact)
			view := compiled.executionView()
			if !rv64MutableFuncrefGlobalLocalTransfer(view) || !view.usesGuestGlobalStoreHelper() {
				t.Fatalf("local-transfer witness missing: code=%d/%x entries=%v/%v globals=%+v imports=%+v exports=%v validation=%v", len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.Globals, view.GlobalImports, view.Exports, railshotValidateCompiled(view))
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
			closingConsumer, err := rt.Instantiate(context.Background(), module, WithImports(NewImports().Global("env", "source", source).Global("env", "target", target)))
			module.Close()
			if err != nil {
				t.Fatal(err)
			}
			if err = closingConsumer.beginInvocation(); err != nil {
				t.Fatal("admit parked close regression", err)
			}
			closingInvocation := true
			defer func() {
				if closingInvocation {
					closingConsumer.endInvocation()
				}
			}()
			if err = closingConsumer.Close(); err != nil {
				t.Fatal("logically close parked consumer", err)
			}
			closePanicked := false
			func() {
				defer func() { closePanicked = recover() != nil }()
				closingConsumer.dispatchGuestGlobalStore(hostFuncRefDispatchBit, []uint64{descriptorA, 2}, nil)
			}()
			if closePanicked || readGlobalObject(closingConsumer.globalCells[2], ValFuncRef) != descriptorA {
				t.Fatal("parked in-flight private write failed after logical close")
			}
			closingInvocation = false
			closingConsumer.endInvocation()
			if err = closingConsumer.WaitClosed(context.Background()); err != nil {
				t.Fatal("complete parked consumer close", err)
			}
			if readGlobalObject(consumer.globalCells[2], ValFuncRef) != 0 || consumer.globalCells[2].owner != nil {
				t.Fatal("private transfer cell did not start unowned and null")
			}
			forgedBefore := producerA.referenceLifetime().snapshot()
			forgedPanicked := false
			func() {
				defer func() { forgedPanicked = recover() != nil }()
				consumer.dispatchGuestGlobalStore(hostFuncRefDispatchBit, []uint64{descriptorA, 0}, nil)
			}()
			if !forgedPanicked || consumer.globalCells[2].owner != nil || readGlobalObject(source, ValFuncRef) != descriptorA || readGlobalObject(target, ValFuncRef) != 0 || producerA.referenceLifetime().snapshot() != forgedBefore {
				t.Fatal("forged local-transfer destination changed ownership or values")
			}
			before := producerA.referenceLifetime().snapshot()
			if _, err = consumer.Invoke("capture"); err != nil {
				t.Fatal("capture foreign descriptor", err)
			}
			if got := readGlobalObject(consumer.globalCells[2], ValFuncRef); got != descriptorA || consumer.globalCells[2].instanceOwner() != consumer {
				t.Fatal("private cell did not capture descriptor with consumer ownership", got, descriptorA)
			}
			afterCapture := producerA.referenceLifetime().snapshot()
			if afterCapture.ResourceRoots != before.ResourceRoots+1 {
				t.Fatal("private capture did not retain one provider root", before, afterCapture)
			}
			if _, err = producerB.Invoke("store"); err != nil {
				t.Fatal("replace imported source provider", err)
			}
			descriptorB := readGlobalObject(source, ValFuncRef)
			if descriptorB == 0 || descriptorB == descriptorA {
				t.Fatal("replacement provider did not publish a distinct descriptor", descriptorA, descriptorB)
			}
			aBeforeReplace := producerA.referenceLifetime().snapshot()
			bBeforeReplace := producerB.referenceLifetime().snapshot()
			if _, err = consumer.Invoke("capture"); err != nil {
				t.Fatal("replace private provider", err)
			}
			if got := readGlobalObject(consumer.globalCells[2], ValFuncRef); got != descriptorB {
				t.Fatal("private cell did not replace captured provider", got, descriptorB)
			}
			aAfterReplace := producerA.referenceLifetime().snapshot()
			bAfterReplace := producerB.referenceLifetime().snapshot()
			if aBeforeReplace.ResourceRoots == 0 || aAfterReplace.ResourceRoots+1 != aBeforeReplace.ResourceRoots || bAfterReplace.ResourceRoots != bBeforeReplace.ResourceRoots+1 {
				t.Fatal("private provider replacement did not transfer one root", aBeforeReplace, aAfterReplace, bBeforeReplace, bAfterReplace)
			}
			if err = producerA.Close(); err != nil {
				t.Fatal("close replaced provider", err)
			}
			if replaced := producerA.referenceLifetime().snapshot(); !replaced.LogicallyClosed || replaced.PhysicalResources || replaced.ResourceRoots != 0 {
				t.Fatal("replaced provider remained retained", replaced)
			}
			if err = source.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
				t.Fatal("clear imported source", err)
			}
			if got := producerB.referenceLifetime().snapshot(); got.ResourceRoots+1 != bAfterReplace.ResourceRoots || got.ResourceRoots == 0 {
				t.Fatal("private cell did not become sole captured owner", bAfterReplace, got)
			}
			beforePublish := producerB.referenceLifetime().snapshot()
			if _, err = consumer.Invoke("publish"); err != nil {
				t.Fatal("publish captured descriptor", err)
			}
			if readGlobalObject(target, ValFuncRef) != descriptorB || producerB.referenceLifetime().snapshot().ResourceRoots != beforePublish.ResourceRoots+1 {
				t.Fatal("publish did not transfer an independent target root")
			}
			if _, err = consumer.Invoke("clear_local"); err != nil {
				t.Fatal("clear private cell", err)
			}
			if readGlobalObject(consumer.globalCells[2], ValFuncRef) != 0 || readGlobalObject(target, ValFuncRef) != descriptorB || producerB.referenceLifetime().snapshot().ResourceRoots != beforePublish.ResourceRoots {
				t.Fatal("private clear did not retire only the private root")
			}
			if err = producerB.Close(); err != nil {
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
			parked := producerB.referenceLifetime().snapshot()
			if !parked.LogicallyClosed || !parked.PhysicalResources || parked.ResourceRoots != 1 {
				t.Fatal("target did not retain closed provider", parked)
			}
			if err = target.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
				t.Fatal("clear retained target", err)
			}
			retired := producerB.referenceLifetime().snapshot()
			if !retired.LogicallyClosed || retired.PhysicalResources || retired.ResourceRoots != 0 {
				t.Fatal("target clear did not retire provider", retired)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalTransferAdmission(t *testing.T) {
	compiled := rv64TableStageCompile(t, "mutable-funcref-local-transfer-admission", rv64MutableFuncrefLocalTransferModule(), false)
	defer compiled.Close()
	if !rv64MutableFuncrefGlobalLocalTransfer(compiled.executionView()) {
		t.Fatal("base local-transfer witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"code":          func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"export":        func(v *Compiled) { v.Exports = map[string]int{"capture": 0, "publish": 1, "other": 2} },
		"global-export": func(v *Compiled) { v.GlobalExports = map[string]int{"cell": 2} },
		"import-name":   func(v *Compiled) { v.GlobalImports[0].Name = "other" },
		"immutable":     func(v *Compiled) { v.Globals[2].Mutable = false },
		"entry":         func(v *Compiled) { v.Entry = []int{0, 288, 580} },
		"feature":       func(v *Compiled) { v.requiredFeatures &^= CoreFeatureMutableGlobal },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *compiled.executionView()
			bad.GlobalImports = slices.Clone(bad.GlobalImports)
			bad.Globals = slices.Clone(bad.Globals)
			bad.Exports = maps.Clone(bad.Exports)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			mutate(&bad)
			if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile local-transfer artifact admitted", err)
			}
		})
	}
}
