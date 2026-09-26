//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

func (f *fn) emitVMovmskpd(dst, src Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VMovmskpd(dst, src)
		return
	}
	f.a.SseMapRR(0x66, 0, 0x50, dst, src)
}

func (f *fn) emitVMovmskps(dst, src Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VMovmskps(dst, src)
		return
	}
	f.a.SseMapRR(0, 0, 0x50, dst, src)
}

func (f *fn) emitVPabsb(dst, src Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSSE3) {
		f.a.VPabsb(dst, src)
		return
	}
	if !f.cpuHas(shared.AMD64SSSE3) {
		f.simdFallback(0x1C, dst, src, src)
		return
	}
	f.a.SseMapRR(0x66, 0x38, 0x1C, dst, src)
}

func (f *fn) emitVPabsd(dst, src Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSSE3) {
		f.a.VPabsd(dst, src)
		return
	}
	if !f.cpuHas(shared.AMD64SSSE3) {
		f.simdFallback(0x1E, dst, src, src)
		return
	}
	f.a.SseMapRR(0x66, 0x38, 0x1E, dst, src)
}

func (f *fn) emitVPabsw(dst, src Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSSE3) {
		f.a.VPabsw(dst, src)
		return
	}
	if !f.cpuHas(shared.AMD64SSSE3) {
		f.simdFallback(0x1D, dst, src, src)
		return
	}
	f.a.SseMapRR(0x66, 0x38, 0x1D, dst, src)
}

func (f *fn) emitVPacksswb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPacksswb(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x63, dst, s1, s2)
}

func (f *fn) emitVPaddb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPaddb(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xFC, dst, s1, s2)
}

func (f *fn) emitVPaddd(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPaddd(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xFE, dst, s1, s2)
}

func (f *fn) emitVPaddq(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPaddq(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xD4, dst, s1, s2)
}

func (f *fn) emitVPaddsb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPaddsb(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xEC, dst, s1, s2)
}

func (f *fn) emitVPaddsw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPaddsw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xED, dst, s1, s2)
}

func (f *fn) emitVPaddusb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPaddusb(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xDC, dst, s1, s2)
}

func (f *fn) emitVPaddusw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPaddusw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xDD, dst, s1, s2)
}

func (f *fn) emitVPaddw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPaddw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xFD, dst, s1, s2)
}

func (f *fn) emitVPand(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPand(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xDB, dst, s1, s2)
}

func (f *fn) emitVPandn(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPandn(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xDF, dst, s1, s2)
}

func (f *fn) emitVPavgb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPavgb(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xE0, dst, s1, s2)
}

func (f *fn) emitVPavgw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPavgw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xE3, dst, s1, s2)
}

func (f *fn) emitVPcmpeqb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPcmpeqb(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x74, dst, s1, s2)
}

func (f *fn) emitVPcmpeqd(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPcmpeqd(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x76, dst, s1, s2)
}

func (f *fn) emitVPcmpeqq(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VPcmpeqq(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE41) {
		f.simdFallback(0x29, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x29, dst, s1, s2)
}

func (f *fn) emitVPcmpeqw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPcmpeqw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x75, dst, s1, s2)
}

func (f *fn) emitVPcmpgtb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPcmpgtb(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x64, dst, s1, s2)
}

func (f *fn) emitVPcmpgtd(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPcmpgtd(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x66, dst, s1, s2)
}

func (f *fn) emitVPcmpgtq(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE42) {
		f.a.VPcmpgtq(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE42) {
		f.simdFallback(0x37, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x37, dst, s1, s2)
}

func (f *fn) emitVPcmpgtw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPcmpgtw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x65, dst, s1, s2)
}

