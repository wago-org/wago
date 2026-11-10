//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestStateOnlyProductionHasNoCodeImage(t *testing.T) {
	vector := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	vector = append(vector, 0x0b)
	for name, source := range map[string][]byte{
		"empty":         wasmtest.Module(),
		"scalar-types":  wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)))),
		"vector-types":  wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil)))),
		"scalar-global": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 7, 0x0b}))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0)))),
		"vector-global": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, false, vector))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0)))),
		"vector-import": wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "g", wasm.V128, false))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0)))),
		"memory-data":   wasmtest.Module(wasmtest.Section(5, []byte{1, 1, 1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))), wasmtest.Section(11, []byte{1, 0, 0x41, 0, 0x0b, 1, 42})),
	} {
		t.Run(name, func(t *testing.T) {
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			for _, deferred := range []bool{false, true} {
				image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: deferred, Interruptible: true})
				if err != nil || image == nil {
					t.Fatal("state-only compilation", err)
				}
				if image.CodeImage != nil || len(image.Code) != 0 || len(image.Entry) != 0 || len(image.InternalEntry) != 0 {
					t.Fatal("state-only compilation allocated a code image")
				}
			}
			if image, err := Compile(source); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("private scalar shape widened", err)
			}
			if err := admitSWARDecodedBudget(m); !errors.Is(err, ErrUnsupported) {
				t.Fatal("private SWAR shape widened", err)
			}
		})
	}
}

func mutableFuncrefGetSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}))),
	)
}

func mutableFuncrefClearSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("clear", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x24, 0, 0x0b}))),
	)
}

func mutableFuncrefStoreSelfSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("store", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}))),
	)
}

func mutableFuncrefLocalCopySource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd2, 0, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy_local", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 1, 0x24, 0, 0x0b}))),
	)
}

func mutableFuncrefLocalPublishSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{6, 0, 0, 0, 0, 0, 0}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0),
			wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_local_0", 0, 2),
			wasmtest.ExportEntry("set_local_1", 0, 3),
			wasmtest.ExportEntry("clear_local", 0, 4),
			wasmtest.ExportEntry("publish", 0, 5),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 1, 0x0b}),
			wasmtest.Code([]byte{0xd2, 1, 0x24, 1, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x24, 1, 0x0b}),
			wasmtest.Code([]byte{0x23, 1, 0x24, 0, 0x0b}),
		)),
	)
}

func mutableFuncrefLocalTransferSource() []byte {
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

func mutableFuncrefLocalExportSource() []byte {
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

func mutableFuncrefLocalObserveSource() []byte {
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

func mutableFuncrefLocalCallSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0),
			wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2),
			wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4),
			wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 42, 0x0b}),
			wasmtest.Code([]byte{0x41, 57, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func mutableFuncrefLocalTailCallSource(source []byte) []byte {
	tail := slices.Clone(source)
	pattern := []byte{0x23, 0, 0x14, 0, 0x0b}
	i := bytes.LastIndex(tail, pattern)
	if i < 0 {
		panic("mutable call_ref body not found")
	}
	tail[i+2] = 0x15
	return tail
}

func mutableFuncrefLocalCallScalarSource(typ wasm.ValType, targetBody []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0),
			wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2),
			wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4),
			wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code(targetBody),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func mutableFuncrefLocalCallI32Source() []byte {
	return mutableFuncrefLocalCallScalarSource(wasm.I32, []byte{0x20, 0, 0x41, 1, 0x6a, 0x0b})
}

func mutableFuncrefLocalTailCallI32Source() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x23, 0, 0x15, 0, 0x0b}),
		)),
	)
}

func TestMutableFuncrefLocalTailCallI32Production(t *testing.T) {
	source := mutableFuncrefLocalTailCallI32Source()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil || !admitMutableFunctionGlobalLocalCall(m) {
		t.Fatal("i32 local-tail-call admission", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, NativeTailCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("i32 local-tail-call compilation", err)
	}
	wantDigest := [sha256.Size]byte{0xe4, 0x24, 0x8a, 0xa0, 0xc2, 0xc6, 0xab, 0x35, 0x98, 0xc5, 0xb9, 0xd9, 0x1c, 0x16, 0x63, 0x39, 0xb5, 0xe3, 0x2f, 0xea, 0x21, 0x78, 0x1f, 0xc2, 0x8e, 0x45, 0xbc, 0xde, 0xc7, 0x0c, 0xa2, 0x87}
	wantEntry := []int{0, 608, 1264, 1984, 2704, 3360}
	wantPlan := enc.NativeTailPlan{Kind: enc.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 5, FrameBytes: 80, SourceBase: 56, SourceStride: 8, ReferenceBase: 64}
	if len(image.Code) != 4764 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || image.NativeTail == nil || !slices.Equal(image.NativeTail.Plans, []enc.NativeTailPlan{wantPlan}) || !image.NativeReferenceFunctions || image.NativeReferenceCalls || !image.NativeReferenceControl {
		t.Fatalf("i32 local-tail-call compilation drifted: code=%d digest=%x entries=%v/%v ref=%t/%t/%t tail=%+v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl, image.NativeTail)
	}
}

func mutableFuncrefLocalCallI64Source() []byte {
	return mutableFuncrefLocalCallScalarSource(wasm.I64, []byte{0x20, 0, 0x42, 1, 0x7c, 0x0b})
}

func mutableFuncrefLocalCallF32Source() []byte {
	return mutableFuncrefLocalCallScalarSource(wasm.F32, []byte{0x20, 0, 0x43, 0, 0, 0x80, 0x3f, 0x92, 0x0b})
}

func mutableFuncrefLocalCallF64Source() []byte {
	return mutableFuncrefLocalCallScalarSource(wasm.F64, []byte{0x20, 0, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b})
}

func mutableFuncrefLocalCallMixedSource() []byte {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x20, 1, 0x42, 1, 0x7c, 0x20, 2, 0x43, 0, 0, 0x80, 0x3f, 0x92, 0x20, 3, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func mutableFuncrefLocalCallWideI32Source() []byte {
	types := make([]wasm.ValType, 9)
	identity, increment, call := []byte{}, []byte{}, []byte{}
	for i := byte(0); i < 9; i++ {
		types[i] = wasm.I32
		identity = append(identity, 0x20, i)
		increment = append(increment, 0x20, i, 0x41, 1, 0x6a)
		call = append(call, 0x20, i)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(append(identity, 0x0b)), wasmtest.Code(append(increment, 0x0b)),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), wasmtest.Code(append(call, 0x23, 0, 0x14, 0, 0x0b)),
		)),
	)
}

