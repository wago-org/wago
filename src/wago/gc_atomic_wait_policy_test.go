//go:build !wago_no_gc

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCAtomicWaitDynamicFuncrefReachabilityRejected(t *testing.T) {
	consumer := &Compiled{
		HasTable:  true,
		TableType: ValFuncRef,
		codeCache: &compiledCodeCache{
			compiledGCProductState: compiledGCProductState{compiledGCProducts{gcStructProduct: stagedGCStructGeneric}},
			flags:                  compiledCacheAtomicWaitHelpers,
		},
	}

	err := consumer.validateImportBindings(nil, newReferenceStore(false))
	if err == nil || !strings.Contains(err.Error(), "dynamic funcref reachability") || !strings.Contains(err.Error(), "atomic wait helpers") {
		t.Fatalf("GC+Threads dynamic funcref error = %v, want atomic-wait rejection", err)
	}
}

func TestGCAtomicWaitReferenceImportRejected(t *testing.T) {
	if !nativeCollectorFrameRootsBuilt {
		t.Skip("requires compiled native frame roots")
	}
	collector := new(gc.Collector)
	store := &referenceStore{gcDomainStoreState: testGCDomainStoreState(t, &gcDomainTopology{first: &gcStoreDomain{collector: collector}, n: 1})}
	store.gcTopologyLocked().last = store.gcTopologyLocked().first
	producer := &Instance{
		c: &Compiled{
			Entry:        []int{0},
			Funcs:        []FuncSig{{Params: []ValType{ValAnyRef}, Results: []ValType{ValI32}}},
			validateMemo: &validateMemo{compiledGCFrameRootState: testCompiledGCFrameRootState(&compiledGCFrameRoots{})},
		},
		gc:       collector,
		refStore: store,
	}
	export := &InstanceExport{inst: producer, localIdx: 0, params: producer.c.Funcs[0].Params, results: producer.c.Funcs[0].Results}
	consumer := &Compiled{
		Imports:        []string{"env.read"},
		importFuncSigs: []FuncSig{{Params: []ValType{ValAnyRef}, Results: []ValType{ValI32}}},
		codeCache: &compiledCodeCache{
			compiledGCProductState: compiledGCProductState{compiledGCProducts{gcStructProduct: stagedGCStructGeneric}},
			flags:                  compiledCacheAtomicWaitHelpers,
		},
		validateMemo: &validateMemo{compiledGCFrameRootState: testCompiledGCFrameRootState(&compiledGCFrameRoots{})},
	}

	err := consumer.validateImportBindings(testImports("env.read", export).bindings, store)
	if err == nil || !strings.Contains(err.Error(), "atomic wait helpers") {
		t.Fatalf("GC+Threads reference import error = %v, want atomic-wait rejection", err)
	}
}

func TestGCAtomicWaitScalarRuntimeGCDomainImportRejected(t *testing.T) {
	if !nativeCollectorFrameRootsBuilt {
		t.Skip("requires compiled native frame roots")
	}
	collector := new(gc.Collector)
	domain := &gcStoreDomain{collector: collector}
	store := &referenceStore{gcDomainStoreState: testGCDomainStoreState(t, &gcDomainTopology{first: domain, last: domain, n: 1})}
	producer := &Instance{
		c: &Compiled{
			Entry: []int{0},
			Funcs: []FuncSig{{Results: []ValType{ValI32}}},
		},
		gc:       collector,
		refStore: store,
	}
	export := &InstanceExport{inst: producer, localIdx: 0, results: producer.c.Funcs[0].Results}
	consumer := &Compiled{
		Imports:        []string{"env.notify"},
		importFuncSigs: []FuncSig{{Results: []ValType{ValI32}}},
		codeCache: &compiledCodeCache{
			compiledGCProductState: compiledGCProductState{compiledGCProducts{gcStructProduct: stagedGCStructGeneric}},
			flags:                  compiledCacheAtomicWaitHelpers,
		},
		validateMemo: &validateMemo{compiledGCFrameRootState: testCompiledGCFrameRootState(&compiledGCFrameRoots{})},
	}

	err := consumer.validateImportBindings(testImports("env.notify", export).bindings, store)
	if err == nil || !strings.Contains(err.Error(), "Runtime GC-domain import") || !strings.Contains(err.Error(), "atomic wait helpers") {
		t.Fatalf("GC+Threads scalar Runtime GC import error = %v, want atomic-wait GC-domain rejection", err)
	}
}

func TestGCAtomicWaitForeignRuntimeGCDomainImportRejected(t *testing.T) {
	if !nativeCollectorFrameRootsBuilt {
		t.Skip("requires compiled native frame roots")
	}
	collector := new(gc.Collector)
	domain := &gcStoreDomain{collector: collector}
	producerStore := &referenceStore{gcDomainStoreState: testGCDomainStoreState(t, &gcDomainTopology{first: domain, last: domain, n: 1})}
	producer := &Instance{
		c: &Compiled{
			Entry: []int{0},
			Funcs: []FuncSig{{Results: []ValType{ValI32}}},
		},
		gc:       collector,
		refStore: producerStore,
	}
	export := &InstanceExport{inst: producer, localIdx: 0, results: producer.c.Funcs[0].Results}
	consumer := &Compiled{
		Imports:        []string{"env.notify"},
		importFuncSigs: []FuncSig{{Results: []ValType{ValI32}}},
		codeCache: &compiledCodeCache{
			compiledGCProductState: compiledGCProductState{compiledGCProducts{gcStructProduct: stagedGCStructGeneric}},
			flags:                  compiledCacheAtomicWaitHelpers,
		},
		validateMemo: &validateMemo{compiledGCFrameRootState: testCompiledGCFrameRootState(&compiledGCFrameRoots{})},
	}
	consumerStore := newReferenceStore(false)

	err := consumer.validateImportBindings(testImports("env.notify", export).bindings, consumerStore)
	if err == nil || !strings.Contains(err.Error(), "Runtime GC-domain import") || !strings.Contains(err.Error(), "atomic wait helpers") {
		t.Fatalf("GC+Threads foreign Runtime GC import error = %v, want atomic-wait GC-domain rejection", err)
	}
}
