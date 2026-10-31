//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import "crypto/sha256"

var (
	rv64GCBrOnCastConcreteCodeDigest     = [sha256.Size]byte{0x5b, 0x04, 0x1d, 0x26, 0xf2, 0xd8, 0x6a, 0x8e, 0x00, 0x56, 0x3f, 0xab, 0x18, 0x03, 0x3e, 0x9d, 0xcd, 0x9f, 0xfb, 0x5d, 0x04, 0xdd, 0x5f, 0x4e, 0x96, 0x76, 0x7c, 0xfb, 0x3c, 0x2f, 0xd5, 0xe0}
	rv64GCBrOnCastFailConcreteCodeDigest = [sha256.Size]byte{0x1f, 0xee, 0x2c, 0x34, 0x72, 0x82, 0x52, 0xec, 0x57, 0xd9, 0xf7, 0xd0, 0x94, 0xe9, 0x7a, 0x75, 0x48, 0x07, 0x29, 0x6f, 0x8f, 0x17, 0xbe, 0x90, 0x48, 0x3e, 0x69, 0xd8, 0x4d, 0x7a, 0x6d, 0x1b}
)

func rv64GCBrOnCastConcreteExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 14616 && sha256.Sum256(c.code) == rv64GCBrOnCastConcreteCodeDigest
}

func rv64GCBrOnCastFailConcreteExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 17944 && sha256.Sum256(c.code) == rv64GCBrOnCastFailConcreteCodeDigest
}

func rv64GCBrOnCastConcrete(c *Compiled) bool {
	return rv64GCConcreteSubtypeProduct(c, rv64GCBrOnCastConcreteExecutionCode, stagedGCStructBrOnCastConcrete, []int{0, 1648, 9504})
}

func rv64GCBrOnCastFailConcrete(c *Compiled) bool {
	return rv64GCConcreteSubtypeProduct(c, rv64GCBrOnCastFailConcreteExecutionCode, stagedGCStructBrOnCastFailConcrete, []int{0, 1648, 12768})
}