func TestMutableFuncrefLocalTailCallRemainingProduction(t *testing.T) {
	tests := map[string]struct {
		source []byte
		length int
		digest [sha256.Size]byte
		entry  []int
		plan   enc.NativeTailPlan
	}{
		"zero": {
			mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallSource()), 4684,
			[sha256.Size]byte{0x1f, 0x30, 0x45, 0xc8, 0xe4, 0xa6, 0x83, 0xec, 0x12, 0x3e, 0x06, 0x69, 0x96, 0xad, 0x8d, 0xc9, 0x74, 0x42, 0x2b, 0x47, 0x9e, 0xc4, 0xfe, 0x3e, 0x47, 0xbb, 0x05, 0xd9, 0x39, 0xd4, 0xc9, 0x11},
			[]int{0, 608, 1216, 1936, 2656, 3312}, enc.NativeTailPlan{Kind: enc.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 3, FrameBytes: 64, SourceBase: 48, SourceStride: 8, ReferenceBase: 48},
		},
		"i64": {
			mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallI64Source()), 4748,
			[sha256.Size]byte{0x2e, 0x48, 0x94, 0x36, 0x66, 0xa9, 0xc3, 0x67, 0x67, 0x64, 0xed, 0x6b, 0xfb, 0xe3, 0x9f, 0x2a, 0xc3, 0xcd, 0x17, 0x3f, 0xbf, 0x00, 0x2a, 0xec, 0xf1, 0xe9, 0x1e, 0x7e, 0xbd, 0x20, 0x59, 0x7f},
			[]int{0, 608, 1248, 1968, 2688, 3344}, enc.NativeTailPlan{Kind: enc.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 5, FrameBytes: 80, SourceBase: 56, SourceStride: 8, ReferenceBase: 64},
		},
		"f32": {
			mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallF32Source()), 4764,
			[sha256.Size]byte{0x0b, 0x13, 0x8c, 0xd4, 0x22, 0x27, 0x1b, 0x77, 0xb1, 0x4a, 0xe8, 0x9a, 0xa8, 0xbf, 0x56, 0x9a, 0xcd, 0x51, 0x0c, 0xf4, 0x2f, 0x52, 0x4c, 0x11, 0x55, 0x4d, 0x46, 0x38, 0x21, 0x9a, 0x97, 0x02},
			[]int{0, 608, 1264, 1984, 2704, 3360}, enc.NativeTailPlan{Kind: enc.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 5, FrameBytes: 80, SourceBase: 56, SourceStride: 8, ReferenceBase: 64},
		},
		"f64": {
			mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallF64Source()), 4764,
			[sha256.Size]byte{0xac, 0xd2, 0x1b, 0x4c, 0x20, 0x4a, 0x52, 0x82, 0xe7, 0xc5, 0x4b, 0x6e, 0xda, 0x68, 0x0a, 0x0d, 0x39, 0x90, 0x95, 0xf0, 0xa3, 0x14, 0xc1, 0xa7, 0xad, 0xc2, 0x15, 0x40, 0xba, 0x06, 0x40, 0xf1},
			[]int{0, 608, 1264, 1984, 2704, 3360}, enc.NativeTailPlan{Kind: enc.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 5, FrameBytes: 80, SourceBase: 56, SourceStride: 8, ReferenceBase: 64},
		},
		"mixed": {
			mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallMixedSource()), 5068,
			[sha256.Size]byte{0x69, 0xc3, 0xe7, 0x92, 0x30, 0x07, 0x14, 0xbe, 0x21, 0x46, 0xc3, 0x6f, 0xf0, 0x39, 0x18, 0x33, 0xe9, 0x3c, 0x77, 0x47, 0x7f, 0xac, 0xea, 0x9d, 0x70, 0x87, 0x2e, 0xba, 0xf9, 0xc5, 0xc8, 0x2b},
			[]int{0, 672, 1504, 2224, 2944, 3600}, enc.NativeTailPlan{Kind: enc.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 11, FrameBytes: 128, SourceBase: 80, SourceStride: 8, ReferenceBase: 112},
		},
		"wide-i32": {
			mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallWideI32Source()), 5644,
			[sha256.Size]byte{0x76, 0xd8, 0xe9, 0xcd, 0x0e, 0x05, 0x1e, 0x33, 0x6a, 0xb4, 0xd3, 0xa5, 0xd2, 0xe8, 0x8f, 0xc3, 0x8b, 0xf4, 0xb0, 0x98, 0x1c, 0x2d, 0x41, 0xe9, 0x61, 0x16, 0xf1, 0x00, 0x91, 0xa3, 0x84, 0x1d},
			[]int{0, 800, 1952, 2672, 3392, 4048}, enc.NativeTailPlan{Kind: enc.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 21, FrameBytes: 208, SourceBase: 120, SourceStride: 8, ReferenceBase: 192},
		},
	}
	for name, test := range tests {
		source := test.source
		t.Run(name, func(t *testing.T) {
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModule(source)
			if err != nil || !admitMutableFunctionGlobalLocalCall(m) {
				t.Fatal("local-tail-call admission", err)
			}
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, NativeTailCalls: true, SyncHostSlots: 64})
			if err != nil || image == nil {
				t.Fatal("local-tail-call compilation", err)
			}
			if len(image.Code) != test.length || sha256.Sum256(image.Code) != test.digest || !slices.Equal(image.Entry, test.entry) || !slices.Equal(image.InternalEntry, test.entry) || image.NativeTail == nil || !slices.Equal(image.NativeTail.Plans, []enc.NativeTailPlan{test.plan}) || !image.NativeReferenceFunctions || image.NativeReferenceCalls || !image.NativeReferenceControl {
				t.Fatalf("%s local-tail-call compilation drifted: code=%d digest=%x entries=%v/%v tail=%+v refs=%t/%t/%t", name, len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeTail, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl)
			}
		})
	}
}

func mutableFuncrefLocalCallVectorSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0xfd, 77, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func mutableFuncrefLocalTailCallVectorSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0xfd, 77, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x23, 0, 0x15, 0, 0x0b}),
		)),
	)
}

func TestMutableFuncrefLocalTailCallVectorProduction(t *testing.T) {
	source := mutableFuncrefLocalTailCallVectorSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil || !admitMutableFunctionGlobalLocalCall(m) {
		t.Fatal("vector local-tail-call admission", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, NativeTailCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("vector local-tail-call compilation", err)
	}
	wantDigest := [sha256.Size]byte{0xb8, 0xcd, 0x65, 0x80, 0xb2, 0xce, 0x6a, 0x47, 0x66, 0xe2, 0x0d, 0x33, 0x94, 0x75, 0xf7, 0x09, 0xdb, 0xfc, 0x7a, 0xe8, 0xdd, 0xca, 0x5f, 0x0d, 0xd1, 0x6f, 0xe1, 0x01, 0x31, 0x11, 0x3e, 0x64}
	wantEntry := []int{0, 624, 1280, 2000, 2720, 3360}
	wantPlan := enc.NativeTailPlan{Kind: enc.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 5, FrameBytes: 96, SourceBase: 64, SourceStride: 16, ReferenceBase: 80}
	if len(image.Code) != 4796 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || image.NativeTail == nil || !slices.Equal(image.NativeTail.Plans, []enc.NativeTailPlan{wantPlan}) || !image.NativeReferenceFunctions || image.NativeReferenceCalls || !image.NativeReferenceControl {
		t.Fatalf("vector local-tail-call compilation drifted: code=%d digest=%x entries=%v/%v tail=%+v ref=%t/%t/%t", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeTail, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl)
	}
}

func mutableFuncrefLocalCallMixedVectorSource() []byte {
	types := []wasm.ValType{wasm.I32, wasm.V128, wasm.F64}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x20, 1, 0xfd, 77, 0x20, 2, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func mutableFuncrefLocalCallReferenceCarrierSource() []byte {
	params := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64}
	results := []wasm.ValType{wasm.ExternRef, wasm.FuncRef, wasm.I32, wasm.I64}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 2, 0x20, 1, 0x20, 0, 0x20, 3, 0x0b}),
			wasmtest.Code([]byte{0x20, 2, 0x20, 1, 0x20, 0, 0x41, 1, 0x6a, 0x20, 3, 0x42, 1, 0x7c, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func mutableFuncrefLocalCallHostReferenceCarrierSource() []byte {
	types := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64, wasm.F32, wasm.F64}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x20, 5, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x20, 1, 0x20, 2, 0x20, 3, 0x42, 1, 0x7c, 0x20, 4, 0x43, 0, 0, 0x80, 0x3f, 0x92, 0x20, 5, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x20, 5, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func mutableFuncrefLocalCallReferenceVectorCarrierSource() []byte {
	types := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.V128, wasm.F64}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x20, 1, 0x20, 2, 0x20, 3, 0xfd, 77, 0x20, 4, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func mutableFuncrefLocalCallReferenceVectorLocalTransferSource() []byte {
	types := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.V128, wasm.F64}
	identity, transform, call := mutableReferenceVectorLocalTransferBodies(false)
	codeWithLocals := func(body []byte) []byte {
		fn := []byte{byte(len(types))}
		for _, typ := range types {
			fn = append(fn, 1, wasm.MustEncodeValType(typ))
		}
		fn = append(fn, body...)
		return append(wasmtest.ULEB(uint32(len(fn))), fn...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			codeWithLocals(identity), codeWithLocals(transform),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), codeWithLocals(call),
		)),
	)
}

