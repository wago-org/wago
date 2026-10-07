//go:build amd64 && (linux || darwin || windows)

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func BenchmarkCompileInertPrefixLocalWrites(b *testing.B) {
	const n = 8192
	body := []byte{0}
	for i := 0; i < n; i++ {
		body = append(body, 0x41, 1)
	}
	for i := 0; i < n; i++ {
		body = append(body, 0x41, 1, 0x21, 0)
	}
	for i := 0; i < n; i++ {
		body = append(body, 0x1a)
	}
	body = append(body, 0x0b)
	m := benchDecodeValidateModule(b, benchModuleBytes([]benchFuncDef{{params: []wasm.ValType{wasm.I32}, body: body}}, false))
	benchmarkCompileModule(b, m)
}

func TestLocalReferenceSummaryRestoredIfPrefix(t *testing.T) {
	savedShared, savedPrefix := sharedScalarEnabled, ifPrefixRematEnabled
	sharedScalarEnabled, ifPrefixRematEnabled = false, true
	defer func() { sharedScalarEnabled, ifPrefixRematEnabled = savedShared, savedPrefix }()
	for _, manyLocals := range []bool{false, true} {
		for _, affine := range []bool{false, true} {
			body := []byte{0}
			if manyLocals {
				body = []byte{1, 65, 0x7f}
			}
			body = append(body, 0x20, 0)
			if affine {
				body = append(body, 0x41, 17, 0x6a)
			}
			body = append(body, 0x20, 1, 0x04, 0x7f, 0x41, 7, 0x05, 0x41, 11, 0x0b,
				0x41, 0xe3, 0, 0x21, 0, 0x6a, 0x0b)
			m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, compact := range []bool{false, true} {
				for _, merge := range []bool{false, true} {
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, CompactNative: compact, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"reg-merge": merge}})
					if err != nil {
						t.Fatal(err)
					}
					if diagnosticsEnabled && stats.Funcs[0].Peephole["if-prefix-remat"] != 1 {
						t.Fatal("fixture did not restore an if prefix")
					}
					for _, x := range []uint64{0, 42, 0xffffffff} {
						for _, condition := range []uint64{0, 1} {
							want := uint32(x) + 11
							if condition != 0 {
								want = uint32(x) + 7
							}
							if affine {
								want += 17
							}
							if got := runCompiledAmd64u(t, cm, x, condition); uint32(got) != want {
								t.Fatalf("manyLocals=%v affine=%v compact=%v merge=%v x=%d condition=%d: got=%d want=%d", manyLocals, affine, compact, merge, x, condition, uint32(got), want)
							}
						}
					}
					if cm.CodeImage != nil {
						if err := cm.CodeImage.Close(); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
		}
	}
}
