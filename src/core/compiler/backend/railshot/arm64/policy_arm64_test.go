//go:build arm64

package arm64

import (
	"bytes"
	"fmt"
	"sync"
	"testing"

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
