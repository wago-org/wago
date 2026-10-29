//go:build wago_profile

package wago

import (
	"context"
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"

	profilereport "github.com/wago-org/wago/profile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestProfileBoundaryCrossInstanceReentry(t *testing.T) {
	session := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
	defer session.Close()
	compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(session), benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	var a, b *Instance
	host := func(next **Instance) slotHostFunc {
		return func(mod HostModule, args, results []uint64) {
			if args[0] == 0 {
				results[0] = 10
				return
			}
			out, err := (*next).InvokeFromHost(context.Background(), mod, "g", args[0]-1)
			if err != nil {
				panic(err)
			}
			results[0] = out[0] + 1
		}
	}
	a, err = Instantiate(compiled, testImports("env.f", host(&b)))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err = Instantiate(compiled, testImports("env.f", host(&a)))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	got, err := a.Invoke("g", 2)
	if err != nil || len(got) != 1 || got[0] != 12 {
		t.Fatalf("result %v, %v", got, err)
	}
	spans := session.Spans()
	report, err := profilereport.AnalyzeTimeline(spans, session.Status())
	if err != nil || !report.Complete {
		t.Fatalf("timeline %v, %v", report, err)
	}
	kinds := map[string]int{}
	instances := map[uint64]bool{}
	var invocation uint64
	for _, s := range spans {
		kinds[s.Kind]++
		instances[s.InstanceID] = true
		if s.InvocationID == 0 || s.InstanceID == 0 || s.Outcome != "return" {
			t.Fatalf("invalid span %+v", s)
		}
		if invocation == 0 {
			invocation = s.InvocationID
		}
		if invocation != s.InvocationID {
			t.Fatalf("invocation changed: %+v", s)
		}
	}
	if len(instances) != 2 || kinds["guest-activation"] != 3 || kinds["host-callback"] != 3 || kinds["guest-invocation"] != 3 {
		t.Fatal(kinds, instances, spans)
	}
	for i, span := range spans {
		want := uint64(0)
		if i > 0 {
			want = spans[i-1].ID
		}
		if span.ParentID != want {
			t.Fatalf("broken re-entry ancestry at %d: parent %d want %d", i, span.ParentID, want)
		}
	}
	for _, in := range []*Instance{a, b} {
		if activeHostInvocationContext(in).traceSession != nil {
			t.Fatal("trace context escaped invocation")
		}
	}
}

func TestProfileBoundaryPanicAndExitComplete(t *testing.T) {
	cases := []struct {
		name, outcome string
		value         any
	}{{"panic", "panic", "host panic"}, {"exit", "host-exit", HostExit{Code: 7}}, {"cancel", "cancelled", HostTrap{Err: context.Canceled}}}
	if os.Getenv("WAGO_PROFILE_NIL_PANIC") == "1" {
		cases = append(cases, struct {
			name, outcome string
			value         any
		}{"nil-panic", "panic", nil})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
			defer session.Close()
			compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(session), benchReturningImportModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			fail := true
			in, err := Instantiate(compiled, testImports("env.f", slotHostFunc(func(_ HostModule, args, results []uint64) {
				if fail {
					panic(tc.value)
				}
				results[0] = args[0] + 1
			})))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			var got []uint64
			var recovered any
			panicked := false
			func() {
				returned := false
				defer func() {
					recovered = recover()
					panicked = !returned
				}()
				got, err = in.Invoke("g", 1)
				returned = true
			}()
			wantPanic := tc.outcome == "panic"
			if panicked != wantPanic || wantPanic && recovered != tc.value {
				t.Fatalf("panic = (%v, %#v), want (%v, %#v)", panicked, recovered, wantPanic, tc.value)
			}
			if wantPanic && got != nil {
				t.Fatalf("guest continued after host panic: %v", got)
			}
			spans := session.Spans()
			if len(spans) != 3 {
				t.Fatal(spans)
			}
			for _, s := range spans {
				if s.End == 0 || s.Outcome != tc.outcome {
					t.Fatalf("unfinished/wrong outcome %+v, err %v", s, err)
				}
			}
			if activeHostInvocationContext(in).traceSession != nil {
				t.Fatal("trace context leaked")
			}
			state := in.ensurePluginState()
			if state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0 || in.invocationState.Load()&instanceInvocationCount != 0 {
				t.Fatalf("invocation authority leaked: gate=%#x identity=%d state=%#x", state.invokeMu.state.Load(), state.invocationID, in.invocationState.Load())
			}
			fail = false
			got, err = in.Invoke("g", 1)
			if err != nil || len(got) != 1 || got[0] != 2 {
				t.Fatalf("instance not reusable after %s: %v, %v", tc.name, got, err)
			}
		})
	}
}

