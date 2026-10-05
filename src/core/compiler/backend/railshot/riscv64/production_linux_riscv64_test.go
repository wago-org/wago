//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

type productionCallFixture struct {
	engine              *coreruntime.Engine
	memory              *coreruntime.JobMemory
	args, trap, results []byte
	base                uintptr
	entries             []int
}

func productionCalls(t *testing.T, data []byte, force bool) *productionCallFixture {
	return productionCallsWithOptions(t, data, ProductionOptions{ForceFallback: force})
}

func productionCallsWithOptions(t *testing.T, data []byte, opts ProductionOptions) *productionCallFixture {
	t.Helper()
	m, err := CompileProduction(data, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.CodeImage.Close(); err != nil {
			t.Error(err)
		}
	})
	var mapping []byte
	var base uintptr
	if opts.DeferCodeMapping {
		code, transferErr := m.CodeImage.(*coreruntime.CodeBuffer).TakeHeap()
		if transferErr != nil {
			t.Fatal(transferErr)
		}
		mapping, base, err = coreruntime.MapCode(code)
	} else {
		mapping, base, err = m.CodeImage.Take()
	}
	t.Cleanup(func() {
		if err := coreruntime.Unmap(mapping); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.DeferCodeMapping {
		if err := coreruntime.SealCode(mapping); err != nil {
			t.Fatal(err)
		}
	}
	e, err := coreruntime.NewEngineWithStackBytes(coreruntime.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Close(); err != nil {
			t.Error(err)
		}
	})
	j, err := coreruntime.NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := j.Close(); err != nil {
			t.Error(err)
		}
	})
	j.SetStackFence(e.StackLimit())
	arena, err := coreruntime.NewArena(8192)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := arena.Close(); err != nil {
			t.Error(err)
		}
	})
	return &productionCallFixture{engine: e, memory: j, args: arena.Alloc(256 * 8), trap: arena.Alloc(coreruntime.TrapBufferBytes), results: arena.Alloc(256 * 8), base: base, entries: m.Entry}
}

// This fixture stages Go-owned carriers through the real production boundary.
// A failed call leaves caller-owned results unchanged.
func (f *productionCallFixture) invoke(index int, args, results []uint64) error {
	for i, value := range args {
		binary.LittleEndian.PutUint64(f.args[i*8:], value)
	}
	clear(f.results)
	if err := f.engine.Call(f.base+uintptr(f.entries[index]), f.args, f.memory.LinearMemory(), f.trap, f.results); err != nil {
		return err
	}
	for i := range results {
		results[i] = binary.LittleEndian.Uint64(f.results[i*8:])
	}
	return nil
}

func TestProductionEnvelopeMultivalueAndIndexedControl(t *testing.T) {
	for _, tc := range multivalueCases() {
		t.Run(tc.name, func(t *testing.T) {
			for _, force := range []bool{false, true} {
				f := productionCalls(t, binaryModule(tc.funcs...), force)
				for _, call := range tc.calls {
					got := make([]uint64, len(call.want))
					if err := f.invoke(0, call.args, got); err != nil || !reflect.DeepEqual(got, append([]uint64{}, call.want...)) {
						t.Fatal("production vector/control result", tc.name, got, call.want, err)
					}
				}
			}
		})
	}
}

func TestProductionEnvelopeSharedRawI32Carriers(t *testing.T) {
	for _, op := range []byte{0x48, 0x49, 0x6a, 0x6b, 0x6c} {
		fn := fixtureFunc{params: []byte{0x7f, 0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x20, 1, op, 0x0b}}
		f := productionCalls(t, binaryModule(fn), false)
		args := []uint64{dirty(0x80000000), dirty(0xffffffff)}
		want := map[byte]uint64{0x48: 1, 0x49: 1, 0x6a: 0x7fffffff, 0x6b: 0x80000001, 0x6c: 0x80000000}[op]
		var result [1]uint64
		if err := f.invoke(0, args, result[:]); err != nil || result[0] != want {
			t.Fatal(op, result, err)
		}
	}
}

