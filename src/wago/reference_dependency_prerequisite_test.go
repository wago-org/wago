//go:build linux && !tinygo && !wago_target_tinygo

package wago

import "testing"

func TestDependencyReleasePrerequisiteExactlyOnceAndPanic(t *testing.T) {
	for _, adopted := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "typed"}[adopted], func(t *testing.T) {
			g := dependencyTestGraph(t, 2, 1)
			target := dependencyTestMember(t, g, false)
			consumer := dependencyTestMember(t, g, true)
			receipt := dependencyTestAcquire(t, g, target)
			if adopted {
				if _, err := receipt.adopt(consumer, dependencyMemory); err != nil {
					t.Fatal(err)
				}
			}
			first, second := new(int), new(int)
			before, after := 0, 0
			g.afterRelease = func(owner *Instance) {
				after++
				if owner != target.owner || dependencyTestRefs(owner) != 0 {
					t.Error("prerequisite panic skipped the real producer decrement")
				}
				panic(second)
			}
			value, panicked := cleanupTestPanic(func() {
				receipt.releaseWithCleanup(func() {
					before++
					g.mu.Lock()
					record, _ := g.recordLocked(receipt.identity)
					claimed := record != nil && record.state == dependencyReleasing
					g.mu.Unlock()
					if !claimed || dependencyTestRefs(target.owner) != 1 {
						t.Error("counter prerequisite ran before graph claim or after physical release")
					}
					panic(first)
				})
			})
			if !panicked || value != first || before != 1 || after != 1 || g.live != 0 || target.receipts != 0 || consumer.outgoing.Load() != 0 || dependencyTestRefs(target.owner) != 0 {
				t.Fatal("first prerequisite panic or completed accounting lost", panicked, value, before, after)
			}
			if receipt.releaseWithCleanup(func() { before++ }) || before != 1 {
				t.Fatal("alias release ran the counter prerequisite again")
			}
		})
	}
}
