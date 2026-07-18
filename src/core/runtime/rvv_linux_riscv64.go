//go:build linux && riscv64

package runtime

import (
	"encoding/binary"
	"os"
	goruntime "runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	// Linux/RISC-V reserves syscall 258 for riscv_hwprobe.
	sysRISCVHWProbe = 258

	riscvHWProbeKeyIMAExt0          = 4
	riscvHWProbeIMAExtV             = uint64(1 << 2)
	riscvHWProbeKeyMisalignedScalar = 9

	auxvATHWCAP    = uint64(16)
	riscvHWCAPISAV = uint64(1 << ('V' - 'A'))
)

type riscvHWProbePair struct {
	key   int64
	value uint64
}

var (
	riscv64RVVOnce sync.Once
	riscv64RVVOK   bool

	riscv64MisalignedOnce sync.Once
	riscv64MisalignedPerf RISCV64MisalignedPerf
	riscv64MisalignedOK   bool
)

// RISCV64MisalignedPerf is Linux's all-CPU performance classification for
// misaligned scalar native-word accesses.
type RISCV64MisalignedPerf uint8

const (
	RISCV64MisalignedUnknown RISCV64MisalignedPerf = iota
	RISCV64MisalignedEmulated
	RISCV64MisalignedSlow
	RISCV64MisalignedFast
	RISCV64MisalignedUnsupported
)

// RISCV64HasRVV reports whether every online CPU implements ratified RVV 1.0
// and Linux enabled vector state for this process. It is cached because host
// capabilities and process vector permission do not change after exec.
func RISCV64HasRVV() bool {
	riscv64RVVOnce.Do(func() { riscv64RVVOK = detectRISCV64RVV() })
	return riscv64RVVOK
}

// RISCV64MisalignedScalarPerformance returns the all-online-CPU hwprobe
// classification. Linux's RISC-V userspace ABI supports misaligned scalar
// accesses regardless of speed; this result is for optimization policy and
// native qualification, not semantic admission.
func RISCV64MisalignedScalarPerformance() (RISCV64MisalignedPerf, bool) {
	riscv64MisalignedOnce.Do(func() {
		key, value, syscallOK := probeRISCV64Value(riscvHWProbeKeyMisalignedScalar)
		if syscallOK && key == riscvHWProbeKeyMisalignedScalar && value <= uint64(RISCV64MisalignedUnsupported) {
			riscv64MisalignedPerf = RISCV64MisalignedPerf(value)
			riscv64MisalignedOK = true
		}
	})
	return riscv64MisalignedPerf, riscv64MisalignedOK
}

func detectRISCV64RVV() bool {
	key, extensions, probeOK := probeRISCV64IMAExtensions()
	hwcap, hwcapOK := readRISCV64HWCAP()
	return riscv64RVVCapabilitiesOK(key, extensions, probeOK, hwcap, hwcapOK)
}

func riscv64RVVCapabilitiesOK(probeKey int64, extensions uint64, probeOK bool, hwcap uint64, hwcapOK bool) bool {
	return probeOK && probeKey == riscvHWProbeKeyIMAExt0 &&
		extensions&riscvHWProbeIMAExtV != 0 &&
		hwcapOK && hwcap&riscvHWCAPISAV != 0
}

func probeRISCV64IMAExtensions() (key int64, extensions uint64, ok bool) {
	return probeRISCV64Value(riscvHWProbeKeyIMAExt0)
}

func probeRISCV64Value(requestedKey int64) (key int64, value uint64, ok bool) {
	pair := riscvHWProbePair{key: requestedKey}
	_, _, errno := syscall.RawSyscall6(
		sysRISCVHWProbe,
		uintptr(unsafe.Pointer(&pair)),
		1,
		0, // cpusetsize
		0, // cpus: nil selects the intersection of all online CPUs
		0, // flags
		0,
	)
	goruntime.KeepAlive(&pair)
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
		if tag == 0 {
			break
		}
		auxv = auxv[entryBytes:]
	}
	return 0, false
}
