//go:build linux && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// Primitive controls use actual retainer/receipt operations on unmapped owners.
// RV64 mapped adapter controls separately prove native lifetime and teardown.
func TestDependencyCounterClaimRollbackAndRetry(t *testing.T) {
	for _, state := range []string{"pending", "typed", "external"} {
		for _, panicBefore := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-panic%t", state, panicBefore), func(t *testing.T) {
				g := dependencyTestGraph(t, 2, 1)
				target := dependencyTestMember(t, g, false)
				consumerGraph := g
				if state == "external" {
					consumerGraph = dependencyTestGraph(t, 1, 1)
				}
				consumer := dependencyTestMember(t, consumerGraph, true)
				r := dependencyTestAcquire(t, g, target)
				if state != "pending" {
					if _, err := r.adopt(consumer, dependencyTableStorage); err != nil {
						t.Fatal(err)
					}
				}
				g.mu.Lock()
				before, _ := g.recordLocked(r.identity)
				record := *before
				free := append([]uint16(nil), g.free...)
				incoming, external, outgoing := target.incoming, target.external, consumer.outgoing.Load()
				g.mu.Unlock()
				callbacks, attempts := 0, 0
				g.afterRelease = func(*Instance) { callbacks++ }
				marker := new(int)
				value, panicked := cleanupTestPanic(func() {
					if r.releaseWithCounter(func() bool {
						attempts++
						if !g.mu.TryLock() {
							t.Fatal("counter claim ran under graph lock")
						}
						current, _ := g.recordLocked(r.identity)
						releasing := current != nil && current.state == dependencyReleasing
						g.mu.Unlock()
						if !releasing || dependencyTestRefs(target.owner) != 1 || r.release() || r.releaseWithCounter(func() bool { t.Error("alias reached counter"); return true }) {
							t.Fatal("reversible claim lost or duplicated its real physical root")
						}
						if _, err := r.adopt(consumer, dependencyTableStorage); !errors.Is(err, errDependencyState) {
							t.Fatal("releasing receipt admitted adoption", err)
						}
						if _, err := r.transfer(consumer, dependencyTableStorage); !errors.Is(err, errDependencyState) {
							t.Fatal("releasing receipt admitted transfer", err)
						}
						if err := g.unregister(target); !errors.Is(err, errDependencyState) {
							t.Fatal("claimed target retired", err)
						}
						if _, err := g.acquirePending(target, false); !errors.Is(err, errDependencyCapacity) {
							t.Fatal("claimed slot was reused before counter decision", err)
						}
						if panicBefore {
							panic(marker)
						}
						return false
					}) {
						t.Error("denied counter claim reported physical release")
					}
				})
				g.mu.Lock()
				after, _ := g.recordLocked(r.identity)
				exact := after != nil && *after == record && g.live == 1 && reflect.DeepEqual(g.free, free) && target.incoming == incoming && target.external == external && target.receipts == 1 && consumer.outgoing.Load() == outgoing
				g.mu.Unlock()
				if !exact || attempts != 1 || callbacks != 0 || dependencyTestRefs(target.owner) != 1 || panicked != panicBefore || panicBefore && value != marker {
					t.Fatal("counter denial changed exact ownership or first panic", exact, value, panicked)
				}
				if !r.releaseWithCounter(func() bool { attempts++; return true }) || attempts != 2 || callbacks != 1 || dependencyTestRefs(target.owner) != 0 || consumer.outgoing.Load() != 0 || g.live != 0 {
					t.Fatal("repaired counter retry failed to release once")
				}
				next, err := g.acquirePending(target, false)
				if err != nil || next.identity == r.identity {
					t.Fatal("completed slot did not change generation", err)
				}
				defer next.release()
				if r.releaseWithCounter(func() bool { t.Error("stale receipt reached counter"); return true }) {
					t.Fatal("old acquisition released recycled storage")
				}
				if _, err := r.adopt(consumer, dependencyTableStorage); !errors.Is(err, errDependencyIdentity) {
					t.Fatal("old identity adopted a new receipt", err)
				}
			})
		}
	}
}

