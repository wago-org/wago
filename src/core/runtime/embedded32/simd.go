package embedded32

import (
	"encoding/binary"
	"math"
	"math/bits"
)

// V128 is the embedded helper representation. Generated code carries the same
// bits in four little-endian GPRs and spills them as four adjacent uint32 words.
type V128 [16]byte

// SIMDFrame is the common pointer-only helper ABI for computational SIMD
// operations. Scalar carries shift counts or splat input bits. A/B/C are ordered
// Wasm operands and Out receives the result. ScalarOut receives reductions and
// lane masks.
type SIMDFrame struct {
	Op        uint32
	Scalar    uint64
	A, B, C   V128
	Immediate V128
	Out       V128
	ScalarOut uint64
	Memory    []byte
	Address   uint32
	Lane      uint32
	Trap      Trap
}

func laneU(v *V128, width, lane int) uint64 {
	off := lane * width / 8
	switch width {
	case 8:
		return uint64(v[off])
	case 16:
		return uint64(binary.LittleEndian.Uint16(v[off:]))
	case 32:
		return uint64(binary.LittleEndian.Uint32(v[off:]))
	case 64:
		return binary.LittleEndian.Uint64(v[off:])
	default:
		panic("embedded32: invalid lane width")
	}
}
func laneS(v *V128, width, lane int) int64 {
	u := laneU(v, width, lane)
	shift := 64 - width
	return int64(u<<shift) >> shift
}
func putLane(v *V128, width, lane int, x uint64) {
	off := lane * width / 8
	switch width {
	case 8:
		v[off] = byte(x)
	case 16:
		binary.LittleEndian.PutUint16(v[off:], uint16(x))
	case 32:
		binary.LittleEndian.PutUint32(v[off:], uint32(x))
	case 64:
		binary.LittleEndian.PutUint64(v[off:], x)
	default:
		panic("embedded32: invalid lane width")
	}
}
func laneCount(width int) int { return 128 / width }
func laneMask(width int) uint64 {
	if width == 64 {
		return ^uint64(0)
	}
	return uint64(1)<<width - 1
}
func boolLane(ok bool, width int) uint64 {
	if ok {
		return laneMask(width)
	}
	return 0
}
func signedBounds(width int) (int64, int64) {
	if width == 64 {
		return math.MinInt64, math.MaxInt64
	}
	return -(int64(1) << (width - 1)), (int64(1) << (width - 1)) - 1
}
func satS(x int64, width int) uint64 {
	lo, hi := signedBounds(width)
	if x < lo {
		x = lo
	}
	if x > hi {
		x = hi
	}
	return uint64(x) & laneMask(width)
}
func satU(x int64, width int) uint64 {
	if x <= 0 {
		return 0
	}
	max := laneMask(width)
	if uint64(x) > max {
		return max
	}
	return uint64(x)
}
func f32(v *V128, lane int) float32       { return math.Float32frombits(uint32(laneU(v, 32, lane))) }
func putF32(v *V128, lane int, x float32) { putLane(v, 32, lane, uint64(math.Float32bits(x))) }
func f64(v *V128, lane int) float64       { return math.Float64frombits(laneU(v, 64, lane)) }
func putF64(v *V128, lane int, x float64) { putLane(v, 64, lane, math.Float64bits(x)) }
func quiet32(x uint32) uint32 {
	if x&0x7f800000 != 0x7f800000 || x&0x007fffff == 0 {
		return 0x7fc00000
	}
	return x | 0x00400000
}
func minmax32(aBits, bBits uint32, max bool) uint32 {
	a, b := math.Float32frombits(aBits), math.Float32frombits(bBits)
	if math.IsNaN(float64(a)) {
		return quiet32(aBits)
	}
	if math.IsNaN(float64(b)) {
		return quiet32(bBits)
	}
	if a == b {
		if a == 0 {
			if max {
				return aBits & bBits
			}
			return aBits | bBits
		}
		return aBits
	}
	if max {
		if a > b {
			return aBits
		}
		return bBits
	}
	if a < b {
		return aBits
	}
	return bBits
}
func pminmax32(aBits, bBits uint32, max bool) uint32 {
	a, b := math.Float32frombits(aBits), math.Float32frombits(bBits)
	if max {
		if a < b {
			return bBits
		}
		return aBits
	}
	if b < a {
		return bBits
	}
	return aBits
}
func pminmax64(aBits, bBits uint64, max bool) uint64 {
	a, b := math.Float64frombits(aBits), math.Float64frombits(bBits)
	if max {
		if a < b {
			return bBits
		}
		return aBits
	}
	if b < a {
		return bBits
	}
	return aBits
}

