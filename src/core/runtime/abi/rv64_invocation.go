package abi

// RV64 public contexts extend the shared120-byte image. The capability word is
// reserved for instance-owned tail dispatch; the invocation word borrows an
// Engine-owned view only while that root's execution lease is held.
const (
	RV64TailCapabilityOffset          = 120
	RV64InvocationContextOffset       = 128
	RV64InstanceContextBytes          = 136
	RV64InvocationViewBytes           = 64
	RV64InvocationMagic               = uint64(0x5741474f49564331)
	RV64InvocationFenceOffset         = 8
	RV64InvocationTopOffset           = 16
	RV64InvocationArgumentsOffset     = 24
	RV64InvocationArgumentBytesOffset = 32
	RV64InvocationResultsOffset       = 40
	RV64InvocationResultBytesOffset   = 48
)
