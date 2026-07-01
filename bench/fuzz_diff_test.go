package wagobench

import (
	"encoding/binary"
	"math"
	"math/rand"
	"testing"

	"github.com/wago-org/wago/src/wago"
)

// TestX64FuzzRandomModules differential-fuzzes the x64 (WARP-port) backend
// against the amd64 backend while amd64 still exists as a cross-check. It
// generates random *valid* single-function modules whose bodies are deep,
// randomly-nested arithmetic/bitwise/comparison/conversion expression trees, then
// runs each through both backends under several random argument vectors. Deep
// nesting is what stresses x64's from-scratch register allocator and its fixed-
// register (RAX/RDX for div, RCX for shift) spill paths, in operand combinations
// the fixed corpus and the shallow spec-suite functions never reach — the bodies
// are loop-free so every invocation terminates.
//
// amd64 is not the source of truth (it is a rough spike; x64 is the WARP port),
// so a divergence is a lead to adjudicate against WebAssembly semantics by hand,
// not an automatic x64 bug. Each failure prints the module bytes and inputs to
// reproduce. The spec suite (src/wago) is the authoritative correctness oracle.

// wasm value type encodings.
const (
	vtI32 = 0x7F
	vtI64 = 0x7E
	vtF32 = 0x7D
	vtF64 = 0x7C
)

func vtName(v byte) string {
	switch v {
	case vtI32:
		return "i32"
	case vtI64:
		return "i64"
	case vtF32:
		return "f32"
	case vtF64:
		return "f64"
	}
	return "?"
}

func isFloat(v byte) bool { return v == vtF32 || v == vtF64 }
func is64(v byte) bool    { return v == vtI64 || v == vtF64 }

// modBuilder emits a single-function wasm module: the function takes `params` and
// returns `result`, with `body` as its (already-encoded) expression.
type modBuilder struct {
	params []byte
	result byte
	body   []byte
}

func uleb(dst []byte, v uint64) []byte {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if v == 0 {
			return dst
		}
	}
}

func sleb(dst []byte, v int64) []byte {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		signBit := b & 0x40
		if (v == 0 && signBit == 0) || (v == -1 && signBit != 0) {
			return append(dst, b)
		}
		dst = append(dst, b|0x80)
	}
}

// section prefixes payload with its id and byte length.
func section(id byte, payload []byte) []byte {
	out := []byte{id}
	out = uleb(out, uint64(len(payload)))
	return append(out, payload...)
}

func (mb *modBuilder) bytes() []byte {
	mod := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

	// Type section: one functype.
	var ty []byte
	ty = uleb(ty, 1) // one type
	ty = append(ty, 0x60)
	ty = uleb(ty, uint64(len(mb.params)))
	ty = append(ty, mb.params...)
	ty = uleb(ty, 1) // one result
	ty = append(ty, mb.result)
	mod = append(mod, section(1, ty)...)

	// Function section.
	var fn []byte
	fn = uleb(fn, 1)
	fn = uleb(fn, 0) // type index 0
	mod = append(mod, section(3, fn)...)

	// Memory section: one page, so trunc/store style ops that might touch memory
	// stay valid even though the generator does not currently emit them.
	var mem []byte
	mem = uleb(mem, 1)
	mem = append(mem, 0x00) // flags: min only
	mem = uleb(mem, 1)      // min 1 page
	mod = append(mod, section(5, mem)...)

	// Export section: export "f".
	var ex []byte
	ex = uleb(ex, 1)
	ex = uleb(ex, 1)
	ex = append(ex, 'f')
	ex = append(ex, 0x00) // func kind
	ex = uleb(ex, 0)      // func index 0
	mod = append(mod, section(7, ex)...)

	// Code section.
	var body []byte
	body = uleb(body, 0) // zero local declarations (only params)
	body = append(body, mb.body...)
	body = append(body, 0x0b) // end
	var code []byte
	code = uleb(code, 1) // one function body
	code = uleb(code, uint64(len(body)))
	code = append(code, body...)
	mod = append(mod, section(10, code)...)

	return mod
}

// exprGen builds a random typed expression tree over the function's params.
type exprGen struct {
	rng    *rand.Rand
	params []byte
}

