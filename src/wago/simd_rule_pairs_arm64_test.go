//go:build (linux || darwin) && arm64 && !tinygo && !wago_precompiled

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func simdPairProfiles() []uint64 { return []uint64{0} }

func compileSIMDPair(t testing.TB, raw []byte, profile uint64) simdPairCode {
	t.Helper()
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var stats railshotModuleStats
	opts := railshotCompileOptions{Workers: 1, DeferCodeMapping: true}
	if compilerTelemetryEnabled {
		opts.Stats = &stats
	}
	cm, err := railshotCompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		t.Fatal("unexpected mapped code image")
	}
	out := simdPairCode{bytes: cm.Code, entry: uint32(cm.Entry[0]), spills: -1, literals: -1}
	if compilerTelemetryEnabled {
		if len(stats.Funcs) != 1 || stats.Funcs[0].SharedScalar {
			t.Fatal("expected established SIMD compiler")
		}
		out.spills = stats.Funcs[0].Spills
		out.literals = stats.Funcs[0].NativeSize.LiteralPoolBytes
	}
	return out
}
