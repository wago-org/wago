//go:build amd64

package amd64

import (
	"bytes"
	"fmt"
	"sync"
	"testing"

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
		defer cm.CodeImage.Close()
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

func TestCompileModuleRejectsInvalidObjectiveAMD64(t *testing.T) {
	m := mod1(t, nil, nil, []byte{0x00, 0x0b})
	objective := OptimizationObjective(255)
	if _, err := CompileModuleWith(m, CompileOptions{Objective: &objective}); err == nil {
		t.Fatal("invalid optimization objective was accepted")
	}
}
