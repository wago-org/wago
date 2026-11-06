//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestTailLoopInlineCapabilityGates(t *testing.T) {
	requireCompilerDiagnostics(t)
	params, results := []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}},
		funcDef{params, results, []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 17, 0x6a, 0x12, 1, 0x0b, 0x0b}})
	for _, tc := range []struct {
		name string
		opts CompileOptions
		want bool
	}{
		{"default", CompileOptions{}, !profileEnabled},
		{"option-off", CompileOptions{Optimizations: map[string]bool{"tail-loop-inline": false}}, false},
		{"inline-off", CompileOptions{Optimizations: map[string]bool{"inline": false}}, false},
		{"self-loop-off", CompileOptions{Optimizations: map[string]bool{"tail-self-loop": false}}, false},
		{"compact", CompileOptions{CompactNative: true}, false},
		{"interruptible", CompileOptions{Interruptible: true}, false},
		{"sources", CompileOptions{Profile: true, SourceMaps: true}, false},
		{"unwind", CompileOptions{Profile: true, UnwindMaps: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.opts.Profile && !profileEnabled {
				t.Skip("source and unwind maps require a profiling build")
			}
			var stats ModuleStats
			tc.opts.Stats = &stats
			compiled, err := CompileModuleWith(m, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["inline-tail-loop"] != 0; got != tc.want {
				t.Fatalf("inline=%t want %t", got, tc.want)
			}
		})
	}
}

func TestTailLoopInlineResetsLocalsAndRoutesFunctionLabels(t *testing.T) {
	if profileEnabled {
		t.Skip("experimental tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	defer func() { tailLoopInlineEnabled = old }()
	params := []wasm.ValType{wasm.I32, wasm.I32}
	results := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}},
		funcDef{params, results, []byte{1, 1, 0x7f,
			0x20, 0, 0x45, 0x04, 0x40,
			0x20, 1, 0x20, 2, 0x6a, 0x0c, 1,
			0x05, 0x41, 0xe3, 0, 0x21, 2,
			0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 17, 0x6a, 0x12, 1, 0x0b, 0x0b}})
	for _, enabled := range []bool{false, true} {
		tailLoopInlineEnabled = enabled
		for _, n := range []uint64{0, 1, 2, 31, 511, 100000} {
			var stats ModuleStats
			opts := CompileOptions{}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runAmd64TailInlineWithOptions(t, m, opts, n|0xdeadbeef00000000, 0xfffffff0)
			want := uint64(uint32(0xfffffff0 + n*17))
			if err != nil || got != want {
				t.Fatalf("enabled=%t n=%d got=%d,%v want %d", enabled, n, got, err, want)
			}
			if diagnosticsEnabled {
				count := stats.Funcs[0].Peephole["inline-tail-loop"]
				if (count != 0) != enabled {
					t.Fatalf("enabled=%t inline tail selections=%d", enabled, count)
				}
			}
		}
	}
}

func runAmd64TailInlineWithOptions(t *testing.T, m *wasm.Module, opts CompileOptions, args ...uint64) (uint64, error) {
	t.Helper()
	c, err := CompileModuleWith(m, opts)
	if err != nil {
		return 0, err
	}
	if c.CodeImage != nil {
		defer c.CodeImage.Close()
	}
	return runCompiledAmd64u(t, c, args...), nil
}

func TestTailLoopInlinePermutationsAndCallerPrefix(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, block, add, mul := wasm.I32, byte(0x41), byte(0x7f), byte(0x6a), byte(0x6c)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, constant, block, add, mul = wasm.I64, 0x42, 0x7e, 0x7c, 0x7e
			fold = func(v uint64) uint64 { return v }
		}
		params := []wasm.ValType{wasm.I32, typ, typ}
		caller := []byte{0, constant, 5, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, add, 0x0b}
		helper := []byte{0, 0x20, 0, 0x45, 0x04, block, 0x20, 1, constant, 3, mul, 0x20, 2, add, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x20, 2, 0x20, 1, constant, 7, add, 0x12, 1, 0x0b, 0x0b}
		m := modFuncs(t, funcDef{params, []wasm.ValType{typ}, caller}, funcDef{params, []wasm.ValType{typ}, helper})
		for _, enabled := range []bool{false, true} {
			var stats ModuleStats
			c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"tail-loop-inline": enabled}})
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range []uint64{0, 1, 2, 31, 100000} {
				a, b := fold(0xfffffffffffffff0), fold(0x80000000cafebabe)
				aa, bb := a, b
				for range n {
					a, b = b, fold(a+7)
				}
				want := fold(3*a + b + 5)
				if !wide {
					aa |= 0x1234567800000000
					bb |= 0xdeadbeef00000000
				}
				if got := runCompiledAmd64u(t, c, n|0xcafebabe00000000, aa, bb); got != want {
					t.Fatalf("wide=%t enabled=%t n=%d got=%x want=%x", wide, enabled, n, got, want)
				}
			}
			if c.CodeImage != nil {
				c.CodeImage.Close()
			}
			if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tail-loop"] != 0) != (enabled && !profileEnabled) {
				t.Fatalf("wide=%t enabled=%t tail loop selection mismatch", wide, enabled)
			}
		}
	}
}

func TestTailLoopInlineDefiniteWriteElidesZeros(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, block, add := wasm.I32, byte(0x41), byte(0x7f), byte(0x6a)
		if wide {
			typ, constant, block, add = wasm.I64, 0x42, 0x7e, 0x7c
		}
		params, results := []wasm.ValType{wasm.I32, typ}, []wasm.ValType{typ}
		body := []byte{1, 1, block, 0x20, 0, 0x45, 0x04, block, 0x20, 1, 0x05, 0x20, 0}
		if wide {
			body = append(body, 0xad)
		}
		body = append(body, constant, 3, add, 0x21, 2, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x20, 2, add, 0x12, 1, 0x0b, 0x0b)
		m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}}, funcDef{params, results, body})
		for _, enabled := range []bool{false, true} {
			var stats ModuleStats
			c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"tail-loop-inline": enabled}})
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range []uint64{0, 1, 2, 31, 100000} {
				initial := uint64(0xfffffffffffffff0)
				want := initial + n*(n+1)/2 + 3*n
				if !wide {
					initial = uint64(uint32(initial)) | 0xcafebabe00000000
					want = uint64(uint32(want))
				}
				if got := runCompiledAmd64u(t, c, n|0xdeadbeef00000000, initial); got != want {
					t.Fatalf("wide=%t enabled=%t n=%d got=%x want=%x", wide, enabled, n, got, want)
				}
			}
			if c.CodeImage != nil {
				c.CodeImage.Close()
			}
			if diagnosticsEnabled && (stats.Funcs[0].Peephole["tail-inline-zero-elide"] != 0) != (enabled && !profileEnabled) {
				t.Fatalf("wide=%t enabled=%t overwritten tail locals still zeroed", wide, enabled)
			}
		}
	}
}