func TestProfileBoundaryPreparedCalls(t *testing.T) {
	session := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
	defer session.Close()
	compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(session), benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, testImports("env.f", slotHostFunc(func(_ HostModule, args, results []uint64) { results[0] = args[0] + 1 })))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		out, err := fn.Invoke(uint64(i))
		if err != nil || len(out) != 1 || out[0] != uint64(i+1) {
			t.Fatalf("%v, %v", out, err)
		}
	}
	spans := session.Spans()
	if len(spans) != 9 {
		t.Fatalf("prepared fast path skipped boundaries: %+v", spans)
	}
	if report, err := profilereport.AnalyzeTimeline(spans, session.Status()); err != nil || !report.Complete {
		t.Fatal(report, err)
	}
}

func TestProfileBoundaryPreparedNilPanicComplete(t *testing.T) {
	if os.Getenv("WAGO_PROFILE_NIL_PANIC") != "1" {
		t.Skip("requires GODEBUG=panicnil=1")
	}
	for _, tc := range []struct {
		name    string
		session bool
		invoke  func(*WasmFunc, *PreparedSession) ([]uint64, error)
	}{
		{"function", false, func(fn *WasmFunc, _ *PreparedSession) ([]uint64, error) { return fn.Invoke(1) }},
		{"session-fixed", true, func(_ *WasmFunc, s *PreparedSession) ([]uint64, error) { return s.Invoke1(1) }},
		{"session-slice", true, func(_ *WasmFunc, s *PreparedSession) ([]uint64, error) { return s.invokeArgs([]uint64{1}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
			defer session.Close()
			compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(session), benchReturningImportModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			fail := true
			in, err := Instantiate(compiled, testImports("env.f", slotHostFunc(func(_ HostModule, args, results []uint64) {
				if fail {
					panic(nil)
				}
				results[0] = args[0] + 1
			})))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("g")
			if err != nil {
				t.Fatal(err)
			}
			var prepared *PreparedSession
			if tc.session {
				prepared, err = fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer prepared.Close()
			}
			got, callErr, panicked, recovered := invokeProfilePanic(tc.invoke, fn, prepared)
			if !panicked || recovered != nil || got != nil || callErr != nil {
				t.Fatalf("legacy nil panic = (%v, %#v, %v, %v)", panicked, recovered, got, callErr)
			}
			spans := session.Spans()
			if len(spans) != 3 {
				t.Fatal(spans)
			}
			for _, span := range spans {
				if span.End == 0 || span.Outcome != "panic" {
					t.Fatalf("unfinished/wrong prepared span: %+v", span)
				}
			}
			state := in.ensurePluginState()
			if activeHostInvocationContext(in).traceSession != nil {
				t.Fatal("trace context leaked after prepared panic")
			}
			if tc.session {
				if prepared.state.active.Load() || state.invokeMu.state.Load()&invocationGateHeld == 0 || state.invocationID == 0 || in.invocationState.Load()&instanceInvocationCount != 1 {
					t.Fatalf("prepared call leaked or lost its reservation: active=%v gate=%#x identity=%d state=%#x", prepared.state.active.Load(), state.invokeMu.state.Load(), state.invocationID, in.invocationState.Load())
				}
			} else if state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0 || in.invocationState.Load()&instanceInvocationCount != 0 {
				t.Fatalf("prepared function authority leaked: gate=%#x identity=%d state=%#x", state.invokeMu.state.Load(), state.invocationID, in.invocationState.Load())
			}
			fail = false
			got, err = tc.invoke(fn, prepared)
			if err != nil || len(got) != 1 || got[0] != 2 {
				t.Fatalf("prepared route not reusable: %v, %v", got, err)
			}
			if prepared != nil {
				prepared.Close()
			}
			if state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0 || in.invocationState.Load()&instanceInvocationCount != 0 {
				t.Fatalf("prepared reservation did not drain: gate=%#x identity=%d state=%#x", state.invokeMu.state.Load(), state.invocationID, in.invocationState.Load())
			}
		})
	}
}

func TestProfileBoundaryPreparedTypedHostNilPanicComplete(t *testing.T) {
	if os.Getenv("WAGO_PROFILE_NIL_PANIC") != "1" {
		t.Skip("requires GODEBUG=panicnil=1")
	}
	session := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
	defer session.Close()
	compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(session), benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	fail := true
	imports := NewImports()
	imports.HostFunc("env", "f", func(value int32) int32 {
		if fail {
			panic(nil)
		}
		return value + 1
	})
	in, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if !prepared.state.host || prepared.state.hostCall == nil {
		t.Fatal("typed host session did not select its reserved prepared call")
	}
	got, callErr, panicked, recovered := invokeProfilePanic(func(*WasmFunc, *PreparedSession) ([]uint64, error) {
		return prepared.Invoke1(I32(1))
	}, nil, nil)
	if !panicked || recovered != nil || got != nil || callErr != nil {
		t.Fatalf("typed prepared nil panic = (%v, %#v, %v, %v)", panicked, recovered, got, callErr)
	}
	spans := session.Spans()
	if len(spans) != 3 {
		t.Fatal(spans)
	}
	for _, span := range spans {
		if span.End == 0 || span.Outcome != "panic" {
			t.Fatalf("unfinished/wrong typed prepared span: %+v", span)
		}
	}
	state := in.ensurePluginState()
	if activeHostInvocationContext(in).traceSession != nil || prepared.state.active.Load() || state.invokeMu.state.Load()&invocationGateHeld == 0 || state.invocationID == 0 || in.invocationState.Load()&instanceInvocationCount != 1 {
		t.Fatalf("typed prepared call leaked or lost its reservation: trace=%v active=%v gate=%#x identity=%d state=%#x", activeHostInvocationContext(in).traceSession != nil, prepared.state.active.Load(), state.invokeMu.state.Load(), state.invocationID, in.invocationState.Load())
	}
	fail = false
	got, err = prepared.Invoke1(I32(1))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 2 {
		t.Fatalf("typed prepared route not reusable: %v, %v", got, err)
	}
	prepared.Close()
	if state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0 || in.invocationState.Load()&instanceInvocationCount != 0 {
		t.Fatalf("typed prepared reservation did not drain: gate=%#x identity=%d state=%#x", state.invokeMu.state.Load(), state.invocationID, in.invocationState.Load())
	}
}

func TestProfileBoundaryDirectHostReexportComplete(t *testing.T) {
	if goruntime.GOARCH == "riscv64" {
		t.Skip("RV64 rejects direct Go host reexports before publication")
	}
	cases := []struct {
		name, outcome string
		value         any
	}{{"panic", "panic", "host panic"}, {"exit", "host-exit", HostExit{Code: 7}}, {"cancel", "cancelled", HostTrap{Err: context.Canceled}}}
	if os.Getenv("WAGO_PROFILE_NIL_PANIC") == "1" {
		cases = append(cases, struct {
			name, outcome string
			value         any
		}{"nil-panic", "panic", nil})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
			defer session.Close()
			compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(session), directHostReexportModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			fail := true
			in, err := Instantiate(compiled, testImports("env.f", func(value int32) int32 {
				if fail {
					panic(tc.value)
				}
				return value + 1
			}))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, callErr, panicked, recovered := invokeProfilePanic(func(*WasmFunc, *PreparedSession) ([]uint64, error) {
				return in.Invoke("f", I32(1))
			}, nil, nil)
			wantPanic := tc.outcome == "panic"
			if panicked != wantPanic || wantPanic && recovered != tc.value || wantPanic && got != nil {
				t.Fatalf("direct reexport panic = (%v, %#v, %v), want (%v, %#v)", panicked, recovered, got, wantPanic, tc.value)
			}
			spans := session.Spans()
			if len(spans) != 2 {
				t.Fatal(spans)
			}
			for _, span := range spans {
				if span.End == 0 || span.Outcome != tc.outcome {
					t.Fatalf("unfinished/wrong direct reexport span: %+v, err %v", span, callErr)
				}
			}
			state := in.ensurePluginState()
			if activeHostInvocationContext(in).traceSession != nil || state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0 || in.invocationState.Load()&instanceInvocationCount != 0 {
				t.Fatalf("direct reexport authority leaked: trace=%v gate=%#x identity=%d state=%#x", activeHostInvocationContext(in).traceSession != nil, state.invokeMu.state.Load(), state.invocationID, in.invocationState.Load())
			}
			fail = false
			got, err = in.Invoke("f", I32(1))
			if err != nil || len(got) != 1 || AsI32(got[0]) != 2 {
				t.Fatalf("direct reexport not reusable: %v, %v", got, err)
			}
		})
	}
}