func TestProductionEnvelopeColdTrapsAndReuse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   []byte
		locals []byte
		args   []uint64
		code   coreruntime.TrapCode
		pc     uint32
	}{
		{"unreachable", []byte{0, 0x0b}, nil, nil, coreruntime.TrapUnreachable, 1},
		{"declared-locals", []byte{0, 0x0b}, []byte{1, 0x81, 1, 0x7f}, nil, coreruntime.TrapUnreachable, 4},
		{"zero", []byte{0x20, 0, 0x20, 1, 0x6d, 0x0b}, nil, []uint64{dirty(17), dirty(0)}, coreruntime.TrapDivZero, 5},
		{"overflow", []byte{0x20, 0, 0x20, 1, 0x6d, 0x0b}, nil, []uint64{dirty(0x80000000), dirty(0xffffffff)}, coreruntime.TrapDivOverflow, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := bytesForI32(len(tc.args))
			callerBody := []byte{}
			for i := range tc.args {
				callerBody = append(callerBody, 0x20, byte(i))
			}
			callerBody = append(callerBody, 0x10, 1, 0x41, 7, 0x6a, 0x0b)
			f := productionCalls(t, binaryModule(fixtureFunc{params: params, result: 0x7f, body: callerBody}, fixtureFunc{params: params, result: 0x7f, locals: tc.locals, body: tc.body}, fixtureFunc{result: 0x7f, body: []byte{0x41, 42, 0x0b}}), false)
			for range 10 {
				result := []uint64{0x55555555}
				err := f.invoke(0, tc.args, result)
				var trap *coreruntime.TrapError
				if !errors.As(err, &trap) || trap.Code != tc.code || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || trap.Frames[0].ProgramCounter != tc.pc || !trap.Frames[0].HasProgramCounter || result[0] != 0x55555555 {
					t.Fatal("production cold/source trap", result, err)
				}
				if err := f.invoke(2, nil, result); err != nil || result[0] != 42 {
					t.Fatal("production trap reuse", result, err)
				}
			}
		})
	}
}

func bytesForI32(n int) []byte {
	params := make([]byte, n)
	for i := range params {
		params[i] = 0x7f
	}
	return params
}

func TestProductionEnvelopeRecursionBoundsAndWarmAllocation(t *testing.T) {
	f := productionCalls(t, binaryModule(sumCallFixture(0)), false)
	result := []uint64{0x5555}
	err := f.invoke(0, []uint64{10000}, result)
	var trap *coreruntime.TrapError
	if !errors.As(err, &trap) || trap.Code != coreruntime.TrapStackFenceBreached || result[0] != 0x5555 {
		t.Fatal("production recursive stack guard", result, err)
	}
	args := []uint64{dirty(10)}
	if err := f.invoke(0, args, result); err != nil || result[0] != 55 {
		t.Fatal("production recursive reuse", result, err)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if err := f.invoke(0, args, result); err != nil {
			panic(err)
		}
	}); allocations != 0 {
		t.Fatal("production warm call allocations", allocations)
	}
}

func TestProductionEnvelopeHeapPublicationAndExactFrameBounds(t *testing.T) {
	for _, heap := range []bool{false, true} {
		data := binaryModule(fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x0b}})
		f := productionCallsWithOptions(t, data, ProductionOptions{DeferCodeMapping: heap})
		// The bridge saves 128 bytes, and this fully framed identity needs 32.
		// Leave 16: the bridge check passes but the guest must reject its frame.
		f.memory.SetStackFence(f.engine.StackTop() - 128 - 16)
		result := []uint64{0x5555}
		args := []uint64{dirty(41)}
		var trap *coreruntime.TrapError
		err := f.invoke(0, args, result)
		if !errors.As(err, &trap) || trap.Code != coreruntime.TrapStackFenceBreached || result[0] != 0x5555 {
			t.Fatal("guest frame exceeded available stack", result, err)
		}
		f.memory.SetStackFence(f.engine.StackTop() - 128 - 32)
		if err := f.invoke(0, args, result); err != nil || result[0] != 41 {
			t.Fatal("exact guest frame capacity rejected", result, err)
		}
		f.memory.SetStackFence(f.engine.StackLimit())
	}
}
