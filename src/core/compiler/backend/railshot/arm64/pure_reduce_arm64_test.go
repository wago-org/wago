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

func pureReduceModule(t testing.TB, step int32, mask uint32) *wasm.Module {
	body := []byte{2, 4, 0x7f, 3, 0x7e}
	i32 := func(v int32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(v)...) }
	i64 := func(v int64) { body = append(body, 0x42); body = append(body, wasmtest.SLEB64(v)...) }
	i32(0x7ffffffe)
	body = append(body, 0x21, 1, 0x20, 0, 0x21, 2)
	i64(-6)
	body = append(body, 0x21, 5)
	i64(-9223372036854775553)
	body = append(body, 0x21, 6)
	body = append(body, 0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40)
	// X = ((I+19) xor ((I+19)>>7))*17; scratch 3 preserves X.
	body = append(body, 0x20, 5, 0x20, 1)
	i32(19)
	body = append(body, 0x6a, 0x22, 3, 0x20, 3)
	i32(7)
	body = append(body, 0x76, 0x73)
	i32(17)
	body = append(body, 0x6c, 0x22, 3)
	i32(int32(mask))
	body = append(body, 0x71, 0xad, 0x22, 7, 0x7c, 0x21, 5)
	// Y = (I xor 0x9e3779b9)>>3; retain Y separately from its mask.
	body = append(body, 0x20, 6, 0x20, 7, 0x20, 1)
	i32(-1640531527)
	body = append(body, 0x73)
	i32(3)
	body = append(body, 0x76, 0x22, 4)
	i32(int32(mask))
	body = append(body, 0x71, 0xad, 0x7e, 0x7c, 0x21, 6)
	body = append(body, 0x20, 1)
	i32(step)
	body = append(body, 0x6a, 0x21, 1, 0x20, 2)
	i32(1)
	body = append(body, 0x6b, 0x22, 2, 0x0d, 0, 0x0b, 0x0b)
	body = append(body, 0x20, 5, 0x20, 6, 0x85, 0x20, 3, 0xad, 0x85, 0x20, 4, 0xad, 0x85, 0x20, 7, 0x85, 0x20, 1, 0xad, 0x85, 0x20, 2, 0xad, 0x85, 0x0b)
	return mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, body)
}

func pureReduceExpected(n uint32, step int32, mask uint32) uint64 {
	i := uint32(0x7ffffffe)
	a, b := ^uint64(5), uint64(0x80000000000000ff)
	var x, y, last uint32
	for left := n; left != 0; left-- {
		x = i + 19
		x = (x ^ (x >> 7)) * 17
		y = (i ^ 0x9e3779b9) >> 3
		last = x & mask
		a += uint64(last)
		b += uint64(last) * uint64(y&mask)
		i += uint32(step)
	}
	return a ^ b ^ uint64(x) ^ uint64(y) ^ uint64(last) ^ uint64(i)
}

func TestPureReduceIndependentExecution(t *testing.T) {
	old := pureReduceEnabled
	pureReduceEnabled = true
	defer func() { pureReduceEnabled = old }()
	for _, step := range []int32{3, -5, 0x7fffffff, 0x40000000} {
		for _, mask := range []uint32{1023, 65535, 131071} {
			m := pureReduceModule(t, step, mask)
			for _, n := range []uint32{0, 1, 2, 3, 4, 5, 7, 8, 9, 31, 64, 4097} {
				t.Run(fmt.Sprintf("step%d/mask%x/count%d", step, mask, n), func(t *testing.T) {
					got := runArm64u(t, m, uint64(n))
					want := pureReduceExpected(n, step, mask)
					if got != want {
						t.Fatalf("got %x, want %x", got, want)
					}
				})
			}
		}
	}
}

func TestPureReducePrecisionAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := pureReduceEnabled
	pureReduceEnabled = true
	defer func() { pureReduceEnabled = old }()
	for _, mask := range []uint32{1023, 65535, 131071} {
		stats := compileWithStats(t, pureReduceModule(t, 3, mask), false).Funcs[0]
		want := 1
		if mask == 131071 {
			want = 0
		}
		if got := stats.Peephole["pure-reduce-vector"]; got != want {
			t.Fatalf("mask %x admission %d, want %d (%v)", mask, got, want, stats.Peephole)
		}
	}
}

func TestPureReducePolicyRollback(t *testing.T) {
	requireCompilerDiagnostics(t)
	var stats ModuleStats
	cm, err := CompileModuleWith(pureReduceModule(t, 3, 1023), CompileOptions{Stats: &stats, Optimizations: map[string]bool{"pure-reduce-vector": false}})
	if err != nil {
		t.Fatal(err)
	}
	cm.CodeImage.Close()
	if stats.Funcs[0].Peephole["pure-reduce-vector"] != 0 {
		t.Fatal("disabled vectorization admitted")
	}
}

