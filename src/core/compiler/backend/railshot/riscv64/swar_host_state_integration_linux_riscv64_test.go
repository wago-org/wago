//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

type swarHostCombinedInstance struct {
	engine              *coreruntime.Engine
	job                 *coreruntime.JobMemory
	args, results, trap []byte
	ctrl                []byte
	globals             *swarGlobalsOwner
	data                *swarDataOwner
	host                *swarHostOwner
}

func swarHostCombinedInstanceRV64(t *testing.T, image *swarModuleImage, guest, thunks *coreruntime.CodeBuffer) *swarHostCombinedInstance {
	t.Helper()
	e, _, args, results, trap, ctrl := thunkFixtureRV64(t, 4)
	j, err := coreruntime.NewJobMemoryGrowable(65536, 131072)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := j.Close(); err != nil {
			t.Error(err)
		}
	})
	j.SetStackFence(e.StackLimit())
	j.SetCustomCtx(thunkPointerRV64(ctrl))
	if err := j.RebindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	in := &swarHostCombinedInstance{engine: e, job: j, args: args, results: results, trap: trap, ctrl: ctrl}
	in.globals, err = image.instantiateGlobals(j)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := in.globals.Close(); err != nil {
			t.Error(err)
		}
	})
	in.data, err = image.instantiateData(j)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := in.data.Close(); err != nil {
			t.Error(err)
		}
	})
	in.host = swarHostOwnerRV64(t, image, guest, thunks, j)
	return in
}

