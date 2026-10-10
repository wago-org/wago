//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func dotLoopModule(t testing.TB, terms int, start, limit uint32, biasA, biasB int32) *wasm.Module {
	// Parameter 0 is stream A; locals 1/2 are counter/accumulator, 3/4
	// retain the source iteration's address aliases, 5 is invariant stream B.
	body := []byte{1, 5, 0x7f}
	constant := func(v uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(v))...) }
	constant(start)
	body = append(body, 0x21, 1)
	constant(0xfffffff1)
	body = append(body, 0x21, 2)
	constant(2048)
	body = append(body, 0x21, 5, 0x03, 0x40, 0x20, 2)
	for i := 0; i < terms; i++ {
		for stream := 0; stream < 2; stream++ {
			base, alias, bias := byte(0), byte(3), biasA
			if stream == 1 {
				base, alias, bias = 5, 4, biasB
			}
			body = append(body, 0x20, base, 0x20, 1, 0x6a, 0x22, alias, 0x28, 2, byte(i*4))
			constant(uint32(bias))
			body = append(body, 0x6a)
		}
		body = append(body, 0x6c, 0x6a)
	}
	body = append(body, 0x21, 2, 0x20, 1)
	constant(uint32(terms * 4))
	body = append(body, 0x6a, 0x22, 1)
	constant(limit)
	body = append(body, 0x47, 0x0d, 0, 0x0b)
	// Include each final scratch local and counter in the observable result.
	body = append(body, 0x20, 2, 0x20, 3, 0x73, 0x20, 4, 0x73, 0x20, 1, 0x73, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
}

func TestDotLoopIndependentExecution(t *testing.T) {
	old := dotLoopEnabled
	dotLoopEnabled = true
	defer func() { dotLoopEnabled = old }()
	setup := func(mem []byte) {
		for i := 0; i < len(mem)/4; i++ {
			binary.LittleEndian.PutUint32(mem[i*4:], uint32(i)*0x9e3779b9+0x80000001)
		}
	}
	for _, terms := range []int{1, 2, 4} {
		for _, trip := range []struct{ start, limit uint32 }{{0, 64}, {4, 68}, {0, uint32(terms * 4)}, {16, 64}} {
			for _, biases := range [][2]int32{{0, 0}, {-128, 37}, {0x7fffffff, -2147483648}} {
				for _, base := range []uint32{0, 1, 4, 1024, 2048, 65520, 0xfffffff0} {
					t.Run(fmt.Sprintf("terms%d/start%d/end%d/bias%d,%d/base%x", terms, trip.start, trip.limit, biases[0], biases[1], base), func(t *testing.T) {
						m := dotLoopModule(t, terms, trip.start, trip.limit, biases[0], biases[1])
						mem := make([]byte, 65536)
						setup(mem)
						want, trapped := uint32(0xfffffff1), false
						for offset := trip.start; offset != trip.limit; offset += uint32(terms * 4) {
							for i := 0; i < terms; i++ {
								a, b := base+offset+uint32(i*4), uint32(2048)+offset+uint32(i*4)
								if uint64(a)+4 > uint64(len(mem)) || uint64(b)+4 > uint64(len(mem)) {
									trapped = true
									break
								}
								want += (binary.LittleEndian.Uint32(mem[a:]) + uint32(biases[0])) * (binary.LittleEndian.Uint32(mem[b:]) + uint32(biases[1]))
							}
							if trapped {
								break
							}
						}
						want ^= base + trip.limit - uint32(terms*4)
						want ^= 2048 + trip.limit - uint32(terms*4)
						want ^= trip.limit
						got, err := runArm64WrapperMem(t, m, base, setup)
						if trapped {
							if err == nil {
								t.Fatalf("expected trap, got %x", got)
							}
							return
						}
						if err != nil || got != want {
							t.Fatalf("got %x, %v; want %x", got, err, want)
						}
					})
				}
			}
		}
	}
}

func TestDotLoopAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := dotLoopEnabled
	dotLoopEnabled = true
	defer func() { dotLoopEnabled = old }()
	for _, terms := range []int{1, 2, 4} {
		stats := compileWithStats(t, dotLoopModule(t, terms, 0, 64, -128, 37), false).Funcs[0]
		if stats.Peephole["dot-loop-vector"] != 1 {
			t.Fatalf("terms %d not admitted: %v", terms, stats.Peephole)
		}
	}
}

