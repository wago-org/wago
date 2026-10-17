//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func nativeFramePacketForTest(t *testing.T, frame int) []byte {
	t.Helper()
	info := FunctionInfo{FrameHeaderBytes: callFrameHeader, mode: emissionMode{production: true, interruptible: true, invocationContext: true, functionIndex: 3, entryPC: 1}}
	b, got, err := frameFunction(nil, nil, 0, (frame-callFrameHeader)/8, true, -1, info)
	if err != nil || got.FrameBytes != frame {
		t.Fatal("production frame fixture", frame, got.FrameBytes, err)
	}
	return b
}

func TestNativeFrameBytesCanonicalProductionPackets(t *testing.T) {
	for _, frame := range []int{48, 64, 2032, 2048, 2064, 4080, 4096, 4112, MaxFrameBytes} {
		b := nativeFramePacketForTest(t, frame)
		image := append(make([]byte, 16), b...)
		got, err := NativeFrameBytes(image, 16)
		if err != nil || got != frame {
			t.Fatal("canonical frame read", frame, got, err)
		}
		if a := testing.AllocsPerRun(4, func() {
			if got, err := NativeFrameBytes(image, 16); err != nil || got != frame {
				panic("frame read")
			}
		}); a != 0 {
			t.Fatal("frame packet validation allocation", a)
		}
	}
}

func TestNativeFrameBytesRejectsMalformedPrewritePackets(t *testing.T) {
	b := nativeFramePacketForTest(t, 2064)
	for _, off := range []int{0, 4, 8, 12, 16, 20, 24, 28, 32, 36} {
		bad := bytes.Clone(b)
		bad[off] ^= 1 // instruction opcode, never a patched cold immediate
		if frame, err := NativeFrameBytes(bad, 0); frame != 0 || !errors.Is(err, ErrUnsupported) {
			t.Fatal("malformed frame guard", off, frame, err)
		}
	}
	for _, n := range []int{0, 15, 16, 19, 20, 23, 24, 35, 36, 39} {
		if frame, err := NativeFrameBytes(b[:n], 0); frame != 0 || !errors.Is(err, ErrUnsupported) {
			t.Fatal("truncated frame packet", n, frame, err)
		}
	}
	for _, entry := range []int{-16, 1, len(b), len(b) + 16} {
		if frame, err := NativeFrameBytes(b, entry); frame != 0 || !errors.Is(err, ErrUnsupported) {
			t.Fatal("frame entry range", entry, frame, err)
		}
	}
	for _, off := range []int{8, 32} {
		for _, target := range []int{0, 16, 40, len(b), len(b) + 4} {
			bad := bytes.Clone(b)
			a := enc.Asm{B: bad}
			if err := a.PatchLong(off, int64(target)); err != nil {
				t.Fatal(err)
			}
			if frame, err := NativeFrameBytes(bad, 0); frame != 0 || !errors.Is(err, ErrUnsupported) {
				t.Fatal("cold target outside canonical shared destination", off, target, frame, err)
			}
		}
	}
	for _, off := range []int{4, 28} {
		for _, bit := range []uint{8, 12, 15, 20} { // skip distance, condition, and both operands
			bad := bytes.Clone(b)
			w := binary.LittleEndian.Uint32(bad[off:])
			binary.LittleEndian.PutUint32(bad[off:], w^(1<<bit))
			if _, err := NativeFrameBytes(bad, 0); !errors.Is(err, ErrUnsupported) {
				t.Fatal("changed guard branch field", off, bit, err)
			}
		}
	}
	for _, off := range []int{8, 12, 32, 36} {
		bits := []uint{7} // AUIPC destination and JALR link register
		if off == 12 || off == 36 {
			bits = append(bits, 12, 15) // JALR funct3 and base register
		}
		for _, bit := range bits {
			bad := bytes.Clone(b)
			w := binary.LittleEndian.Uint32(bad[off:])
			binary.LittleEndian.PutUint32(bad[off:], w^(1<<bit))
			if _, err := NativeFrameBytes(bad, 0); !errors.Is(err, ErrUnsupported) {
				t.Fatal("changed cold veneer field", off, bit, err)
			}
		}
	}
	// A plausible bounded integer still must be an aligned native frame.
	bad := bytes.Clone(nativeFramePacketForTest(t, 64))
	w := binary.LittleEndian.Uint32(bad[16:])
	binary.LittleEndian.PutUint32(bad[16:], w+1<<20)
	if _, err := NativeFrameBytes(bad, 0); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unaligned frame constant accepted", err)
	}
}

func TestNativeFrameBytesPublicLargeCodeSpan(t *testing.T) {
	packet := nativeFramePacketForTest(t, 48)
	entry := MaxCodeBytes + 16
	image := make([]byte, entry+len(packet))
	copy(image[entry:], packet)
	if frame, err := NativeFrameBytes(image, entry); err != nil || frame != 48 {
		t.Fatalf("public large code frame=%d,%v", frame, err)
	}
	if allocations := testing.AllocsPerRun(4, func() {
		if frame, err := NativeFrameBytes(image, entry); err != nil || frame != 48 {
			panic("public frame read failed")
		}
	}); allocations != 0 {
		t.Fatal("large-span warm frame allocations", allocations)
	}
	image[entry] ^= 1
	if _, err := NativeFrameBytes(image, entry); !errors.Is(err, ErrUnsupported) {
		t.Fatal("large image bypassed guard validation", err)
	}
}
