//go:build linux && !tinygo && !wago_target_tinygo

package wago

import (
	goruntime "runtime"
	"sort"
	"testing"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestReferenceEntryCleanupTokenMultiplicityAndMapReentry(t *testing.T) {
	for _, directClaim := range []bool{false, true} {
		t.Run(map[bool]string{false: "closeRuntime", true: "orderedClaim"}[directClaim], func(t *testing.T) {
			first := new(int)
			var store *referenceStore
			var callbacks [2]int
			var detached, unlocked bool
			shared := cleanupTestRootedProducer(t, func() {
				callbacks[0]++
				if !store.mu.TryLock() {
					panic("producer callback under reference-store lock")
				}
				unlocked = true
				detached = len(store.byToken) == 0 && len(store.byIdentity) == 0
				store.mu.Unlock()
				store.closeRuntime() // maps already claimed: no second token root
				panic(first)
			})
			if !shared.retainResourceRootForFinalization() {
				t.Fatal("second distinct token root")
			}
			other := cleanupTestRootedProducer(t, func() { callbacks[1]++ })
			entries := []*funcrefTokenEntry{
				{token: 1, descriptor: 1, owner: shared},
				{token: 2, descriptor: 2, owner: shared},
				{token: 3, descriptor: 3, owner: other},
			}
			// Diagnostic map-owned tokens exercise exact root multiplicity and
			// map claim/reentry; no public issuance or native descriptor is claimed.
			store = &referenceStore{byToken: make(map[uint64]*funcrefTokenEntry), byIdentity: make(map[funcrefIdentity]*funcrefTokenEntry)}
			for _, entry := range entries {
				store.byToken[entry.token] = entry
				store.byIdentity[funcrefIdentity{descriptor: entry.descriptor}] = entry
			}
			var batch referenceTokenEntries
			var release func()
			if directClaim {
				store.mu.Lock()
				store.runtimeClosed = true
				batch = store.releaseEntriesLocked()
				store.mu.Unlock()
				// Force the panicking shared producer before the later owner;
				// ordering never duplicates or changes the claimed token units.
				sort.Slice(batch.funcrefs, func(i, j int) bool { return batch.funcrefs[i].token < batch.funcrefs[j].token })
				release = func() { releaseReferenceEntries(batch) }
			} else {
				release = store.closeRuntime
			}
			value, panicked := cleanupTestPanic(release)
			if !panicked || value != first || callbacks != [2]int{1, 1} || !detached || !unlocked {
				t.Fatalf("panic=%v/%v callbacks=%v detached=%v unlocked=%v", value, panicked, callbacks, detached, unlocked)
			}
			cleanupTestConsumedProducer(t, shared)
			cleanupTestConsumedProducer(t, other)
			for _, entry := range batch.funcrefs {
				if entry != nil {
					t.Fatal("drain retained a consumed token pointer")
				}
			}
			// Clearing the local drain must not erase the shared token objects.
			for i, entry := range entries {
				if entry.token != uint64(i+1) || entry.descriptor != uint64(i+1) || entry.owner == nil {
					t.Fatal("shared token object mutated by slice drain")
				}
			}
			store.closeRuntime()
			if callbacks != [2]int{1, 1} {
				t.Fatal("repeated store close released consumed roots")
			}
		})
	}
}

func TestReferenceEntryCleanupHostNotificationPrecedesProducer(t *testing.T) {
	first := new(int)
	var host *HostFuncRef
	var notified bool
	owner := cleanupTestRootedProducer(t, func() {
		host.mu.Lock()
		notified = !host.tokenLive && host.source == nil && host.descriptor == 0
		host.mu.Unlock()
		panic(first)
	})
	owner.c.NumImports = 1
	owner.c.Imports = []string{"host.call"}
	owner.c.FuncTypeID = append(owner.c.FuncTypeID, 1)
	directory := make([]byte, 2*coreruntime.FuncRefDescBytes)
	owner.funcRefDescs = directory
	descriptor := uint64(uintptr(unsafe.Pointer(&directory[coreruntime.FuncRefDescBytes])))
	defer goruntime.KeepAlive(directory)
	host = &HostFuncRef{source: owner, descriptor: descriptor, tokenLive: true}
	owner.imports = resolvedImports{owner.c.functionImportBindingKey(0): host}
	owner.ensureImportState().importRefEntries = []importReferenceEntry{{address: descriptor, host: host}}
	// This is a diagnostic directory membership fixture for the real host
	// notification lookup. It publishes no executable thunk or guest entry.
	if owner.hostFuncRefForDescriptor(descriptor) != host {
		t.Fatal("fixture did not resolve the actual host token owner")
	}
	batch := referenceTokenEntries{funcrefs: []*funcrefTokenEntry{{token: 1, descriptor: descriptor, owner: owner}}}
	value, panicked := cleanupTestPanic(func() { releaseReferenceEntries(batch) })
	if !panicked || value != first || !notified || batch.funcrefs[0] != nil {
		t.Fatalf("panic=%v/%v notified=%v retained=%v", value, panicked, notified, batch.funcrefs[0])
	}
	cleanupTestConsumedProducer(t, owner)
}

func TestReferenceEntryCleanupNotificationFailureStillReleasesRoot(t *testing.T) {
	first, later := new(int), new(int)
	var callbacks [2]int
	one := cleanupTestRootedProducer(t, func() { callbacks[0]++; panic(later) })
	two := cleanupTestRootedProducer(t, func() { callbacks[1]++ })
	batch := referenceTokenEntries{funcrefs: []*funcrefTokenEntry{{token: 1, owner: one}, {token: 2, owner: two}}}
	var notifications int
	value, panicked := cleanupTestPanic(func() {
		releaseReferenceEntriesWith(batch, func(entry *funcrefTokenEntry) {
			notifications++
			if entry.token == 1 {
				panic(first) // local metadata phase; actual root cannot be skipped
			}
		}, nil, nil)
	})
	if !panicked || value != first || notifications != 2 || callbacks != [2]int{1, 1} {
		t.Fatalf("panic=%v/%v notifications=%d callbacks=%v", value, panicked, notifications, callbacks)
	}
	cleanupTestConsumedProducer(t, one)
	cleanupTestConsumedProducer(t, two)
}
