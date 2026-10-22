//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestExceptionScalarNativeExecution(t *testing.T) {
	for _, nested := range []bool{false, true} {
		m := exceptionScalarModule(t, nested)
		code, _ := compileExceptionFunction(t, m, 0)
		mapping, entry, err := coreruntime.MapCode(code)
		if err != nil {
			t.Fatal(err)
		}
		defer coreruntime.Unmap(mapping)
		engine, err := coreruntime.NewEngineWithStackBytes(coreruntime.MinNativeStackBytes)
		if err != nil {
			t.Fatal(err)
		}
		defer engine.Close()
		job, err := coreruntime.NewJobMemory(0)
		if err != nil {
			t.Fatal(err)
		}
		defer job.Close()
		arena, err := coreruntime.NewArena(4096)
		if err != nil {
			t.Fatal(err)
		}
		defer arena.Close()
		args, results, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
		tags := arena.Alloc(len(m.Tags) * 8)
		for i := range m.Tags {
			binary.LittleEndian.PutUint64(tags[i*8:], uint64(0x100+i))
		}
		job.SetEHTagDirPtr(uintptr(unsafe.Pointer(unsafe.SliceData(tags))))
		context := arena.Alloc(abi.RV64InstanceContextBytes)
		job.CaptureInstanceContextBytes(context)
		job.SetRV64NativeContext(uintptr(unsafe.Pointer(unsafe.SliceData(context))))
		job.SetRV64InvocationView(engine.RV64InvocationViewPointer())
		job.SetStackFence(engine.StackLimit())
		if err := job.BindTrapCell(trap); err != nil {
			t.Fatal(err)
		}
		for i := range 32 {
			binary.LittleEndian.PutUint64(args, uint64(42+i))
			if err := engine.CallPrepared(entry, args, job.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != uint64(42+i) {
				t.Fatalf("nested=%t round=%d result=%x: %v", nested, i, results, err)
			}
		}
	}
}

func exceptionNativeRunner(t testing.TB, m *wasm.Module) func(uint64) (uint64, error) {
	t.Helper()
	code, _ := compileExceptionFunction(t, m, 0)
	mapping, entry, err := coreruntime.MapCode(code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coreruntime.Unmap(mapping) })
	engine, err := coreruntime.NewEngineWithStackBytes(coreruntime.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	job, err := coreruntime.NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = job.Close() })
	arena, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = arena.Close() })
	args, results, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
	tags := arena.Alloc(len(m.Tags) * 8)
	for i := range m.Tags {
		binary.LittleEndian.PutUint64(tags[i*8:], uint64(0x100+i))
	}
	job.SetEHTagDirPtr(uintptr(unsafe.Pointer(unsafe.SliceData(tags))))
	context := arena.Alloc(abi.RV64InstanceContextBytes)
	job.CaptureInstanceContextBytes(context)
	job.SetRV64NativeContext(uintptr(unsafe.Pointer(unsafe.SliceData(context))))
	job.SetRV64InvocationView(engine.RV64InvocationViewPointer())
	job.SetStackFence(engine.StackLimit())
	if err := job.BindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	return func(argument uint64) (uint64, error) {
		clear(trap)
		binary.LittleEndian.PutUint64(args, argument)
		err := engine.CallPrepared(entry, args, job.LinMemBase(), trap, results)
		return binary.LittleEndian.Uint64(results), err
	}
}

func TestExceptionReferenceNativeExecution(t *testing.T) {
	for _, tc := range []struct {
		name         string
		null, branch bool
		trap         coreruntime.TrapCode
	}{
		{name: "rethrow"}, {name: "null", null: true, trap: coreruntime.TrapNullReference},
		{name: "branch-exit", branch: true, trap: coreruntime.TrapUnhandledException},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := exceptionNativeRunner(t, exceptionReferenceModule(t, tc.null, tc.branch))
			for round := range 32 {
				got, err := run(uint64(42 + round))
				if tc.trap == 0 {
					if err != nil || got != uint64(42+round) {
						t.Fatalf("round=%d result=%d: %v", round, got, err)
					}
				} else {
					var trapped *coreruntime.TrapError
					if !errors.As(err, &trapped) || trapped.Code != tc.trap {
						t.Fatalf("round=%d trap: %v", round, err)
					}
				}
			}
		})
	}
}
