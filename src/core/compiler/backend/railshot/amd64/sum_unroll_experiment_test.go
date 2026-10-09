//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

// Parameters are address, count, initial sum, and live values. Results expose
// the sum, both induction locals, and a checksum of all other live parameters.
func sumUnrollModule(t testing.TB, pressure int) *wasm.Module {
	t.Helper()
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I64}
	for range pressure {
		params = append(params, wasm.I64)
	}
	body := []byte{0, 0x02, 0x40, 0x03, 0x40,
		0x20, 1, 0x45, 0x0d, 1,
		0x20, 2, 0x20, 0, 0x29, 3, 0, 0x7c, 0x21, 2,
		0x20, 0, 0x41, 8, 0x6a, 0x21, 0,
		0x20, 1, 0x41, 1, 0x6b, 0x21, 1, 0x0c, 0,
		0x0b, 0x0b, 0x20, 2, 0x20, 0, 0x20, 1, 0x42, 0}
	for i := 0; i < pressure; i++ {
		body = append(body, 0x20, byte(3+i), 0x7c)
	}
	body = append(body, 0x0b)
	m := modMem(t, 1, params, []wasm.ValType{wasm.I64, wasm.I32, wasm.I32, wasm.I64}, body)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

type sumNative struct {
	eng                 *rt.Engine
	entry               uintptr
	args, results, trap []byte
}

func sumUnrollNative(t testing.TB, m *wasm.Module, opts CompileOptions) *sumNative {
	t.Helper()
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		t.Cleanup(func() { cm.CodeImage.Close() })
	}
	eng, err := rt.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })
	arena, err := rt.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { arena.Close() })
	code, entry, err := rt.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Unmap(code) })
	return &sumNative{eng, entry + uintptr(cm.Entry[0]), arena.Alloc(128), arena.Alloc(32), arena.Alloc(rt.TrapBufferBytes)}
}

func (n *sumNative) call(mem *rt.JobMemory, addr, count uint32, seed uint64, pressure int) ([4]uint64, error) {
	clear(n.args)
	clear(n.results)
	clear(n.trap)
	binary.LittleEndian.PutUint32(n.args, addr)
	binary.LittleEndian.PutUint32(n.args[8:], count)
	binary.LittleEndian.PutUint64(n.args[16:], seed)
	for i := 0; i < pressure; i++ {
		binary.LittleEndian.PutUint64(n.args[(3+i)*8:], uint64(i+1)*0x123456789abcdef)
	}
	err := n.eng.Call(n.entry, n.args, mem.LinearMemory(), n.trap, n.results)
	var out [4]uint64
	for i := range out {
		out[i] = binary.LittleEndian.Uint64(n.results[8*i:])
	}
	// Wasm i32 results occupy only the low word of the ABI slot.
	out[1] = uint64(uint32(out[1]))
	out[2] = uint64(uint32(out[2]))
	return out, err
}

// Independent byte-addressed oracle. Address addition is explicitly uint32;
// effective load bounds use widened arithmetic. It does not use compiler code.
func sumOracle(mem []byte, addr, count uint32, seed uint64, pressure int) ([4]uint64, bool) {
	sum := seed
	for count != 0 {
		if uint64(addr)+8 > uint64(len(mem)) {
			return [4]uint64{}, true
		}
		sum += binary.LittleEndian.Uint64(mem[uint64(addr) : uint64(addr)+8])
		addr += 8
		count--
	}
	var checksum uint64
	for i := 0; i < pressure; i++ {
		checksum += uint64(i+1) * 0x123456789abcdef
	}
	return [4]uint64{sum, uint64(addr), 0, checksum}, false
}