func TestDotLoopPolicyRollback(t *testing.T) {
	requireCompilerDiagnostics(t)
	var stats ModuleStats
	cm, err := CompileModuleWith(dotLoopModule(t, 2, 0, 64, -128, 37), CompileOptions{
		Stats: &stats, Optimizations: map[string]bool{"dot-loop-vector": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	cm.CodeImage.Close()
	if stats.Funcs[0].Peephole["dot-loop-vector"] != 0 {
		t.Fatal("disabled loop vectorization admitted")
	}
}

func TestDotLoopRejectsEffectsAndMismatchedTerms(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := dotLoopEnabled
	dotLoopEnabled = true
	defer func() { dotLoopEnabled = old }()
	for _, test := range []struct {
		name         string
		prefix       []byte
		changeOffset bool
		shared       bool
	}{
		{"store", []byte{0x41, 0, 0x41, 1, 0x36, 2, 0}, false, false},
		{"changed-base", []byte{0x41, 4, 0x21, 0}, false, false},
		{"unequal-offsets", nil, true, false},
		{"shared-memory", nil, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := dotLoopModule(t, 2, 0, 64, -128, 37)
			body := m.Code[0].BodyBytes
			if len(test.prefix) != 0 {
				pos := bytes.Index(body, []byte{0x03, 0x40}) + 2
				body = append(append(append([]byte{}, body[:pos]...), test.prefix...), body[pos:]...)
			}
			if test.changeOffset {
				pos := bytes.Index(body, []byte{0x28, 2, 0})
				if pos < 0 {
					t.Fatal("missing load")
				}
				body[pos+2] = 4
			}
			m.Code[0].BodyBytes = body
			if test.shared {
				m.Memories[0].Shared = true
				m.Memories[0].Limits.HasMax = true
				m.Memories[0].Limits.Max = 1
			}
			stats := compileWithStats(t, m, false).Funcs[0]
			if stats.Peephole["dot-loop-vector"] != 0 {
				t.Fatalf("unsafe loop admitted: %v", stats.Peephole)
			}
		})
	}
}

func BenchmarkDotLoopGeneral(b *testing.B) {
	for _, limit := range []uint32{8, 16, 32, 64, 256, 4096} {
		for _, enabled := range []bool{false, true} {
			b.Run(fmt.Sprintf("bytes%d/vector%v", limit, enabled), func(b *testing.B) {
				cm, err := CompileModuleWith(dotLoopModule(b, 2, 0, limit, -128, 37), CompileOptions{Optimizations: map[string]bool{"dot-loop-vector": enabled}})
				if err != nil {
					b.Fatal(err)
				}
				defer cm.CodeImage.Close()
				eng, err := coreruntime.NewEngine()
				if err != nil {
					b.Fatal(err)
				}
				defer eng.Close()
				mem, err := coreruntime.NewJobMemory(65536)
				if err != nil {
					b.Fatal(err)
				}
				defer mem.Close()
				for i := 0; i < len(mem.CurrentBytes())/4; i++ {
					binary.LittleEndian.PutUint32(mem.CurrentBytes()[i*4:], uint32(i)*0x9e3779b9+0x80000001)
				}
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
				args, result, trap := arena.Alloc(16), arena.Alloc(16), arena.Alloc(coreruntime.TrapBufferBytes)
				want := uint32(0xfffffff1)
				for i := uint32(0); i < limit; i += 4 {
					want += (binary.LittleEndian.Uint32(mem.CurrentBytes()[i:]) - 128) * (binary.LittleEndian.Uint32(mem.CurrentBytes()[2048+i:]) + 37)
				}
				want ^= limit - 8
				want ^= 2048 + limit - 8
				want ^= limit
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, mem.LinearMemory(), trap, result); err != nil || binary.LittleEndian.Uint32(result) != want {
					b.Fatalf("got %x, %v; want %x", binary.LittleEndian.Uint32(result), err, want)
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := eng.Call(entry+uintptr(cm.Entry[0]), args, mem.LinearMemory(), trap, result); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
