package riscv64

import "github.com/wago-org/wago/src/core/codeimage"

// NativeTailPlan is address-free immutable source/physical frame metadata.
// Imported target owners are attached separately for each instance.
type NativeTailPlan struct {
	Kind, TypeIndex                      uint32
	FunctionIndex, ImportIndex, SourcePC uint32
	FrameBytes, SourceBase, SourceStride int
	ReferenceBase                        int
}

const (
	NativeTailDirectImport uint32 = iota
	NativeTailIndirect
	NativeTailReference
)

type NativeTailModule struct{ Plans []NativeTailPlan }

// CompiledModule carries a production native image and its function entries.
// RV64G emits fixed-width instructions; optional extensions are not selected.
type CompiledModule struct {
	NativeReferenceCalls                                       bool
	NativeTail                                                 *NativeTailModule
	Code                                                       []byte
	CodeImage                                                  codeimage.Image
	Entry, InternalEntry                                       []int
	DirectPrepared, DirectPreparedLight, DirectPreparedBounded []uint64
	PreparedIsolatedTables                                     bool
	RequiredAMD64Features                                      uint32
	RequiresBMI2, RequiresAVX2, RequiresAVX512                 bool
	RequiresBitCount                                           uint8
}