func (f *fn) emitVPhaddd(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSSE3) {
		f.a.VPhaddd(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSSE3) {
		f.simdFallback(0x02, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x02, dst, s1, s2)
}

func (f *fn) emitVPmaddubsw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSSE3) {
		f.a.VPmaddubsw(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSSE3) {
		f.simdFallback(0x04, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x04, dst, s1, s2)
}

func (f *fn) emitVPmaddwd(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPmaddwd(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xF5, dst, s1, s2)
}

func (f *fn) emitVPmaxsb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VPmaxsb(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE41) {
		f.simdFallback(0x3C, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x3C, dst, s1, s2)
}

func (f *fn) emitVPmaxsd(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VPmaxsd(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE41) {
		f.simdFallback(0x3D, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x3D, dst, s1, s2)
}

func (f *fn) emitVPmaxsw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPmaxsw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xEE, dst, s1, s2)
}

func (f *fn) emitVPmaxub(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPmaxub(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xDE, dst, s1, s2)
}

func (f *fn) emitVPmaxud(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VPmaxud(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE41) {
		f.simdFallback(0x3F, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x3F, dst, s1, s2)
}

func (f *fn) emitVPmaxuw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VPmaxuw(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE41) {
		f.simdFallback(0x3E, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x3E, dst, s1, s2)
}

func (f *fn) emitVPminsb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VPminsb(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE41) {
		f.simdFallback(0x38, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x38, dst, s1, s2)
}

func (f *fn) emitVPminsd(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VPminsd(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE41) {
		f.simdFallback(0x39, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x39, dst, s1, s2)
}

func (f *fn) emitVPminsw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPminsw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xEA, dst, s1, s2)
}

func (f *fn) emitVPminub(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPminub(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xDA, dst, s1, s2)
}

func (f *fn) emitVPminud(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VPminud(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE41) {
		f.simdFallback(0x3B, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x3B, dst, s1, s2)
}

func (f *fn) emitVPminuw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VPminuw(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE41) {
		f.simdFallback(0x3A, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x3A, dst, s1, s2)
}

func (f *fn) emitVPmovmskb(dst, src Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPmovmskb(dst, src)
		return
	}
	f.a.SseMapRR(0x66, 0, 0xD7, dst, src)
}

func (f *fn) emitVPmuldq(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VPmuldq(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE41) {
		f.simdFallback(0x28, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x28, dst, s1, s2)
}

func (f *fn) emitVPmulhrsw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSSE3) {
		f.a.VPmulhrsw(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSSE3) {
		f.simdFallback(0x0B, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x0B, dst, s1, s2)
}

func (f *fn) emitVPmulld(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VPmulld(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE41) {
		f.simdFallback(0x40, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x40, dst, s1, s2)
}

func (f *fn) emitVPmullw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPmullw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xD5, dst, s1, s2)
}

func (f *fn) emitVPmuludq(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPmuludq(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xF4, dst, s1, s2)
}

func (f *fn) emitVPor(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPor(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xEB, dst, s1, s2)
}

func (f *fn) emitVPpackssdw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPpackssdw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x6B, dst, s1, s2)
}

func (f *fn) emitVPpacksswb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPpacksswb(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x63, dst, s1, s2)
}

func (f *fn) emitVPpackusdw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VPpackusdw(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE41) {
		f.simdFallback(0x2B, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x2B, dst, s1, s2)
}

func (f *fn) emitVPpackuswb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPpackuswb(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x67, dst, s1, s2)
}

func (f *fn) emitVPshufb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSSE3) {
		f.a.VPshufb(dst, s1, s2)
		return
	}
	if !f.cpuHas(shared.AMD64SSSE3) {
		f.simdFallback(0x00, dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0x38, 0x00, dst, s1, s2)
}

func (f *fn) emitVPslld(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPslld(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xF2, dst, s1, s2)
}

func (f *fn) emitVPsllq(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsllq(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xF3, dst, s1, s2)
}

func (f *fn) emitVPsllw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsllw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xF1, dst, s1, s2)
}

func (f *fn) emitVPsrad(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsrad(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xE2, dst, s1, s2)
}

func (f *fn) emitVPsraw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsraw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xE1, dst, s1, s2)
}

func (f *fn) emitVPsrld(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsrld(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xD2, dst, s1, s2)
}