func TestDependencyCounterClaimSurvivesRecordGrowth(t *testing.T) {
	g := dependencyTestGraph(t, 2, 32)
	target := dependencyTestMember(t, g, false)
	other := dependencyTestMember(t, g, false)
	r := dependencyTestAcquire(t, g, target)
	g.mu.Lock()
	before, _ := g.recordLocked(r.identity)
	want, capacity := *before, cap(g.records)
	g.mu.Unlock()
	var extra []dependencyReceipt
	defer func() {
		for _, receipt := range extra {
			receipt.release()
		}
	}()
	if r.releaseWithCounter(func() bool {
		for i := 0; i <= capacity; i++ {
			receipt, err := g.acquirePending(other, false)
			if err != nil {
				t.Fatal(err)
			}
			extra = append(extra, receipt)
		}
		return false
	}) {
		t.Fatal("denied counter released after record relocation")
	}
	g.mu.Lock()
	after, _ := g.recordLocked(r.identity)
	exact := cap(g.records) > capacity && after != nil && *after == want
	g.mu.Unlock()
	if !exact || dependencyTestRefs(target.owner) != 1 || !r.releaseWithCounter(func() bool { return true }) {
		t.Fatal("rollback wrote a stale record pointer")
	}
}

func TestDependencyCounterClaimBlocksConcurrentAliases(t *testing.T) {
	g := dependencyTestGraph(t, 2, 1)
	target := dependencyTestMember(t, g, false)
	consumer := dependencyTestMember(t, g, true)
	r := dependencyTestAcquire(t, g, target)
	if _, err := r.adopt(consumer, dependencyMemory); err != nil {
		t.Fatal(err)
	}
	entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan bool, 1)
	go func() { done <- r.releaseWithCounter(func() bool { close(entered); <-resume; return false }) }()
	joined, resumed := false, false
	defer func() {
		if !resumed {
			close(resume)
		}
		if !joined {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("counter rollback cleanup join timed out")
			}
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("counter claim did not start")
	}
	if r.release() || r.releaseWithCounter(func() bool { t.Error("concurrent alias consumed counter"); return true }) || dependencyTestRefs(target.owner) != 1 || consumer.outgoing.Load() != 1 {
		t.Fatal("concurrent counter claim lost outstanding ownership")
	}
	if err := g.unregister(consumer); !errors.Is(err, errDependencyState) {
		t.Fatal("consumer retired before counter decision", err)
	}
	if _, err := r.transfer(consumer, dependencyGlobalCell); !errors.Is(err, errDependencyState) {
		t.Fatal("concurrent transfer changed reversible claim", err)
	}
	close(resume)
	resumed = true
	select {
	case released := <-done:
		joined = true
		if released {
			t.Fatal("failed counter reported release")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("counter rollback did not finish")
	}
	dependencyTestCounts(t, g, target, 1, 1, 0, 1)
	if !r.releaseWithCounter(func() bool { return true }) {
		t.Fatal("retry lost same acquisition")
	}
}

func TestDependencyCounterCommitPhysicalPanicCompletes(t *testing.T) {
	g := dependencyTestGraph(t, 2, 1)
	target := dependencyTestMember(t, g, false)
	consumer := dependencyTestMember(t, g, true)
	r := dependencyTestAcquire(t, g, target)
	if _, err := r.adopt(consumer, dependencyMemory); err != nil {
		t.Fatal(err)
	}
	marker, committed, callbacks := new(int), false, 0
	g.afterRelease = func(*Instance) {
		callbacks++
		if !committed || dependencyTestRefs(target.owner) != 0 || consumer.outgoing.Load() != 1 || r.release() {
			t.Error("callback crossed counter commit incorrectly")
		}
		panic(marker)
	}
	value, panicked := cleanupTestPanic(func() { r.releaseWithCounter(func() bool { committed = true; return true }) })
	if !panicked || value != marker || callbacks != 1 || g.live != 0 || consumer.outgoing.Load() != 0 || r.release() {
		t.Fatal("post-commit physical panic rolled back or duplicated the acquisition", value, panicked)
	}
	dependencyTestCounts(t, g, target, 0, 0, 0, 0)
}
