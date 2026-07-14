package wago

import (
	"context"
	"testing"
)

func TestFuncrefGlobalProducerRetentionLifecycle(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(funcrefCallableProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	producer, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	descriptor, ok := producer.localFuncrefDescriptor(0)
	if !ok {
		t.Fatal("producer has no local funcref descriptor")
	}
	g := newGlobal(ValFuncRef, descriptor, V128{}, true)
	if !g.retainProducerInstance(producer) {
		t.Fatal("funcref global did not retain its producer")
	}
	if !g.retainProducerInstance(producer) {
		t.Fatal("repeated retention rejected the current producer")
	}
	producer.lifeMu.Lock()
	refs := producer.resourceRefs
	producer.lifeMu.Unlock()
	if refs != 1 {
		t.Fatalf("resource roots = %d, want one deduplicated root", refs)
	}
	writeGlobalObject(g, ValFuncRef, 0)
	if g.retainProducerInstance(producer) {
		t.Fatal("null descriptor retained a producer")
	}
	producer.lifeMu.Lock()
	refs = producer.resourceRefs
	producer.lifeMu.Unlock()
	if refs != 0 {
		t.Fatalf("overwritten descriptor retained %d roots", refs)
	}
	if err := g.Close(); err != nil {
		t.Fatalf("Close global: %v", err)
	}
}
