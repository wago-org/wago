//go:build (linux || darwin) && (amd64 || arm64) && !tinygo && !wago_precompiled

package wago

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"testing"
)

// Qualify source forms on the unchanged compiler. This is not a matcher.
type extmulShape struct {
	bits         int
	signed, high bool
}

func extmulShapes() (out []extmulShape) {
	for _, bits := range []int{8, 16, 32} {
		for _, signed := range []bool{false, true} {
			for _, high := range []bool{false, true} {
				out = append(out, extmulShape{bits, signed, high})
			}
		}
	}
	return
}
func (s extmulShape) String() string { return fmt.Sprintf("%d-%s-%s", s.bits, s.sign(), s.half()) }
func (s extmulShape) sign() string {
	if s.signed {
		return "s"
	}
	return "u"
}
func (s extmulShape) half() string {
	if s.high {
		return "high"
	}
	return "low"
}
func (s extmulShape) expression(fused, alias bool) string {
	x, y := "(local.get $x)", "(local.get $y)"
	if alias {
		y = x
	}
	dst, src := fmt.Sprintf("i%dx%d", s.bits*2, 64/s.bits), fmt.Sprintf("i%dx%d", s.bits, 128/s.bits)
	if fused {
		return fmt.Sprintf("(%s.extmul_%s_%s_%s %s %s)", dst, s.half(), src, s.sign(), x, y)
	}
	ext := fmt.Sprintf("%s.extend_%s_%s_%s", dst, s.half(), src, s.sign())
	return fmt.Sprintf("(%s.mul (%s %s) (%s %s))", dst, ext, x, ext, y)
}
func extmulModule(t testing.TB, s extmulShape, fused bool, mode string) []byte {
	t.Helper()
	// Keep independent vectors live across both extensions and the multiplication.
	var locals, before, after string
	if mode == "pressure" {
		for i := 0; i < 24; i++ {
			locals += fmt.Sprintf(" (local $k%d v128)", i)
			before += fmt.Sprintf(" (local.set $k%d (v128.xor (local.get $x) (v128.const i32x4 %d %d %d %d)))", i, i+1, i+2, i+3, i+4)
			after += fmt.Sprintf(" (v128.store (i32.const %d) (local.get $k%d))", 32768+16*i, i)
		}
	}
	return watToWasm(t, fmt.Sprintf(`(module (memory 1 1)
 (func (export "run") (param $count i32) (result i32)
 (local $p i32) (local $x v128) (local $y v128) %s
 (block $done (loop $next
 (br_if $done (i32.eqz (local.get $count)))
 (local.set $x (v128.load (local.get $p)))
 (local.set $y (v128.load offset=16 (local.get $p))) %s
 (v128.store offset=32 (local.get $p) %s)
 ;; Observe borrowed source locals after the destructive arithmetic.
 (v128.store (local.get $p) (local.get $x))
 (v128.store offset=16 (local.get $p) (local.get $y)) %s
 (local.set $p (i32.add (local.get $p) (i32.const 48)))
 (local.set $count (i32.sub (local.get $count) (i32.const 1))) (br $next)))
 (local.get $p)))`, locals, before, s.expression(fused, mode == "alias"), after))
}