func TestSWARRV64CombinedHostGlobalDataParkedNestedCloseDropPanicCancelReuse(t *testing.T) {
	source, fixtures, initial, initialMemory, payload := swarHostCombinedStateSource(256)
	image, err := compileSWARHostModule(source, emissionMode{production: true, interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	guest, thunks := swarHostCodeBuffers(t, image)
	outer := swarHostCombinedInstanceRV64(t, image, guest, thunks)
	inner := swarHostCombinedInstanceRV64(t, image, guest, thunks)
	entry := guest.Base() + uintptr(image.functions[0].Entry)
	if outer.globals.directory == inner.globals.directory || outer.data.directory == inner.data.directory || outer.host.dispatch == inner.host.dispatch || outer.host.directory == inner.host.directory {
		t.Fatal("combined per-instance state aliased")
	}
	input := [2]uint64{0xfff0000000000001, 0x7fa1234500000001}
	hostResult := [4]uint64{0x80000001, 0x8123456789abcdef, 0x80000000, 0xfff0000000000001}
	prepare := func(in *swarHostCombinedInstance, value [2]uint64, drop uint32) {
		clear(in.trap)
		binary.LittleEndian.PutUint64(in.args, value[0])
		binary.LittleEndian.PutUint64(in.args[8:], value[1])
		binary.LittleEndian.PutUint64(in.args[16:], dirty(101))
		binary.LittleEndian.PutUint64(in.args[24:], dirty(drop))
		for i := range in.results {
			in.results[i] = 0xa5
		}
	}
	callback := func(expected *swarHostCombinedInstance) func(uintptr, uint32, []uint64, []uint64) {
		return func(ctrl uintptr, index uint32, args, results []uint64) {
			if ctrl != thunkPointerRV64(expected.ctrl) || index != 255 || len(args) != 4 || len(results) != 4 || args[0] != 101 || args[1] != 0x8123456789abcdef || args[2] != 0x7f800123 || args[3] != 0xfff0000000000001 {
				panic("combined scalar host metadata/raw/normalized carrier")
			}
			// Deliberately dirty narrow callback results must normalize on resume.
			results[0], results[1], results[2], results[3] = dirty(uint32(hostResult[0])), hostResult[1], dirty(uint32(hostResult[2])), hostResult[3]
		}
	}
	check := func(in *swarHostCombinedInstance, old, value [2]uint64, oldMemory []byte) error {
		expected := []uint64{old[0], old[1], value[0], value[1], binary.LittleEndian.Uint64(oldMemory), binary.LittleEndian.Uint64(oldMemory[8:]), 0x7ff0000000000001, hostResult[0], hostResult[1], hostResult[2], hostResult[3], value[0], value[1], binary.LittleEndian.Uint64(payload), binary.LittleEndian.Uint64(payload[8:])}
		for i, want := range expected {
			if binary.LittleEndian.Uint64(in.results[i*8:]) != want {
				return fmt.Errorf("combined parked result cell %d: got %x, want %x", i, in.results[:120], expected)
			}
		}
		if !bytes.Equal(in.results[120:], bytes.Repeat([]byte{0xa5}, len(in.results)-120)) || !bytes.Equal(in.job.CurrentBytes()[101:117], payload) || !bytes.Equal(in.job.CurrentBytes()[7:9], []byte{0xa5, 0x5a}) {
			return errors.New("combined results/memory neighbors or tail")
		}
		cell := swarOwnedGlobalCell(in.globals, 0, 16)
		if binary.LittleEndian.Uint64(cell) != value[0] || binary.LittleEndian.Uint64(cell[8:]) != value[1] || binary.LittleEndian.Uint64(swarOwnedGlobalCell(in.globals, 1, 8)) != 0x7ff0000000000001 {
			return errors.New("combined mutable/neighbor raw cells")
		}
		return nil
	}
	prepare(outer, input, 0)
	outerCalls, innerCalls := 0, 0
	nestedValue := [2]uint64{0x8000000000000000, 0x7ff00000cafebabe}
	err = outer.engine.CallWithHostBase(entry, outer.args, outer.job.LinMemBase(), outer.trap, outer.results, outer.ctrl, func(ctrl uintptr, index uint32, args, results []uint64) {
		outerCalls++
		goruntime.GC()
		prepare(inner, nestedValue, 0)
		if err := inner.engine.CallWithHostBase(entry, inner.args, inner.job.LinMemBase(), inner.trap, inner.results, inner.ctrl, func(ctrl uintptr, index uint32, args, results []uint64) {
			innerCalls++
			goruntime.GC()
			callback(inner)(ctrl, index, args, results)
		}); err != nil {
			panic(err)
		}
		if err := check(inner, initial, nestedValue, initialMemory); err != nil {
			panic(err)
		}
		// Inner calls have joined, while the outer stack remains parked. Retire
		// all inner owners without touching the shared executable images.
		if err := inner.host.Close(); err != nil {
			panic(err)
		}
		if err := inner.data.Close(); err != nil {
			panic(err)
		}
		if err := inner.globals.Close(); err != nil {
			panic(err)
		}
		callback(outer)(ctrl, index, args, results)
	})
	if err != nil || outerCalls != 1 || innerCalls != 1 {
		t.Fatal("combined nested/parked sibling retirement", outerCalls, innerCalls, err)
	}
	if err := check(outer, initial, input, initialMemory); err != nil {
		t.Fatal(err)
	}
	ctx := outer.job.CaptureInstanceContext()
	if err := outer.host.Close(); err != nil {
		t.Fatal(err)
	}
	closed := outer.job.CaptureInstanceContext()
	if closed.ImportDispatch != 0 || closed.FuncRefDescPtr != 0 || closed.GlobalsPtr != ctx.GlobalsPtr || closed.PassiveDataPtr != ctx.PassiveDataPtr {
		t.Fatal("combined host retirement damaged globals/data")
	}
	outer.host = swarHostOwnerRV64(t, image, guest, thunks, outer.job)
	prepare(outer, input, 1)
	if err := outer.engine.CallWithHostBase(entry, outer.args, outer.job.LinMemBase(), outer.trap, outer.results, outer.ctrl, callback(outer)); err != nil {
		t.Fatal(err)
	}
	if err := check(outer, input, input, payload); err != nil {
		t.Fatal(err)
	}
	directory := swarOwnedDataDirectory(outer.data, 3)
	if binary.LittleEndian.Uint32(directory[40:]) != 0 || binary.LittleEndian.Uint64(directory[32:]) == 0 || binary.LittleEndian.Uint32(directory[44:]) != 0 {
		t.Fatal("combined dropped descriptor pointer/pad")
	}
	// The leaf has already returned its four scalar results when the dropped
	// data operation traps in the root. Prior global and result-prefix effects
	// persist; memory and all later root result writes must remain blocked.
	newValue := [2]uint64{input[0] ^ 1, input[1] ^ 1}
	prepare(outer, newValue, 0)
	err = outer.engine.CallWithHostBase(entry, outer.args, outer.job.LinMemBase(), outer.trap, outer.results, outer.ctrl, callback(outer))
	var trap *coreruntime.TrapError
	pc := uint32(len(fixtures[0].locals) + bytes.Index(fixtures[0].body, []byte{0xfc, 8}))
	if !errors.As(err, &trap) || trap.Code != coreruntime.TrapLinMemOutOfBounds || binary.LittleEndian.Uint64(outer.trap[16:]) != uint64(pc)<<32|257 || !bytes.Equal(outer.job.CurrentBytes()[101:117], payload) || !bytes.Equal(outer.results[32:], bytes.Repeat([]byte{0xa5}, len(outer.results)-32)) {
		t.Fatal("combined post-host dropped-data cold source/ordered effects", err)
	}
	for i, want := range hostResult {
		if binary.LittleEndian.Uint64(outer.results[i*8:]) != want {
			t.Fatal("post-host trap lost already returned scalar prefix", i)
		}
	}
	globalCell := swarOwnedGlobalCell(outer.globals, 0, 16)
	if binary.LittleEndian.Uint64(globalCell) != newValue[0] || binary.LittleEndian.Uint64(globalCell[8:]) != newValue[1] {
		t.Fatal("post-host trap rolled back earlier global mutation")
	}
	if err := outer.data.Close(); err != nil {
		t.Fatal(err)
	}
	if after := outer.job.CaptureInstanceContext(); after.PassiveDataPtr != 0 || after.ImportDispatch != outer.host.dispatch || after.FuncRefDescPtr != outer.host.directory || after.GlobalsPtr != outer.globals.directory {
		t.Fatal("data retirement damaged host/global identity")
	}
	outer.data, err = image.instantiateData(outer.job)
	if err != nil {
		t.Fatal(err)
	}
	prepare(outer, input, 0)
	marker := new(int)
	func() {
		defer func() {
			if recover() != marker {
				t.Error("combined parked panic identity")
			}
		}()
		_ = outer.engine.CallWithHostBase(entry, outer.args, outer.job.LinMemBase(), outer.trap, outer.results, outer.ctrl, func(uintptr, uint32, []uint64, []uint64) { panic(marker) })
	}()
	if !bytes.Equal(outer.results, bytes.Repeat([]byte{0xa5}, len(outer.results))) || !bytes.Equal(outer.job.CurrentBytes()[101:117], initialMemory) || binary.LittleEndian.Uint64(globalCell) != input[0] || binary.LittleEndian.Uint64(globalCell[8:]) != input[1] {
		t.Fatal("panicked callback resumed root or rolled back earlier global")
	}
	prepare(outer, newValue, 1)
	err = outer.engine.CallWithHostBase(entry, outer.args, outer.job.LinMemBase(), outer.trap, outer.results, outer.ctrl, func(uintptr, uint32, []uint64, []uint64) { coreruntime.RequestInterrupt(outer.trap) })
	if !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted || !bytes.Equal(outer.results, bytes.Repeat([]byte{0xa5}, len(outer.results))) || !bytes.Equal(outer.job.CurrentBytes()[101:117], initialMemory) || binary.LittleEndian.Uint32(swarOwnedDataDirectory(outer.data, 3)[40:]) != 16 || binary.LittleEndian.Uint64(globalCell) != newValue[0] || binary.LittleEndian.Uint64(globalCell[8:]) != newValue[1] {
		t.Fatal("cancelled host resumed memory/drop/results or lost earlier global", err)
	}
	prepare(outer, input, 0)
	if err := outer.engine.CallWithHostBase(entry, outer.args, outer.job.LinMemBase(), outer.trap, outer.results, outer.ctrl, callback(outer)); err != nil {
		t.Fatal("combined engine/state reuse", err)
	}
	if err := check(outer, newValue, input, initialMemory); err != nil {
		t.Fatal(err)
	}
	// A fresh data owner resets active memory but does not replace host/global
	// identities. Successful warm mutation/call/reload remains allocation-free.
	warmCallback := callback(outer)
	if n := testing.AllocsPerRun(50, func() {
		// Defined calls share and replace the argument-arena prefix. Rebuild
		// root carriers on every invocation; host-returned scratch is not input.
		prepare(outer, input, 0)
		if err := outer.engine.CallWithHostBase(entry, outer.args, outer.job.LinMemBase(), outer.trap, outer.results, outer.ctrl, warmCallback); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("combined host/global/data warm allocations", n)
	}
}
