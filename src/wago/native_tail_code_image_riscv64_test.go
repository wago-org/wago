//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64PublicNativeTailPublishedCodeImageLease(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) { rv64PublishedCodeImageLeaseForTest(t, artifact) })
	}
}

func rv64PublishedCodeImageLeaseForTest(t *testing.T, artifact bool) {
	t.Helper()
	c := rv64NativeTailCompile(t, "c2r-published-code", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
	in, err := Instantiate(c, NewImports())
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	other, err := Instantiate(c, NewImports())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	owner := rv64TailCodeOwner{in}
	if image, err := railshot.NewNativeTailCodeImage(owner); image != nil || !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("cache refs alone authorized an unleased instance", err)
	}
	if !in.retainResourceRoot() {
		t.Fatal("physical owner root")
	}
	root := true
	defer func() {
		if root {
			in.releaseResourceRoot()
		}
	}()
	image, err := railshot.NewNativeTailCodeImage(owner)
	if err != nil || image.Base() != in.base || len(image.Bytes()) != len(in.c.code) || !bytes.Equal(image.Bytes(), in.c.code) {
		t.Fatal("actual sealed logical cache view", err)
	}
	params, results := []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}
	if key, err := owner.NativeTailFunctionKey(0, params, results); err != nil || key != in.c.funcTypeKey(0) {
		t.Fatal("retained exact function key", key, err)
	}
	if _, err := owner.NativeTailFunctionKey(1, params, results); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("foreign function index accepted", err)
	}
	if _, err := owner.NativeTailFunctionKey(0, []wasm.ValType{wasm.I64}, results); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("wrong function transport accepted", err)
	}
	if n := testing.AllocsPerRun(4, func() {
		if _, err := owner.NativeTailFunctionKey(0, params, results); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("warm key lookup allocated", n)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	// Closing the retained execution snapshot clears its convenience code view.
	// It must not revoke the instance's particular physical mapping lease.
	if err := in.c.Close(); err != nil || in.c.code != nil || image.Validate() != nil {
		t.Fatal("execution snapshot close broke retained RX view", err)
	}
	if err := in.Close(); err != nil || in.resourcesClosed || image.Validate() != nil {
		t.Fatal("logical close broke physically retained image", err)
	}
	if n := testing.AllocsPerRun(4, func() {
		if err := image.Validate(); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("warm runtime image validation allocations", n)
	}
	in.releaseResourceRoot()
	root = false
	if !in.resourcesClosed || other.resourcesClosed || other.c.loadCodeCache().refs == 0 || !errors.Is(image.Validate(), railshot.ErrUnsupported) {
		t.Fatal("another instance's cache ref authorized physically retired owner")
	}
}

func TestRV64PublicNativeTailPublishedHandBuiltMapping(t *testing.T) {
	c := rv64NativeTailCompile(t, "c2r-hand-built-code", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), false)
	v := cloneCompiledMetadata(c)
	v.codeCache, v.validateMemo = nil, nil
	defer v.Close()
	n := len(v.code)
	if err := v.prepareCodeMapping(); err != nil {
		t.Fatal(err)
	}
	if v.loadValidateMemo() == nil || v.loadCompileIndexes().railshotTail.mappedCodeBytes != n || v.codeCache.sealed || offHeapSlicePtr(v.code) != v.codeCache.base {
		t.Fatal("hand-built exact RW mapping metadata")
	}
	in, err := Instantiate(v, NewImports())
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := in.beginInvocation(); err != nil {
		t.Fatal(err)
	}
	defer in.endInvocation()
	image, err := railshot.NewNativeTailCodeImage(rv64TailCodeOwner{in})
	if err != nil || len(image.Bytes()) != n || image.Base() != in.base {
		t.Fatal("hand-built published exact span", err)
	}
}

func TestRV64PublicNativeTailPublishedCodeRejectsUnpublishedViews(t *testing.T) {
	c := rv64NativeTailCompile(t, "c2r-code-view-guards", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), false)
	in, err := Instantiate(c, NewImports())
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !in.retainResourceRoot() {
		t.Fatal("physical owner root")
	}
	defer in.releaseResourceRoot()
	cache := in.c.loadCodeCache()
	owner := rv64TailCodeOwner{in}
	code, base, sealed, refs, context := in.c.code, cache.base, cache.sealed, cache.refs, in.nativeContext
	for _, tc := range []struct {
		name string
		edit func()
	}{
		{"unsealed", func() { cache.sealed = false }},
		{"no-code-ref", func() { cache.refs = 0 }},
		{"wrong-cache-base", func() { cache.base += 16 }},
		{"heap-code", func() { in.c.code = bytes.Clone(code) }},
		{"interior-code", func() { in.c.code = code[16:] }},
		{"shorter-logical-code", func() { in.c.code = code[:len(code)-4] }},
		{"page-padding-code", func() {
			if len(code)+4 > len(cache.mem) {
				t.Fatal("fixture has no page padding")
			}
			in.c.code = cache.mem[:len(code)+4]
		}},
		{"empty-code", func() { in.c.code = nil }},
		{"missing-context", func() { in.nativeContext = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.edit()
			image, err := railshot.NewNativeTailCodeImage(owner)
			in.c.code, cache.base, cache.sealed, cache.refs, in.nativeContext = code, base, sealed, refs, context
			if image != nil || !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("unpublished/replaced runtime mapping accepted", err)
			}
		})
	}
}
