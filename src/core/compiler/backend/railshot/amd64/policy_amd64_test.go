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

func TestHiddenOptimizationFamiliesUsePerCompilePolicyAMD64(t *testing.T) {
	names := []string{
		"simd-superopt", "swar-idioms", "interval-region-pins", "magic-div",
		"shared-trap-body", "shared-adapters", "dead-gc-new", "gc-ref-facts", "gc-native-alloc",
	}
	overrides := make(map[string]bool, len(names))
	for _, name := range names {
		overrides[name] = false
	}
	selection, err := optimizationBindings.ResolveSnapshot(overrides, OptimizationSnapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	policy := shared.DefaultCodegenPolicy(selection)
	for _, name := range names {
		if policy.EnabledOption(optimizationBindings.Option(name)) {
			t.Errorf("per-compile policy did not disable %s", name)
		}
	}
}

func TestNativeCompactionPolicyAndRollbackAMD64(t *testing.T) {
	beforeEnabled, beforeDisabled := nativeCompactionEnabled, nativeCompactionDisabled
	beforeLimitOverride := finalizerDeletionLimitOverride
	beforeRel32Override := finalizerRel32SiteLimitOverride
	beforeLoopOverride := loopCompactionByteLimitOverride
	nativeCompactionEnabled, nativeCompactionDisabled = false, false
	t.Cleanup(func() {
		nativeCompactionEnabled, nativeCompactionDisabled = beforeEnabled, beforeDisabled
		finalizerDeletionLimitOverride = beforeLimitOverride
		finalizerRel32SiteLimitOverride = beforeRel32Override
		loopCompactionByteLimitOverride = beforeLoopOverride
	})

	selection := currentCodegenPolicy().Selection
	ordinary := shared.DefaultCodegenPolicy(selection)
	compact := shared.CompactCodegenPolicy(selection)
	if compactNativePolicy(ordinary) {
		t.Fatal("ordinary policy unexpectedly enabled native compaction")
	}
	if !compactNativePolicy(compact) {
		t.Fatal("compact policy did not enable native compaction")
	}
	f := fn{policy: compact}
	if got := f.finalizerDeletionLimit(); got != shared.MaxWideOffsetMapDeletions {
		t.Fatalf("Size finalizer deletion limit = %d, want %d", got, shared.MaxWideOffsetMapDeletions)
	}
	finalizerDeletionLimitOverride = 64
	if got := f.finalizerDeletionLimit(); got != 64 {
		t.Fatalf("finalizer deletion limit override = %d, want 64", got)
	}
	finalizerDeletionLimitOverride = 0
	if got := finalizerRel32Limit(compact); got != 2048 {
		t.Fatalf("Size rel32 site limit = %d, want 2048", got)
	}
	finalizerRel32SiteLimitOverride = 1536
	if got := finalizerRel32Limit(compact); got != 1536 {
		t.Fatalf("rel32 experiment limit = %d, want 1536", got)
	}
	if got := loopCompactionLimit(compact); got != 64<<10 {
		t.Fatalf("Size loop compaction limit = %d, want 64 KiB", got)
	}
	if got := (&fn{policy: compact}).jumpTableBranchRelaxationIterations(); got != 1 {
		t.Fatalf("Size jump-table relaxation iterations = %d, want 1", got)
	}
	if got := (&fn{policy: compact}).jumpTableBranchRelaxationLimit(); got != 32 {
		t.Fatalf("Size jump-table branch budget = %d, want 32", got)
	}
	finalizerRel32SiteLimitOverride = 256
	loopCompactionByteLimitOverride = 16 << 10
	if got := finalizerRel32Limit(compact); got != 256 {
		t.Fatalf("rel32 rollback limit = %d, want 256", got)
	}
	if got := loopCompactionLimit(compact); got != 16<<10 {
		t.Fatalf("loop rollback limit = %d, want 16 KiB", got)
	}

	nativeCompactionEnabled = true
	if !compactNativePolicy(ordinary) {
		t.Fatal("WAGO_COMPACT=1 override did not enable compaction")
	}
	nativeCompactionDisabled = true
	if compactNativePolicy(compact) || compactNativePolicy(ordinary) {
		t.Fatal("WAGO_COMPACT=0 rollback did not disable compaction")
	}
}

func TestFunctionStartPaddingPolicyAMD64(t *testing.T) {
	selection := currentCodegenPolicy().Selection
	ordinary := shared.DefaultCodegenPolicy(selection)
	compact := shared.CompactCodegenPolicy(selection)
	hot := funcHints{flags: hintHasLoop}
	for _, test := range []struct {
		name      string
		off       int
		bodyBytes int
		adapter   bool
		hints     funcHints
		policy    CodegenPolicy
		want      int
	}{
		{name: "ordinary tiny leaf", off: 3, bodyBytes: 12, policy: ordinary, want: 0},
		{name: "ordinary adapter", off: 3, bodyBytes: 12, adapter: true, policy: ordinary, want: 13},
		{name: "ordinary hot within budget", off: 12, bodyBytes: 64, hints: hot, policy: ordinary, want: 4},
		{name: "ordinary hot over budget", off: 3, bodyBytes: 64, hints: hot, policy: ordinary, want: 0},
		{name: "compact", off: 3, bodyBytes: 512, adapter: true, hints: hot, policy: compact, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := functionStartPadding(test.off, test.bodyBytes, test.adapter, test.hints, test.policy); got != test.want {
				t.Fatalf("padding = %d, want %d", got, test.want)
			}
		})
	}
}

