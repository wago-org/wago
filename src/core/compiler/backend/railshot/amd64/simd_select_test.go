//go:build amd64

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

// Compare compact selectors against the pre-existing named instruction encoders.
// These byte checks protect opcode, prefix, operand order and requirement masks
// independently of the execution oracle tests.
func TestSIMDBinaryDescriptorEncodings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		op       simdBinaryOp
		emit     func(*x86.Asm, Reg, Reg, Reg)
		required shared.AMD64Features
	}{
		{"VPacksswb", opVPacksswb, (*x86.Asm).VPacksswb, shared.AMD64AVX},
		{"VPaddb", opVPaddb, (*x86.Asm).VPaddb, shared.AMD64AVX},
		{"VPaddd", opVPaddd, (*x86.Asm).VPaddd, shared.AMD64AVX},
		{"VPaddq", opVPaddq, (*x86.Asm).VPaddq, shared.AMD64AVX},
		{"VPaddsb", opVPaddsb, (*x86.Asm).VPaddsb, shared.AMD64AVX},
		{"VPaddsw", opVPaddsw, (*x86.Asm).VPaddsw, shared.AMD64AVX},
		{"VPaddusb", opVPaddusb, (*x86.Asm).VPaddusb, shared.AMD64AVX},
		{"VPaddusw", opVPaddusw, (*x86.Asm).VPaddusw, shared.AMD64AVX},
		{"VPaddw", opVPaddw, (*x86.Asm).VPaddw, shared.AMD64AVX},
		{"VPand", opVPand, (*x86.Asm).VPand, shared.AMD64AVX},
		{"VPandn", opVPandn, (*x86.Asm).VPandn, shared.AMD64AVX},
		{"VPavgb", opVPavgb, (*x86.Asm).VPavgb, shared.AMD64AVX},
		{"VPavgw", opVPavgw, (*x86.Asm).VPavgw, shared.AMD64AVX},
		{"VPcmpeqb", opVPcmpeqb, (*x86.Asm).VPcmpeqb, shared.AMD64AVX},
		{"VPcmpeqd", opVPcmpeqd, (*x86.Asm).VPcmpeqd, shared.AMD64AVX},
		{"VPcmpeqq", opVPcmpeqq, (*x86.Asm).VPcmpeqq, shared.AMD64AVX | shared.AMD64SSE41},
		{"VPcmpeqw", opVPcmpeqw, (*x86.Asm).VPcmpeqw, shared.AMD64AVX},
		{"VPcmpgtb", opVPcmpgtb, (*x86.Asm).VPcmpgtb, shared.AMD64AVX},
		{"VPcmpgtd", opVPcmpgtd, (*x86.Asm).VPcmpgtd, shared.AMD64AVX},
		{"VPcmpgtq", opVPcmpgtq, (*x86.Asm).VPcmpgtq, shared.AMD64AVX | shared.AMD64SSE42},
		{"VPcmpgtw", opVPcmpgtw, (*x86.Asm).VPcmpgtw, shared.AMD64AVX},
		{"VPhaddd", opVPhaddd, (*x86.Asm).VPhaddd, shared.AMD64AVX | shared.AMD64SSSE3},
		{"VPmaddubsw", opVPmaddubsw, (*x86.Asm).VPmaddubsw, shared.AMD64AVX | shared.AMD64SSSE3},
		{"VPmaddwd", opVPmaddwd, (*x86.Asm).VPmaddwd, shared.AMD64AVX},
		{"VPmaxsb", opVPmaxsb, (*x86.Asm).VPmaxsb, shared.AMD64AVX | shared.AMD64SSE41},
		{"VPmaxsd", opVPmaxsd, (*x86.Asm).VPmaxsd, shared.AMD64AVX | shared.AMD64SSE41},
		{"VPmaxsw", opVPmaxsw, (*x86.Asm).VPmaxsw, shared.AMD64AVX},
		{"VPmaxub", opVPmaxub, (*x86.Asm).VPmaxub, shared.AMD64AVX},
		{"VPmaxud", opVPmaxud, (*x86.Asm).VPmaxud, shared.AMD64AVX | shared.AMD64SSE41},
		{"VPmaxuw", opVPmaxuw, (*x86.Asm).VPmaxuw, shared.AMD64AVX | shared.AMD64SSE41},
		{"VPminsb", opVPminsb, (*x86.Asm).VPminsb, shared.AMD64AVX | shared.AMD64SSE41},
		{"VPminsd", opVPminsd, (*x86.Asm).VPminsd, shared.AMD64AVX | shared.AMD64SSE41},
		{"VPminsw", opVPminsw, (*x86.Asm).VPminsw, shared.AMD64AVX},
		{"VPminub", opVPminub, (*x86.Asm).VPminub, shared.AMD64AVX},
		{"VPminud", opVPminud, (*x86.Asm).VPminud, shared.AMD64AVX | shared.AMD64SSE41},
		{"VPminuw", opVPminuw, (*x86.Asm).VPminuw, shared.AMD64AVX | shared.AMD64SSE41},
		{"VPmuldq", opVPmuldq, (*x86.Asm).VPmuldq, shared.AMD64AVX | shared.AMD64SSE41},
		{"VPmulhrsw", opVPmulhrsw, (*x86.Asm).VPmulhrsw, shared.AMD64AVX | shared.AMD64SSSE3},
		{"VPmulld", opVPmulld, (*x86.Asm).VPmulld, shared.AMD64AVX | shared.AMD64SSE41},
		{"VPmullw", opVPmullw, (*x86.Asm).VPmullw, shared.AMD64AVX},
		{"VPmuludq", opVPmuludq, (*x86.Asm).VPmuludq, shared.AMD64AVX},
		{"VPor", opVPor, (*x86.Asm).VPor, shared.AMD64AVX},
		{"VPpackssdw", opVPpackssdw, (*x86.Asm).VPpackssdw, shared.AMD64AVX},
		{"VPpacksswb", opVPpacksswb, (*x86.Asm).VPpacksswb, shared.AMD64AVX},
		{"VPpackusdw", opVPpackusdw, (*x86.Asm).VPpackusdw, shared.AMD64AVX | shared.AMD64SSE41},
		{"VPpackuswb", opVPpackuswb, (*x86.Asm).VPpackuswb, shared.AMD64AVX},
		{"VPshufb", opVPshufb, (*x86.Asm).VPshufb, shared.AMD64AVX | shared.AMD64SSSE3},
		{"VPslld", opVPslld, (*x86.Asm).VPslld, shared.AMD64AVX},
		{"VPsllq", opVPsllq, (*x86.Asm).VPsllq, shared.AMD64AVX},
		{"VPsllw", opVPsllw, (*x86.Asm).VPsllw, shared.AMD64AVX},
		{"VPsrad", opVPsrad, (*x86.Asm).VPsrad, shared.AMD64AVX},
		{"VPsraw", opVPsraw, (*x86.Asm).VPsraw, shared.AMD64AVX},
		{"VPsrld", opVPsrld, (*x86.Asm).VPsrld, shared.AMD64AVX},
		{"VPsrlq", opVPsrlq, (*x86.Asm).VPsrlq, shared.AMD64AVX},
		{"VPsrlw", opVPsrlw, (*x86.Asm).VPsrlw, shared.AMD64AVX},
		{"VPsubb", opVPsubb, (*x86.Asm).VPsubb, shared.AMD64AVX},
		{"VPsubd", opVPsubd, (*x86.Asm).VPsubd, shared.AMD64AVX},
		{"VPsubq", opVPsubq, (*x86.Asm).VPsubq, shared.AMD64AVX},
		{"VPsubsb", opVPsubsb, (*x86.Asm).VPsubsb, shared.AMD64AVX},
		{"VPsubsw", opVPsubsw, (*x86.Asm).VPsubsw, shared.AMD64AVX},
		{"VPsubusb", opVPsubusb, (*x86.Asm).VPsubusb, shared.AMD64AVX},
		{"VPsubusw", opVPsubusw, (*x86.Asm).VPsubusw, shared.AMD64AVX},
		{"VPsubw", opVPsubw, (*x86.Asm).VPsubw, shared.AMD64AVX},
		{"VPunpckhbw", opVPunpckhbw, (*x86.Asm).VPunpckhbw, shared.AMD64AVX},
		{"VPunpckhdq", opVPunpckhdq, (*x86.Asm).VPunpckhdq, shared.AMD64AVX},
		{"VPunpckhwd", opVPunpckhwd, (*x86.Asm).VPunpckhwd, shared.AMD64AVX},
		{"VPunpcklbw", opVPunpcklbw, (*x86.Asm).VPunpcklbw, shared.AMD64AVX},
		{"VPunpckldq", opVPunpckldq, (*x86.Asm).VPunpckldq, shared.AMD64AVX},
		{"VPunpcklwd", opVPunpcklwd, (*x86.Asm).VPunpcklwd, shared.AMD64AVX},
		{"VPxor", opVPxor, (*x86.Asm).VPxor, shared.AMD64AVX},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for dst := Reg(0); dst < 16; dst++ {
				for _, pair := range [][2]Reg{{1, 2}, {9, 10}, {dst, 1}, {1, dst}, {dst, dst}} {
					var got, want x86.Asm
					sc := scratch{amd64Features: shared.AMD64KnownFeatures}
					f := fn{a: &got, sc: &sc}
					tc.op.emit(&f, dst, pair[0], pair[1])
					tc.emit(&want, dst, pair[0], pair[1])
					if !bytes.Equal(got.B, want.B) || sc.usedAMD64Features != tc.required {
						t.Fatalf("regs=%v,%v bytes=%x want=%x features=%x want=%x", dst, pair, got.B, want.B, sc.usedAMD64Features, tc.required)
					}
				}
			}
		})
	}
}

