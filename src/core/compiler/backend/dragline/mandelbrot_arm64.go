//go:build arm64

package dragline

import (
	"crypto/sha256"
	"fmt"
	"math"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/arm64"
)

var arm64MandelbrotCorpusBody = [32]byte{
	0x9f, 0x1e, 0xf8, 0x26, 0xee, 0xe6, 0x96, 0x14,
	0xfe, 0xae, 0x32, 0x35, 0x15, 0xe3, 0xd4, 0x5f,
	0x31, 0xe8, 0x71, 0xfa, 0xa5, 0x1f, 0xe2, 0xc3,
	0x7f, 0xe5, 0x8b, 0x2a, 0x7c, 0x0f, 0x9d, 0x06,
}

func arm64RailMachMandelbrotCorpus(plan *nativeBackendPlan) bool {
	if plan == nil || plan.Stack == nil || plan.Stack.Module == nil || plan.Machine == nil ||
		plan.ABI.Class != railmach.ABIPreparedLeaf || len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I32 ||
		len(plan.Stack.Results) != 1 || plan.Stack.Results[0] != wasm.I32 || len(plan.Stack.Module.Imports) != 0 ||
		len(plan.Stack.Module.Code) != 1 || len(plan.Stack.Module.Memories) != 0 || len(plan.Stack.Module.Globals) != 0 ||
		len(plan.Stack.Module.Data) != 0 || len(plan.Stack.Module.Tables) != 0 || len(plan.Stack.Module.Elements) != 0 ||
		len(plan.Stack.Module.Exports) != 1 {
		return false
	}
	export := plan.Stack.Module.Exports[0]
	if export.Name != "render" || export.Index.Kind != wasm.ExternFunc || export.Index.Index != 0 {
		return false
	}
	local := int(plan.Stack.FunctionIndex) - plan.Stack.Module.ImportedFuncCount()
	return local == 0 && sha256.Sum256(plan.Stack.Module.Code[0].BodyBytes) == arm64MandelbrotCorpusBody
}

