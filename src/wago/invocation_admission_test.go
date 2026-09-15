package wago

import (
	"context"
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCallCancellationWhileWaitingForAdmission(t *testing.T) {
	c := MustCompile(benchAddOneModule())
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	state := in.lockInvocation(0)
	released := false
	defer func() {
		if !released {
			state.unlockInvocation()
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := in.Call(ctx, "f", ValueI32(41)); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Call = %v, want canceled", err)
		}
	case <-time.After(time.Second):
		state.unlockInvocation()
		released = true
		<-done
		t.Fatal("canceled Call remained blocked on invocation admission")
	}
	state.unlockInvocation()
	released = true
	out, err := in.Call(context.Background(), "f", ValueI32(41))
	if err != nil || len(out) != 1 || out[0].I32() != 42 {
		t.Fatalf("next Call = %v, %v", out, err)
	}
}

// admissionContext reports when a waiter reaches the cancellable wait.
type admissionContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *admissionContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestInvocationGateCanceledWaiters(t *testing.T) {
	for _, count := range []int{1, 64} {
		var gate invocationGate
		gate.Lock()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, count)
		for i := 0; i < count; i++ {
			waiter := &admissionContext{Context: ctx, waiting: make(chan struct{})}
			go func() { done <- gate.lockContext(waiter) }()
			<-waiter.waiting
		}
		cancel()
		for i := 0; i < count; i++ {
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("waiter = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("waiter did not return")
			}
		}
		gate.Unlock()
		gate.Lock()
		gate.Unlock()
	}
}

func TestInvocationGateCancellationAtAdmission(t *testing.T) {
	for i := 0; i < 100; i++ {
		var gate invocationGate
		gate.Lock()
		base, cancel := context.WithCancel(context.Background())
		ctx := &admissionContext{Context: base, waiting: make(chan struct{})}
		done := make(chan error, 1)
		go func() { done <- gate.lockContext(ctx) }()
		<-ctx.waiting
		cancel()
		gate.Unlock()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("admission = %v", err)
		}
		gate.Lock()
		gate.Unlock()
	}
}

func TestCallDeadlineWhileCallbackOwnsAdmission(t *testing.T) {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	c := MustCompile(returningImportModule(sig, []byte{0, 0x20, 0, 0x10, 0, 0x0b}))
	defer c.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": func(v int32) int32 {
		once.Do(func() { close(entered); <-release })
		return v + 1
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	done := make(chan error, 1)
	go func() {
		out, err := in.Call(context.Background(), "g", ValueI32(41))
		if err == nil && (len(out) != 1 || out[0].I32() != 42) {
			err = fmt.Errorf("first call = %v", out)
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("callback did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	waiter := make(chan error, 1)
	go func() { _, err := in.Call(ctx, "g", ValueI32(9)); waiter <- err }()
	select {
	case err := <-waiter:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("waiter = %v", err)
		}
	case <-time.After(time.Second):
		close(release)
		<-done
		<-waiter
		t.Fatal("deadline did not cancel admission")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	out, err := in.Call(context.Background(), "g", ValueI32(41))
	if err != nil || len(out) != 1 || out[0].I32() != 42 {
		t.Fatalf("next call = %v, %v", out, err)
	}
}

func BenchmarkCallAdmission(b *testing.B) {
	c := MustCompile(benchAddOneModule())
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Call(context.Background(), "f", ValueI32(41)); err != nil {
			b.Fatal(err)
		}
	}
}

func TestInvocationGateUncontendedAllocations(t *testing.T) {
	var gate invocationGate
	allocations := testing.AllocsPerRun(100, func() {
		if err := gate.lockContext(context.Background()); err != nil {
			panic(err)
		}
		gate.Unlock()
	})
	if allocations != 0 {
		t.Fatalf("uncontended gate allocations = %v, want 0", allocations)
	}
}

func TestContextEntryCancellationWhileWaiting(t *testing.T) {
	for _, entry := range []string{"invoke", "host-token"} {
		t.Run(entry, func(t *testing.T) {
			in := &Instance{}
			state := in.lockInvocation(0)
			ctx, cancel := context.WithCancel(context.Background())
			contexts := invocationContextSetFor(ctx)
			cancel()
			done := make(chan error, 1)
			go func() {
				var err error
				if entry == "invoke" {
					_, err = in.invokeEntry("unused", nil, contexts, false)
				} else {
					_, err = in.invokeWithToken("unused", nil, contexts, newInvocationID(), false, false, nil)
				}
				done <- err
			}()
			select {
			case err := <-done:
				state.unlockInvocation()
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("entry = %v", err)
				}
			case <-time.After(time.Second):
				state.unlockInvocation()
				<-done
				t.Fatal("canceled context entry waited for admission")
			}
		})
	}
}

func TestInvocationGateConcurrentOwners(t *testing.T) {
	var gate invocationGate
	var owners atomic.Int32
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 100; j++ {
				gate.Lock()
				if owners.Add(1) != 1 {
					t.Error("multiple invocation owners")
				}
				goruntime.Gosched()
				if owners.Add(-1) != 0 {
					t.Error("invocation ownership changed")
				}
				gate.Unlock()
			}
		}()
	}
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("invocation owner lost its wakeup")
	}
}
