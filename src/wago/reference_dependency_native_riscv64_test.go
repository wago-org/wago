//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
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
	ownership := rv64ExistingFunctionDependencyOwnership(consumer)
	if ownership.functions.n != 1 || len(ownership.functions.extra) != 0 {
		t.Fatal("warm exact receipt rows accumulated history", ownership.functions.n, len(ownership.functions.extra))
	}
}

func TestRV64FunctionDependenciesDeniedReceiptRetainsRetryRow(t *testing.T) {
	g, err := newRV64FunctionDependencyGraph(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	owners := rv64FunctionDependencyUnitOwners(t, 2)
	target, consumer := owners[0], owners[1]
	consumer.beginConstruction(nil)
	defer consumer.endConstruction()
	var batch functionImportAttachments
	if err := rv64AcquireFunctionDependency(&batch, target, g); err != nil {
		t.Fatal(err)
	}
	railshotInstallFunctionDependencies(consumer, &batch)
	if err := railshotAdoptFunctionDependencies(consumer, true); err != nil {
		t.Fatal(err)
	}
	ownership := rv64ExistingFunctionDependencyOwnership(consumer)
	row := &ownership.functions.inline[0]
	receipt := row.root.receipt
	t.Cleanup(func() {
		g.mu.Lock()
		if record, _ := g.recordLocked(receipt.identity); record != nil && record.state == dependencyReleasing {
			record.state = dependencyTyped
		}
		g.mu.Unlock()
		ownership.functions.drain(ownership)
	})
	g.mu.Lock()
	record, _ := g.recordLocked(receipt.identity)
	if record == nil || record.state != dependencyTyped {
		g.mu.Unlock()
		t.Fatal("missing typed function receipt")
	}
	// Private exact-state denial seam. This deliberately omits the real typed to
	// external accounting transition; it only makes the receipt claim reject.
	record.state = dependencyReleasing
	g.mu.Unlock()
	if _, panicked := cleanupTestPanic(func() { ownership.functions.drain(ownership) }); !panicked {
		t.Fatal("denied exact receipt release did not report failure")
	}
	ownership.functions.mu.Lock()
	retained := row.state == rv64FunctionDependencyAttached && row.root.receipt.identity == receipt.identity
	ownership.functions.mu.Unlock()
	if !retained || dependencyTestRefs(target) != 1 || g.live != 1 || ownership.member.outgoing.Load() != 1 {
		t.Fatal("denied function receipt lost its stable retry owner")
	}
	ownership.physicalCleanupFinished.Store(true)
	rv64MaybeCompleteFunctionDependencies(ownership)
	if ownership.cleanupCompleted.Load() {
		t.Fatal("denied function receipt permitted cleanup completion")
	}
	g.mu.Lock()
	record, _ = g.recordLocked(receipt.identity)
	record.state = dependencyTyped
	g.mu.Unlock()
	consumer.lifeMu.Lock()
	consumer.resourcesClosed = true
	consumer.lifeMu.Unlock()
	ownership.functions.drain(ownership)
	if !ownership.functions.complete() || dependencyTestRefs(target) != 0 || g.live != 0 || ownership.member.outgoing.Load() != 0 {
		t.Fatal("restored exact function receipt did not complete")
	}
	if !ownership.cleanupCompleted.Load() {
		t.Fatal("completed function rows did not unblock cleanup completion")
	}
	if ownership.member.active.Load() {
		t.Fatal("late function receipt retry did not retire its exact member")
	}
}

func TestRV64DependencyComponentInternalDriverPhysicalOrder(t *testing.T) {
	g, err := newRV64FunctionDependencyGraph(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	owners := rv64FunctionDependencyUnitOwners(t, 2)
	a, b := owners[0], owners[1]
	a.c, b.c = &Compiled{}, &Compiled{}
	aOwnership := rv64FunctionDependencyOwnershipFor(a)
	bOwnership := rv64FunctionDependencyOwnershipFor(b)
	if err := g.register(&aOwnership.member); err != nil {
		t.Fatal(err)
	}
	if err := g.register(&bOwnership.member); err != nil {
		t.Fatal(err)
	}
	dependencyTestInvocationLease(&aOwnership.member, &bOwnership.member)
	aToB := dependencyTestAcquire(t, g, &bOwnership.member)
	bToA := dependencyTestAcquire(t, g, &aOwnership.member)
	if typed, err := aToB.adopt(&aOwnership.member, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	if typed, err := bToA.adopt(&bOwnership.member, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	aOwnership.functions.push(functionDependencyRoot{producer: b, receipt: aToB})
	bOwnership.functions.push(functionDependencyRoot{producer: a, receipt: bToA})
	aOwnership.managed, bOwnership.managed = true, true
	t.Cleanup(func() {
		var cleanup resourceCleanup
		cleanup.run(func() { aOwnership.functions.drain(aOwnership) })
		cleanup.run(func() { bOwnership.functions.drain(bOwnership) })
		cleanup.finish()
	})
	dependencyTestClosedForDiscovery(t, &aOwnership.member, &bOwnership.member)
	firstPanic := new(int)
	var visited []int
	persistedBeforeCallback := false
	a.referenceLifetime().afterPhysicalRelease(func() {
		persistedBeforeCallback = aOwnership.componentRelease.Load() != nil && aOwnership.componentRelease.Load() == bOwnership.componentRelease.Load()
		visited = append(visited, 0)
		panic(firstPanic)
	})
	b.referenceLifetime().afterPhysicalRelease(func() { visited = append(visited, 1) })
	type driverResult struct {
		value    any
		panicked bool
	}
	halfPublished := make(chan struct{})
	resumePublication := make(chan struct{})
	var resumeOnce sync.Once
	resume := func() { resumeOnce.Do(func() { close(resumePublication) }) }
	driverDone := make(chan driverResult, 1)
	joined := false
	defer func() {
		resume()
		if !joined {
			select {
			case <-driverDone:
			case <-time.After(5 * time.Second):
				t.Error("timed out joining component driver during test cleanup")
			}
		}
	}()
	go func() {
		var result driverResult
		result.value, result.panicked = cleanupTestPanic(func() {
			rv64CollectClosedDependencyComponentWithPublicationHook(&aOwnership.member, 2, 2, func() {
				close(halfPublished)
				<-resumePublication
			})
		})
		driverDone <- result
	}()
	select {
	case <-halfPublished:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out pausing component coordinator publication")
	}
	firstOwnership, firstMember := aOwnership, &aOwnership.member
	secondOwnership := bOwnership
	if firstOwnership.componentRelease.Load() == nil {
		firstOwnership, firstMember, secondOwnership = bOwnership, &bOwnership.member, aOwnership
	}
	coordinator := firstOwnership.componentRelease.Load()
	if coordinator == nil || coordinator.published.Load() || secondOwnership.componentRelease.Load() != nil {
		t.Fatal("half-published coordinator did not retain its publication gate")
	}
	if rv64CollectClosedDependencyComponent(firstMember, 2, 2) || len(visited) != 0 || coordinator.physicalJournalsCompleteForTest() {
		t.Fatal("second worker drove a half-published component")
	}
	resume()
	var result driverResult
	select {
	case result = <-driverDone:
		joined = true
	case <-time.After(5 * time.Second):
		t.Fatal("timed out joining component driver")
	}
	if !result.panicked || result.value != firstPanic || !persistedBeforeCallback || !reflect.DeepEqual(visited, []int{0, 1}) {
		t.Fatalf("component driver panic=%v/%t callbacks=%v", result.value, result.panicked, visited)
	}
	if g.live != 0 || dependencyTestRefs(a) != 0 || dependencyTestRefs(b) != 0 || !a.resourcesClosed || !b.resourcesClosed || !aOwnership.cleanupCompleted.Load() || !bOwnership.cleanupCompleted.Load() || aOwnership.member.active.Load() || bOwnership.member.active.Load() || aOwnership.componentRelease.Load() != nil || bOwnership.componentRelease.Load() != nil {
		t.Fatal("component driver left physical roots, retry ownership, or registered members")
	}
}

func TestRV64DependencyComponentRejectsForeignMemberBeforePhysicalClaim(t *testing.T) {
	g, err := newRV64FunctionDependencyGraph(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	a := rv64FunctionDependencyUnitOwners(t, 1)[0]
	aOwnership := rv64FunctionDependencyOwnershipFor(a)
	b := &Instance{}
	bMember := &dependencyMember{owner: b}
	if err := g.register(&aOwnership.member); err != nil {
		t.Fatal(err)
	}
	if err := g.register(bMember); err != nil {
		t.Fatal(err)
	}
	dependencyTestInvocationLease(&aOwnership.member, bMember)
	aToB := dependencyTestAcquire(t, g, bMember)
	bToA := dependencyTestAcquire(t, g, &aOwnership.member)
	if typed, err := aToB.adopt(&aOwnership.member, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	if typed, err := bToA.adopt(bMember, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	defer func() {
		var cleanup resourceCleanup
		cleanup.run(func() { aToB.release() })
		cleanup.run(func() { bToA.release() })
		cleanup.finish()
	}()
	dependencyTestClosedForDiscovery(t, &aOwnership.member, bMember)
	aFinalizers := &instanceFinalizers{physical: func() {}}
	bFinalizers := &instanceFinalizers{physical: func() {}}
	a.finalizers = aFinalizers
	b.finalizers = bFinalizers
	aRefs, bRefs := dependencyTestRefs(a), dependencyTestRefs(b)
	if rv64CollectClosedDependencyComponent(&aOwnership.member, 2, 2) {
		t.Fatal("mixed RV/foreign component reached physical claim")
	}
	if a.resourcesClosed || b.resourcesClosed || a.finalizers != aFinalizers || b.finalizers != bFinalizers || dependencyTestRefs(a) != aRefs || dependencyTestRefs(b) != bRefs || aOwnership.member.collecting.Load() || bMember.collecting.Load() || aOwnership.componentRelease.Load() != nil {
		t.Fatal("rejected mixed component mutated lifecycle, roots, finalizers, or retry ownership")
	}
}

func TestRV64DependencyComponentRetryDoesNotRepeatMappedTeardown(t *testing.T) {
	c := rv64PublicStartCompile(t, rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), false)
	a, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Instantiate(c)
	if err != nil {
		a.Close()
		t.Fatal(err)
	}
	sibling, err := Instantiate(c)
	if err != nil {
		a.Close()
		b.Close()
		t.Fatal(err)
	}
	defer a.Close()
	defer b.Close()
	defer sibling.Close()
	base, refs := c.codeCache.base, c.codeCache.refs
	if base == 0 || refs != 3 {
		t.Fatal("mapped shared-code fixture", base, refs)
	}

	g, err := newRV64FunctionDependencyGraph(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	aOwnership := rv64FunctionDependencyOwnershipFor(a)
	bOwnership := rv64FunctionDependencyOwnershipFor(b)
	if err := g.register(&aOwnership.member); err != nil {
		t.Fatal(err)
	}
	if err := g.register(&bOwnership.member); err != nil {
		t.Fatal(err)
	}
	aToB := dependencyTestAcquire(t, g, &bOwnership.member)
	bToA := dependencyTestAcquire(t, g, &aOwnership.member)
	if err := a.beginInvocation(); err != nil {
		t.Fatal(err)
	}
	aLease := true
	defer func() {
		if aLease {
			a.endInvocation()
		}
	}()
	if err := b.beginInvocation(); err != nil {
		t.Fatal(err)
	}
	bLease := true
	defer func() {
		if bLease {
			b.endInvocation()
		}
	}()
	if typed, err := aToB.adopt(&aOwnership.member, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	if typed, err := bToA.adopt(&bOwnership.member, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	aOwnership.functions.push(functionDependencyRoot{producer: b, receipt: aToB})
	bOwnership.functions.push(functionDependencyRoot{producer: a, receipt: bToA})
	aOwnership.managed, bOwnership.managed = true, true
	a.endInvocation()
	aLease = false
	b.endInvocation()
	bLease = false
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if a.resourcesClosed || b.resourcesClosed || dependencyTestRefs(a) != 1 || dependencyTestRefs(b) != 1 {
		t.Fatal("cycle did not retain mapped instances through logical close")
	}

	setDenied := func(denied bool) {
		g.mu.Lock()
		record, _ := g.recordLocked(aToB.identity)
		if record == nil {
			g.mu.Unlock()
			t.Fatal("lost exact denied receipt")
		}
		if denied {
			record.state = dependencyReleasing
		} else {
			record.state = dependencyTyped
		}
		g.mu.Unlock()
	}
	denied := false
	defer func() {
		if denied {
			setDenied(false)
		}
		cleanupTestPanic(func() { rv64CollectClosedDependencyComponent(&aOwnership.member, 2, 2) })
	}()
	aCallbacks, bCallbacks := 0, 0
	reentrantCompleted := false
	a.referenceLifetime().afterPhysicalRelease(func() {
		aCallbacks++
		reentrantCompleted = rv64CollectClosedDependencyComponent(&aOwnership.member, 2, 2)
	})
	b.referenceLifetime().afterPhysicalRelease(func() { bCallbacks++ })
	value, panicked := cleanupTestPanic(func() {
		rv64CollectClosedDependencyComponentWithPublicationHook(&aOwnership.member, 2, 2, func() {
			setDenied(true)
			denied = true
		})
	})
	if !panicked || value == nil {
		t.Fatal("denied exact holder did not preserve retry boundary", value, panicked)
	}
	coordinator := aOwnership.componentRelease.Load()
	if coordinator == nil || coordinator != bOwnership.componentRelease.Load() || !coordinator.published.Load() {
		t.Fatal("denied mapped teardown lost its persistent coordinator")
	}
	aIndex, bIndex := -1, -1
	for i, member := range coordinator.release.claim.members {
		switch member.owner {
		case a:
			aIndex = i
		case b:
			bIndex = i
		}
	}
	if aIndex < 0 || bIndex < 0 || !coordinator.teardownDone[aIndex].Load() || !coordinator.teardownDone[bIndex].Load() || coordinator.release.physicalJournals[aIndex].Load() != 0 || coordinator.release.physicalJournals[bIndex].Load() != 2 || aCallbacks != 0 || bCallbacks != 1 {
		t.Fatal("denied mapped teardown did not retain only the incomplete holder journal", aIndex, bIndex, aCallbacks, bCallbacks)
	}
	if c.codeCache.refs != 1 || c.codeCache.base != base {
		t.Fatal("first mapped teardown did not release exactly two code references", c.codeCache.refs, c.codeCache.base, base)
	}

	setDenied(false)
	denied = false
	if !rv64CollectClosedDependencyComponent(&aOwnership.member, 2, 2) {
		t.Fatal("repaired exact holder did not complete component retry")
	}
	if reentrantCompleted || aCallbacks != 1 || bCallbacks != 1 || c.codeCache.refs != 1 || c.codeCache.base != base {
		t.Fatal("retry repeated mapped teardown or completed reentrantly", reentrantCompleted, aCallbacks, bCallbacks, c.codeCache.refs, c.codeCache.base)
	}
	got, err := sibling.Invoke("identity", 0xdeadbeef80000001)
	if err != nil || !reflect.DeepEqual(got, []uint64{0x80000001}) {
		t.Fatal("surviving shared-code sibling after retry", got, err)
	}
	if aOwnership.componentRelease.Load() != nil || bOwnership.componentRelease.Load() != nil || !aOwnership.cleanupCompleted.Load() || !bOwnership.cleanupCompleted.Load() || aOwnership.member.active.Load() || bOwnership.member.active.Load() || g.live != 0 {
		t.Fatal("mapped retry left coordinator, member, or receipt state")
	}
}

func (coordinator *rv64DependencyComponentCoordinator) physicalJournalsCompleteForTest() bool {
	if coordinator == nil || coordinator.release == nil {
		return false
	}
	for i := range coordinator.release.physicalJournals {
		if coordinator.release.physicalJournals[i].Load() != 0 {
			return true
		}
	}
	return false
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
