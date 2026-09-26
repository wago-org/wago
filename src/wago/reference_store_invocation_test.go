package wago

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCInvocationDomainUsesRegisteredAssociation(t *testing.T) {
	collector := new(gc.Collector)
	domain := &gcStoreDomain{collector: collector}
	in := &Instance{gc: collector}
	store := &referenceStore{
		instances: map[*Instance]*referenceStoreInstance{
			in: {gcDomain: domain},
		},
	}
	in.refStore = store

	if got := in.gcInvocationDomain(); got != domain {
		t.Fatalf("invocation domain = %p, want registered domain %p", got, domain)
	}
	store.instances[in].gcDomain = nil
	if got := in.gcInvocationDomain(); got != nil {
		t.Fatalf("released invocation domain = %p, want nil", got)
	}
}

func TestGCDomainCollectorIndexTracksOrderedTopology(t *testing.T) {
	topology := new(gcDomainTopology)
	domains := make([]gcStoreDomain, gcCollectorIndexThreshold+2)
	for i := range domains {
		domains[i].collector = new(gc.Collector)
		topology.appendDomainLocked(&domains[i])
		if got := topology.domainForCollectorLocked(domains[i].collector); got != &domains[i] {
			t.Fatalf("collector %d lookup = %p, want %p", i, got, &domains[i])
		}
	}
	if topology.byCollector == nil || topology.n != len(domains) {
		t.Fatalf("collector index state: indexed=%t domains=%d, want indexed and %d", topology.byCollector != nil, topology.n, len(domains))
	}
	if !topology.unlinkDomainLocked(&domains[2]) || topology.domainForCollectorLocked(domains[2].collector) != nil {
		t.Fatal("removed collector remains in the topology index")
	}
	if topology.first != &domains[0] || topology.last != &domains[len(domains)-1] || topology.n != len(domains)-1 {
		t.Fatalf("unlink changed ordered topology: first=%p last=%p count=%d", topology.first, topology.last, topology.n)
	}
}

func TestGCFrameCodeRangeRepresentativeOwnerUnlinksInConstantTime(t *testing.T) {
	collector := new(gc.Collector)
	compiled := &Compiled{code: []byte{0xc3}, validateMemo: &validateMemo{gcFrameRoots: &compiledGCFrameRoots{}}}
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
	index := store.gcDomains.codeRanges
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
	if store.gcDomains.codeRanges != nil {
		t.Fatal("empty code-range index was retained")
	}
}

func TestReferenceStoreInvocationDomainsIgnoreUnusedImportBindings(t *testing.T) {
	collector := new(gc.Collector)
	domain := &gcStoreDomain{id: 1, collector: collector}
	producer := &Instance{c: &Compiled{}, gc: collector}
	store := &referenceStore{
		instances: map[*Instance]*referenceStoreInstance{
			producer: {gcDomain: domain},
		},
		gcDomains: &gcDomainTopology{first: domain, last: domain, n: 1},
	}
	producer.refStore = store

	consumer := &Instance{
		c:       &Compiled{},
		imports: testImports("env.unused", &InstanceExport{inst: producer}).bindings,
	}
	if err := store.registerInstance(consumer); err != nil {
		t.Fatal(err)
	}
	consumer.refStore = store
	if got := consumer.gcInvocationDomains().len(); got != 0 {
		t.Fatalf("unused import binding added %d GC invocation domain(s), want 0", got)
	}
}

func TestDynamicFuncrefImportOfPrivateGCInvocationDomainRejected(t *testing.T) {
	collector := new(gc.Collector)
	domain := &gcStoreDomain{id: 1, collector: collector, private: true}
	producer := &Instance{
		c:  &Compiled{Entry: []int{0}, Funcs: []FuncSig{{Results: []ValType{ValI32}}}},
		gc: collector,
	}
	store := &referenceStore{
		instances: map[*Instance]*referenceStoreInstance{
			producer: {gcDomain: domain},
		},
	}
	producer.refStore = store
	export := &InstanceExport{inst: producer, localIdx: 0, results: producer.c.Funcs[0].Results}
	consumer := &Compiled{
		Imports:        []string{"env.run"},
		importFuncSigs: []FuncSig{{Results: []ValType{ValI32}}},
		HasTable:       true,
		TableType:      ValFuncRef,
	}

	err := consumer.validateImportBindings(testImports("env.run", export).bindings, store)
	if err == nil || !strings.Contains(err.Error(), "dynamic funcref import") || !strings.Contains(err.Error(), "private GC invocation domain") {
		t.Fatalf("dynamic private-domain import error = %v, want explicit rejection", err)
	}
}

