//go:build wago_regalloccheck

package codegen

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"sync"
	"testing"
)

type testSourceFinalWitness struct{ closes int }

func (w *testSourceFinalWitness) Close() { w.closes++ }

func TestSourceFinalWitnessAttemptHistoryAndClose(t *testing.T) {
	m, a := validatedContextModule(t)
	opts := SourceOptions(Options{}, m, a, wasm.ValidationFeatures{})
	ctx := SourceContextFor(opts, m)
	work, storage := 100, 100
	one := BeginSourceFinalAttempt(ctx, m, 0, &work, &storage)
	w := new(testSourceFinalWitness)
	if SourceFinalAttemptFailure(one) != regalloccheck.NoFailure || !StoreSourceFinalWitness(one, m, w) {
		t.Fatal("attempt store")
	}
	beforeWork, beforeStorage := work, storage
	RetireSourceFinalWitness(ctx, m, 0)
	if w.closes != 1 || work != beforeWork || storage != beforeStorage || StoreSourceFinalWitness(one, m, new(testSourceFinalWitness)) {
		t.Fatal("retirement authority/refund")
	}
	two := BeginSourceFinalAttempt(ctx, m, 0, &work, &storage)
	if two == one || work != beforeWork-1 || storage != beforeStorage {
		t.Fatal("reused generation or allocation")
	}
	w2 := new(testSourceFinalWitness)
	if !StoreSourceFinalWitness(two, m, w2) {
		t.Fatal("second store")
	}
	RetireSourceFinalAttempt(one, m)
	if w2.closes != 0 || !SourceFinalAttemptValid(two, ctx, m, 0) {
		t.Fatal("old owner retired newer attempt")
	}
	var delivered bool
	SetSourceReporter(opts, m, func(r SourceReport) {
		delivered = true
		if w2.closes != 1 || ctx.finalWitnesses != nil || ctx.module != nil {
			t.Error("witness retained at callback")
		}
	})
	CloseSourceContext(opts, m)
	CloseSourceContext(opts, m)
	if !delivered || w2.closes != 1 || SourceFinalAttemptValid(two, ctx, m, 0) || StoreSourceFinalWitness(two, m, new(testSourceFinalWitness)) {
		t.Fatal("retired context authority")
	}
}

func TestSourceFinalWitnessQuotaConsumeAndOverflow(t *testing.T) {
	m, a := validatedContextModule(t)
	opts := SourceOptions(Options{}, m, a, wasm.ValidationFeatures{})
	ctx := SourceContextFor(opts, m)
	defer CloseSourceContext(opts, m)
	work, storage := 100, 0
	if tok := BeginSourceFinalAttempt(ctx, m, 0, &work, &storage); SourceFinalAttemptFailure(tok) != regalloccheck.ResourceLimit || ctx.finalWitnesses != nil || work != 100 {
		t.Fatal("allocation before storage credit")
	}
	storage = 100
	tok := BeginSourceFinalAttempt(ctx, m, 0, &work, &storage)
	w := new(testSourceFinalWitness)
	if !StoreSourceFinalWitness(tok, m, w) || StoreSourceFinalWitness(tok, m, new(testSourceFinalWitness)) {
		t.Fatal("duplicate store")
	}
	taken, got := TakeSourceFinalWitness(ctx, m, 0)
	if taken != tok || got != w || StoreSourceFinalWitness(tok, m, new(testSourceFinalWitness)) {
		t.Fatal("consume authority")
	}
	if _, got := TakeSourceFinalWitness(ctx, m, 0); got != nil {
		t.Fatal("duplicate take")
	}
	got.Close()
	RetireSourceFinalWitness(ctx, m, 0)
	ctx.finalSerial = ^uint64(0)
	if t2 := BeginSourceFinalAttempt(ctx, m, 0, &work, &storage); SourceFinalAttemptFailure(t2) != regalloccheck.ResourceLimit {
		t.Fatal("serial overflow")
	}
	beforeWork, beforeStorage := ctx.finalWork, ctx.finalStorage
	if !ReserveFinalSourcePass(ctx, m) || ChargeFinalSourceWitnessPass(ctx, m, 1, 0) || ctx.finalWork != 0 || ctx.finalStorage != 0 {
		t.Fatal("final pool replenished")
	}
	if beforeWork != 16384 || beforeStorage != 8192 {
		t.Fatal("worker collection stole final credits")
	}
}

func TestSourceFinalWitnessConcurrentOneAllocation(t *testing.T) {
	m, a := validatedContextModule(t)
	for i := 1; i < 8; i++ {
		m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
		m.Code = append(m.Code, m.Code[0])
	}
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a); err != nil {
		t.Fatal(err)
	}
	opts := SourceOptions(Options{}, m, a, wasm.ValidationFeatures{})
	ctx := SourceContextFor(opts, m)
	defer CloseSourceContext(opts, m)
	var wg sync.WaitGroup
	var remaining [8]int
	var witnesses [8]*testSourceFinalWitness
	for i := range remaining {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			work, storage := 100, 100
			tok := BeginSourceFinalAttempt(ctx, m, i, &work, &storage)
			witnesses[i] = new(testSourceFinalWitness)
			if !StoreSourceFinalWitness(tok, m, witnesses[i]) {
				t.Error("parallel store")
			}
			remaining[i] = storage
		}(i)
	}
	wg.Wait()
	used := 0
	for _, r := range remaining {
		used += 100 - r
	}
	if used != 16 {
		t.Fatal("collection charged more than once", used)
	}
	CloseSourceContext(opts, m)
	for _, w := range witnesses {
		if w.closes != 1 {
			t.Fatal("lost concurrent witness")
		}
	}
}
