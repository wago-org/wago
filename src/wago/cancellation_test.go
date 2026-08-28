//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestCallContextInterruptsNativeLoop(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("spin", 0, 0),
			wasmtest.ExportEntry("value", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}), // loop { br 0 }
			wasmtest.Code([]byte{0x41, 0x07, 0x0b}),
		)),
	)
	rt := NewRuntime()
	defer rt.Close()
	compiled, err := rt.Compile(mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), compiled)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := in.Call(ctx, "spin"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("spin error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancellation took %v, want bounded interruption", elapsed)
	}

	// The watcher must leave the shared trap cell clean for the next invocation.
	out, err := in.Call(context.Background(), "value")
	if err != nil || len(out) != 1 || out[0].I32() != 7 {
		t.Fatalf("post-cancel value = %v, %v; want 7", out, err)
	}
}

func TestRuntimeInstantiateContextInterruptsNativeStartLoop(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(8, wasmtest.ULEB(0)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}),
		)),
	)
	rt := NewRuntime()
	defer rt.Close()
	compiled, err := rt.Compile(mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if in, err := rt.Instantiate(ctx, compiled); in != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("infinite start instantiate = %v, %v; want nil, context deadline", in, err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("start cancellation took %v, want bounded interruption", elapsed)
	}
}

func TestStartCancellationWatchBackgroundAllocFree(t *testing.T) {
	in := &Instance{trap: make([]byte, 4)}
	if got := testing.AllocsPerRun(100, func() {
		in.startCancellationWatch(context.Background(), in.trap)()
	}); got != 0 {
		t.Fatalf("background cancellation watch allocations = %v, want 0", got)
	}
}

func TestInstantiateContextChecksImportedStartBeforeDispatch(t *testing.T) {
	compiled := MustCompile(importedStartModule())
	defer compiled.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	in, err := Instantiate(compiled, InstantiateOptions{
		Context: ctx,
		Imports: Imports{"env.start": HostFunc(func(HostModule, []uint64, []uint64) {
			calls++
		})},
	})
	if in != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled imported start = %v, %v; want nil, context cancellation", in, err)
	}
	if calls != 0 {
		t.Fatalf("canceled imported start ran %d time(s), want 0", calls)
	}
}

func TestRuntimeInstantiateContextCancelsNativeEntryWait(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(8, wasmtest.ULEB(0)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithIndependentInstanceExecution(false)))
	defer rt.Close()
	compiled, err := rt.Compile(mod)
	if err != nil {
		t.Fatal(err)
	}

	nativeExecutionMu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		in, instantiateErr := rt.Instantiate(ctx, compiled)
		if in != nil {
			_ = in.Close()
		}
		done <- instantiateErr
	}()
	select {
	case err := <-done:
		nativeExecutionMu.Unlock()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("native-entry wait error = %v, want context deadline", err)
		}
	case <-time.After(time.Second):
		nativeExecutionMu.Unlock()
		<-done
		t.Fatal("instantiation did not cancel while the native-entry lease was held")
	}
}

func TestRuntimeInstantiateContextCancelsHostResumeWait(t *testing.T) {
	void := wasmtest.FuncType(nil, nil)
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("block")...), 0x00, 0x00)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(void)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(8, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithIndependentInstanceExecution(false)))
	defer rt.Close()
	compiled, err := rt.Compile(mod)
	if err != nil {
		t.Fatal(err)
	}

	hostEntered := make(chan struct{})
	leaseHeld := make(chan struct{})
	releaseLease := make(chan struct{})
	competitorDone := make(chan struct{})
	go func() {
		defer close(competitorDone)
		select {
		case <-hostEntered:
		case <-releaseLease:
			return
		}
		nativeExecutionMu.Lock()
		close(leaseHeld)
		<-releaseLease
		nativeExecutionMu.Unlock()
	}()
	defer func() {
		close(releaseLease)
		<-competitorDone
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		in, instantiateErr := rt.Instantiate(ctx, compiled, WithImports(Imports{
			"env.block": HostFunc(func(HostModule, []uint64, []uint64) {
				close(hostEntered)
				<-leaseHeld
			}),
		}))
		if in != nil {
			_ = in.Close()
		}
		done <- instantiateErr
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("host-resume wait error = %v, want context deadline", err)
		}
	case <-time.After(time.Second):
		t.Fatal("instantiation did not cancel while host resume waited for the native lease")
	}
}