func TestDynamicFuncrefImportOfTransitivePrivateGCInvocationDomainRejected(t *testing.T) {
	privateDomain := &gcStoreDomain{id: 1, collector: new(gc.Collector), private: true}
	var domains gcInvocationDomainSet
	domains.add(privateDomain)
	relay := &Instance{c: &Compiled{Entry: []int{0}, Funcs: []FuncSig{{Results: []ValType{ValI32}}}}}
	relay.executionFlags.Store(executionFlagImportedGCDomain)
	store := &referenceStore{
		instances: map[*Instance]*referenceStoreInstance{
			relay: {invocationDomains: &domains},
		},
	}
	relay.refStore = store
	export := &InstanceExport{inst: relay, localIdx: 0, results: relay.c.Funcs[0].Results}
	consumer := &Compiled{
		Imports:        []string{"env.run"},
		importFuncSigs: []FuncSig{{Results: []ValType{ValI32}}},
		HasTable:       true,
		TableType:      ValFuncRef,
	}

	err := consumer.validateImportBindings(testImports("env.run", export).bindings, store)
	if err == nil || !strings.Contains(err.Error(), "dynamic funcref import") || !strings.Contains(err.Error(), "private GC invocation domain") {
		t.Fatalf("dynamic transitive private-domain import error = %v, want explicit rejection", err)
	}
}

func TestDynamicInvocationDomainsIncludePrivateLocalDomain(t *testing.T) {
	first := &gcStoreDomain{id: 1, collector: new(gc.Collector)}
	private := &gcStoreDomain{id: 2, collector: new(gc.Collector), private: true}
	last := &gcStoreDomain{id: 3, collector: new(gc.Collector), prev: first}
	first.next = last
	in := &Instance{c: &Compiled{}, gc: private.collector}
	in.executionFlags.Store(executionFlagDynamicGCDomain | executionFlagImportedGCDomain)
	store := &referenceStore{
		gcDomains: &gcDomainTopology{first: first, last: last, n: 2},
		instances: map[*Instance]*referenceStoreInstance{
			in: {gcDomain: private, dynamicInvocationDomains: true},
		},
	}
	in.refStore = store

	domains := in.gcInvocationDomains()
	if got := domains.len(); got != 3 {
		t.Fatalf("dynamic private invocation domains = %d, want 3", got)
	}
	for i, want := range []*gcStoreDomain{first, private, last} {
		if got := domains.at(i); got != want {
			t.Fatalf("dynamic private invocation domain %d = %p, want %p", i, got, want)
		}
	}
	owner := newInvocationID()
	lease := in.lockGCInvocation(owner)
	for _, domain := range []*gcStoreDomain{first, private, last} {
		domain.invocationState.Lock()
		got := domain.invocationOwner
		domain.invocationState.Unlock()
		if got != owner {
			lease.unlock()
			t.Fatalf("dynamic private invocation owner = %d, want %d", got, owner)
		}
	}
	lease.unlock()
}

func TestDynamicInvocationDomainInspectionHoldsTopologyReadLease(t *testing.T) {
	domain := &gcStoreDomain{id: 1, collector: new(gc.Collector)}
	in := &Instance{c: &Compiled{}, gc: domain.collector}
	in.executionFlags.Store(executionFlagDynamicGCDomain | executionFlagImportedGCDomain)
	topology := &gcDomainTopology{first: domain, last: domain, n: 1}
	store := &referenceStore{
		gcDomains: topology,
		instances: map[*Instance]*referenceStoreInstance{
			in: {gcDomain: domain, dynamicInvocationDomains: true},
		},
	}
	in.refStore = store

	domains, locked := in.gcInvocationDomainsForInspection()
	if locked != topology || domains.len() != 1 || domains.at(0) != domain {
		if locked != nil {
			locked.RUnlock()
		}
		t.Fatalf("inspection view = %#v topology %p, want one domain and topology %p", domains, locked, topology)
	}
	writeStarted := make(chan struct{})
	writeAcquired := make(chan struct{})
	go func() {
		close(writeStarted)
		topology.Lock()
		close(writeAcquired)
		topology.Unlock()
	}()
	<-writeStarted
	select {
	case <-writeAcquired:
		locked.RUnlock()
		t.Fatal("topology writer bypassed dynamic inspection read lease")
	default:
	}
	locked.RUnlock()
	select {
	case <-writeAcquired:
	case <-time.After(time.Second):
		t.Fatal("topology writer did not resume after inspection read lease release")
	}
}

