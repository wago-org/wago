//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"strings"
	"testing"
)

func TestPrepareFunctionRejectsReexportWithoutPanic(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	producerMod := mustCompileWat(rt, t, `(module (func (export "f") (result i32) (i32.const 7)))`)
	producer, err := rt.Instantiate(context.Background(), producerMod)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	export, err := producer.ExportedFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	relayMod := mustCompileWat(rt, t, `(module
		(import "p" "f" (func $f (result i32)))
		(export "f" (func $f)))`)
	relay, err := rt.Instantiate(context.Background(), relayMod, WithImports(Imports{"p.f": export}))
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("PrepareFunction panicked: %v", recovered)
		}
	}()
	if _, err := relay.PrepareFunction("f"); err == nil || !strings.Contains(err.Error(), "re-exported imports must use Invoke") {
		t.Fatalf("PrepareFunction re-export error = %v", err)
	}
}

func TestPreparedFunctionReferenceBoundaries(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
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
		t.Fatalf("get = %v, %v", out, err)
	}
	token := out[0]
	descriptor, ok := producer.localFuncrefDescriptor(0)
	if !ok || descriptor == token {
		t.Fatalf("descriptor/token = %#x/%#x, want distinct internal/public values", descriptor, token)
	}

	consumerMod := mustCompileWat(rt, t, `(module
		(type $target (func (result i32)))
		(table 1 funcref)
		(global $marker (mut i32) (i32.const 0))
		(func (export "call") (param funcref) (result i32)
			(i32.const 1) (global.set $marker)
			(i32.const 0) (local.get 0) (table.set 0)
			(i32.const 0) (call_indirect (type $target)))
		(export "marker" (global $marker)))`)
	consumer, err := rt.Instantiate(context.Background(), consumerMod)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	call, err := consumer.PrepareFunction("call")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := call.Invoke(token); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("prepared funcref call = %v, %v; want 42", got, err)
	}
	if err := consumer.SetGlobal("marker", 0); err != nil {
		t.Fatal(err)
	}
	if got, err := call.Invoke(1); err == nil || got != nil || !strings.Contains(err.Error(), "invalid funcref token") {
		t.Fatalf("forged prepared call = %v, %v", got, err)
	}
	if marker, err := consumer.Global("marker"); err != nil || marker != 0 {
		t.Fatalf("marker after forged token = %d, %v; want 0", marker, err)
	}

	other := NewRuntime()
	otherMod, err := other.Compile(funcrefCallableProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := other.Instantiate(context.Background(), otherMod)
	if err != nil {
		t.Fatal(err)
	}
	foreignOut, err := foreign.Invoke("get")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := call.Invoke(foreignOut[0]); err == nil || got != nil || !strings.Contains(err.Error(), "invalid funcref token") {
		t.Fatalf("cross-runtime prepared call = %v, %v", got, err)
	}
	_ = foreign.Close()
	_ = other.Close()

	get, err := producer.PrepareFunction("get")
	if err != nil {
		t.Fatal(err)
	}
	preparedOut, err := get.Invoke()
	if err != nil || len(preparedOut) != 1 || preparedOut[0] != token || preparedOut[0] == descriptor {
		t.Fatalf("prepared funcref result = %v, %v; want opaque token %#x", preparedOut, err, token)
	}
	if got, err := call.Invoke(preparedOut[0]); err != nil || AsI32(got[0]) != 42 {
		t.Fatalf("prepared result round trip = %v, %v", got, err)
	}
}

func TestPreparedFunctionExternrefValidation(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod := mustCompileWat(rt, t, `(module
		(func (export "echo") (param externref) (result externref) (local.get 0)))`)
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("echo")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := rt.NewExternRef("value")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := fn.Invoke(ref.token); err != nil || len(got) != 1 || got[0] != ref.token {
		t.Fatalf("prepared externref echo = %v, %v", got, err)
	}
	if got, err := fn.Invoke(1); err == nil || got != nil || !strings.Contains(err.Error(), "invalid externref token") {
		t.Fatalf("forged externref = %v, %v", got, err)
	}
	other := NewRuntime()
	foreign, err := other.NewExternRef("foreign")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := fn.Invoke(foreign.token); err == nil || got != nil || !strings.Contains(err.Error(), "invalid externref token") {
		t.Fatalf("cross-store externref = %v, %v", got, err)
	}
	_ = other.Close()
}

func preparedV128Slots(v V128) []uint64 {
	return []uint64{
		uint64(v[0]) | uint64(v[1])<<8 | uint64(v[2])<<16 | uint64(v[3])<<24 | uint64(v[4])<<32 | uint64(v[5])<<40 | uint64(v[6])<<48 | uint64(v[7])<<56,
		uint64(v[8]) | uint64(v[9])<<8 | uint64(v[10])<<16 | uint64(v[11])<<24 | uint64(v[12])<<32 | uint64(v[13])<<40 | uint64(v[14])<<48 | uint64(v[15])<<56,
	}
}

func TestPreparedFunctionMixedReferenceResultSlots(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	producerMod, _ := rt.Compile(funcrefCallableProducerModule())
	producer, err := rt.Instantiate(context.Background(), producerMod)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	fun, err := producer.Invoke("get")
	if err != nil {
		t.Fatal(err)
	}
	ext, err := rt.NewExternRef("mixed")
	if err != nil {
		t.Fatal(err)
	}
	mod := mustCompileWat(rt, t, `(module
		(func (export "mixed")
			(param funcref externref i32 i64 f32 f64 v128)
			(result i32 funcref v128 externref i64 f32 f64)
			(local.get 2)
			(local.get 0)
			(local.get 6)
			(local.get 1)
			(local.get 3)
			(local.get 4)
			(local.get 5)))`)
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("mixed")
	if err != nil {
		t.Fatal(err)
	}
	vec := V128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	vecSlots := preparedV128Slots(vec)
	args := []uint64{fun[0], ext.token, I32(-7), I64(-9), F32(3.5), F64(-2.25), vecSlots[0], vecSlots[1]}
	got, err := fn.Invoke(args...)
	if err != nil {
		t.Fatal(err)
	}
	want := []uint64{I32(-7), fun[0], vecSlots[0], vecSlots[1], ext.token, I64(-9), F32(3.5), F64(-2.25)}
	if len(got) != len(want) {
		t.Fatalf("mixed result slots = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mixed result slot %d = %#x, want %#x (all=%v)", i, got[i], want[i], got)
		}
	}
}
