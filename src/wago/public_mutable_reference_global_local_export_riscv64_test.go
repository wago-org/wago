//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64MutableFuncrefLocalExportModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{5, 0, 0, 0, 0, 0}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0),
			wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2),
			wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x24, 0, 0x0b}),
		)),
	)
}

func TestRV64PublicMutableFuncrefGlobalLocalExport(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()

			producerCode := rv64TableStageCompile(t, "mutable-funcref-local-export-producer", rv64ReferenceGlobalExportModule(t, false, false, false, 1), artifact)
			producerModule, err := rt.Module(producerCode)
			if err != nil {
				t.Fatal(err)
			}
			producer, err := rt.Instantiate(context.Background(), producerModule)
			producerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			foreign, err := producer.GlobalValue("reference")
			if err != nil || foreign.FuncRef().IsNull() {
				t.Fatal("foreign producer token", foreign, err)
			}

			compiled := rv64TableStageCompile(t, "mutable-funcref-local-export", rv64MutableFuncrefLocalExportModule(), artifact)
			view := compiled.executionView()
			if !(rv64MutableFuncrefGlobalLocalExport(view) || rv64PrimitiveReferenceContract(view)) || !view.usesGuestGlobalStoreHelper() {
				t.Fatalf("local-export witness missing: code=%d/%x entries=%v/%v globals=%+v exports=%v/%v validation=%v", len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.Globals, view.Exports, view.GlobalExports, railshotValidateCompiled(view))
			}
			module, err := rt.Module(compiled)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), module)
			module.Close()
			if err != nil {
				t.Fatal(err)
			}
			if got, err := consumer.GlobalValue("cell"); err != nil || !got.FuncRef().IsNull() {
				t.Fatal("exported cell did not start null", got, err)
			}
			before := producer.referenceLifetime().snapshot()
			if err = consumer.SetGlobalValue("cell", foreign); err != nil {
				t.Fatal("install foreign descriptor through host export", err)
			}
			if consumer.globalCells[0].instanceOwner() != consumer {
				t.Fatal("host ingress did not install consumer container ownership")
			}
			afterInstall := producer.referenceLifetime().snapshot()
			if afterInstall.ResourceRoots != before.ResourceRoots+1 {
				t.Fatal("host ingress did not retain one foreign owner root", before, afterInstall)
			}
			if err = producer.Close(); err != nil {
				t.Fatal(err)
			}
			parked := producer.referenceLifetime().snapshot()
			if !parked.LogicallyClosed || !parked.PhysicalResources || parked.ResourceRoots < 2 {
				t.Fatal("exported cell did not retain closed foreign producer", parked)
			}
			if _, err = consumer.Invoke("set_0"); err != nil {
				t.Fatal("replace foreign descriptor with local zero", err)
			}
			local0, err := consumer.GlobalValue("cell")
			if err != nil || local0.FuncRef().IsNull() || !consumer.FuncRefMatchesFunction(local0.FuncRef(), 0) {
				t.Fatal("local function zero was not published", local0, err)
			}
			if cellRetired := producer.referenceLifetime().snapshot(); !cellRetired.LogicallyClosed || !cellRetired.PhysicalResources || cellRetired.ResourceRoots+1 != parked.ResourceRoots {
				t.Fatal("local replacement did not retire exactly the exported-cell root", parked, cellRetired)
			}
			if _, err = consumer.Invoke("set_1"); err != nil {
				t.Fatal("replace local descriptor", err)
			}
			local1, err := consumer.GlobalValue("cell")
			if err != nil || local1.FuncRef().IsNull() || !consumer.FuncRefMatchesFunction(local1.FuncRef(), 1) || local1 == local0 {
				t.Fatal("local function one was not published distinctly", local0, local1, err)
			}
			if _, err = consumer.Invoke("clear"); err != nil {
				t.Fatal("clear exported local cell", err)
			}
			if got, err := consumer.GlobalValue("cell"); err != nil || !got.FuncRef().IsNull() {
				t.Fatal("guest clear did not publish null", got, err)
			}
			beforeForeignClear := producer.referenceLifetime().snapshot()
			if err = consumer.SetGlobalValue("cell", foreign); err != nil {
				t.Fatal("reinstall foreign descriptor for null overwrite", err)
			}
			installedForClear := producer.referenceLifetime().snapshot()
			if installedForClear.ResourceRoots != beforeForeignClear.ResourceRoots+1 {
				t.Fatal("foreign reinstall did not add exactly one exported-cell root", beforeForeignClear, installedForClear)
			}
			if _, err = consumer.Invoke("clear"); err != nil {
				t.Fatal("clear foreign descriptor", err)
			}
			if afterForeignClear := producer.referenceLifetime().snapshot(); afterForeignClear.ResourceRoots+1 != installedForClear.ResourceRoots || readGlobalObject(consumer.globalCells[0], ValFuncRef) != 0 {
				t.Fatal("foreign-to-null overwrite did not retire exactly one root", installedForClear, afterForeignClear)
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
			if err = rt.CloseContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			if retired := producer.referenceLifetime().snapshot(); !retired.LogicallyClosed || retired.PhysicalResources || retired.ResourceRoots != 0 {
				t.Fatal("runtime token release did not retire foreign producer", retired)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalExportAdmission(t *testing.T) {
	compiled := rv64TableStageCompile(t, "mutable-funcref-local-export-admission", rv64MutableFuncrefLocalExportModule(), os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load")
	defer compiled.Close()
	if !(rv64MutableFuncrefGlobalLocalExport(compiled.executionView()) || rv64PrimitiveReferenceContract(compiled.executionView())) {
		t.Fatal("base local-export witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"function-export": func(v *Compiled) {
			v.Exports = map[string]int{"target_0": 0, "target_1": 1, "set_0": 2, "set_1": 3, "other": 4}
		},
		"global-export": func(v *Compiled) { v.GlobalExports = map[string]int{"other": 0} },
		"immutable":     func(v *Compiled) { v.Globals[0].Mutable = false },
		"entry": func(v *Compiled) {
			v.Entry = slices.Clone(v.Entry)
			v.Entry[len(v.Entry)-1] += 4
		},
		"entry-variant": func(v *Compiled) {
			v.Entry = []int{0, 208, 416, 752, 1088}
			if len(v.code) == 1580 {
				v.Entry = []int{0, 576, 1152, 1872, 2592}
			}
			v.InternalEntry = slices.Clone(v.Entry)
		},
		"internal-entry": func(v *Compiled) {
			v.InternalEntry = slices.Clone(v.InternalEntry)
			v.InternalEntry[len(v.InternalEntry)-1] += 4
		},
		"feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureMutableGlobal },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *compiled.executionView()
			bad.Globals = slices.Clone(bad.Globals)
			bad.Exports = maps.Clone(bad.Exports)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			mutate(&bad)
			if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile local-export artifact admitted", err)
			}
		})
	}
}
