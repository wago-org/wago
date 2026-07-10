//go:build darwin && arm64 && wago_guardpage

package wago

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
)

func instantiateDarwinGuardLoad(t *testing.T, c *Compiled) *Instance {
	t.Helper()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate guard module: %v", err)
	}
	return in
}

func compileDarwinGuardLoad(t *testing.T) *Compiled {
	t.Helper()
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased), loadModule())
	if err != nil {
		t.Fatalf("compile guard module: %v", err)
	}
	return c
}

// TestDarwinGuardPageGOMAXPROCSOne proves fault delivery is synchronous on the
// faulting thread. A Mach-port receiver implemented as a Go goroutine deadlocks
// here because enterNative retains the only P while wasm executes.
func TestDarwinGuardPageGOMAXPROCSOne(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)

	in := instantiateDarwinGuardLoad(t, compileDarwinGuardLoad(t))
	defer in.Close()
	if _, err := in.Invoke("f", I32(1<<20)); err == nil {
		t.Fatal("out-of-bounds load did not trap with GOMAXPROCS=1")
	}
	if r, err := in.Invoke("f", I32(8)); err != nil || AsI32(r[0]) != 0 {
		t.Fatalf("instance did not remain usable after trap: result=%v err=%v", r, err)
	}
}

// TestDarwinGuardPageParallelFaults checks that handler state comes only from
// the faulting context and reservation registry. No per-call global identifies
// the active instance, so independent guarded calls can fault concurrently.
func TestDarwinGuardPageParallelFaults(t *testing.T) {
	const workers = 16
	c := compileDarwinGuardLoad(t)
	instances := make([]*Instance, workers)
	for i := range instances {
		instances[i] = instantiateDarwinGuardLoad(t, c)
		defer instances[i].Close()
	}

	start := make(chan struct{})
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i, in := range instances {
		wg.Add(1)
		go func(i int, in *Instance) {
			defer wg.Done()
			<-start
			for n := 0; n < 32; n++ {
				if _, err := in.Invoke("f", I32(int32(1<<20+i*8))); err == nil {
					errCh <- fmt.Errorf("worker %d iteration %d: OOB load did not trap", i, n)
					return
				}
				if r, err := in.Invoke("f", I32(8)); err != nil || AsI32(r[0]) != 0 {
					errCh <- fmt.Errorf("worker %d iteration %d: in-bounds result=%v err=%v", i, n, r, err)
					return
				}
			}
		}(i, in)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

//go:noinline
func causeDarwinNonWasmFault() {
	var p *byte
	*p = 1
}

func recoverDarwinNonWasmFault() (recovered any) {
	defer func() { recovered = recover() }()
	causeDarwinNonWasmFault()
	return nil
}

// TestDarwinGuardPageChainsGoFaults verifies that an unrelated memory fault is
// forwarded to the Go runtime's saved handler and remains a recoverable Go
// panic instead of being swallowed or misreported as a wasm trap.
func TestDarwinGuardPageChainsGoFaults(t *testing.T) {
	in := instantiateDarwinGuardLoad(t, compileDarwinGuardLoad(t))
	defer in.Close()
	if got := recoverDarwinNonWasmFault(); got == nil {
		t.Fatal("nil-pointer fault was not delivered to the Go runtime")
	}
}
