//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestOwnedHostFuncrefEgressRoundTripAndCloseOrdering(t *testing.T) {
	rt := NewRuntime()
	owner, err := rt.NewHostFuncRef(HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = I32(42)
	}), FuncSig{Results: []ValType{ValI32}})
	if err != nil {
		t.Fatalf("NewHostFuncRef: %v", err)
	}

	producerMod, err := rt.Compile(watToWasm(t, `(module
		(type $target-type (func (result i32)))
		(import "env" "target" (func $target (type $target-type)))
		(table 1 funcref)
		(elem declare func $target)
		(global $held (mut funcref) (ref.null func))
		(func (export "get") (result funcref) (ref.func $target))
		(func (export "hold") (param funcref) (local.get 0) (global.set $held))
		(func (export "held") (result funcref) (global.get $held))
		(func (export "call") (param funcref) (result i32)
			(i32.const 0) (local.get 0) (table.set 0)
			(i32.const 0) (call_indirect (type $target-type)))
	)`))
	if err != nil {
		t.Fatalf("Compile producer: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerMod, WithImports(Imports{"env.target": owner}))
	if err != nil {
		t.Fatalf("Instantiate producer: %v", err)
	}
	if err := owner.Close(); err == nil || !strings.Contains(err.Error(), "live importer") {
		t.Fatalf("Close owner with importer error = %v, want close-order rejection", err)
	}

	out, err := producer.Call(context.Background(), "get")
	if err != nil || len(out) != 1 || out[0].Type() != ValFuncRef || out[0].FuncRef().IsNull() {
		t.Fatalf("owned host get = %v, %v; want one non-null funcref", out, err)
	}
	token := out[0]
	if _, err := producer.Call(context.Background(), "hold", token); err != nil {
		t.Fatalf("hold owned host token: %v", err)
	}
	if held, err := producer.Call(context.Background(), "held"); err != nil || len(held) != 1 || held[0] != token {
		t.Fatalf("held owned host token = %v, %v; want %v", held, err, token)
	}
	if got, err := producer.Call(context.Background(), "call", token); err != nil || len(got) != 1 || got[0].I32() != 42 {
		t.Fatalf("call owned host token = %v, %v; want 42", got, err)
	}
	alias, err := rt.Instantiate(context.Background(), producerMod, WithImports(Imports{"env.target": owner}))
	if err != nil {
		t.Fatalf("Instantiate alias producer: %v", err)
	}
	aliasOut, err := alias.Call(context.Background(), "get")
	if err != nil || len(aliasOut) != 1 || aliasOut[0] != token {
		t.Fatalf("alias owned host identity = %v, %v; want %v", aliasOut, err, token)
	}
	if err := alias.Close(); err != nil {
		t.Fatalf("Close alias producer: %v", err)
	}

	consumerMod, err := rt.Compile(funcrefCallableConsumerModule())
	if err != nil {
		t.Fatalf("Compile consumer: %v", err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerMod)
	if err != nil {
		t.Fatalf("Instantiate consumer: %v", err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("Close producer: %v", err)
	}
	if got, err := consumer.Call(context.Background(), "call", token); err != nil || len(got) != 1 || got[0].I32() != 42 {
		t.Fatalf("call retained owned host token = %v, %v; want 42", got, err)
	}
	if err := owner.Close(); err == nil || !strings.Contains(err.Error(), "live funcref token") {
		t.Fatalf("Close owner with live token error = %v, want token-lifetime rejection", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("Close consumer: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close runtime: %v", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("Close owner after runtime/instances: %v", err)
	}
}

func TestOwnedHostFuncrefRequiresExactRuntimeSignatureAndMetadata(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	owner, err := rt.NewHostFuncRef(HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = I32(7)
	}), FuncSig{Results: []ValType{ValI32}})
	if err != nil {
		t.Fatalf("NewHostFuncRef: %v", err)
	}
	defer owner.Close()

	for name, mod := range map[string]string{
		"parameter mismatch": `(module (import "env" "target" (func (param i32))))`,
		"result mismatch":    `(module (import "env" "target" (func (result i64))))`,
	} {
		t.Run(name, func(t *testing.T) {
			compiled, err := rt.Compile(watToWasm(t, mod))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if _, err := rt.Instantiate(context.Background(), compiled, WithImports(Imports{"env.target": owner})); err == nil || !strings.Contains(err.Error(), "signature") {
				t.Fatalf("Instantiate mismatch error = %v, want exact-signature rejection", err)
			}
		})
	}

	other := NewRuntime()
	defer other.Close()
	foreignMod, err := other.Compile(watToWasm(t, `(module (import "env" "target" (func (result i32))))`))
	if err != nil {
		t.Fatalf("Compile foreign importer: %v", err)
	}
	if _, err := other.Instantiate(context.Background(), foreignMod, WithImports(Imports{"env.target": owner})); err == nil || !strings.Contains(err.Error(), "incompatible reference store") {
		t.Fatalf("cross-runtime owner import error = %v, want store rejection", err)
	}
}

func TestHostFuncrefCallBoundaryUsesOpaqueTokens(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	var callbackBits uint64
	roundTrip := HostFunc(func(_ HostModule, params, results []uint64) {
		callbackBits = params[0]
		results[0] = params[0]
	})
	mod, err := rt.Compile(watToWasm(t, `(module
		(type $rt (func (param funcref) (result funcref)))
		(import "env" "roundtrip" (func $roundtrip (type $rt)))
		(func $target)
		(elem declare func $target)
		(func (export "run") (result funcref)
			(ref.func $target)
			(call $roundtrip))
	)`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.roundtrip": roundTrip}))
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	out, err := in.Invoke("run")
	if err != nil || len(out) != 1 || out[0] == 0 {
		t.Fatalf("run = %v, %v; want one non-null funcref token", out, err)
	}
	if callbackBits != out[0] {
		t.Fatalf("host callback saw %#x, public result token is %#x; want the same opaque token", callbackBits, out[0])
	}
	if descriptor, ok := in.localFuncrefDescriptor(0); ok && callbackBits == descriptor {
		t.Fatalf("host callback observed internal descriptor %#x", descriptor)
	}
}

func TestFuncrefHostReentryControlFrameIsDemandDriven(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	consumerMod, err := rt.Compile(funcrefCallableConsumerModule())
	if err != nil {
		t.Fatalf("Compile funcref ingress caller: %v", err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerMod)
	if err != nil {
		t.Fatalf("Instantiate funcref ingress caller: %v", err)
	}
	defer consumer.Close()
	if !consumer.syncMode || len(consumer.ctrl) != coreruntime.HostCtrlFrameBytes {
		t.Fatalf("funcref ingress control frame = sync %v, %d bytes; want sync and %d bytes", consumer.syncMode, len(consumer.ctrl), coreruntime.HostCtrlFrameBytes)
	}

	fixedMod, err := rt.Compile(benchTable0IndirectModule())
	if err != nil {
		t.Fatalf("Compile fixed table-0 module: %v", err)
	}
	fixed, err := rt.Instantiate(context.Background(), fixedMod)
	if err != nil {
		t.Fatalf("Instantiate fixed table-0 module: %v", err)
	}
	defer fixed.Close()
	if fixed.syncMode || len(fixed.ctrl) != 0 {
		t.Fatalf("fixed table-0 control frame = sync %v, %d bytes; want unchanged async-free path", fixed.syncMode, len(fixed.ctrl))
	}
}

func TestHostFuncrefResultRejectsForgedAndCrossRuntimeTokensBeforeReentry(t *testing.T) {
	foreignRT := NewRuntime()
	foreignMod, err := foreignRT.Compile(funcrefCallableProducerModule())
	if err != nil {
		t.Fatalf("Compile foreign producer: %v", err)
	}
	foreign, err := foreignRT.Instantiate(context.Background(), foreignMod)
	if err != nil {
		t.Fatalf("Instantiate foreign producer: %v", err)
	}
	foreignOut, err := foreign.Invoke("get")
	if err != nil || len(foreignOut) != 1 || foreignOut[0] == 0 {
		t.Fatalf("foreign get = %v, %v", foreignOut, err)
	}
	foreignToken := foreignOut[0]
	defer func() {
		_ = foreign.Close()
		_ = foreignRT.Close()
	}()

	for name, token := range map[string]uint64{"forged": 1, "cross-runtime": foreignToken} {
		t.Run(name, func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			mod, err := rt.Compile(watToWasm(t, `(module
				(import "env" "source" (func $source (result funcref)))
				(global $marker (mut i32) (i32.const 0))
				(func (export "run") (result funcref)
					(call $source)
					(i32.const 1) (global.set $marker))
				(export "marker" (global $marker))
			)`))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.source": HostFunc(func(_ HostModule, _, results []uint64) {
				results[0] = token
			})}))
			if err != nil {
				t.Fatalf("Instantiate: %v", err)
			}
			defer in.Close()
			if got, err := in.Invoke("run"); err == nil || !strings.Contains(err.Error(), "invalid funcref token") || got != nil {
				t.Fatalf("run with %s token = %v, %v; want pre-reentry rejection", name, got, err)
			}
			if marker, err := in.Global("marker"); err != nil || AsI32(marker) != 0 {
				t.Fatalf("marker after %s rejection = %d, %v; want 0", name, AsI32(marker), err)
			}
		})
	}
}