func TestSumUnrollOracle(t *testing.T) {
	selectSumUnroll(t)
	mem, err := rt.NewJobMemory(3 << 20)
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()
	data := mem.CurrentBytes()
	for i := range data {
		data[i] = byte((uint64(i)*37 + (uint64(i)>>9)*71) ^ 0xa5)
	}
	saved := append([]byte(nil), data...)
	var counts []uint32
	for n := uint32(0); n <= 35; n++ {
		counts = append(counts, n)
	}
	counts = append(counts, 63, 64, 65, 66, 127, 128, 129, 130, 255, 256, 257, 258, 511, 512, 513, 8192, 262144)
	for _, pressure := range []int{0, 4, 12} {
		for _, bounded := range []bool{false, true} {
			t.Run(fmt.Sprintf("pressure%d/bounded%t", pressure, bounded), func(t *testing.T) {
				m := sumUnrollModule(t, pressure)
				if bounded {
					m.Memories[0].Limits.HasMax = true
					m.Memories[0].Limits.Max = 48
				}
				original := append([]byte(nil), m.Code[0].BodyBytes...)
				native := sumUnrollNative(t, m, CompileOptions{})
				baseline := sumUnrollBaseline(t, m)
				scalar := sumUnrollNative(t, m, CompileOptions{Optimizations: map[string]bool{"linear-sum-loop": false}})
				for _, addr := range []uint32{0, 1, 7, 8, 63, 128} {
					for _, count := range counts {
						for _, seed := range []uint64{0, ^uint64(0) - 3} {
							want, trap := sumOracle(data, addr, count, seed, pressure)
							got, err := native.call(mem, addr, count, seed, pressure)
							ref, refErr := scalar.call(mem, addr, count, seed, pressure)
							base, baseErr := baseline.call(mem, addr, count, seed, pressure)
							if baseErr != nil || base != want {
								t.Fatalf("baseline success mismatch: %x want %x: %v", base, want, baseErr)
							}
							if trap || err != nil || refErr != nil || got != want || ref != want {
								t.Fatalf("addr=%d count=%d seed=%x got=%x ref=%x want=%x err=%v/%v", addr, count, seed, got, ref, want, err, refErr)
							}
						}
					}
				}
				for _, tc := range [][2]uint32{{uint32(len(data)) - 8, 1}, {uint32(len(data)) - 8, 2}, {uint32(len(data)) - 7, 1}, {^uint32(0), 0}, {^uint32(0), 1}, {0, ^uint32(0)}, {uint32(len(data)) - 16, 1 << 29}, {0, 1 << 29}} {
					want, trap := sumOracle(data, tc[0], tc[1], 13, pressure)
					got, err := native.call(mem, tc[0], tc[1], 13, pressure)
					ref, refErr := scalar.call(mem, tc[0], tc[1], 13, pressure)
					if (err != nil) != trap || (refErr != nil) != trap || !trap && (got != want || ref != want) {
						t.Fatalf("boundary %v got=%x ref=%x want=%x trap=%v err=%v/%v", tc, got, ref, want, trap, err, refErr)
					}
					base, baselineErr := baseline.call(mem, tc[0], tc[1], 13, pressure)
					if (baselineErr != nil) != trap || !trap && base != want {
						t.Fatal("baseline trap mismatch")
					}
					if trap && (!bytes.Equal(native.trap, baseline.trap) || !bytes.Equal(native.trap[:20], scalar.trap[:20])) {
						t.Fatalf("trap metadata differs at %v: %x / %x", tc, native.trap, scalar.trap)
					}
				}

				if !bytes.Equal(original, m.Code[0].BodyBytes) {
					t.Fatal("compiler changed Wasm instructions")
				}
			})
		}
	}
	if !bytes.Equal(data, saved) {
		t.Fatal("read-only reduction changed memory")
	}
}

