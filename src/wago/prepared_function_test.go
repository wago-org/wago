package wago

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestPreparedFunctionInvokeAndCacheIndependence(t *testing.T) {
	if _, err := (*PreparedFunction)(nil).Invoke(); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("nil prepared invoke error = %v", err)
	}
	in, err := Instantiate(MustCompile(benchAddOneModule()), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	fn, err := in.PrepareFunction("f")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	// Replace every tiny Instance cache slot; the prepared signature must own its
	// result-width metadata rather than aliasing a round-robin cache slot.
	for i := range in.ic {
		in.ic[i] = invokeCache{export: "other", valid: true, resultWide: []bool{true, true}}
	}
	got, err := fn.Invoke(I32(41))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("result = %v, want [42]", got)
	}
	if _, err := fn.Invoke(); err == nil || !strings.Contains(err.Error(), "expects 1") {
		t.Fatalf("arity error = %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := fn.Invoke(I32(1)); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("invoke after close error = %v", err)
	}
}

func TestPreparedFunctionPrivateFastPath(t *testing.T) {
	saved := preparedPrivateEntryEnabled
	savedIsolated := preparedIsolatedEntryEnabled
	savedDirectInt := preparedDirectIntEnabled
	defer func() {
		preparedPrivateEntryEnabled = saved
		preparedIsolatedEntryEnabled = savedIsolated
		preparedDirectIntEnabled = savedDirectInt
	}()
	preparedIsolatedEntryEnabled = true
	preparedDirectIntEnabled = true

	for _, enabled := range []bool{true, false} {
		preparedPrivateEntryEnabled = enabled
		in, err := Instantiate(MustCompile(benchAddOneModule()), InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate enabled=%v: %v", enabled, err)
		}
		fn, err := in.PrepareFunction("f")
		if err != nil {
			t.Fatalf("prepare enabled=%v: %v", enabled, err)
		}
		wantFast := enabled && in.preparedPrivateEligible()
		if fn.privateFast != wantFast {
			t.Fatalf("private fast enabled=%v: got %v, want %v", enabled, fn.privateFast, wantFast)
		}
		wantIsolated := wantFast && in.preparedIsolatedEligible()
		if fn.isolatedFast != wantIsolated {
			t.Fatalf("isolated fast enabled=%v: got %v, want %v", enabled, fn.isolatedFast, wantIsolated)
		}
		wantDirect := wantIsolated && preparedDirectIntSupported && preparedDirectIntSignature(in.c.Funcs[0]) && in.c.directPreparedAt(0)
		if fn.directIntFast != wantDirect {
			t.Fatalf("direct int enabled=%v: got %v, want %v", enabled, fn.directIntFast, wantDirect)
		}
		got, err := fn.Invoke(I32(41))
		if err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
			t.Fatalf("invoke enabled=%v: got %v, err %v", enabled, got, err)
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close enabled=%v: %v", enabled, err)
		}
	}
}

func TestPreparedFunctionDirectIntArgumentsAndTrap(t *testing.T) {
	if !preparedDirectIntSupported {
		t.Log("architecture does not support direct prepared integer entry")
		return
	}
	add64 := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), add64)
	if err != nil {
		t.Fatal(err)
	}
	if !compiled.directPreparedAt(0) {
		t.Fatal("fresh i64 add did not retain packed direct-entry selection")
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal direct-entry module: %v", err)
	}
	var decoded Compiled
	if err := decoded.UnmarshalBinary(blob); err != nil {
		t.Fatalf("unmarshal direct-entry module: %v", err)
	}
	if decoded.directPreparedAt(0) || decoded.InternalEntry[0] != internalEntryOffset(compiled.InternalEntry[0]) {
		t.Fatalf("decoded direct metadata = selected %v, entry %d; want wrapper fallback entry %d",
			decoded.directPreparedAt(0), decoded.InternalEntry[0], internalEntryOffset(compiled.InternalEntry[0]))
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate add64: %v", err)
	}
	fn, err := in.PrepareFunction("add")
	if err != nil {
		t.Fatalf("prepare add64: %v", err)
	}
	if !fn.directIntFast {
		t.Fatal("i64 add did not select direct integer entry")
	}
	got, err := fn.Invoke(0x1_0000_0000, 7)
	if err != nil || len(got) != 1 || got[0] != 0x1_0000_0007 {
		t.Fatalf("direct i64 add = %v, %v", got, err)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("close add64: %v", err)
	}

	div := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("div", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6d, 0x0b}))),
	)
	in, err = Instantiate(MustCompile(div), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate div: %v", err)
	}
	defer in.Close()
	fn, err = in.PrepareFunction("div")
	if err != nil {
		t.Fatalf("prepare div: %v", err)
	}
	if !fn.directIntFast {
		t.Fatal("i32 div did not select direct integer entry")
	}
	if _, err := fn.Invoke(I32(7), I32(0)); err == nil {
		t.Fatal("direct division by zero did not trap")
	}
	got, err = fn.Invoke(I32(8), I32(2))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 4 {
		t.Fatalf("direct i32 div after trap = %v, %v", got, err)
	}
}

