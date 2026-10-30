//go:build amd64

package amd64

// This file is intentionally self-contained and also compiles on 9713878ab.
// Copy only this file to that checkout for the baseline: off runs identically,
// on skips explicitly when the option is absent. No baseline source edits needed.
import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func lifetimeFixture(tb testing.TB, kind string, count int) *wasm.Module {
	tb.Helper()
	body := []byte{0x41, 42, 0xfb, 0, 0, 0x21, 0, 0x20, 0, 0x22, 1, 0x1a, 0x01, 0x20, 1, 0xfb, 2, 0, 0, 0x0b}
	if kind == "nonqualifying" {
		body = []byte{0x41, 42, 0xfb, 0, 0, 0x21, 0, 0x02, 0x40, 0x01, 0x0b, 0x20, 0, 0xfb, 2, 0, 0, 0x0b}
	}
	if kind == "no-gc" {
		body = []byte{0x41, 42, 0x0b}
	}
	// Two nullable references of type 0, separate declaration from expression.
	code := append([]byte{1, 2, 0x63, 0}, body...)
	codes := make([][]byte, count)
	funcs := make([][]byte, count)
	for i := range codes {
		codes[i] = append(wasmtest.ULEB(uint32(len(code))), code...)
		funcs[i] = []byte{1}
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(10, wasmtest.Vec(codes...)))
	m, err := wasm.DecodeModule(data)
	if err != nil {
		tb.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	return m
}

func BenchmarkGCLifetimeCompile(b *testing.B) {
	for _, kind := range []string{"qualifying", "nonqualifying", "no-gc"} {
		b.Run(kind, func(b *testing.B) {
			m := lifetimeFixture(b, kind, 32)
			for _, on := range []bool{false, true} {
				name := "off"
				if on {
					name = "on"
				}
				b.Run(name, func(b *testing.B) {
					opts := CompileOptions{GCStructHelpers: true, DeferCodeMapping: true, Workers: 1}
					found := false
					selection := map[string]bool{}
					for _, k := range OptKnobs() {
						selection[k.Name] = k.On
						if k.Name == "gc-lifetime-analyze" {
							found = true
							selection[k.Name] = on
						}
					}
					if on && !found {
						b.Skip("baseline has no gc-lifetime-analyze; use off as oracle")
					}
					opts.Optimizations = selection
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						cm, err := CompileModuleWith(m, opts)
						if err != nil {
							b.Fatal(err)
						}
						if cm.CodeImage != nil {
							if err = cm.CodeImage.Close(); err != nil {
								b.Fatal(err)
							}
						}
					}
				})
			}
		})
	}
}
