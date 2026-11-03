//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"runtime"
	"testing"
	"time"
)

func preparedGCTrapModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x45, 0x04, 0x40, 0, 0x0b, 0x20, 0, 0xfb, 0, 0, 0xfb, 2, 0, 0, 0x0b}))))
}

func publicGCScalarModule(result wasm.ValType, resultCode byte, body []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, resultCode, 0}, wasmtest.FuncType(nil, []wasm.ValType{result}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("value", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func publicGCScalarLifetimeModule(result wasm.ValType, resultCode byte, value []byte) []byte {
	body := append(append([]byte(nil), value...), 0xfb, 0, 0, 0x21, 0, 0x20, 0, 0x22, 1, 0x1a, 0x01, 0x20, 1, 0xfb, 2, 0, 0, 0x0b)
	code := append([]byte{1, 2, 0x63, 0}, body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, resultCode, 0}, wasmtest.FuncType(nil, []wasm.ValType{result}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("value", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))),
	)
}

func TestPublicGCScalarCertificateUsesSafePreparedBoundary(t *testing.T) {
	for _, tc := range []struct {
		name       string
		result     wasm.ValType
		resultCode byte
		value      []byte
		want       uint64
	}{
		{"i32", wasm.I32, 0x7f, []byte{0x41, 42}, 42},
		{"i64", wasm.I64, 0x7e, []byte{0x42, 43}, 43},
		{"f32", wasm.F32, 0x7d, []byte{0x43, 0, 0, 0x80, 0x3f}, 0x3f800000},
		{"f64", wasm.F64, 0x7c, []byte{0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f}, 0x3ff0000000000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			data := publicGCScalarModule(tc.result, tc.resultCode, append(append([]byte(nil), tc.value...), 0xfb, 0, 0, 0xfb, 2, 0, 0, 0x0b))
			if runtime.GOARCH == "amd64" {
				config = config.WithOptimization("gc-lifetime-analyze", true)
				data = publicGCScalarLifetimeModule(tc.result, tc.resultCode, tc.value)
			}
			c, err := Compile(config, data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Invoke("value")
			if err != nil || len(got) != 1 || got[0] != tc.want {
				t.Fatalf("cold result=%v err=%v", got, err)
			}
			fn := in.publicGCScalarCertificate("value")
			if fn == nil || fn.hostPrepared != nil {
				t.Fatalf("certificate=%p prepared=%p bounded=%v lifetime=%v private=%v revocable=%v", fn, fn.hostPrepared, fn != nil && fn.boundedWrapper, fn != nil && fn.gcLifetimeScalar, fn != nil && fn.gcPrivateContext, fn != nil && fn.gcRevocableContext)
			}
			got, err = in.Invoke("value")
			if err != nil || len(got) != 1 || got[0] != tc.want || fn.hostPrepared == nil {
				t.Fatalf("warm result=%v err=%v prepared=%p", got, err, fn.hostPrepared)
			}
			if fn.gcRevocableContext {
				state := fn.tryBeginPrivateGCState()
				if state == nil {
					t.Fatal("warm public certificate did not acquire private gate")
				}
				got, err = fn.invokePrivatePublicGCScalar(state)
				if err != nil || len(got) != 1 || got[0] != tc.want {
					t.Fatalf("private result=%v err=%v", got, err)
				}
				// A changed version must fail the exact cached-context proof. The
				// public call then takes the ordinary route and refreshes the cache.
				in.ensurePluginState().nativeContextVersion.Add(1)
				if state = fn.tryBeginPrivateGCState(); state != nil {
					in.endPrivateGCInvocation(state)
					t.Fatal("stale public context was admitted")
				}
				got, err = in.Invoke("value")
				if err != nil || len(got) != 1 || got[0] != tc.want {
					t.Fatalf("stale fallback result=%v err=%v", got, err)
				}
			}
			if _, err = in.Invoke("value", 1); err == nil {
				t.Fatal("ineligible arity did not use checked fallback")
			}
			in.Close()
			if _, err = in.Invoke("value"); err == nil {
				t.Fatal("closed instance accepted certified call")
			}
		})
	}
}

