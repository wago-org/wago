//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestTailDynamicRV64ActiveForeignInterruptAndReuse(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}, Results: []wasm.ValType{wasm.V128}}
	f, view := tailInvocationFixtureForTest(t, false, 2, 2)
	body := func(source uintptr) func(*integerEmitter) {
		return func(e *integerEmitter) {
			e.a.Constant64(enc.X28, int64(tailRAPointer(f.witness)+240))
			e.a.Addi(enc.X5, enc.Zero, 1)
			e.a.Sw(enc.X5, enc.X28, 0)
			tailDynamicReferenceForTest(e, source)
			e.tailRAProofCall(0, sig, 0, func(i int) int32 { return int32(48 + i*16) })
		}
	}
	a := tailRANativeFrameInvocation(t, sig, f.a, f.ca, f.da, 0, true, body(tailRAPointer(f.witness)+224))
	b := tailRANativeFrameInvocation(t, sig, f.b, f.cb, f.db, 1, true, body(tailRAPointer(f.witness)+232))
	leaf := tailRANativeFrameInvocation(t, sig, f.b, f.cb, f.db, 2, true, func(e *integerEmitter) { tailRANativeSerialize(e, sig.Results, 0) })
	oa := tailDynamicOwnerForTest(t, []tailRAFunction{a})
	ob := tailDynamicOwnerForTest(t, []tailRAFunction{b, leaf})
	a, b = oa.functions[0], ob.functions[0]
	f.da, f.db = oa.descriptors, ob.descriptors
	root := tailRANativeRoot(t, a, f.witness)
	if err := root.Seal(); err != nil {
		t.Fatal(err)
	}
	owners := []tailRADynamicOwner{oa, ob}
	for _, fn := range []tailRAFunction{a, b} {
		s := tailDynamicSiteForTest(t, fn, owners, fn.info.FrameBytes-8)
		s.Continuations = append(s.Continuations, NativeTailCodeRange{root.Base(), uintptr(len(root.Bytes()))})
		tailDynamicBindForTest(t, s, len(a.snapshot)+len(b.snapshot)+len(leaf.snapshot)+len(root.Bytes()))
	}
	binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(ob.descriptors)+40))
	binary.LittleEndian.PutUint64(f.witness[232:], uint64(tailRAPointer(oa.descriptors)+40))
	for i := range f.results {
		f.results[i] = 0xa5
	}
	f.bind(t, f.e.StackLimit())
	f.a.SetRV64InvocationView(view)
	done := make(chan error, 1)
	go func() { done <- f.e.CallPrepared(root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results) }()
	word := (*uint32)(unsafe.Pointer(&f.witness[240]))
	deadline, started := time.Now().Add(2*time.Second), false
	for time.Now().Before(deadline) {
		if atomic.LoadUint32(word) != 0 {
			started = true
			break
		}
		select {
		case err := <-done:
			t.Fatal("dynamic cycle returned", err)
		default:
		}
		time.Sleep(100 * time.Microsecond)
	}
	stop := coreruntime.RequestInterruptAsync(f.trap)
	err := <-done
	stop()
	var trap *coreruntime.TrapError
	if !started || !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex > 1 || trap.Frames[0].ProgramCounter != 3 {
		t.Fatal("active dynamic entry poll", err)
	}
	if !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
		t.Fatal("cancelled hop committed results")
	}
	binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(ob.descriptors)+80))
	binary.LittleEndian.PutUint64(f.args, 0x7ff0000000000001)
	binary.LittleEndian.PutUint64(f.args[8:], 0x8000000000000000)
	f.bind(t, f.e.StackLimit())
	f.a.SetRV64InvocationView(view)
	if err := f.e.CallPrepared(root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results); err != nil || binary.LittleEndian.Uint64(f.results) != 0x7ff0000000000001 || binary.LittleEndian.Uint64(f.results[8:]) != 0x8000000000000000 {
		t.Fatal("cold dynamic reuse", err)
	}
	f.assertReturned(t)
}
