//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// NativeFrameBytes reads the canonical production entry guard packet from a
// retained compiler image. It checks the stack-fence load, both prewrite guards,
// canonical frame constant and bounded cold destinations. It does not establish
// code ownership or authenticate the rest of a trusted native artifact.
func NativeFrameBytes(code []byte, entry int) (int, error) {
	if entry < 0 || entry&15 != 0 || entry > len(code)-36 || len(code) > 1<<31-1 {
		return 0, unsupported("native frame entry/span")
	}
	b := code[entry:]
	w := binary.LittleEndian.Uint32(b[16:])
	frame, end := 0, 20
	if w&0xfffff == 0x13|uint32(enc.Scratch0)<<7 {
		frame = int(int32(w) >> 20)
	} else if w&0xfff == 0x37|uint32(enc.Scratch0)<<7 {
		frame = int(int32(w & 0xfffff000))
		next := binary.LittleEndian.Uint32(b[20:])
		if next&0xfffff == 0x1b|uint32(enc.Scratch0)<<7|uint32(enc.Scratch0)<<15 {
			frame += int(int32(next) >> 20)
			end = 24
		}
	} else {
		return 0, unsupported("native frame constant instruction")
	}
	if frame < 16 || frame > MaxFrameBytes || frame&15 != 0 || len(b) < end+16 {
		return 0, unsupported("native frame constant/span bound")
	}
	// Compare the fixed RV64G words directly. Building a temporary Asm here
	// escapes its backing slice; this reader is also used on warm entry refresh.
	var expected [10]uint32
	expected[0] = (uint32(4096-abi.StackFenceOffset) << 20) | uint32(enc.X25)<<15 | 3<<12 | uint32(enc.X29)<<7 | 0x03
	expected[1] = uint32(enc.X29)<<20 | uint32(enc.SP)<<15 | 7<<12 | 6<<8 | 0x63
	expected[2] = uint32(enc.Scratch1)<<7 | 0x17
	expected[3] = uint32(enc.Scratch1)<<15 | 0x67
	n := 5
	if frame <= 2047 {
		expected[4] = uint32(frame)<<20 | uint32(enc.Scratch0)<<7 | 0x13
		if end != 20 {
			return 0, unsupported("native frame canonical small constant")
		}
	} else {
		expected[4] = uint32((frame+2048)>>12)<<12 | uint32(enc.Scratch0)<<7 | 0x37
		low := int32(uint32(frame)<<20) >> 20
		if low != 0 {
			expected[5] = (uint32(low)&0xfff)<<20 | uint32(enc.Scratch0)<<15 | uint32(enc.Scratch0)<<7 | 0x1b
			n++
		}
		if end != n*4 {
			return 0, unsupported("native frame canonical large constant")
		}
	}
	expected[n] = 0x20<<25 | uint32(enc.X29)<<20 | uint32(enc.SP)<<15 | uint32(enc.Scratch1)<<7 | 0x33
	expected[n+1] = uint32(enc.Scratch0)<<20 | uint32(enc.Scratch1)<<15 | 7<<12 | 6<<8 | 0x63
	expected[n+2], expected[n+3] = expected[2], expected[3]
	n += 4
	for i, word := range expected[:n] {
		mask := uint32(0xffffffff)
		switch i {
		case 2, n - 2:
			mask = 0xfff
		case 3, n - 1:
			mask = 0xfffff
		}
		if binary.LittleEndian.Uint32(b[i*4:])&mask != word&mask {
			return 0, unsupported("native frame prewrite guard packet")
		}
	}
	cold := int64(-1)
	for _, off := range []int{8, n*4 - 8} {
		hi := int64(int32(binary.LittleEndian.Uint32(b[off:]) & 0xfffff000))
		lo := int64(int32(binary.LittleEndian.Uint32(b[off+4:])) >> 20)
		target := int64(off) + hi + lo
		if target < int64(n*4) || target&3 != 0 || target > int64(len(b)-4) || cold >= 0 && target != cold {
			return 0, unsupported("native frame cold guard destination")
		}
		cold = target
	}
	return frame, nil
}
