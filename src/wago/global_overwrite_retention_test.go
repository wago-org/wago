package wago

import (
	"context"
	"sync"
	"testing"
)

func localFuncrefGlobalProducer(t *testing.T, rt *Runtime, global *Global, value int32) *Instance {
	t.Helper()
	module := mustCompileWat(rt, t, `(module
		(import "env" "global" (global $global (mut funcref)))
		(func $target (result i32) (i32.const `+itoa32(value)+`))
		(elem declare func $target)
		(func (export "seed") (ref.func $target) (global.set $global)))`)
	in, err := rt.Instantiate(context.Background(), module, WithImports(Imports{"env.global": global}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("seed"); err != nil {
		t.Fatal(err)
	}
	return in
}

func globalRetainedCount(g *Global) int {
	g.owner.mu.Lock()
	defer g.owner.mu.Unlock()
	return len(g.owner.retained)
}

func TestGlobalOverwriteNullReleasesProducerImmediately(t *testing.T) {
	rt := NewRuntime()
	global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	producer := localFuncrefGlobalProducer(t, rt, global, 101)
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	assertRetainedInstanceState(t, "global producer before null overwrite", producer, 1, true)
	if got := globalRetainedCount(global); got != 1 {
		t.Fatalf("retained roots before null overwrite = %d, want 1", got)
	}
	if err := global.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
		t.Fatal(err)
	}
	if got := globalRetainedCount(global); got != 0 {
		t.Fatalf("retained roots immediately after null overwrite = %d, want 0", got)
	}
	assertRetainedInstanceState(t, "global producer after null overwrite", producer, 0, false)
	_ = global.Close()
	_ = rt.Close()
}

func TestGlobalOverwriteReplacesProducerAndBoundsRepeatedWrites(t *testing.T) {
	rt := NewRuntime()
	global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	code, err := rt.Compile(funcrefCallableProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	producerA, err := rt.Instantiate(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}
	producerB, err := rt.Instantiate(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}
	outA, err := producerA.Call(context.Background(), "get")
	if err != nil {
		t.Fatal(err)
	}
	outB, err := producerB.Call(context.Background(), "get")
	if err != nil {
		t.Fatal(err)
	}
	tokenA, tokenB := outA[0], outB[0]
	if err := global.SetValue(tokenA); err != nil {
		t.Fatal(err)
	}
	assertRetainedInstanceState(t, "producer A with token and global roots", producerA, 2, true)
	if err := global.SetValue(tokenB); err != nil {
		t.Fatal(err)
	}
	assertRetainedInstanceState(t, "producer A after replacement", producerA, 1, true)
	assertRetainedInstanceState(t, "producer B after replacement", producerB, 2, true)
	for i := 0; i < 100; i++ {
		if err := global.SetValue(tokenB); err != nil {
			t.Fatal(err)
		}
	}
	if got := globalRetainedCount(global); got != 1 {
		t.Fatalf("retained roots after repeated same-token writes = %d, want 1", got)
	}
	assertRetainedInstanceState(t, "producer B after repeated replacement", producerB, 2, true)
	value, err := global.GetValue()
	if err != nil || value.Bits() != tokenB.Bits() {
		t.Fatalf("global value after replacement = %v, %v; want token B", value, err)
	}
	consumerCode, err := rt.Compile(funcrefCallableConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerCode)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := consumer.Call(context.Background(), "call", value); err != nil || len(got) != 1 || got[0].I32() != 42 {
		t.Fatalf("replacement call = %v, %v; want 42", got, err)
	}
	_ = consumer.Close()
	if err := global.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
		t.Fatal(err)
	}
	if got := globalRetainedCount(global); got != 0 {
		t.Fatalf("retained roots after final null = %d, want 0", got)
	}
	assertRetainedInstanceState(t, "producer B after null", producerB, 1, true)
	_ = producerA.Close()
	_ = producerB.Close()
	_ = global.Close()
	_ = rt.Close()
	assertRetainedInstanceState(t, "producer A after store teardown", producerA, 0, false)
	assertRetainedInstanceState(t, "producer B after store teardown", producerB, 0, false)
}

func TestGlobalOverwriteRacesCloseWithoutDoubleRelease(t *testing.T) {
	for i := 0; i < 100; i++ {
		rt := NewRuntime()
		global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
		if err != nil {
			t.Fatal(err)
		}
		code, err := rt.Compile(funcrefCallableProducerModule())
		if err != nil {
			t.Fatal(err)
		}
		producer, err := rt.Instantiate(context.Background(), code)
		if err != nil {
			t.Fatal(err)
		}
		out, err := producer.Call(context.Background(), "get")
		if err != nil {
			t.Fatal(err)
		}
		if err := global.SetValue(out[0]); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_ = global.SetValue(ValueFuncRef(NullFuncRef()))
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = global.Close()
		}()
		close(start)
		wg.Wait()
		if err := global.Close(); err != nil {
			t.Fatal(err)
		}
		_ = producer.Close()
		_ = rt.Close()
		assertRetainedInstanceState(t, "racing producer teardown", producer, 0, false)
	}
}
