//go:build linux && arm64

package compiler

import (
	"encoding/binary"
	"testing"
)

func TestLinuxARM64AuxvHasMOPS(t *testing.T) {
	auxv := make([]byte, 32)
	binary.LittleEndian.PutUint64(auxv[0:], linuxAuxvHWCAP2)
	binary.LittleEndian.PutUint64(auxv[8:], linuxHWCAP2MOPS)
	if !linuxARM64AuxvHasMOPS(auxv) {
		t.Fatal("MOPS HWCAP2 bit was not detected")
	}
	binary.LittleEndian.PutUint64(auxv[8:], 0)
	if linuxARM64AuxvHasMOPS(auxv) {
		t.Fatal("MOPS reported without its HWCAP2 bit")
	}
}