func mutableFuncrefLocalCallReferenceVectorBranchTransferSource() []byte {
	types := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.V128, wasm.F64}
	identity, transform, call := mutableReferenceVectorBranchTransferBodies(false)
	codeWithLocals := func(body []byte) []byte {
		fn := []byte{byte(len(types))}
		for _, typ := range types {
			fn = append(fn, 1, wasm.MustEncodeValType(typ))
		}
		fn = append(fn, body...)
		return append(wasmtest.ULEB(uint32(len(fn))), fn...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			codeWithLocals(identity), codeWithLocals(transform),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), codeWithLocals(call),
		)),
	)
}

// Byte encodings of public bridges can change when their transport grows.
// Retain function-frame, entry, plan and code-size checks independently.
func requireMutableFuncrefCallFrames(t *testing.T, image *enc.CompiledModule) {
	t.Helper()
	for index, entry := range image.Entry {
		if entry < 0 || entry&15 != 0 || entry >= len(image.Code) {
			t.Fatal("invalid mutable funcref entry", index, entry)
		}
		if frame, err := NativeFrameBytes(image.Code, entry); err != nil || frame < 32 || frame&15 != 0 || frame > MaxFrameBytes {
			t.Fatal("invalid mutable funcref frame", index, frame, err)
		}
	}
}

func TestMutableFuncrefLocalCallReferenceVectorBranchTransferProduction(t *testing.T) {
	for _, tail := range []bool{false, true} {
		t.Run(fmt.Sprintf("tail%t", tail), func(t *testing.T) {
			source := mutableFuncrefLocalCallReferenceVectorBranchTransferSource()
			if tail {
				source = mutableFuncrefLocalTailCallSource(source)
			}
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModule(source)
			if err != nil || !admitMutableFunctionGlobalReferenceCarrierCall(m) {
				t.Fatal("reference-vector branch-transfer admission", err)
			}
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, NativeTailCalls: tail, SyncHostSlots: 64})
			if err != nil || image == nil {
				t.Fatal("reference-vector branch-transfer compilation", err)
			}
			wantCode := 8812

			var wantTail *enc.NativeTailModule
			if tail {
				wantCode = 6412

				wantTail = &enc.NativeTailModule{Plans: []enc.NativeTailPlan{{Kind: enc.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 43, FrameBytes: 304, SourceBase: 208, SourceStride: 16, ReferenceBase: 288}}}
			}
			wantEntry := []int{0, 1072, 2496, 3216, 3936, 4576}
			requireMutableFuncrefCallFrames(t, image)
			if len(image.Code) > wantCode || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl || !reflect.DeepEqual(image.NativeTail, wantTail) {
				t.Fatalf("reference-vector branch-transfer compilation drifted: tail=%t code=%d digest=%x entries=%v/%v refs=%t/%t/%t tail-plan=%+v", tail, len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl, image.NativeTail)
			}
		})
	}
}

func TestMutableFuncrefLocalCallReferenceVectorLocalTransferProduction(t *testing.T) {
	for _, tail := range []bool{false, true} {
		t.Run(fmt.Sprintf("tail%t", tail), func(t *testing.T) {
			source := mutableFuncrefLocalCallReferenceVectorLocalTransferSource()
			if tail {
				source = mutableFuncrefLocalTailCallSource(source)
			}
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModule(source)
			if err != nil || !admitMutableFunctionGlobalReferenceCarrierCall(m) {
				t.Fatal("reference-vector local-transfer admission", err)
			}
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, NativeTailCalls: tail, SyncHostSlots: 64})
			if err != nil || image == nil {
				t.Fatal("reference-vector local-transfer compilation", err)
			}
			wantCode := 8604

			var wantTail *enc.NativeTailModule
			if tail {
				wantCode = 6204

				wantTail = &enc.NativeTailModule{Plans: []enc.NativeTailPlan{{Kind: enc.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 43, FrameBytes: 304, SourceBase: 208, SourceStride: 16, ReferenceBase: 288}}}
			}
			wantEntry := []int{0, 1072, 2288, 3008, 3728, 4368}
			requireMutableFuncrefCallFrames(t, image)
			if len(image.Code) > wantCode || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl || !reflect.DeepEqual(image.NativeTail, wantTail) {
				t.Fatalf("reference-vector local-transfer compilation drifted: tail=%t code=%d digest=%x entries=%v/%v refs=%t/%t/%t tail-plan=%+v", tail, len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl, image.NativeTail)
			}
		})
	}
}

func TestMutableFuncrefLocalCallReferenceVectorCarrierProduction(t *testing.T) {
	for _, tail := range []bool{false, true} {
		t.Run(fmt.Sprintf("tail%t", tail), func(t *testing.T) {
			source := mutableFuncrefLocalCallReferenceVectorCarrierSource()
			if tail {
				source = mutableFuncrefLocalTailCallSource(source)
			}
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModule(source)
			if err != nil || !admitMutableFunctionGlobalReferenceCarrierCall(m) {
				t.Fatal("reference-vector-carrier local-call admission", err)
			}
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, NativeTailCalls: tail, SyncHostSlots: 64})
			if err != nil || image == nil {
				t.Fatal("reference-vector-carrier local-call compilation", err)
			}
			wantCode := 8012

			var wantTail *enc.NativeTailModule
			if tail {
				wantCode = 5628

				wantTail = &enc.NativeTailModule{Plans: []enc.NativeTailPlan{{Kind: enc.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 13, FrameBytes: 224, SourceBase: 128, SourceStride: 16, ReferenceBase: 208}}}
			}
			wantEntry := []int{0, 880, 1904, 2624, 3344, 3984}
			requireMutableFuncrefCallFrames(t, image)
			if len(image.Code) > wantCode || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl || !reflect.DeepEqual(image.NativeTail, wantTail) {
				t.Fatalf("reference-vector-carrier compilation drifted: tail=%t code=%d digest=%x entries=%v/%v refs=%t/%t/%t tail-plan=%+v", tail, len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl, image.NativeTail)
			}
		})
	}
}

