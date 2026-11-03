//go:build !tinygo && !wago_target_tinygo

package gc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"
)

func helperBudgetProcessors(t *testing.T, processors int) {
	t.Helper()
	previous := runtime.GOMAXPROCS(processors)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
}

func helperBudgetCollector(t *testing.T, workers int) *Collector {
	t.Helper()
	c, err := NewCollector(Config{Workers: workers}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func requireHelperBudget(t *testing.T, want int32) {
	t.Helper()
	if got := processGCHelpers.Load(); got != want {
		t.Fatalf("process helper ownership = %d, want %d", got, want)
	}
}

func TestProcessHelperBudgetBoundsMultipleCollectors(t *testing.T) {
	helperBudgetProcessors(t, 8)
	requireHelperBudget(t, 0)
	a := helperBudgetCollector(t, 8)
	b := helperBudgetCollector(t, 8)
	pa, pb := a.parallelPool(), b.parallelPool()
	if pa.ownedHelpers != 7 || pb.ownedHelpers != 0 {
		t.Fatalf("grants = %d, %d, want 7, 0", pa.ownedHelpers, pb.ownedHelpers)
	}
	if pb.availableParticipants() != 1 {
		t.Fatal("denied collector cannot make owner-only progress")
	}
	requireHelperBudget(t, 7)

	a.Close()
	requireHelperBudget(t, 0)
	if got := b.parallelPool().ownedHelpers; got != 7 {
		t.Fatalf("reacquired helpers = %d, want 7", got)
	}
	b.Close()
	b.Close()
	requireHelperBudget(t, 0)
}

func TestProcessHelperBudgetPartialGrantAndConfiguredWorkers(t *testing.T) {
	helperBudgetProcessors(t, 8)
	requireHelperBudget(t, 0)
	a := helperBudgetCollector(t, 4)
	b := helperBudgetCollector(t, 8)
	pa, pb := a.parallelPool(), b.parallelPool()
	if pa.ownedHelpers != 3 || pb.ownedHelpers != 4 {
		t.Fatalf("grants = %d, %d, want 3, 4", pa.ownedHelpers, pb.ownedHelpers)
	}
	if b.cfg.Workers != 8 {
		t.Fatalf("configured reservation workers changed to %d", b.cfg.Workers)
	}
	pb.phase = parallelCopy
	pb.hook = func(uint8, int) {}
	pb.selectParticipants(parallelMinObjects * 8)
	if pb.participants != 5 {
		t.Fatalf("active copy participants = %d, want 5", pb.participants)
	}
	pb.hook = nil
	a.Close()
	b.Close()
	requireHelperBudget(t, 0)
}

func TestProcessHelperBudgetWorkersAndProcessors(t *testing.T) {
	for _, processors := range []int{1, 2, 4, 8} {
		for workers := 2; workers <= 8; workers++ {
			t.Run(fmt.Sprintf("processors%d/workers%d", processors, workers), func(t *testing.T) {
				previous := runtime.GOMAXPROCS(processors)
				defer runtime.GOMAXPROCS(previous)
				requireHelperBudget(t, 0)
				c := helperBudgetCollector(t, workers)
				p := c.parallelPool()
				want := min(workers-1, processors-1)
				if p.ownedHelpers != want || p.availableParticipants() != 1+want {
					t.Fatalf("helpers/available = %d/%d, want %d/%d", p.ownedHelpers, p.availableParticipants(), want, 1+want)
				}
				c.Close()
				requireHelperBudget(t, 0)
			})
		}
	}
}

func TestProcessHelperBudgetConcurrentClaimRelease(t *testing.T) {
	if helperBudgetSubprocess(t) {
		return
	}
	requireHelperBudget(t, 0)
	const contenders = 32
	start := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	grants := make(chan int, contenders)
	var wg sync.WaitGroup
	defer func() {
		unblock()
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		helperBudgetAwait(t, done, "claim/release cleanup")
	}()
	for range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			grant := claimProcessGCHelpers(1, maxProcessGCHelpers)
			grants <- grant
			<-release
			releaseProcessGCHelpers(grant)
		}()
	}
	close(start)
	var total int
	for range contenders {
		select {
		case grant := <-grants:
			total += grant
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for grants")
		}
	}
	if total != maxProcessGCHelpers {
		t.Fatalf("concurrent grants = %d, want %d", total, maxProcessGCHelpers)
	}
	requireHelperBudget(t, maxProcessGCHelpers)
	unblock()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	helperBudgetAwait(t, done, "claim/release completion")
	requireHelperBudget(t, 0)
}