func (f *fn) emitVPsrlq(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsrlq(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xD3, dst, s1, s2)
}

func (f *fn) emitVPsrlw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsrlw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xD1, dst, s1, s2)
}

func (f *fn) emitVPsubb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsubb(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xF8, dst, s1, s2)
}

func (f *fn) emitVPsubd(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsubd(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xFA, dst, s1, s2)
}

func (f *fn) emitVPsubq(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsubq(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xFB, dst, s1, s2)
}

func (f *fn) emitVPsubsb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsubsb(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xE8, dst, s1, s2)
}

func (f *fn) emitVPsubsw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsubsw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xE9, dst, s1, s2)
}

func (f *fn) emitVPsubusb(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsubusb(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xD8, dst, s1, s2)
}

func (f *fn) emitVPsubusw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsubusw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xD9, dst, s1, s2)
}

func (f *fn) emitVPsubw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsubw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xF9, dst, s1, s2)
}

func (f *fn) emitVPtest(a1, a2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VPtest(a1, a2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE41) {
		f.simdTestZero(a1, a2)
		return
	}
	f.a.SseMapRR(0x66, 0x38, 0x17, a1, a2)
}

func (f *fn) emitVPunpckhbw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPunpckhbw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x68, dst, s1, s2)
}

func (f *fn) emitVPunpckhdq(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPunpckhdq(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x6A, dst, s1, s2)
}

func (f *fn) emitVPunpckhwd(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPunpckhwd(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x69, dst, s1, s2)
}

func (f *fn) emitVPunpcklbw(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPunpcklbw(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x60, dst, s1, s2)
}

func (f *fn) emitVPunpckldq(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPunpckldq(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x62, dst, s1, s2)
}

func (f *fn) emitVPunpcklwd(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPunpcklwd(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0x61, dst, s1, s2)
}

func (f *fn) emitVPxor(dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPxor(dst, s1, s2)
		return
	}
	f.legacySIMDBinary(0x66, 0, 0xEF, dst, s1, s2)
}

func (f *fn) emitVcvtdq2pd(dst, src Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.Vcvtdq2pd(dst, src)
		return
	}
	f.a.SseMapRR(0xf3, 0, 0xE6, dst, src)
}

func (f *fn) emitVcvtdq2ps(dst, src Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.Vcvtdq2ps(dst, src)
		return
	}
	f.a.SseMapRR(0, 0, 0x5B, dst, src)
}

func (f *fn) emitVcvtpd2ps(dst, src Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.Vcvtpd2ps(dst, src)
		return
	}
	f.a.SseMapRR(0x66, 0, 0x5A, dst, src)
}

func (f *fn) emitVcvtps2pd(dst, src Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.Vcvtps2pd(dst, src)
		return
	}
	f.a.SseMapRR(0, 0, 0x5A, dst, src)
}

func (f *fn) emitVcvttpd2dq(dst, src Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.Vcvttpd2dq(dst, src)
		return
	}
	f.a.SseMapRR(0x66, 0, 0xE6, dst, src)
}

func (f *fn) emitVcvttps2dq(dst, src Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.Vcvttps2dq(dst, src)
		return
	}
	f.a.SseMapRR(0xf3, 0, 0x5B, dst, src)
}

func (f *fn) emitVFPackedAdd(dst, s1, s2 Reg, f64 bool) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VFPackedAdd(dst, s1, s2, f64)
		return
	}
	f.legacySIMDBinary(packedPrefix(f64), 0, 88, dst, s1, s2)
}

func (f *fn) emitVFPackedSub(dst, s1, s2 Reg, f64 bool) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VFPackedSub(dst, s1, s2, f64)
		return
	}
	f.legacySIMDBinary(packedPrefix(f64), 0, 92, dst, s1, s2)
}

func (f *fn) emitVFPackedMul(dst, s1, s2 Reg, f64 bool) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VFPackedMul(dst, s1, s2, f64)
		return
	}
	f.legacySIMDBinary(packedPrefix(f64), 0, 89, dst, s1, s2)
}