func TestPublicGCScalarCertificateRevocationFallsBack(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profile builds retain ordinary GC entry")
	}
	if runtime.GOARCH != "amd64" {
		t.Skip("lifetime scalar certificate is currently emitted on amd64")
	}
	for _, kind := range []string{"collection", "guarded-access", "reference-store", "publication"} {
		t.Run(kind, func(t *testing.T) {
			config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-lifetime-analyze", true)
			c, err := Compile(config, publicGCScalarLifetimeModule(wasm.I32, 0x7f, []byte{0x41, 42}))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 128}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i := 0; i < 2; i++ {
				if got, err := in.Invoke("value"); err != nil || len(got) != 1 || got[0] != 42 {
					t.Fatalf("warm %d result=%v err=%v", i, got, err)
				}
			}
			fn := in.publicGCScalarCertificate("value")
			if !fn.gcRevocableContext {
				t.Skip("diagnostic mode retained the ordinary checked route")
			}
			state := fn.tryBeginPrivateGCState()
			if state == nil {
				t.Fatal("warm public entry not admitted")
			}
			in.endPrivateGCInvocation(state)
			switch kind {
			case "collection":
				err = in.CollectGC()
			case "guarded-access":
				unlock := in.lockInstanceNativeStateForHostAccess()
				unlock()
			case "reference-store":
				_, err = in.NewExternRef("publication")
			case "publication":
				in.markNativeControlShared()
			}
			if err != nil {
				t.Fatal(err)
			}
			if state = fn.tryBeginPrivateGCState(); state != nil {
				in.endPrivateGCInvocation(state)
				t.Fatal("revoked public entry was admitted")
			}
			got, err := in.Invoke("value")
			if err != nil || len(got) != 1 || got[0] != 42 {
				t.Fatalf("fallback result=%v err=%v", got, err)
			}
		})
	}
}

