//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"maps"
	"slices"
)

func rv64ReferenceResourceImportCopy(c *Compiled) *Compiled {
	v := rv64ReferenceImportCopy(c)
	v.GlobalImports = slices.Clone(v.GlobalImports)
	v.GlobalExports = maps.Clone(v.GlobalExports)
	v.memoryDir.exports = maps.Clone(v.memoryDir.exports)
	v.validateMemo.importModuleEnds = slices.Clone(c.loadValidateMemo().importModuleEnds)
	return v
}