func TestProcessHelperBudgetProcessorChanges(t *testing.T) {
	if helperBudgetSubprocess(t) {
		return
	}
	helperBudgetProcessors(t, 8)
	requireHelperBudget(t, 0)
	c := helperBudgetCollector(t, 8)
	p := c.parallelPool()
	if p.ownedHelpers != 7 {
		t.Fatalf("initial helpers = %d", p.ownedHelpers)
	}

	runtime.GOMAXPROCS(2)
	p.hook = func(uint8, int) {}
	p.selectParticipants(parallelMinObjects * 8)
	if p.participants != 2 || p.ownedHelpers != 7 {
		t.Fatalf("after shrink participants/helpers = %d/%d, want 2/7", p.participants, p.ownedHelpers)
	}
	runtime.GOMAXPROCS(1)
	p.selectParticipants(parallelMinObjects * 8)
	if p.participants != 1 || p.ownedHelpers != 7 {
		t.Fatalf("at one processor participants/helpers = %d/%d, want 1/7", p.participants, p.ownedHelpers)
	}
	p.hook = nil

	// Participant selection is frozen for an in-flight phase. A processor
	// change affects only the next phase and never revokes a running helper.
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	// Cleanup is LIFO: unblock before collector Close, including Fatal paths.
	t.Cleanup(unblock)
	p.hook = func(_ uint8, worker int) {
		if worker == 1 {
			close(entered)
			<-release
		}
	}
	runtime.GOMAXPROCS(8)
	p.selectParticipants(parallelMinObjects * 8)
	done := make(chan struct{})
	go func() { p.run(); close(done) }()
	t.Cleanup(func() { unblock(); helperBudgetAwait(t, done, "phase cleanup") })
	helperBudgetAwait(t, entered, "worker entry")
	runtime.GOMAXPROCS(1)
	if p.participants != 8 {
		t.Fatalf("in-flight participants changed to %d", p.participants)
	}
	unblock()
	helperBudgetAwait(t, done, "phase completion")
	p.hook = nil
	c.Close()

	runtime.GOMAXPROCS(2)
	d := helperBudgetCollector(t, 8)
	if got := d.parallelPool().ownedHelpers; got != 1 {
		t.Fatalf("two-processor helpers = %d, want 1", got)
	}
	runtime.GOMAXPROCS(8)
	if got := d.parallelPool().ownedHelpers; got != 7 {
		t.Fatalf("top-up helpers = %d, want 7", got)
	}
	d.Close()
	requireHelperBudget(t, 0)
}

func TestProcessHelperBudgetPoisonRetainsOwnership(t *testing.T) {
	helperBudgetProcessors(t, 4)
	requireHelperBudget(t, 0)
	c := helperBudgetCollector(t, 4)
	p := c.parallelPool()
	p.phase = parallelSweep
	p.hook = func(_ uint8, worker int) {
		if worker == 1 {
			panic("poison")
		}
	}
	p.selectParticipants(parallelMinObjects * 4)
	p.run()
	if p.poison == nil {
		t.Fatal("helper panic did not poison collector")
	}
	requireHelperBudget(t, 3)
	c.Close()
	requireHelperBudget(t, 0)
}

func TestProcessHelperBudgetConstructorFailureHasNoCharge(t *testing.T) {
	requireHelperBudget(t, 0)
	if _, err := NewCollector(Config{Workers: 9}, nil); err == nil {
		t.Fatal("invalid constructor succeeded")
	}
	requireHelperBudget(t, 0)
}

// A broken join can also block collector cleanup. Bound the entire fixture in
// a child process, not just channel receives. No sleeps or performance samples.
func helperBudgetSubprocess(t *testing.T) bool {
	t.Helper()
	if os.Getenv("WAGO_HELPER_BUDGET_CHILD") == t.Name() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.timeout=10s", "-test.count=1")
	cmd.Env = append(os.Environ(), "WAGO_HELPER_BUDGET_CHILD="+t.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bounded fixture: %v (context %v)\n%s", err, ctx.Err(), out)
	}
	return true
}
func helperBudgetAwait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}