func TestPublicGCScalarCertificateRetainsCollectorLeaseAndTrap(t *testing.T) {
	data := publicGCScalarModule(wasm.I32, 0x7f, []byte{0x41, 1, 0x04, 0x40, 0, 0x0b, 0x41, 7, 0xfb, 0, 0, 0xfb, 2, 0, 0, 0x0b})
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	store := newReferenceStore(false)
	defer store.closeRuntime()
	in, err := instantiateCore(c, InstantiateOptions{store: store, GC: GCConfig{NurseryBytes: 256, SurvivorBytes: 256}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err = in.Invoke("value"); err == nil {
		t.Fatal("cold unreachable did not trap")
	}
	if in.publicGCScalarCertificate("value") == nil {
		t.Fatal("trapping cold call did not publish checked certificate")
	}
	if _, err = in.Invoke("value"); err == nil {
		t.Fatal("certified unreachable did not trap")
	} else {
		var trap *rt.TrapError
		if !errors.As(err, &trap) || trap.Code != rt.TrapUnreachable {
			t.Fatalf("wrong trap: %v", err)
		}
	}

	lease := in.lockGCInvocation(newInvocationID())
	done := make(chan error, 1)
	go func() { _, callErr := in.Invoke("value"); done <- callErr }()
	select {
	case callErr := <-done:
		lease.unlock()
		t.Fatalf("certified call bypassed collector lease: %v", callErr)
	case <-time.After(20 * time.Millisecond):
	}
	lease.unlock()
	select {
	case callErr := <-done:
		if callErr == nil {
			t.Fatal("resumed certified call lost trap")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("certified call did not resume")
	}
}

func TestPreparedGCFrameRebindsAfterCollectionAndTrap(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), preparedGCTrapModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, cfg := range []GCConfig{{}, {CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}} {
		store := newReferenceStore(false)
		defer store.closeRuntime()
		in, err := instantiateCore(c, InstantiateOptions{store: store, GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		fn, err := in.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		if !fn.gcPrepared {
			t.Fatal("fixture did not select cached GC entry")
		}
		for i := uint64(1); i <= 30; i++ {
			got, err := fn.Invoke(i)
			if err != nil || len(got) != 1 || got[0] != i {
				t.Fatalf("run=%v err=%v", got, err)
			}
			if fn.hostPrepared == nil {
				t.Fatal("prepared frame was not cached")
			}
			if _, err = fn.Invoke(0); err == nil {
				t.Fatal("unreachable did not trap")
			} else {
				var trap *rt.TrapError
				if !errors.As(err, &trap) || trap.Code != rt.TrapUnreachable {
					t.Fatalf("wrong trap: %v", err)
				}
			}
			if err = in.CollectGC(); err != nil {
				t.Fatal(err)
			}
		}
		// A warm prepared handle must still wait for the collector domain lease.
		lease := in.lockGCInvocation(newInvocationID())
		done := make(chan error, 1)
		go func() { _, err := fn.Invoke(7); done <- err }()
		select {
		case err := <-done:
			lease.unlock()
			t.Fatalf("bypassed collector lease: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
		lease.unlock()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("call did not resume")
		}
	}
}

func TestPreparedGCFrameRestoresInheritedBinding(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), preparedGCTrapModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	restore := bindHostInvocationParent(in, parent)
	defer restore()
	original, count := activeHostInvocationContext(in), activeHostInvocationBindings.Load()
	for _, n := range []uint64{7, 0, 9, 0, 11} {
		got, err := fn.Invoke(n)
		if n == 0 {
			var trap *rt.TrapError
			if !errors.As(err, &trap) || trap.Code != rt.TrapUnreachable {
				t.Fatalf("unexpected trap: %v", err)
			}
		} else if err != nil || len(got) != 1 || got[0] != n {
			t.Fatalf("result=%v error=%v", got, err)
		}
		if now := activeHostInvocationContext(in); now != original {
			t.Fatalf("binding changed: %+v / %+v", now, original)
		}
		if now := activeHostInvocationBindings.Load(); now != count {
			t.Fatalf("binding leak: count=%d want=%d", now, count)
		}
	}
}

func TestPreparedGCScalarWidthAndBits(t *testing.T) {
	for _, tc := range []struct {
		typ    wasm.ValType
		code   byte
		narrow bool
	}{
		{wasm.I32, 0x7f, true}, {wasm.F32, 0x7d, true}, {wasm.I64, 0x7e, false}, {wasm.F64, 0x7c, false},
	} {
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, tc.code, 0}, wasmtest.FuncType([]wasm.ValType{tc.typ}, []wasm.ValType{tc.typ}))),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 0, 0, 0xfb, 2, 0, 0, 0x0b}))))
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		fn, err := in.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		if fn.gcScalar32 != tc.narrow {
			t.Fatalf("type=%x narrow admission=%v", tc.code, fn.gcScalar32)
		}
		for _, bits := range []uint64{0, 1, 0x80000000, 0x7fc12345, 0x7f812345, 0x7f800000, 0xffffffffffffffff, 0x7ff812345678abcd, 0xdeadbeef80000000} {
			want := bits
			if tc.narrow {
				want = uint64(uint32(bits))
			}
			got, err := fn.Invoke(bits)
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("type=%x bits=%x got=%v err=%v want=%x", tc.code, bits, got, err, want)
			}
		}
		if _, err := fn.Invoke(); err == nil {
			t.Fatal("missing argument accepted")
		}
		if _, err := fn.Invoke(1, 2); err == nil {
			t.Fatal("extra argument accepted")
		}
		in.Close()
		if _, err := fn.Invoke(1); err == nil {
			t.Fatal("closed instance accepted GC call")
		}
	}
}

func TestPreparedGCScalarBoundaryPolicyAndGlobals(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 0, 0, 0x24, 0, 0x20, 0, 0x0b}))))
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, disabled := range []bool{false, true} {
		in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{DisableCollection: disabled, VerifyAfterCollect: true, StressNurseryBytes: 128}})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		fn, err := in.WasmFunc("set")
		if err != nil {
			t.Fatal(err)
		}
		if !fn.gcScalar32 || !fn.gcHasRefGlobals || !fn.gcBoundarySafe {
			t.Fatal("fixture did not enter scalar GC boundary")
		}
		for i := uint64(1); i <= 50; i++ {
			before := in.gc.Stats().FullCollections
			out, err := fn.Invoke(i)
			if err != nil || len(out) != 1 || out[0] != i {
				t.Fatalf("invoke: %v %v", out, err)
			}
			if disabled && in.gc.Stats().FullCollections != before+1 {
				t.Fatal("disabled allocation collection lost boundary fallback")
			}
			// Collect without refreshing the instance globals: invocation must already
			// have published the new reference to the collector's exact global slot.
			lease := in.lockGCInvocation(newInvocationID())
			domain := in.lockGCCollector()
			err = in.gc.CollectFull(gc.EmptyRoots{})
			unlockGCCollector(domain)
			lease.unlock()
			if err != nil {
				t.Fatal(err)
			}
			ref := gc.Ref(uint32(readGlobalObject(in.globalCells[0], ValAnyRef)))
			v, err := in.gc.StructGet(ref, 0)
			if err != nil || uint64(v.I32()) != i {
				t.Fatalf("global lost after collection: %v %v", v, err)
			}
		}
	}
}

