//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

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

func rv64MutableFuncrefLocalObserveModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{6, 0, 0, 0, 0, 0, 1}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0),
			wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2),
			wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4),
			wasmtest.ExportEntry("is_null", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}),
		)),
	)
}

func TestRV64PublicMutableFuncrefGlobalLocalObserve(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()

			producerCode := rv64TableStageCompile(t, "mutable-funcref-local-observe-producer", rv64ReferenceGlobalExportModule(t, false, false, false, 1), artifact)
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

			compiled := rv64TableStageCompile(t, "mutable-funcref-local-observe", rv64MutableFuncrefLocalObserveModule(), artifact)
			view := compiled.executionView()
			if !rv64MutableFuncrefGlobalLocalObserve(view) || !view.usesGuestGlobalStoreHelper() {
				t.Fatalf("local-observe witness missing: code=%d/%x entries=%v/%v globals=%+v exports=%v/%v validation=%v", len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.Globals, view.Exports, view.GlobalExports, railshotValidateCompiled(view))
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
			assertNull := func(want uint64) {
				t.Helper()
				got, invokeErr := consumer.Invoke("is_null")
				if invokeErr != nil || !slices.Equal(got, []uint64{want}) {
					t.Fatal("guest local mutable funcref observation", got, invokeErr, "want", want)
				}
			}
			assertNull(1)
			before := producer.referenceLifetime().snapshot()
			if err = consumer.SetGlobalValue("cell", foreign); err != nil {
				t.Fatal("install foreign descriptor", err)
			}
			installed := producer.referenceLifetime().snapshot()
			if installed.ResourceRoots != before.ResourceRoots+1 {
				t.Fatal("host ingress did not retain foreign producer", before, installed)
			}
			assertNull(0)
			if err = producer.Close(); err != nil {
				t.Fatal(err)
			}
			parked := producer.referenceLifetime().snapshot()
			if !parked.LogicallyClosed || !parked.PhysicalResources {
				t.Fatal("observed foreign descriptor did not retain producer", parked)
			}
			if _, err = consumer.Invoke("set_0"); err != nil {
				t.Fatal("replace foreign descriptor with local zero", err)
			}
			assertNull(0)
			if retired := producer.referenceLifetime().snapshot(); retired.ResourceRoots+1 != parked.ResourceRoots {
				t.Fatal("local replacement did not retire exactly one cell root", parked, retired)
			}
			if _, err = consumer.Invoke("clear"); err != nil {
				t.Fatal("clear observed cell", err)
			}
			assertNull(1)
			if _, err = consumer.Invoke("set_1"); err != nil {
				t.Fatal("publish local one", err)
			}
			assertNull(0)
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
			if final := producer.referenceLifetime().snapshot(); final.PhysicalResources || final.ResourceRoots != 0 {
				t.Fatal("runtime token release did not retire foreign producer", final)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalObserveAdmission(t *testing.T) {
	compiled := rv64TableStageCompile(t, "mutable-funcref-local-observe-admission", rv64MutableFuncrefLocalObserveModule(), os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load")
	defer compiled.Close()
	if !rv64MutableFuncrefGlobalLocalObserve(compiled.executionView()) {
		t.Fatal("base local-observe witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"function-export": func(v *Compiled) {
			v.Exports = map[string]int{"target_0": 0, "target_1": 1, "set_0": 2, "set_1": 3, "clear": 4, "other": 5}
		},
		"global-export": func(v *Compiled) { v.GlobalExports = map[string]int{"other": 0} },
		"immutable":     func(v *Compiled) { v.Globals[0].Mutable = false },
		"observer-signature": func(v *Compiled) {
			v.Funcs = slices.Clone(v.Funcs)
			v.Funcs[5].Results = nil
		},
		"entry": func(v *Compiled) {
			v.Entry = slices.Clone(v.Entry)
			v.Entry[len(v.Entry)-1] += 4
		},
		"entry-variant": func(v *Compiled) {
			v.Entry = []int{0, 208, 416, 752, 1088, 1360}
			if len(v.code) == 1836 {
				v.Entry = []int{0, 576, 1152, 1872, 2592, 3248}
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
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Globals = slices.Clone(bad.Globals)
			bad.Exports = maps.Clone(bad.Exports)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			mutate(&bad)
			if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile local-observe artifact admitted", err)
			}
		})
	}
}
