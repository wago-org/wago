//go:build linux && riscv64 && wago_profile && !wago_precompiled && !tinygo && !wago_target_tinygo

package wago

import (
	"os"
	"testing"

	profilereport "github.com/wago-org/wago/profile"
)

func TestRV64ProfileNativeImportedStartNilPanicCompletes(t *testing.T) {
	if os.Getenv("WAGO_NATIVE_IMPORTED_START_NIL_PANIC") != "1" {
		t.Skip("legacy panic(nil) case is opt-in")
	}
	trace := NewCodeProfile(CodeProfileOptions{TraceLifecycle: true})
	defer trace.Close()
	consumerCode, err := Compile(NewRuntimeConfig().WithCodeProfile(trace), invocationContextImportedStartModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	producerCode := rv64PublicStartCompile(t, rv64PublicNativeStartHostPanicProducerModule(), false)
	calls := 0
	var callerToken Caller
	producer, err := Instantiate(producerCode, NewImports().Function("env", "f", CallerHostCallFunc(func(caller Caller, _ HostCall) {
		calls++
		callerToken = caller
		panic(nil)
	})))
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	export, err := producer.ExportedFunc("boot")
	if err != nil {
		t.Fatal(err)
	}
	beforeRoots := producer.referenceLifetime().snapshot().ResourceRoots
	in, err := Instantiate(consumerCode, testImports("env.outer", export))
	if in != nil {
		defer in.Close()
	}
	if in != nil || err == nil || calls != 1 || callerToken.valid() {
		t.Fatalf("profiled native imported start nil panic = (%p, %v, calls=%d, caller-valid=%t)", in, err, calls, callerToken.valid())
	}
	spans := trace.Spans()
	wantKinds := []string{"instantiate", "initialize-host", "guest-activation", "host-callback", "logical-close", "physical-release"}
	wantOutcomes := []string{"error", "panic", "panic", "panic", "return", "return"}
	if len(spans) != len(wantKinds) {
		t.Fatal(spans)
	}
	wantParents := []uint64{0, spans[0].ID, spans[1].ID, spans[2].ID, 0, 0}
	for i, span := range spans {
		if span.Kind != wantKinds[i] || span.Outcome != wantOutcomes[i] || span.ParentID != wantParents[i] || span.End == 0 {
			t.Fatalf("wrong native imported-start span %d: %+v", i, span)
		}
		if i == 3 {
			if span.InstanceID == spans[0].InstanceID {
				t.Fatalf("producer span %d used consumer identity: %+v", i, span)
			}
		} else if span.InstanceID != spans[0].InstanceID {
			t.Fatalf("consumer span %d changed identity: %+v", i, span)
		}
	}
	if producer.referenceLifetime().snapshot().ResourceRoots != beforeRoots || producer.invocationState.Load()&instanceInvocationCount != 0 || consumerCode.codeCache.refs != 0 {
		t.Fatal("profiled native imported-start panic retained identity, attachment, invocation, or code reference")
	}
	if report, err := profilereport.AnalyzeTimeline(spans, trace.Status()); err != nil || !report.Complete {
		t.Fatal(report, err)
	}
}