func TestPreparedGCFrameObservesLateReferenceStore(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), preparedGCTrapModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fn.Invoke(7); err != nil {
		t.Fatal(err)
	}
	if in.refStore != nil {
		t.Fatal("fixture already has a reference store")
	}
	if _, err = in.referenceStoreForBoundary(); err != nil {
		t.Fatal(err)
	}
	lease := in.lockGCInvocation(newInvocationID())
	if !lease.acquired {
		t.Fatal("late store did not provide a domain lease")
	}
	done := make(chan error, 1)
	go func() { _, err := fn.Invoke(9); done <- err }()
	select {
	case err := <-done:
		lease.unlock()
		t.Fatalf("warm handle bypassed newly published domain: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	lease.unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("call failed to resume after domain release")
	}
}

func TestPreparedGCCombinedAdmissionClose(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), preparedGCTrapModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	in.ensurePluginState()
	var lease preparedGCInvocationLease
	if err := in.beginPreparedGCInvocation(&lease); err != nil {
		t.Fatal(err)
	}
	if !lease.combined {
		lease.unlockGCInvocation()
		t.Fatal("standalone GC did not combine admission")
	}
	if err := in.Close(); err != nil {
		lease.unlockGCInvocation()
		t.Fatal(err)
	}
	if in.resourcesClosed || in.invocationState.Load() != instanceInvocationClosed|1 {
		t.Fatal("close discarded active GC resources")
	}
	lease.unlockGCInvocation()
	if !in.resourcesClosed || in.invocationState.Load() != instanceInvocationClosed || lease.state.invocationID != 0 {
		t.Fatal("GC release leaked lifetime or invocation identity")
	}
}

func TestPreparedGCPlainEntryRetainsDisabledCollectionBoundary(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), preparedGCTrapModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, disabled := range []bool{false, true} {
		in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{DisableCollection: disabled, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		fn, err := in.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		if fn.gcScalar32Plain != (!disabled && !codeProfileEnabled) {
			t.Fatal("unexpected plain GC entry selection")
		}
		for i := 0; i < 300; i++ {
			before := in.gc.Stats().FullCollections
			got, err := fn.Invoke(7)
			if err != nil || len(got) != 1 || got[0] != 7 {
				t.Fatalf("call %d: %v %v", i, got, err)
			}
			if disabled && in.gc.Stats().FullCollections != before+1 {
				t.Fatal("disabled allocation policy lost boundary collection")
			}
		}
	}
}

func TestPreparedGCPrivateContextInvalidation(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profile builds retain general context binding")
	}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcStructReductionBytes(false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if !fn.gcPrivateContext {
		t.Fatal("fixture did not certify private GC context")
	}
	call := func() {
		t.Helper()
		out, err := fn.Invoke(1)
		if err != nil || len(out) != 1 || out[0] != 0 {
			t.Fatalf("result=%v error=%v", out, err)
		}
	}
	call()
	call()
	state := in.ensurePluginState()
	version := state.nativeContextVersion.Load()
	call()
	if state.nativeContextVersion.Load() != version || fn.hostContextVersion != version {
		t.Fatal("warm private context was not reused")
	}
	if nativeGCEntryValidationEnabled {
		// Even a cached binding must retain the hardened ABI checks.
		view := in.jm.GCNativeViewPtr()
		in.jm.SetGCNativeViewPtr(0)
		if _, err := fn.Invoke(1); err == nil {
			t.Fatal("warm entry skipped GC view validation")
		}
		in.jm.SetGCNativeViewPtr(view)
		call()
	}
	// Collector backing can change without replacing the instance-view pointer.
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	call()
	// Guarded access invalidates the observation before changing pointer data.
	unlock := in.lockInstanceNativeStateForHostAccess()
	in.jm.SetGCNativeViewPtr(0)
	unlock()
	call()
	if in.jm.GCNativeViewPtr() == 0 {
		t.Fatal("invalidated GC pointer was not rebound")
	}
	version = state.nativeContextVersion.Load()
	// Publication transfers the context to the general native execution lease.
	in.markNativeControlShared()
	call()
	if state.nativeContextVersion.Load() == version {
		t.Fatal("shared context incorrectly reused private binding")
	}
}