func TestPreparedFunctionIsolatedEligibility(t *testing.T) {
	in, err := Instantiate(MustCompile(benchAddOneModule()), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if in.c.boundsMode == BoundsChecksSignalsBased {
		if in.preparedPrivateEligible() || in.preparedIsolatedEligible() {
			t.Fatal("signals-based instance must use the guarded entry")
		}
		return
	}
	if !in.preparedIsolatedEligible() {
		t.Fatalf("plain scalar instance should be isolated: private=%v dir=%v sharedctx=%v sync=%v memory=%v owns=%v globals=%d table=%#x gc=%v imports=%d refs=%v",
			in.preparedPrivateEligible(), in.memoryDir != nil, in.nativeControlShared, in.syncMode, in.memory != nil, in.ownsMem,
			len(in.globalCells), in.tableDescPtr, in.gc != nil, in.c.NumImports, in.c.NeedsFuncRefDescs)
	}

	in.globalCells = []*Global{nil}
	if in.preparedIsolatedEligible() {
		t.Fatal("instance with host-visible globals should not be isolated")
	}
	in.globalCells = nil
	in.tableDescPtr = 1
	if in.preparedIsolatedEligible() {
		t.Fatal("instance with a native table should not be isolated")
	}
	in.tableDescPtr = 0
	in.c.NumImports = 1
	if in.preparedIsolatedEligible() {
		t.Fatal("instance with function imports should not be isolated")
	}
	in.c.NumImports = 0
	in.c.NeedsFuncRefDescs = true
	if in.preparedIsolatedEligible() {
		t.Fatal("instance with function-reference descriptors should not be isolated")
	}
}

func TestPreparedFunctionIsolatedInstancesRunConcurrently(t *testing.T) {
	c := MustCompile(benchAddOneModule())
	if c.boundsMode == BoundsChecksSignalsBased {
		t.Skip("signals-based execution requires the guarded entry")
	}
	instances := make([]*Instance, 2)
	prepared := make([]*PreparedFunction, 2)
	for i := range instances {
		var err error
		instances[i], err = Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate %d: %v", i, err)
		}
		defer instances[i].Close()
		prepared[i], err = instances[i].PrepareFunction("f")
		if err != nil {
			t.Fatalf("prepare %d: %v", i, err)
		}
		if !prepared[i].isolatedFast {
			t.Fatalf("prepared %d did not select isolated entry", i)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(prepared))
	for i := range prepared {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := 0; n < 1_000; n++ {
				got, err := prepared[i].Invoke(I32(int32(n)))
				if err != nil {
					errs <- err
					return
				}
				if len(got) != 1 || AsI32(got[0]) != int32(n+1) {
					errs <- fmt.Errorf("instance %d result %v at %d", i, got, n)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestInvokeScalarUsesIsolatedEntry(t *testing.T) {
	savedInvoke := invokePrivateEntryEnabled
	savedIsolated := preparedIsolatedEntryEnabled
	invokePrivateEntryEnabled = true
	preparedIsolatedEntryEnabled = true
	defer func() {
		invokePrivateEntryEnabled = savedInvoke
		preparedIsolatedEntryEnabled = savedIsolated
	}()

	in, err := Instantiate(MustCompile(benchAddOneModule()), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if !in.preparedIsolatedEligible() {
		if in.c.boundsMode == BoundsChecksSignalsBased {
			t.Skip("signals-based execution requires the guarded entry")
		}
		t.Fatal("plain scalar instance should be eligible for isolated entry")
	}
	if _, err := in.Invoke("f", I32(0)); err != nil {
		t.Fatalf("warm invoke: %v", err)
	}

	type result struct {
		values []uint64
		err    error
	}
	done := make(chan result, 1)
	nativeExecutionMu.Lock()
	go func() {
		values, err := in.Invoke("f", I32(41))
		done <- result{values: values, err: err}
	}()
	select {
	case got := <-done:
		nativeExecutionMu.Unlock()
		if got.err != nil || len(got.values) != 1 || AsI32(got.values[0]) != 42 {
			t.Fatalf("isolated invoke = %v, %v", got.values, got.err)
		}
	case <-time.After(time.Second):
		nativeExecutionMu.Unlock()
		<-done
		t.Fatal("isolated Invoke waited for the process-wide native execution lease")
	}

	// Attachment can make native control shared after this export was cached.
	// The cached scalar shape must then fall back to rebinding under the lease.
	in.nativeControlShared = true
	done = make(chan result, 1)
	nativeExecutionMu.Lock()
	go func() {
		values, err := in.Invoke("f", I32(42))
		done <- result{values: values, err: err}
	}()
	select {
	case got := <-done:
		nativeExecutionMu.Unlock()
		t.Fatalf("shared-control invoke bypassed the execution lease: %v, %v", got.values, got.err)
	case <-time.After(20 * time.Millisecond):
		nativeExecutionMu.Unlock()
	}
	got := <-done
	if got.err != nil || len(got.values) != 1 || AsI32(got.values[0]) != 43 {
		t.Fatalf("shared-control invoke = %v, %v", got.values, got.err)
	}
}

func BenchmarkPreparedInvokeAddOne(b *testing.B) {
	c := benchMustCompile(b, benchAddOneModule())
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("f")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := fn.Invoke(I32(int32(i)))
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}
