//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"reflect"
	"testing"
)

func TestRetainedResourceCleanupRVFrozenAliasesOrderAndPanic(t *testing.T) {
	first, later := new(int), new(int)
	var entries []importGlobalReference
	var order []int
	var owners [3]*Instance
	for i := range owners {
		index := i
		owners[i] = cleanupTestRootedProducer(t, func() {
			order = append(order, index)
			for _, entry := range entries {
				if entry.owner == owners[index] {
					panic("alias retained current frozen root during callback")
				}
			}
			if index == 0 {
				panic(first)
			}
			if index == 1 {
				panic(later)
			}
		})
	}
	entries = []importGlobalReference{
		{global: &Global{}, owner: owners[0], descriptor: 11},
		{global: &Global{}, owner: owners[1], descriptor: 22},
		{global: &Global{}, owner: owners[0], descriptor: 11},
		{global: &Global{}, descriptor: 99}, // Null-owner metadata clears too.
		{global: &Global{}, owner: owners[2], descriptor: 33},
		{global: &Global{}, owner: owners[1], descriptor: 22},
	}
	value, panicked := cleanupTestPanic(func() { railshotReleaseGlobalReferences(entries) })
	if !panicked || value != first || !reflect.DeepEqual(order, []int{0, 1, 2}) {
		t.Fatalf("panic=%v/%v order=%v", value, panicked, order)
	}
	for _, owner := range owners {
		cleanupTestConsumedProducer(t, owner)
	}
	for _, entry := range entries {
		if entry != (importGlobalReference{}) {
			t.Fatal("consumed frozen list still retains owner/global/descriptor")
		}
	}
	railshotReleaseGlobalReferences(entries)
	if len(order) != 3 {
		t.Fatal("repeated frozen drain released aliases twice")
	}
}

func TestRetainedResourceCleanupRVFrozenAliasesRecursiveDrain(t *testing.T) {
	first := new(int)
	var entries []importGlobalReference
	var counts [2]int
	var one *Instance
	one = cleanupTestRootedProducer(t, func() {
		counts[0]++
		for _, entry := range entries {
			if entry.owner == one {
				panic("current alias group not consumed before recursion")
			}
		}
		railshotReleaseGlobalReferences(entries)
		panic(first)
	})
	two := cleanupTestRootedProducer(t, func() { counts[1]++ })
	entries = []importGlobalReference{
		{global: &Global{}, owner: one, descriptor: 11},
		{global: &Global{}, owner: two, descriptor: 22},
		{global: &Global{}, owner: one, descriptor: 11},
		{global: &Global{}, owner: two, descriptor: 22},
	}
	value, panicked := cleanupTestPanic(func() { railshotReleaseGlobalReferences(entries) })
	if !panicked || value != first || counts != [2]int{1, 1} {
		t.Fatalf("panic=%v/%v counts=%v", value, panicked, counts)
	}
	cleanupTestConsumedProducer(t, one)
	cleanupTestConsumedProducer(t, two)
	for _, entry := range entries {
		if entry != (importGlobalReference{}) {
			t.Fatal("recursive drain retained consumed metadata")
		}
	}
	railshotReleaseGlobalReferences(entries)
	if counts != [2]int{1, 1} {
		t.Fatal("outer loop did not reread recursively consumed aliases")
	}
}

func TestRetainedResourceCleanupRVFrozenWarmNoAllocation(t *testing.T) {
	owner := &Instance{}
	if !owner.retainResourceRoot() {
		t.Fatal("warm shadow owner")
	}
	owner.releaseResourceRoot()
	entries := make([]importGlobalReference, 256)
	global := &Global{}
	allocations := testing.AllocsPerRun(100, func() {
		if !owner.retainResourceRoot() {
			panic("live frozen owner root")
		}
		for i := range entries {
			entries[i] = importGlobalReference{global: global, owner: owner, descriptor: 11}
		}
		railshotReleaseGlobalReferences(entries)
	})
	if allocations != 0 || owner.hasResourceRoots() {
		t.Fatalf("allocations=%v roots=%v", allocations, owner.hasResourceRoots())
	}
	for _, entry := range entries {
		if entry != (importGlobalReference{}) {
			t.Fatal("warm drain retained frozen metadata")
		}
	}
}
