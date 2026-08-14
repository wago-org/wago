//go:build amd64

package amd64

import (
	"bytes"
	"fmt"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestCompileModuleWithPoliciesDoNotCrossTalkAMD64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x0b}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
	)

	compile := func(regABI bool) ([]byte, error) {
		cm, err := CompileModuleWith(m, CompileOptions{
			Workers:       1,
			Optimizations: map[string]bool{"reg-abi": regABI},
		})
		if err != nil {
			return nil, err
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		return append([]byte(nil), cm.Code...), nil
	}

	register, err := compile(true)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := compile(false)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(register, stack) {
		t.Fatal("register-ABI policies produced identical code; test cannot detect cross-talk")
	}

	before := CurrentOptKnobSnapshot()
	const goroutines = 8
	const iterations = 8
	errCh := make(chan error, goroutines)
	var wg sync.WaitGroup
	for worker := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			regABI := worker%2 == 0
			want := stack
			if regABI {
				want = register
			}
			for iteration := range iterations {
				got, err := compile(regABI)
				if err != nil {
					errCh <- fmt.Errorf("worker %d iteration %d: %w", worker, iteration, err)
					return
				}
				if !bytes.Equal(got, want) {
					errCh <- fmt.Errorf("worker %d iteration %d: policy output changed", worker, iteration)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if after := CurrentOptKnobSnapshot(); after != before {
		t.Fatal("per-compilation policy mutated process defaults")
	}
}

func TestNativeCompactionObjectiveAndRollbackAMD64(t *testing.T) {
	beforeEnabled, beforeDisabled := nativeCompactionEnabled, nativeCompactionDisabled
	beforeLimitOverride := finalizerDeletionLimitOverride
	nativeCompactionEnabled, nativeCompactionDisabled = false, false
	t.Cleanup(func() {
		nativeCompactionEnabled, nativeCompactionDisabled = beforeEnabled, beforeDisabled
		finalizerDeletionLimitOverride = beforeLimitOverride
	})

	selection := currentCodegenPolicy().Selection
	balanced := shared.CodegenPolicyForObjective(selection, OptimizeBalanced)
	size := shared.CodegenPolicyForObjective(selection, OptimizeSize)
	if compactNativePolicy(balanced) {
		t.Fatal("Balanced unexpectedly enabled native compaction")
	}
	if !compactNativePolicy(size) {
		t.Fatal("Size did not enable native compaction")
	}
	f := fn{policy: size}
	if got := f.finalizerDeletionLimit(); got != shared.MaxOffsetMapDeletions {
		t.Fatalf("Size finalizer deletion limit = %d, want %d", got, shared.MaxOffsetMapDeletions)
	}
	finalizerDeletionLimitOverride = 48
	if got := f.finalizerDeletionLimit(); got != 48 {
		t.Fatalf("finalizer deletion limit override = %d, want 48", got)
	}
	finalizerDeletionLimitOverride = 0

	nativeCompactionEnabled = true
	if !compactNativePolicy(balanced) {
		t.Fatal("WAGO_COMPACT=1 override did not enable Balanced compaction")
	}
	nativeCompactionDisabled = true
	if compactNativePolicy(size) || compactNativePolicy(balanced) {
		t.Fatal("WAGO_COMPACT=0 rollback did not disable compaction")
	}
}

func TestFunctionStartPaddingObjectivesAMD64(t *testing.T) {
	selection := currentCodegenPolicy().Selection
	policy := func(objective OptimizationObjective) CodegenPolicy {
		return shared.CodegenPolicyForObjective(selection, objective)
	}
	hot := funcHints{hasLoop: true}
	for _, test := range []struct {
		name      string
		off       int
		bodyBytes int
		adapter   bool
		hints     funcHints
		objective OptimizationObjective
		want      int
	}{
		{name: "speed tiny", off: 3, bodyBytes: 12, objective: OptimizeSpeed, want: 13},
		{name: "balanced tiny leaf", off: 3, bodyBytes: 12, objective: OptimizeBalanced, want: 0},
		{name: "balanced adapter", off: 3, bodyBytes: 12, adapter: true, objective: OptimizeBalanced, want: 13},
		{name: "balanced hot within budget", off: 12, bodyBytes: 64, hints: hot, objective: OptimizeBalanced, want: 4},
		{name: "balanced hot over budget", off: 3, bodyBytes: 64, hints: hot, objective: OptimizeBalanced, want: 0},
		{name: "size", off: 3, bodyBytes: 512, adapter: true, hints: hot, objective: OptimizeSize, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := functionStartPadding(test.off, test.bodyBytes, test.adapter, test.hints, policy(test.objective)); got != test.want {
				t.Fatalf("padding = %d, want %d", got, test.want)
			}
		})
	}
}

func TestObjectiveLayoutSerialParallelParityAMD64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x41, 0x02, 0x6a, 0x0b}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x41, 0x03, 0x6a, 0x0b}},
	)
	compile := func(objective OptimizationObjective, workers int) []byte {
		cm, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Workers: workers})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		return append([]byte(nil), cm.Code...)
	}
	balanced := compile(OptimizeBalanced, 1)
	sizeSerial := compile(OptimizeSize, 1)
	sizeParallel := compile(OptimizeSize, 3)
	if !bytes.Equal(sizeSerial, sizeParallel) {
		t.Fatal("Size layout differs between serial and parallel compilation")
	}
	if len(sizeSerial) > len(balanced) {
		t.Fatalf("Size output = %d bytes, Balanced = %d; want no larger Size layout", len(sizeSerial), len(balanced))
	}
}

func TestCompileModuleRejectsInvalidObjectiveAMD64(t *testing.T) {
	m := mod1(t, nil, nil, []byte{0x00, 0x0b})
	objective := OptimizationObjective(255)
	if _, err := CompileModuleWith(m, CompileOptions{Objective: &objective}); err == nil {
		t.Fatal("invalid optimization objective was accepted")
	}
}
