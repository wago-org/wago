//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"strings"
	"testing"
)

func TestReferenceTokensWaitForClosingInvocationQuiescence(t *testing.T) {
	rt := NewRuntime()
	producerMod, err := rt.Compile(funcrefCallableProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	producer, err := rt.Instantiate(context.Background(), producerMod)
	if err != nil {
		t.Fatal(err)
	}
	out, err := producer.Invoke("get")
	if err != nil || len(out) != 1 || out[0] == 0 {
		t.Fatalf("producer get = %v, %v", out, err)
	}
	token := out[0]
	entered := make(chan struct{})
	resume := make(chan struct{})
	writerMod := mustCompileWat(rt, t, `(module
		(type $target (func (result i32)))
		(import "env" "block" (func $block))
		(table 1 funcref)
		(func (export "use") (param funcref) (result i32)
			(call $block)
			(i32.const 0) (local.get 0) (table.set 0)
			(i32.const 0) (call_indirect (type $target))))`)
	writer, err := rt.Instantiate(context.Background(), writerMod, WithImports(Imports{
		"env.block": HostFunc(func(HostModule, []uint64, []uint64) {
			close(entered)
			<-resume
		}),
	}))
	if err != nil {
		t.Fatal(err)
	}
	callDone := make(chan error, 1)
	go func() {
		result, err := writer.Invoke("use", token)
		if err == nil && (len(result) != 1 || AsI32(result[0]) != 42) {
			err = &unexpectedReferenceResult{result: result}
		}
		callDone <- err
	}()
	<-entered
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	rt.refStore.mu.Lock()
	writerState := rt.refStore.instances[writer]
	tokens := len(rt.refStore.byToken)
	rt.refStore.mu.Unlock()
	if writerState == nil || !writerState.closeAccounted || writerState.quiesced || writerState.resourcesReleased {
		t.Fatalf("writer store state before resume = %+v", writerState)
	}
	if tokens != 1 {
		t.Fatalf("token entries before quiescence = %d, want 1", tokens)
	}
	if !producer.hasPhysicalResources() || producer.resourceRefs == 0 {
		t.Fatalf("producer released before writer quiescence: live=%v roots=%d", producer.hasPhysicalResources(), producer.resourceRefs)
	}

	close(resume)
	if err := <-callDone; err != nil && !strings.Contains(err.Error(), "interrupt") {
		t.Fatalf("resumed descriptor use = %v; want result 42 or caller-close interruption", err)
	}
	rt.refStore.mu.Lock()
	remainingInstances := len(rt.refStore.instances)
	remainingTokens := len(rt.refStore.byToken)
	rt.refStore.mu.Unlock()
	if remainingInstances != 0 || remainingTokens != 0 {
		t.Fatalf("store after quiescence: instances=%d tokens=%d", remainingInstances, remainingTokens)
	}
	if producer.hasPhysicalResources() || producer.resourceRefs != 0 {
		t.Fatalf("producer after token teardown: live=%v roots=%d", producer.hasPhysicalResources(), producer.resourceRefs)
	}
}

type unexpectedReferenceResult struct{ result []uint64 }

func (e *unexpectedReferenceResult) Error() string { return "unexpected resumed funcref result" }