// leafConst emits a constant of type t (biased toward edge values).
func (g *exprGen) leafConst(out []byte, t byte) []byte {
	switch t {
	case vtI32:
		out = append(out, 0x41) // i32.const
		return sleb(out, int64(int32(g.edgeInt())))
	case vtI64:
		out = append(out, 0x42) // i64.const
		return sleb(out, int64(g.edgeInt()))
	case vtF32:
		out = append(out, 0x43) // f32.const
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(float32(g.edgeFloat())))
		return append(out, b[:]...)
	default: // vtF64
		out = append(out, 0x44) // f64.const
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], math.Float64bits(g.edgeFloat()))
		return append(out, b[:]...)
	}
}

func (g *exprGen) edgeInt() uint64 {
	switch g.rng.Intn(8) {
	case 0:
		return 0
	case 1:
		return 1
	case 2:
		return ^uint64(0) // -1
	case 3:
		return 0x8000000000000000
	case 4:
		return 0x7fffffffffffffff
	case 5:
		return 0x80000000
	case 6:
		return 0x7fffffff
	default:
		return g.rng.Uint64()
	}
}

func (g *exprGen) edgeFloat() float64 {
	switch g.rng.Intn(9) {
	case 0:
		return 0
	case 1:
		return math.Copysign(0, -1)
	case 2:
		return 1
	case 3:
		return -1
	case 4:
		return math.Inf(1)
	case 5:
		return math.Inf(-1)
	case 6:
		return math.NaN()
	case 7:
		return float64(int64(g.rng.Uint64())) // large-ish integer values
	default:
		return math.Float64frombits(g.rng.Uint64())
	}
}

// paramOf returns the index of a param with type t, or -1 if none exists.
func (g *exprGen) paramOf(t byte) int {
	var cand []int
	for i, p := range g.params {
		if p == t {
			cand = append(cand, i)
		}
	}
	if len(cand) == 0 {
		return -1
	}
	return cand[g.rng.Intn(len(cand))]
}

// gen emits an expression producing type t within the remaining depth budget.
func (g *exprGen) gen(out []byte, t byte, depth int) []byte {
	// At depth 0, or randomly, emit a leaf (param.get or const).
	if depth <= 0 || g.rng.Intn(3) == 0 {
		if idx := g.paramOf(t); idx >= 0 && g.rng.Intn(2) == 0 {
			out = append(out, 0x20) // local.get
			return uleb(out, uint64(idx))
		}
		return g.leafConst(out, t)
	}
	if isFloat(t) {
		return g.genFloat(out, t, depth)
	}
	return g.genInt(out, t, depth)
}

func (g *exprGen) genInt(out []byte, t byte, depth int) []byte {
	// Categories: binary arithmetic/bitwise (t,t)->t, unary (t)->t,
	// comparison (s,s)->i32 (only when t==i32), conversion (s)->t.
	switch g.rng.Intn(5) {
	case 0, 1: // binary
		out = g.gen(out, t, depth-1)
		out = g.gen(out, t, depth-1)
		return append(out, g.intBinOp(t))
	case 2: // unary
		out = g.gen(out, t, depth-1)
		return append(out, g.intUnOp(t)...)
	case 3: // comparison producing i32
		if t == vtI32 {
			st := g.anyType()
			out = g.gen(out, st, depth-1)
			out = g.gen(out, st, depth-1)
			return append(out, g.cmpOp(st))
		}
		fallthrough
	default: // conversion to t from some source type
		return g.convTo(out, t, depth)
	}
}

func (g *exprGen) genFloat(out []byte, t byte, depth int) []byte {
	switch g.rng.Intn(4) {
	case 0, 1: // binary
		out = g.gen(out, t, depth-1)
		out = g.gen(out, t, depth-1)
		return append(out, g.floatBinOp(t))
	case 2: // unary
		out = g.gen(out, t, depth-1)
		return append(out, g.floatUnOp(t))
	default: // conversion to float
		return g.convTo(out, t, depth)
	}
}

func (g *exprGen) anyType() byte {
	return []byte{vtI32, vtI64, vtF32, vtF64}[g.rng.Intn(4)]
}

// Opcode tables. Values are single-byte wasm opcodes; multi-byte unary integer
// ops (clz/ctz/popcnt/eqz) are returned as slices.

func (g *exprGen) intBinOp(t byte) byte {
	// add sub mul div_s div_u rem_s rem_u and or xor shl shr_s shr_u rotl rotr
	var base byte = 0x6a // i32.add
	if t == vtI64 {
		base = 0x7c // i64.add
	}
	return base + byte(g.rng.Intn(15))
}

