//go:build (linux || darwin) && (amd64 || arm64) && !tinygo && !wago_precompiled

package wago

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"testing"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

// These are exact core-SIMD source identities, not proposed compiler rewrites.
// Each record has two independent inputs and a full 16-byte result (48 bytes).
func simdPairExpression(family string, simplified bool) string {
	switch family {
	case "average":
		if simplified {
			return `(i8x16.avgr_u (local.get $x) (local.get $y))`
		}
		half := func(which string) string {
			return fmt.Sprintf(`(i16x8.shr_u
   (i16x8.add (i16x8.add (i16x8.extend_%s_i8x16_u (local.get $x))
    (i16x8.extend_%s_i8x16_u (local.get $y)))
    (v128.const i16x8 1 1 1 1 1 1 1 1)) (i32.const 1))`, which, which)
		}
		return `(i8x16.narrow_i16x8_u ` + half("low") + half("high") + `)`
	case "high-byte":
		if simplified {
			return `(i32x4.shr_u (local.get $x) (i32.const 30))`
		}
		out := `(v128.const i32x4 0 0 0 0)`
		for lane := 0; lane < 4; lane++ {
			out = fmt.Sprintf(`(i32x4.replace_lane %d %s (i32.shr_u (i8x16.extract_lane_u %d (local.get $x)) (i32.const 6)))`, lane, out, lane*4+3)
		}
		return out
	case "dot-shifts":
		if simplified {
			return `(i32x4.sub
    (i32x4.shr_s (i32x4.shl (local.get $x) (i32.const 16)) (i32.const 16))
    (i32x4.shr_s (local.get $x) (i32.const 16)))`
		}
		return simdPairExpression("dot-sub", false)
	case "dot-sub":
		if simplified {
			return `(i32x4.dot_i16x8_s (local.get $x) (v128.const i16x8 1 -1 1 -1 1 -1 1 -1))`
		}
		return `(i32x4.sub
   (i32x4.dot_i16x8_s (local.get $x) (v128.const i16x8 1 0 1 0 1 0 1 0))
   (i32x4.dot_i16x8_s (local.get $x) (v128.const i16x8 0 1 0 1 0 1 0 1)))`
	default:
		panic("unknown SIMD pair")
	}
}

func simdPairModule(t testing.TB, family string, simplified bool) []byte {
	t.Helper()
	return watToWasm(t, fmt.Sprintf(`(module
 (memory 1 1)
 (func (export "run") (param $count i32) (result i32)
 (local $p i32) (local $x v128) (local $y v128)
 (block $done (loop $next
  (br_if $done (i32.eqz (local.get $count)))
  (local.set $x (v128.load (local.get $p)))
  (local.set $y (v128.load offset=16 (local.get $p)))
  (v128.store offset=32 (local.get $p) %s)
  (local.set $p (i32.add (local.get $p) (i32.const 48)))
  (local.set $count (i32.sub (local.get $count) (i32.const 1)))
  (br $next)))
 (local.get $p)))`, simdPairExpression(family, simplified)))
}

// Independent lane oracle. No simplified Wasm output is used as a reference.
func simdPairWant(family string, x, y []byte) (out [16]byte) {
	switch family {
	case "average":
		for i := range out {
			out[i] = byte((uint16(x[i]) + uint16(y[i]) + 1) / 2)
		}
	case "high-byte":
		for i := 0; i < 4; i++ {
			binary.LittleEndian.PutUint32(out[i*4:], uint32(x[i*4+3])/64)
		}
	case "dot-sub", "dot-shifts":
		for i := 0; i < 4; i++ {
			a := int32(int16(binary.LittleEndian.Uint16(x[i*4:])))
			b := int32(int16(binary.LittleEndian.Uint16(x[i*4+2:])))
			binary.LittleEndian.PutUint32(out[i*4:], uint32(a-b))
		}
	default:
		panic("unknown SIMD pair")
	}
	return
}