func TestCompactLayoutSerialParallelParityAMD64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x41, 0x02, 0x6a, 0x0b}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x41, 0x03, 0x6a, 0x0b}},
	)
	compile := func(compact bool, workers int) []byte {
		cm, err := CompileModuleWith(m, CompileOptions{CompactNative: compact, Workers: workers})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		return append([]byte(nil), cm.Code...)
	}
	ordinary := compile(false, 1)
	compactSerial := compile(true, 1)
	compactParallel := compile(true, 3)
	if !bytes.Equal(compactSerial, compactParallel) {
		t.Fatal("compact layout differs between serial and parallel compilation")
	}
	if len(compactSerial) > len(ordinary) {
		t.Fatalf("compact output = %d bytes, ordinary = %d; want no larger compact layout", len(compactSerial), len(ordinary))
	}
}

func TestAccumulatorImmediateCompactionAndRollbackAMD64(t *testing.T) {
	selection := currentCodegenPolicy().Selection
	ordinary := shared.DefaultCodegenPolicy(selection)
	compact := shared.CompactCodegenPolicy(selection)
	if compactAccumulatorImmediatePolicy(ordinary) {
		t.Fatal("ordinary policy unexpectedly enabled accumulator immediates")
	}
	if !compactAccumulatorImmediatePolicy(compact) {
		t.Fatal("compaction did not enable accumulator immediates")
	}
	disabled, err := optimizationBindings.ResolveSnapshot(map[string]bool{"accumulator-immediate": false}, OptimizationSnapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	compactDisabled := shared.CompactCodegenPolicy(disabled)
	if compactAccumulatorImmediatePolicy(compactDisabled) {
		t.Fatal("per-compilation rollback did not disable accumulator immediates")
	}
}

func TestModuleCodeCapacityIsCompactionAwareAMD64(t *testing.T) {
	selection := currentCodegenPolicy().Selection
	ordinary := shared.DefaultCodegenPolicy(selection)
	compact := shared.CompactCodegenPolicy(selection)
	const bodyBytes = 8 << 20
	ordinaryCap := moduleCodeCapacityAMD64(bodyBytes, 1000, ordinary)
	compactCap := moduleCodeCapacityAMD64(bodyBytes, 1000, compact)
	if compactCap >= ordinaryCap {
		t.Fatalf("compact module capacity = %d, want less than ordinary %d", compactCap, ordinaryCap)
	}
	if got, want := moduleCodeCapacityAMD64(100, 3, compact), moduleCodeCapacityAMD64(100, 3, ordinary); got != want {
		t.Fatalf("small-module compact capacity = %d, want ordinary %d", got, want)
	}
}
