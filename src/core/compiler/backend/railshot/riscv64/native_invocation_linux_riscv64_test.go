//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestNativeInvocationEntryRejectsInvalidViewsBeforeResults(t *testing.T) {
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("id", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}))))
	decoded, err := wasm.DecodeModuleByteBackedWithLimits(source, wasm.ValidationFeatures{}, wasm.DecodeLimits{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := CompileModuleWith(decoded.Module, CompileOptions{Interruptible: true, InvocationContext: true, DeferCodeMapping: true})
	if err != nil {
		t.Fatal(err)
	}
	memory, entry, err := coreruntime.MapCode(c.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(memory)
	e, err := coreruntime.NewEngineWithStackBytes(coreruntime.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	j, err := coreruntime.NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	arena, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	args, results, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
	binary.LittleEndian.PutUint64(args, 49)
	if err := j.BindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	j.SetStackFence(e.StackLimit())
	view := e.RV64InvocationViewPointer()
	j.SetRV64InvocationView(view)
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != 49 {
		t.Fatal("qualified view", err)
	}
	for _, test := range []struct {
		name         string
		pointer      uintptr
		field        int
		incoming     enc.Reg
		stackDescent bool
	}{{"nil", 0, -1, 0, false}, {"unaligned", view + 1, -1, 0, false}, {"past-top", view + 64, -1, 0, false}, {"wrapped-span", ^uintptr(0) &^ 7, -1, 0, false}, {"short-child-span", view - abi.RV64NativeWrapperSaveBytes - 8, -1, 0, true}, {"magic", view, 0, 0, false}, {"top", view, 16, 0, false}, {"fence", view, 8, 0, false}, {"short-args", view, 32, 0, false}, {"short-results", view, 48, 0, false}, {"argument-pointer", view, -1, enc.X10, false}, {"result-pointer", view, -1, enc.X13, false}} {
		t.Run(test.name, func(t *testing.T) {
			j.SetRV64InvocationView(test.pointer)
			call := entry
			if test.field >= 0 || test.incoming != 0 || test.stackDescent {
				var wrapper enc.Asm
				if test.field >= 0 {
					wrapper.Constant64(enc.X28, int64(view))
					wrapper.Sd(enc.Zero, enc.X28, int32(test.field))
				}
				if test.incoming != 0 {
					wrapper.Addi(test.incoming, test.incoming, 8)
				}
				if test.stackDescent {
					wrapper.Addi(enc.SP, enc.SP, -128)
				}
				wrapper.Constant64(enc.X28, int64(entry))
				wrapper.Jalr(enc.Zero, enc.X28, 0)
				mapping, address, err := coreruntime.MapCode(wrapper.B)
				if err != nil {
					t.Fatal(err)
				}
				defer coreruntime.Unmap(mapping)
				call = address
			}
			const canary = uint64(0xa55a0123456789ef)
			binary.LittleEndian.PutUint64(results, canary)
			err := e.CallPrepared(call, args, j.LinMemBase(), trap, results)
			var nativeTrap *coreruntime.TrapError
			if !errors.As(err, &nativeTrap) || nativeTrap.Code != coreruntime.TrapCalledFnNotLinked || binary.LittleEndian.Uint64(results) != canary {
				t.Fatal("invalid view reached result publication", err)
			}
			j.SetRV64InvocationView(view)
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != 49 {
				t.Fatal("view after cold rejection", err)
			}
		})
	}
}
