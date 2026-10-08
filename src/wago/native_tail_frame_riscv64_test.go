//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestRV64PublicNativeTailActualSourceFrameBeforePublication(t *testing.T) {
	c := rv64NativeTailCompile(t, "c2q-source-frame", rv64PublicTailModule([]wasm.ValType{wasm.I32}, false), false)
	v := cloneCompiledMetadata(c)
	v.validateMemo, v.codeCache = &validateMemo{}, nil
	p := rv64NativeTailModule(c).Plans[0]
	got, err := railshot.NativeFrameBytes(v.code, v.Entry[int(p.FunctionIndex)-v.NumImports])
	if err != nil || got != p.FrameBytes {
		t.Fatal("real source frame", got, p.FrameBytes, err)
	}
	p.FrameBytes += 16 // Still structurally valid symbolic metadata.
	rv64StoreNativeTailModule(v, &encoder.NativeTailModule{Plans: []encoder.NativeTailPlan{p}})
	if err := rv64ValidateNativeTail(v); err != nil {
		t.Fatal("wrong-frame regression did not reach native-byte check", err)
	}
	cache := v.loadCodeCache()
	base, sealed, refs := cache.base, cache.sealed, cache.refs
	if in, err := Instantiate(v, testImports("env.tail", HostCallFunc(func(HostCall) {}))); in != nil || !errors.Is(err, railshot.ErrUnsupported) || cache.base != base || cache.sealed != sealed || cache.refs != refs {
		t.Fatal("mismatched source frame reached executable publication", err)
	}
}
