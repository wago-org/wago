package wago

import (
	"context"
	"testing"
)

func TestFailedInstantiationTerminatesReferenceStoreRegistration(t *testing.T) {
	rt := NewRuntime()
	producerCode, err := rt.Compile(funcrefCallableProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	producer, err := rt.Instantiate(context.Background(), producerCode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := producer.Invoke("get"); err != nil {
		t.Fatalf("issue funcref token: %v", err)
	}
	if _, err := rt.NewExternRef("failed-instantiation-root"); err != nil {
		t.Fatalf("issue externref: %v", err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("close producer: %v", err)
	}

	failing := mustCompileWat(rt, t, `(module (func $fail unreachable) (start $fail))`)
	if in, err := rt.Instantiate(context.Background(), failing); in != nil || err == nil {
		t.Fatalf("failing Instantiate = %p, %v; want nil start trap", in, err)
	}

	rt.refStore.mu.Lock()
	if live := rt.refStore.liveInstances; live != 0 {
		rt.refStore.mu.Unlock()
		t.Fatalf("liveInstances after rollback = %d, want 0", live)
	}
	for instance, state := range rt.refStore.instances {
		if state.closeAccounted && !state.quiesced {
			rt.refStore.mu.Unlock()
			t.Fatalf("rollback left close-accounted unquiesced instance %p: %+v", instance, state)
		}
	}
	rt.refStore.mu.Unlock()

	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
	rt.refStore.mu.Lock()
	defer rt.refStore.mu.Unlock()
	if rt.refStore.liveInstances != 0 || len(rt.refStore.instances) != 0 {
		t.Fatalf("terminal store membership: live=%d instances=%d", rt.refStore.liveInstances, len(rt.refStore.instances))
	}
	if len(rt.refStore.byToken) != 0 || len(rt.refStore.byIdentity) != 0 {
		t.Fatalf("funcref token maps retained after terminal rollback: token=%d identity=%d", len(rt.refStore.byToken), len(rt.refStore.byIdentity))
	}
	if len(rt.refStore.externrefs) != 0 || rt.refStore.externKey != 0 {
		t.Fatalf("externref storage retained after terminal rollback: slots=%d key=%#x", len(rt.refStore.externrefs), rt.refStore.externKey)
	}
}