// SIMDHelperValid mirrors the decoder's complete 256-instruction SIMD and
// relaxed-SIMD inventory. Simple instructions may still be lowered directly,
// while this helper remains the complete compact correctness fallback.
func SIMDHelperValid(op uint32) bool {
	if op > 275 {
		return false
	}
	switch op {
	case 154, 162, 165, 166, 175, 176, 178, 179, 180, 187, 194, 197, 198, 207, 208, 210, 211, 212, 226, 238:
		return false // reserved proposal-table holes
	}
	return true
}

// RunSIMD executes the portable 32-bit helper baseline. Simple word-wise and
// packed add/sub operations also have direct encoder sequences; this helper is
// the correctness fallback for high-register-pressure and complex operations.
//
//export wago_embedded32_simd
func RunSIMD(f *SIMDFrame) {
	if !SIMDHelperValid(f.Op) {
		panic("embedded32: invalid SIMD helper opcode")
	}
	f.Out, f.ScalarOut, f.Trap = V128{}, 0, TrapNone
	op := f.Op

	if op <= 11 || op >= 84 && op <= 93 {
		runSIMDMemory(f)
		return
	}
	if op == 12 {
		f.Out = f.Immediate
		return
	}
	if op == 13 {
		for i, x := range f.Immediate {
			if x < 16 {
				f.Out[i] = f.A[x]
			} else {
				f.Out[i] = f.B[x-16]
			}
		}
		return
	}
	if op >= 21 && op <= 34 {
		runSIMDLane(f)
		return
	}
	if op == 14 || op == 256 { // strict deterministic swizzle projection
		for i, x := range f.B {
			if x < 16 {
				f.Out[i] = f.A[x]
			}
		}
		return
	}
	if op >= 15 && op <= 20 {
		width := [...]int{8, 16, 32, 64, 32, 64}[op-15]
		for i := 0; i < laneCount(width); i++ {
			putLane(&f.Out, width, i, f.Scalar)
		}
		return
	}
	if op >= 35 && op <= 64 {
		width, base := 8, uint32(35)
		if op >= 45 {
			width, base = 16, 45
		}
		if op >= 55 {
			width, base = 32, 55
		}
		kind := op - base
		for i := 0; i < laneCount(width); i++ {
			au, bu := laneU(&f.A, width, i), laneU(&f.B, width, i)
			as, bs := laneS(&f.A, width, i), laneS(&f.B, width, i)
			var ok bool
			switch kind {
			case 0:
				ok = au == bu
			case 1:
				ok = au != bu
			case 2:
				ok = as < bs
			case 3:
				ok = au < bu
			case 4:
				ok = as > bs
			case 5:
				ok = au > bu
			case 6:
				ok = as <= bs
			case 7:
				ok = au <= bu
			case 8:
				ok = as >= bs
			case 9:
				ok = au >= bu
			}
			putLane(&f.Out, width, i, boolLane(ok, width))
		}
		return
	}
	if op >= 65 && op <= 76 {
		width, base := 32, uint32(65)
		if op >= 71 {
			width, base = 64, 71
		}
		kind := op - base
		for i := 0; i < laneCount(width); i++ {
			var a, b float64
			if width == 32 {
				a, b = float64(f32(&f.A, i)), float64(f32(&f.B, i))
			} else {
				a, b = f64(&f.A, i), f64(&f.B, i)
			}
			var ok bool
			switch kind {
			case 0:
				ok = a == b
			case 1:
				ok = a != b
			case 2:
				ok = a < b
			case 3:
				ok = a > b
			case 4:
				ok = a <= b
			case 5:
				ok = a >= b
			}
			putLane(&f.Out, width, i, boolLane(ok, width))
		}
		return
	}
	switch op {
	case 77:
		for i := range f.Out {
			f.Out[i] = ^f.A[i]
		}
		return
	case 78, 79, 80, 81:
		for i := range f.Out {
			switch op {
			case 78:
				f.Out[i] = f.A[i] & f.B[i]
			case 79:
				f.Out[i] = f.A[i] &^ f.B[i]
			case 80:
				f.Out[i] = f.A[i] | f.B[i]
			case 81:
				f.Out[i] = f.A[i] ^ f.B[i]
			}
		}
		return
	case 82, 265, 266, 267, 268:
		for i := range f.Out {
			f.Out[i] = (f.A[i] & f.C[i]) | (f.B[i] &^ f.C[i])
		}
		return
	case 83:
		for _, x := range f.A {
			if x != 0 {
				f.ScalarOut = 1
				break
			}
		}
		return
	case 94:
		putF32(&f.Out, 0, float32(f64(&f.A, 0)))
		putF32(&f.Out, 1, float32(f64(&f.A, 1)))
		return
	case 95:
		putF64(&f.Out, 0, float64(f32(&f.A, 0)))
		putF64(&f.Out, 1, float64(f32(&f.A, 1)))
		return
	}

	if op == 96 || op == 97 || op == 98 || op == 128 || op == 129 || op == 160 || op == 161 || op == 192 || op == 193 {
		width := 8
		if op >= 128 {
			width = 16
		}
		if op >= 160 {
			width = 32
		}
		if op >= 192 {
			width = 64
		}
		kind := op
		for i := 0; i < laneCount(width); i++ {
			x := laneS(&f.A, width, i)
			var y uint64
			switch kind {
			case 96, 128, 160, 192:
				if x < 0 {
					y = uint64(-x)
				} else {
					y = uint64(x)
				}
			case 97, 129, 161, 193:
				y = uint64(-x)
			case 98:
				y = uint64(bits.OnesCount8(uint8(x)))
			}
			putLane(&f.Out, width, i, y)
		}
		return
	}
	if op == 99 || op == 131 || op == 163 || op == 195 {
		width := 8
		if op == 131 {
			width = 16
		}
		if op == 163 {
			width = 32
		}
		if op == 195 {
			width = 64
		}
		f.ScalarOut = 1
		for i := 0; i < laneCount(width); i++ {
			if laneU(&f.A, width, i) == 0 {
				f.ScalarOut = 0
				break
			}
		}
		return
	}
	if op == 100 || op == 132 || op == 164 || op == 196 {
		width := 8
		if op == 132 {
			width = 16
		}
		if op == 164 {
			width = 32
		}
		if op == 196 {
			width = 64
		}
		for i := 0; i < laneCount(width); i++ {
			f.ScalarOut |= (laneU(&f.A, width, i) >> uint(width-1) & 1) << uint(i)
		}
		return
	}
	if op >= 101 && op <= 102 {
		narrow(f, 16, 8, op == 101)
		return
	}
	if op >= 133 && op <= 134 {
		narrow(f, 32, 16, op == 133)
		return
	}
	if op >= 135 && op <= 138 {
		extend(f, 8, 16, op == 135 || op == 136, op == 136 || op == 138)
		return
	}
	if op >= 167 && op <= 170 {
		extend(f, 16, 32, op == 167 || op == 168, op == 168 || op == 170)
		return
	}
	if op >= 199 && op <= 202 {
		extend(f, 32, 64, op == 199 || op == 200, op == 200 || op == 202)
		return
	}

	if (op >= 107 && op <= 109) || (op >= 139 && op <= 141) || (op >= 171 && op <= 173) || (op >= 203 && op <= 205) {
		width, base := 8, uint32(107)
		if op >= 139 {
			width, base = 16, 139
		}
		if op >= 171 {
			width, base = 32, 171
		}
		if op >= 203 {
			width, base = 64, 203
		}
		sh := uint(f.Scalar % uint64(width))
		for i := 0; i < laneCount(width); i++ {
			var x uint64
			switch op - base {
			case 0:
				x = laneU(&f.A, width, i) << sh
			case 1:
				x = uint64(laneS(&f.A, width, i) >> sh)
			case 2:
				x = laneU(&f.A, width, i) >> sh
			}
			putLane(&f.Out, width, i, x)
		}
		return
	}
	if op == 110 || op == 113 || op == 142 || op == 145 || op == 174 || op == 177 || op == 206 || op == 209 {
		width, sub := 8, op == 113
		if op == 142 || op == 145 {
			width, sub = 16, op == 145
		}
		if op == 174 || op == 177 {
			width, sub = 32, op == 177
		}
		if op == 206 || op == 209 {
			width, sub = 64, op == 209
		}
		for i := 0; i < laneCount(width); i++ {
			x, y := laneU(&f.A, width, i), laneU(&f.B, width, i)
			if sub {
				x -= y
			} else {
				x += y
			}
			putLane(&f.Out, width, i, x)
		}
		return
	}
	if (op >= 111 && op <= 115) || (op >= 143 && op <= 147) {
		width := 8
		if op >= 143 {
			width = 16
		}
		signed := op == 111 || op == 114 || op == 143 || op == 146
		sub := op == 114 || op == 115 || op == 146 || op == 147
		for i := 0; i < laneCount(width); i++ {
			var out uint64
			if signed {
				x, y := laneS(&f.A, width, i), laneS(&f.B, width, i)
				if sub {
					out = satS(x-y, width)
				} else {
					out = satS(x+y, width)
				}
			} else {
				x, y := int64(laneU(&f.A, width, i)), int64(laneU(&f.B, width, i))
				if sub {
					out = satU(x-y, width)
				} else {
					out = satU(x+y, width)
				}
			}
			putLane(&f.Out, width, i, out)
		}
		return
	}
	if op == 118 || op == 119 || op == 120 || op == 121 || op == 150 || op == 151 || op == 152 || op == 153 || op == 182 || op == 183 || op == 184 || op == 185 {
		width := 8
		if op >= 150 {
			width = 16
		}
		if op >= 182 {
			width = 32
		}
		signed := op == 118 || op == 120 || op == 150 || op == 152 || op == 182 || op == 184
		max := op == 120 || op == 121 || op == 152 || op == 153 || op == 184 || op == 185
		for i := 0; i < laneCount(width); i++ {
			a, b := laneU(&f.A, width, i), laneU(&f.B, width, i)
			takeB := false
			if signed {
				as, bs := laneS(&f.A, width, i), laneS(&f.B, width, i)
				if max {
					takeB = bs > as
				} else {
					takeB = bs < as
				}
			} else if max {
				takeB = b > a
			} else {
				takeB = b < a
			}
			if takeB {
				a = b
			}
			putLane(&f.Out, width, i, a)
		}
		return
	}
	if op == 123 || op == 155 {
		width := 8
		if op == 155 {
			width = 16
		}
		for i := 0; i < laneCount(width); i++ {
			putLane(&f.Out, width, i, (laneU(&f.A, width, i)+laneU(&f.B, width, i)+1)>>1)
		}
		return
	}
	if op == 124 || op == 125 || op == 126 || op == 127 {
		inW, outW, signed := 8, 16, op == 124
		if op >= 126 {
			inW, outW, signed = 16, 32, op == 126
		}
		for i := 0; i < laneCount(outW); i++ {
			var x int64
			if signed {
				x = laneS(&f.A, inW, 2*i) + laneS(&f.A, inW, 2*i+1)
			} else {
				x = int64(laneU(&f.A, inW, 2*i) + laneU(&f.A, inW, 2*i+1))
			}
			putLane(&f.Out, outW, i, uint64(x))
		}
		return
	}
	if op == 130 || op == 273 {
		for i := 0; i < 8; i++ {
			x := laneS(&f.A, 16, i) * laneS(&f.B, 16, i)
			v := (x + 0x4000) >> 15
			if v > 32767 {
				v = 32767
			}
			if v < -32768 {
				v = -32768
			}
			putLane(&f.Out, 16, i, uint64(v))
		}
		return
	}
	if op == 149 || op == 181 || op == 213 {
		width := 16
		if op == 181 {
			width = 32
		}
		if op == 213 {
			width = 64
		}
		for i := 0; i < laneCount(width); i++ {
			putLane(&f.Out, width, i, laneU(&f.A, width, i)*laneU(&f.B, width, i))
		}
		return
	}
	if op == 156 || op == 157 || op == 158 || op == 159 {
		extmul(f, 8, 16, op == 156 || op == 157, op == 157 || op == 159)
		return
	}
	if op == 188 || op == 189 || op == 190 || op == 191 {
		extmul(f, 16, 32, op == 188 || op == 189, op == 189 || op == 191)
		return
	}
	if op == 220 || op == 221 || op == 222 || op == 223 {
		extmul(f, 32, 64, op == 220 || op == 221, op == 221 || op == 223)
		return
	}
	if op == 186 {
		for i := 0; i < 4; i++ {
			x := laneS(&f.A, 16, 2*i)*laneS(&f.B, 16, 2*i) + laneS(&f.A, 16, 2*i+1)*laneS(&f.B, 16, 2*i+1)
			putLane(&f.Out, 32, i, uint64(x))
		}
		return
	}
	if op >= 214 && op <= 219 {
		kind := op - 214
		for i := 0; i < 2; i++ {
			a, b := laneS(&f.A, 64, i), laneS(&f.B, 64, i)
			var ok bool
			switch kind {
			case 0:
				ok = a == b
			case 1:
				ok = a != b
			case 2:
				ok = a < b
			case 3:
				ok = a > b
			case 4:
				ok = a <= b
			case 5:
				ok = a >= b
			}
			putLane(&f.Out, 64, i, boolLane(ok, 64))
		}
		return
	}

	if op == 224 || op == 236 {
		width := 32
		if op == 236 {
			width = 64
		}
		for i := 0; i < laneCount(width); i++ {
			putLane(&f.Out, width, i, laneU(&f.A, width, i)&^(uint64(1)<<uint(width-1)))
		}
		return
	}
	if op == 225 || op == 237 {
		width := 32
		if op == 237 {
			width = 64
		}
		for i := 0; i < laneCount(width); i++ {
			putLane(&f.Out, width, i, laneU(&f.A, width, i)^(uint64(1)<<uint(width-1)))
		}
		return
	}
	if op == 103 || op == 104 || op == 105 || op == 106 || op == 116 || op == 117 || op == 122 || op == 148 || op == 227 || op == 239 {
		floatUnary(f)
		return
	}
	if (op >= 228 && op <= 235) || (op >= 240 && op <= 247) || (op >= 269 && op <= 272) {
		floatBinary(f)
		return
	}
	if op >= 248 && op <= 255 || op >= 257 && op <= 260 {
		floatConvert(f)
		return
	}
	if op >= 261 && op <= 264 {
		floatMadd(f)
		return
	}
	if op == 274 || op == 275 {
		relaxedDot(f, op == 275)
		return
	}
	panic("embedded32: unimplemented valid SIMD helper opcode")
}

