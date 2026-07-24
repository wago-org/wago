package wago

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// Port of wazero's concurrent compilation, instantiation, and execution hammer.
// Each worker builds distinct code and checks an exact arithmetic oracle; sharing
// one Runtime makes registry/config/compiler races observable under -race.
func TestWazeroPortConcurrentCompileInstantiateExecute(t *testing.T) {
	workers := 16
	iterations := 50
	if testing.Short() {
		workers, iterations = 4, 10
	}
	rt := NewRuntime()
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})

	start := make(chan struct{})
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for p := 0; p < workers; p++ {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for n := 1; n <= iterations; n++ {
				want := (p + 1) * n
				body := []byte{0x01, 0x01, 0x7f} // one i32 local
				for i := 0; i < want; i++ {
					body = append(body, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00)
				}
				body = append(body, 0x20, 0x00, 0x0b)
				code := append(wasmtest.ULEB(uint32(len(body))), body...)
				mod := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
					wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec(code)),
				)
				compiled, err := rt.Compile(mod)
				if err != nil {
					errCh <- fmt.Errorf("worker %d iteration %d compile: %w", p, n, err)
					return
				}
				in, err := rt.Instantiate(context.Background(), compiled)
				if err != nil {
					_ = compiled.Close()
					errCh <- fmt.Errorf("worker %d iteration %d instantiate: %w", p, n, err)
					return
				}
				got, callErr := in.Invoke("f")
				instanceCloseErr := in.Close()
				compiledCloseErr := compiled.Close()
				if callErr != nil || len(got) != 1 || AsI32(got[0]) != int32(want) {
					errCh <- fmt.Errorf("worker %d iteration %d f() = %v, %v; want %d", p, n, got, callErr, want)
					return
				}
				if instanceCloseErr != nil {
					errCh <- fmt.Errorf("worker %d iteration %d close instance: %w", p, n, instanceCloseErr)
					return
				}
				if compiledCloseErr != nil {
					errCh <- fmt.Errorf("worker %d iteration %d close compiled module: %w", p, n, compiledCloseErr)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}