func TestSIMDPairLaneIdentities(t *testing.T) {
	// S14: exhaust all byte pairs; the widest sum is 511, so i16 cannot wrap.
	for x := 0; x < 256; x++ {
		for y := 0; y < 256; y++ {
			widened := (uint16(x) + uint16(y) + 1) >> 1
			rounded := uint16(x/2 + y/2 + (x%2+y%2+1)/2)
			if widened != rounded || widened > 255 {
				t.Fatalf("average %d,%d", x, y)
			}
		}
	}
	// S15: low 24 bits cannot contribute to logical shift 30. Enumerate every
	// high byte and independently vary all low bytes, including their high bits.
	for high := uint32(0); high < 256; high++ {
		for _, low := range []uint32{0, 1, 0x7fffff, 0x800000, 0xffffff} {
			x := high<<24 | low
			if x>>30 != high/64 {
				t.Fatalf("high byte %#x", x)
			}
		}
	}
	// S16: dot(x,(1,0))-dot(x,(0,1)) = a-b = dot(x,(1,-1)).
	// Signed i16 inputs give [-65535,65535], which fits i32. The equality also
	// holds modulo 2^32. Sweep every a with five b extremes and zero crossings.
	for a := -32768; a <= 32767; a++ {
		for _, b := range []int{-32768, -1, 0, 1, 32767} {
			original := int64(a)*1 + int64(b)*0 - (int64(a)*0 + int64(b)*1)
			fused := int64(a)*1 + int64(b)*-1
			packed := uint32(uint16(int16(a))) | uint32(uint16(int16(b)))<<16
			shifted := (int32(packed<<16) >> 16) - (int32(packed) >> 16)
			if uint32(original) != uint32(fused) || uint32(original) != uint32(shifted) {
				t.Fatalf("dot %d,%d", a, b)
			}
		}
	}
}

func TestSIMDPairNearMissControls(t *testing.T) {
	// These valid semantic alternatives violate one premise. Reject them in
	// the lane model before they can become an execution oracle.
	cases := []struct {
		name           string
		correct, wrong [16]byte
	}{}
	x := []byte{255, 254, 253, 252, 251, 250, 249, 248, 247, 246, 245, 244, 243, 242, 241, 240}
	y := bytes.Repeat([]byte{255}, 16)
	avg := simdPairWant("average", x, y)
	var wrap, truncate, swap [16]byte
	for i := range wrap {
		wrap[i] = byte(x[i]+y[i]+1) >> 1
		truncate[i] = byte((uint16(x[i]) + uint16(y[i])) >> 1)
		swap[i] = avg[(i+8)%16]
	}
	cases = append(cases, struct {
		name           string
		correct, wrong [16]byte
	}{"narrow wrapping add", avg, wrap}, struct {
		name           string
		correct, wrong [16]byte
	}{"round down", avg, truncate}, struct {
		name           string
		correct, wrong [16]byte
	}{"swapped halves", avg, swap})
	x = []byte{7, 8, 0, 64, 19, 20, 64, 128, 31, 32, 128, 192, 43, 44, 192, 255}
	high := simdPairWant("high-byte", x, y)
	var byte2, signed, count5 [16]byte
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint32(byte2[i*4:], uint32(x[i*4+2])>>6)
		binary.LittleEndian.PutUint32(signed[i*4:], uint32(int32(int8(x[i*4+3]))>>6))
		binary.LittleEndian.PutUint32(count5[i*4:], uint32(x[i*4+3])>>5)
	}
	cases = append(cases, struct {
		name           string
		correct, wrong [16]byte
	}{"wrong byte mapping", high, byte2}, struct {
		name           string
		correct, wrong [16]byte
	}{"sign extension", high, signed}, struct {
		name           string
		correct, wrong [16]byte
	}{"wrong shift count", high, count5})
	dot := simdPairWant("dot-sub", x, y)
	wrongProducer := simdPairWant("dot-sub", y, x)
	cases = append(cases, struct {
		name           string
		correct, wrong [16]byte
	}{"different vector producer", dot, wrongProducer})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.correct == c.wrong {
				t.Fatal("near miss passed full-vector equality")
			}
		})
	}
}

type simdPairCode struct {
	bytes            []byte
	entry            uint32
	spills, literals int
}
type simdPairExecution struct {
	code                simdPairCode
	eng                 *wruntime.Engine
	jm                  *wruntime.JobMemory
	args, results, trap []byte
	entry               uintptr
}

func newSIMDPairExecution(t testing.TB, raw []byte, profile uint64) *simdPairExecution {
	t.Helper()
	c := compileSIMDPair(t, raw, profile)
	eng, err := wruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })
	jm, err := wruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { jm.Close() })
	ar, err := wruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ar.Close() })
	loaded, base, err := wruntime.MapCode(c.bytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { wruntime.Unmap(loaded) })
	if !bytes.Equal(loaded[:len(c.bytes)], c.bytes) {
		t.Fatal("loaded code mismatch")
	}
	t.Logf("Go=%s target=%s/%s profile=%x bounds=explicit API=Engine.Call source=%x loaded=%x source-B=%d native-B=%d spills=%d literal-B=%d", runtime.Version(), runtime.GOOS, runtime.GOARCH, profile, sha256.Sum256(raw), sha256.Sum256(loaded[:len(c.bytes)]), len(raw), len(c.bytes), c.spills, c.literals)
	return &simdPairExecution{code: c, eng: eng, jm: jm, args: ar.Alloc(16), results: ar.Alloc(16), trap: ar.Alloc(wruntime.TrapBufferBytes), entry: base + uintptr(c.entry)}
}