func TestMutableFuncrefLocalCallHostReferenceCarrierProduction(t *testing.T) {
	source := mutableFuncrefLocalCallHostReferenceCarrierSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil || !admitMutableFunctionGlobalReferenceCarrierCall(m) {
		t.Fatal("host-reference-carrier local-call admission", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("host-reference-carrier local-call compilation", err)
	}

	wantEntry := []int{0, 720, 1600, 2320, 3040, 3696}
	requireMutableFuncrefCallFrames(t, image)
	if len(image.Code) > 7548 || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl || image.NativeTail != nil {
		t.Fatalf("host-reference-carrier local-call compilation drifted: code=%d digest=%x entries=%v/%v refs=%t/%t/%t tail=%+v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl, image.NativeTail)
	}
}

func TestMutableFuncrefLocalTailCallHostReferenceCarrierProduction(t *testing.T) {
	source := mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallHostReferenceCarrierSource())
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil || !admitMutableFunctionGlobalReferenceCarrierCall(m) {
		t.Fatal("host-reference-carrier local-tail-call admission", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, NativeTailCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("host-reference-carrier local-tail-call compilation", err)
	}

	wantEntry := []int{0, 720, 1600, 2320, 3040, 3696}
	wantPlan := enc.NativeTailPlan{Kind: enc.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 15, FrameBytes: 160, SourceBase: 96, SourceStride: 8, ReferenceBase: 144}
	requireMutableFuncrefCallFrames(t, image)
	if len(image.Code) > 5212 || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || image.NativeTail == nil || !slices.Equal(image.NativeTail.Plans, []enc.NativeTailPlan{wantPlan}) || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl {
		t.Fatalf("host-reference-carrier local-tail-call compilation drifted: code=%d digest=%x entries=%v/%v refs=%t/%t/%t tail=%+v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl, image.NativeTail)
	}
}

func TestMutableFuncrefLocalCallReferenceCarrierProduction(t *testing.T) {
	source := mutableFuncrefLocalCallReferenceCarrierSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil || !admitMutableFunctionGlobalReferenceCarrierCall(m) {
		t.Fatal("reference-carrier local-call admission", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("reference-carrier local-call compilation", err)
	}

	wantEntry := []int{0, 672, 1408, 2128, 2848, 3504}
	requireMutableFuncrefCallFrames(t, image)
	if len(image.Code) > 7244 || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl || image.NativeTail != nil {
		t.Fatalf("reference-carrier local-call compilation drifted: code=%d digest=%x entries=%v/%v refs=%t/%t/%t tail=%+v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl, image.NativeTail)
	}
}

func TestMutableFuncrefLocalTailCallReferenceCarrierProduction(t *testing.T) {
	source := mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallReferenceCarrierSource())
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil || !admitMutableFunctionGlobalReferenceCarrierCall(m) {
		t.Fatal("reference-carrier local-tail-call admission", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, NativeTailCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("reference-carrier local-tail-call compilation", err)
	}

	wantEntry := []int{0, 672, 1408, 2128, 2848, 3504}
	wantPlan := enc.NativeTailPlan{Kind: enc.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 11, FrameBytes: 128, SourceBase: 80, SourceStride: 8, ReferenceBase: 112}
	requireMutableFuncrefCallFrames(t, image)
	if len(image.Code) > 4972 || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || image.NativeTail == nil || !slices.Equal(image.NativeTail.Plans, []enc.NativeTailPlan{wantPlan}) || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl {
		t.Fatalf("reference-carrier local-tail-call compilation drifted: code=%d digest=%x entries=%v/%v refs=%t/%t/%t tail=%+v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl, image.NativeTail)
	}
}

func mutableFuncrefLocalTailCallMixedVectorSource() []byte {
	types := []wasm.ValType{wasm.I32, wasm.V128, wasm.F64}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x20, 1, 0xfd, 77, 0x20, 2, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x23, 0, 0x15, 0, 0x0b}),
		)),
	)
}

func TestMutableFuncrefLocalTailCallMixedVectorProduction(t *testing.T) {
	source := mutableFuncrefLocalTailCallMixedVectorSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil || !admitMutableFunctionGlobalLocalCall(m) {
		t.Fatal("mixed vector local-tail-call admission", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, NativeTailCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mixed vector local-tail-call compilation", err)
	}

	wantEntry := []int{0, 768, 1680, 2400, 3120, 3760}
	wantPlan := enc.NativeTailPlan{Kind: enc.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 9, FrameBytes: 160, SourceBase: 96, SourceStride: 16, ReferenceBase: 144}
	requireMutableFuncrefCallFrames(t, image)
	if len(image.Code) > 5308 || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || image.NativeTail == nil || !slices.Equal(image.NativeTail.Plans, []enc.NativeTailPlan{wantPlan}) || !image.NativeReferenceFunctions || image.NativeReferenceCalls || !image.NativeReferenceControl {
		t.Fatalf("mixed vector local-tail-call compilation drifted: code=%d digest=%x entries=%v/%v tail=%+v ref=%t/%t/%t", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeTail, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl)
	}
}

func TestMutableFuncrefLocalCallMixedVectorProduction(t *testing.T) {
	source := mutableFuncrefLocalCallMixedVectorSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil || !admitMutableFunctionGlobalLocalCall(m) {
		t.Fatal("mixed vector local-call admission", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mixed vector local-call compilation", err)
	}

	wantEntry := []int{0, 768, 1680, 2400, 3120, 3760}
	requireMutableFuncrefCallFrames(t, image)
	if len(image.Code) > 7596 || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl {
		t.Fatalf("mixed vector local-call compilation drifted: code=%d digest=%x entries=%v/%v ref=%t/%t/%t", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl)
	}
}

func TestMutableFuncrefLocalCallVectorProduction(t *testing.T) {
	source := mutableFuncrefLocalCallVectorSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil || !admitMutableFunctionGlobalLocalCall(m) {
		t.Fatal("vector local-call admission", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("vector local-call compilation", err)
	}

	wantEntry := []int{0, 624, 1280, 2000, 2720, 3360}
	requireMutableFuncrefCallFrames(t, image)
	if len(image.Code) > 6988 || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl {
		t.Fatalf("vector local-call compilation drifted: code=%d digest=%x entries=%v/%v ref=%t/%t/%t", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl)
	}
}

func TestMutableFuncrefLocalCallWideI32Production(t *testing.T) {
	source := mutableFuncrefLocalCallWideI32Source()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil || !admitMutableFunctionGlobalLocalCall(m) {
		t.Fatal("wide i32 local-call admission", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("wide i32 local-call compilation", err)
	}

	wantEntry := []int{0, 800, 1952, 2672, 3392, 4048}
	requireMutableFuncrefCallFrames(t, image)
	if len(image.Code) > 8060 || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl {
		t.Fatalf("wide i32 local-call compilation drifted: code=%d digest=%x entries=%v/%v ref=%t/%t/%t", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl)
	}
}

func TestMutableFuncrefLocalCallMixedProduction(t *testing.T) {
	source := mutableFuncrefLocalCallMixedSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil || !admitMutableFunctionGlobalLocalCall(m) {
		t.Fatal("mixed local-call admission", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mixed local-call compilation", err)
	}

	wantEntry := []int{0, 672, 1504, 2224, 2944, 3600}
	requireMutableFuncrefCallFrames(t, image)
	if len(image.Code) > 7340 || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl {
		t.Fatalf("mixed local-call compilation drifted: code=%d digest=%x entries=%v/%v ref=%t/%t/%t", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl)
	}
}

func TestMutableFuncrefLocalCallI32Production(t *testing.T) {
	source := mutableFuncrefLocalCallI32Source()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalLocalCall(m) {
		t.Fatal("i32 local-call source was not admitted")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref i32 local-call compilation", err)
	}

	wantEntry := []int{0, 608, 1264, 1984, 2704, 3360}
	requireMutableFuncrefCallFrames(t, image)
	if len(image.Code) > 6940 || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl {
		t.Fatalf("mutable funcref i32 local-call compilation drifted: code=%d digest=%x entries=%v/%v ref=%t/%t/%t", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl)
	}
}

func TestMutableFuncrefLocalCallScalarProduction(t *testing.T) {
	tests := map[string]struct {
		source func() []byte
		bytes  int
		entry  []int
	}{
		"i64": {mutableFuncrefLocalCallI64Source, 6908, []int{0, 608, 1248, 1968, 2688, 3344}},
		"f32": {mutableFuncrefLocalCallF32Source, 6940, []int{0, 608, 1264, 1984, 2704, 3360}},
		"f64": {mutableFuncrefLocalCallF64Source, 6924, []int{0, 608, 1264, 1984, 2704, 3360}},
	}
	for name, test := range tests {
		source := test.source()
		if err := AdmitProductionSource(source); err != nil {
			t.Error(name, err)
			continue
		}
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Error(name, err)
			continue
		}
		if !admitMutableFunctionGlobalLocalCall(m) {
			t.Error(name, "scalar local-call source was not admitted")
			continue
		}
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, SyncHostSlots: 64})
		if err != nil || image == nil {
			t.Error(name, err)
			continue
		}
		requireMutableFuncrefCallFrames(t, image)
		if len(image.Code) > test.bytes || !slices.Equal(image.Entry, test.entry) || !slices.Equal(image.InternalEntry, test.entry) || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl {
			t.Errorf("%s scalar local-call compilation drifted: code=%d digest=%x entries=%v/%v ref=%t/%t/%t", name, len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl)
		}
	}
}

func TestMutableFuncrefLocalCallProduction(t *testing.T) {
	source := mutableFuncrefLocalCallSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalLocalCall(m) {
		t.Fatal("local-call source was not admitted")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref local-call compilation", err)
	}

	wantEntry := []int{0, 608, 1216, 1936, 2656, 3312}
	requireMutableFuncrefCallFrames(t, image)
	if len(image.Code) > 6828 || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl {
		t.Fatalf("mutable funcref local-call compilation drifted: code=%d digest=%x entries=%v/%v ref=%t/%t/%t", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry, image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl)
	}
}

func TestMutableFuncrefLocalCallProductionRejectsBroaderSources(t *testing.T) {
	for variant, source := range map[string]func() []byte{
		"zero":                   mutableFuncrefLocalCallSource,
		"zero-tail":              func() []byte { return mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallSource()) },
		"i32":                    mutableFuncrefLocalCallI32Source,
		"i32-tail":               mutableFuncrefLocalTailCallI32Source,
		"i64":                    mutableFuncrefLocalCallI64Source,
		"i64-tail":               func() []byte { return mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallI64Source()) },
		"f32":                    mutableFuncrefLocalCallF32Source,
		"f32-tail":               func() []byte { return mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallF32Source()) },
		"f64":                    mutableFuncrefLocalCallF64Source,
		"f64-tail":               func() []byte { return mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallF64Source()) },
		"mixed":                  mutableFuncrefLocalCallMixedSource,
		"mixed-tail":             func() []byte { return mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallMixedSource()) },
		"wide-i32":               mutableFuncrefLocalCallWideI32Source,
		"wide-i32-tail":          func() []byte { return mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallWideI32Source()) },
		"vector":                 mutableFuncrefLocalCallVectorSource,
		"vector-tail":            mutableFuncrefLocalTailCallVectorSource,
		"mixed-vector":           mutableFuncrefLocalCallMixedVectorSource,
		"mixed-vector-tail":      mutableFuncrefLocalTailCallMixedVectorSource,
		"reference-carrier":      mutableFuncrefLocalCallReferenceCarrierSource,
		"host-reference-carrier": mutableFuncrefLocalCallHostReferenceCarrierSource,
		"host-reference-carrier-tail": func() []byte {
			return mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallHostReferenceCarrierSource())
		},
		"reference-vector-carrier": mutableFuncrefLocalCallReferenceVectorCarrierSource,
		"reference-vector-carrier-tail": func() []byte {
			return mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallReferenceVectorCarrierSource())
		},
		"reference-vector-local-transfer": mutableFuncrefLocalCallReferenceVectorLocalTransferSource,
		"reference-vector-local-transfer-tail": func() []byte {
			return mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallReferenceVectorLocalTransferSource())
		},
		"reference-vector-branch-transfer": mutableFuncrefLocalCallReferenceVectorBranchTransferSource,
		"reference-vector-branch-transfer-tail": func() []byte {
			return mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallReferenceVectorBranchTransferSource())
		},
		"reference-carrier-tail": func() []byte {
			return mutableFuncrefLocalTailCallSource(mutableFuncrefLocalCallReferenceCarrierSource())
		},
	} {
		for name, mutate := range map[string]func(*wasm.Module){
			"abstract-global":       func(m *wasm.Module) { m.Globals[0].Type.Type = wasm.FuncRef },
			"target-body":           func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x00, 0x0b} },
			"writer-other-function": func(m *wasm.Module) { m.Code[2].BodyBytes = []byte{0xd2, 5, 0x24, 0, 0x0b} },
			"wrong-call-export":     func(m *wasm.Module) { m.Exports[5].Name = "other" },
			"reordered-types": func(m *wasm.Module) {
				m.Types[0], m.Types[1] = m.Types[1], m.Types[0]
				for i, index := range []uint32{1, 1, 0, 0, 0, 1} {
					m.FuncTypes[i].Index = index
				}
				ref := m.Globals[0].Type.Type.Ref()
				m.Globals[0].Type.Type = wasm.RefVal(wasm.Ref(ref.Nullable(), wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), ref.Exact()))
				m.Globals[0].Init.BodyBytes = []byte{0xd0, 1, 0x0b}
				m.Code[4].BodyBytes = []byte{0xd0, 1, 0x24, 0, 0x0b}
				m.Code[5].BodyBytes = slices.Clone(m.Code[5].BodyBytes)
				m.Code[5].BodyBytes[len(m.Code[5].BodyBytes)-2] = 1
			},
		} {
			t.Run(variant+"-"+name, func(t *testing.T) {
				m, err := wasm.DecodeModule(source())
				if err != nil {
					t.Fatal(err)
				}
				mutate(m)
				if admitMutableFunctionGlobalLocalCall(m) {
					t.Fatal("broader local-call source admitted")
				}
				checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(true, true))
			})
		}
	}
}