func TestSumUnrollFullMemory32(t *testing.T) {
	selectSumUnroll(t)
	mem, err := rt.NewJobMemory(1 << 32)
	if err != nil {
		t.Skipf("4 GiB mapping: %v", err)
	}
	defer mem.Close()
	data := mem.CurrentBytes()
	for i := 0; i < 64; i++ {
		binary.LittleEndian.PutUint64(data[i*8:], uint64(i+1)*0xfedcba987654321)
		binary.LittleEndian.PutUint64(data[len(data)-(i+1)*8:], ^uint64(i*97))
	}
	m := sumUnrollModule(t, 0)
	native := sumUnrollNative(t, m, CompileOptions{})
	baseline := sumUnrollBaseline(t, m)
	scalar := sumUnrollNative(t, m, CompileOptions{Optimizations: map[string]bool{"linear-sum-loop": false}})
	for _, tc := range [][2]uint32{{0xfffffff8, 0}, {0xfffffff8, 1}, {0xfffffff8, 2}, {0xfffffff0, 9}, {0xffffff80, 17}, {0xffffff80, 33}, {0xffffff00, 35}, {0xfffffe00, 65}, {0xfffffc00, 129}, {0xfffff800, 257}, {0xfffff000, 513}, {0xfffffff9, 1}, {0xffffffef, 3}} {
		want, trap := sumOracle(data, tc[0], tc[1], ^uint64(0), 0)
		got, err := native.call(mem, tc[0], tc[1], ^uint64(0), 0)
		ref, refErr := scalar.call(mem, tc[0], tc[1], ^uint64(0), 0)
		if (err != nil) != trap || (refErr != nil) != trap || !trap && (got != want || ref != want) {
			t.Fatalf("wrap %v: %x/%x want %x errors %v/%v", tc, got, ref, want, err, refErr)
		}
		base, baselineErr := baseline.call(mem, tc[0], tc[1], ^uint64(0), 0)
		if (baselineErr != nil) != trap || !trap && base != want {
			t.Fatal("baseline wrap trap mismatch")
		}
		if trap && (!bytes.Equal(native.trap, baseline.trap) || !bytes.Equal(native.trap[:20], scalar.trap[:20])) {
			t.Fatal("wrap trap metadata differs")
		}
	}
}

func BenchmarkSumUnrollExecute(b *testing.B) {
	selectSumUnroll(b)
	m := sumUnrollModule(b, 0)
	mem, err := rt.NewJobMemory(64 << 20)
	if err != nil {
		b.Fatal(err)
	}
	defer mem.Close()
	data := mem.CurrentBytes()
	for i := range data {
		data[i] = byte(i*37 + 11)
	}
	native := sumUnrollNative(b, m, CompileOptions{})
	for _, addr := range []uint32{0, 1} {
		for _, count := range []uint32{0, 8, 16, 33, 512, 8192, 262144, 8388607} {
			b.Run(fmt.Sprintf("addr%d/n%d", addr, count), func(b *testing.B) {
				want, trap := sumOracle(data, addr, count, 7, 0)
				if trap {
					b.Fatal("invalid benchmark input")
				}
				got, err := native.call(mem, addr, count, 7, 0)
				if err != nil || got != want {
					b.Fatalf("preflight: %x want %x: %v", got, want, err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := native.eng.Call(native.entry, native.args, mem.LinearMemory(), native.trap, native.results); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				if binary.LittleEndian.Uint64(native.results) != want[0] {
					b.Fatal("incorrect sum")
				}
			})
		}
	}
}

func BenchmarkSumUnrollCompile(b *testing.B) {
	selectSumUnroll(b)
	for _, pressure := range []int{0, 12} {
		b.Run(fmt.Sprintf("pressure%d", pressure), func(b *testing.B) {
			m := sumUnrollModule(b, pressure)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModule(m)
				if err != nil {
					b.Fatal(err)
				}
				cm.CodeImage.Close()
			}
		})
	}
}

func TestSumUnrollArtifacts(t *testing.T) {
	dir := os.Getenv("WAGO_SUM_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("set WAGO_SUM_ARTIFACT_DIR")
	}
	selectSumUnroll(t)
	requireCompilerDiagnostics(t)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, pressure := range []int{0, 4, 12} {
		m := sumUnrollModule(t, pressure)
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("pressure%d", pressure)
		if err := os.WriteFile(filepath.Join(dir, name+".bin"), cm.Code, 0644); err != nil {
			t.Fatal(err)
		}
		s := stats.Funcs[0]
		text := fmt.Sprintf("code=%d frame=%d spills=%d reloads=%d peepholes=%v\n", s.CodeBytes, s.FrameBytes, s.Spills, s.Reloads, s.Peephole)
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
	}
}