func TestProfileBoundaryDeferredHostEventComplete(t *testing.T) {
	if goruntime.GOARCH == "riscv64" {
		t.Skip("RV64 rejects deferred host events before publication")
	}
	cases := []struct {
		name, outcome string
		value         any
	}{{"panic", "panic", "event panic"}, {"exit", "host-exit", HostExit{Code: 7}}, {"cancel", "cancelled", HostTrap{Err: context.Canceled}}}
	if os.Getenv("WAGO_PROFILE_NIL_PANIC") == "1" {
		cases = append(cases, struct {
			name, outcome string
			value         any
		}{"nil-panic", "panic", nil})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
			defer session.Close()
			compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(session), profileDeferredHostEventModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			fail := true
			calls := 0
			in, err := Instantiate(compiled, testImports("env.event", I32HostEvent(func(int32) {
				calls++
				if fail {
					panic(tc.value)
				}
			})))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, callErr, panicked, recovered := invokeProfilePanic(func(*WasmFunc, *PreparedSession) ([]uint64, error) {
				return in.Invoke("run", I32(2))
			}, nil, nil)
			wantPanic := tc.outcome == "panic"
			if panicked != wantPanic || wantPanic && recovered != tc.value || wantPanic && got != nil {
				t.Fatalf("deferred event panic = (%v, %#v, %v), want (%v, %#v)", panicked, recovered, got, wantPanic, tc.value)
			}
			if calls != 1 {
				t.Fatalf("event replay continued after failure: calls=%d", calls)
			}
			spans := session.Spans()
			if len(spans) != 1 || spans[0].End == 0 || spans[0].Outcome != tc.outcome {
				t.Fatalf("unfinished/wrong deferred event span: %+v, err %v", spans, callErr)
			}
			state := in.ensurePluginState()
			if activeHostInvocationContext(in).traceSession != nil || state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0 || in.invocationState.Load()&instanceInvocationCount != 0 {
				t.Fatalf("deferred event authority leaked: trace=%v gate=%#x identity=%d state=%#x", activeHostInvocationContext(in).traceSession != nil, state.invokeMu.state.Load(), state.invocationID, in.invocationState.Load())
			}
			fail = false
			calls = 0
			got, err = in.Invoke("run", I32(0))
			if err != nil || len(got) != 0 || calls != 0 {
				t.Fatalf("undelivered event was retried: %v, %v, calls=%d", got, err, calls)
			}
			got, err = in.Invoke("run", I32(2))
			if err != nil || len(got) != 0 || calls != 2 {
				t.Fatalf("deferred event path not reusable: %v, %v, calls=%d", got, err, calls)
			}
		})
	}
}