func (f *fn) emitVFPackedDiv(dst, s1, s2 Reg, f64 bool) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VFPackedDiv(dst, s1, s2, f64)
		return
	}
	f.legacySIMDBinary(packedPrefix(f64), 0, 94, dst, s1, s2)
}

func (f *fn) emitVFPackedMin(dst, s1, s2 Reg, f64 bool) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VFPackedMin(dst, s1, s2, f64)
		return
	}
	f.legacySIMDBinary(packedPrefix(f64), 0, 93, dst, s1, s2)
}

func (f *fn) emitVFPackedMax(dst, s1, s2 Reg, f64 bool) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VFPackedMax(dst, s1, s2, f64)
		return
	}
	f.legacySIMDBinary(packedPrefix(f64), 0, 95, dst, s1, s2)
}

func (f *fn) emitVPsllwImm(dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsllwImm(dst, src, imm)
		return
	}
	if dst != src {
		f.mov128(dst, src)
	}
	f.a.SseMapRRI(0x66, 0, 113, Reg(6), dst, imm)
}

func (f *fn) emitVPsrlwImm(dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsrlwImm(dst, src, imm)
		return
	}
	if dst != src {
		f.mov128(dst, src)
	}
	f.a.SseMapRRI(0x66, 0, 113, Reg(2), dst, imm)
}

func (f *fn) emitVPsrawImm(dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsrawImm(dst, src, imm)
		return
	}
	if dst != src {
		f.mov128(dst, src)
	}
	f.a.SseMapRRI(0x66, 0, 113, Reg(4), dst, imm)
}

func (f *fn) emitVPslldImm(dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPslldImm(dst, src, imm)
		return
	}
	if dst != src {
		f.mov128(dst, src)
	}
	f.a.SseMapRRI(0x66, 0, 114, Reg(6), dst, imm)
}

func (f *fn) emitVPsrldImm(dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsrldImm(dst, src, imm)
		return
	}
	if dst != src {
		f.mov128(dst, src)
	}
	f.a.SseMapRRI(0x66, 0, 114, Reg(2), dst, imm)
}

func (f *fn) emitVPsradImm(dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsradImm(dst, src, imm)
		return
	}
	if dst != src {
		f.mov128(dst, src)
	}
	f.a.SseMapRRI(0x66, 0, 114, Reg(4), dst, imm)
}

func (f *fn) emitVPsllqImm(dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsllqImm(dst, src, imm)
		return
	}
	if dst != src {
		f.mov128(dst, src)
	}
	f.a.SseMapRRI(0x66, 0, 115, Reg(6), dst, imm)
}

func (f *fn) emitVPsrlqImm(dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPsrlqImm(dst, src, imm)
		return
	}
	if dst != src {
		f.mov128(dst, src)
	}
	f.a.SseMapRRI(0x66, 0, 115, Reg(2), dst, imm)
}

func (f *fn) emitPinsrb(dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64SSE41) {
		f.a.Pinsrb(dst, src, imm)
		return
	}
	f.insertSIMDLane(dst, src, imm, 1)
}

func (f *fn) emitPextrb(dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64SSE41) {
		f.a.Pextrb(dst, src, imm)
		return
	}
	f.extractSIMDLane(dst, src, imm, 1)
}

func (f *fn) emitPinsrd(dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64SSE41) {
		f.a.Pinsrd(dst, src, imm)
		return
	}
	f.insertSIMDLane(dst, src, imm, 4)
}

func (f *fn) emitPextrd(dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64SSE41) {
		f.a.Pextrd(dst, src, imm)
		return
	}
	f.extractSIMDLane(dst, src, imm, 4)
}

func (f *fn) emitPinsrq(dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64SSE41) {
		f.a.Pinsrq(dst, src, imm)
		return
	}
	f.insertSIMDLane(dst, src, imm, 8)
}

func (f *fn) emitPextrq(dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64SSE41) {
		f.a.Pextrq(dst, src, imm)
		return
	}
	f.extractSIMDLane(dst, src, imm, 8)
}
