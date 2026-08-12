package wago

import (
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

// A compiled artifact is intentionally reusable by bounded instance pools.
// The first instantiation also seals a compiler-produced writable code image,
// so concurrent first users exercise both publication and reference counting.
func TestConcurrentInstantiateSharedCompiled(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("value", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x2a, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	const workers = 16
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			instance, err := Instantiate(compiled)
			if err != nil {
				errs <- err
				return
			}
			defer instance.Close()
			values, err := instance.Invoke("value")
			if err != nil {
				errs <- err
				return
			}
			if len(values) != 1 || AsI32(values[0]) != 42 {
				errs <- &concurrentInstantiateResultError{values: values}
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

type concurrentInstantiateResultError struct{ values []uint64 }

func (e *concurrentInstantiateResultError) Error() string {
	return "concurrent shared-Compiled invocation returned an unexpected value"
}
