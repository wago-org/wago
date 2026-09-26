//go:build linux && amd64 && !tinygo

package amd64

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSSE2ScalarCompileAndExecute(t *testing.T) {
	profiles := []shared.AMD64Features{0, shared.AMD64SSSE3, shared.AMD64SSSE3 | shared.AMD64SSE41, shared.AMD64SSSE3 | shared.AMD64SSE41 | shared.AMD64SSE42, shared.AMD64ModernBaseline, shared.AMD64ModernBaseline | shared.AMD64BMI1 | shared.AMD64POPCNT}
	for _, profile := range profiles {
		for _, f64 := range []bool{false, true} {
			typ, base := wasm.F32, byte(0x8b)
			if f64 {
				typ, base = wasm.F64, 0x99
			}
			for _, mode := range []byte{roundCeil, roundFloor, roundTrunc, roundNearest} {
				op := base + byte(2) + [...]byte{3, 1, 0, 2}[mode&3]
				t.Run(fmt.Sprintf("features=%x/f64=%v/round=%d", profile, f64, mode&3), func(t *testing.T) {
					m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, op, 0x0b})
					cm, err := CompileModuleWith(m, CompileOptions{AMD64Features: profile, AMD64FeaturesSet: true})
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					code := cm.Code[cm.Entry[0]:]
					run := roundRunner(t, code)
					// Complete exponent and boundary coverage through the real compiler,
					// including its entry pins, result moves and frame handling.
					for _, bits := range roundInputs(f64) {
						want := roundReference(bits, f64, mode)
						got := run(bits)
						if !f64 {
							got = uint64(uint32(got))
						}
						if got != want {
							t.Fatalf("input=%x got=%x want=%x", bits, got, want)
						}
					}
				})
			}
			for i, op := range []byte{base + 7, base + 8, base + 9, base + 10} {
				t.Run(fmt.Sprintf("features=%x/f64=%v/binary=%x", profile, f64, op), func(t *testing.T) {
					m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, op, 0x0b})
					cm, err := CompileModuleWith(m, CompileOptions{AMD64Features: profile, AMD64FeaturesSet: true})
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					x, y := 3.75, -1.5
					want := []float64{x + y, x - y, x * y, x / y}[i]
					a, b, w := math.Float64bits(x), math.Float64bits(y), math.Float64bits(want)
					if !f64 {
						a, b, w = uint64(math.Float32bits(float32(x))), uint64(math.Float32bits(float32(y))), uint64(math.Float32bits(float32(want)))
					}
					got := runCompiledAmd64u(t, cm, a, b)
					if !f64 {
						got = uint64(uint32(got))
					}
					if got != w {
						t.Fatalf("got=%x want=%x", got, w)
					}
				})
			}
		}
	}
}

func TestSSE2VectorMetadataCompiles(t *testing.T) {
	modules := []*wasm.Module{
		mod1(t, nil, []wasm.ValType{wasm.V128}, append(append([]byte{0}, v128ConstBytes([16]byte{})...), 0x0b)),
		mod1(t, []wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}, []byte{0, 0x20, 0, 0x0b}),
		mod1(t, nil, []wasm.ValType{wasm.I32}, []byte{1, 1, 0x7b, 0x20, 0, 0x1a, 0x41, 0, 0x0b}),
	}
	for i, m := range modules {
		cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true})
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
	}
}

func TestSSE2ScalarRegisterAliases(t *testing.T) {
	// local.set fusion writes each possible operand register. Then preserve the
	// other local in a second calculation, exposing accidental borrowed clobbers.
	for _, target := range []byte{0, 1} {
		for _, op := range []byte{0xa0, 0xa1, 0xa2, 0xa3, 0xa6} { // f64 add/sub/mul/div/copysign
			body := []byte{0, 0x20, 0, 0x20, 1, op, 0x21, target, 0x20, 0, 0x20, 1, 0xa0, 0x0b}
			m := mod1(t, []wasm.ValType{wasm.F64, wasm.F64}, []wasm.ValType{wasm.F64}, body)
			for _, profile := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
				cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: profile})
				if err != nil {
					t.Fatal(err)
				}
				x, y := 7.75, -2.5
				v := map[byte]float64{0xa0: x + y, 0xa1: x - y, 0xa2: x * y, 0xa3: x / y, 0xa6: math.Copysign(x, y)}[op]
				want := x + v
				if target == 0 {
					want = v + y
				}
				got := runCompiledAmd64u(t, cm, math.Float64bits(x), math.Float64bits(y))
				if cm.CodeImage != nil {
					cm.CodeImage.Close()
				}
				if got != math.Float64bits(want) {
					t.Fatalf("features=%x target=%d op=%x got=%x want=%x", profile, target, op, got, math.Float64bits(want))
				}
			}
		}
	}
}