func profileDeferredHostEventModule() []byte {
	typeEvent := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	importEvent := append(wasmtest.Name("env"), wasmtest.Name("event")...)
	importEvent = append(importEvent, 0, 0)
	body := []byte{
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x00, 0x45, 0x0d, 0x01,
		0x20, 0x00, 0x10, 0x00,
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00,
		0x0c, 0x00, 0x0b, 0x0b,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(typeEvent)),
		wasmtest.Section(2, wasmtest.Vec(importEvent)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestProfileBoundaryReentryNilPanicComplete(t *testing.T) {
	if os.Getenv("WAGO_PROFILE_NIL_PANIC") != "1" {
		t.Skip("requires GODEBUG=panicnil=1")
	}
	session := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
	defer session.Close()
	compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(session), benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	var a, b *Instance
	fail := true
	a, err = Instantiate(compiled, testImports("env.f", slotHostFunc(func(mod HostModule, _ []uint64, results []uint64) {
		out, callErr := b.InvokeFromHost(context.Background(), mod, "g", 0)
		if callErr != nil {
			panic(callErr)
		}
		results[0] = out[0] + 1
	})))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err = Instantiate(compiled, testImports("env.f", slotHostFunc(func(_ HostModule, _ []uint64, results []uint64) {
		if fail {
			panic(nil)
		}
		results[0] = 10
	})))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	got, callErr, panicked, recovered := invokeProfilePanic(func(*WasmFunc, *PreparedSession) ([]uint64, error) {
		return a.Invoke("g", 1)
	}, nil, nil)
	if !panicked || recovered != nil || got != nil || callErr != nil {
		t.Fatalf("legacy nil reentry panic = (%v, %#v, %v, %v)", panicked, recovered, got, callErr)
	}
	spans := session.Spans()
	if len(spans) != 6 {
		t.Fatal(spans)
	}
	for _, span := range spans {
		if span.End == 0 || span.Outcome != "panic" {
			t.Fatalf("unfinished/wrong reentry span: %+v", span)
		}
	}
	for _, in := range []*Instance{a, b} {
		state := in.ensurePluginState()
		if activeHostInvocationContext(in).traceSession != nil || state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0 || in.invocationState.Load()&instanceInvocationCount != 0 {
			t.Fatalf("reentry authority leaked: trace=%v gate=%#x identity=%d state=%#x", activeHostInvocationContext(in).traceSession != nil, state.invokeMu.state.Load(), state.invocationID, in.invocationState.Load())
		}
	}
	fail = false
	got, err = a.Invoke("g", 1)
	if err != nil || len(got) != 1 || got[0] != 11 {
		t.Fatalf("reentry path not reusable: %v, %v", got, err)
	}
}

func invokeProfilePanic(invoke func(*WasmFunc, *PreparedSession) ([]uint64, error), fn *WasmFunc, session *PreparedSession) (got []uint64, err error, panicked bool, recovered any) {
	func() {
		returned := false
		defer func() {
			recovered = recover()
			panicked = !returned
		}()
		got, err = invoke(fn, session)
		returned = true
	}()
	return got, err, panicked, recovered
}

func TestProfileBoundaryDirectGuestEntries(t *testing.T) {
	session := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
	defer session.Close()
	compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(session), identityI32Module())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("identity")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		got, err := in.Invoke("identity", 41)
		if err != nil || got[0] != 41 {
			t.Fatal(got, err)
		}
		got, err = fn.Invoke(42)
		if err != nil || got[0] != 42 {
			t.Fatal(got, err)
		}
	}
	spans := session.Spans()
	if len(spans) != 6 {
		t.Fatal(spans)
	}
	seen := map[uint64]bool{}
	for _, span := range spans {
		if span.Kind != "guest-invocation" || span.Function != 0 || span.InvocationID == 0 || seen[span.InvocationID] || span.Outcome != "return" || span.ParentID != 0 {
			t.Fatal(span)
		}
		seen[span.InvocationID] = true
	}
	if r, err := profilereport.AnalyzeTimeline(spans, session.Status()); err != nil || !r.Complete {
		t.Fatal(r, err)
	}
}