func TestForeignRuntimeStaticGCProducerImportRejectedForDynamicConsumer(t *testing.T) {
	producerStore := newReferenceStore(false)
	collector := new(gc.Collector)
	producer := &Instance{
		c:        &Compiled{Entry: []int{0}, Funcs: []FuncSig{{Results: []ValType{ValI32}}}},
		gc:       collector,
		refStore: producerStore,
	}
	producerStore.instances = map[*Instance]*referenceStoreInstance{
		producer: {gcDomain: &gcStoreDomain{id: 1, collector: collector}},
	}
	export := &InstanceExport{inst: producer, localIdx: 0, results: producer.c.Funcs[0].Results}
	consumer := &Compiled{
		Imports:        []string{"env.run"},
		importFuncSigs: []FuncSig{{Results: []ValType{ValI32}}},
		HasTable:       true,
		TableType:      ValFuncRef,
	}

	err := consumer.validateImportBindings(testImports("env.run", export).bindings, newReferenceStore(false))
	if err == nil || !strings.Contains(err.Error(), "GC-domain producer") || !strings.Contains(err.Error(), "same Runtime") {
		t.Fatalf("foreign static GC producer import error = %v, want same-Runtime rejection", err)
	}
}

func TestForeignRuntimeDynamicFuncrefProducerImportRejected(t *testing.T) {
	producerStore := newReferenceStore(false)
	producer := &Instance{
		c:        &Compiled{Entry: []int{0}, Funcs: []FuncSig{{Results: []ValType{ValI32}}}},
		refStore: producerStore,
	}
	producer.executionFlags.Store(executionFlagDynamicGCDomain)
	export := &InstanceExport{inst: producer, localIdx: 0, results: producer.c.Funcs[0].Results}
	consumer := &Compiled{
		Imports:        []string{"env.run"},
		importFuncSigs: []FuncSig{{Results: []ValType{ValI32}}},
	}

	err := consumer.validateImportBindings(testImports("env.run", export).bindings, newReferenceStore(false))
	if err == nil || !strings.Contains(err.Error(), "dynamic funcref producer") || !strings.Contains(err.Error(), "same Runtime") {
		t.Fatalf("foreign dynamic producer import error = %v, want same-Runtime rejection", err)
	}
}

func BenchmarkGCInvocationDomainManyDomains(b *testing.B) {
	const domainCount = 4096
	collectors := make([]gc.Collector, domainCount)
	domains := make([]gcStoreDomain, domainCount)
	for i := range domains {
		domains[i].collector = &collectors[i]
		if i > 0 {
			domains[i].prev = &domains[i-1]
		}
		if i+1 < len(domains) {
			domains[i].next = &domains[i+1]
		}
	}
	target := &domains[len(domains)-1]
	in := &Instance{gc: target.collector}
	store := &referenceStore{
		gcDomains: &gcDomainTopology{first: &domains[0], last: target, n: len(domains)},
		instances: map[*Instance]*referenceStoreInstance{
			in: {gcDomain: target},
		},
	}
	in.refStore = store

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if in.gcInvocationDomain() != target {
			b.Fatal("invocation domain changed")
		}
	}
}

func BenchmarkGCDomainLookupByCollector(b *testing.B) {
	for _, domainCount := range []int{4, 16, 256, 4096} {
		b.Run(fmt.Sprintf("domains=%d", domainCount), func(b *testing.B) {
			collectors := make([]gc.Collector, domainCount)
			domains := make([]gcStoreDomain, domainCount)
			topology := new(gcDomainTopology)
			for i := range domains {
				domains[i].collector = &collectors[i]
				topology.appendDomainLocked(&domains[i])
			}
			target := &collectors[len(collectors)-1]
			b.Run("indexed", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if topology.domainForCollectorLocked(target) != &domains[len(domains)-1] {
						b.Fatal("collector domain lookup changed")
					}
				}
			})
			b.Run("linked-list", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					var got *gcStoreDomain
					for domain := topology.first; domain != nil; domain = domain.next {
						if domain.collector == target {
							got = domain
							break
						}
					}
					if got != &domains[len(domains)-1] {
						b.Fatal("collector domain lookup changed")
					}
				}
			})
		})
	}
}