func TestMutableFuncrefLocalObserveProduction(t *testing.T) {
	source := mutableFuncrefLocalObserveSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalLocalObserve(m) {
		t.Fatal("local-observe source was not admitted")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref local-observe compilation", err)
	}
	wantDigest := [sha256.Size]byte{0x3d, 0x4f, 0x37, 0x9c, 0xa5, 0xff, 0xe8, 0x94, 0xf4, 0xb4, 0xe4, 0x59, 0xc8, 0xeb, 0x23, 0x19, 0xb4, 0x28, 0x0c, 0x6d, 0x31, 0x31, 0xcb, 0x22, 0x2c, 0xa7, 0x45, 0x93, 0x6f, 0x59, 0x0c, 0x64}
	wantEntry := []int{0, 208, 416, 768, 1120, 1408}
	if len(image.Code) != 1900 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) {
		t.Fatalf("mutable funcref local-observe compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefLocalObserveProductionRejectsBroaderSources(t *testing.T) {
	for name, mutate := range map[string]func(*wasm.Module){
		"observer-returns-ref": func(m *wasm.Module) {
			m.Types[1].SubTypes[0].Comp.Results[0] = wasm.FuncRef
			m.Code[5].BodyBytes = []byte{0x23, 0, 0x0b}
		},
		"observer-other-global": func(m *wasm.Module) { m.Code[5].BodyBytes = []byte{0x23, 1, 0xd1, 0x0b} },
		"writer-other-function": func(m *wasm.Module) { m.Code[2].BodyBytes = []byte{0xd2, 2, 0x24, 0, 0x0b} },
		"wrong-observer-export": func(m *wasm.Module) { m.Exports[5].Name = "other" },
		"wrong-global-export":   func(m *wasm.Module) { m.Exports[6].Name = "other" },
		"reordered-types": func(m *wasm.Module) {
			m.Types[0], m.Types[1] = m.Types[1], m.Types[0]
			for i := range m.FuncTypes {
				m.FuncTypes[i].Index = 1
			}
			m.FuncTypes[5].Index = 0
		},
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(mutableFuncrefLocalObserveSource())
			if err != nil {
				t.Fatal(err)
			}
			mutate(m)
			if admitMutableFunctionGlobalLocalObserve(m) {
				t.Fatal("broader local-observe source admitted")
			}
			checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(true, true))
		})
	}
}

func TestMutableFuncrefLocalExportProduction(t *testing.T) {
	source := mutableFuncrefLocalExportSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalLocalExport(m) {
		t.Fatal("local-export source was not admitted")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref local-export compilation", err)
	}
	wantDigest := [sha256.Size]byte{0x97, 0x84, 0x33, 0x60, 0xf8, 0xa8, 0x06, 0xd0, 0x61, 0xc0, 0x53, 0xe9, 0x63, 0x48, 0xdd, 0xba, 0x4a, 0xbb, 0x17, 0x69, 0x23, 0x45, 0x99, 0xef, 0x08, 0x9f, 0x52, 0x89, 0x20, 0xd1, 0x0c, 0x4f}
	wantEntry := []int{0, 208, 416, 768, 1120}
	if len(image.Code) != 1644 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) {
		t.Fatalf("mutable funcref local-export compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefLocalExportProductionRejectsBroaderSources(t *testing.T) {
	for name, mutate := range map[string]func(*wasm.Module){
		"local-nonnull": func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0xd2, 0, 0x0b} },
		"foreign-import": func(m *wasm.Module) {
			m.Imports = append(m.Imports, wasm.Import{Module: "env", Name: "cell", Type: wasm.ExternType{}})
		},
		"set-other-func":      func(m *wasm.Module) { m.Code[2].BodyBytes = []byte{0xd2, 2, 0x24, 0, 0x0b} },
		"write-other-global":  func(m *wasm.Module) { m.Code[4].BodyBytes = []byte{0xd0, 0x70, 0x24, 1, 0x0b} },
		"wrong-global-export": func(m *wasm.Module) { m.Exports[5].Name = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(mutableFuncrefLocalExportSource())
			if err != nil {
				t.Fatal(err)
			}
			mutate(m)
			if admitMutableFunctionGlobalLocalExport(m) {
				t.Fatal("broader local-export source admitted")
			}
			checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(true, true))
		})
	}
}