func TestPureReducePreservesLeafABIAndPressureFallback(t *testing.T) {
	requireCompilerDiagnostics(t)
	leaf := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0, 0x03, 0x40, 0x20, 2, 0x20, 1, 0xad, 0x7c, 0x21, 2, 0x20, 1, 0x41, 3, 0x6a, 0x21, 1,
		0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 2, 0x0b})
	if stats := compileWithStats(t, leaf, false).Funcs[0]; stats.Peephole["pure-reduce-vector"] != 0 {
		t.Fatal("caller-pin-preserving ABI was clobbered")
	}
	m := pureReduceModule(t, 3, 1023)
	body := m.Code[0].BodyBytes
	pos := bytes.Index(body, []byte{0x20, 1, 0x41, 19, 0x6a}) + 5
	var extra []byte
	for i := int32(0); i < 32; i++ {
		extra = append(extra, 0x41)
		extra = append(extra, wasmtest.SLEB32(256+i)...)
		extra = append(extra, 0x73)
	}
	m.Code[0].BodyBytes = append(append(append([]byte{}, body[:pos]...), extra...), body[pos:]...)
	if stats := compileWithStats(t, m, false).Funcs[0]; stats.Peephole["pure-reduce-vector"] != 0 {
		t.Fatal("excessive vector register pressure was admitted")
	}
	old := pureReduceEnabled
	defer func() { pureReduceEnabled = old }()
	for _, n := range []uint64{1, 4, 9} {
		pureReduceEnabled = false
		want := runArm64u(t, m, n)
		pureReduceEnabled = true
		got := runArm64u(t, m, n)
		if got != want {
			t.Fatalf("pressure fallback count %d: got %x, want %x", n, got, want)
		}
	}
}

func simplePureReduceModule(t testing.TB) *wasm.Module {
	body := []byte{2, 2, 0x7f, 1, 0x7e, 0x20, 0, 0x21, 2, 0x41}
	body = append(body, wasmtest.SLEB32(0x7ffffffe)...)
	body = append(body, 0x21, 1, 0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40,
		0x20, 3, 0x20, 1, 0xad, 0x7c, 0x21, 3, 0x20, 1, 0x41, 3, 0x6a, 0x21, 1,
		0x20, 2, 0x41, 1, 0x6b, 0x22, 2, 0x0d, 0, 0x0b, 0x0b, 0x20, 3, 0x0b)
	return mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, body)
}

func TestPureReduceFullWidthSumAndTraps(t *testing.T) {
	old := pureReduceEnabled
	pureReduceEnabled = true
	defer func() { pureReduceEnabled = old }()
	m := simplePureReduceModule(t)
	for _, n := range []uint32{0, 1, 2, 3, 4, 7, 8, 31, 4096} {
		want := uint64(0)
		value := uint32(0x7ffffffe)
		for i := uint32(0); i < n; i++ {
			want += uint64(value)
			value += 3
		}
		if got := runArm64u(t, m, uint64(n)); got != want {
			t.Fatalf("count %d: got %x, want %x", n, got, want)
		}
	}
	trapModule := simplePureReduceModule(t)
	body := trapModule.Code[0].BodyBytes
	pos := bytes.Index(body, []byte{0x03, 0x40}) + 2
	trapModule.Code[0].BodyBytes = append(append(append([]byte{}, body[:pos]...), 0x41, 1, 0x41, 0, 0x6d, 0x1a), body[pos:]...)
	if _, err := runArm64Wrapper(t, trapModule, 0); err != nil {
		t.Fatalf("trap moved before zero-trip guard: %v", err)
	}
	for _, n := range []uint64{1, 4, 31} {
		if _, err := runArm64Wrapper(t, trapModule, n); err == nil {
			t.Fatalf("count %d: missing source division trap", n)
		}
	}
}

func BenchmarkPureReduceGeneral(b *testing.B) {
	old := pureReduceEnabled
	defer func() { pureReduceEnabled = old }()
	for _, complex := range []bool{false, true} {
		for _, n := range []uint32{1, 3, 4, 8, 16, 64, 4096} {
			for _, enabled := range []bool{false, true} {
				b.Run(fmt.Sprintf("complex%v/count%d/vector%v", complex, n, enabled), func(b *testing.B) {
					pureReduceEnabled = enabled
					m := simplePureReduceModule(b)
					if complex {
						m = pureReduceModule(b, 3, 1023)
					}
					cm, err := CompileModule(m)
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
					for i := 0; i < 4; i++ {
						args[i] = byte(n >> (8 * i))
					}
					if err := eng.Call(entry+uintptr(cm.Entry[0]), args, mem.LinearMemory(), trap, result); err != nil {
						b.Fatal(err)
					}
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if err := eng.Call(entry+uintptr(cm.Entry[0]), args, mem.LinearMemory(), trap, result); err != nil {
							b.Fatal(err)
						}
						want := uint64(0)
						if complex {
							want = pureReduceExpected(n, 3, 1023)
						} else {
							value := uint32(0x7ffffffe)
							for i := uint32(0); i < n; i++ {
								want += uint64(value)
								value += 3
							}
						}
						if got := binary.LittleEndian.Uint64(result); got != want {
							b.Fatalf("got %x, want %x", got, want)
						}
					}
				})
			}
		}
	}
}