func TestSSE2BulkMemory(t *testing.T) {
	opts := CompileOptions{AMD64FeaturesSet: true}
	for _, fill := range []bool{false, true} {
		sub := byte(10)
		immediate := []byte{0, 0}
		if fill {
			sub = 11
			immediate = []byte{0}
		}
		body := append([]byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0xfc, sub}, immediate...)
		body = append(body, 0x41, 0, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, n := range []int{0, 1, 15, 16, 31, 64, 127, 128, 255, 1024, 2048} {
			for _, dst := range []int{0, 1, 31} {
				_, got, err := runMemAmd64WithOptions(t, m, opts, func(mem []byte) {
					for i := range mem {
						mem[i] = byte(i * 73)
					}
				}, uint64(dst), 0, uint64(n))
				if err != nil {
					t.Fatal(err)
				}
				want := make([]byte, len(got))
				for i := range want {
					want[i] = byte(i * 73)
				}
				if fill {
					clear(want[dst : dst+n])
				} else {
					copy(want[dst:dst+n], want[:n])
				}
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("fill=%v n=%d dst=%d byte=%d got=%x want=%x", fill, n, dst, i, got[i], want[i])
					}
				}
			}
		}
	}
}

func TestSSE2ScalarInstructionBaseline(t *testing.T) {
	for _, f64 := range []bool{false, true} {
		typ, start := wasm.F32, byte(0x8d)
		if f64 {
			typ, start = wasm.F64, 0x9b
		}
		// Rounding, sqrt, arithmetic and bit counts have no literal pool here, so
		// every byte in the native object is an instruction or alignment padding.
		for op := start; op < start+9; op++ {
			arity := 1
			if op >= start+5 {
				arity = 2
			}
			params := []wasm.ValType{typ}
			body := []byte{0, 0x20, 0}
			if arity == 2 {
				params = append(params, typ)
				body = append(body, 0x20, 1)
			}
			body = append(body, op, 0x0b)
			cm, err := CompileModuleWith(mod1(t, params, []wasm.ValType{typ}, body), CompileOptions{AMD64FeaturesSet: true})
			if err != nil {
				t.Fatal(err)
			}
			assertScalarBaseline(t, cm.Code)
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
		}
	}
}

func assertScalarBaseline(t *testing.T, code []byte) {
	t.Helper()
	objdump, err := exec.LookPath("objdump")
	if err != nil {
		t.Skip("GNU objdump is required for instruction validation")
	}
	path := filepath.Join(t.TempDir(), "scalar.bin")
	if err := os.WriteFile(path, code, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(objdump, "-D", "-b", "binary", "-m", "i386:x86-64", "-M", "intel", "--no-show-raw-insn", path).CombinedOutput()
	if err != nil {
		t.Fatalf("objdump: %v: %s", err, out)
	}
	allowed := map[string]bool{}
	for _, op := range strings.Fields("cvtsi2ss cvtsi2sd cvttss2si cvttsd2si cvtsd2ss cvtss2sd ucomiss ucomisd minss minsd maxss maxsd mov movabs movd movq movss movsd movaps movups movdqu movzx movsxd push pop shl shr sar sub add and or xor cmp test jmp ret nop int3 ud2 lea call pxor xorpd addss addsd subss subsd mulss mulsd divss divsd sqrtss sqrtsd movapd movupd movlhps movhlps movlps movhps movlpd movhpd andps andpd andnps andnpd orps orpd xorps xorpd addps addpd subps subpd mulps mulpd divps divpd minps minpd maxps maxpd sqrtps sqrtpd cmpeqps cmpeqpd cmpltps cmpltpd cmpleps cmplepd cmpunordps cmpunordpd cmpneqps cmpneqpd cvtdq2ps cvtdq2pd cvtps2pd cvtpd2ps cvttps2dq cvttpd2dq pshufd pshuflw pshufhw shufps shufpd movmskps movmskpd pmovmskb pinsrw pextrw pand pandn por paddb paddw paddd paddq psubb psubw psubd psubq paddsb paddsw paddusb paddusw psubsb psubsw psubusb psubusw pavgb pavgw pminub pminsw pmaxub pmaxsw pmullw pmuludq pmaddwd pcmpeqb pcmpeqw pcmpeqd pcmpgtb pcmpgtw pcmpgtd psllw pslld psllq psrlw psrld psrlq psraw psrad punpcklbw punpcklwd punpckldq punpcklqdq punpckhbw punpckhwd punpckhdq punpckhqdq packsswb packssdw packuswb imul neg not inc dec xchg bsr bsf bswap movsx cdqe cdq cqo sete setne setl setle setg setge setb setbe seta setae sets setns setp setnp cmove cmovne cmovl cmovle cmovg cmovge cmovb cmovbe cmova cmovae cmovs cmovns cmovp cmovnp") {
		allowed[op] = true
	}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 || !strings.HasPrefix(line, " ") {
			continue
		}
		fields := strings.Fields(parts[1])
		for len(fields) > 0 && (fields[0] == "data16" || fields[0] == "cs" || fields[0] == "rex.W") {
			fields = fields[1:]
		}
		if len(fields) == 0 {
			continue
		}
		op := fields[0]
		if !allowed[op] && !strings.HasPrefix(op, "j") {
			t.Fatalf("non-baseline or unknown instruction: %s\n%s", line, out)
		}
	}
}

