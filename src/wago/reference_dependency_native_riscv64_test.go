//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"testing"
	"time"
)

// Isolated graph tests use real root/lease/drain operations on unmapped owners;
// the public tests separately qualify real mapped construction and retirement.
func rv64FunctionDependencyUnitOwners(t *testing.T, n int) []*Instance {
	owners := make([]*Instance, n)
	for i := range owners {
		owners[i] = &Instance{}
	}
	t.Cleanup(func() {
		for _, in := range owners {
			if dependencyTestRefs(in) != 0 {
				t.Error("unit owner retained a physical root")
			}
			in.lifeMu.Lock()
			in.resourcesClosed = true
			in.lifeMu.Unlock()
			railshotFunctionDependencyCleanupCompleted(in)
		}
	})
	return owners
}

func rv64FunctionDependencyPhysicalShadow(t *testing.T, in *Instance, want int) {
	t.Helper()
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	state := in.importState.Load()
	if in.resourceRefs != want || state == nil || state.rootAccounting.ledger == nil || state.rootAccounting.ledger.external != want {
		t.Fatal("physical shadow must stay entirely anonymous", in.resourceRefs, want)
	}
}

func TestRV64FunctionDependenciesCapacityAndDrain(t *testing.T) {
	for _, nodeCap := range []int{1, 3} {
		t.Run(map[int]string{1: "node-cap", 3: "receipt-cap"}[nodeCap], func(t *testing.T) {
			g, err := newRV64FunctionDependencyGraph(nodeCap, 1)
			if err != nil {
				t.Fatal(err)
			}
			owners := rv64FunctionDependencyUnitOwners(t, 3)
			a, b, consumer := owners[0], owners[1], owners[2]
			consumer.beginConstruction(nil)
			defer consumer.endConstruction()
			var batch functionImportAttachments
			for _, producer := range []*Instance{a, b} {
				if err := rv64AcquireFunctionDependency(&batch, producer, g); err != nil {
					t.Fatal(err)
				}
			}
			if batch.dependencies.roots.n != 2 || batch.dependencies.roots.inline[0].receipt.graph != g || batch.dependencies.roots.inline[1].receipt.graph != nil {
				t.Fatal("typed/pending plus identified anonymous fallback")
			}
			railshotInstallFunctionDependencies(consumer, &batch)
			defer railshotDrainInstanceFunctionDependencies(consumer)
			if railshotHasBuilderFunctionDependencies(&batch) || batch.set.n != 0 {
				t.Fatal("builder retained terminal ownership")
			}
			if err := railshotAdoptFunctionDependencies(consumer, false); err == nil {
				t.Fatal("adopted under native construction lease")
			}
			if err := railshotAdoptFunctionDependencies(consumer, true); err != nil {
				t.Fatal(err)
			}
			am := &rv64ExistingFunctionDependencyOwnership(a).member
			incoming := 0
			if nodeCap == 3 {
				incoming = 1
			}
			dependencyTestCounts(t, g, am, 1, 1, 1-incoming, incoming)
			for _, producer := range []*Instance{a, b} {
				rv64FunctionDependencyPhysicalShadow(t, producer, 1)
			}
			if !railshotDrainInstanceFunctionDependencies(consumer) || !railshotDrainInstanceFunctionDependencies(consumer) {
				t.Fatal("managed marker lost after drain")
			}
			batch.detachAll()
			for _, producer := range []*Instance{a, b} {
				rv64FunctionDependencyPhysicalShadow(t, producer, 0)
			}
			if g.live != 0 {
				t.Fatal("receipt leak")
			}
		})
	}
}

func TestRV64FunctionDependenciesPartialAdoptionCloseGate(t *testing.T) {
	g, err := newRV64FunctionDependencyGraph(3, 2)
	if err != nil {
		t.Fatal(err)
	}
	owners := rv64FunctionDependencyUnitOwners(t, 3)
	a, b, consumer := owners[0], owners[1], owners[2]
	consumer.beginConstruction(nil)
	defer consumer.endConstruction()
	var batch functionImportAttachments
	for _, p := range []*Instance{a, b} {
		if err := rv64AcquireFunctionDependency(&batch, p, g); err != nil {
			t.Fatal(err)
		}
	}
	railshotInstallFunctionDependencies(consumer, &batch)
	defer railshotDrainInstanceFunctionDependencies(consumer)
	// Block the second endpoint, allowing the first real adoption to commit.
	b.lifeMu.Lock()
	done := make(chan error, 1)
	go func() { done <- railshotAdoptFunctionDependencies(consumer, true) }()
	cm := &rv64ExistingFunctionDependencyOwnership(consumer).member
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for cm.outgoing.Load() != 1 {
		select {
		case <-ticker.C:
		case <-deadline.C:
			b.lifeMu.Unlock()
			<-done
			t.Fatal("first adoption never committed")
		}
	}
	consumer.closeInvocationEntry()
	b.lifeMu.Unlock()
	if err := <-done; !errors.Is(err, errDependencyClosed) {
		t.Fatal("late closed-gate adoption", err)
	}
	dependencyTestCounts(t, g, &rv64ExistingFunctionDependencyOwnership(a).member, 1, 1, 0, 1)
	dependencyTestCounts(t, g, &rv64ExistingFunctionDependencyOwnership(b).member, 1, 1, 1, 0)
	railshotDrainInstanceFunctionDependencies(consumer)
	for _, p := range []*Instance{a, b} {
		rv64FunctionDependencyPhysicalShadow(t, p, 0)
	}
}

