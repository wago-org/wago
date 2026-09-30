//go:build wago_profile && !wago_precompiled

package wago

import (
	"context"
	"sync"
	"testing"

	profilereport "github.com/wago-org/wago/profile"
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
	for _, tc := range []struct {
		name, outcome string
		value         any
	}{{"panic", "panic", "host panic"}, {"exit", "host-exit", HostExit{Code: 7}}, {"cancel", "cancelled", HostTrap{Err: context.Canceled}}} {
		t.Run(tc.name, func(t *testing.T) {
			session := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
			defer session.Close()
			compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(session), benchReturningImportModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled, testImports("env.f", slotHostFunc(func(HostModule, []uint64, []uint64) { panic(tc.value) })))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			func() {
				defer func() {
					r := recover()
					if tc.name == "panic" && r != tc.value {
						t.Fatalf("panic changed: %v", r)
					}
				}()
				_, err = in.Invoke("g", 1)
			}()
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
	if len(spans) != 4 {
		t.Fatal(spans)
	}
	for i, span := range spans {
		want := "trap"
		if i == 3 {
			want = "return"
		}
		if span.Outcome != want || span.End == 0 {
			t.Fatal(span)
		}
	}
	if spans[1].ParentID != spans[0].ID {
		t.Fatal(spans)
	}
	if r, err := profilereport.AnalyzeTimeline(spans, trace.Status()); err != nil || !r.Complete {
		t.Fatal(r, err)
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
