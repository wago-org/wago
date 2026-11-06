//go:build amd64

package amd64

import (
	"bytes"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func zeroTailFlagFixture(wide bool) ([]byte, callReloc, callReloc) {
	code := append([]byte{0xcc, 0xcc, 0xcc, 0xcc, 0xcc}, []byte{0x48, 0x81, 0xc4, 0, 0, 0, 0, 0x45, 0x85, 0xc9, 0x0f, 0x85, 1, 2, 3, 4, 0xe9, 5, 6, 7, 8}...)
	code = append(code, 0xcc, 0xcc, 0xcc, 0xcc, 0xcc)
	nonzero, zero := tailEdgeNonzero32, tailEdgeZero32
	if wide {
		code[12] = 0x4d
		nonzero, zero = tailEdgeNonzero64, tailEdgeZero64
	}
	return code, callReloc{at: 17, target: 1, internal: true, tailEdge: nonzero}, callReloc{at: 22, target: 1, internal: true, tailEdge: zero}
}

func TestZeroTailFlagPacketPreservesSurroundingOffsets(t *testing.T) {
	for _, wide := range []bool{false, true} {
		code, nz, z := zeroTailFlagFixture(wide)
		original := append([]byte(nil), code...)
		site, ok := rewriteZeroTailFlagPacket(code, &nz, &z)
		if !ok || site != 8 || nz.at != 7 || z.at != 12 {
			t.Fatalf("wide=%t site=%d ok=%t relocs=%d/%d", wide, site, ok, nz.at, z.at)
		}
		if !bytes.Equal(code[:5], original[:5]) || !bytes.Equal(code[26:], original[26:]) || !bytes.Equal(code[5:16], original[15:26]) {
			t.Fatal("terminal packet rewrite moved surrounding code or changed branch payloads")
		}
		for _, b := range code[16:26] {
			if b != 0x90 {
				t.Fatal("vacated terminal space is not unreachable padding")
			}
		}
	}
}

func TestZeroTailFlagPacketRejectsUnprovedSequences(t *testing.T) {
	for _, mutate := range []func([]byte, *callReloc, *callReloc){
		func(b []byte, n, z *callReloc) { b[8] = 1 },
		func(b []byte, n, z *callReloc) { b[12] = 0x4d },
		func(b []byte, n, z *callReloc) { b[14] = 0xc8 },
		func(b []byte, n, z *callReloc) { b[16] = 0x84 },
		func(b []byte, n, z *callReloc) { b[21] = 0xe8 },
		func(b []byte, n, z *callReloc) { z.target = 2 },
		func(b []byte, n, z *callReloc) { n.internal = false },
		func(b []byte, n, z *callReloc) { z.gcStub = gcSharedStubResolveObject },
		func(b []byte, n, z *callReloc) { n.at = 1; z.at = 6 },
		func(b []byte, n, z *callReloc) { z.at++ },
	} {
		code, nz, z := zeroTailFlagFixture(false)
		mutate(code, &nz, &z)
		original := append([]byte(nil), code...)
		oldNZ, oldZ := nz, z
		if _, ok := rewriteZeroTailFlagPacket(code, &nz, &z); ok || !bytes.Equal(code, original) || nz != oldNZ || z != oldZ {
			t.Fatal("unproved packet was changed")
		}
	}
}

func TestTailArgumentFlagsRequireAnEmptyFrame(t *testing.T) {
	old := nativeCompactionEnabled
	nativeCompactionEnabled = false
	defer func() { nativeCompactionEnabled = old }()
	for _, frameBytes := range []uint32{0, 32} {
		code, nz, z := zeroTailFlagFixture(false)
		original := append([]byte(nil), code...)
		nz.tailEdge |= tailReuseArgumentFlags
		f := fn{a: &encoder.Asm{B: code}, sc: &scratch{tailFrameSites: []int{8}}, relocs: []callReloc{nz, z}, localTailLeaf: true, policy: currentCodegenPolicy()}
		f.finishLocalTailArgumentFlags(frameBytes)
		if f.relocs[0].tailEdge != tailEdgeNonzero32 {
			t.Fatal("temporary argument-flag proof leaked into module relocations")
		}
		if frameBytes == 0 {
			if f.relocs[0].at != 7 || len(f.sc.tailFrameSites) != 0 {
				t.Fatal("empty tail frame was not removed from terminal metadata")
			}
		} else if !bytes.Equal(code, original) || f.relocs[0].at != nz.at || len(f.sc.tailFrameSites) != 1 {
			t.Fatal("real frame release or its zero test was removed")
		}
	}
}
