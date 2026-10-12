//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"testing"
)

func TestRV64GuestGlobalStoreRetainsDescriptorBeforeParking(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	source, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()

	producerCode := rv64TableStageCompile(t, "mutable-funcref-prepark-producer", rv64MutableFuncrefStoreSelfModule(), false)
	defer producerCode.Close()
	producerModule, err := rt.Module(producerCode)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := rt.Instantiate(context.Background(), producerModule, WithImports(NewImports().Global("env", "cell", source)))
	producerModule.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = producer.Invoke("store"); err != nil {
		t.Fatal("seed source", err)
	}
	descriptor := readGlobalObject(source, ValFuncRef)
	if descriptor == 0 {
		t.Fatal("producer stored a null descriptor")
	}

	consumerCode := rv64TableStageCompile(t, "mutable-funcref-prepark-consumer", rv64MutableFuncrefCopyModule(), false)
	defer consumerCode.Close()
	consumerModule, err := rt.Module(consumerCode)
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerModule, WithImports(NewImports().Global("env", "source", source).Global("env", "target", target)))
	consumerModule.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if retained, err := consumer.retainGuestGlobalStoreDescriptor(hostFuncRefDispatchBit, []uint64{0}, nil); err != nil || retained != nil {
		t.Fatalf("null pre-park retain = %p, %v; want nil, nil", retained, err)
	}
	invalidBefore := producer.referenceLifetime().snapshot()
	if retained, err := consumer.retainGuestGlobalStoreDescriptor(hostFuncRefDispatchBit, []uint64{descriptor + 8}, nil); err == nil || retained != nil {
		if retained != nil {
			retained.releaseResourceRoot()
		}
		t.Fatalf("invalid pre-park retain = %p, %v; want nil, error", retained, err)
	}
	if invalidAfter := producer.referenceLifetime().snapshot(); invalidAfter != invalidBefore {
		t.Fatal("invalid pre-park retain changed descriptor ownership", invalidBefore, invalidAfter)
	}

	before := producer.referenceLifetime().snapshot()
	nativeExecutionMu.Lock()
	retained, retainErr := consumer.retainGuestGlobalStoreDescriptor(hostFuncRefDispatchBit, []uint64{descriptor}, nil)
	if retainErr != nil || retained != producer {
		nativeExecutionMu.Unlock()
		t.Fatalf("pre-park retain = %p, %v; want producer %p", retained, retainErr, producer)
	}
	leaseHeld := true
	defer func() {
		if leaseHeld {
			retained.releaseResourceRoot()
		}
	}()
	retainedSnapshot := producer.referenceLifetime().snapshot()
	if retainedSnapshot.ResourceRoots != before.ResourceRoots+1 {
		nativeExecutionMu.Unlock()
		t.Fatalf("pre-park root count = %d, want %d", retainedSnapshot.ResourceRoots, before.ResourceRoots+1)
	}

	started := make(chan struct{})
	cleared := make(chan error, 1)
	go func() {
		close(started)
		cleared <- source.SetValue(ValueFuncRef(NullFuncRef()))
	}()
	<-started
	nativeExecutionMu.Unlock()
	if err = <-cleared; err != nil {
		t.Fatal("concurrent source clear", err)
	}
	if err = producer.Close(); err != nil {
		t.Fatal("close descriptor owner", err)
	}
	parked := producer.referenceLifetime().snapshot()
	if !parked.LogicallyClosed || !parked.PhysicalResources || parked.ResourceRoots == 0 {
		t.Fatal("pre-park root did not preserve the closed descriptor owner", parked)
	}

	consumer.dispatchGuestGlobalStore(hostFuncRefDispatchBit, []uint64{descriptor}, nil)
	leaseHeld = false
	retained.releaseResourceRoot()
	if got := readGlobalObject(target, ValFuncRef); got != descriptor {
		t.Fatalf("published descriptor = %#x, want %#x", got, descriptor)
	}
	published := producer.referenceLifetime().snapshot()
	if !published.PhysicalResources || published.ResourceRoots != before.ResourceRoots {
		t.Fatal("temporary root did not transfer to one persistent target root", before, published)
	}

	if err = target.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
		t.Fatal("clear target", err)
	}
	retiredSnapshot := producer.referenceLifetime().snapshot()
	if retiredSnapshot.PhysicalResources || retiredSnapshot.ResourceRoots != 0 {
		t.Fatal("target clear did not retire the descriptor owner", retiredSnapshot)
	}
}
