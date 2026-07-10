//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestHostCreatedFuncRefGlobalSharesOwnedTokenAndCallableIdentity(t *testing.T) {
	rt := NewRuntime()
	owner, err := rt.NewHostFuncRef(HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = I32(42)
	}), FuncSig{Results: []ValType{ValI32}})
	if err != nil {
		t.Fatalf("NewHostFuncRef: %v", err)
	}
	producerMod, err := rt.Compile(hostFuncRefGlobalProducerModule(t))
	if err != nil {
		t.Fatalf("Compile producer: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerMod, WithImports(Imports{"env.target": owner}))
	if err != nil {
		t.Fatalf("Instantiate producer: %v", err)
	}
	out, err := producer.Call(context.Background(), "get")
	if err != nil || len(out) != 1 || out[0].FuncRef().IsNull() {
		t.Fatalf("owned host token = %v, %v; want one non-null funcref", out, err)
	}
	token := out[0]

	shared, err := rt.NewFuncRefGlobal(token.FuncRef(), true)
	if err != nil {
		t.Fatalf("NewFuncRefGlobal: %v", err)
	}
	if shared.Type != ValFuncRef || !shared.Mutable || shared.owner == nil || shared.owner.store != rt.refStore || shared.owner.instance != nil {
		t.Fatalf("host funcref global metadata = type %s mutable %v owner %#v", shared.Type, shared.Mutable, shared.owner)
	}
	if got, err := shared.GetValue(); err != nil || got != token {
		t.Fatalf("GetValue = %v, %v; want %v", got, err, token)
	}

	importerMod, err := rt.Compile(importedReferenceGlobalModule(wasm.FuncRef, true))
	if err != nil {
		t.Fatalf("Compile importer: %v", err)
	}
	importer, err := rt.Instantiate(context.Background(), importerMod, WithImports(Imports{"env.ref": shared}))
	if err != nil {
		t.Fatalf("Instantiate importer: %v", err)
	}
	got, err := importer.Call(context.Background(), "get")
	if err != nil || len(got) != 1 || got[0] != token {
		t.Fatalf("imported get = %v, %v; want %v", got, err, token)
	}
	if err := shared.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
		t.Fatalf("SetValue(null): %v", err)
	}
	got, err = importer.Call(context.Background(), "get")
	if err != nil || len(got) != 1 || !got[0].FuncRef().IsNull() {
		t.Fatalf("imported get after null = %v, %v; want null", got, err)
	}
	got, err = importer.Call(context.Background(), "set_and_get", token)
	if err != nil || len(got) != 1 || got[0] != token {
		t.Fatalf("shared set_and_get = %v, %v; want %v", got, err, token)
	}

	callerMod, err := rt.Compile(funcrefCallableConsumerModule())
	if err != nil {
		t.Fatalf("Compile caller: %v", err)
	}
	caller, err := rt.Instantiate(context.Background(), callerMod)
	if err != nil {
		t.Fatalf("Instantiate caller: %v", err)
	}
	if got, err := caller.Call(context.Background(), "call", token); err != nil || len(got) != 1 || got[0].I32() != 42 {
		t.Fatalf("call host funcref from global token = %v, %v; want 42", got, err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("Close producer: %v", err)
	}
	if got, err := shared.GetValue(); err != nil || got != token {
		t.Fatalf("GetValue after producer close = %v, %v; want retained %v", got, err, token)
	}
	if got, err := caller.Call(context.Background(), "call", token); err != nil || len(got) != 1 || got[0].I32() != 42 {
		t.Fatalf("call retained host funcref = %v, %v; want 42", got, err)
	}
	if err := shared.Close(); err == nil || !strings.Contains(err.Error(), "live importer") {
		t.Fatalf("Close global with importer error = %v", err)
	}
	if err := owner.Close(); err == nil || !strings.Contains(err.Error(), "live funcref token") {
		t.Fatalf("Close host owner with live token error = %v", err)
	}
	if err := importer.Close(); err != nil {
		t.Fatalf("Close importer: %v", err)
	}
	if err := caller.Close(); err != nil {
		t.Fatalf("Close caller: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("Close host owner: %v", err)
	}
	if got, err := shared.GetValue(); err != nil || got != token {
		t.Fatalf("GetValue after owner close = %v, %v; want retained %v", got, err, token)
	}
	if err := shared.Close(); err != nil {
		t.Fatalf("Close global: %v", err)
	}
}

func TestHostCreatedFuncRefGlobalNullAndOwnerBoundaries(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	nullGlobal, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatalf("NewFuncRefGlobal(null): %v", err)
	}
	defer nullGlobal.Close()
	if got, err := nullGlobal.GetValue(); err != nil || !got.FuncRef().IsNull() {
		t.Fatalf("null GetValue = %v, %v; want null", got, err)
	}
	if err := nullGlobal.SetValue(ValueOf(ValFuncRef, 1)); err == nil || !strings.Contains(err.Error(), "invalid funcref token") {
		t.Fatalf("SetValue(forged) error = %v", err)
	}
	if got, err := nullGlobal.GetValue(); err != nil || !got.FuncRef().IsNull() {
		t.Fatalf("value after forged set = %v, %v; want null", got, err)
	}
	immutable, err := rt.NewFuncRefGlobal(NullFuncRef(), false)
	if err != nil {
		t.Fatalf("NewFuncRefGlobal immutable null: %v", err)
	}
	defer immutable.Close()
	if err := immutable.SetValue(ValueFuncRef(NullFuncRef())); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("immutable SetValue error = %v", err)
	}

	owner, err := rt.NewHostFuncRef(HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = I32(7)
	}), FuncSig{Results: []ValType{ValI32}})
	if err != nil {
		t.Fatalf("NewHostFuncRef: %v", err)
	}
	defer owner.Close()
	producerMod, err := rt.Compile(hostFuncRefGlobalProducerModule(t))
	if err != nil {
		t.Fatalf("Compile producer: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerMod, WithImports(Imports{"env.target": owner}))
	if err != nil {
		t.Fatalf("Instantiate producer: %v", err)
	}
	defer producer.Close()
	out, err := producer.Call(context.Background(), "get")
	if err != nil || len(out) != 1 {
		t.Fatalf("owned get = %v, %v", out, err)
	}
	token := out[0]

	foreign := NewRuntime()
	defer foreign.Close()
	if _, err := foreign.NewFuncRefGlobal(token.FuncRef(), true); err == nil || !strings.Contains(err.Error(), "invalid funcref token") {
		t.Fatalf("cross-runtime constructor error = %v", err)
	}
	forged := ValueOf(ValFuncRef, token.Bits()^0x9e3779b97f4a7c15).FuncRef()
	if _, err := rt.NewFuncRefGlobal(forged, true); err == nil || !strings.Contains(err.Error(), "invalid funcref token") {
		t.Fatalf("forged constructor error = %v", err)
	}

	raw, err := rt.Instantiate(context.Background(), producerMod, WithImports(Imports{"env.target": HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = I32(7)
	})}))
	if err != nil {
		t.Fatalf("Instantiate raw host producer: %v", err)
	}
	defer raw.Close()
	if got, err := raw.Call(context.Background(), "get"); err == nil || !strings.Contains(err.Error(), "invalid funcref result") || got != nil {
		t.Fatalf("raw HostFunc egress = %v, %v; want fail-closed owner rejection", got, err)
	}

	closed := NewRuntime()
	if err := closed.Close(); err != nil {
		t.Fatalf("close constructor runtime: %v", err)
	}
	if _, err := closed.NewFuncRefGlobal(NullFuncRef(), true); err == nil || !strings.Contains(err.Error(), "closed runtime") {
		t.Fatalf("closed-runtime constructor error = %v", err)
	}
}

func TestHostCreatedFuncRefGlobalPersistenceAndLayoutsStayFailClosed(t *testing.T) {
	if got := unsafe.Sizeof(Global{}); got != 40 {
		t.Fatalf("Global size = %d, want 40", got)
	}
	if got := unsafe.Sizeof(Instance{}); got != 776 {
		t.Fatalf("Instance size = %d, want 776", got)
	}
	if got := unsafe.Sizeof(referenceStore{}); got != 88 {
		t.Fatalf("referenceStore size = %d, want 88", got)
	}
	compiled := MustCompile(importedReferenceGlobalModule(wasm.FuncRef, true))
	defer compiled.Close()
	if _, err := compiled.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "reference global metadata") {
		t.Fatalf("MarshalBinary error = %v, want reference-global rejection", err)
	}
	if _, err := Capture(compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "reference global metadata") {
		t.Fatalf("Capture error = %v, want reference-global rejection", err)
	}
}

func hostFuncRefGlobalProducerModule(t *testing.T) []byte {
	t.Helper()
	return watToWasm(t, `(module
		(type $target-type (func (result i32)))
		(import "env" "target" (func $target (type $target-type)))
		(elem declare func $target)
		(func (export "get") (result funcref) (ref.func $target))
	)`)
}
