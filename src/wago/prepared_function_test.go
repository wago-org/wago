package wago

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
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
		in.ic[i] = invokeCache{export: "other", valid: true, slotWide: []bool{true, true}}
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
		directMode := in.preparedMemoryFreeEntryMode()
		wantDirect := enabled && (preparedIsolatedEntryEnabled && directMode == preparedEntryIsolated || preparedDirectIntPrivateSupported && directMode == preparedEntryPrivate) &&
			preparedDirectIntSupported && preparedDirectIntSignature(in.c.Funcs[0]) && in.c.directPreparedAt(0)
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
	got, err := fn.Invoke2(0x1_0000_0000, 7)
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
	compiled, err = Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), div)
	if err != nil {
		t.Fatalf("compile div: %v", err)
	}
	in, err = Instantiate(compiled, InstantiateOptions{})
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
	if _, err := fn.Invoke2(I32(7), I32(0)); err == nil {
		t.Fatal("direct division by zero did not trap")
	}
	got, err = fn.Invoke2(I32(8), I32(2))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 4 {
		t.Fatalf("direct i32 div after trap = %v, %v", got, err)
	}
}

func TestPreparedFunctionFixedArityFourArguments(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I64, wasm.I64, wasm.I64, wasm.I64},
			[]wasm.ValType{wasm.I64},
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x20, 0x01, 0x7c,
			0x20, 0x02, 0x7c,
			0x20, 0x03, 0x7c,
			0x0b,
		}))),
	)
	in, err := Instantiate(MustCompile(module), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("sum")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	got, err := fn.Invoke4(1, 2, 4, 8)
	if err != nil || len(got) != 1 || got[0] != 15 {
		t.Fatalf("fixed four-argument sum = %v, %v; want 15", got, err)
	}
	if _, err := fn.Invoke3(1, 2, 4); err == nil || !strings.Contains(err.Error(), "expects 4") {
		t.Fatalf("fixed arity mismatch error = %v", err)
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
			in.preparedPrivateEligible(), in.memoryDir != nil, in.nativeControlIsShared(), in.syncMode, in.memory != nil, in.ownsMem,
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
	in.markNativeControlShared()
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

func TestNumericMultiResultFastPaths(t *testing.T) {
	savedPrepared := preparedPrivateEntryEnabled
	savedScalar := preparedScalarFastEnabled
	savedInvoke := invokePrivateEntryEnabled
	preparedPrivateEntryEnabled = true
	preparedScalarFastEnabled = true
	invokePrivateEntryEnabled = true
	defer func() {
		preparedPrivateEntryEnabled = savedPrepared
		preparedScalarFastEnabled = savedScalar
		invokePrivateEntryEnabled = savedInvoke
	}()

	compiled, err := Compile(
		NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit),
		hostToWasmI32SignatureModule(2, 2),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	prepared, err := in.PrepareFunction("f")
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.scalarFast || !prepared.privateFast {
		t.Fatalf("numeric multi-result prepared path not selected: scalar=%v private=%v", prepared.scalarFast, prepared.privateFast)
	}
	check := func(name string, got []uint64, err error) {
		t.Helper()
		if err != nil || len(got) != 2 || AsI32(got[0]) != 7 || AsI32(got[1]) != 11 {
			t.Fatalf("%s = %v, %v; want [7 11]", name, got, err)
		}
	}
	values, callErr := in.Invoke("f", I32(7), I32(11))
	check("Invoke", values, callErr)
	values, callErr = prepared.Invoke(I32(7), I32(11))
	check("PreparedFunction.Invoke", values, callErr)

	ic := in.findInvokeCache("f")
	if ic == nil || ic.entryMode == preparedEntryGeneral {
		t.Fatalf("numeric multi-result Invoke cache mode = %v", ic)
	}
	// Native control may become shared after the cache is populated. The public
	// Invoke fast path must honor that dynamic exclusion and take the serialized
	// fallback rather than using its cached isolated entry.
	in.markNativeControlShared()
	type result struct {
		values []uint64
		err    error
	}
	done := make(chan result, 1)
	nativeExecutionMu.Lock()
	go func() {
		values, err := in.Invoke("f", I32(7), I32(11))
		done <- result{values: values, err: err}
	}()
	select {
	case got := <-done:
		nativeExecutionMu.Unlock()
		t.Fatalf("shared-control multi-result invoke bypassed execution lease: %v, %v", got.values, got.err)
	case <-time.After(20 * time.Millisecond):
		nativeExecutionMu.Unlock()
	}
	got := <-done
	check("shared-control Invoke", got.values, got.err)
}

func TestNumericFastPathsPreserveScalarWidths(t *testing.T) {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
	body := make([]byte, 0, len(types)*2+1)
	for i := range types {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	prepared, err := in.PrepareFunction("f")
	if err != nil {
		t.Fatal(err)
	}
	args := []uint64{I32(-7), I64(-9), F32(1.25), F64(-3.5)}
	check := func(name string, got []uint64, err error) {
		t.Helper()
		if err != nil || len(got) != len(args) {
			t.Fatalf("%s = %v, %v", name, got, err)
		}
		for i := range got {
			if got[i] != args[i] {
				t.Fatalf("%s result[%d] = %#x, want %#x", name, i, got[i], args[i])
			}
		}
	}
	values, callErr := in.Invoke("f", args...)
	check("Invoke", values, callErr)
	values, callErr = prepared.Invoke(args...)
	check("PreparedFunction.Invoke", values, callErr)
}

func hostToWasmI32SignatureModule(params, results int) []byte {
	paramTypes := make([]wasm.ValType, params)
	for i := range paramTypes {
		paramTypes[i] = wasm.I32
	}
	resultTypes := make([]wasm.ValType, results)
	body := make([]byte, 0, results*2+1)
	for i := range resultTypes {
		resultTypes[i] = wasm.I32
		if i < params {
			body = append(body, 0x20, byte(i)) // local.get i
		} else {
			body = append(body, 0x41, byte(i+1)) // i32.const i+1
		}
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(paramTypes, resultTypes))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
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
		res, err := fn.Invoke1(I32(int32(i)))
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func TestPreparedIntCallBlockEnvironmentOverride(t *testing.T) {
	t.Setenv("WAGO_PREPARED_INT_CALL_BLOCK", "0")
	if preparedIntCallBlockSetting() {
		t.Fatal("explicit call-block disable was ignored")
	}
	t.Setenv("WAGO_PREPARED_INT_CALL_BLOCK", "1")
	if !preparedIntCallBlockSetting() {
		t.Fatal("explicit call-block enable was ignored")
	}
}

func BenchmarkPreparedInvokeAddOneCallBlock(b *testing.B) {
	before := preparedIntCallBlockEnabled
	defer func() { preparedIntCallBlockEnabled = before }()
	modes := []bool{false, true}
	if os.Getenv("WAGO_CALL_BLOCK_ON_FIRST") == "1" {
		modes[0], modes[1] = modes[1], modes[0]
	}
	for _, enabled := range modes {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			preparedIntCallBlockEnabled = enabled
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
				res, err := fn.Invoke1(I32(int32(i)))
				if err != nil {
					b.Fatal(err)
				}
				benchResultSink = res
			}
		})
	}
}
