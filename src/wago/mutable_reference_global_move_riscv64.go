//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import "crypto/sha256"

var rv64MutableFuncrefMoveCodeDigest = [sha256.Size]byte{0x67, 0x11, 0xc3, 0x51, 0x63, 0xb8, 0x5b, 0xa9, 0xa9, 0x56, 0xa3, 0x58, 0x7e, 0xe5, 0xd5, 0xc5, 0xdb, 0x70, 0xa4, 0xca, 0x4e, 0x25, 0x06, 0x64, 0xf5, 0xfd, 0xdb, 0x38, 0x1b, 0x32, 0x8d, 0x48}

func rv64MutableFuncrefGlobalMoveExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 1132 && sha256.Sum256(c.code) == rv64MutableFuncrefMoveCodeDigest
}

// rv64MutableFuncrefGlobalMove authenticates the first exact multi-write guest
// reference product. Its sole function copies source to target and then clears
// source, with each write crossing the ordinary parked ownership transaction.
func rv64MutableFuncrefGlobalMove(c *Compiled) bool {
	return rv64MutableFuncrefGlobalTransferMetadata(c, "move") && rv64MutableFuncrefGlobalMoveExecutionCode(c)
}
