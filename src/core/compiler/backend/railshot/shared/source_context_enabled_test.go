//go:build wago_regalloccheck

package shared

import (
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func sourceWorkerFixture(t *testing.T) (*wasm.Module, *wasm.ValidatedModuleAnalysis, *codegen.SourceContext) {
	t.Helper()
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc}}}}}, FuncTypes: []wasm.TypeIdx{{}}, Code: []wasm.Func{{BodyBytes: []byte{0x0b}}}}
	a := new(wasm.ValidatedModuleAnalysis)
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a); err != nil {
		t.Fatal(err)
	}
	var opts codegen.Options
	if !codegen.AttachSourceContext(&opts, m, a, wasm.ValidationFeatures{}) {
		t.Fatal("context unavailable")
	}
	return m, a, codegen.SourceContextFor(opts, m)
}

func sourceWorkerLedger(t *testing.T, m *wasm.Module, a *wasm.ValidatedModuleAnalysis) *wasm.SourceLedger {
	t.Helper()
	l, result, err := wasm.BuildSourceLedger(m, a, 0, wasm.ValidationFeatures{}, wasm.SourceLedgerLimits{})
	if err != nil || result.Coverage != wasm.SourceContractsComplete || l == nil {
		t.Fatalf("ledger: %+v / %v", result, err)
	}
	t.Cleanup(l.Close)
	return l
}

func TestSourceWorkerAttemptOwnershipAndCleanup(t *testing.T) {
	m, a, ctx := sourceWorkerFixture(t)
	var s ScalarState
	if BeginSourceAttempt(&s, m, 0) != nil {
		t.Fatal("missing context admitted")
	}
	SetSourceContext(&s, ctx)
	token := BeginSourceAttempt(&s, m, 0)
	ledger := sourceWorkerLedger(t, m, a)
	if token == nil || !AttachSourceLedger(token, ledger) {
		t.Fatal("valid ledger ownership rejected")
	}
	// A nested begin never grants ownership to its cleanup and preserves parent.
	func() {
		var nested *SourceAttempt
		defer func() {
			EndSourceAttempt(nested)
			if recover() == nil {
				t.Error("nested begin accepted")
			}
		}()
		nested = BeginSourceAttempt(&s, m, 0)
	}()
	if s.sourceAttempt != token || !ledger.ValidFor(m, 0, wasm.ValidationFeatures{}) {
		t.Fatal("nested cleanup retired parent ledger")
	}
	EndSourceAttempt(token)
	EndSourceAttempt(token)
	if ledger.ValueCount() != 0 || ledger.EventCount() != 0 || ledger.ValidFor(m, 0, wasm.ValidationFeatures{}) || s.sourceAttempt != nil || s.sourceContext != ctx {
		t.Fatal("attempt cleanup lost ownership or retained ledger")
	}
	// Worker teardown is a final safety net, including an abandoned attempt.
	token = BeginSourceAttempt(&s, m, 0)
	ledger = sourceWorkerLedger(t, m, a)
	if !AttachSourceLedger(token, ledger) {
		t.Fatal("reused worker rejected")
	}
	s.FinishWorker()
	if ledger.EventCount() != 0 || s.sourceAttempt != nil || s.sourceContext != nil || !a.ValidFor(m) {
		t.Fatal("worker retained state or invalidated shared context")
	}
}

func TestSourceWorkerPanicAndInvalidLedger(t *testing.T) {
	m, a, ctx := sourceWorkerFixture(t)
	var s ScalarState
	SetSourceContext(&s, ctx)
	ledger := sourceWorkerLedger(t, m, a)
	func() {
		var token *SourceAttempt
		defer func() {
			EndSourceAttempt(token)
			if recover() != "original emission panic" {
				t.Error("cleanup changed original panic")
			}
		}()
		token = BeginSourceAttempt(&s, m, 0)
		if !AttachSourceLedger(token, ledger) {
			t.Fatal("ledger ownership rejected")
		}
		panic("original emission panic")
	}()
	if ledger.EventCount() != 0 || s.sourceAttempt != nil {
		t.Fatal("panic retained ledger")
	}
	if BeginSourceAttempt(&s, &wasm.Module{}, 0) != nil || BeginSourceAttempt(&s, m, -1) != nil || BeginSourceAttempt(&s, m, 1) != nil {
		t.Fatal("foreign/out-of-range attempt admitted")
	}
	token := BeginSourceAttempt(&s, m, 0)
	defer EndSourceAttempt(token)
	foreign, foreignAnalysis, _ := sourceWorkerFixture(t)
	foreignLedger := sourceWorkerLedger(t, foreign, foreignAnalysis)
	if AttachSourceLedger(token, foreignLedger) || !foreignLedger.ValidFor(foreign, 0, wasm.ValidationFeatures{}) || AttachSourceLedger(token, nil) {
		t.Fatal("invalid attach transferred ownership")
	}
}

func TestSourceWorkersBorrowContextConcurrently(t *testing.T) {
	m, analysis, ctx := sourceWorkerFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		ledger := sourceWorkerLedger(t, m, analysis)
		wg.Add(1)
		go func() {
			defer wg.Done()
			var s ScalarState
			SetSourceContext(&s, ctx)
			token := BeginSourceAttempt(&s, m, 0)
			if token == nil {
				t.Error("shared immutable context unavailable")
			}
			if !AttachSourceLedger(token, ledger) {
				t.Error("worker ledger ownership unavailable")
			}
			EndSourceAttempt(token)
			s.FinishWorker()
			if ledger.EventCount() != 0 || ledger.ValueCount() != 0 {
				t.Error("worker retained ledger pools")
			}
		}()
	}
	wg.Wait()
	if _, _, ok := codegen.ValidatedSourceContext(ctx, m); !ok {
		t.Fatal("worker cleared another worker's context")
	}
}
