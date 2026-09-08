package wago

import (
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
		imports: Imports{"env.unused": &InstanceExport{inst: producer}},
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

	err := consumer.validateImportBindings(Imports{"env.run": export}, store)
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

	err := consumer.validateImportBindings(Imports{"env.run": export}, store)
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

	err := consumer.validateImportBindings(Imports{"env.run": export}, newReferenceStore(false))
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

	err := consumer.validateImportBindings(Imports{"env.run": export}, newReferenceStore(false))
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
