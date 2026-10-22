//go:build linux && !tinygo && !wago_target_tinygo

package wago

import "testing"

func TestRetainedResourceCleanupTableClaimsBeforeCallbacks(t *testing.T) {
	table := &Table{retained: make(map[*Instance]*retainedInstanceRoot)}
	var counts [3]int
	var first any
	var producers []*Instance
	for i := range counts {
		index, marker := i, new(int)
		producer := cleanupTestRootedProducer(t, func() {
			if !table.mu.TryLock() {
				panic("retained table callback under Table.mu")
			}
			claimed := table.closed && table.retained == nil
			table.mu.Unlock()
			if !claimed {
				panic("retained table batch still published")
			}
			table.releaseRetainedInstances() // No second map claim under reentry.
			counts[index]++
			if index < 2 {
				if first == nil {
					first = marker // Actual map traversal order is unspecified.
				}
				panic(marker)
			}
		})
		table.retained[producer] = &retainedInstanceRoot{precise: true}
		producers = append(producers, producer)
	}
	value, panicked := cleanupTestPanic(table.releaseRetainedInstances)
	if !panicked || value != first || counts != [3]int{1, 1, 1} {
		t.Fatalf("panic=%v/%v first=%v counts=%v", value, panicked, first, counts)
	}
	for _, producer := range producers {
		cleanupTestConsumedProducer(t, producer)
	}
	table.releaseRetainedInstances()
	if counts != [3]int{1, 1, 1} {
		t.Fatal("repeated table close decremented consumed map roots")
	}
}

func TestRetainedResourceCleanupGlobalExactOwnerAndReentry(t *testing.T) {
	container := &Instance{}
	global := &Global{cell: make([]byte, 8), owner: &globalOwner{instance: container, retained: make(map[*Instance]*retainedInstanceRoot)}}
	var counts [3]int
	var first any
	var producers []*Instance
	for i := range counts {
		index, marker := i, new(int)
		producer := cleanupTestRootedProducer(t, func() {
			if !global.owner.mu.TryLock() {
				panic("retained global callback under globalOwner.mu")
			}
			claimed := global.owner.closed && global.owner.retained == nil && global.cell == nil
			global.owner.mu.Unlock()
			if !claimed {
				panic("retained global batch or cell still published")
			}
			global.instanceOwnerClosed(container)
			counts[index]++
			if index < 2 {
				if first == nil {
					first = marker
				}
				panic(marker)
			}
		})
		global.owner.retained[producer] = &retainedInstanceRoot{precise: true}
		producers = append(producers, producer)
	}
	global.instanceOwnerClosed(&Instance{}) // Only the exact owner may claim.
	if global.owner.closed || len(global.owner.retained) != 3 || len(global.cell) != 8 || counts != [3]int{} {
		t.Fatal("unrelated instance claimed the retained global batch")
	}
	value, panicked := cleanupTestPanic(func() { global.instanceOwnerClosed(container) })
	if !panicked || value != first || counts != [3]int{1, 1, 1} {
		t.Fatalf("panic=%v/%v first=%v counts=%v", value, panicked, first, counts)
	}
	for _, producer := range producers {
		cleanupTestConsumedProducer(t, producer)
	}
	global.instanceOwnerClosed(container)
	if counts != [3]int{1, 1, 1} {
		t.Fatal("repeated global close decremented consumed map roots")
	}
}

func TestRetainedResourceCleanupSliceExactUnitsAndRecursiveDrain(t *testing.T) {
	first := new(int)
	var roots []*Instance
	var counts [2]int
	one := cleanupTestRootedProducer(t, func() {
		counts[0]++
		for _, root := range roots[:3] {
			if root != nil {
				panic("current producer units still published during callback")
			}
		}
		releaseRetainedResourceRoots(roots) // Consume the later owner's unit.
		panic(first)
	})
	if !one.retainResourceRootForFinalization() {
		t.Fatal("second independent producer root")
	}
	two := cleanupTestRootedProducer(t, func() { counts[1]++ })
	roots = []*Instance{one, nil, one, two}
	value, panicked := cleanupTestPanic(func() { releaseRetainedResourceRoots(roots) })
	if !panicked || value != first || counts != [2]int{1, 1} {
		t.Fatalf("panic=%v/%v counts=%v", value, panicked, counts)
	}
	for _, root := range roots {
		if root != nil {
			t.Fatal("consumed slice still retains a producer")
		}
	}
	cleanupTestConsumedProducer(t, one)
	cleanupTestConsumedProducer(t, two)
	releaseRetainedResourceRoots(roots)
	if counts != [2]int{1, 1} {
		t.Fatal("recursive or repeated drain consumed a root twice")
	}
}

func TestRetainedResourceCleanupWarmExactSliceNoAllocation(t *testing.T) {
	// These live Go-only owners never enter finalization and own no native
	// storage. Warm the RV shadow node before measuring actual root operations.
	owner := &Instance{}
	if !owner.retainResourceRoot() {
		t.Fatal("warm owner root")
	}
	owner.releaseResourceRoot()
	roots := make([]*Instance, 2)
	allocations := testing.AllocsPerRun(100, func() {
		if !owner.retainResourceRoot() || !owner.retainResourceRoot() {
			panic("live owner root acquisition")
		}
		roots[0], roots[1] = owner, owner
		releaseRetainedResourceRoots(roots)
	})
	if allocations != 0 || owner.hasResourceRoots() || roots[0] != nil || roots[1] != nil {
		t.Fatalf("allocations=%v roots=%v batch=%v", allocations, owner.hasResourceRoots(), roots)
	}
}

func TestRetainedResourceCleanupTablePruneDrainsClaimedRoots(t *testing.T) {
	table := &Table{desc: make([]byte, 8), retained: make(map[*Instance]*retainedInstanceRoot)}
	var visited [3]int
	var first any
	var producers []*Instance
	for i := range visited {
		producer := cleanupTestRootedProducer(t, func() {
			if !table.mu.TryLock() {
				panic("prune callback under table lock")
			}
			remaining := len(table.retained)
			table.mu.Unlock()
			if remaining != 0 {
				panic("prune left claimed roots published")
			}
			table.pruneRetainedInstances()
			visited[i]++
			if i != 2 {
				marker := new(int)
				if first == nil {
					first = marker
				}
				panic(marker)
			}
		})
		table.retained[producer] = &retainedInstanceRoot{precise: true}
		producers = append(producers, producer)
	}
	value, panicked := cleanupTestPanic(table.pruneRetainedInstances)
	if !panicked || value != first || visited != [3]int{1, 1, 1} {
		t.Fatal("prune abandoned claimed units", value, panicked, visited)
	}
	for _, producer := range producers {
		cleanupTestConsumedProducer(t, producer)
	}
	table.pruneRetainedInstances()
	if visited != [3]int{1, 1, 1} {
		t.Fatal("prune repeated consumed units")
	}
}
