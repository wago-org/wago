//go:build (linux || darwin) && arm64

package arm64

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
			got, err := runArm64WrapperWithOptions(t, m, opts, n|0xdeadbeef00000000, 0xfffffff0)
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

func TestTailLoopInlineDivisionConstantsRemainImmutable(t *testing.T) {
	if profileEnabled {
		t.Skip("experimental tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	defer func() { tailLoopInlineEnabled = old }()
	params := []wasm.ValType{wasm.I32, wasm.I32}
	results := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}},
		funcDef{params, results, []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05, 0x20, 0, 0x41, 10, 0x6e, 0x20, 1, 0x20, 0, 0x41, 10, 0x70, 0x6a, 0x12, 1, 0x0b, 0x0b}})
	for _, enabled := range []bool{false, true} {
		tailLoopInlineEnabled = enabled
		for _, n := range []uint32{0, 1, 9, 10, 11, 127, 12345, 0x7fffffff, 0x80000000, 0xffffffff} {
			opts := CompileOptions{}
			var stats ModuleStats
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runArm64WrapperWithOptions(t, m, opts, uint64(n)|0xdeadbeef00000000, 0xfffffff0)
			want := uint32(0xfffffff0)
			for value := n; value != 0; value /= 10 {
				want += value % 10
			}
			if err != nil || got != uint64(want) {
				t.Fatalf("enabled=%t n=%d got=%d,%v want %d", enabled, n, got, err, want)
			}
			if diagnosticsEnabled && enabled && stats.Funcs[0].Peephole["tail-inline-div-const"] < 2 {
				t.Fatal("division constant leases were not exercised")
			}
		}
	}
}

func TestTailLoopInlineOverwrittenLocalNeedsNoZero(t *testing.T) {
	if profileEnabled {
		t.Skip("experimental tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	tailLoopInlineEnabled = true
	defer func() { tailLoopInlineEnabled = old }()
	params := []wasm.ValType{wasm.I32, wasm.I32}
	results := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}},
		funcDef{params, results, []byte{1, 1, 0x7f, 0x20, 1, 0x41, 1, 0x6a, 0x21, 2,
			0x20, 0, 0x45, 0x04, 0x7f, 0x20, 2, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x20, 2, 0x12, 1, 0x0b, 0x0b}})
	for _, n := range []uint64{0, 1, 2, 31, 100000} {
		opts := CompileOptions{}
		var stats ModuleStats
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		got, err := runArm64WrapperWithOptions(t, m, opts, n|0xdeadbeef00000000, 0xfffffff0)
		want := uint64(uint32(0xfffffff0 + n + 1))
		if err != nil || got != want {
			t.Fatalf("n=%d got=%d,%v want %d", n, got, err, want)
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["tail-inline-zero-elide"] < 2 {
			t.Fatal("unneeded local zeros remain")
		}
	}
}

func TestTailLoopInlineParallelAndIdentityArguments(t *testing.T) {
	if profileEnabled {
		t.Skip("experimental tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	tailLoopInlineEnabled = true
	defer func() { tailLoopInlineEnabled = old }()
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}
	results := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x10, 1, 0x0b}},
		funcDef{params, results, []byte{0, 0x20, 0, 0x45, 0x04, 0x7f,
			0x20, 1, 0x41, 2, 0x6c, 0x20, 2, 0x41, 3, 0x6c, 0x6a, 0x20, 3, 0x41, 5, 0x6c, 0x6a,
			0x05, 0x20, 0, 0x41, 1, 0x6b, 0x20, 2, 0x20, 1, 0x41, 7, 0x6a, 0x20, 3, 0x12, 1, 0x0b, 0x0b}})
	for _, n := range []uint64{0, 1, 2, 3, 31, 511, 100000} {
		opts := CompileOptions{}
		var stats ModuleStats
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		a, b, c := uint32(0xfffffff0), uint32(0x12345678), uint32(0xabcdef01)
		got, err := runArm64WrapperWithOptions(t, m, opts, n|0xdeadbeef00000000, uint64(a)|0xdeadbeef00000000, uint64(b)|0xdeadbeef00000000, uint64(c)|0xdeadbeef00000000)
		for range n {
			a, b = b, a+7
		}
		want := uint64(2*a + 3*b + 5*c)
		if err != nil || got != want {
			t.Fatalf("n=%d got=%d,%v want %d", n, got, err, want)
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-tail-identity-arg"] < 1 {
			t.Fatal("unchanged argument was copied")
		}
	}
}