func TestProfileBoundaryConcurrentDirectEntriesAndClose(t *testing.T) {
	session := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
	defer session.Close()
	compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(session), identityI32Module())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 20; n++ {
				in.Invoke("identity", 1)
			}
		}()
	}
	in.Close()
	wg.Wait()
	spans := session.Spans()
	r, err := profilereport.AnalyzeTimeline(spans, session.Status())
	if err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	for _, s := range spans {
		if s.End == 0 {
			t.Fatal("unfinished invocation", s)
		}
	}
}

func TestProfileBoundaryTypedAndReservedEntries(t *testing.T) {
	trace := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
	defer trace.Close()
	compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(trace), identityI32Module())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 3; i++ {
		out, err := in.InvokeValues(context.Background(), "identity", ValueI32(7))
		if err != nil || out[0].I32() != 7 {
			t.Fatal(out, err)
		}
	}
	fn, err := in.WasmFunc("identity")
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		out, err := reserved.Invoke1(8)
		if err != nil || out[0] != 8 {
			reserved.Close()
			t.Fatal(out, err)
		}
	}
	reserved.Close()
	spans := trace.Spans()
	if len(spans) != 6 {
		t.Fatal(spans)
	}
	for _, s := range spans {
		if s.InvocationID == 0 || s.Kind != "guest-invocation" || s.Outcome != "return" || s.End == 0 {
			t.Fatal(s)
		}
	}
	if r, err := profilereport.AnalyzeTimeline(spans, trace.Status()); err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	if spans[3].InvocationID != spans[4].InvocationID || spans[4].InvocationID != spans[5].InvocationID {
		t.Fatal("session reservation identity changed")
	}
	if spans[3].ID == spans[4].ID {
		t.Fatal("session calls share activation identity")
	}
}