func TestMutableFuncrefLocalTransferProduction(t *testing.T) {
	source := mutableFuncrefLocalTransferSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalLocalTransfer(m) {
		t.Fatal("local-transfer source was not admitted")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref local-transfer compilation", err)
	}
	wantDigest := [sha256.Size]byte{0x23, 0x8e, 0x2e, 0xca, 0x4f, 0x71, 0xe2, 0x0f, 0x02, 0x35, 0xae, 0xc6, 0x14, 0x8b, 0x5b, 0x6f, 0x59, 0x4e, 0xbb, 0xdb, 0x58, 0x81, 0xe4, 0xa4, 0x51, 0x16, 0xee, 0x91, 0x2c, 0x24, 0xce, 0x48}
	wantEntry := []int{0, 288, 576}
	if len(image.Code) != 1100 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) {
		t.Fatalf("mutable funcref local-transfer compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefLocalTransferProductionRejectsBroaderSources(t *testing.T) {
	for name, mutate := range map[string]func(*wasm.Module){
		"import-name":   func(m *wasm.Module) { m.Imports[0].Name = "other" },
		"local-nonnull": func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0xd2, 0, 0x0b} },
		"capture-target": func(m *wasm.Module) {
			m.Code[0].BodyBytes = []byte{0x23, 1, 0x24, 2, 0x0b}
		},
		"publish-source": func(m *wasm.Module) {
			m.Code[1].BodyBytes = []byte{0x23, 2, 0x24, 0, 0x0b}
		},
		"clear-import": func(m *wasm.Module) { m.Code[2].BodyBytes = []byte{0xd0, 0x70, 0x24, 1, 0x0b} },
		"wrong-export": func(m *wasm.Module) { m.Exports[2].Name = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(mutableFuncrefLocalTransferSource())
			if err != nil {
				t.Fatal(err)
			}
			mutate(m)
			if admitMutableFunctionGlobalLocalTransfer(m) {
				t.Fatal("broader local-transfer source admitted")
			}
			checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(true, true))
		})
	}
}

func TestMutableFuncrefLocalPublishProduction(t *testing.T) {
	source := mutableFuncrefLocalPublishSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalLocalPublish(m) {
		t.Fatal("exact mutable funcref local-publish source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref local-publish compilation", err)
	}
	wantDigest := [sha256.Size]byte{0xa4, 0xf7, 0x18, 0xf2, 0xd3, 0xa5, 0xb4, 0x22, 0x2c, 0xb0, 0x62, 0xf5, 0x0b, 0x06, 0xe4, 0x19, 0x0e, 0xa0, 0x0f, 0x16, 0x90, 0xf8, 0x50, 0xe7, 0x03, 0x72, 0xe1, 0xcc, 0xf0, 0x81, 0xa6, 0x5a}
	wantEntry := []int{0, 208, 416, 768, 1120, 1408}
	if len(image.Code) != 1932 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) {
		t.Fatalf("mutable funcref local-publish compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefLocalPublishProductionRejectsBroaderSources(t *testing.T) {
	for name, mutate := range map[string]func(*wasm.Module){
		"import-name":    func(m *wasm.Module) { m.Imports[0].Name = "other" },
		"local-nonnull":  func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0xd2, 0, 0x0b} },
		"set-other-func": func(m *wasm.Module) { m.Code[2].BodyBytes = []byte{0xd2, 2, 0x24, 1, 0x0b} },
		"clear-import":   func(m *wasm.Module) { m.Code[4].BodyBytes = []byte{0xd0, 0x70, 0x24, 0, 0x0b} },
		"reverse-copy":   func(m *wasm.Module) { m.Code[5].BodyBytes = []byte{0x23, 0, 0x24, 1, 0x0b} },
		"wrong-export":   func(m *wasm.Module) { m.Exports[5].Name = "other" },
		"global-export": func(m *wasm.Module) {
			m.Exports = append(m.Exports, wasm.Export{Name: "cell", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 1}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(mutableFuncrefLocalPublishSource())
			if err != nil {
				t.Fatal(err)
			}
			mutate(m)
			if admitMutableFunctionGlobalLocalPublish(m) {
				t.Fatal("broader mutable funcref local-publish source matched exact profile")
			}
			checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(true, true))
		})
	}
}

func TestMutableFuncrefLocalCopyProduction(t *testing.T) {
	source := mutableFuncrefLocalCopySource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalLocalCopy(m) {
		t.Fatal("exact mutable funcref local-copy source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref local-copy compilation", err)
	}
	wantDigest := [sha256.Size]byte{0xfc, 0xe7, 0x94, 0xfe, 0x45, 0x2b, 0x9f, 0x89, 0x40, 0x2a, 0x11, 0x7c, 0x18, 0x0e, 0x71, 0x27, 0x20, 0x4d, 0x0b, 0x77, 0x56, 0xc9, 0x82, 0x29, 0x67, 0x2a, 0x9c, 0x74, 0x8e, 0x27, 0xa0, 0x9e}
	if len(image.Code) != 524 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, []int{0}) || !slices.Equal(image.InternalEntry, []int{0}) {
		t.Fatalf("mutable funcref local-copy compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefLocalCopyProductionRejectsBroaderSources(t *testing.T) {
	module := func(imports, globals, exports, body []byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, imports),
			wasmtest.Section(3, []byte{1, 0}),
			wasmtest.Section(6, globals),
			wasmtest.Section(7, exports),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
	}
	exactImports := wasmtest.Vec(wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true))
	exactGlobals := wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd2, 0, 0x0b}))
	exactExports := wasmtest.Vec(wasmtest.ExportEntry("copy_local", 0, 0))
	exactBody := []byte{0x23, 1, 0x24, 0, 0x0b}
	for name, source := range map[string][]byte{
		"import-source": module(exactImports, exactGlobals, exactExports, []byte{0x23, 0, 0x24, 0, 0x0b}),
		"reverse":       module(exactImports, exactGlobals, exactExports, []byte{0x23, 0, 0x24, 1, 0x0b}),
		"null-local":    module(exactImports, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b})), exactExports, exactBody),
		"immutable":     module(exactImports, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd2, 0, 0x0b})), exactExports, exactBody),
		"wrong-import": module(
			wasmtest.Vec(wasmtest.GlobalImportEntry("env", "other", wasm.FuncRef, true)), exactGlobals, exactExports, exactBody,
		),
		"wrong-export": module(exactImports, exactGlobals, wasmtest.Vec(wasmtest.ExportEntry("other", 0, 0)), exactBody),
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal("fixture decoding", err)
			}
			if admitMutableFunctionGlobalLocalCopy(m) {
				t.Fatal("broader mutable funcref local-copy source matched exact profile")
			}
			checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(true, true))
		})
	}
}

