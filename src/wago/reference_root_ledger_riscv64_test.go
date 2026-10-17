//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rootLedgerSnapshotRV64(in *Instance) (refs, external int, node *referenceRootLedger) {
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	refs = in.resourceRefs
	if state := in.importState.Load(); state != nil {
		node = state.rootAccounting.ledger
		if node != nil {
			external = node.external
		}
	}
	return
}

func assertRootLedgerRV64(t *testing.T, in *Instance, want int) *referenceRootLedger {
	t.Helper()
	refs, external, node := rootLedgerSnapshotRV64(in)
	if refs != want || external != want || node == nil {
		t.Fatalf("resource/shadow roots = %d/%d, node %p; want %d", refs, external, node, want)
	}
	return node
}

func TestRV64ReferenceRootShadowRejectedAcquisitionDoesNotAllocate(t *testing.T) {
	var absent *Instance
	if absent.retainResourceRoot() || absent.retainResourceRootForFinalization() {
		t.Fatal("nil owner acquired a root")
	}
	absent.releaseResourceRoot()
	closed := &Instance{}
	closed.invocationState.Store(instanceInvocationClosed)
	for _, tc := range []struct {
		name string
		in   *Instance
	}{
		{"closed", closed},
		{"physically-closed", &Instance{resourcesClosed: true}},
		{"exhausted", &Instance{resourceRefs: maxInt()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.in.retainResourceRoot() {
				t.Fatal("rejected owner acquired a root")
			}
			if tc.name != "closed" && tc.in.retainResourceRootForFinalization() {
				t.Fatal("rejected owner acquired a finalization root")
			}
			if tc.in.importState.Load() != nil {
				t.Fatal("rejected acquisition allocated a sidecar")
			}
		})
	}
	empty := &Instance{}
	empty.releaseResourceRoot()
	if empty.importState.Load() != nil {
		t.Fatal("zero release allocated a sidecar")
	}
	// Existing/synthetic roots may predate the shadow and must not allocate on
	// release, nor turn a zero release into negative accounting.
	legacy := &Instance{resourceRefs: 1}
	legacy.releaseResourceRoot()
	legacy.releaseResourceRoot()
	if legacy.resourceRefs != 0 || legacy.importState.Load() != nil {
		t.Fatal("untracked release changed the lazy ownership contract")
	}
}

func TestRV64ReferenceRootShadowBalancesAnonymousAndPreexistingRoots(t *testing.T) {
	in := &Instance{resourceRefs: 3}
	if !in.retainResourceRoot() {
		t.Fatal("ordinary acquisition failed")
	}
	node := assertRootLedgerRV64(t, in, 4)
	in.invocationState.Store(instanceInvocationClosed)
	if in.retainResourceRoot() {
		t.Fatal("closed owner admitted an ordinary root")
	}
	assertRootLedgerRV64(t, in, 4)
	if !in.retainResourceRootForFinalization() {
		t.Fatal("physically live closed owner rejected a finalization root")
	}
	assertRootLedgerRV64(t, in, 5)
	for n := 4; n >= 0; n-- {
		in.releaseResourceRoot()
		if assertRootLedgerRV64(t, in, n) != node {
			t.Fatal("release replaced the shadow node")
		}
	}
	in.releaseResourceRoot()
	assertRootLedgerRV64(t, in, 0)
	if !in.retainResourceRootForFinalization() {
		t.Fatal("warmed finalization acquisition failed")
	}
	if assertRootLedgerRV64(t, in, 1) != node {
		t.Fatal("zero-count reuse replaced the node")
	}
	in.releaseResourceRoot()
}

func TestRV64ReferenceRootShadowWarmAcquisitionReleaseNoAlloc(t *testing.T) {
	in := &Instance{}
	if !in.retainResourceRoot() {
		t.Fatal("warmup acquisition failed")
	}
	node := assertRootLedgerRV64(t, in, 1)
	in.releaseResourceRoot()
	failed := false
	allocs := testing.AllocsPerRun(100, func() {
		if !in.retainResourceRoot() || !in.retainResourceRootForFinalization() {
			failed = true
			return
		}
		in.releaseResourceRoot()
		in.releaseResourceRoot()
	})
	if failed || allocs != 0 {
		t.Fatalf("warmed retain/release failed=%t allocations=%g", failed, allocs)
	}
	if assertRootLedgerRV64(t, in, 0) != node {
		t.Fatal("warm traffic replaced the node")
	}
}

func TestRV64ReferenceRootShadowConcurrentBalance(t *testing.T) {
	in := &Instance{}
	var workers sync.WaitGroup
	failures := make(chan struct{}, 16)
	for worker := 0; worker < cap(failures); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for n := 0; n < 100; n++ {
				if !in.retainResourceRoot() {
					failures <- struct{}{}
					return
				}
				in.releaseResourceRoot()
			}
		}()
	}
	workers.Wait()
	if len(failures) != 0 {
		t.Fatal("concurrent acquisition failed")
	}
	assertRootLedgerRV64(t, in, 0)
}

func TestRV64ReferenceRootShadowPreservesPhysicalClose(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}))),
	)
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "root-shadow-identity", source, artifact)
			in, err := Instantiate(c, nil)
			if in != nil {
				defer in.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			held := 0
			defer func() {
				for held > 0 {
					in.releaseResourceRoot()
					held--
				}
			}()
			got, err := in.Invoke("identity", 0xffff000012345678)
			if err != nil || len(got) != 1 || got[0] != 0x12345678 {
				t.Fatalf("native identity: %x %v", got, err)
			}
			if !in.retainResourceRoot() {
				t.Fatal("live instance rejected root")
			}
			held++
			assertRootLedgerRV64(t, in, 1)
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			if !in.hasPhysicalResources() || in.retainResourceRoot() {
				t.Fatal("logical close changed held storage or ordinary admission")
			}
			if !in.retainResourceRootForFinalization() {
				t.Fatal("held storage rejected finalization root")
			}
			held++
			assertRootLedgerRV64(t, in, 2)
			in.releaseResourceRoot()
			held--
			assertRootLedgerRV64(t, in, 1)
			if !in.hasPhysicalResources() {
				t.Fatal("first release freed storage still held by a root")
			}
			in.releaseResourceRoot()
			held--
			assertRootLedgerRV64(t, in, 0)
			if in.hasPhysicalResources() || in.retainResourceRootForFinalization() {
				t.Fatal("last release failed to finalize storage or allowed resurrection")
			}
			in.releaseResourceRoot()
			assertRootLedgerRV64(t, in, 0)
		})
	}
}
