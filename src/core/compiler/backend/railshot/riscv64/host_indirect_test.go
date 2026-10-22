//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

func TestHostIndirectRV64EmissionBaselineAndCounts(t *testing.T) {
	for _, slots := range []int{0, 1, 64, 65, 128, 256} {
		for _, owned := range []bool{false, true} {
			code := HostIndirectSyncThunk(0xe1234567, slots, slots)
			if owned {
				code = HostIndirectOwnedSyncThunk(0xe1234567, slots, slots)
			}
			if len(code) == 0 || len(code)&3 != 0 || len(code) > MaxCodeBytes {
				t.Fatal("invalid thunk extent", slots, len(code))
			}
			for pc := 0; pc < len(code); pc += 4 {
				w := binary.LittleEndian.Uint32(code[pc:])
				switch w & 127 {
				case 0x03, 0x23, 0x13, 0x1b, 0x37, 0x33, 0x63, 0x17, 0x67:
				default:
					t.Fatalf("nonbaseline word pc%d %#x", pc, w)
				}
				if w&127 == 0x03 || w&127 == 0x13 || w&127 == 0x1b || w&127 == 0x37 || w&127 == 0x33 || w&127 == 0x17 || w&127 == 0x67 {
					rd := (w >> 7) & 31
					if rd == 3 || rd == 4 || rd == 26 || rd == 27 {
						t.Fatal("reserved Go/thread register defined", pc, rd)
					}
				}
			}
			t.Logf("HOST_THUNK owned=%t slots=%d bytes=%d sha256=%x", owned, slots, len(code), sha256.Sum256(code))
		}
	}
	if bytes.Equal(HostIndirectSyncThunk(0, 1, 1), HostIndirectOwnedSyncThunk(0, 1, 1)) {
		t.Fatal("home policy ignored")
	}
	for _, bad := range [][2]int{{-1, 0}, {0, -1}, {65536, 0}, {0, 65536}} {
		if code := HostIndirectSyncThunk(0, bad[0], bad[1]); len(code) != 0 {
			t.Fatal("invalid slot count emitted", bad)
		}
		if code := HostIndirectOwnedSyncThunk(0, bad[0], bad[1]); len(code) != 0 {
			t.Fatal("invalid owned slot count emitted", bad)
		}
	}
}
