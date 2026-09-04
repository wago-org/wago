//go:build arm64

package arm64

import (
	"bytes"
	"fmt"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestCompileModuleWithPoliciesDoNotCrossTalkArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x0b}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
	)

	compile := func(smallFrame bool) ([]byte, error) {
		cm, err := CompileModuleWith(m, CompileOptions{
			Workers: 1,
			Optimizations: map[string]bool{
				"frame-elide-reghomed": false,
				"small-frame":          smallFrame,
			},
		})
		if err != nil {
			return nil, err
		}
		return append([]byte(nil), cm.Code...), nil
	}

	compact, err := compile(true)
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := compile(false)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(compact, reserved) {
		t.Fatal("small-frame policies produced identical code; test cannot detect cross-talk")
	}

	before := CurrentOptKnobSnapshot()
	const goroutines = 8
	const iterations = 16
	errCh := make(chan error, goroutines)
	var wg sync.WaitGroup
	for worker := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			smallFrame := worker%2 == 0
			want := reserved
			if smallFrame {
				want = compact
			}
			for iteration := range iterations {
				got, err := compile(smallFrame)
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

func TestHiddenOptimizationFamiliesUsePerCompilePolicyArm64(t *testing.T) {
	names := []string{
		"simd-superopt", "interval-region-pins", "magic-div",
		"shared-trap-body", "shared-adapters", "zero-branch", "mul-add-fuse", "entry-init-elision",
		"v128-direct-results",
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

func TestNativeCompactionPolicyAndRollbackArm64(t *testing.T) {
	beforeEnabled, beforeDisabled := nativeCompactionEnabled, nativeCompactionDisabled
	beforeLimitOverride := finalizerDeletionLimitOverride
	nativeCompactionEnabled, nativeCompactionDisabled = false, false
	t.Cleanup(func() {
		nativeCompactionEnabled, nativeCompactionDisabled = beforeEnabled, beforeDisabled
		finalizerDeletionLimitOverride = beforeLimitOverride
	})

	selection := currentCodegenPolicy().Selection
	ordinary := fn{policy: shared.DefaultCodegenPolicy(selection)}
	compact := fn{policy: shared.CompactCodegenPolicy(selection)}
	if ordinary.compactNative() {
		t.Fatal("ordinary policy unexpectedly enabled native compaction")
	}
	if !compact.compactNative() {
		t.Fatal("compact policy did not enable native compaction")
	}
	if got := compact.finalizerDeletionLimit(); got != maxFinalizerDeletions {
		t.Fatalf("compact finalizer deletion limit = %d, want %d", got, maxFinalizerDeletions)
	}
	finalizerDeletionLimitOverride = 64
	if got := compact.finalizerDeletionLimit(); got != 64 {
		t.Fatalf("finalizer deletion limit override = %d, want 64", got)
	}
	finalizerDeletionLimitOverride = 0

	nativeCompactionEnabled = true
	if !ordinary.compactNative() {
		t.Fatal("WAGO_COMPACT=1 override did not enable compaction")
	}
	nativeCompactionDisabled = true
	if compact.compactNative() || ordinary.compactNative() {
		t.Fatal("WAGO_COMPACT=0 rollback did not disable compaction")
	}
}

func TestFunctionStartPaddingPolicyArm64(t *testing.T) {
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
		{name: "ordinary tiny leaf", off: 4, bodyBytes: 12, policy: ordinary, want: 0},
		{name: "ordinary adapter", off: 4, bodyBytes: 12, adapter: true, policy: ordinary, want: 12},
		{name: "ordinary hot within budget", off: 12, bodyBytes: 64, hints: hot, policy: ordinary, want: 4},
		{name: "ordinary hot over budget", off: 4, bodyBytes: 64, hints: hot, policy: ordinary, want: 0},
		{name: "compact", off: 4, bodyBytes: 512, adapter: true, hints: hot, policy: compact, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := functionStartPadding(test.off, test.bodyBytes, test.adapter, test.hints, test.policy); got != test.want {
				t.Fatalf("padding = %d, want %d", got, test.want)
			}
		})
	}
}

func TestCompactLayoutSerialParallelParityArm64(t *testing.T) {
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
		return append([]byte(nil), cm.Code...)
	}
	ordinary := compile(false, 1)
	compactSerial := compile(true, 1)
	compactParallel := compile(true, 3)
	if !bytes.Equal(compactSerial, compactParallel) {
		t.Fatal("compact layout differs between serial and parallel compilation")
	}
	if len(compactSerial) >= len(ordinary) {
		t.Fatalf("compact output = %d bytes, ordinary = %d; want smaller compact layout", len(compactSerial), len(ordinary))
	}
}
