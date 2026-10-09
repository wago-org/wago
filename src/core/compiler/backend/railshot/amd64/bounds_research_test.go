//go:build amd64 && (linux || darwin || windows)

package amd64

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func boundsResearchModule(t testing.TB, sources, repeats int) *wasm.Module {
	t.Helper()
	params := make([]wasm.ValType, sources)
	for i := range params {
		params[i] = wasm.I32
	}
	body := []byte{0}
	for r := 0; r < repeats; r++ {
		for i := 0; i < sources; i++ {
			body = append(body, 0x20, byte(i), 0x28, 2, 0, 0x1a)
		}
	}
	body = append(body, 0x0b)
	m := modMem(t, 1, params, nil, body)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBoundsFactsChangedSourceTrapAndMemory(t *testing.T) {
	// The first load proves p+4. A visible store precedes changing p to q.
	// A stale proof would make the last load miss the required bounds trap.
	body := []byte{0, 0x20, 0, 0x28, 2, 0, 0x1a,
		0x41, 0, 0x41, 42, 0x36, 2, 0,
		0x20, 1, 0x21, 0,
		0x20, 0, 0x28, 2, 0, 0x1a, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, nil, body)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, address := range []uint64{65532, 65533, 65536, 0xffffffff} {
		var firstMemory []byte
		var firstError string
		for _, disable := range []bool{false, true} {
			_, memory, err := runMemAmd64WithOptions(t, m, CompileOptions{NoBoundsFacts: disable}, nil, 0, address)
			if (err != nil) != (address > 65532) {
				t.Fatalf("address=%x disabled=%v: trap=%v", address, disable, err)
			}
			if memory[0] != 42 {
				t.Fatal("store before trap was lost")
			}
			message := fmt.Sprint(err)
			if !disable {
				firstMemory, firstError = memory, message
			} else if !bytes.Equal(firstMemory, memory) || firstError != message {
				t.Fatal("facts changed trap or memory")
			}
		}
	}
}

func TestBoundsFactsIndependentSourceChecks(t *testing.T) {
	requireCompilerDiagnostics(t)
	previous := multiBoundsCertEnabled
	multiBoundsCertEnabled = true
	defer func() { multiBoundsCertEnabled = previous }()
	for _, sources := range []int{1, 2, len((fn{}).boundsCerts), len((fn{}).boundsCerts) + 1} {
		t.Run(fmt.Sprint(sources), func(t *testing.T) {
			m := boundsResearchModule(t, sources, 2)
			for _, guard := range []bool{false, true} {
				for _, disable := range []bool{false, true} {
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: guard, NoBoundsFacts: disable, Stats: &stats})
					if err != nil {
						t.Fatal(err)
					}
					cm.CodeImage.Close()
					s := stats.Funcs[0]
					if s.SharedScalar {
						t.Fatal("fixture did not reach established memory compiler")
					}
					checks, elided := sources, sources
					if sources > len((fn{}).boundsCerts) || disable {
						checks, elided = 2*sources, 0
					}
					if guard {
						checks, elided = 0, 0
					}
					if int(s.BoundsChecks) != checks || int(s.BoundsChecksElidable) != elided {
						t.Fatalf("guard=%v disabled=%v: checks=%d elided=%d want %d/%d", guard, disable, s.BoundsChecks, s.BoundsChecksElidable, checks, elided)
					}
				}
			}
		})
	}
}

func BenchmarkBoundsFactsCompile(b *testing.B) {
	for _, sources := range []int{1, 2, 8, 9, 32} {
		m := boundsResearchModule(b, sources, 32)
		b.Run(fmt.Sprintf("sources=%d", sources), func(b *testing.B) { boundsResearchCompile(b, m) })
	}
	for _, name := range []string{"cjson", "coremark", "zstd", "wren"} {
		b.Run(name, func(b *testing.B) {
			path := filepath.Join("..", "..", "..", "..", "..", "..", "corpus", "workloads", "semantic", name, name+".wasm")
			data, err := os.ReadFile(path)
			if err != nil {
				b.Fatal(err)
			}
			m, err := wasm.DecodeModule(data)
			if err != nil {
				b.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				b.Fatal(err)
			}
			boundsResearchCompile(b, m)
		})
	}
}

func boundsResearchCompile(b *testing.B, m *wasm.Module) {
	b.Helper()
	for _, guard := range []bool{false, true} {
		b.Run(fmt.Sprintf("guard=%v", guard), func(b *testing.B) {
			opts := CompileOptions{Workers: 1, ElideBoundsChecks: guard}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					b.Fatal(err)
				}
				cm.CodeImage.Close()
			}
		})
	}
}