func TestPreparedGCPrivateContextRepeatedOOM(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profile builds retain general context binding")
	}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcArrayReductionBytes(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 16, TinyCollectEveryAlloc: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if !fn.gcPrivateContext {
		t.Fatal("fixture did not certify private GC context")
	}
	for i := 0; i < 5; i++ {
		if _, err := fn.Invoke(1); err == nil {
			t.Fatal("oversized array allocation succeeded")
		}
		if in.ensurePluginState().invocationID != 0 {
			t.Fatal("failed call retained invocation ownership")
		}
		if err := in.CollectGC(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPreparedGCRevocableEntryExternalAccess(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profile builds retain ordinary GC entry")
	}
	for _, kind := range []string{"collection", "guarded-access", "reference-store", "publication"} {
		t.Run(kind, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcStructReductionBytes(false, false))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fn.Invoke(1); err != nil {
				t.Fatal(err)
			}
			var lease privateGCTestLease
			if !fn.tryBeginPrivateGCInvocation(&lease) {
				t.Fatal("warm private entry not admitted")
			}
			released := false
			defer func() {
				if !released {
					lease.unlockGCInvocation()
				}
			}()
			done := make(chan error, 1)
			go func() {
				switch kind {
				case "collection":
					done <- in.CollectGC()
				case "guarded-access":
					unlock := in.lockInstanceNativeStateForHostAccess()
					unlock()
					done <- nil
				case "reference-store":
					_, err := in.NewExternRef("publication")
					done <- err
				case "publication":
					in.markNativeControlShared()
					done <- nil
				}
			}()
			deadline := time.Now().Add(5 * time.Second)
			for lease.state.invokeMu.state.Load()&invocationGateRevoked == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if lease.state.invokeMu.state.Load()&invocationGateRevoked == 0 {
				t.Fatal("external access did not revoke entry")
			}
			select {
			case err := <-done:
				t.Fatalf("external access escaped active gate: %v", err)
			default:
			}
			lease.unlockGCInvocation()
			released = true
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("external access did not resume")
			}
			var next privateGCTestLease
			if fn.tryBeginPrivateGCInvocation(&next) {
				next.unlockGCInvocation()
				t.Fatal("revoked entry was admitted")
			}
			out, err := fn.Invoke(7)
			if err != nil || len(out) != 1 || out[0] != 21 {
				t.Fatalf("fallback result=%v error=%v", out, err)
			}
		})
	}
}

func TestPreparedGCRevocableEntryClose(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profile builds retain ordinary GC entry")
	}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcStructReductionBytes(false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fn.Invoke(1); err != nil {
		t.Fatal(err)
	}
	var lease privateGCTestLease
	if !fn.tryBeginPrivateGCInvocation(&lease) {
		t.Fatal("warm private entry not admitted")
	}
	in.Close()
	if in.resourcesClosed {
		lease.unlockGCInvocation()
		t.Fatal("close released active entry resources")
	}
	lease.unlockGCInvocation()
	if !in.resourcesClosed {
		t.Fatal("final owner failed to release closed resources")
	}
	if _, err := fn.Invoke(1); err == nil {
		t.Fatal("closed entry accepted invocation")
	}
}

func TestPreparedGCRevocableEntryRepeatedOOM(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profile builds retain ordinary GC entry")
	}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcArrayReductionBytes(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 16, TinyCollectEveryAlloc: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	_, first := fn.Invoke(1)
	if first == nil {
		t.Fatal("oversized array allocation succeeded")
	}
	for i := 0; i < 5; i++ {
		state := fn.tryBeginPrivateGCState()
		if state == nil {
			t.Fatal("failed ordinary call did not leave reusable private entry")
		}
		if _, err := fn.invokePrivateGCScalar(1, state); err == nil || err.Error() != first.Error() {
			t.Fatalf("private failure=%v, ordinary=%v", err, first)
		}
		if state.invocationID != 0 {
			t.Fatal("failed private call retained invocation identity")
		}
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
}

type privateGCTestLease struct {
	in    *Instance
	state *instancePluginState
}

func (l *privateGCTestLease) unlockGCInvocation() { l.in.endPrivateGCInvocation(l.state) }
func (fn *WasmFunc) tryBeginPrivateGCInvocation(l *privateGCTestLease) bool {
	state := fn.tryBeginPrivateGCState()
	if state == nil {
		return false
	}
	l.in, l.state = fn.in, state
	return true
}
