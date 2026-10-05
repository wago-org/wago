package riscv64

import "github.com/wago-org/wago/src/core/codeimage"

// CompiledModule carries a production native image and its function entries.
// RV64G emits fixed-width instructions; optional extensions are not selected.
type CompiledModule struct {
	Code                                                       []byte
	CodeImage                                                  codeimage.Image
	Entry, InternalEntry                                       []int
	DirectPrepared, DirectPreparedLight, DirectPreparedBounded []uint64
	PreparedIsolatedTables                                     bool
	RequiredAMD64Features                                      uint32
	RequiresBMI2, RequiresAVX2, RequiresAVX512                 bool
	RequiresBitCount                                           uint8
}
