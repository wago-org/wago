//go:build !riscv64

package wago

import (
	"sync/atomic"
	"testing"
	"unsafe"
)

func TestReferenceRootShadowExistingTargetFootprint(t *testing.T) {
	// Mirror the sidecar before the RV64-only holder. Empty accounting must
	// preserve its size and each following field's alignment on existing targets.
	type previousImportState struct {
		pluginGCImports  map[uint32]struct{}
		funcrefImports   atomic.Pointer[funcrefImportContainers]
		importRefEntries []importReferenceEntry
		globalRefEntries []importGlobalReference
	}
	var previous previousImportState
	var current instanceImportState
	if unsafe.Sizeof(referenceRootAccounting{}) != 0 || unsafe.Sizeof(current) != unsafe.Sizeof(previous) ||
		unsafe.Offsetof(current.funcrefImports) != unsafe.Offsetof(previous.funcrefImports) ||
		unsafe.Offsetof(current.importRefEntries) != unsafe.Offsetof(previous.importRefEntries) ||
		unsafe.Offsetof(current.globalRefEntries) != unsafe.Offsetof(previous.globalRefEntries) {
		t.Fatal("existing-target import sidecar footprint changed")
	}
}

func TestReferenceRootShadowExistingTargetRetainReleaseNoAlloc(t *testing.T) {
	in := &Instance{}
	failed := false
	allocs := testing.AllocsPerRun(100, func() {
		if !in.retainResourceRoot() || !in.retainResourceRootForFinalization() {
			failed = true
			return
		}
		in.releaseResourceRoot()
		in.releaseResourceRoot()
	})
	if failed || allocs != 0 || in.importState.Load() != nil || in.resourceRefs != 0 {
		t.Fatalf("existing-target roots failed=%t allocations=%g sidecar=%p roots=%d", failed, allocs, in.importState.Load(), in.resourceRefs)
	}
}