func memoryWindow(f *SIMDFrame, n int) ([]byte, bool) {
	end := uint64(f.Address) + uint64(n)
	if end > uint64(len(f.Memory)) {
		f.Trap = TrapMemoryOutOfBounds
		return nil, false
	}
	return f.Memory[int(f.Address):int(end)], true
}

func runSIMDMemory(f *SIMDFrame) {
	op := f.Op
	switch op {
	case 0: // v128.load
		if p, ok := memoryWindow(f, 16); ok {
			copy(f.Out[:], p)
		}
	case 1, 2: // load8x8_s/u
		p, ok := memoryWindow(f, 8)
		if !ok {
			return
		}
		for i, x := range p {
			if op == 1 {
				putLane(&f.Out, 16, i, uint64(int16(int8(x))))
			} else {
				putLane(&f.Out, 16, i, uint64(x))
			}
		}
	case 3, 4: // load16x4_s/u
		p, ok := memoryWindow(f, 8)
		if !ok {
			return
		}
		for i := 0; i < 4; i++ {
			x := binary.LittleEndian.Uint16(p[i*2:])
			if op == 3 {
				putLane(&f.Out, 32, i, uint64(int32(int16(x))))
			} else {
				putLane(&f.Out, 32, i, uint64(x))
			}
		}
	case 5, 6: // load32x2_s/u
		p, ok := memoryWindow(f, 8)
		if !ok {
			return
		}
		for i := 0; i < 2; i++ {
			x := binary.LittleEndian.Uint32(p[i*4:])
			if op == 5 {
				putLane(&f.Out, 64, i, uint64(int64(int32(x))))
			} else {
				putLane(&f.Out, 64, i, uint64(x))
			}
		}
	case 7, 8, 9, 10: // splat loads
		width := 8 << uint(op-7)
		p, ok := memoryWindow(f, width/8)
		if !ok {
			return
		}
		var x uint64
		switch width {
		case 8:
			x = uint64(p[0])
		case 16:
			x = uint64(binary.LittleEndian.Uint16(p))
		case 32:
			x = uint64(binary.LittleEndian.Uint32(p))
		case 64:
			x = binary.LittleEndian.Uint64(p)
		}
		for i := 0; i < laneCount(width); i++ {
			putLane(&f.Out, width, i, x)
		}
	case 11: // v128.store, complete-width preflight before mutation
		if p, ok := memoryWindow(f, 16); ok {
			copy(p, f.A[:])
		}
	case 84, 85, 86, 87: // lane loads
		width := 8 << uint(op-84)
		if int(f.Lane) >= laneCount(width) {
			panic("embedded32: invalid SIMD load lane")
		}
		p, ok := memoryWindow(f, width/8)
		if !ok {
			return
		}
		f.Out = f.A
		var x uint64
		switch width {
		case 8:
			x = uint64(p[0])
		case 16:
			x = uint64(binary.LittleEndian.Uint16(p))
		case 32:
			x = uint64(binary.LittleEndian.Uint32(p))
		case 64:
			x = binary.LittleEndian.Uint64(p)
		}
		putLane(&f.Out, width, int(f.Lane), x)
	case 88, 89, 90, 91: // lane stores
		width := 8 << uint(op-88)
		if int(f.Lane) >= laneCount(width) {
			panic("embedded32: invalid SIMD store lane")
		}
		p, ok := memoryWindow(f, width/8)
		if !ok {
			return
		}
		x := laneU(&f.A, width, int(f.Lane))
		switch width {
		case 8:
			p[0] = byte(x)
		case 16:
			binary.LittleEndian.PutUint16(p, uint16(x))
		case 32:
			binary.LittleEndian.PutUint32(p, uint32(x))
		case 64:
			binary.LittleEndian.PutUint64(p, x)
		}
	case 92:
		if p, ok := memoryWindow(f, 4); ok {
			copy(f.Out[:4], p)
		}
	case 93:
		if p, ok := memoryWindow(f, 8); ok {
			copy(f.Out[:8], p)
		}
	default:
		panic("embedded32: invalid SIMD memory opcode")
	}
}