func TestProfileBoundaryReexportedHost(t *testing.T) {
	trace := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
	defer trace.Close()
	compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(trace), invocationContextReexportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	calls := 0
	in, err := Instantiate(compiled, testImports("env.outer", slotHostFunc(func(HostModule, []uint64, []uint64) { calls++ })))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 2; i++ {
		if _, err := in.InvokeValues(context.Background(), "call"); err != nil {
			t.Fatal(err)
		}
	}
	spans := trace.Spans()
	if calls != 2 || len(spans) != 4 {
		t.Fatal(calls, spans)
	}
	for i := 0; i < 4; i += 2 {
		if spans[i+1].Kind != "host-callback" || spans[i+1].ParentID != spans[i].ID || spans[i+1].Function != 0 {
			t.Fatal(spans)
		}
	}
}

func TestProfileBoundaryReservedHostCalls(t *testing.T) {
	trace := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
	defer trace.Close()
	compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(trace).WithIndependentInstanceExecution(true), benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	imports := NewImports()
	imports.HostFunc("env", "f", func(v int32) int32 { return v + 1 })
	in, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	if !reserved.state.host {
		reserved.Close()
		t.Fatal("reserved host fixture missed specialized path")
	}
	for i := 0; i < 3; i++ {
		out, err := reserved.Invoke1(uint64(i))
		if err != nil || out[0] != uint64(i+1) {
			reserved.Close()
			t.Fatal(out, err)
		}
	}
	reserved.Close()
	spans := trace.Spans()
	if len(spans) != 9 {
		t.Fatal(spans)
	}
	for i := 0; i < 9; i += 3 {
		if spans[i+1].ParentID != spans[i].ID || spans[i+2].ParentID != spans[i+1].ID {
			t.Fatal(spans)
		}
	}
	if r, err := profilereport.AnalyzeTimeline(spans, trace.Status()); err != nil || !r.Complete {
		t.Fatal(r, err)
	}
}

func TestProfileLifecycleDefersPhysicalRelease(t *testing.T) {
	trace := NewCodeProfile(CodeProfileOptions{TraceLifecycle: true})
	defer trace.Close()
	compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(trace), identityI32Module())
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(compiled)
	if err != nil {
		compiled.Close()
		t.Fatal(err)
	}
	if err := in.beginInvocation(); err != nil {
		t.Fatal(err)
	}
	compiled.Close()
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	spans := trace.Spans()
	if len(spans) != 2 || spans[0].Kind != "instantiate" || spans[1].Kind != "logical-close" {
		t.Fatal(spans)
	}
	events, _ := trace.Read(0)
	for _, event := range events {
		if event.Kind == "retire" {
			t.Fatal("mapping retired before retained invocation released")
		}
	}
	in.endInvocation()
	spans = trace.Spans()
	if len(spans) != 3 || spans[2].Kind != "physical-release" || spans[2].Start < spans[1].End {
		t.Fatal(spans)
	}
	for _, span := range spans {
		if span.InstanceID == 0 || span.InstanceID != spans[0].InstanceID || span.Outcome != "return" || span.End == 0 {
			t.Fatal(span)
		}
	}
	if r, err := profilereport.AnalyzeTimeline(spans, trace.Status()); err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	in.Close()
	if len(trace.Spans()) != 3 {
		t.Fatal("idempotent close duplicated lifecycle spans")
	}
}

func TestProfileLifecycleStartAncestry(t *testing.T) {
	trace := NewCodeProfile(CodeProfileOptions{TraceLifecycle: true})
	defer trace.Close()
	compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(trace), hostImportStartModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	imports := NewImports()
	imports.HostFunc("env", "f", func(HostCall) {})
	in, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	spans := trace.Spans()
	if len(spans) != 4 {
		t.Fatal(spans)
	}
	want := []string{"instantiate", "initialize-guest", "guest-activation", "host-callback"}
	for i, span := range spans {
		if span.Kind != want[i] || span.InstanceID != spans[0].InstanceID || span.Outcome != "return" {
			t.Fatal(span)
		}
		if i > 0 && span.ParentID != spans[i-1].ID {
			t.Fatalf("start ancestry broken: %+v", spans)
		}
	}
	if r, err := profilereport.AnalyzeTimeline(spans, trace.Status()); err != nil || !r.Complete {
		t.Fatal(r, err)
	}
}

