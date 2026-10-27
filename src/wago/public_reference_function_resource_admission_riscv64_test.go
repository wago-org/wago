//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"maps"
)

func rv64ReferenceResourceCopy(c *Compiled) *Compiled {
	copy := rv64ReferenceStateCopy(c)
	copy.Exports = maps.Clone(copy.Exports)
	copy.GlobalExports = maps.Clone(copy.GlobalExports)
	copy.memoryDir.exports = maps.Clone(copy.memoryDir.exports)
	return copy
}
