//go:build (linux && (amd64 || arm64) && !tinygo && !wago_guardpage) || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_guardpage)

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func stagedSharedMemoryEHProviderModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x01})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00}, []byte{0x00, 0x00})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x63, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("memory", byte(wasm.ExternMem), 0),
			wasmtest.ExportEntry("throw-tag", byte(wasm.ExternTag), 0),
			wasmtest.ExportEntry("other-tag", byte(wasm.ExternTag), 1),
			wasmtest.ExportEntry("throw", byte(wasm.ExternFunc), 0),
		)),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x08, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0xe3, 0x00, 0x0b}), // provider table value: 99
		)),
	)
}

func stagedSharedMemoryEHCatcherModule() []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, byte(wasm.ExternMem), 0x00, 0x01)
	catchBody := func(clause ...byte) []byte {
		body := []byte{
			0x02, 0x40, // block
			0x1f, 0x7f, 0x01, // try_table i32 with one catch
		}
		body = append(body, clause...)
		return append(body,
			0x10, 0x00, // call imported thrower
			0x41, 0x09, // normal result (unreachable)
			0x0b, 0x0f, // end try_table; return
			0x0b,       // end block (catch target)
			0x23, 0x00, // caller global: 42
			0x41, 0x00, // table index 0
			0x11, 0x01, 0x00, // caller table function: 5
			0x6a, 0x0b, // i32.add; end
		)
	}
	catchAll := catchBody(byte(wasm.CatchAll), 0x00)
	catchReorderedTag := catchBody(byte(wasm.CatchTag), 0x01, 0x00)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(
			memoryImport,
			stagedTagImportEntry("env", "other-tag", 0),
			stagedTagImportEntry("env", "throw-tag", 0),
			stagedFuncImportEntry("env", "throw", 0),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x01}, []byte{0x01}, []byte{0x01})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x2a, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("catch-all", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("catch-tag", byte(wasm.ExternFunc), 3),
		)),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x05, 0x0b}), // caller table value: 5
			wasmtest.Code(catchAll),
			wasmtest.Code(catchReorderedTag),
		)),
	)
}

func TestStagedCrossInstanceExceptionRestoresSharedMemoryInstanceContext(t *testing.T) {
	// A constant-only catch can hide stale basedata after handler-jump unwinding.
	// Reordered tags prove dispatch uses the catcher, while the different global
	// and table values prove its continuation also returned to the same instance.
	providerCode := compileStagedExceptionHandling(t, stagedSharedMemoryEHProviderModule())
	defer providerCode.Close()
	provider, err := instantiateCore(providerCode, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate EH provider: %v", err)
	}
	defer provider.Close()

	memory, err := provider.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	throwTag, err := provider.ExportedTag("throw-tag")
	if err != nil {
		t.Fatal(err)
	}
	otherTag, err := provider.ExportedTag("other-tag")
	if err != nil {
		t.Fatal(err)
	}
	thrower, err := provider.ExportedFunc("throw")
	if err != nil {
		t.Fatal(err)
	}

	catcherCode := compileStagedExceptionHandling(t, stagedSharedMemoryEHCatcherModule())
	defer catcherCode.Close()
	catcher, err := instantiateCore(catcherCode, InstantiateOptions{Imports: testImports(
		"env.memory", memory,
		"env.other-tag", otherTag,
		"env.throw-tag", throwTag,
		"env.throw", thrower,
	)})
	if err != nil {
		t.Fatalf("instantiate EH catcher: %v", err)
	}
	defer catcher.Close()

	for _, name := range []string{"catch-all", "catch-tag"} {
		t.Run(name, func(t *testing.T) {
			got, err := catcher.Invoke(name)
			if err != nil || len(got) != 1 || uint32(got[0]) != 47 {
				t.Fatalf("shared-memory cross-instance catch result=%v err=%v, want caller global+table value 47", got, err)
			}
		})
	}
}

func stagedSameInstanceDirtyGlobalEHModule() []byte {
	// Loop-weighted gets make global 0 a module-wide register pin on both native
	// backends. The loop exits after one iteration; its purpose is to exercise the
	// delayed cell write used by a genuinely pinned mutable global.
	heat := []byte{0x02, 0x40, 0x03, 0x40}
	for range 32 { // two bodies clear ARM64's 50*loop-weight module-pin bar
		heat = append(heat, 0x23, 0x00, 0x1a) // global.get 0; drop
	}
	heat = append(heat, 0x0c, 0x01, 0x0b, 0x0b) // br 1; end loop; end block
	catchBody := func(clause ...byte) []byte {
		body := append([]byte(nil), heat...)
		body = append(body,
			0x02, 0x40, // block
			0x1f, 0x7f, 0x01, // try_table i32 with one catch
		)
		body = append(body, clause...)
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(77)...)
		return append(body,
			0x24, 0x00, // global.set 0 (kept dirty in its module pin)
			0x08, 0x00, // throw tag 0
			0x41, 0x09, // normal result (unreachable)
			0x0b, 0x0f, // end try_table; return
			0x0b,       // end block (catch target)
			0x23, 0x00, // global.get 0 must still observe 77
			0x0b,
		)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x00})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x01, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("catch-all-dirty-global", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("catch-tag-dirty-global", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(catchBody(byte(wasm.CatchAll), 0x00)),
			wasmtest.Code(catchBody(byte(wasm.CatchTag), 0x00, 0x00)),
		)),
	)
}

func TestStagedSameInstanceExceptionPreservesDirtyPinnedGlobal(t *testing.T) {
	// Handler context restoration also runs for direct same-instance throws. It
	// must not let the subsequent register refresh replace a pending global.set
	// with the older value in the global's backing cell.
	compiled := compileStagedExceptionHandling(t, stagedSameInstanceDirtyGlobalEHModule())
	defer compiled.Close()
	instance, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	for _, name := range []string{"catch-all-dirty-global", "catch-tag-dirty-global"} {
		t.Run(name, func(t *testing.T) {
			got, err := instance.Invoke(name)
			if err != nil || len(got) != 1 || uint32(got[0]) != 77 {
				t.Fatalf("same-instance catch result=%v err=%v, want dirty pinned global value 77", got, err)
			}
		})
	}
}
