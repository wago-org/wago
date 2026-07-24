//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"strings"
	"sync"
	"testing"
)

func TestGlobalSynchronizationScalarHostVersusGuest(t *testing.T) {
	rt := NewRuntime()
	global := NewGlobalI64(0x1111111111111111, true)
	module := mustCompileWat(rt, t, `(module
		(import "env" "g" (global $g (mut i64)))
		(func (export "read") (result i64) (global.get $g))
		(func (export "write") (param i64) (local.get 0) (global.set $g)))`)
	in, err := rt.Instantiate(context.Background(), module, WithImports(Imports{"env.g": global}))
	if err != nil {
		t.Fatal(err)
	}
	const a = uint64(0x1111111111111111)
	const b = uint64(0xeeeeeeeeeeeeeeee)
	start := make(chan struct{})
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			value := a
			if i&1 != 0 {
				value = b
			}
			if _, err := in.Invoke("write", value); err != nil {
				errCh <- err
				return
			}
			out, err := in.Invoke("read")
			if err != nil {
				errCh <- err
				return
			}
			if out[0] != a && out[0] != b {
				errCh <- &unexpectedGlobalValue{kind: "scalar", bits: out[0]}
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			value := a
			if i&1 != 0 {
				value = b
			}
			if err := global.Set(value); err != nil {
				errCh <- err
				return
			}
			got := global.Get()
			if got != a && got != b {
				errCh <- &unexpectedGlobalValue{kind: "scalar", bits: got}
				return
			}
		}
	}()
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	if _, err := in.Invoke("write", a); err != nil {
		t.Fatal(err)
	}
	if got := global.Get(); got != a {
		t.Fatalf("host read after guest write = %#x, want %#x", got, a)
	}
	if err := global.Set(b); err != nil {
		t.Fatal(err)
	}
	if out, err := in.Invoke("read"); err != nil || out[0] != b {
		t.Fatalf("guest read after host write = %#x, %v; want %#x", out, err, b)
	}
	_ = in.Close()
	_ = global.Close()
	_ = rt.Close()
}

type unexpectedGlobalValue struct {
	kind string
	bits uint64
}

func (e *unexpectedGlobalValue) Error() string {
	return "unexpected " + e.kind + " global value"
}

func synchronizationV128Slots(v V128) []uint64 {
	return []uint64{binary.LittleEndian.Uint64(v[:8]), binary.LittleEndian.Uint64(v[8:])}
}

func synchronizationV128FromSlots(slots []uint64) V128 {
	var v V128
	binary.LittleEndian.PutUint64(v[:8], slots[0])
	binary.LittleEndian.PutUint64(v[8:], slots[1])
	return v
}

func TestGlobalSynchronizationV128HostVersusGuestDoesNotTear(t *testing.T) {
	rt := NewRuntime()
	var a, b V128
	for i := range a {
		a[i] = 0xaa
		b[i] = 0x55
	}
	global := NewGlobalV128(a, true)
	module := mustCompileWat(rt, t, `(module
		(import "env" "g" (global $g (mut v128)))
		(func (export "read") (result v128) (global.get $g))
		(func (export "write") (param v128) (local.get 0) (global.set $g)))`)
	in, err := rt.Instantiate(context.Background(), module, WithImports(Imports{"env.g": global}))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errCh := make(chan string, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			value := a
			if i&1 != 0 {
				value = b
			}
			if _, err := in.Invoke("write", synchronizationV128Slots(value)...); err != nil {
				errCh <- err.Error()
				return
			}
			out, err := in.Invoke("read")
			if err != nil {
				errCh <- err.Error()
				return
			}
			got := synchronizationV128FromSlots(out)
			if got != a && got != b {
				errCh <- "guest observed torn v128"
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			value := a
			if i&1 != 0 {
				value = b
			}
			if err := global.SetV128(value); err != nil {
				errCh <- err.Error()
				return
			}
			got := global.GetV128()
			if got != a && got != b {
				errCh <- "host observed torn v128"
				return
			}
		}
	}()
	close(start)
	wg.Wait()
	close(errCh)
	for message := range errCh {
		t.Fatal(message)
	}
	_ = in.Close()
	_ = global.Close()
	_ = rt.Close()
}

func funcrefValueProducer(t *testing.T, rt *Runtime, value int32) (*Instance, Value) {
	t.Helper()
	module := mustCompileWat(rt, t, `(module
		(func $target (result i32) (i32.const `+itoa32(value)+`))
		(elem declare func $target)
		(func (export "get") (result funcref) (ref.func $target)))`)
	in, err := rt.Instantiate(context.Background(), module)
	if err != nil {
		t.Fatal(err)
	}
	out, err := in.Call(context.Background(), "get")
	if err != nil {
		t.Fatal(err)
	}
	return in, out[0]
}

func TestGlobalSynchronizationFuncrefReplacementVersusGuestCall(t *testing.T) {
	rt := NewRuntime()
	global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	producerA, tokenA := funcrefValueProducer(t, rt, 111)
	producerB, tokenB := funcrefValueProducer(t, rt, 222)
	readerMod := mustCompileWat(rt, t, `(module
		(type $target (func (result i32)))
		(import "env" "g" (global (mut funcref)))
		(table 1 funcref)
		(func (export "call") (result i32)
			(i32.const 0) (global.get 0) (table.set 0)
			(i32.const 0) (call_indirect (type $target))))`)
	reader, err := rt.Instantiate(context.Background(), readerMod, WithImports(Imports{"env.g": global}))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			out, err := reader.Invoke("call")
			if err != nil {
				if !strings.Contains(err.Error(), "uninitialized") && !strings.Contains(err.Error(), "null") && !strings.Contains(err.Error(), "indirect call out of bounds") {
					errCh <- err
					return
				}
				continue
			}
			if out[0] != I32(111) && out[0] != I32(222) {
				errCh <- &unexpectedGlobalValue{kind: "funcref result", bits: out[0]}
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		values := []Value{tokenA, tokenB, ValueFuncRef(NullFuncRef())}
		for i := 0; i < 1000; i++ {
			if err := global.SetValue(values[i%len(values)]); err != nil {
				errCh <- err
				return
			}
		}
	}()
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	if got := globalRetainedCount(global); got > 1 {
		t.Fatalf("funcref replacement retained %d roots, want at most one", got)
	}
	_ = reader.Close()
	_ = global.SetValue(ValueFuncRef(NullFuncRef()))
	_ = producerA.Close()
	_ = producerB.Close()
	_ = global.Close()
	_ = rt.Close()
	assertRetainedInstanceState(t, "funcref producer A teardown", producerA, 0, false)
	assertRetainedInstanceState(t, "funcref producer B teardown", producerB, 0, false)
}

func TestGlobalSynchronizationGuestHostAndClose(t *testing.T) {
	for i := 0; i < 100; i++ {
		rt := NewRuntime()
		global := NewGlobalI64(0, true)
		module := mustCompileWat(rt, t, `(module
			(import "env" "g" (global $g (mut i64)))
			(func (export "step") (result i64)
				(global.set $g (i64.add (global.get $g) (i64.const 1)))
				(global.get $g)))`)
		in, err := rt.Instantiate(context.Background(), module, WithImports(Imports{"env.g": global}))
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); <-start; _, _ = in.Invoke("step") }()
		go func() { defer wg.Done(); <-start; _ = global.Set(uint64(i)); _ = global.Get() }()
		go func() { defer wg.Done(); <-start; _ = in.Close() }()
		close(start)
		wg.Wait()
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
		if err := global.Close(); err != nil {
			t.Fatal(err)
		}
		_ = rt.Close()
	}
}
