// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

import (
	"math/rand"
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCFrameCodeRangeIndexMatchesLinear(t *testing.T) {
	collector := new(gc.Collector)
	c := &Compiled{code: make([]byte, 7000), validateMemo: &validateMemo{compiledGCFrameRootState: testCompiledGCFrameRootState(&compiledGCFrameRoots{})}}
	store := &referenceStore{instances: make(map[*Instance]*referenceStoreInstance)}
	instances := make([]*Instance, 64)
	for i := range instances {
		in := &Instance{c: c, gc: collector, base: uintptr(0x10000 + i*8192 + 137)}
		instances[i] = in
		store.instances[in] = &referenceStoreInstance{}
		store.registerGCFrameCodeRangeLocked(in)
	}
	rng := rand.New(rand.NewSource(715))
	for _, removed := range rng.Perm(len(instances)) {
		for k := 0; k < 128; k++ {
			pc := uintptr(0x10000 + rng.Intn(65*8192))
			var want *Instance
			for in := range store.instances {
				if pc >= in.base && pc-in.base < uintptr(len(c.code)) {
					want = in
					break
				}
			}
			if got := store.gcFrameOwner(pc, collector); got != want {
				t.Fatalf("PC %x: index=%p linear=%p", pc, got, want)
			}
		}
		store.unregisterGCFrameCodeRangeLocked(instances[removed])
		delete(store.instances, instances[removed])
	}
	if store.gcTopologyLocked().codeRanges != nil {
		t.Fatal("empty index retained")
	}
}

func TestGCFrameCodeRangeSingletonPromotesAndRetires(t *testing.T) {
	collector := new(gc.Collector)
	c := &Compiled{code: make([]byte, 64), validateMemo: &validateMemo{compiledGCFrameRootState: testCompiledGCFrameRootState(&compiledGCFrameRoots{})}}
	first := &Instance{c: c, gc: collector, base: 0x10000}
	second := &Instance{c: c, gc: collector, base: 0x20000}
	store := &referenceStore{instances: map[*Instance]*referenceStoreInstance{first: {}}}
	store.registerGCFrameCodeRangeLocked(first)
	if store.gcTopologyLocked().singleCodeOwner != first || store.gcTopologyLocked().codeRanges != nil {
		t.Fatal("one code owner allocated a directory")
	}
	if store.gcFrameOwner(first.base+1, collector) != first || store.gcFrameOwner(first.base+64, collector) != nil {
		t.Fatal("singleton range boundaries differ from the indexed path")
	}
	store.instances[second] = &referenceStoreInstance{}
	store.registerGCFrameCodeRangeLocked(second)
	if store.gcTopologyLocked().singleCodeOwner != nil || store.gcTopologyLocked().codeRanges == nil {
		t.Fatal("second owner failed to promote the directory")
	}
	for _, in := range []*Instance{first, second} {
		if store.gcFrameOwner(in.base+1, collector) != in {
			t.Fatal("promotion omitted a live owner")
		}
		store.unregisterGCFrameCodeRangeLocked(in)
		delete(store.instances, in)
	}
	if store.gcTopologyLocked().codeRanges != nil || store.gcTopologyLocked().singleCodeOwner != nil {
		t.Fatal("empty directory retained owners")
	}
	store.instances[first] = &referenceStoreInstance{}
	store.registerGCFrameCodeRangeLocked(first)
	store.unregisterGCFrameCodeRangeLocked(first)
	delete(store.instances, first)
	if store.gcTopologyLocked().singleCodeOwner != nil {
		t.Fatal("retired singleton retained")
	}
}

func TestGCFrameCodeRangeRepresentativeOwnerUnlinksInConstantTime(t *testing.T) {
	collector := new(gc.Collector)
	compiled := &Compiled{code: []byte{0xc3}, validateMemo: &validateMemo{compiledGCFrameRootState: testCompiledGCFrameRootState(&compiledGCFrameRoots{})}}
	first := &Instance{c: compiled, gc: collector, base: 0x10000}
	second := &Instance{c: compiled, gc: collector, base: 0x10000}
	store := &referenceStore{
		instances: map[*Instance]*referenceStoreInstance{
			first:  {},
			second: {},
		},
	}
	store.registerGCFrameCodeRangeLocked(first)
	store.registerGCFrameCodeRangeLocked(second)
	index := store.gcTopologyLocked().codeRanges
	collectorRanges := index.byCollector[collector]
	key := gcFrameCodeRangeKey{collector: collector, compiled: compiled, base: first.base}
	image := collectorRanges.byImage[key]
	if image == nil || image.owner != second || image.owners == nil || image.owners.instance != second {
		t.Fatalf("shared code representative = %+v, want second instance", image)
	}

	store.unregisterGCFrameCodeRangeLocked(second)
	if image.owner != first || image.owners == nil || image.owners.instance != first || index.byInstance[second] != nil {
		t.Fatalf("representative after unlink = %+v, want first instance", image)
	}
	store.unregisterGCFrameCodeRangeLocked(first)
	if store.gcTopologyLocked().codeRanges != nil {
		t.Fatal("empty code-range index was retained")
	}
}
