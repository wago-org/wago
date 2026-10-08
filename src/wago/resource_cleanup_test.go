//go:build linux && !tinygo && !wago_target_tinygo

package wago

import (
	"reflect"
	"testing"
)

func cleanupTestPanic(fn func()) (value any, panicked bool) {
	returned := false
	defer func() {
		if !returned {
			value = recover()
			panicked = true
		}
	}()
	fn()
	returned = true
	return nil, false
}

func TestResourceCleanupContinuesAndPreservesFirstPanic(t *testing.T) {
	first, second := new(int), new(int)
	var visited []int
	var cleanup resourceCleanup
	cleanup.run(func() { visited = append(visited, 0) })
	cleanup.run(func() { visited = append(visited, 1); panic(first) })
	cleanup.run(func() { visited = append(visited, 2); panic(second) })
	cleanup.run(func() { visited = append(visited, 3) })
	value, panicked := cleanupTestPanic(cleanup.finish)
	if !panicked || value != first || !reflect.DeepEqual(visited, []int{0, 1, 2, 3}) {
		t.Fatalf("panic=%v/%v visited=%v", value, panicked, visited)
	}
}

func TestResourceCleanupWarmSuccessDoesNotAllocate(t *testing.T) {
	var calls int
	if allocations := testing.AllocsPerRun(100, func() {
		var cleanup resourceCleanup
		cleanup.run(func() { calls++ })
		cleanup.finish()
	}); allocations != 0 || calls != 101 {
		t.Fatalf("allocations=%v calls=%d", allocations, calls)
	}
}

func TestImportDedupDrainPanicAndReentry(t *testing.T) {
	var set importDedup[int]
	for i := 0; i < 10; i++ {
		set.add(i)
	}
	if set.index == nil {
		t.Fatal("fixture did not cross the inline and indexed set thresholds")
	}
	first, later := new(int), new(int)
	var counts [11]int
	value, panicked := cleanupTestPanic(func() {
		set.drain(func(item int) {
			counts[item]++ // the ownership unit is consumed before its callback
			if item == 0 {
				if set.n != 0 || set.contains(0) {
					t.Error("original batch still published during release callback")
				}
				set.add(10)
				set.drain(func(nested int) { counts[nested]++ })
				panic(first)
			}
			if item == 4 {
				panic(later)
			}
		})
	})
	if !panicked || value != first {
		t.Fatalf("panic=%v/%v", value, panicked)
	}
	for item, count := range counts {
		if count != 1 {
			t.Errorf("unit %d released %d times", item, count)
		}
	}
	if set.n != 0 || set.extra != nil || set.index != nil || set.inline != [4]int{} {
		t.Fatal("drained batch retained entries")
	}
	set.drain(func(int) { t.Error("consumed unit retried") })
}

func TestImportDedupDrainPreservesNewBatch(t *testing.T) {
	var set importDedup[int]
	set.add(1)
	set.drain(func(item int) {
		if item != 1 {
			t.Fatal(item)
		}
		set.add(2)
	})
	if set.n != 1 || !set.contains(2) || set.contains(1) {
		t.Fatal("reentrant publication was discarded or old batch retained")
	}
	set.drain(func(item int) {
		if item != 2 {
			t.Fatal(item)
		}
	})
}

// These owners exercise real root decrement, tryFinalize, releaseResources, and
// the physical callback. They own no native storage; an empty Compiled makes
// its nil-resource teardown valid without pretending to have a mapped image.
func cleanupTestRootedProducer(t *testing.T, callback func()) *Instance {
	t.Helper()
	in := &Instance{c: &Compiled{}}
	if !in.retainResourceRoot() {
		t.Fatal("retain producer")
	}
	in.referenceLifetime().afterPhysicalRelease(callback)
	in.lifeMu.Lock()
	in.closed = true
	in.lifeMu.Unlock()
	in.invocationState.Store(instanceInvocationClosed)
	return in
}

func cleanupTestConsumedProducer(t *testing.T, in *Instance) {
	t.Helper()
	in.lifeMu.Lock()
	refs, claimed := in.resourceRefs, in.resourcesClosed
	in.lifeMu.Unlock()
	if refs != 0 || !claimed {
		t.Errorf("producer roots=%d physical teardown claimed=%v", refs, claimed)
	}
}

