//go:build linux && !tinygo && !wago_target_tinygo

package wago

import (
	"reflect"
	"testing"
)

func TestPhysicalFinalizerChainContinuesAfterPanic(t *testing.T) {
	first, later := new(int), new(int)
	var visited []int
	in := cleanupTestRootedProducer(t, func() {
		visited = append(visited, 0)
		panic(first)
	})
	in.referenceLifetime().afterPhysicalRelease(func() {
		visited = append(visited, 1)
		panic(later)
	})
	in.referenceLifetime().afterPhysicalRelease(func() { visited = append(visited, 2) })
	value, panicked := cleanupTestPanic(in.releaseResourceRoot)
	if !panicked || value != first || !reflect.DeepEqual(visited, []int{0, 1, 2}) {
		t.Fatalf("panic=%v/%v callbacks=%v", value, panicked, visited)
	}
	cleanupTestConsumedProducer(t, in)
	if in.finalizers != nil {
		t.Fatal("claimed finalizer chain remained published")
	}
	in.releaseResourceRoot()
	in.tryFinalize()
	if !reflect.DeepEqual(visited, []int{0, 1, 2}) {
		t.Fatal("consumed physical finalizers retried")
	}
}

func TestPhysicalFinalizerChainPreservesReentrantImmediateCallback(t *testing.T) {
	first, later := new(int), new(int)
	var visited []int
	var in *Instance
	in = cleanupTestRootedProducer(t, func() {
		visited = append(visited, 0)
		// This stage deliberately preserves the established resourcesClosed
		// timing: a callback registered during physical callbacks runs now.
		in.referenceLifetime().afterPhysicalRelease(func() {
			visited = append(visited, 1)
			in.releaseResourceRoot() // root already consumed: no second chain
			panic(first)
		})
		visited = append(visited, 99) // cannot resume a panicking callback body
	})
	in.referenceLifetime().afterPhysicalRelease(func() {
		visited = append(visited, 2)
		panic(later)
	})
	in.referenceLifetime().afterPhysicalRelease(func() { visited = append(visited, 3) })
	value, panicked := cleanupTestPanic(in.releaseResourceRoot)
	if !panicked || value != first || !reflect.DeepEqual(visited, []int{0, 1, 2, 3}) {
		t.Fatalf("panic=%v/%v callbacks=%v", value, panicked, visited)
	}
	cleanupTestConsumedProducer(t, in)
	if in.finalizers != nil {
		t.Fatal("reentrant callback republished the consumed chain")
	}
}

func TestPhysicalFinalizerChainDrainsOwnedContainersBeforeOwnerCallback(t *testing.T) {
	first, later := new(int), new(int)
	var visited []int
	var root *Instance
	root = cleanupTestRootedProducer(t, func() {
		if root.globalCells != nil || root.funcRefDescs != nil || root.jm != nil || root.ar != nil || root.eng != nil {
			panic("owner callback preceded native view cleanup")
		}
		visited = append(visited, 2)
	})
	producer := cleanupTestRootedProducer(t, func() {
		visited = append(visited, 0)
		panic(first)
	})
	globalProducer := cleanupTestRootedProducer(t, func() {
		visited = append(visited, 1)
		panic(later)
	})
	root.table = &Table{retained: map[*Instance]*retainedInstanceRoot{producer: {precise: true}}}
	root.globalCells = []*Global{{cell: make([]byte, 8), owner: &globalOwner{instance: root, retained: map[*Instance]*retainedInstanceRoot{globalProducer: {precise: true}}}}}
	root.funcRefDescs = make([]byte, 40)
	value, panicked := cleanupTestPanic(root.releaseResourceRoot)
	if !panicked || value != first || !reflect.DeepEqual(visited, []int{0, 1, 2}) {
		t.Fatalf("panic=%v/%v callbacks=%v", value, panicked, visited)
	}
	for _, in := range []*Instance{root, producer, globalProducer} {
		cleanupTestConsumedProducer(t, in)
		in.tryFinalize()
	}
	if !reflect.DeepEqual(visited, []int{0, 1, 2}) {
		t.Fatal("consumed container callbacks retried")
	}
}