func TestRV64FunctionDependenciesWarmReuse(t *testing.T) {
	g, err := newRV64FunctionDependencyGraph(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	owners := rv64FunctionDependencyUnitOwners(t, 2)
	target, consumer := owners[0], owners[1]
	consumer.beginConstruction(nil)
	defer consumer.endConstruction()
	failed := false
	allocs := testing.AllocsPerRun(100, func() {
		var batch functionImportAttachments
		if rv64AcquireFunctionDependency(&batch, target, g) != nil {
			failed = true
			return
		}
		railshotInstallFunctionDependencies(consumer, &batch)
		if railshotAdoptFunctionDependencies(consumer, true) != nil {
			failed = true
		}
		railshotDrainInstanceFunctionDependencies(consumer)
	})
	if failed || allocs != 0 || g.live != 0 || dependencyTestRefs(target) != 0 {
		t.Fatal("warm exact receipt reuse", failed, allocs, g.live)
	}
}

func TestRV64FunctionDependenciesAuthorityFailureIsNotCapacity(t *testing.T) {
	g, _ := newRV64FunctionDependencyGraph(1, 1)
	other, _ := newRV64FunctionDependencyGraph(1, 1)
	owner := rv64FunctionDependencyUnitOwners(t, 1)[0]
	if _, err := rv64RegisteredFunctionDependencyMember(g, owner); err != nil {
		t.Fatal(err)
	}
	var batch functionImportAttachments
	if err := rv64AcquireFunctionDependency(&batch, owner, other); !errors.Is(err, errDependencyMember) || dependencyTestRefs(owner) != 0 || batch.dependencies.roots.n != 0 {
		t.Fatal("foreign authority became anonymous fallback", err)
	}
	owner.closeInvocationEntry()
	if err := rv64AcquireFunctionDependency(&batch, owner, g); !errors.Is(err, errDependencyClosed) || dependencyTestRefs(owner) != 0 {
		t.Fatal("closed owner became anonymous fallback", err)
	}
}

func TestRV64FunctionDependenciesBuilderRollbackOverflowAndPanic(t *testing.T) {
	const count = 9 // both inline overflow and the ordered-set index are exercised
	g, err := newRV64FunctionDependencyGraph(count, count)
	if err != nil {
		t.Fatal(err)
	}
	owners := rv64FunctionDependencyUnitOwners(t, count)
	var batch functionImportAttachments
	callbacks := make([]int, count)
	marker := new(int)
	for i, producer := range owners {
		if err := rv64AcquireFunctionDependency(&batch, producer, g); err != nil {
			t.Fatal(err)
		}
		batch.set.push(producer)
		producer.referenceLifetime().afterPhysicalRelease(func() {
			callbacks[i]++
			if i == 0 {
				panic(marker)
			}
		})
		if err := producer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if batch.dependencies.roots.n != count || batch.dependencies.roots.index == nil || batch.set.index == nil {
		t.Fatal("fixture did not acquire overflow builder receipt records")
	}
	var caught any
	func() {
		defer func() { caught = recover() }()
		batch.detachAll()
	}()
	if caught != marker || batch.dependencies.roots.n != 0 || batch.set.n != 0 {
		t.Fatal("builder rollback lost panic or retained ownership", caught)
	}
	batch.detachAll()
	for i, producer := range owners {
		state := rv64ExistingFunctionDependencyOwnership(producer)
		if callbacks[i] != 1 || dependencyTestRefs(producer) != 0 || !state.cleanupCompleted.Load() || state.member.active.Load() {
			t.Fatal("overflow builder rollback did not retire each acquisition exactly once", i)
		}
	}
	if g.live != 0 || len(g.owners) != 0 {
		t.Fatal("builder rollback registry leak")
	}
}
