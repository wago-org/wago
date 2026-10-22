//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func productionInterruptTrap(t *testing.T, err error, function, pc uint32) {
	t.Helper()
	var trap *coreruntime.TrapError
	if !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != function || trap.Frames[0].ProgramCounter != pc {
		t.Fatal("production interruption/source", err)
	}
}

// The foreign call releases the sole Go P. Observe one native-written byte,
// then publish interruption while the guest is still active. The first word's
// sole writer is the guest and only its transition from zero matters here.
func interruptActiveProduction(t *testing.T, f *productionCallFixture, entry int, args []uint64) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- f.invoke(entry, args, []uint64{0xcafe}) }()
	word := (*uint32)(unsafe.Pointer(&f.memory.HostBytes()[0]))
	deadline := time.Now().Add(2 * time.Second)
	started := false
	for time.Now().Before(deadline) {
		if atomic.LoadUint32(word) != 0 {
			started = true
			break
		}
		select {
		case err := <-done:
			t.Fatal("guest returned before active interruption", err)
		default:
		}
		time.Sleep(100 * time.Microsecond)
	}
	stop := coreruntime.RequestInterruptAsync(f.trap)
	defer stop()
	// Always wait for the foreign call before fixture cleanup can unmap its
	// code, stack or descriptors. A broken safepoint hangs until test timeout.
	err := <-done
	if !started {
		t.Fatal("guest progress was not observed", err)
	}
	return err
}

func TestProductionInterruptLoopNestedColdUnwindAndReuse(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	fs := []fixtureFunc{
		{body: []byte{3, 0x40, 0x41, 0, 0x41, 1, 0x36, 2, 0, 0x0c, 0, 0x0b, 0x0b}},
		{body: []byte{0x10, 0, 0x0b}},
		{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x0b}},
	}
	f := productionCallsWithOptions(t, stateModule(fs, false, 1, 1, true, nil, nil), ProductionOptions{Interruptible: true})
	productionInterruptTrap(t, interruptActiveProduction(t, f, 1, nil), 0, 1)
	clear(f.trap)
	result := []uint64{0}
	if err := f.invoke(2, []uint64{dirty(41)}, result); err != nil || result[0] != 41 {
		t.Fatal("reuse after nested interruption", result, err)
	}
	coreruntime.RequestInterrupt(f.trap)
	result[0] = 0xcafe
	productionInterruptTrap(t, f.invoke(2, []uint64{dirty(9)}, result), 2, 1)
	if result[0] != 0xcafe {
		t.Fatal("entry interruption committed caller output")
	}
	clear(f.trap)
	if allocs := testing.AllocsPerRun(100, func() {
		if err := f.invoke(2, []uint64{dirty(41)}, result); err != nil {
			panic(err)
		}
	}); allocs != 0 {
		t.Fatal("polling warm call allocated", allocs)
	}
}

func TestProductionInterruptBulkLoop(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	fs := []fixtureFunc{bulkFixture(11, 0), {result: 0x7f, body: []byte{0x41, 41, 0x0b}}}
	f := productionCallsWithOptions(t, stateModule(fs, false, 128, 128, true, nil, nil), ProductionOptions{Interruptible: true})
	productionInterruptTrap(t, interruptActiveProduction(t, f, 0, []uint64{dirty(0), dirty(0xab), dirty(128 * 65536)}), 0, 7)
	// Cancellation may occur after in-bounds writes. It must stop before the
	// complete long operation finishes and leave the runtime reusable.
	if f.memory.HostBytes()[128*65536-1] != 0 {
		t.Fatal("bulk operation completed before interruption")
	}
	clear(f.trap)
	result := []uint64{0}
	if err := f.invoke(1, nil, result); err != nil || result[0] != 41 {
		t.Fatal("reuse after bulk interruption", result, err)
	}
}

func TestProductionInterruptClearMultivalueControl(t *testing.T) {
	for _, tc := range multivalueCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := productionCallsWithOptions(t, binaryModule(tc.funcs...), ProductionOptions{Interruptible: true})
			for _, call := range tc.calls {
				result := make([]uint64, len(call.want))
				if err := f.invoke(0, call.args, result); err != nil || !slices.Equal(result, call.want) {
					t.Fatal("clear polls changed control/result carriers", result, call.want, err)
				}
			}
		})
	}
}
