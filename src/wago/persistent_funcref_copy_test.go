//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	goruntime "runtime"
	"testing"
)

func localFuncrefTableProducer(rt *Runtime, t *testing.T, table *Table, value int32) *Instance {
	t.Helper()
	mod := mustCompileWat(rt, t, `(module
		(import "env" "table" (table 1 1 funcref))
		(func $target (result i32) (i32.const `+itoa32(value)+`))
		(elem declare func $target)
		(func (export "seed")
			(i32.const 0) (ref.func $target) (table.set 0)))`)
	in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.table": table}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("seed"); err != nil {
		t.Fatal(err)
	}
	return in
}

func itoa32(v int32) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	n := int64(v)
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n != 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func tableCaller(t *testing.T, table *Table) *Instance {
	t.Helper()
	code := MustCompile(importedFuncrefTableCallerModule())
	t.Cleanup(func() { _ = code.Close() })
	in, err := Instantiate(code, Imports{"env.table": table})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func forceReferenceGC() {
	for range 3 {
		goruntime.GC()
	}
}

func clearTableAndFinalizeRoots(t *testing.T, table *Table) {
	t.Helper()
	clearSharedFuncrefSlot(t, table)
}

func TestPersistentFuncrefTableToTableCopiesRetainActualProducer(t *testing.T) {
	for _, copyBody := range []struct {
		name string
		wat  string
	}{
		{"table.copy", `(i32.const 0) (i32.const 0) (i32.const 1) (table.copy $dst $src)`},
		{"table.get-table.set", `(i32.const 0) (i32.const 0) (table.get $src) (table.set $dst)`},
	} {
		t.Run(copyBody.name, func(t *testing.T) {
			rt := NewRuntime()
			source, _ := NewTable(1, 1)
			destination, _ := NewTable(1, 1)
			producer := localFuncrefTableProducer(rt, t, source, 73)
			writerMod := mustCompileWat(rt, t, `(module
				(import "env" "src" (table $src 1 1 funcref))
				(import "env" "dst" (table $dst 1 1 funcref))
				(func (export "copy") `+copyBody.wat+`))`)
			writer, err := rt.Instantiate(context.Background(), writerMod, WithImports(Imports{"env.src": source, "env.dst": destination}))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Invoke("copy"); err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := source.Close(); err != nil {
				t.Fatal(err)
			}
			forceReferenceGC()
			caller := tableCaller(t, destination)
			if got := tableTestCallI32(t, caller, "call"); got != 73 {
				t.Fatalf("destination call = %d, want 73", got)
			}
			if err := caller.Close(); err != nil {
				t.Fatal(err)
			}
			if !producer.hasPhysicalResources() || producer.resourceRefs != 1 {
				t.Fatalf("producer before overwrite: live=%v roots=%d", producer.hasPhysicalResources(), producer.resourceRefs)
			}
			clearTableAndFinalizeRoots(t, destination)
			if producer.hasPhysicalResources() || producer.resourceRefs != 0 {
				t.Fatalf("producer after overwrite: live=%v roots=%d", producer.hasPhysicalResources(), producer.resourceRefs)
			}
			_ = destination.Close()
			_ = rt.Close()
		})
	}
}

func TestPersistentFuncrefTableToGlobalRetainsActualProducer(t *testing.T) {
	rt := NewRuntime()
	source, _ := NewTable(1, 1)
	global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	producer := localFuncrefTableProducer(rt, t, source, 81)
	writerMod := mustCompileWat(rt, t, `(module
		(import "env" "src" (table 1 1 funcref))
		(import "env" "dst" (global $dst (mut funcref)))
		(func (export "copy") (i32.const 0) (table.get 0) (global.set $dst)))`)
	writer, err := rt.Instantiate(context.Background(), writerMod, WithImports(Imports{"env.src": source, "env.dst": global}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = writer.Invoke("copy")
	if err != nil {
		t.Fatal(err)
	}
	_ = producer.Close()
	_ = writer.Close()
	_ = source.Close()
	forceReferenceGC()
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
	if got := tableTestCallI32(t, reader, "call"); got != 81 {
		t.Fatalf("global call = %d, want 81", got)
	}
	_ = reader.Close()
	if err := global.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
		t.Fatal(err)
	}
	scanGlobalAfterOverwrite(t, rt, global)
	if producer.hasPhysicalResources() || producer.resourceRefs != 0 {
		t.Fatalf("producer after global overwrite: live=%v roots=%d", producer.hasPhysicalResources(), producer.resourceRefs)
	}
	_ = global.Close()
	_ = rt.Close()
}

func TestPersistentFuncrefGlobalToGlobalRetainsActualProducer(t *testing.T) {
	rt := NewRuntime()
	g1, _ := rt.NewFuncRefGlobal(NullFuncRef(), true)
	g2, _ := rt.NewFuncRefGlobal(NullFuncRef(), true)
	producerMod, _ := rt.Compile(funcrefCallableProducerModule())
	producer, err := rt.Instantiate(context.Background(), producerMod)
	if err != nil {
		t.Fatal(err)
	}
	token, err := producer.Invoke("get")
	if err != nil {
		t.Fatal(err)
	}
	if err := g1.SetValue(ValueFuncRef(FuncRef{token: token[0]})); err != nil {
		t.Fatal(err)
	}
	writerMod := mustCompileWat(rt, t, `(module
		(import "env" "g1" (global (mut funcref)))
		(import "env" "g2" (global $g2 (mut funcref)))
		(func (export "copy") (global.get 0) (global.set $g2)))`)
	writer, err := rt.Instantiate(context.Background(), writerMod, WithImports(Imports{"env.g1": g1, "env.g2": g2}))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = writer.Invoke("copy")
	_ = producer.Close()
	_ = writer.Close()
	_ = g1.Close()
	forceReferenceGC()
	value, err := g2.GetValue()
	if err != nil || value.Type() != ValFuncRef || value.FuncRef().IsNull() {
		t.Fatalf("G2 value = %v, %v", value, err)
	}
	callerMod, _ := rt.Compile(funcrefCallableConsumerModule())
	caller, _ := rt.Instantiate(context.Background(), callerMod)
	got, err := caller.Call(context.Background(), "call", value)
	if err != nil || got[0].I32() != 42 {
		t.Fatalf("G2 call = %v, %v", got, err)
	}
	_ = caller.Close()
	_ = g2.SetValue(ValueFuncRef(NullFuncRef()))
	scanGlobalAfterOverwrite(t, rt, g2)
	g2.owner.mu.Lock()
	retained := len(g2.owner.retained)
	g2.owner.mu.Unlock()
	if retained != 0 {
		t.Fatalf("G2 retained roots after overwrite = %d, want 0", retained)
	}
	_ = g2.Close()
	_ = rt.Close()
	if producer.hasPhysicalResources() || producer.resourceRefs != 0 {
		t.Fatalf("producer after G2/root token teardown: live=%v roots=%d", producer.hasPhysicalResources(), producer.resourceRefs)
	}
}

func assertRetainedInstanceState(t *testing.T, name string, in *Instance, wantRefs int, wantPhysical bool) {
	t.Helper()
	in.lifeMu.Lock()
	refs, physical := in.resourceRefs, !in.resourcesClosed
	in.lifeMu.Unlock()
	if refs != int32(wantRefs) || physical != wantPhysical {
		t.Fatalf("%s: roots=%d physical=%v, want roots=%d physical=%v", name, refs, physical, wantRefs, wantPhysical)
	}
}

func instantiateTableCopyWriter(t *testing.T, rt *Runtime, source, destination *Table, body string) *Instance {
	t.Helper()
	wat := `(module
		(import "env" "src" (table $src 1 1 funcref))
		(import "env" "dst" (table $dst 1 1 funcref))
		(func (export "copy") ` + body + `))`
	var (
		writer *Instance
		err    error
	)
	if rt != nil {
		module := mustCompileWat(rt, t, wat)
		writer, err = rt.Instantiate(context.Background(), module, WithImports(Imports{"env.src": source, "env.dst": destination}))
	} else {
		compiled := MustCompile(watToWasmCA(t, wat))
		t.Cleanup(func() { _ = compiled.Close() })
		writer, err = Instantiate(compiled, Imports{"env.src": source, "env.dst": destination})
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Invoke("copy"); err != nil {
		t.Fatal(err)
	}
	return writer
}

func localFuncrefTableProducerStoreless(t *testing.T, table *Table, value int32) *Instance {
	t.Helper()
	compiled := MustCompile(watToWasmCA(t, `(module
		(import "env" "table" (table 1 1 funcref))
		(func $target (result i32) (i32.const `+itoa32(value)+`))
		(elem declare func $target)
		(func (export "seed") (i32.const 0) (ref.func $target) (table.set 0)))`))
	t.Cleanup(func() { _ = compiled.Close() })
	in, err := Instantiate(compiled, Imports{"env.table": table})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("seed"); err != nil {
		t.Fatal(err)
	}
	return in
}

func TestCrossRuntimePersistentFuncrefTableCopiesRetainProxy(t *testing.T) {
	for _, copyBody := range []struct {
		name string
		wat  string
	}{
		{"table.copy", `(i32.const 0) (i32.const 0) (i32.const 1) (table.copy $dst $src)`},
		{"table.get-table.set", `(i32.const 0) (i32.const 0) (table.get $src) (table.set $dst)`},
	} {
		t.Run(copyBody.name, func(t *testing.T) {
			rtA, rtB := NewRuntime(), NewRuntime()
			source, _ := NewTable(1, 1)
			destination, _ := NewTable(1, 1)
			producer := localFuncrefTableProducer(rtA, t, source, 91)
			writer := instantiateTableCopyWriter(t, rtB, source, destination, copyBody.wat)

			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			assertRetainedInstanceState(t, "cross-runtime writer before producer close", writer, 2, true)
			destination.mu.Lock()
			retained := len(destination.retained)
			destination.mu.Unlock()
			if retained != 1 {
				t.Fatalf("destination retained roots = %d, want one bounded proxy", retained)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			assertRetainedInstanceState(t, "cross-runtime writer after precise resolution", writer, 0, false)
			assertRetainedInstanceState(t, "cross-runtime producer before source close", producer, 2, true)
			if err := source.Close(); err != nil {
				t.Fatalf("source Close after precise transfer: %v", err)
			}
			assertRetainedInstanceState(t, "cross-runtime producer after source close", producer, 1, true)
			forceReferenceGC()
			caller := tableCaller(t, destination)
			if got := tableTestCallI32(t, caller, "call"); got != 91 {
				t.Fatalf("destination call = %d, want 91", got)
			}
			if err := caller.Close(); err != nil {
				t.Fatal(err)
			}
			destination.mu.Lock()
			retained = len(destination.retained)
			destination.mu.Unlock()
			if retained != 1 {
				t.Fatalf("reader finalization grew proxy roots to %d, want 1", retained)
			}
			clearTableAndFinalizeRoots(t, destination)
			assertRetainedInstanceState(t, "writer after overwrite", writer, 0, false)
			assertRetainedInstanceState(t, "producer after overwrite", producer, 0, false)
			_ = destination.Close()
			_ = rtA.Close()
			_ = rtB.Close()
		})
	}
}

func TestStorelessPersistentFuncrefTableCopyRetainsProxy(t *testing.T) {
	source, _ := NewTable(1, 1)
	destination, _ := NewTable(1, 1)
	producer := localFuncrefTableProducerStoreless(t, source, 92)
	writer := instantiateTableCopyWriter(t, nil, source, destination, `(i32.const 0) (i32.const 0) (table.get $src) (table.set $dst)`)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if writer.refStore != nil {
		t.Fatal("storeless writer unexpectedly allocated a reference store")
	}
	assertRetainedInstanceState(t, "storeless writer before producer close", writer, 2, true)
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	assertRetainedInstanceState(t, "storeless writer after precise resolution", writer, 0, false)
	assertRetainedInstanceState(t, "storeless producer before source close", producer, 2, true)
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	assertRetainedInstanceState(t, "storeless producer after source close", producer, 1, true)
	forceReferenceGC()
	caller := tableCaller(t, destination)
	if got := tableTestCallI32(t, caller, "call"); got != 92 {
		t.Fatalf("destination call = %d, want 92", got)
	}
	_ = caller.Close()
	clearTableAndFinalizeRoots(t, destination)
	assertRetainedInstanceState(t, "storeless writer after overwrite", writer, 0, false)
	assertRetainedInstanceState(t, "storeless producer after overwrite", producer, 0, false)
	_ = destination.Close()
}

func TestCrossRuntimePersistentFuncrefTableToGlobalRetainsProxy(t *testing.T) {
	rtA, rtB := NewRuntime(), NewRuntime()
	source, _ := NewTable(1, 1)
	destination, err := rtB.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	producer := localFuncrefTableProducer(rtA, t, source, 93)
	writerMod := mustCompileWat(rtB, t, `(module
		(import "env" "src" (table 1 1 funcref))
		(import "env" "dst" (global $dst (mut funcref)))
		(func (export "copy") (i32.const 0) (table.get 0) (global.set $dst)))`)
	writer, err := rtB.Instantiate(context.Background(), writerMod, WithImports(Imports{"env.src": source, "env.dst": destination}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Invoke("copy"); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	assertRetainedInstanceState(t, "table-to-global writer before producer close", writer, 2, true)
	_ = producer.Close()
	assertRetainedInstanceState(t, "table-to-global writer after precise resolution", writer, 0, false)
	assertRetainedInstanceState(t, "table-to-global producer before source close", producer, 2, true)
	if err := source.Close(); err != nil {
		t.Fatalf("source Close after precise transfer: %v", err)
	}
	assertRetainedInstanceState(t, "table-to-global producer after source close", producer, 1, true)
	forceReferenceGC()
	readerMod := mustCompileWat(rtB, t, `(module
		(type $target (func (result i32)))
		(import "env" "g" (global (mut funcref)))
		(table 1 funcref)
		(func (export "call") (result i32)
			(i32.const 0) (global.get 0) (table.set 0)
			(i32.const 0) (call_indirect (type $target))))`)
	reader, err := rtB.Instantiate(context.Background(), readerMod, WithImports(Imports{"env.g": destination}))
	if err != nil {
		t.Fatal(err)
	}
	if got := tableTestCallI32(t, reader, "call"); got != 93 {
		t.Fatalf("global call = %d, want 93", got)
	}
	_ = reader.Close()
	if err := destination.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
		t.Fatal(err)
	}
	scanGlobalAfterOverwrite(t, rtB, destination)
	assertRetainedInstanceState(t, "table-to-global writer after overwrite", writer, 0, false)
	assertRetainedInstanceState(t, "table-to-global producer after overwrite", producer, 0, false)
	_ = destination.Close()
	_ = rtA.Close()
	_ = rtB.Close()
}

func TestPersistentFuncrefMixedSourceChainRetainsProxy(t *testing.T) {
	rtA, rtB := NewRuntime(), NewRuntime()
	source, _ := NewTable(1, 1)
	intermediate, _ := NewTable(1, 1)
	destination, _ := NewTable(1, 1)
	producer := localFuncrefTableProducer(rtA, t, source, 94)
	writerA := instantiateTableCopyWriter(t, rtA, source, intermediate, `(i32.const 0) (i32.const 0) (table.get $src) (table.set $dst)`)
	writerB := instantiateTableCopyWriter(t, rtB, intermediate, destination, `(i32.const 0) (i32.const 0) (table.get $src) (table.set $dst)`)
	_ = writerA.Close()
	_ = writerB.Close()
	_ = producer.Close()
	assertRetainedInstanceState(t, "chain writer A", writerA, 0, false)
	assertRetainedInstanceState(t, "chain writer B", writerB, 0, false)
	assertRetainedInstanceState(t, "chain producer", producer, 3, true)
	forceReferenceGC()
	caller := tableCaller(t, destination)
	if got := tableTestCallI32(t, caller, "call"); got != 94 {
		t.Fatalf("chained destination call = %d, want 94", got)
	}
	_ = caller.Close()
	clearTableAndFinalizeRoots(t, destination)
	assertRetainedInstanceState(t, "chain writer B after overwrite", writerB, 0, false)
	if err := intermediate.Close(); err != nil {
		t.Fatal(err)
	}
	assertRetainedInstanceState(t, "chain writer A after intermediate close", writerA, 0, false)
	assertRetainedInstanceState(t, "chain producer after intermediate close", producer, 1, true)
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	assertRetainedInstanceState(t, "chain producer after source close", producer, 0, false)
	_ = destination.Close()
	_ = rtA.Close()
	_ = rtB.Close()
}

func TestPersistentFuncrefPublicIngressAndCrossInstanceResult(t *testing.T) {
	rt := NewRuntime()
	destination, _ := NewTable(1, 1)
	producerMod, _ := rt.Compile(funcrefCallableProducerModule())
	producer, _ := rt.Instantiate(context.Background(), producerMod)
	token, err := producer.Invoke("get")
	if err != nil {
		t.Fatal(err)
	}
	relayMod := mustCompileWat(rt, t, `(module (func (export "echo") (param funcref) (result funcref) (local.get 0)))`)
	relay, _ := rt.Instantiate(context.Background(), relayMod)
	relayToken, err := relay.Invoke("echo", token[0])
	if err != nil || relayToken[0] != token[0] {
		t.Fatalf("cross-instance result = %v, %v", relayToken, err)
	}
	writerMod := mustCompileWat(rt, t, `(module
		(import "env" "dst" (table 1 1 funcref))
		(func (export "store") (param funcref) (i32.const 0) (local.get 0) (table.set 0)))`)
	writer, _ := rt.Instantiate(context.Background(), writerMod, WithImports(Imports{"env.dst": destination}))
	if _, err := writer.Invoke("store", relayToken[0]); err != nil {
		t.Fatal(err)
	}
	_ = producer.Close()
	_ = relay.Close()
	_ = writer.Close()
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if len(rt.refStore.byToken) != 0 {
		t.Fatal("runtime token store remained after all runtime instances quiesced")
	}
	forceReferenceGC()
	caller := tableCaller(t, destination)
	if got := tableTestCallI32(t, caller, "call"); got != 42 {
		t.Fatalf("call after token teardown = %d, want 42", got)
	}
	_ = caller.Close()
	clearTableAndFinalizeRoots(t, destination)
	if producer.hasPhysicalResources() || producer.resourceRefs != 0 {
		t.Fatalf("producer after public-ingress overwrite: live=%v roots=%d", producer.hasPhysicalResources(), producer.resourceRefs)
	}
	_ = destination.Close()
}
