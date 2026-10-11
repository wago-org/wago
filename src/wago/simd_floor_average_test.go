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

type floorShape struct {
	bits   int
	signed bool
}

func floorShapes() []floorShape {
	return []floorShape{{8, false}, {8, true}, {16, false}, {16, true}, {32, false}, {32, true}}
}
func (s floorShape) String() string {
	sign := "u"
	if s.signed {
		sign = "s"
	}
	return fmt.Sprintf("%d-%s", s.bits, sign)
}
func (s floorShape) expression(commute, count int, alias bool) string {
	x, y := "(local.get $x)", "(local.get $y)"
	if alias {
		y = x
	}
	ax, ay, xx, xy := x, y, x, y
	if commute&1 != 0 {
		ax, ay = ay, ax
	}
	if commute&2 != 0 {
		xx, xy = xy, xx
	}
	lane := fmt.Sprintf("i%dx%d", s.bits, 128/s.bits)
	sign := "u"
	if s.signed {
		sign = "s"
	}
	a := fmt.Sprintf("(v128.and %s %s)", ax, ay)
	b := fmt.Sprintf("(%s.shr_%s (v128.xor %s %s) (i32.const %d))", lane, sign, xx, xy, count)
	if commute&4 != 0 {
		a, b = b, a
	}
	return fmt.Sprintf("(%s.add %s %s)", lane, a, b)
}
func floorModule(t testing.TB, s floorShape, commute, count int, mode string) []byte {
	t.Helper()
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
 (block $done (loop $next (br_if $done (i32.eqz (local.get $count)))
 (local.set $x (v128.load (local.get $p))) (local.set $y (v128.load offset=16 (local.get $p))) %s
 (v128.store offset=32 (local.get $p) %s)
 (v128.store (local.get $p) (local.get $x)) (v128.store offset=16 (local.get $p) (local.get $y)) %s
 (local.set $p (i32.add (local.get $p) (i32.const 48)))
 (local.set $count (i32.sub (local.get $count) (i32.const 1))) (br $next))) (local.get $p)))`, locals, before, s.expression(commute, count, mode == "alias"), after))
}
func floorRead(p []byte, bits int) uint64 {
	switch bits {
	case 8:
		return uint64(p[0])
	case 16:
		return uint64(binary.LittleEndian.Uint16(p))
	default:
		return uint64(binary.LittleEndian.Uint32(p))
	}
}
func floorWrite(p []byte, bits int, n uint64) {
	switch bits {
	case 8:
		p[0] = byte(n)
	case 16:
		binary.LittleEndian.PutUint16(p, uint16(n))
	default:
		binary.LittleEndian.PutUint32(p, uint32(n))
	}
}
func floorSigned(n uint64, bits int) int64 { shift := 64 - bits; return int64(n<<shift) >> shift }

// Mathematical widened sum and division, independent of the Wasm bit identity.
func floorLane(s floorShape, a, b uint64) uint64 {
	if !s.signed {
		return (a + b) / 2
	}
	sum := floorSigned(a, s.bits) + floorSigned(b, s.bits)
	q := sum / 2
	if sum < 0 && sum%2 != 0 {
		q--
	}
	return uint64(q)
}
func floorWant(s floorShape, x, y []byte) (out [16]byte) {
	for i := 0; i < 16; i += s.bits / 8 {
		floorWrite(out[i:], s.bits, floorLane(s, floorRead(x[i:], s.bits), floorRead(y[i:], s.bits)))
	}
	return
}
func floorInput(s floorShape, count int) []byte {
	out := make([]byte, count*48)
	r := rand.New(rand.NewSource(871))
	r.Read(out)
	max := uint64(1)<<s.bits - 1
	edges := []uint64{0, 1, max >> 1, (max >> 1) + 1, max - 1, max}
	for n := 0; n < count && n < 36; n++ {
		for lane := 0; lane < 128/s.bits; lane++ {
			floorWrite(out[n*48+lane*s.bits/8:], s.bits, edges[(n/6+lane)%6])
			floorWrite(out[n*48+16+lane*s.bits/8:], s.bits, edges[(n%6+lane*3)%6])
		}
	}
	return out
}
func checkFloor(s floorShape, mode string, input, got []byte) error {
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
		want := floorWant(s, x, y)
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
				if binary.LittleEndian.Uint32(got[32768+i*16+lane*4:]) != binary.LittleEndian.Uint32(x[lane*4:])^uint32(i+lane+1) {
					return fmt.Errorf("live vector changed")
				}
			}
		}
	}
	return nil
}
func TestSIMDFloorAverageByteModel(t *testing.T) {
	for _, signed := range []bool{false, true} {
		s := floorShape{8, signed}
		for a := uint64(0); a < 256; a++ {
			for b := uint64(0); b < 256; b++ {
				shift := int64(a ^ b)
				if signed {
					shift = int64(int8(a ^ b))
				}
				got := byte(int64(a&b) + (shift >> 1))
				if got != byte(floorLane(s, a, b)) {
					t.Fatalf("%s %d,%d", s, a, b)
				}
			}
		}
	}
	// Negative odd sums must floor rather than truncate toward zero.
	if byte(floorLane(floorShape{8, true}, 0xff, 0)) != 0xff {
		t.Fatal("negative odd sum did not floor")
	}
	// Wrapping addition and rounded-up averaging are different contracts.
	if floorLane(floorShape{8, false}, 255, 255) == uint64(byte(254)>>1) || floorLane(floorShape{8, false}, 0, 1) == 1 {
		t.Fatal("accepted wrong average contract")
	}
}
func TestSIMDFloorAverageSources(t *testing.T) {
	for _, s := range floorShapes() {
		for commute := 0; commute < 8; commute++ {
			for _, count := range []int{1, 1 + s.bits, 1 - s.bits} {
				// Pressure and alias cases need not multiply the commutation/count matrix.
				modes := []string{"plain"}
				if commute == 0 && count == 1 {
					modes = append(modes, "alias", "pressure")
				}
				for _, mode := range modes {
					for _, profile := range simdPairProfiles() {
						t.Run(fmt.Sprintf("%s/commute=%d/count=%d/%s/profile=%x", s, commute, count, mode, profile), func(t *testing.T) {
							raw := floorModule(t, s, commute, count, mode)
							e := newSIMDPairExecution(t, raw, profile)
							input := floorInput(s, 64)
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
							if err := checkFloor(s, mode, input, mem); err != nil {
								t.Fatal(err)
							}
							if err := checkFloor(s, mode, input, input); err == nil {
								t.Fatal("accepted omitted execution")
							}
							mem[32] ^= 1
							if err := checkFloor(s, mode, input, mem); err == nil {
								t.Fatal("accepted output corruption")
							}
							mem[32] ^= 1
							mem[0] ^= 1
							if err := checkFloor(s, mode, input, mem); err == nil {
								t.Fatal("accepted input corruption")
							}
							mem[0] ^= 1
							if mode == "pressure" {
								mem[32768] ^= 1
								if err := checkFloor(s, mode, input, mem); err == nil {
									t.Fatal("accepted live-vector corruption")
								}
							}
						})
					}
				}
			}
		}
	}
}
func BenchmarkSIMDFloorAverage(b *testing.B) {
	for _, s := range floorShapes() {
		for _, profile := range simdPairProfiles() {
			b.Run(fmt.Sprintf("%s/profile=%x", s, profile), func(b *testing.B) {
				e := newSIMDPairExecution(b, floorModule(b, s, 0, 1, "plain"), profile)
				input := floorInput(s, 256)
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
					if err := checkFloor(s, "plain", input, mem); err != nil {
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
func BenchmarkSIMDFloorAverageCompile(b *testing.B) {
	for _, s := range floorShapes() {
		for _, profile := range simdPairProfiles() {
			b.Run(fmt.Sprintf("%s/profile=%x", s, profile), func(b *testing.B) {
				raw := floorModule(b, s, 0, 1, "plain")
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					compileSIMDPair(b, raw, profile)
				}
			})
		}
	}
}
