//go:build !amd64 || tinygo

package amd64

// On non-amd64 hosts the amd64 backend is only cross-compiled (its output is
// never executed here), and TinyGo cannot run the CPUID assembly stub. Either
// way, assume the 128-bit baseline so no wide-vector opcode is emitted.
const (
	hasAVX2   = false
	hasAVX512 = false
)