func mutableFuncrefCopySource() []byte {
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

func TestMutableFuncrefCopyProduction(t *testing.T) {
	source := mutableFuncrefCopySource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalCopy(m) {
		t.Fatal("exact mutable funcref copy source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref copy compilation", err)
	}
	wantDigest := [sha256.Size]byte{0xab, 0x15, 0xad, 0x58, 0x56, 0x58, 0xba, 0x10, 0x1c, 0x8a, 0xd4, 0x4f, 0x8b, 0xee, 0x9c, 0x5c, 0xbc, 0x1a, 0xb1, 0x04, 0x2a, 0x05, 0xa6, 0x48, 0xe3, 0x57, 0xd7, 0x6f, 0x87, 0x6e, 0xd8, 0x76}
	if len(image.Code) != 524 || sha256.Sum256(image.Code) != wantDigest || len(image.Entry) != 1 || image.Entry[0] != 0 || len(image.InternalEntry) != 1 || image.InternalEntry[0] != 0 {
		t.Fatalf("mutable funcref copy compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefCopyProductionRejectsBroaderSources(t *testing.T) {
	module := func(imports, exports, body []byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, imports),
			wasmtest.Section(3, []byte{1, 0}),
			wasmtest.Section(7, exports),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
	}
	exactImports := wasmtest.Vec(
		wasmtest.GlobalImportEntry("env", "source", wasm.FuncRef, true),
		wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
	)
	exactExports := wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0))
	for name, source := range map[string][]byte{
		"reverse":      module(exactImports, exactExports, []byte{0x23, 1, 0x24, 0, 0x0b}),
		"same-cell":    module(exactImports, exactExports, []byte{0x23, 0, 0x24, 0, 0x0b}),
		"null":         module(exactImports, exactExports, []byte{0xd0, 0x70, 0x24, 1, 0x0b}),
		"wrong-export": module(exactImports, wasmtest.Vec(wasmtest.ExportEntry("other", 0, 0)), []byte{0x23, 0, 0x24, 1, 0x0b}),
		"wrong-name": module(wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "other", wasm.FuncRef, true),
			wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
		), exactExports, []byte{0x23, 0, 0x24, 1, 0x0b}),
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal("fixture decoding", err)
			}
			if admitMutableFunctionGlobalCopy(m) {
				t.Fatal("broader mutable funcref copy source matched exact profile")
			}
			checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(true, true))
		})
	}
}

func mutableFuncrefMoveSource() []byte {
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

func mutableFuncrefBidirectionalCopySource() []byte {
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

func TestMutableFuncrefBidirectionalCopyProduction(t *testing.T) {
	source := mutableFuncrefBidirectionalCopySource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalBidirectionalCopy(m) {
		t.Fatal("exact mutable funcref bidirectional copy source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref bidirectional copy compilation", err)
	}
	wantDigest := [sha256.Size]byte{0xb2, 0xb7, 0x62, 0xca, 0x55, 0x2d, 0x4f, 0x9b, 0x4b, 0x69, 0xaa, 0x27, 0x86, 0x30, 0xca, 0xe0, 0xe9, 0x39, 0x2d, 0x86, 0x73, 0x70, 0xd9, 0xeb, 0x85, 0x61, 0xfa, 0x98, 0x7f, 0x25, 0x23, 0x30}
	if len(image.Code) != 812 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, []int{0, 288}) || !slices.Equal(image.InternalEntry, []int{0, 288}) {
		t.Fatalf("mutable funcref bidirectional copy compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefBidirectionalCopyProductionRejectsBroaderSources(t *testing.T) {
	module := func(exports, first, second []byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, wasmtest.Vec(
				wasmtest.GlobalImportEntry("env", "source", wasm.FuncRef, true),
				wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
			)),
			wasmtest.Section(3, []byte{2, 0, 0}),
			wasmtest.Section(7, exports),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(first), wasmtest.Code(second))),
		)
	}
	exactExports := wasmtest.Vec(
		wasmtest.ExportEntry("copy_to_target", 0, 0),
		wasmtest.ExportEntry("copy_to_source", 0, 1),
	)
	forward := []byte{0x23, 0, 0x24, 1, 0x0b}
	reverse := []byte{0x23, 1, 0x24, 0, 0x0b}
	for name, source := range map[string][]byte{
		"two-forward":       module(exactExports, forward, forward),
		"two-reverse":       module(exactExports, reverse, reverse),
		"swapped-functions": module(exactExports, reverse, forward),
		"wrong-first-export": module(wasmtest.Vec(
			wasmtest.ExportEntry("other", 0, 0),
			wasmtest.ExportEntry("copy_to_source", 0, 1),
		), forward, reverse),
		"swapped-exports": module(wasmtest.Vec(
			wasmtest.ExportEntry("copy_to_source", 0, 1),
			wasmtest.ExportEntry("copy_to_target", 0, 0),
		), forward, reverse),
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal("fixture decoding", err)
			}
			if admitMutableFunctionGlobalBidirectionalCopy(m) {
				t.Fatal("broader bidirectional copy source matched exact profile")
			}
			checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(true, true))
		})
	}
}

func TestMutableFuncrefMoveProduction(t *testing.T) {
	source := mutableFuncrefMoveSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalMove(m) {
		t.Fatal("exact mutable funcref move source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref move compilation", err)
	}
	wantDigest := [sha256.Size]byte{0x84, 0x7d, 0xb8, 0xcc, 0x6d, 0xc5, 0x2c, 0xbc, 0x15, 0xf2, 0xae, 0xf7, 0x83, 0x75, 0xe2, 0x1a, 0xce, 0x9b, 0x08, 0xfa, 0x39, 0xb7, 0x13, 0xca, 0xdf, 0x9e, 0x68, 0xbf, 0xd7, 0xd8, 0xcb, 0x9b}
	if len(image.Code) != 588 || sha256.Sum256(image.Code) != wantDigest || len(image.Entry) != 1 || image.Entry[0] != 0 || len(image.InternalEntry) != 1 || image.InternalEntry[0] != 0 {
		t.Fatalf("mutable funcref move compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefMoveProductionRejectsBroaderSources(t *testing.T) {
	module := func(exports, body []byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, wasmtest.Vec(
				wasmtest.GlobalImportEntry("env", "source", wasm.FuncRef, true),
				wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
			)),
			wasmtest.Section(3, []byte{1, 0}),
			wasmtest.Section(7, exports),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
	}
	exactExports := wasmtest.Vec(wasmtest.ExportEntry("move", 0, 0))
	for name, source := range map[string][]byte{
		"copy-only":    module(exactExports, []byte{0x23, 0, 0x24, 1, 0x0b}),
		"clear-only":   module(exactExports, []byte{0xd0, 0x70, 0x24, 0, 0x0b}),
		"clear-first":  module(exactExports, []byte{0xd0, 0x70, 0x24, 0, 0x23, 0, 0x24, 1, 0x0b}),
		"wrong-clear":  module(exactExports, []byte{0x23, 0, 0x24, 1, 0xd0, 0x70, 0x24, 1, 0x0b}),
		"wrong-export": module(wasmtest.Vec(wasmtest.ExportEntry("other", 0, 0)), []byte{0x23, 0, 0x24, 1, 0xd0, 0x70, 0x24, 0, 0x0b}),
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal("fixture decoding", err)
			}
			if admitMutableFunctionGlobalMove(m) {
				t.Fatal("broader mutable funcref move source matched exact profile")
			}
			checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(true, true))
		})
	}
}

