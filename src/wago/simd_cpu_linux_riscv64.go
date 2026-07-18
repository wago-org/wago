//go:build linux && riscv64

package wago

import (
	"encoding/binary"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	// Linux/RISC-V reserves syscall 258 for riscv_hwprobe.
	sysRISCVHWProbe = 258

	riscvHWProbeKeyIMAExt0 = 4
	riscvHWProbeIMAExtV    = uint64(1 << 2)

	auxvATHWCAP    = uint64(16)
	riscvHWCAPISAV = uint64(1 << ('V' - 'A'))
)

type riscvHWProbePair struct {
	key   int64
	value uint64
}

// detectRISCV64SIMDHostFeatures requires both the versioned kernel hardware
// probe and the process HWCAP. riscv_hwprobe's V bit specifically means the
// ratified V 1.0 extension and is intersected over all online CPUs. HWCAP
// additionally reflects whether Linux allowed vector state for this process at
// exec time. Neither source is sufficient by itself on all deployed kernels.
func detectRISCV64SIMDHostFeatures() bool {
	key, extensions, probeOK := probeRISCV64IMAExtensions()
	hwcap, hwcapOK := readRISCV64HWCAP()
	return riscv64SIMDCapabilitiesOK(key, extensions, probeOK, hwcap, hwcapOK)
}

func riscv64SIMDCapabilitiesOK(probeKey int64, extensions uint64, probeOK bool, hwcap uint64, hwcapOK bool) bool {
	return probeOK && probeKey == riscvHWProbeKeyIMAExt0 &&
		extensions&riscvHWProbeIMAExtV != 0 &&
		hwcapOK && hwcap&riscvHWCAPISAV != 0
}

func probeRISCV64IMAExtensions() (key int64, extensions uint64, ok bool) {
	pair := riscvHWProbePair{key: riscvHWProbeKeyIMAExt0}
	_, _, errno := syscall.RawSyscall6(
		sysRISCVHWProbe,
		uintptr(unsafe.Pointer(&pair)),
		1,
		0, // cpusetsize
		0, // cpus: nil selects all online CPUs
		0, // flags
		0,
	)
	runtime.KeepAlive(&pair)
	return pair.key, pair.value, errno == 0
}

func readRISCV64HWCAP() (uint64, bool) {
	data, err := os.ReadFile("/proc/self/auxv")
	if err != nil {
		return 0, false
	}
	return parseRISCV64HWCAP(data)
}

func parseRISCV64HWCAP(auxv []byte) (uint64, bool) {
	const entryBytes = 16
	for len(auxv) >= entryBytes {
		tag := binary.LittleEndian.Uint64(auxv[:8])
		value := binary.LittleEndian.Uint64(auxv[8:entryBytes])
		if tag == auxvATHWCAP {
			return value, true
		}
		if tag == 0 { // AT_NULL
			break
		}
		auxv = auxv[entryBytes:]
	}
	return 0, false
}
