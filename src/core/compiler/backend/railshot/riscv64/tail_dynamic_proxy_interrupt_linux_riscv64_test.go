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

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestTailDynamicProxyRV64ActiveCycleInterruptAndRetirement(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	x := tailDynamicProxyFixtureForTest(t, false, true)
	f := x.f
	for i := range f.results {
		f.results[i] = 0xa5
	}
	copy(f.args, x.input)
	binary.LittleEndian.PutUint64(f.args, ^uint64(0))
	clear(f.witness[244:248])
	f.bind(t, f.e.StackLimit())
	f.a.SetRV64InvocationView(x.view)
	done := make(chan error, 1)
	go func() { done <- f.e.CallPrepared(x.root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results) }()
	word := (*uint32)(unsafe.Pointer(&f.witness[244]))
	deadline, started := time.Now().Add(2*time.Second), false
	for time.Now().Before(deadline) {
		if atomic.LoadUint32(word) == 3 {
			started = true
			break
		}
		select {
		case err := <-done:
			t.Fatal("imported proxy cycle returned", err)
		default:
		}
		time.Sleep(100 * time.Microsecond)
	}
	stop := coreruntime.RequestInterruptAsync(f.trap)
	err := <-done
	stop()
	var trap *coreruntime.TrapError
	if !started || !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex > 1 || trap.Frames[0].ProgramCounter != 3 {
		t.Fatal("active imported proxy entry poll", err)
	}
	if !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
		t.Fatal("cancelled proxy committed result")
	}
	binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(x.ob.descriptors)+120))
	if err := x.invoke(t, x.root.Base(), 1); err != nil || !x.resultsMatch() {
		t.Fatal("cancelled proxy cold reuse", err)
	}
	f.assertReturned(t)
	for _, o := range x.dispatchers {
		if err := o.Close(); err != nil {
			t.Fatal(err)
		}
		if o.dynamic != nil || o.contexts != nil || o.code != nil {
			t.Fatal("retired proxy factory retained source/target bindings")
		}
	}
	if binary.LittleEndian.Uint64(f.ca[120:]) != 0 || binary.LittleEndian.Uint64(f.cb[120:]) != 0 {
		t.Fatal("retired capabilities remain visible")
	}
	for _, owner := range []tailRADynamicOwner{x.oa, x.ob} {
		for i := 0; i < len(owner.functions)+len(owner.targets); i++ {
			if owner.retainedFunction(i).code.Base() == 0 {
				t.Fatal("factory freed borrowed proxy/producer image")
			}
		}
	}
}