func TestProfileLifecycleFailedInitialization(t *testing.T) {
	trace := NewCodeProfile(CodeProfileOptions{TraceLifecycle: true})
	defer trace.Close()
	compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(trace), failingLocalStartModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, testImports("env.f", slotHostFunc(func(HostModule, []uint64, []uint64) {})))
	if err == nil || in != nil {
		t.Fatal("start trap accepted", in, err)
	}
	spans := trace.Spans()
	// This start calls the host successfully before trapping. Boundary tracing
	// also records that activation/callback and both failed-instance cleanup
	// phases; cleanup must not inherit the failed guest's call ancestry.
	wantKinds := []string{"instantiate", "initialize-guest", "guest-activation", "host-callback", "logical-close", "physical-release"}
	if len(spans) != len(wantKinds) {
		t.Fatal(spans)
	}
	wantParents := []uint64{0, spans[0].ID, spans[1].ID, spans[2].ID, 0, 0}
	for i, span := range spans {
		want := "trap"
		if i >= 3 {
			want = "return"
		}
		if span.Kind != wantKinds[i] || span.ParentID != wantParents[i] || span.Outcome != want || span.End == 0 || span.InstanceID != spans[0].InstanceID {
			t.Fatal(span)
		}
	}
	if r, err := profilereport.AnalyzeTimeline(spans, trace.Status()); err != nil || !r.Complete {
		t.Fatal(r, err)
	}
}

func TestProfileLifecycleDefinedInitializationPanicComplete(t *testing.T) {
	cases := []struct {
		name  string
		value any
	}{{name: "value", value: "defined start panic"}}
	if os.Getenv("WAGO_DEFINED_START_NIL_PANIC") == "1" {
		cases = append(cases, struct {
			name  string
			value any
		}{name: "nil", value: nil})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trace := NewCodeProfile(CodeProfileOptions{TraceLifecycle: true})
			defer trace.Close()
			compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(trace), hostImportStartModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			calls := 0
			var captured *Instance
			var callerToken Caller
			imports := NewImports().Function("env", "f", CallerHostCallFunc(func(caller Caller, _ HostCall) {
				calls++
				captured = caller.in
				callerToken = caller
				panic(tc.value)
			}))
			in, err := Instantiate(compiled, imports)
			if in != nil {
				defer in.Close()
			}
			if in != nil || err == nil || calls != 1 {
				t.Fatalf("failed defined start = (%p, %v, calls=%d)", in, err, calls)
			}
			spans := trace.Spans()
			wantKinds := []string{"instantiate", "initialize-guest", "guest-activation", "host-callback", "logical-close", "physical-release"}
			if len(spans) != len(wantKinds) {
				t.Fatal(spans)
			}
			wantParents := []uint64{0, spans[0].ID, spans[1].ID, spans[2].ID, 0, 0}
			wantOutcomes := []string{"error", "panic", "panic", "panic", "return", "return"}
			for i, span := range spans {
				if span.Kind != wantKinds[i] || span.ParentID != wantParents[i] || span.Outcome != wantOutcomes[i] || span.End == 0 || span.InstanceID != spans[0].InstanceID {
					t.Fatalf("wrong defined-start span %d: %+v", i, span)
				}
			}
			if captured == nil || callerToken.valid() || !captured.resourcesClosed || captured.constructionIsActive() || captured.invocationState.Load()&instanceInvocationCount != 0 || compiled.codeCache.refs != 0 {
				t.Fatal("failed defined start retained callback or instance lifetime")
			}
			if state := captured.pluginState.Load(); state != nil && (state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0) {
				t.Fatal("failed defined start retained invocation gate")
			}
			if report, err := profilereport.AnalyzeTimeline(spans, trace.Status()); err != nil || !report.Complete {
				t.Fatal(report, err)
			}
		})
	}
}

