//go:build linux && amd64 && wago_experimental_owned_rw_staging

package wago

import (
	"bytes"
	"errors"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"reflect"
	"testing"
	"unsafe"
)

func TestOwnedRWStagingSelector(t *testing.T) {
	for value, want := range map[string]bool{"": false, "0": false, "1": true} {
		if parseOwnedRWStaging(value) != want {
			t.Fatal(value)
		}
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("invalid value accepted")
			}
		}()
		parseOwnedRWStaging("2")
	}()
	for _, workers := range []int{0, 1, 2, 8} {
		for _, on := range []bool{false, true} {
			opts := railshotCompileOptions{Workers: workers, DeferCodeMapping: true}
			configureOwnedRWStagingMode(&opts, on)
			if opts.DeferCodeMapping == (on && workers == 1) {
				t.Fatalf("workers=%d on=%v", workers, on)
			}
		}
	}
}
func TestOwnedRWStagingPublicationAndReuse(t *testing.T) {
	// These tests are deliberately serial: selection is startup-only in ordinary
	// use; save/restore here permits both compilation modes in one test process.
	saved := ownedRWStaging
	defer func() { ownedRWStaging = saved }()
	cfg := NewRuntimeConfig().WithFunctionWorkers(1).WithBoundsChecks(BoundsChecksExplicit)
	ownedRWStaging = false
	heap, err := Compile(cfg, fibWasm)
	if err != nil {
		t.Fatal(err)
	}
	defer heap.Close()
	ownedRWStaging = true
	mapped, err := Compile(cfg, fibWasm)
	if err != nil {
		t.Fatal(err)
	}
	defer mapped.Close()
	if len(heap.codeCache.mem) != 0 || len(mapped.codeCache.mem) == 0 || mapped.codeCache.flags&compiledCacheWritableCode == 0 {
		t.Fatal("unexpected staging ownership/state")
	}
	if !bytes.Equal(heap.code, mapped.code) || !reflect.DeepEqual(heap.Entry, mapped.Entry) || !reflect.DeepEqual(heap.InternalEntry, mapped.InternalEntry) {
		t.Fatal("staging changed native bytes/entries")
	}
	address := unsafe.Pointer(&mapped.code[0])
	for i := 0; i < 2; i++ {
		in, err := Instantiate(mapped, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		result, callErr := in.Invoke("fib", I32(10))
		closeErr := in.Close()
		if callErr != nil || closeErr != nil || len(result) != 1 || result[0] != 55 {
			t.Fatalf("reuse result=%v call=%v close=%v", result, callErr, closeErr)
		}
		if unsafe.Pointer(&mapped.code[0]) != address || !mapped.codeCache.sealed {
			t.Fatal("activation copied mapping or failed to seal")
		}
	}
	if err := mapped.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mapped.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Instantiate(mapped, InstantiateOptions{}); err == nil {
		t.Fatal("closed mapping reused")
	}
}

// Exercise the exact post-Take publication failure, without waiting for GC or
// finalizers. This is ownership plumbing, not a fabricated GC-root failure.
func TestOwnedRWStagingPostTakePublicationFailure(t *testing.T) {
	image, err := coreruntime.NewCodeBuffer(1)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	if err = image.Append([]byte{0xc3}); err != nil {
		t.Fatal(err)
	}
	code := image.Bytes()
	mapping, base, err := image.Take()
	if err != nil {
		t.Fatal(err)
	}
	staged := newCompilerCompiled()
	staged.code = code
	staged.codeCache.mem = mapping
	staged.codeCache.base = base
	staged.codeCache.flags |= compiledCacheWritableCode
	cache := staged.codeCache
	// The snapshot budget fails after ownership has moved into Compiled.
	staged.validateMemo.snapshotLimit = 1
	staged.Data = []DataInit{{Bytes: make([]byte, 4096)}}
	unmaps := 0
	staged.ensureCompileIndexes().unmapCode = func(mem []byte) error { unmaps++; return coreruntime.Unmap(mem) }
	defer func() {
		if cache.mem != nil {
			_ = coreruntime.Unmap(cache.mem)
			cache.mem = nil
		}
	}()
	installCompilerCompiledFinalizer(staged)
	published, err := publishCompilerCompiled(staged)
	if published != nil {
		defer published.Close()
	}
	if err == nil || !errors.Is(err, ErrResourceLimit) || published != nil {
		t.Fatalf("publication failure=%v result=%v", err, published)
	}
	if unmaps != 1 || cache.mem != nil || cache.base != 0 || !cache.closed {
		t.Fatalf("post-Take cleanup not synchronous: unmaps=%d retained=%v base=%x closed=%v", unmaps, cache.mem != nil, cache.base, cache.closed)
	}
	if err = image.Close(); err != nil {
		t.Fatal(err)
	}
	if unmaps != 1 {
		t.Fatal("transferred image attempted second owner cleanup")
	}
}