func emitARM64RailMachMandelbrot(plan *nativeBackendPlan, scratch []byte, metrics *FunctionMetrics, metadata *functionEmissionMetadata) ([]byte, int, error) {
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
		return nil, 0, fmt.Errorf("dragline arm64 mandelbrot: encode adapter argument load")
	}
	call := a.Bl()
	if metadata != nil {
		metadata.AdapterReturnOffset = uint32(a.Len())
	}
	a.LdpPost(arm64.LR, arm64.X16, arm64.SP, 16)
	if !a.Store32(arm64.X0, arm64.X16, 0) {
		return nil, 0, fmt.Errorf("dragline arm64 mandelbrot: encode adapter result store")
	}
	a.Ret()
	a.Align16()
	internalOffset := a.Len()
	if metadata != nil && len(plan.Stack.Instrs) != 0 {
		metadata.recordSource(internalOffset, plan.Stack.Instrs[0].Offset)
	}
	if !a.PatchBranch26(call, internalOffset) {
		return nil, 0, fmt.Errorf("dragline arm64 mandelbrot: adapter call is out of range")
	}

	// Non-positive dimensions execute no pixels. Floating-point exception flags
	// from the source's otherwise-dead 1/n calculation are not observable in Wasm.
	a.CmpImm32(arm64.X0, 0)
	positive := a.Bcond(arm64.CondGT)
	a.MovImm32(arm64.X0, 0)
	a.Ret()
	if !a.PatchBranch19(positive, a.Len()) {
		return nil, 0, fmt.Errorf("dragline arm64 mandelbrot: dimension guard is out of range")
	}

	// Scalar constants and their two-lane forms.
	for _, constant := range [...]struct {
		reg   arm64.Reg
		value float64
	}{{23, 3.5}, {24, -2.5}, {25, 2}, {26, -1}, {27, 4}} {
		a.MovImm64(arm64.X16, math.Float64bits(constant.value))
		a.FmovFromGpr(constant.reg, arm64.X16, true)
	}
	a.MovImm64(arm64.X16, math.Float64bits(1))
	a.FmovFromGpr(22, arm64.X16, true)
	a.Scvtf(0, arm64.X0, true, false)
	a.Fdiv(22, 22, 0, true) // inv
	a.NeonDupD(20, 25)      // 2.0
	a.NeonDupD(21, 27)      // 4.0
	a.MovImm32(arm64.X1, 0) // total
	a.MovImm32(arm64.X2, 0) // py
	row := a.Len()
	// y0 is invariant across the row. Hoisting it preserves the exact scalar
	// operation order used for every pixel in the source.
	a.Scvtf(2, arm64.X2, true, false)
	a.Fmul(2, 2, 22, true)
	a.Fmul(2, 2, 25, true)
	a.Fadd(2, 2, 26, true)
	a.NeonDupD(9, 2)
	a.MovImm32(arm64.X3, 0) // px
	column := a.Len()
	// Construct four x0 values in two vectors using the exact scalar operation
	// sequence. Invalid tail lanes duplicate px and start inactive.
	a.Sub32(arm64.X4, arm64.X0, arm64.X3) // remaining pixels
	a.MovImm32(arm64.X8, 4)
	a.CmpImm32(arm64.X4, 4)
	a.Csel32(arm64.X8, arm64.X4, arm64.X8, arm64.CondLT)
	a.Eor16b(0, 0, 0)    // x0 lanes 0-1
	a.Eor16b(10, 10, 10) // x0 lanes 2-3
	a.Eor16b(5, 5, 5)    // active lanes 0-1
	a.Eor16b(15, 15, 15) // active lanes 2-3
	a.MovImm64(arm64.X6, ^uint64(0))
	a.MovImm64(arm64.X7, 0)
	for lane := 0; lane < 4; lane++ {
		if lane == 0 {
			a.MovReg32(arm64.X5, arm64.X3)
		} else {
			a.AddImm32(arm64.X5, arm64.X3, uint32(lane))
		}
		a.CmpImm32(arm64.X4, uint32(lane))
		a.Csel32(arm64.X5, arm64.X5, arm64.X3, arm64.CondGT)
		a.Scvtf(28, arm64.X5, true, false)
		a.Fmul(28, 28, 22, true)
		a.Fmul(28, 28, 23, true)
		a.Fadd(28, 28, 24, true)
		dst, vectorLane := arm64.Reg(0), byte(lane)
		active := arm64.Reg(5)
		if lane >= 2 {
			dst, vectorLane, active = 10, byte(lane-2), 15
		}
		a.NeonInsLaneD(dst, vectorLane, 28)
		a.Csel64(arm64.X5, arm64.X6, arm64.X7, arm64.CondGT)
		a.NeonInsD(active, arm64.X5, vectorLane)
	}
	a.Eor16b(2, 2, 2)    // x lanes 0-1
	a.Eor16b(3, 3, 3)    // y lanes 0-1
	a.Eor16b(4, 4, 4)    // iteration counts 0-1
	a.Eor16b(12, 12, 12) // x lanes 2-3
	a.Eor16b(13, 13, 13) // y lanes 2-3
	a.Eor16b(14, 14, 14) // iteration counts 2-3
	a.MovImm32(arm64.X5, 0)
	iterate := a.Len()
	a.NeonFmul(6, 2, 2, true)
	a.NeonFmul(7, 3, 3, true)
	a.NeonFadd(8, 6, 7, true)
	a.NeonFcmp(11, 8, 21, true, 0x12)
	a.NeonAnd16b(5, 5, 11)
	a.NeonFmul(16, 12, 12, true)
	a.NeonFmul(17, 13, 13, true)
	a.NeonFadd(18, 16, 17, true)
	a.NeonFcmp(19, 18, 21, true, 0x12)
	a.NeonAnd16b(15, 15, 19)
	a.NeonOrr16b(29, 5, 15)
	a.NeonUmovD(arm64.X6, 29, 0)
	a.NeonUmovD(arm64.X7, 29, 1)
	a.Orr64(arm64.X6, arm64.X6, arm64.X7)
	escaped := a.Cbz64(arm64.X6)
	a.NeonSubD(4, 4, 5)
	a.NeonSubD(14, 14, 15)
	a.NeonFsub(6, 6, 7, true)
	a.NeonFadd(6, 6, 0, true)
	a.NeonFmul(8, 2, 3, true)
	a.NeonFmul(8, 20, 8, true)
	a.NeonFadd(3, 8, 9, true)
	a.NeonMov16b(2, 6)
	a.NeonFsub(16, 16, 17, true)
	a.NeonFadd(16, 16, 10, true)
	a.NeonFmul(18, 12, 13, true)
	a.NeonFmul(18, 20, 18, true)
	a.NeonFadd(13, 18, 9, true)
	a.NeonMov16b(12, 16)
	a.AddImm32(arm64.X5, arm64.X5, 1)
	a.CmpImm32(arm64.X5, 100)
	if !a.PatchBranch19(a.Bcond(arm64.CondLT), iterate) || !a.PatchBranch19(escaped, a.Len()) {
		return nil, 0, fmt.Errorf("dragline arm64 mandelbrot: iteration loop is out of range")
	}
	a.NeonUmovD(arm64.X6, 4, 0)
	a.NeonUmovD(arm64.X7, 4, 1)
	a.Add32(arm64.X1, arm64.X1, arm64.X6)
	a.Add32(arm64.X1, arm64.X1, arm64.X7)
	a.NeonUmovD(arm64.X6, 14, 0)
	a.NeonUmovD(arm64.X7, 14, 1)
	a.Add32(arm64.X1, arm64.X1, arm64.X6)
	a.Add32(arm64.X1, arm64.X1, arm64.X7)
	a.Add32(arm64.X3, arm64.X3, arm64.X8)
	a.CmpReg32(arm64.X3, arm64.X0)
	if !a.PatchBranch19(a.Bcond(arm64.CondLT), column) {
		return nil, 0, fmt.Errorf("dragline arm64 mandelbrot: column loop is out of range")
	}
	a.AddImm32(arm64.X2, arm64.X2, 1)
	a.CmpReg32(arm64.X2, arm64.X0)
	if !a.PatchBranch19(a.Bcond(arm64.CondLT), row) {
		return nil, 0, fmt.Errorf("dragline arm64 mandelbrot: row loop is out of range")
	}
	a.MovReg32(arm64.X0, arm64.X1)
	a.Ret()

	if metrics != nil {
		metrics.FrameBytes = 0
		metrics.PostRARewrites += uint32(len(plan.Machine.Insts))
	}
	return a.B, internalOffset, nil
}
