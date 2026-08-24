//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func BenchmarkExecSharedAdapterTailAMD64(b *testing.B) {
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	m := modFuncs(b,
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x01, 0x41, 0x0b, 0x0b}},
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x02, 0x41, 0x0c, 0x0b}},
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x03, 0x41, 0x0d, 0x0b}},
	)
	m.Exports = append(m.Exports,
		wasm.Export{Name: "g", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}},
		wasm.Export{Name: "h", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 2}},
	)
	for _, tc := range []struct {
		name    string
		compact bool
	}{
		{"ordinary", false},
		{"compact", true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			cm, err := CompileModuleWith(m, CompileOptions{CompactNative: tc.compact})
			if err != nil {
				b.Fatal(err)
			}
			eng, err := coreruntime.NewEngine()
			if err != nil {
				b.Fatal(err)
			}
			defer eng.Close()
			jm, err := coreruntime.NewJobMemory(65536)
			if err != nil {
				b.Fatal(err)
			}
			defer jm.Close()
			arena, err := coreruntime.NewArena(4096)
			if err != nil {
				b.Fatal(err)
			}
			defer arena.Close()
			code, entry, err := coreruntime.MapCode(cm.Code)
			if err != nil {
				b.Fatal(err)
			}
			defer coreruntime.Unmap(code)
			results, trap := arena.Alloc(16), arena.Alloc(8)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), nil, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if got0, got1 := binary.LittleEndian.Uint64(results), binary.LittleEndian.Uint64(results[8:]); got0 != 1 || got1 != 11 {
				b.Fatalf("results = %d,%d, want 1,11", got0, got1)
			}
		})
	}
}