func runSIMDLane(f *SIMDFrame) {
	op := f.Op
	width := 8
	switch {
	case op >= 24 && op <= 26:
		width = 16
	case op >= 27 && op <= 28:
		width = 32
	case op >= 29 && op <= 30:
		width = 64
	case op >= 31 && op <= 32:
		width = 32
	case op >= 33:
		width = 64
	}
	if int(f.Lane) >= laneCount(width) {
		panic("embedded32: invalid SIMD lane")
	}
	replace := op == 23 || op == 26 || op == 28 || op == 30 || op == 32 || op == 34
	if replace {
		f.Out = f.A
		putLane(&f.Out, width, int(f.Lane), f.Scalar)
		return
	}
	x := laneU(&f.A, width, int(f.Lane))
	if op == 21 {
		x = uint64(uint32(int32(int8(x))))
	}
	if op == 24 {
		x = uint64(uint32(int32(int16(x))))
	}
	f.ScalarOut = x
}

func narrow(f *SIMDFrame, inW, outW int, signed bool) {
	n := laneCount(inW)
	for i := 0; i < n; i++ {
		var x uint64
		if signed {
			x = satS(laneS(&f.A, inW, i), outW)
		} else {
			x = satU(laneS(&f.A, inW, i), outW)
		}
		putLane(&f.Out, outW, i, x)
	}
	for i := 0; i < n; i++ {
		var x uint64
		if signed {
			x = satS(laneS(&f.B, inW, i), outW)
		} else {
			x = satU(laneS(&f.B, inW, i), outW)
		}
		putLane(&f.Out, outW, n+i, x)
	}
}
func extend(f *SIMDFrame, inW, outW int, signed, high bool) {
	start := 0
	if high {
		start = laneCount(inW) / 2
	}
	for i := 0; i < laneCount(outW); i++ {
		if signed {
			putLane(&f.Out, outW, i, uint64(laneS(&f.A, inW, start+i)))
		} else {
			putLane(&f.Out, outW, i, laneU(&f.A, inW, start+i))
		}
	}
}
func extmul(f *SIMDFrame, inW, outW int, signed, high bool) {
	start := 0
	if high {
		start = laneCount(inW) / 2
	}
	for i := 0; i < laneCount(outW); i++ {
		if signed {
			putLane(&f.Out, outW, i, uint64(laneS(&f.A, inW, start+i)*laneS(&f.B, inW, start+i)))
		} else {
			putLane(&f.Out, outW, i, laneU(&f.A, inW, start+i)*laneU(&f.B, inW, start+i))
		}
	}
}
func floatUnary(f *SIMDFrame) {
	width := 32
	if f.Op == 116 || f.Op == 117 || f.Op == 122 || f.Op == 148 || f.Op == 239 {
		width = 64
	}
	for i := 0; i < laneCount(width); i++ {
		if width == 32 {
			x := float64(f32(&f.A, i))
			switch f.Op {
			case 103:
				x = math.Ceil(x)
			case 104:
				x = math.Floor(x)
			case 105:
				x = math.Trunc(x)
			case 106:
				x = math.RoundToEven(x)
			case 227:
				x = math.Sqrt(x)
			}
			if x == 0 {
				x = math.Copysign(0, float64(f32(&f.A, i)))
			}
			putF32(&f.Out, i, float32(x))
		} else {
			x := f64(&f.A, i)
			switch f.Op {
			case 116:
				x = math.Ceil(x)
			case 117:
				x = math.Floor(x)
			case 122:
				x = math.Trunc(x)
			case 148:
				x = math.RoundToEven(x)
			case 239:
				x = math.Sqrt(x)
			}
			if x == 0 {
				x = math.Copysign(0, f64(&f.A, i))
			}
			putF64(&f.Out, i, x)
		}
	}
}
func floatBinary(f *SIMDFrame) {
	width := 32
	if f.Op >= 240 && f.Op <= 247 || f.Op >= 271 {
		width = 64
	}
	for i := 0; i < laneCount(width); i++ {
		if width == 32 {
			ab, bb := uint32(laneU(&f.A, 32, i)), uint32(laneU(&f.B, 32, i))
			a, b := math.Float32frombits(ab), math.Float32frombits(bb)
			switch f.Op {
			case 228:
				putF32(&f.Out, i, a+b)
			case 229:
				putF32(&f.Out, i, a-b)
			case 230:
				putF32(&f.Out, i, a*b)
			case 231:
				putF32(&f.Out, i, a/b)
			case 232, 269:
				putLane(&f.Out, 32, i, uint64(minmax32(ab, bb, false)))
			case 233, 270:
				putLane(&f.Out, 32, i, uint64(minmax32(ab, bb, true)))
			case 234:
				putLane(&f.Out, 32, i, uint64(pminmax32(ab, bb, false)))
			case 235:
				putLane(&f.Out, 32, i, uint64(pminmax32(ab, bb, true)))
			}
		} else {
			ab, bb := laneU(&f.A, 64, i), laneU(&f.B, 64, i)
			a, b := math.Float64frombits(ab), math.Float64frombits(bb)
			switch f.Op {
			case 240:
				putF64(&f.Out, i, a+b)
			case 241:
				putF64(&f.Out, i, a-b)
			case 242:
				putF64(&f.Out, i, a*b)
			case 243:
				putF64(&f.Out, i, a/b)
			case 244, 271:
				putLane(&f.Out, 64, i, minmax64(ab, bb, false))
			case 245, 272:
				putLane(&f.Out, 64, i, minmax64(ab, bb, true))
			case 246:
				putLane(&f.Out, 64, i, pminmax64(ab, bb, false))
			case 247:
				putLane(&f.Out, 64, i, pminmax64(ab, bb, true))
			}
		}
	}
}
func satTrunc(x float64, signed bool) uint64 {
	if math.IsNaN(x) {
		return 0
	}
	if signed {
		if x <= -0x1p31 {
			return 0x80000000
		}
		if x >= 0x1p31 {
			return 0x7fffffff
		}
		return uint64(uint32(int32(math.Trunc(x))))
	}
	if x <= 0 {
		return 0
	}
	if x >= 0x1p32 {
		return math.MaxUint32
	}
	return uint64(uint32(math.Trunc(x)))
}
func floatConvert(f *SIMDFrame) {
	switch f.Op {
	case 248, 249, 257, 258:
		for i := 0; i < 4; i++ {
			putLane(&f.Out, 32, i, satTrunc(float64(f32(&f.A, i)), f.Op == 248 || f.Op == 257))
		}
	case 250, 251:
		for i := 0; i < 4; i++ {
			if f.Op == 250 {
				putF32(&f.Out, i, float32(int32(laneU(&f.A, 32, i))))
			} else {
				putF32(&f.Out, i, float32(uint32(laneU(&f.A, 32, i))))
			}
		}
	case 252, 253, 259, 260:
		for i := 0; i < 2; i++ {
			putLane(&f.Out, 32, i, satTrunc(f64(&f.A, i), f.Op == 252 || f.Op == 259))
		}
	case 254, 255:
		for i := 0; i < 2; i++ {
			if f.Op == 254 {
				putF64(&f.Out, i, float64(int32(laneU(&f.A, 32, i))))
			} else {
				putF64(&f.Out, i, float64(uint32(laneU(&f.A, 32, i))))
			}
		}
	}
}
func floatMadd(f *SIMDFrame) {
	width := 32
	if f.Op >= 263 {
		width = 64
	}
	neg := f.Op == 262 || f.Op == 264
	for i := 0; i < laneCount(width); i++ {
		if width == 32 {
			x := f32(&f.A, i) * f32(&f.B, i)
			if neg {
				x = -x
			}
			putF32(&f.Out, i, x+f32(&f.C, i))
		} else {
			x := f64(&f.A, i) * f64(&f.B, i)
			if neg {
				x = -x
			}
			putF64(&f.Out, i, x+f64(&f.C, i))
		}
	}
}
func relaxedDot(f *SIMDFrame, add bool) {
	if !add {
		for i := 0; i < 8; i++ {
			sum := laneS(&f.A, 8, 2*i)*laneS(&f.B, 8, 2*i) +
				laneS(&f.A, 8, 2*i+1)*laneS(&f.B, 8, 2*i+1)
			putLane(&f.Out, 16, i, uint64(sum))
		}
		return
	}
	for i := 0; i < 4; i++ {
		sum := laneS(&f.C, 32, i)
		for j := 0; j < 4; j++ {
			k := 4*i + j
			sum += laneS(&f.A, 8, k) * laneS(&f.B, 8, k)
		}
		putLane(&f.Out, 32, i, uint64(sum))
	}
}