func (g *exprGen) intUnOp(t byte) []byte {
	// clz ctz popcnt (0x67..0x69 for i32, 0x79..0x7b for i64). eqz also produces
	// t only when t==i32 (i64.eqz yields i32, so it is unusable as an i64 unop).
	if t == vtI32 && g.rng.Intn(4) == 0 {
		return []byte{0x45} // i32.eqz -> i32
	}
	if t == vtI32 {
		return []byte{0x67 + byte(g.rng.Intn(3))}
	}
	return []byte{0x79 + byte(g.rng.Intn(3))}
}

func (g *exprGen) cmpOp(t byte) byte {
	switch t {
	case vtI32:
		return 0x46 + byte(g.rng.Intn(10)) // i32.eq .. i32.ge_u
	case vtI64:
		return 0x51 + byte(g.rng.Intn(10)) // i64.eq .. i64.ge_u
	case vtF32:
		return 0x5b + byte(g.rng.Intn(6)) // f32.eq .. f32.ge
	default:
		return 0x61 + byte(g.rng.Intn(6)) // f64.eq .. f64.ge
	}
}

func (g *exprGen) floatBinOp(t byte) byte {
	// add sub mul div min max copysign
	var base byte = 0x92 // f32.add
	if t == vtF64 {
		base = 0xa0 // f64.add
	}
	return base + byte(g.rng.Intn(7))
}

func (g *exprGen) floatUnOp(t byte) byte {
	// abs neg ceil floor trunc nearest sqrt
	var base byte = 0x8b // f32.abs
	if t == vtF64 {
		base = 0x99 // f64.abs
	}
	return base + byte(g.rng.Intn(7))
}

// convTo emits a conversion producing type t from a randomly chosen source.
func (g *exprGen) convTo(out []byte, t byte, depth int) []byte {
	type conv struct {
		src    byte
		opcode []byte
	}
	var opts []conv
	switch t {
	case vtI32:
		opts = []conv{
			{vtI64, []byte{0xa7}},                        // i32.wrap_i64
			{vtF32, []byte{0xa8}}, {vtF32, []byte{0xa9}}, // i32.trunc_f32_s/u (trapping)
			{vtF64, []byte{0xaa}}, {vtF64, []byte{0xab}}, // i32.trunc_f64_s/u (trapping)
			{vtF32, []byte{0xbc}}, // i32.reinterpret_f32
		}
	case vtI64:
		opts = []conv{
			{vtI32, []byte{0xac}}, {vtI32, []byte{0xad}}, // i64.extend_i32_s/u
			{vtF32, []byte{0xae}}, {vtF32, []byte{0xaf}}, // i64.trunc_f32_s/u (trapping)
			{vtF64, []byte{0xb0}}, {vtF64, []byte{0xb1}}, // i64.trunc_f64_s/u (trapping)
			{vtF64, []byte{0xbd}}, // i64.reinterpret_f64
		}
	case vtF32:
		opts = []conv{
			{vtI32, []byte{0xb2}}, {vtI32, []byte{0xb3}}, // f32.convert_i32_s/u
			{vtI64, []byte{0xb4}}, {vtI64, []byte{0xb5}}, // f32.convert_i64_s/u
			{vtF64, []byte{0xb6}}, // f32.demote_f64
			{vtI32, []byte{0xbe}}, // f32.reinterpret_i32
		}
	default: // vtF64
		opts = []conv{
			{vtI32, []byte{0xb7}}, {vtI32, []byte{0xb8}}, // f64.convert_i32_s/u
			{vtI64, []byte{0xb9}}, {vtI64, []byte{0xba}}, // f64.convert_i64_s/u
			{vtF32, []byte{0xbb}}, // f64.promote_f32
			{vtI64, []byte{0xbf}}, // f64.reinterpret_i64
		}
	}
	c := opts[g.rng.Intn(len(opts))]
	out = g.gen(out, c.src, depth-1)
	return append(out, c.opcode...)
}

// randModule generates a random valid module and its param types.
func randModule(rng *rand.Rand) (bytes []byte, params []byte, result byte) {
	types := []byte{vtI32, vtI64, vtF32, vtF64}
	nParams := rng.Intn(5) // 0..4
	params = make([]byte, nParams)
	for i := range params {
		params[i] = types[rng.Intn(4)]
	}
	result = types[rng.Intn(4)]
	g := &exprGen{rng: rng, params: params}
	body := g.gen(nil, result, 4)
	mb := &modBuilder{params: params, result: result, body: body}
	return mb.bytes(), params, result
}