func TestMutableFuncrefStoreSelfProduction(t *testing.T) {
	source := mutableFuncrefStoreSelfSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalSetSelf(m) {
		t.Fatal("exact mutable funcref self-store source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref self-store compilation", err)
	}
	wantDigest := [sha256.Size]byte{0xa4, 0x9d, 0x58, 0x12, 0xf0, 0x3a, 0x44, 0x3f, 0x6d, 0x36, 0x5a, 0xf3, 0x4c, 0x6a, 0x0c, 0x09, 0xf7, 0xc7, 0x14, 0xda, 0x82, 0x27, 0xb1, 0x25, 0x35, 0xab, 0xa6, 0x5a, 0xac, 0x77, 0x8a, 0xb0}
	if len(image.Code) != 588 || sha256.Sum256(image.Code) != wantDigest || len(image.Entry) != 1 || image.Entry[0] != 0 || len(image.InternalEntry) != 1 || image.InternalEntry[0] != 0 {
		t.Fatalf("mutable funcref self-store compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefStoreSelfProductionRejectsBroaderSources(t *testing.T) {
	module := func(exports, body []byte, functions ...[]byte) []byte {
		if len(functions) == 0 {
			functions = [][]byte{body}
		}
		funcTypes := make([]byte, 1+len(functions))
		funcTypes[0] = byte(len(functions))
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
			wasmtest.Section(3, funcTypes),
			wasmtest.Section(7, exports),
			wasmtest.Section(10, wasmtest.Vec(functions...)),
		)
	}
	exactExports := wasmtest.Vec(wasmtest.ExportEntry("store", 0, 0), wasmtest.ExportEntry("cell", 3, 0))
	differentTarget := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true),
			wasmtest.GlobalImportEntry("env", "other", wasm.FuncRef, true),
		)),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, exactExports),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd2, 0, 0x24, 1, 0x0b}))),
	)
	for name, source := range map[string][]byte{
		"copy-existing": module(exactExports, wasmtest.Code([]byte{0x23, 0, 0x24, 0, 0x0b})),
		"mixed-null":    module(exactExports, wasmtest.Code([]byte{0xd2, 0, 0x1a, 0xd0, 0x70, 0x24, 0, 0x0b})),
		"other-target":  differentTarget,
		"wrong-export": module(
			wasmtest.Vec(wasmtest.ExportEntry("other", 0, 0), wasmtest.ExportEntry("cell", 3, 0)),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}),
		),
		"extra-function": module(
			exactExports,
			nil,
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x0b}),
		),
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal("fixture decoding", err)
			}
			if admitMutableFunctionGlobalSetSelf(m) {
				t.Fatal("broader mutable funcref self-store source matched exact profile")
			}
			checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(true, true))
		})
	}
}

func TestMutableFuncrefClearProduction(t *testing.T) {
	source := mutableFuncrefClearSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalClear(m) {
		t.Fatal("exact mutable funcref clear source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref clear compilation", err)
	}
	wantDigest := [sha256.Size]byte{0x60, 0x09, 0xe7, 0x34, 0xdf, 0x2a, 0x40, 0x08, 0x02, 0x88, 0x6b, 0xe1, 0x74, 0x81, 0xd3, 0xf3, 0x12, 0x12, 0x0a, 0x36, 0x37, 0x11, 0x65, 0x7c, 0xb5, 0xa6, 0x07, 0x62, 0x80, 0x47, 0x2c, 0x88}
	if len(image.Code) != 524 || sha256.Sum256(image.Code) != wantDigest || len(image.Entry) != 1 || image.Entry[0] != 0 || len(image.InternalEntry) != 1 || image.InternalEntry[0] != 0 {
		t.Fatalf("mutable funcref clear compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefClearProductionRejectsBroaderSources(t *testing.T) {
	module := func(exports, body []byte, functions ...[]byte) []byte {
		if len(functions) == 0 {
			functions = [][]byte{body}
		}
		funcTypes := make([]byte, 1+len(functions))
		funcTypes[0] = byte(len(functions))
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
			wasmtest.Section(3, funcTypes),
			wasmtest.Section(7, exports),
			wasmtest.Section(10, wasmtest.Vec(functions...)),
		)
	}
	exactExports := wasmtest.Vec(wasmtest.ExportEntry("clear", 0, 0), wasmtest.ExportEntry("cell", 3, 0))
	for name, source := range map[string][]byte{
		"copy-existing":  module(exactExports, wasmtest.Code([]byte{0x23, 0, 0x24, 0, 0x0b})),
		"store-non-null": module(exactExports, wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b})),
		"wrong-export": module(
			wasmtest.Vec(wasmtest.ExportEntry("other", 0, 0), wasmtest.ExportEntry("cell", 3, 0)),
			wasmtest.Code([]byte{0xd0, 0x70, 0x24, 0, 0x0b}),
		),
		"extra-function": module(
			exactExports,
			nil,
			wasmtest.Code([]byte{0xd0, 0x70, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x0b}),
		),
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal("fixture decoding", err)
			}
			if admitMutableFunctionGlobalClear(m) {
				t.Fatal("broader mutable funcref clear source matched exact profile")
			}
			checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(true, true))
		})
	}
}

func TestMutableFuncrefGetProduction(t *testing.T) {
	if err := AdmitProductionSource(mutableFuncrefGetSource()); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(mutableFuncrefGetSource())
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalGet(m) {
		t.Fatal("exact mutable funcref get source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true})
	if err != nil || image == nil {
		t.Fatal("mutable funcref get compilation", err)
	}
	wantDigest := [sha256.Size]byte{0x4d, 0x22, 0xfb, 0x69, 0xe1, 0x32, 0x2a, 0xe2, 0x30, 0xc0, 0x3c, 0x22, 0x84, 0x14, 0x88, 0xe8, 0x76, 0xcb, 0xb4, 0x90, 0xf9, 0x33, 0xbc, 0x57, 0xe1, 0x7b, 0xa4, 0xdd, 0xaf, 0x4f, 0x86, 0xd0}
	if len(image.Code) != 244 || sha256.Sum256(image.Code) != wantDigest || len(image.Entry) != 1 || image.Entry[0] != 0 || len(image.InternalEntry) != 1 || image.InternalEntry[0] != 0 {
		t.Fatal("mutable funcref get compilation drifted")
	}
}

func TestMutableFuncrefGetProductionRejectsBroaderSources(t *testing.T) {
	funcrefResult := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x0b}))),
	)
	globalSet := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x24, 0, 0x41, 0, 0x0b}))),
	)
	extraFunction := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{2, 0, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}), wasmtest.Code([]byte{0x41, 0, 0x0b}))),
	)
	extraGlobal := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}))),
	)
	wrongExports := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("other", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}))),
	)
	withTable := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(4, []byte{1, 0x70, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}))),
	)
	withStart := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(8, []byte{0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}))),
	)
	for name, source := range map[string][]byte{
		"global-set":     globalSet,
		"raw-funcref":    funcrefResult,
		"extra-function": extraFunction,
		"extra-global":   extraGlobal,
		"wrong-exports":  wrongExports,
		"table":          withTable,
		"start":          withStart,
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal("fixture decoding", err)
			}
			if admitMutableFunctionGlobalGet(m) {
				t.Fatal("broader mutable funcref source matched exact profile")
			}
			checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(true, true))
		})
	}
}

func TestStateOnlyProductionStillValidatesState(t *testing.T) {
	for _, source := range [][]byte{
		wasmtest.Module(wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("bad", 0, 0)))),
		wasmtest.Module(wasmtest.Section(8, []byte{0})),
		wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x23, 0, 0x0b})))),
	} {
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal("fixture decoding", err)
		}
		staged := false
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, MemoryPressure: func() { staged = true }})
		if err == nil || image != nil || staged {
			t.Fatal("invalid functionless state reached staging", err)
		}
	}
}

func stateOnlyMutableFuncrefImportSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("cell", 3, 0))),
	)
}

func TestStateOnlyMutableFuncrefImportProduction(t *testing.T) {
	source := stateOnlyMutableFuncrefImportSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true})
	if err != nil || image == nil {
		t.Fatal("state-only mutable funcref compilation", err)
	}
	if image.CodeImage != nil || len(image.Code) != 0 || len(image.Entry) != 0 || len(image.InternalEntry) != 0 {
		t.Fatal("state-only mutable funcref allocated executable code")
	}

	for name, bad := range map[string][]byte{
		"no-export": wasmtest.Module(
			wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		),
		"extra-import": wasmtest.Module(
			wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true), wasmtest.GlobalImportEntry("env", "count", wasm.I32, true))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("cell", 3, 0))),
		),
		"table": wasmtest.Module(
			wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
			wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("cell", 3, 0))),
		),
	} {
		t.Run(name, func(t *testing.T) {
			decoded, err := wasm.DecodeModule(bad)
			if err != nil {
				t.Fatal(err)
			}
			image, err := CompileModuleWith(decoded, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true})
			if image == nil || err != nil || image.CodeImage != nil || len(image.Code) != 0 || len(image.Entry) != 0 {
				t.Fatal("valid declaration-only mutable funcref state rejected", err)
			}
		})
	}
}
