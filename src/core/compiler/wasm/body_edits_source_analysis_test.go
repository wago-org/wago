package wasm

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestOwnedBodySourceOriginalAnalysis(t *testing.T) {
	m := editTestModule([]byte{0x41, 7, 0x0b}, []byte{0x41, 8, 0x0b})
	var ordinary ValidatedModuleAnalysis
	if err := ValidateModuleWithAnalysis(m, ValidationFeatures{}, 1, ValidationLimits{}, &ordinary); err != nil {
		t.Fatal(err)
	}
	_ = m.typeIndex()
	_ = m.ImportedFuncCount()
	owner, a, err := NewOwnedBodySourceWithAnalysis(m, ValidationFeatures{}, 1, ValidationLimits{})
	if err != nil || !a.ValidFor(m) || a.Flags() != ordinary.Flags() || !reflect.DeepEqual(a.funcs, ordinary.funcs) {
		t.Fatalf("fresh original facts: %v", err)
	}
	if m.structuralTypeCache != nil || m.typeIndexDirectory != nil || m.importIndexDirectory != nil {
		t.Fatal("original caches not detached")
	}
	// The ordinary facts cease to be usable once m changes. The owner remains
	// independent and validates the old private declarations and unchanged body.
	m.Code[0].BodyBytes[0] = 0xff
	m.Types[0].SubTypes[0].Comp.Results[0] = I64
	out, fresh, err := owner.Apply([]BodyEdit{{LocalFunction: 1, BodyBytes: []byte{0x41, 9, 0xb}}}, ValidationFeatures{}, 1, ValidationLimits{})
	if err != nil || !fresh.ValidFor(out) || out.Code[0].BodyBytes[0] != 0x41 || out.Types[0].SubTypes[0].Comp.Results[0] != I32 {
		t.Fatalf("source changed through original alias: %v", err)
	}
}

func TestOwnedBodySourceConcurrentConsume(t *testing.T) {
	m := editTestModule([]byte{0x41, 7, 0x0b})
	owner, _, err := NewOwnedBodySourceWithAnalysis(m, ValidationFeatures{}, 1, ValidationLimits{})
	if err != nil {
		t.Fatal(err)
	}
	const n = 8
	start := make(chan struct{})
	done := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out, a, e := owner.Apply([]BodyEdit{{BodyBytes: []byte{0x41, 9, 0xb}}}, ValidationFeatures{}, 1, ValidationLimits{})
			if e == nil && !a.ValidFor(out) {
				e = errors.New("success without fresh facts")
			}
			done <- e
		}()
	}
	close(start)
	wg.Wait()
	close(done)
	success := 0
	consumed := 0
	for e := range done {
		if e == nil {
			success++
		} else if errors.Is(e, ErrBodySourceConsumed) {
			consumed++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || consumed != n-1 {
		t.Fatalf("success%d consumed%d", success, consumed)
	}
}