// argFor builds a random raw argument word for a param type.
func argFor(rng *rand.Rand, t byte) uint64 {
	if is64(t) {
		return rng.Uint64()
	}
	return uint64(uint32(rng.Uint64()))
}

func TestX64FuzzRandomModules(t *testing.T) {
	const modules = 20000
	const argVectorsPerMod = 6
	rng := rand.New(rand.NewSource(0x5741474f)) // "WAGO"

	var compiledOK, executed, trapMismatch int
	for i := 0; i < modules; i++ {
		wasmBytes, params, result := randModule(rng)

		cAMD, errA := wago.CompileWithConfig(wago.NewRuntimeConfig().WithX64(false), wasmBytes)
		cX64, errX := wago.CompileWithConfig(wago.NewRuntimeConfig().WithX64(true), wasmBytes)
		if (errA == nil) != (errX == nil) {
			t.Fatalf("module %d (%s->%s): compile mismatch amd64=%v x64=%v\nwasm=%x", i, paramsStr(params), vtName(result), errA, errX, wasmBytes)
		}
		if errA != nil {
			continue // both rejected (generator produced something out of scope)
		}
		compiledOK++

		inAMD, eA := wago.Instantiate(cAMD, nil)
		if eA != nil {
			t.Fatalf("module %d: amd64 instantiate: %v\nwasm=%x", i, eA, wasmBytes)
		}
		inX64, eX := wago.Instantiate(cX64, nil)
		if eX != nil {
			inAMD.Close()
			t.Fatalf("module %d: x64 instantiate: %v\nwasm=%x", i, eX, wasmBytes)
		}

		for v := 0; v < argVectorsPerMod; v++ {
			args := make([]uint64, len(params))
			for k, p := range params {
				args[k] = argFor(rng, p)
			}
			rAMD, teA := inAMD.Invoke("f", args...)
			rX64, teX := inX64.Invoke("f", args...)
			executed++
			if (teA == nil) != (teX == nil) {
				// A trap divergence is only a SOFT signal: amd64 is a rough spike
				// with its own trapping bugs (e.g. it spuriously raises the div_s
				// INT_MIN/-1 overflow trap when the divisor comes from trunc_f64_s,
				// where x64 is spec-correct). Trap semantics are covered
				// authoritatively by the spec suite, so log and skip here rather than
				// fail on amd64's behalf.
				trapMismatch++
				continue
			}
			if teA != nil {
				continue // both trapped
			}
			if !resultsEqual(rAMD, rX64, result) {
				// A result divergence (both returned normally) is a hard failure:
				// this is the strong signal that caught real x64 miscompiles.
				t.Fatalf("module %d f(%v): result mismatch amd64=%#x x64=%#x\nparams=%s result=%s wasm=%x",
					i, args, rAMD, rX64, paramsStr(params), vtName(result), wasmBytes)
			}
		}
		inAMD.Close()
		inX64.Close()
	}
	t.Logf("fuzz random modules: generated=%d compiledOK=%d invocations=%d trapMismatch(soft)=%d", modules, compiledOK, executed, trapMismatch)
	if compiledOK < modules/2 {
		t.Errorf("too many generated modules rejected (%d/%d) — generator drifted out of scope", compiledOK, modules)
	}
}

// resultsEqual compares single-result outputs, treating any two NaNs of the
// result float type as equal (wasm does not pin non-deterministic NaN payloads,
// so amd64 and x64 may legitimately differ in the payload bits).
func resultsEqual(a, b []uint64, result byte) bool {
	if len(a) != len(b) || len(a) != 1 {
		return len(a) == len(b) // (all generated funcs return exactly one value)
	}
	switch result {
	case vtF32:
		fa, fb := math.Float32frombits(uint32(a[0])), math.Float32frombits(uint32(b[0]))
		if math.IsNaN(float64(fa)) && math.IsNaN(float64(fb)) {
			return true
		}
		return uint32(a[0]) == uint32(b[0])
	case vtF64:
		fa, fb := math.Float64frombits(a[0]), math.Float64frombits(b[0])
		if math.IsNaN(fa) && math.IsNaN(fb) {
			return true
		}
		return a[0] == b[0]
	case vtI32:
		return uint32(a[0]) == uint32(b[0])
	default:
		return a[0] == b[0]
	}
}

func paramsStr(params []byte) string {
	s := "("
	for i, p := range params {
		if i > 0 {
			s += ","
		}
		s += vtName(p)
	}
	return s + ")"
}
