//go:build linux && amd64 && tinygo && !wago_profile

package wago

import (
	"fmt"
	gruntime "runtime"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// This observes the production host finalizer, not Runtime's asynchronous Close
// task. No code in this test calls Compiled.Close or replaces its finalizer.
// Scope: the non-profile, import-free scalar module's audited ownership graph.
func TestTinyGoCompiledFinalizerClosesMapping(t *testing.T) {
	for _, mode := range []string{"compile-only", "mapped", "activated"} {
		for round := 0; round < 4; round++ {
			// TinyGo scans the whole live task stack, including dead frames.
			// Terminate the creator task instead of retaining its stale owner pointer.
			type result struct {
				cache *compiledCodeCache
				err   error
			}
			ch := make(chan result)
			go func() { cache, err := tinyGoUnreachableCompiledCache(mode); ch <- result{cache, err} }()
			r := <-ch
			cache, err := r.cache, r.err
			if err != nil {
				t.Fatalf("mode=%s round=%d: %v", mode, round, err)
			}
			cache.mu.Lock()
			t.Logf("mode=%s round=%d initialMappedBytes=%d sealed=%v", mode, round, len(cache.mem), cache.sealed)
			cache.mu.Unlock()
			deadline := time.Now().Add(15 * time.Second)
			var closed bool
			var bytes, refs int
			for attempt := 0; attempt < 200 && time.Now().Before(deadline); attempt++ {
				gruntime.GC()
				gruntime.Gosched()
				cache.mu.Lock()
				closed, bytes, refs = cache.closed, len(cache.mem), cache.refs
				cache.mu.Unlock()
				if closed && bytes == 0 && refs == 0 {
					break
				}
			}
			gruntime.KeepAlive(cache)
			if !closed || bytes != 0 || refs != 0 {
				t.Fatalf("real compiled finalizer not observed: mode=%s round=%d closed=%v mappedBytes=%d refs=%d; conservative retention or cleanup failure", mode, round, closed, bytes, refs)
			}
		}
	}
}

//go:noinline
func tinyGoUnreachableCompiledCache(mode string) (*compiledCodeCache, error) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}))),
	)
	compiled, err := Compile(source)
	if err != nil {
		return nil, err
	}
	// The sidecar shares an allocation with the cleared staging Compiled and
	// validateMemo. Its memo owns a separate execution snapshot, never the
	// published Compiled which has the production finalizer. Check that crucial
	// distinction instead of accidentally retaining the object under test.
	if compiled.executionView() == compiled {
		return nil, fmt.Errorf("expected distinct private execution snapshot")
	}
	if mode == "mapped" {
		if err := compiled.prepareCodeMapping(); err != nil {
			return nil, err
		}
	}
	if mode == "activated" {
		instance, err := Instantiate(compiled)
		if err != nil {
			return nil, err
		}
		got, invokeErr := instance.Invoke("f", 41)
		closeErr := instance.Close()
		if invokeErr != nil || closeErr != nil || len(got) != 1 || got[0] != 42 {
			return nil, fmt.Errorf("instance verification: result=%v invoke=%v close=%v", got, invokeErr, closeErr)
		}
	}
	cache := compiled.loadCodeCache()
	if cache == nil {
		return nil, fmt.Errorf("missing code cache")
	}
	cache.mu.Lock()
	initialBytes, sealed := len(cache.mem), cache.sealed
	ready := !cache.closed && cache.refs == 0
	if mode != "compile-only" {
		ready = ready && initialBytes != 0
	}
	if mode == "mapped" {
		ready = ready && !sealed
	}
	if mode == "activated" {
		ready = ready && sealed
	}
	cache.mu.Unlock()
	if !ready {
		return nil, fmt.Errorf("invalid initial cache: mode=%s closed=%v mappedBytes=%d sealed=%v refs=%d", mode, cache.closed, initialBytes, sealed, cache.refs)
	}
	gruntime.KeepAlive(compiled)
	return cache, nil
}
