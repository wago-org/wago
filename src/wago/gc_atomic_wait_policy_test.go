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
			gcStructProduct: stagedGCStructGeneric,
			flags:           compiledCacheAtomicWaitHelpers,
		},
	}

	err := consumer.validateImportBindings(nil, newReferenceStore(false))
	if err == nil || !strings.Contains(err.Error(), "dynamic funcref reachability") || !strings.Contains(err.Error(), "atomic wait helpers") {
		t.Fatalf("GC+Threads dynamic funcref error = %v, want atomic-wait rejection", err)
	}
}

func TestGCAtomicWaitReferenceImportRejected(t *testing.T) {
	collector := new(gc.Collector)
	store := &referenceStore{gcDomains: &gcDomainTopology{first: &gcStoreDomain{collector: collector}, n: 1}}
	store.gcDomains.last = store.gcDomains.first
	producer := &Instance{
		c: &Compiled{
			Entry:        []int{0},
			Funcs:        []FuncSig{{Params: []ValType{ValAnyRef}, Results: []ValType{ValI32}}},
			validateMemo: &validateMemo{gcFrameRoots: &compiledGCFrameRoots{}},
		},
		gc:       collector,
		refStore: store,
	}
	export := &InstanceExport{inst: producer, localIdx: 0, params: producer.c.Funcs[0].Params, results: producer.c.Funcs[0].Results}
	consumer := &Compiled{
		Imports:        []string{"env.read"},
		importFuncSigs: []FuncSig{{Params: []ValType{ValAnyRef}, Results: []ValType{ValI32}}},
		codeCache: &compiledCodeCache{
			gcStructProduct: stagedGCStructGeneric,
			flags:           compiledCacheAtomicWaitHelpers,
		},
		validateMemo: &validateMemo{gcFrameRoots: &compiledGCFrameRoots{}},
	}

	err := consumer.validateImportBindings(Imports{"env.read": export}, store)
	if err == nil || !strings.Contains(err.Error(), "atomic wait helpers") {
		t.Fatalf("GC+Threads reference import error = %v, want atomic-wait rejection", err)
	}
}

func TestGCAtomicWaitScalarRuntimeGCDomainImportRejected(t *testing.T) {
	collector := new(gc.Collector)
	domain := &gcStoreDomain{collector: collector}
	store := &referenceStore{gcDomains: &gcDomainTopology{first: domain, last: domain, n: 1}}
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
			gcStructProduct: stagedGCStructGeneric,
			flags:           compiledCacheAtomicWaitHelpers,
		},
		validateMemo: &validateMemo{gcFrameRoots: &compiledGCFrameRoots{}},
	}

	err := consumer.validateImportBindings(Imports{"env.notify": export}, store)
	if err == nil || !strings.Contains(err.Error(), "Runtime GC-domain import") || !strings.Contains(err.Error(), "atomic wait helpers") {
		t.Fatalf("GC+Threads scalar Runtime GC import error = %v, want atomic-wait GC-domain rejection", err)
	}
}

func TestGCAtomicWaitForeignRuntimeGCDomainImportRejected(t *testing.T) {
	collector := new(gc.Collector)
	domain := &gcStoreDomain{collector: collector}
	producerStore := &referenceStore{gcDomains: &gcDomainTopology{first: domain, last: domain, n: 1}}
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
			gcStructProduct: stagedGCStructGeneric,
			flags:           compiledCacheAtomicWaitHelpers,
		},
		validateMemo: &validateMemo{gcFrameRoots: &compiledGCFrameRoots{}},
	}
	consumerStore := newReferenceStore(false)

	err := consumer.validateImportBindings(Imports{"env.notify": export}, consumerStore)
	if err == nil || !strings.Contains(err.Error(), "Runtime GC-domain import") || !strings.Contains(err.Error(), "atomic wait helpers") {
		t.Fatalf("GC+Threads foreign Runtime GC import error = %v, want atomic-wait GC-domain rejection", err)
	}
}