// Read narrow lanes, multiply in an independently widened host integer, then
// serialize every result bit. Signed 32-bit products fit int64; unsigned fit uint64.
func extmulWant(s extmulShape, x, y []byte) (out [16]byte) {
	lane := func(p []byte) uint64 {
		switch s.bits {
		case 8:
			return uint64(p[0])
		case 16:
			return uint64(binary.LittleEndian.Uint16(p))
		default:
			return uint64(binary.LittleEndian.Uint32(p))
		}
	}
	start := 0
	if s.high {
		start = 8
	}
	for i := 0; i < 64/s.bits; i++ {
		off := start + i*s.bits/8
		a, b := lane(x[off:]), lane(y[off:])
		product := a * b
		if s.signed {
			shift := 64 - s.bits
			product = uint64((int64(a<<shift) >> shift) * (int64(b<<shift) >> shift))
		}
		switch s.bits {
		case 8:
			binary.LittleEndian.PutUint16(out[i*2:], uint16(product))
		case 16:
			binary.LittleEndian.PutUint32(out[i*4:], uint32(product))
		case 32:
			binary.LittleEndian.PutUint64(out[i*8:], product)
		}
	}
	return
}
func extmulInput(s extmulShape, count int) []byte {
	input := make([]byte, count*48)
	r := rand.New(rand.NewSource(870))
	r.Read(input)
	// Cartesian products of the important signed/unsigned edges, with different
	// neighboring lanes and halves. Subsequent records are deterministic random.
	max := uint64(1)<<s.bits - 1
	edges := []uint64{0, 1, max >> 1, (max >> 1) + 1, max - 1, max}
	for n := 0; n < count && n < 36; n++ {
		for lane := 0; lane < 128/s.bits; lane++ {
			a, b := edges[(n/6+lane)%6], edges[(n%6+lane*3)%6]
			for j := 0; j < s.bits/8; j++ {
				input[n*48+lane*s.bits/8+j] = byte(a >> uint(j*8))
				input[n*48+16+lane*s.bits/8+j] = byte(b >> uint(j*8))
			}
		}
	}
	return input
}
func checkExtmul(s extmulShape, mode string, input, got []byte) error {
	if len(got) < len(input) {
		return fmt.Errorf("short memory")
	}
	for off := 0; off < len(input); off += 48 {
		if !bytes.Equal(input[off:off+32], got[off:off+32]) {
			return fmt.Errorf("input changed at %d", off/48)
		}
		x, y := input[off:off+16], input[off+16:off+32]
		if mode == "alias" {
			y = x
		}
		want := extmulWant(s, x, y)
		if !bytes.Equal(want[:], got[off+32:off+48]) {
			return fmt.Errorf("output %d: got %x want %x", off/48, got[off+32:off+48], want)
		}
	}
	if mode == "pressure" && len(input) > 0 {
		if len(got) < 32768+24*16 {
			return fmt.Errorf("short pressure memory")
		}
		x := input[len(input)-48:]
		for i := 0; i < 24; i++ {
			for lane := 0; lane < 4; lane++ {
				want := binary.LittleEndian.Uint32(x[lane*4:]) ^ uint32(i+lane+1)
				if binary.LittleEndian.Uint32(got[32768+i*16+lane*4:]) != want {
					return fmt.Errorf("live vector %d lane %d changed", i, lane)
				}
			}
		}
	}
	return nil
}
func TestSIMDExtmulSourcePairs(t *testing.T) {
	for _, s := range extmulShapes() {
		for _, mode := range []string{"plain", "alias", "pressure"} {
			for _, fused := range []bool{false, true} {
				for _, profile := range simdPairProfiles() {
					t.Run(fmt.Sprintf("%s/%s/extmul=%v/profile=%x", s, mode, fused, profile), func(t *testing.T) {
						e := newSIMDPairExecution(t, extmulModule(t, s, fused, mode), profile)
						input := extmulInput(s, 64)
						mem := e.jm.LinearMemory()
						copy(mem, input)
						before := append([]byte(nil), mem...)
						if err := e.call(0); err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(mem, before) {
							t.Fatal("zero work changed memory")
						}
						if err := e.call(64); err != nil {
							t.Fatal(err)
						}
						if err := checkExtmul(s, mode, input, mem); err != nil {
							t.Fatal(err)
						}
						if err := checkExtmul(s, mode, input, input); err == nil {
							t.Fatal("accepted omitted execution")
						}
						mem[32] ^= 1
						if err := checkExtmul(s, mode, input, mem); err == nil {
							t.Fatal("accepted changed output")
						}
						mem[32] ^= 1
						mem[0] ^= 1
						if err := checkExtmul(s, mode, input, mem); err == nil {
							t.Fatal("accepted changed input")
						}
						mem[0] ^= 1
						if mode == "pressure" {
							mem[32768] ^= 1
							if err := checkExtmul(s, mode, input, mem); err == nil {
								t.Fatal("accepted changed live vector")
							}
						}
					})
				}
			}
		}
	}
}
func BenchmarkSIMDExtmul(b *testing.B) {
	for _, s := range extmulShapes() {
		for _, fused := range []bool{false, true} {
			for _, profile := range simdPairProfiles() {
				b.Run(fmt.Sprintf("%s/extmul=%v/profile=%x", s, fused, profile), func(b *testing.B) {
					e := newSIMDPairExecution(b, extmulModule(b, s, fused, "plain"), profile)
					input := extmulInput(s, 256)
					mem := e.jm.LinearMemory()
					copy(mem, input)
					if err := e.jm.BindTrapCell(e.trap); err != nil {
						b.Fatal(err)
					}
					base := e.jm.LinMemBase()
					binary.LittleEndian.PutUint32(e.args, 256)
					call := func() {
						if err := e.eng.CallPrepared(e.entry, e.args, base, e.trap, e.results); err != nil {
							b.Fatal(err)
						}
					}
					check := func() {
						if binary.LittleEndian.Uint32(e.results) != 256*48 {
							b.Fatal("incomplete work")
						}
						if err := checkExtmul(s, "plain", input, mem); err != nil {
							b.Fatal(err)
						}
					}
					call()
					check()
					b.Logf("API=CallPrepared input=%x", sha256.Sum256(input))
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						call()
					}
					b.StopTimer()
					check()
					b.ReportMetric(float64(len(e.code.bytes)), "native-B")
					b.ReportMetric(256, "vectors/op")
				})
			}
		}
	}
}
func BenchmarkSIMDExtmulCompile(b *testing.B) {
	for _, s := range extmulShapes() {
		for _, fused := range []bool{false, true} {
			for _, profile := range simdPairProfiles() {
				b.Run(fmt.Sprintf("%s/extmul=%v/profile=%x", s, fused, profile), func(b *testing.B) {
					raw := extmulModule(b, s, fused, "plain")
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						compileSIMDPair(b, raw, profile)
					}
				})
			}
		}
	}
}