func TestProfileLifecycleImportedInitialization(t *testing.T) {
	trace := NewCodeProfile(CodeProfileOptions{TraceLifecycle: true})
	defer trace.Close()
	compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(trace), invocationContextImportedStartModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	called := false
	in, err := Instantiate(compiled, testImports("env.outer", slotHostFunc(func(HostModule, []uint64, []uint64) { called = true })))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	spans := trace.Spans()
	if !called || len(spans) != 2 || spans[1].Kind != "initialize-host" || spans[1].Function != 0 || spans[1].ParentID != spans[0].ID || spans[1].Outcome != "return" {
		t.Fatal(called, spans)
	}
}

func TestProfileLifecycleImportedInitializationPanicComplete(t *testing.T) {
	cases := []struct {
		name, outcome string
		value         any
	}{{"panic", "error", "start panic"}, {"exit", "host-exit", HostExit{Code: 7}}, {"cancel", "cancelled", HostTrap{Err: context.Canceled}}}
	if os.Getenv("WAGO_PROFILE_NIL_PANIC") == "1" {
		cases = append(cases, struct {
			name, outcome string
			value         any
		}{"nil-panic", "error", nil})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trace := NewCodeProfile(CodeProfileOptions{TraceLifecycle: true})
			defer trace.Close()
			compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(trace), invocationContextImportedStartModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			fail := true
			calls := 0
			var captured *Instance
			var callerToken Caller
			imports := NewImports()
			imports.Function("env", "outer", CallerHostCallFunc(func(caller Caller, _ HostCall) {
				calls++
				captured = caller.in
				callerToken = caller
				if fail {
					panic(tc.value)
				}
			}))
			in, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
			if in != nil || err == nil || calls != 1 {
				if in != nil {
					in.Close()
				}
				t.Fatalf("failing imported start = (%p, %v, calls=%d)", in, err, calls)
			}
			switch tc.outcome {
			case "host-exit":
				var exit *ExitError
				if !errors.As(err, &exit) || exit.Code != 7 {
					t.Fatalf("host exit changed: %v", err)
				}
			case "cancelled":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation changed: %v", err)
				}
			default:
				want := fmt.Sprint(tc.value)
				if !strings.Contains(err.Error(), "host start panicked") || !strings.Contains(err.Error(), want) {
					t.Fatalf("start panic changed: %v", err)
				}
			}
			spans := trace.Spans()
			wantKinds := []string{"instantiate", "initialize-host", "logical-close", "physical-release"}
			if len(spans) != len(wantKinds) {
				t.Fatal(spans)
			}
			for i, span := range spans {
				wantOutcome := tc.outcome
				wantParent := uint64(0)
				if i == 1 {
					wantParent = spans[0].ID
				}
				if i >= 2 {
					wantOutcome = "return"
				}
				if span.Kind != wantKinds[i] || span.ParentID != wantParent || span.Outcome != wantOutcome || span.End == 0 || span.InstanceID != spans[0].InstanceID {
					t.Fatalf("wrong imported-start span %d: %+v", i, span)
				}
			}
			if compiled.codeCache.refs != 0 {
				t.Fatalf("failed imported start retained code: refs=%d", compiled.codeCache.refs)
			}
			if captured == nil {
				t.Fatal("imported start did not expose its constructing instance")
			}
			if callerToken.valid() || !captured.resourcesClosed || captured.constructionIsActive() || captured.invocationState.Load()&instanceInvocationCount != 0 {
				t.Fatalf("failed imported start retained lifetime: caller=%t resources=%t construction=%t invocations=%d", callerToken.valid(), captured.resourcesClosed, captured.constructionIsActive(), captured.invocationState.Load()&instanceInvocationCount)
			}
			if state := captured.pluginState.Load(); state != nil && (state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0) {
				t.Fatalf("failed imported start retained gate: state=%#x id=%d", state.invokeMu.state.Load(), state.invocationID)
			}
			fail = false
			calls = 0
			in, err = Instantiate(compiled, InstantiateOptions{Imports: imports})
			retry := in
			if retry != nil {
				t.Cleanup(func() {
					if retry != nil {
						_ = retry.Close()
					}
				})
			}
			if err != nil || in == nil || calls != 1 {
				t.Fatalf("reuse after imported-start failure = (%v, %v, calls=%d)", in, err, calls)
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			retry = nil
			if report, err := profilereport.AnalyzeTimeline(trace.Spans(), trace.Status()); err != nil || !report.Complete {
				t.Fatal(report, err)
			}
		})
	}
}