func TestSIMDUnaryDescriptorEncodings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		op       simdUnaryOp
		emit     func(*x86.Asm, Reg, Reg)
		required shared.AMD64Features
	}{
		{"VMovmskpd", opVMovmskpd, (*x86.Asm).VMovmskpd, shared.AMD64AVX},
		{"VMovmskps", opVMovmskps, (*x86.Asm).VMovmskps, shared.AMD64AVX},
		{"VPabsb", opVPabsb, (*x86.Asm).VPabsb, shared.AMD64AVX | shared.AMD64SSSE3},
		{"VPabsd", opVPabsd, (*x86.Asm).VPabsd, shared.AMD64AVX | shared.AMD64SSSE3},
		{"VPabsw", opVPabsw, (*x86.Asm).VPabsw, shared.AMD64AVX | shared.AMD64SSSE3},
		{"VPmovmskb", opVPmovmskb, (*x86.Asm).VPmovmskb, shared.AMD64AVX},
		{"Vcvtdq2pd", opVcvtdq2pd, (*x86.Asm).Vcvtdq2pd, shared.AMD64AVX},
		{"Vcvtdq2ps", opVcvtdq2ps, (*x86.Asm).Vcvtdq2ps, shared.AMD64AVX},
		{"Vcvtpd2ps", opVcvtpd2ps, (*x86.Asm).Vcvtpd2ps, shared.AMD64AVX},
		{"Vcvtps2pd", opVcvtps2pd, (*x86.Asm).Vcvtps2pd, shared.AMD64AVX},
		{"Vcvttpd2dq", opVcvttpd2dq, (*x86.Asm).Vcvttpd2dq, shared.AMD64AVX},
		{"Vcvttps2dq", opVcvttps2dq, (*x86.Asm).Vcvttps2dq, shared.AMD64AVX},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for dst := Reg(0); dst < 16; dst++ {
				for src := Reg(0); src < 16; src++ {
					var got, want x86.Asm
					sc := scratch{amd64Features: shared.AMD64KnownFeatures}
					f := fn{a: &got, sc: &sc}
					tc.op.emit(&f, dst, src)
					tc.emit(&want, dst, src)
					if !bytes.Equal(got.B, want.B) || sc.usedAMD64Features != tc.required {
						t.Fatalf("regs=%v,%v bytes=%x want=%x features=%x want=%x", dst, src, got.B, want.B, sc.usedAMD64Features, tc.required)
					}
				}
			}
		})
	}
}
