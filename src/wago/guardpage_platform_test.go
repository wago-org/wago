//go:build wago_guardpage && (linux || darwin || windows) && (amd64 || arm64)

package wago

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
)

func instantiatePlatformGuardLoad(t *testing.T, c *Compiled) *Instance {
	t.Helper()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate guard module: %v", err)
	}
	return in
}

func compilePlatformGuardLoad(t *testing.T) *Compiled {
	t.Helper()
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased), loadModule())
	if err != nil {
		t.Fatalf("compile guard module: %v", err)
	}
	return c
}

// TestGuardPageGOMAXPROCSOne proves fault delivery is synchronous on the
// faulting thread and does not depend on a helper goroutine.
func TestGuardPageGOMAXPROCSOne(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)

	in := instantiatePlatformGuardLoad(t, compilePlatformGuardLoad(t))
	defer in.Close()
	if _, err := in.Invoke("f", I32(1<<20)); err == nil {
		t.Fatal("out-of-bounds load did not trap with GOMAXPROCS=1")
	}
	if r, err := in.Invoke("f", I32(8)); err != nil || AsI32(r[0]) != 0 {
		t.Fatalf("instance did not remain usable after trap: result=%v err=%v", r, err)
	}
}

// TestGuardPageParallelFaults checks that handler state comes only from
// the faulting context and reservation registry. No per-call global identifies
// the active instance, so independent guarded calls can fault concurrently.
func TestGuardPageParallelFaults(t *testing.T) {
	const workers = 16
	c := compilePlatformGuardLoad(t)
	instances := make([]*Instance, workers)
	for i := range instances {
		instances[i] = instantiatePlatformGuardLoad(t, c)
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
func causePlatformNonWasmFault() {
	var p *byte
	*p = 1
}

func recoverPlatformNonWasmFault() (recovered any) {
	defer func() { recovered = recover() }()
	causePlatformNonWasmFault()
	return nil
}

// TestGuardPageChainsGoFaults verifies that an unrelated memory fault is
// forwarded to the Go runtime's saved handler and remains a recoverable Go
// panic instead of being swallowed or misreported as a wasm trap.
func TestGuardPageChainsGoFaults(t *testing.T) {
	in := instantiatePlatformGuardLoad(t, compilePlatformGuardLoad(t))
	defer in.Close()
	if got := recoverPlatformNonWasmFault(); got == nil {
		t.Fatal("nil-pointer fault was not delivered to the Go runtime")
	}
}