func TestOwnedHostFuncrefRejectsCorruptedDescriptorMetadata(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	owner, err := rt.NewHostFuncRef(HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = I32(1)
	}), FuncSig{Results: []ValType{ValI32}})
	if err != nil {
		t.Fatalf("NewHostFuncRef: %v", err)
	}
	defer owner.Close()
	mod, err := rt.Compile(watToWasm(t, `(module
		(import "env" "target" (func $target (result i32)))
		(elem declare func $target)
		(func (export "get") (result funcref) (ref.func $target))
	)`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.target": owner}))
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	off := coreruntime.TableEntryBytes
	refSlotOff := off + coreruntime.TableEntryRefSlotOffset
	refSlot := binary.LittleEndian.Uint64(in.funcRefDescs[refSlotOff:])
	binary.LittleEndian.PutUint64(in.funcRefDescs[refSlotOff:], refSlot+8)
	if got, err := in.Invoke("get"); err == nil || !strings.Contains(err.Error(), "invalid funcref result") || got != nil {
		t.Fatalf("corrupted owned host get = %v, %v; want fail-closed result", got, err)
	}
	if len(rt.refStore.byToken) != 0 || len(rt.refStore.byDescriptor) != 0 {
		t.Fatal("corrupted host descriptor issued a public token")
	}
}