func (e *simdPairExecution) call(count int) error {
	binary.LittleEndian.PutUint32(e.args, uint32(count))
	if err := e.eng.Call(e.entry, e.args, e.jm.LinearMemory(), e.trap, e.results); err != nil {
		return err
	}
	if got := binary.LittleEndian.Uint32(e.results); got != uint32(count*48) {
		return fmt.Errorf("completed work=%d want %d", got, count*48)
	}
	return nil
}

func simdPairInput(count int) []byte {
	r := rand.New(rand.NewSource(809))
	input := make([]byte, count*48)
	for i := range input {
		input[i] = byte(r.Uint32())
	}
	// Distinct lanes include odd/max sums, high-byte mapping, and signed extremes.
	edge := []byte{0, 0, 255, 127, 0, 128, 255, 255, 1, 0, 254, 255, 1, 128, 254, 127}
	if count > 0 {
		copy(input, edge)
		for i := 16; i < 32; i++ {
			input[i] = 255
		}
	}
	return input
}

func checkSIMDPairMemory(family string, input, got []byte) error {
	if len(got) < len(input) {
		return fmt.Errorf("short memory")
	}
	for start := 0; start < len(input); start += 48 {
		if !bytes.Equal(got[start:start+32], input[start:start+32]) {
			return fmt.Errorf("input changed at record %d", start/48)
		}
		want := simdPairWant(family, input[start:start+16], input[start+16:start+32])
		if !bytes.Equal(got[start+32:start+48], want[:]) {
			return fmt.Errorf("record %d: got %x want %x", start/48, got[start+32:start+48], want)
		}
	}
	return nil
}

func TestSIMDSourcePairs(t *testing.T) {
	for _, family := range []string{"average", "high-byte", "dot-sub", "dot-shifts"} {
		for _, simple := range []bool{false, true} {
			raw := simdPairModule(t, family, simple)
			for _, profile := range simdPairProfiles() {
				t.Run(fmt.Sprintf("%s/simple=%v/profile=%x", family, simple, profile), func(t *testing.T) {
					e := newSIMDPairExecution(t, raw, profile)
					input := simdPairInput(64)
					copy(e.jm.LinearMemory(), input)
					if err := e.call(0); err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(e.jm.LinearMemory()[:len(input)], input) {
						t.Fatal("zero-work case changed memory")
					}
					if err := e.call(64); err != nil {
						t.Fatal(err)
					}
					if err := checkSIMDPairMemory(family, input, e.jm.LinearMemory()); err != nil {
						t.Fatal(err)
					}
					// The same gate rejects skipped work before any timing is accepted.
					if err := checkSIMDPairMemory(family, input, input); err == nil || !strings.HasPrefix(err.Error(), "record 0:") {
						t.Fatalf("skipped work control: %v", err)
					}
					// It also rejects a changed semantic output bit.
					stale := append([]byte(nil), e.jm.LinearMemory()[:len(input)]...)
					stale[32] ^= 1
					if err := checkSIMDPairMemory(family, input, stale); err == nil || !strings.HasPrefix(err.Error(), "record 0:") {
						t.Fatalf("changed result control: %v", err)
					}
				})
			}
		}
	}
}

func BenchmarkSIMDSourcePairs(b *testing.B) {
	for _, family := range []string{"average", "high-byte", "dot-sub", "dot-shifts"} {
		for _, simple := range []bool{false, true} {
			raw := simdPairModule(b, family, simple)
			for _, profile := range simdPairProfiles() {
				b.Run(fmt.Sprintf("%s/simple=%v/profile=%x", family, simple, profile), func(b *testing.B) {
					e := newSIMDPairExecution(b, raw, profile)
					input := simdPairInput(256)
					copy(e.jm.LinearMemory(), input)
					if err := e.call(256); err != nil {
						b.Fatal(err)
					}
					if err := checkSIMDPairMemory(family, input, e.jm.LinearMemory()); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if err := e.call(256); err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
					if err := checkSIMDPairMemory(family, input, e.jm.LinearMemory()); err != nil {
						b.Fatal(err)
					}
					b.ReportMetric(float64(len(e.code.bytes)), "native-B")
					b.ReportMetric(256, "vectors/op")
				})
			}
		}
	}
}

// This row includes decode, validation, and code generation. It excludes WAT
// assembly, executable mapping, and execution, and adds no production observer.
func BenchmarkSIMDSourcePairCompile(b *testing.B) {
	for _, family := range []string{"average", "high-byte", "dot-sub", "dot-shifts"} {
		for _, simple := range []bool{false, true} {
			raw := simdPairModule(b, family, simple)
			for _, profile := range simdPairProfiles() {
				b.Run(fmt.Sprintf("%s/simple=%v/profile=%x", family, simple, profile), func(b *testing.B) {
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
