//go:build linux && amd64

package amd64

import (
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRegisterOnlyMergeConcurrentCompiles(t *testing.T) {
	old := callFreeRegMergesEnabled
	callFreeRegMergesEnabled = true
	t.Cleanup(func() { callFreeRegMergesEnabled = old })
	i32 := []wasm.ValType{wasm.I32}
	body := []byte{
		1, 1, 0x7f,
		0x20, 0, 0x21, 1,
		0x02, 0x40, 0x03, 0x40,
		0x20, 1, 0x45, 0x0d, 1,
		0x20, 1, 0x41, 1, 0x6b, 0x21, 1,
		0x0c, 0, 0x0b, 0x0b, 0x20, 0, 0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(i32, i32))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pass := 0; pass < 4; pass++ {
				m, err := wasm.DecodeModule(data)
				if err != nil {
					errs <- err
					return
				}
				if err = wasm.ValidateModule(m); err != nil {
					errs <- err
					return
				}
				compiled, err := CompileModule(m)
				if err != nil {
					errs <- err
					return
				}
				if err = compiled.CodeImage.Close(); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	for i, state := range registerOnlyMergeStates {
		if state != lsReg {
			t.Fatalf("shared state %d mutated to %d", i, state)
		}
	}
}