// TestInvokeContextInterruptsHostCallLoop guards the runaway-guest guard itself:
// a guest that calls a host import on every loop iteration must be interruptible
// by context, and must not be pre-empted by any fixed host-call re-entry cap. The
// loop here issues far more than the historical 1<<20 re-entry bound before its
// deadline; the cooperative trap-cell interrupt — not a "too many host calls"
// error — is what must break it.
func TestInvokeContextInterruptsHostCallLoop(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	// import env.tick : () -> i32
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("tick")...), 0x00, 0x00)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, i32), // type 0: () -> i32 (the import)
			wasmtest.FuncType(nil, nil), // type 1: () -> ()  (spin)
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))), // func 1 (spin) has type 1
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("spin", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			// spin(): loop { drop(call $tick); br 0 }
			wasmtest.Code([]byte{0x03, 0x40, 0x10, 0x00, 0x1a, 0x0c, 0x00, 0x0b, 0x0b}),
		)),
	)
	calls := 0
	var cancelRequested time.Time
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := MustCompile(mod)
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.tick": HostFunc(func(_ HostModule, _, r []uint64) {
		calls++
		if calls == 1<<20+1 {
			cancelRequested = time.Now()
			cancel()
		}
		r[0] = I32(0)
	})}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	if _, err := in.InvokeContext(ctx, "spin"); !errors.Is(err, context.Canceled) {
		t.Fatalf("spin error = %v, want context cancellation (a re-entry-cap error here is the regression)", err)
	}
	if cancelRequested.IsZero() {
		t.Fatal("host loop returned before requesting cancellation")
	}
	// Only bound the interruption latency after the cancellation request. The
	// million host calls before it deliberately test the removed re-entry cap and
	// become much slower under the race detector without weakening cancellation.
	if elapsed := time.Since(cancelRequested); elapsed > time.Second {
		t.Fatalf("interruption after cancellation took %v, want bounded interruption", elapsed)
	}
	// Prove the loop sailed past the historical 1<<20 host-call re-entry cap:
	// interruption, not a synthetic bound, is what stopped it.
	if calls <= 1<<20 {
		t.Fatalf("host calls = %d, want > %d (loop must exceed the old cap to be a real regression guard)", calls, 1<<20)
	}
}

func TestInvokeContextHostPanicStopsCancellationWatch(t *testing.T) {
	void := wasmtest.FuncType(nil, nil)
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("panic")...), 0x00, 0x00)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(void)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
	compiled := MustCompile(mod)
	defer compiled.Close()
	panicValue := errors.New("host panic sentinel")
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.panic": HostFunc(func(HostModule, []uint64, []uint64) {
		panic(panicValue)
	})}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	ctx, cancel := context.WithCancel(context.Background())
	func() {
		defer func() {
			if got := recover(); got != panicValue {
				t.Fatalf("host panic = %v, want exact sentinel", got)
			}
		}()
		_, _ = in.InvokeContext(ctx, "run")
	}()
	trapPtr := (*uint32)(unsafe.Pointer(&in.trap[0]))
	beforeCancel := atomic.LoadUint32(trapPtr)
	cancel()
	time.Sleep(2 * time.Millisecond)
	afterCancel := atomic.LoadUint32(trapPtr)
	if afterCancel != beforeCancel {
		t.Fatalf("cancellation callback survived host panic: trap %d -> %d", beforeCancel, afterCancel)
	}
}

func TestInvokeContextInterruptsNativeLoop(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("spin", 0, 0),
			wasmtest.ExportEntry("value", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}), // loop { br 0 }
			wasmtest.Code([]byte{0x41, 0x07, 0x0b}),
		)),
	)
	rt := NewRuntime()
	defer rt.Close()
	compiled, err := rt.Compile(mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), compiled)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := in.InvokeContext(ctx, "spin"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("spin error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancellation took %v, want bounded interruption", elapsed)
	}

	// The watcher must leave the shared trap cell clean for the next invocation.
	out, err := in.InvokeContext(context.Background(), "value")
	if err != nil || len(out) != 1 || out[0] != 7 {
		t.Fatalf("post-cancel value = %v, %v; want 7", out, err)
	}
}
