//go:build linux && amd64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func nativeEpochHostModule(start bool) []byte {
	imp := append(wasmtest.Name("env"), wasmtest.Name("callback")...)
	imp = append(imp, byte(wasm.ExternFunc), 1)
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0}, wasmtest.FuncType(nil, nil), []byte{0x60, 0, 1, 0x64, 0})),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("make", 0, 1), wasmtest.ExportEntry("run", 0, 2))),
	}
	if start {
		sections = append(sections, wasmtest.Section(8, wasmtest.ULEB(2)))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x0b}), wasmtest.Code([]byte{0x10, 0, 0x0b}))))
	return wasmtest.Module(sections...)
}

func TestNativeEpochAdvancesAfterRealHostCallback(t *testing.T) {
	entryAdvance := uint32(0)
	if gc.NativeTypeCarriersEnabled() {
		entryAdvance = 1
	}
	for _, start := range []bool{false, true} {
		t.Run(map[bool]string{false: "invoke", true: "construction-start"}[start], func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), nativeEpochHostModule(start))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			calls := 0
			in, err := Instantiate(compiled, InstantiateOptions{Imports: testImports("env.callback", slotHostFunc(func(HostModule, []uint64, []uint64) { calls++ }))})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if in.gc == nil {
				t.Fatal("fixture has no collector")
			}
			if start {
				if calls != 1 || in.gc.NativeTypeEpoch() != 2+entryAdvance {
					t.Fatalf("start callback calls%d epoch%d want1/%d", calls, in.gc.NativeTypeEpoch(), 2+entryAdvance)
				}
			} else {
				before := in.gc.NativeTypeEpoch()
				if _, err := in.Invoke("run"); err != nil {
					t.Fatal(err)
				}
				if calls != 1 || in.gc.NativeTypeEpoch() != before+1+entryAdvance {
					t.Fatalf("callback calls%d epoch%d want1/%d", calls, in.gc.NativeTypeEpoch(), before+1+entryAdvance)
				}
			}
		})
	}
}

func TestNativeEpochAdvancesAfterAtomicWait(t *testing.T) {
	entryAdvance := uint32(0)
	if gc.NativeTypeCarriersEnabled() {
		entryAdvance = 1
	}
	imp := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	imp = append(imp, 0x02, 3, 1, 1)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0}, []byte{0x60, 0, 1, 0x64, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("make", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x0b}), nativeEpochWaitBody())),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	memory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	in, err := Instantiate(compiled, testImports("env.memory", memory))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.gc == nil {
		t.Fatal("fixture has no collector")
	}
	in.markNativeControlShared()
	if in.usesPrivateThreadedExecution() || in.usesIndependentExecution() {
		t.Fatal("fixture did not enter shared execution")
	}
	before := in.gc.NativeTypeEpoch()
	result, err := in.Invoke("run")
	if err != nil || len(result) != 1 || result[0] != 2 {
		t.Fatalf("wait result%v/%v want timed-out2", result, err)
	}
	if in.gc.NativeTypeEpoch() != before+1+entryAdvance {
		t.Fatalf("atomic wait epoch%d want%d", in.gc.NativeTypeEpoch(), before+1+entryAdvance)
	}
}

func nativeEpochWaitBody() []byte {
	// Keep an object live across the wait so its native frame requires roots.
	body := []byte{1, 1, 0x63, 0, 0xfb, 1, 0, 0x21, 0, 0x41, 0, 0x41, 0, 0x42, 1, 0xfe, 1, 2, 0, 0x20, 0, 0xd1, 0x6a, 0x0b}
	return append(wasmtest.ULEB(uint32(len(body))), body...)
}
