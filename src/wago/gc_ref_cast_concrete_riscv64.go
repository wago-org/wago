//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import "crypto/sha256"

var rv64GCRefCastConcreteCodeDigest = [sha256.Size]byte{0x71, 0x96, 0xa1, 0x54, 0xb7, 0x13, 0x08, 0xe1, 0xc8, 0x12, 0x00, 0xea, 0x8e, 0x92, 0xcc, 0x17, 0x0c, 0xf1, 0xa4, 0x96, 0x05, 0x76, 0xd1, 0xf7, 0x1c, 0xe1, 0xe6, 0xc6, 0x71, 0x73, 0xd4, 0x85}

func rv64GCRefCastConcreteExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 12264 && sha256.Sum256(c.code) == rv64GCRefCastConcreteCodeDigest
}

func rv64GCRefCastConcrete(c *Compiled) bool {
	return rv64GCConcreteSubtypeProduct(c, rv64GCRefCastConcreteExecutionCode, stagedGCStructRefCastConcrete, []int{0, 1648, 7488})
}
