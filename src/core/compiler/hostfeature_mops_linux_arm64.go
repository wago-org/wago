//go:build linux && arm64

package compiler

import (
	"encoding/binary"
	"os"
)

const (
	linuxAuxvHWCAP2 = 26
	linuxHWCAP2MOPS = uint64(1) << 43
)

func hostHasARM64MOPS() bool {
	auxv, err := os.ReadFile("/proc/self/auxv")
	return err == nil && linuxARM64AuxvHasMOPS(auxv)
}

func linuxARM64AuxvHasMOPS(auxv []byte) bool {
	for len(auxv) >= 16 {
		tag := binary.LittleEndian.Uint64(auxv[:8])
		value := binary.LittleEndian.Uint64(auxv[8:16])
		auxv = auxv[16:]
		if tag == 0 {
			return false
		}
		if tag == linuxAuxvHWCAP2 {
			return value&linuxHWCAP2MOPS != 0
		}
	}
	return false
}
