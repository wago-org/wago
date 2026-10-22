//go:build linux && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"testing"
)

func TestDependencyReleasingConsumerCannotRetire(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		name := "same-graph-reentrant"
		if foreign {
			name = "foreign-graph-barrier"
		}
		t.Run(name, func(t *testing.T) {
			g := dependencyTestGraph(t, 2, 1)
			consumerGraph := g
			if foreign {
				consumerGraph = dependencyTestGraph(t, 1, 1)
			}
			target := dependencyTestMember(t, g, false)
			consumer := dependencyTestMember(t, consumerGraph, true)
			r := dependencyTestAcquire(t, g, target)
			if typed, err := r.adopt(consumer, dependencyFunction); typed == foreign || err != nil {
				t.Fatalf("adoption typed=%v, error=%v", typed, err)
			}
			// Deliberately expose the existing physical-release claim flag. This
			// primitive fixture owns no native storage or teardown-complete hook.
			consumer.owner.lifeMu.Lock()
			consumer.owner.resourcesClosed = true
			consumer.owner.lifeMu.Unlock()
			assertBlocked := func() {
				dependencyTestCounts(t, g, target, 0, 1, 1, 0)
				if err := consumerGraph.unregister(consumer); !errors.Is(err, errDependencyState) {
					t.Fatalf("consumer retired during releasing receipt: %v", err)
				}
				if consumer.outgoing.Load() != 1 {
					t.Fatal("releasing ownership disappeared before physical call completion")
				}
				if !consumer.active.Load() || r.release() {
					t.Fatal("reentrant retirement/release consumed outstanding ownership")
				}
			}
			if !foreign {
				g.afterRelease = func(*Instance) { assertBlocked() }
				if !r.release() {
					t.Fatal("original release lost ownership")
				}
			} else {
				entered, resume := make(chan struct{}), make(chan struct{})
				done := make(chan bool, 1)
				g.afterRelease = func(*Instance) {
					close(entered)
					<-resume
				}
				go func() { done <- r.release() }()
				<-entered
				// Join even if a blocked-retirement assertion fails. Consumer/target
				// cleanup must not outlive the callback or releasing Go activation.
				joined := false
				defer func() {
					if !joined {
						close(resume)
						<-done
					}
				}()
				assertBlocked()
				close(resume)
				released := <-done
				joined = true
				if !released {
					t.Fatal("foreign release lost ownership")
				}
			}
			g.mu.Lock()
			record, _ := g.recordLocked(r.identity)
			complete := record != nil && record.state == dependencyReleased && record.target == nil && record.consumer == nil
			g.mu.Unlock()
			if !complete || consumer.outgoing.Load() != 0 {
				t.Fatal("completion did not clear record before ending outstanding source ownership")
			}
			if err := consumerGraph.unregister(consumer); err != nil {
				t.Fatalf("consumer retirement after completion: %v", err)
			}
			if consumer.active.Load() {
				t.Fatal("completed consumer remained registered")
			}
			dependencyTestCounts(t, g, target, 0, 0, 0, 0)
		})
	}
}