func TestFunctionImportDrainContinuesAfterPhysicalCallbackPanic(t *testing.T) {
	var attachments functionImportAttachments
	var producers []*Instance
	var counts [10]int
	first, later := new(int), new(int)
	for i := range counts {
		index := i
		producer := cleanupTestRootedProducer(t, func() {
			counts[index]++
			if index == 0 {
				attachments.detachAll() // same drain reentry must be empty
				panic(first)
			}
			if index == 4 {
				panic(later)
			}
		})
		producers = append(producers, producer)
		attachments.set.push(producer)
	}
	value, panicked := cleanupTestPanic(attachments.detachAll)
	if !panicked || value != first {
		t.Fatalf("panic=%v/%v", value, panicked)
	}
	for i, producer := range producers {
		cleanupTestConsumedProducer(t, producer)
		if counts[i] != 1 {
			t.Errorf("physical callback %d ran %d times", i, counts[i])
		}
	}
	attachments.detachAll()
}

func TestBuilderRollbackContinuesEveryAttachmentClass(t *testing.T) {
	var builder instanceBuilder
	first, later := new(int), new(int)
	var visited []string
	var producers []*Instance
	newProducer := func(name string, panicValue any) *Instance {
		producer := cleanupTestRootedProducer(t, func() {
			visited = append(visited, name)
			if panicValue != nil {
				panic(panicValue)
			}
		})
		producers = append(producers, producer)
		return producer
	}
	builder.functionAttachments.set.push(newProducer("function0", first))
	builder.functionAttachments.set.push(newProducer("function1", nil))
	host := &HostFuncRef{importers: 1}
	builder.hostAttachments.set.push(host)
	var globals []*Global
	var tables []*Table
	var tags []*Tag
	for i, name := range []string{"global0", "global1"} {
		var value any
		if i == 0 {
			value = later
		}
		global := &Global{owner: &globalOwner{instance: newProducer(name, value), importers: 1}}
		globals = append(globals, global)
		builder.globalAttachments.set.push(global)
	}
	for _, name := range []string{"table0", "table1"} {
		table := &Table{owner: &tableOwner{instance: newProducer(name, nil), importers: 1}}
		tables = append(tables, table)
		builder.tableAttachments.set.push(table)
	}
	for _, name := range []string{"tag0", "tag1"} {
		tag := &Tag{owner: newProducer(name, nil), importers: 1}
		tags = append(tags, tag)
		builder.tagAttachments.set.push(tag)
	}
	// The final registration notification must run after all attachment groups,
	// even when an earlier producer's physical-release callback panics.
	registered := &Instance{c: &Compiled{}}
	storeEntry := &referenceStoreInstance{}
	store := &referenceStore{instances: map[*Instance]*referenceStoreInstance{registered: storeEntry}, liveInstances: 1}
	registered.refStore = store
	builder.registeredInstance = registered
	value, panicked := cleanupTestPanic(builder.rollbackPreparedState)
	if !panicked || value != first {
		t.Fatalf("panic=%v/%v", value, panicked)
	}
	want := []string{"function0", "function1", "global0", "global1", "table0", "table1", "tag0", "tag1"}
	if !reflect.DeepEqual(visited, want) {
		t.Fatalf("callbacks=%v, want %v", visited, want)
	}
	if host.importers != 0 || !storeEntry.closeAccounted || store.liveInstances != 0 {
		t.Fatal("host or registered-store cleanup skipped")
	}
	for _, producer := range producers {
		cleanupTestConsumedProducer(t, producer)
	}
	for _, global := range globals {
		if global.owner.importers != 0 {
			t.Error("global attachment not consumed")
		}
	}
	for _, table := range tables {
		if table.owner.importers != 0 {
			t.Error("table attachment not consumed")
		}
	}
	for _, tag := range tags {
		if tag.importers != 0 {
			t.Error("tag attachment not consumed")
		}
	}
	builder.rollbackPreparedState()
	if !reflect.DeepEqual(visited, want) {
		t.Fatal("second rollback repeated a consumed physical release")
	}
}
