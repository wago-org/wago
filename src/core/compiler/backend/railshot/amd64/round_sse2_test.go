//go:build linux && amd64 && !tinygo

package amd64

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime"
)

func roundStub(f64, modern, alias bool, mode byte) []byte {
	a := &x86.Asm{}
	a.MovReg64(R11, RCX) // preserve results pointer across variable shifts
	a.FLoadDisp(9, RDI, 0, f64)
	dst := Reg(10)
	if alias {
		dst = 9
	}
	if modern {
		a.Round(dst, 9, f64, mode)
	} else {
		emitRoundSSE2(a, dst, 9, RAX, R8, R9, R10, f64, mode)
	}
	a.FStoreDisp(R11, 0, dst, f64)
	a.Ret()
	return a.B
}

func roundRunner(t testing.TB, code []byte) func(uint64) uint64 {
	t.Helper()
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { jm.Close() })
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ar.Close() })
	mem, entry, err := runtime.MapCode(code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Unmap(mem) })
	args, results, trap := ar.Alloc(8), ar.Alloc(8), ar.Alloc(runtime.TrapBufferBytes)
	return func(v uint64) uint64 {
		binary.LittleEndian.PutUint64(args, v)
		binary.LittleEndian.PutUint64(results, 0)
		if err := eng.Call(entry, args, jm.LinearMemory(), trap, results); err != nil {
			t.Fatal(err)
		}
		return binary.LittleEndian.Uint64(results)
	}
}

func roundReference(bits uint64, f64 bool, mode byte) uint64 {
	x := math.Float64frombits(bits)
	if !f64 {
		x = float64(math.Float32frombits(uint32(bits)))
	}
	if math.IsNaN(x) {
		if f64 {
			return bits | 1<<51
		}
		return uint64(uint32(bits) | 1<<22)
	}
	switch mode & 3 {
	case 0:
		x = math.RoundToEven(x)
	case 1:
		x = math.Floor(x)
	case 2:
		x = math.Ceil(x)
	case 3:
		x = math.Trunc(x)
	}
	if f64 {
		return math.Float64bits(x)
	}
	return uint64(math.Float32bits(float32(x)))
}

func roundInputs(f64 bool) []uint64 {
	mantissa, exponent, sign := uint(23), uint(8), uint(31)
	if f64 {
		mantissa, exponent, sign = 52, 11, 63
	}
	inputs := make([]uint64, 0, 50000)
	// Every exponent, both signs, and fraction boundaries: integral values,
	// ties, neighbors of ties, subnormal/normal boundaries, infinities and NaNs.
	for e := uint64(0); e < 1<<exponent; e++ {
		for _, fraction := range []uint64{0, 1, (1 << (mantissa - 1)) - 1, 1 << (mantissa - 1), (1 << (mantissa - 1)) + 1, (1 << mantissa) - 1} {
			v := e<<mantissa | fraction
			inputs = append(inputs, v, v|1<<sign)
		}
	}
	// All fractional-bit positions and their immediate neighbors.
	for i := 0; i < 55; i++ {
		for _, x := range []float64{math.Ldexp(1, i), math.Ldexp(1, i) + 0.5, float64(i) + 0.5} {
			v := math.Float64bits(x)
			if !f64 {
				v = uint64(math.Float32bits(float32(x)))
			}
			for _, u := range []uint64{v - 1, v, v + 1} {
				inputs = append(inputs, u, u|1<<sign)
			}
		}
	}
	rng := rand.New(rand.NewSource(173))
	for i := 0; i < 8192; i++ {
		v := rng.Uint64()
		if !f64 {
			v = uint64(uint32(v))
		}
		inputs = append(inputs, v)
	}
	return inputs
}

func TestRoundSSE2Semantics(t *testing.T) {
	cpu, _ := os.ReadFile("/proc/cpuinfo")
	hasSSE41 := strings.Contains(string(cpu), " sse4_1 ")
	for _, f64 := range []bool{false, true} {
		inputs := roundInputs(f64)
		for _, mode := range []byte{roundNearest, roundFloor, roundCeil, roundTrunc} {
			for _, alias := range []bool{false, true} {
				t.Run(fmt.Sprintf("f64=%v/mode=%d/alias=%v", f64, mode&3, alias), func(t *testing.T) {
					run := roundRunner(t, roundStub(f64, false, alias, mode))
					var modern func(uint64) uint64
					if hasSSE41 {
						modern = roundRunner(t, roundStub(f64, true, alias, mode))
					}
					for _, bits := range inputs {
						want := roundReference(bits, f64, mode)
						got := run(bits)
						if got != want {
							t.Fatalf("input=%016x got=%016x want=%016x", bits, got, want)
						}
						if modern != nil {
							fast := modern(bits)
							if fast != want {
								t.Fatalf("input=%016x modern=%016x want=%016x", bits, fast, want)
							}
						}
					}
				})
			}
		}
	}
}

// Disassemble instructions, not byte substrings: immediates and displacements
// can contain bytes equal to VEX/EVEX prefixes. These stubs have no literal data.
func TestRoundSSE2InstructionBaseline(t *testing.T) {
	objdump, err := exec.LookPath("objdump")
	if err != nil {
		t.Skip("GNU objdump is required for instruction validation")
	}
	allowed := map[string]bool{}
	for _, op := range strings.Fields("mov movabs movd movq movss movsd shl shr sub add and or xor cmp test je jne ja jae jb jbe js jns jmp ret") {
		allowed[op] = true
	}
	for _, f64 := range []bool{false, true} {
		for _, mode := range []byte{roundNearest, roundFloor, roundCeil, roundTrunc} {
			path := filepath.Join(t.TempDir(), "round.bin")
			if err := os.WriteFile(path, roundStub(f64, false, true, mode), 0600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(objdump, "-D", "-b", "binary", "-m", "i386:x86-64", "-M", "intel", "--no-show-raw-insn", path).CombinedOutput()
			if err != nil {
				t.Fatalf("objdump: %v\n%s", err, out)
			}
			count := 0
			for _, line := range strings.Split(string(out), "\n") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) != 2 || !strings.HasPrefix(line, " ") {
					continue
				}
				fields := strings.Fields(parts[1])
				if len(fields) == 0 {
					continue
				}
				count++
				if !allowed[fields[0]] {
					t.Fatalf("non-baseline or unknown instruction: %s", line)
				}
			}
			if count < 20 {
				t.Fatalf("incomplete disassembly: %s", out)
			}
		}
	}
}

func BenchmarkRoundLowering(b *testing.B) {
	for _, f64 := range []bool{false, true} {
		for _, modern := range []bool{false, true} {
			b.Run(fmt.Sprintf("f64=%v/modern=%v", f64, modern), func(b *testing.B) {
				code := roundStub(f64, modern, true, roundNearest)
				b.Run("emit", func(b *testing.B) {
					a := &x86.Asm{B: make([]byte, 0, 512)}
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						a.B = a.B[:0]
						if modern {
							a.Round(9, 9, f64, roundNearest)
						} else {
							emitRoundSSE2(a, 9, 9, RAX, R8, R9, R10, f64, roundNearest)
						}
					}
					b.ReportMetric(float64(len(a.B)), "code-bytes")
				})
				b.Run("execute", func(b *testing.B) {
					run := roundRunner(b, code)
					bits := math.Float64bits(-3.5)
					if !f64 {
						bits = uint64(math.Float32bits(-3.5))
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						run(bits)
					}
				})
			})
		}
	}
}