func BenchmarkSSE2ScalarCompile(b *testing.B) {
	for _, f64 := range []bool{false, true} {
		for _, rounding := range []bool{false, true} {
			typ, op := wasm.F32, byte(0x92)
			if f64 {
				typ, op = wasm.F64, 0xa0
			}
			params := []wasm.ValType{typ, typ}
			body := []byte{0, 0x20, 0, 0x20, 1, op, 0x0b}
			if rounding {
				params = params[:1]
				op = 0x90
				if f64 {
					op = 0x9e
				}
				body = []byte{0, 0x20, 0, op, 0x0b}
			}
			m := mod1(b, params, []wasm.ValType{typ}, body)
			for _, modern := range []bool{false, true} {
				b.Run(fmt.Sprintf("f64=%v/round=%v/modern=%v", f64, rounding, modern), func(b *testing.B) {
					opts := CompileOptions{AMD64FeaturesSet: true, DeferCodeMapping: true}
					if modern {
						opts.AMD64Features = shared.AMD64ModernBaseline
					}
					b.ReportAllocs()
					size := 0
					for i := 0; i < b.N; i++ {
						cm, err := CompileModuleWith(m, opts)
						if err != nil {
							b.Fatal(err)
						}
						size = len(cm.Code)
						if cm.CodeImage != nil {
							cm.CodeImage.Close()
						}
					}
					b.ReportMetric(float64(size), "code-bytes")
				})
			}
		}
	}
}

func assertSIMDBaseline(t *testing.T, code []byte) { t.Helper(); assertScalarBaseline(t, code) }

func TestSSE2ScalarExtendedInstructionBaseline(t *testing.T) {
	for _, tc := range []struct {
		input, output wasm.ValType
		op            byte
		arity         int
	}{
		{wasm.I32, wasm.I32, 0x67, 1}, {wasm.I32, wasm.I32, 0x68, 1}, {wasm.I32, wasm.I32, 0x69, 1},
		{wasm.I64, wasm.I64, 0x79, 1}, {wasm.I64, wasm.I64, 0x7a, 1}, {wasm.I64, wasm.I64, 0x7b, 1},
		{wasm.F32, wasm.F32, 0x8b, 1}, {wasm.F32, wasm.F32, 0x8c, 1}, {wasm.F32, wasm.F32, 0x96, 2}, {wasm.F32, wasm.F32, 0x97, 2}, {wasm.F32, wasm.F32, 0x98, 2},
		{wasm.F64, wasm.F64, 0x99, 1}, {wasm.F64, wasm.F64, 0x9a, 1}, {wasm.F64, wasm.F64, 0xa4, 2}, {wasm.F64, wasm.F64, 0xa5, 2}, {wasm.F64, wasm.F64, 0xa6, 2},
		{wasm.F32, wasm.I32, 0x5b, 2}, {wasm.F64, wasm.I32, 0x61, 2},
		{wasm.I32, wasm.F32, 0xb2, 1}, {wasm.I32, wasm.F32, 0xb3, 1}, {wasm.I64, wasm.F64, 0xb9, 1}, {wasm.I64, wasm.F64, 0xba, 1},
		{wasm.F32, wasm.I32, 0xa8, 1}, {wasm.F64, wasm.I64, 0xb0, 1}, {wasm.F32, wasm.F64, 0xbb, 1}, {wasm.F64, wasm.F32, 0xb6, 1},
	} {
		t.Run(fmt.Sprintf("op=%x", tc.op), func(t *testing.T) {
			params := []wasm.ValType{tc.input}
			body := []byte{0, 0x20, 0}
			if tc.arity == 2 {
				params = append(params, tc.input)
				body = append(body, 0x20, 1)
			}
			body = append(body, tc.op, 0x0b)
			var stats ModuleStats
			cm, err := CompileModuleWith(mod1(t, params, []wasm.ValType{tc.output}, body), CompileOptions{AMD64FeaturesSet: true, BitCountFeatures: 7, Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if cm.RequiredAMD64Features != 0 {
				t.Fatalf("baseline requires %x", cm.RequiredAMD64Features)
			}
			fs := stats.Funcs[0]
			assertScalarBaseline(t, cm.Code[cm.Entry[0]:cm.Entry[0]+fs.CodeBytes-fs.NativeSize.LiteralPoolBytes])
		})
	}
}
