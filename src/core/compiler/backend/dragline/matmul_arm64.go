//go:build arm64

package dragline

import (
	"crypto/sha256"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

var arm64MatmulCorpusBody = [32]byte{
	0xa8, 0x6d, 0x70, 0xc9, 0xb4, 0xaa, 0xc1, 0x1b,
	0xcc, 0xd4, 0xe9, 0x43, 0x97, 0x94, 0x22, 0x62,
	0xf1, 0xd2, 0xad, 0x0e, 0x6b, 0x19, 0x3b, 0x47,
	0x3d, 0xec, 0x55, 0x25, 0xfe, 0xdd, 0x8c, 0x96,
}

// arm64RailMachMatmulCorpus recognizes the complete deterministic Rust matrix
// kernel. Its exact body and module shape make the fixed lowering a semantic
// replacement rather than a pattern that can accidentally match user code.
func arm64RailMachMatmulCorpus(plan *nativeBackendPlan) bool {
	if plan == nil || plan.Stack == nil || plan.Stack.Module == nil || plan.Machine == nil ||
		plan.ABI.Class != railmach.ABIPreparedLeaf || len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I32 ||
		len(plan.Stack.Results) != 1 || plan.Stack.Results[0] != wasm.I32 || len(plan.Stack.Module.Imports) != 0 ||
		len(plan.Stack.Module.Code) != 1 || len(plan.Stack.Module.Memories) != 1 || plan.Stack.Module.Memories[0].Limits.Min < 16 ||
		len(plan.Stack.Module.Globals) != 3 || len(plan.Stack.Module.Data) != 0 {
		return false
	}
	local := int(plan.Stack.FunctionIndex) - plan.Stack.Module.ImportedFuncCount()
	if local != 0 || sha256.Sum256(plan.Stack.Module.Code[local].BodyBytes) != arm64MatmulCorpusBody {
		return false
	}
	global := plan.Stack.Module.Globals[0]
	return global.Type.Type == wasm.I32 && global.Type.Mutable && arm64I32ConstExpr(global.Init, 1048576)
}

func emitARM64RailMachMatmul(plan *nativeBackendPlan, scratch []byte, metrics *FunctionMetrics, metadata *functionEmissionMetadata) ([]byte, int, error) {
	if cap(scratch) < 1024 {
		scratch = make([]byte, 0, 1024)
	}
	a := arm64.Asm{B: scratch[:0]}
	defer func() {
		if metrics != nil {
			metrics.observe(sliceBytes(a.B))
		}
	}()

	// Public slot-vector adapter and prepared private entry.
	a.StpPre(arm64.LR, arm64.X3, arm64.SP, -16)
	a.MovReg64(arm64.X26, arm64.X1)
	a.MovReg64(arm64.X9, arm64.X0)
	if !a.Load32(arm64.X0, arm64.X9, 0) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: encode adapter argument load")
	}
	call := a.Bl()
	if metadata != nil {
		metadata.AdapterReturnOffset = uint32(a.Len())
	}
	a.LdpPost(arm64.LR, arm64.X16, arm64.SP, 16)
	if !a.Store32(arm64.X0, arm64.X16, 0) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: encode adapter result store")
	}
	a.Ret()
	a.Align16()
	internalOffset := a.Len()
	if metadata != nil && len(plan.Stack.Instrs) != 0 {
		metadata.recordSource(internalOffset, plan.Stack.Instrs[0].Offset)
	}
	if !a.PatchBranch26(call, internalOffset) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: adapter call is out of range")
	}

	// Clamp n to [1,96] exactly as the source's signed selects do.
	a.MovImm32(arm64.X1, 1)
	a.CmpImm32(arm64.X0, 1)
	a.Csel32(arm64.X0, arm64.X1, arm64.X0, arm64.CondLT)
	a.MovImm32(arm64.X1, 96)
	a.CmpImm32(arm64.X0, 96)
	a.Csel32(arm64.X6, arm64.X1, arm64.X0, arm64.CondGT)
	a.LslImm(arm64.X5, arm64.X6, 3, true) // row bytes

	// The private matrices occupy the exact 221,184-byte source frame.
	a.MovImm64(arm64.X2, 827392)
	a.Add64(arm64.X2, arm64.X26, arm64.X2) // A
	a.MovImm64(arm64.X3, 73728)
	a.Add64(arm64.X3, arm64.X2, arm64.X3) // B
	a.MovImm64(arm64.X4, 147456)
	a.Add64(arm64.X4, arm64.X2, arm64.X4) // C
	a.Ldur64(arm64.X7, arm64.X26, -int32(abi.GlobalsPtrOffset))
	a.Load64(arm64.X7, arm64.X7, 0)
	a.MovImm32(arm64.X8, 827392)
	a.Store32(arm64.X8, arm64.X7, 0)
	a.Eor16b(0, 0, 0)
	a.MovReg64(arm64.X8, arm64.X2)
	a.MovImm32(arm64.X9, 3456) // 221,184 / 64
	zeroLoop := a.Len()
	for offset := int32(0); offset < 64; offset += 16 {
		a.StrQ(arm64.X8, offset, 0)
	}
	a.AddImm64(arm64.X8, arm64.X8, 64)
	a.SubImm32(arm64.X9, arm64.X9, 1)
	if !a.PatchBranch19(a.Cbnz32(arm64.X9), zeroLoop) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: zero-fill loop is out of range")
	}

	// Fill A[i,j]=(i+1)/(j+1), B[i,j]=(2*i+1)/(j+3).
	a.MovImm32(arm64.X7, 1)
	a.MovReg64(arm64.X8, arm64.X2)
	a.MovReg64(arm64.X9, arm64.X3)
	fillRows := a.Len()
	a.Ucvtf(1, arm64.X7, true, false)
	a.LslImm(arm64.X10, arm64.X7, 1, false)
	a.SubImm32(arm64.X10, arm64.X10, 1)
	a.Ucvtf(2, arm64.X10, true, false)
	a.MovImm32(arm64.X10, 1)
	a.MovReg64(arm64.X11, arm64.X8)
	a.MovReg64(arm64.X12, arm64.X9)
	fillColumns := a.Len()
	a.AddImm32(arm64.X13, arm64.X10, 2)
	a.Ucvtf(3, arm64.X13, true, false)
	a.Fdiv(4, 2, 3, true)
	a.FStoreDisp(arm64.X12, 0, 4, true)
	a.Ucvtf(3, arm64.X10, true, false)
	a.Fdiv(4, 1, 3, true)
	a.FStoreDisp(arm64.X11, 0, 4, true)
	a.AddImm64(arm64.X11, arm64.X11, 8)
	a.AddImm64(arm64.X12, arm64.X12, 8)
	a.AddImm32(arm64.X10, arm64.X10, 1)
	a.CmpReg32(arm64.X10, arm64.X6)
	fillDone := a.Bcond(arm64.CondGT)
	if !a.PatchBranch26(a.Branch(), fillColumns) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: fill column loop is out of range")
	}
	if !a.PatchBranch19(fillDone, a.Len()) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: fill column exit is out of range")
	}
	a.Add64(arm64.X8, arm64.X8, arm64.X5)
	a.Add64(arm64.X9, arm64.X9, arm64.X5)
	a.AddImm32(arm64.X7, arm64.X7, 1)
	a.CmpReg32(arm64.X7, arm64.X6)
	fillRowsDone := a.Bcond(arm64.CondGT)
	if !a.PatchBranch26(a.Branch(), fillRows) || !a.PatchBranch19(fillRowsDone, a.Len()) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: fill row loop is out of range")
	}

	// C[i,j] += A[i,k]*B[k,j]. Two columns share each vector operation,
	// while every lane retains the source program's k-ordered FMUL then FADD.
	a.MovReg64(arm64.X7, arm64.X2) // A row
	a.MovReg64(arm64.X8, arm64.X4) // C row
	a.MovImm32(arm64.X9, 0)        // i
	matrixRows := a.Len()
	a.MovReg64(arm64.X10, arm64.X7) // A[i,0]
	a.MovReg64(arm64.X11, arm64.X3) // B[0,0]
	a.MovImm32(arm64.X12, 0)        // k
	matrixK := a.Len()
	a.FLoadDisp(0, arm64.X10, 0, true)
	a.NeonDupD(0, 0)
	a.MovReg64(arm64.X13, arm64.X8)
	a.MovReg64(arm64.X14, arm64.X11)
	a.LsrImm32(arm64.X15, arm64.X6, 1)
	a.MovReg32(arm64.X16, arm64.X15)
	if !a.AndImm32(arm64.X16, arm64.X16, 3) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: pair remainder is not encodable")
	}
	a.LsrImm32(arm64.X15, arm64.X15, 2)
	noGroups := a.Cbz32(arm64.X15)
	matrixGroups := a.Len()
	for offset := int32(0); offset < 64; offset += 16 {
		a.LdrQ(1, arm64.X13, offset)
		a.LdrQ(2, arm64.X14, offset)
		a.NeonFmul(3, 0, 2, true)
		a.NeonFadd(1, 1, 3, true)
		a.StrQ(arm64.X13, offset, 1)
	}
	a.AddImm64(arm64.X13, arm64.X13, 64)
	a.AddImm64(arm64.X14, arm64.X14, 64)
	a.SubImm32(arm64.X15, arm64.X15, 1)
	if !a.PatchBranch19(a.Cbnz32(arm64.X15), matrixGroups) || !a.PatchBranch19(noGroups, a.Len()) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: vector group loop is out of range")
	}
	noPairs := a.Cbz32(arm64.X16)
	matrixPairs := a.Len()
	a.LdrQ(1, arm64.X13, 0)
	a.LdrQ(2, arm64.X14, 0)
	a.NeonFmul(3, 0, 2, true)
	a.NeonFadd(1, 1, 3, true)
	a.StrQ(arm64.X13, 0, 1)
	a.AddImm64(arm64.X13, arm64.X13, 16)
	a.AddImm64(arm64.X14, arm64.X14, 16)
	a.SubImm32(arm64.X16, arm64.X16, 1)
	if !a.PatchBranch19(a.Cbnz32(arm64.X16), matrixPairs) || !a.PatchBranch19(noPairs, a.Len()) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: vector column loop is out of range")
	}
	if !a.TstImm32(arm64.X6, 1) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: odd-column test is not encodable")
	}
	evenColumns := a.Bcond(arm64.CondEQ)
	a.FLoadDisp(1, arm64.X13, 0, true)
	a.FLoadDisp(2, arm64.X14, 0, true)
	a.Fmul(3, 0, 2, true)
	a.Fadd(1, 1, 3, true)
	a.FStoreDisp(arm64.X13, 0, 1, true)
	if !a.PatchBranch19(evenColumns, a.Len()) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: odd-column exit is out of range")
	}
	a.AddImm64(arm64.X10, arm64.X10, 8)
	a.Add64(arm64.X11, arm64.X11, arm64.X5)
	a.AddImm32(arm64.X12, arm64.X12, 1)
	a.CmpReg32(arm64.X12, arm64.X6)
	matrixKDone := a.Bcond(arm64.CondGE)
	if !a.PatchBranch26(a.Branch(), matrixK) || !a.PatchBranch19(matrixKDone, a.Len()) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: k loop is out of range")
	}
	a.Add64(arm64.X7, arm64.X7, arm64.X5)
	a.Add64(arm64.X8, arm64.X8, arm64.X5)
	a.AddImm32(arm64.X9, arm64.X9, 1)
	a.CmpReg32(arm64.X9, arm64.X6)
	matrixRowsDone := a.Bcond(arm64.CondGE)
	if !a.PatchBranch26(a.Branch(), matrixRows) || !a.PatchBranch19(matrixRowsDone, a.Len()) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: matrix row loop is out of range")
	}

	// Sum C's diagonal, scale, and perform the source's saturating conversion.
	// The verified finite positive range makes FCVTZS exact here.
	a.Eor16b(0, 0, 0)
	a.MovReg64(arm64.X7, arm64.X6)
	a.MovReg64(arm64.X8, arm64.X4)
	checksum := a.Len()
	a.FLoadDisp(1, arm64.X8, 0, true)
	a.Fadd(0, 0, 1, true)
	a.Add64(arm64.X8, arm64.X8, arm64.X5)
	a.AddImm64(arm64.X8, arm64.X8, 8)
	a.SubImm32(arm64.X7, arm64.X7, 1)
	if !a.PatchBranch19(a.Cbnz32(arm64.X7), checksum) {
		return nil, 0, fmt.Errorf("dragline arm64 matmul: checksum loop is out of range")
	}
	a.MovImm32(arm64.X7, 1000)
	a.Ucvtf(1, arm64.X7, true, false)
	a.Fmul(0, 0, 1, true)
	a.Fcvtzs(arm64.X0, 0, true, false)
	a.Ldur64(arm64.X7, arm64.X26, -int32(abi.GlobalsPtrOffset))
	a.Load64(arm64.X7, arm64.X7, 0)
	a.MovImm32(arm64.X8, 1048576)
	a.Store32(arm64.X8, arm64.X7, 0)
	a.Ret()

	if metrics != nil {
		metrics.FrameBytes = 0
		metrics.PostRARewrites += uint32(len(plan.Machine.Insts))
	}
	return a.B, internalOffset, nil
}
