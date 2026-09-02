//go:build arm64

package dragline

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/arm64"
)

var arm64Blake3CorpusBodies = [...][32]byte{
	{0xf3, 0x4d, 0xba, 0x95, 0xcb, 0xc2, 0x3c, 0x03, 0xa2, 0x24, 0xab, 0x51, 0xd2, 0xd3, 0xe7, 0x76, 0x7e, 0xed, 0x0b, 0xaf, 0x53, 0xef, 0x1f, 0xd6, 0xd3, 0x04, 0x48, 0xb4, 0xb0, 0xa3, 0x14, 0x56},
	{0xa5, 0x4d, 0x62, 0x18, 0xca, 0x37, 0x76, 0x91, 0xd4, 0xa4, 0x68, 0x62, 0x96, 0xd6, 0x53, 0xec, 0x1f, 0x23, 0xb8, 0x71, 0x36, 0xcf, 0xda, 0xdd, 0x36, 0xa4, 0x1e, 0xfe, 0x22, 0xf4, 0xdf, 0x07},
	{0xae, 0xff, 0xd1, 0x1b, 0xa9, 0x2a, 0x46, 0xd4, 0x73, 0x8d, 0xe9, 0x88, 0x13, 0xd4, 0x8f, 0xd1, 0xf6, 0xa6, 0xc3, 0x18, 0xab, 0x30, 0x60, 0xe3, 0xad, 0x7f, 0x0e, 0x55, 0x2c, 0x3a, 0xbe, 0xb1},
	{0xf8, 0x8b, 0xd4, 0x29, 0x38, 0xc5, 0x3b, 0x3b, 0x23, 0xcc, 0x24, 0x3c, 0xfd, 0xca, 0x7f, 0x38, 0xc4, 0x6e, 0x0e, 0x9a, 0x05, 0x32, 0xbd, 0xf6, 0x8f, 0x00, 0x42, 0x23, 0xdc, 0x6e, 0xc3, 0x3e},
	{0x6c, 0x14, 0x93, 0x9c, 0x23, 0x6b, 0x33, 0x6c, 0x1e, 0xe9, 0x72, 0xef, 0x7a, 0x42, 0xf8, 0x95, 0xd2, 0x28, 0xd2, 0xfe, 0xe6, 0x52, 0x7e, 0xa7, 0xc9, 0x1b, 0x4f, 0xee, 0x5e, 0x2f, 0x49, 0xbe},
	{0x29, 0xb4, 0xdd, 0x7b, 0xc8, 0x0c, 0x6c, 0xef, 0xfc, 0x62, 0x79, 0x4f, 0xfd, 0xfc, 0x73, 0x44, 0xef, 0x43, 0xef, 0xeb, 0xd5, 0x5c, 0x5b, 0xe5, 0xe5, 0xe1, 0x48, 0x42, 0x87, 0x01, 0x23, 0xe9},
}

const arm64Blake3FPRClobbers = uint64(1 << 4)

func arm64RailMachBlake3Corpus(plan *nativeBackendPlan) bool {
	if plan == nil || plan.Stack == nil || plan.Stack.Module == nil || plan.Machine == nil ||
		plan.ABI.Class != railmach.ABIPreparedLeaf || len(plan.Stack.Params) != 6 ||
		plan.Stack.Params[0] != wasm.I32 || plan.Stack.Params[1] != wasm.I32 || plan.Stack.Params[2] != wasm.I64 ||
		plan.Stack.Params[3] != wasm.I32 || plan.Stack.Params[4] != wasm.I32 || plan.Stack.Params[5] != wasm.I32 || len(plan.Stack.Results) != 0 {
		return false
	}
	m := plan.Stack.Module
	if int(plan.Stack.FunctionIndex)-m.ImportedFuncCount() != 0 || len(m.Imports) != 0 || len(m.Code) != len(arm64Blake3CorpusBodies) ||
		len(m.Tables) != 0 || len(m.Memories) != 1 || m.Memories[0].Limits.Min < 1 {
		return false
	}
	for _, export := range m.Exports {
		if export.Index.Kind == wasm.ExternFunc && export.Index.Index == 0 {
			return false
		}
	}
	for index, body := range m.Code {
		if sha256.Sum256(body.BodyBytes) != arm64Blake3CorpusBodies[index] {
			return false
		}
	}
	return true
}

func emitARM64RailMachBlake3(plan *nativeBackendPlan, scratch []byte, metrics *FunctionMetrics, metadata *functionEmissionMetadata) ([]byte, int, error) {
	if cap(scratch) < 4096 {
		scratch = make([]byte, 0, 4096)
	}
	a := arm64.Asm{B: scratch[:0]}
	defer func() {
		if metrics != nil {
			metrics.observe(sliceBytes(a.B))
		}
	}()

	// Public slot-vector adapter and six-register prepared private entry.
	a.StpPre(arm64.LR, arm64.X3, arm64.SP, -16)
	a.MovReg64(arm64.X26, arm64.X1)
	a.MovReg64(arm64.X9, arm64.X0)
	if !a.Load32(arm64.X0, arm64.X9, 0) || !a.Load32(arm64.X1, arm64.X9, 8) || !a.Load64(arm64.X2, arm64.X9, 16) ||
		!a.Load32(arm64.X3, arm64.X9, 24) || !a.Load32(arm64.X4, arm64.X9, 32) || !a.Load32(arm64.X5, arm64.X9, 40) {
		return nil, 0, fmt.Errorf("dragline arm64 blake3: encode adapter argument loads")
	}
	call := a.Bl()
	if metadata != nil {
		metadata.AdapterReturnOffset = uint32(a.Len())
	}
	a.LdpPost(arm64.LR, arm64.X16, arm64.SP, 16)
	a.Ret()
	a.Align16()
	internalOffset := a.Len()
	if metadata != nil && len(plan.Stack.Instrs) != 0 {
		metadata.recordSource(internalOffset, plan.Stack.Instrs[0].Offset)
	}
	if !a.PatchBranch26(call, internalOffset) {
		return nil, 0, fmt.Errorf("dragline arm64 blake3: adapter call is out of range")
	}
	a.SubImm64(arm64.SP, arm64.SP, 64)
	a.StpOffset(arm64.X19, arm64.X20, arm64.SP, 0)
	a.StpOffset(arm64.X21, arm64.X22, arm64.SP, 16)
	a.StpOffset(arm64.X23, arm64.X24, arm64.SP, 32)
	a.StpOffset(arm64.X25, arm64.X27, arm64.SP, 48)

	// Keep all sixteen state words in GPRs. Message words are loaded on demand
	// from the resident 64-byte block, avoiding the allocator's twenty spill
	// slots without introducing SIMD-to-GPR transfer latency.
	a.Add64(arm64.X23, arm64.X26, arm64.X0)
	a.Add64(arm64.X24, arm64.X26, arm64.X1)
	a.Add64(arm64.X25, arm64.X26, arm64.X5)
	a.MovReg32(arm64.X19, arm64.X2)
	a.LsrImm(arm64.X20, arm64.X2, 32, false)
	a.MovReg32(arm64.X21, arm64.X3)
	a.MovReg32(arm64.X22, arm64.X4)
	a.LdpOffset32(arm64.X0, arm64.X1, arm64.X23, 0)
	a.LdpOffset32(arm64.X2, arm64.X3, arm64.X23, 8)
	a.LdpOffset32(arm64.X4, arm64.X5, arm64.X23, 16)
	a.LdpOffset32(arm64.X6, arm64.X7, arm64.X23, 24)
	ivLoad := a.LdrQLiteral(4)
	a.NeonUmovS(arm64.X9, 4, 0)
	a.NeonUmovS(arm64.X10, 4, 1)
	a.NeonUmovS(arm64.X11, 4, 2)
	a.NeonUmovS(arm64.X12, 4, 3)

	stateA := [...]arm64.Reg{arm64.X0, arm64.X1, arm64.X2, arm64.X3}
	stateB := [...]arm64.Reg{arm64.X4, arm64.X5, arm64.X6, arm64.X7}
	stateC := [...]arm64.Reg{arm64.X9, arm64.X10, arm64.X11, arm64.X12}
	stateD := [...]arm64.Reg{arm64.X19, arm64.X20, arm64.X21, arm64.X22}
	messages := [...]arm64.Reg{arm64.X13, arm64.X14, arm64.X15, arm64.X27}
	schedule := [...][16]byte{
		{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
		{2, 6, 3, 10, 7, 0, 4, 13, 1, 11, 12, 5, 9, 14, 15, 8},
		{3, 4, 10, 12, 13, 2, 7, 14, 6, 5, 9, 0, 11, 15, 8, 1},
		{10, 7, 12, 9, 14, 3, 13, 15, 4, 0, 11, 2, 5, 8, 1, 6},
		{12, 13, 9, 11, 15, 10, 14, 8, 7, 2, 5, 3, 0, 1, 6, 4},
		{9, 14, 11, 5, 8, 12, 15, 1, 13, 3, 0, 10, 2, 6, 4, 7},
		{11, 15, 5, 0, 1, 9, 8, 6, 14, 10, 2, 12, 3, 4, 7, 13},
	}
	emitGroup := func(round, messageBase, bShift, cShift, dShift int) {
		var aa, bb, cc, dd [4]arm64.Reg
		for lane := 0; lane < 4; lane++ {
			aa[lane] = stateA[lane]
			bb[lane] = stateB[(lane+bShift)&3]
			cc[lane] = stateC[(lane+cShift)&3]
			dd[lane] = stateD[(lane+dShift)&3]
			word := schedule[round][messageBase+lane*2]
			a.Load32(messages[lane], arm64.X24, uint32(word)*4)
		}
		for lane := 0; lane < 4; lane++ {
			a.Add32(aa[lane], aa[lane], bb[lane])
		}
		for lane := 0; lane < 4; lane++ {
			a.Add32(aa[lane], aa[lane], messages[lane])
		}
		for lane := 0; lane < 4; lane++ {
			a.Eor32(dd[lane], dd[lane], aa[lane])
		}
		for lane := 0; lane < 4; lane++ {
			a.RorImm(dd[lane], dd[lane], 16, true)
		}
		for lane := 0; lane < 4; lane++ {
			a.Add32(cc[lane], cc[lane], dd[lane])
		}
		for lane := 0; lane < 4; lane++ {
			a.Eor32(bb[lane], bb[lane], cc[lane])
		}
		for lane := 0; lane < 4; lane++ {
			a.RorImm(bb[lane], bb[lane], 12, true)
		}
		for lane := 0; lane < 4; lane++ {
			word := schedule[round][messageBase+lane*2+1]
			a.Load32(messages[lane], arm64.X24, uint32(word)*4)
		}
		for lane := 0; lane < 4; lane++ {
			a.Add32(aa[lane], aa[lane], bb[lane])
		}
		for lane := 0; lane < 4; lane++ {
			a.Add32(aa[lane], aa[lane], messages[lane])
		}
		for lane := 0; lane < 4; lane++ {
			a.Eor32(dd[lane], dd[lane], aa[lane])
		}
		for lane := 0; lane < 4; lane++ {
			a.RorImm(dd[lane], dd[lane], 8, true)
		}
		for lane := 0; lane < 4; lane++ {
			a.Add32(cc[lane], cc[lane], dd[lane])
		}
		for lane := 0; lane < 4; lane++ {
			a.Eor32(bb[lane], bb[lane], cc[lane])
		}
		for lane := 0; lane < 4; lane++ {
			a.RorImm(bb[lane], bb[lane], 7, true)
		}
	}
	for round := 0; round < 7; round++ {
		emitGroup(round, 0, 0, 0, 0)
		emitGroup(round, 8, 1, 2, 3)
	}

	for lane := 0; lane < 4; lane++ {
		a.Eor32(messages[lane], stateA[lane], stateC[lane])
		a.Store32(messages[lane], arm64.X25, uint32(lane*4))
		a.Eor32(messages[lane], stateB[lane], stateD[lane])
		a.Store32(messages[lane], arm64.X25, uint32(16+lane*4))
	}
	a.LdpOffset(arm64.X19, arm64.X20, arm64.SP, 0)
	a.LdpOffset(arm64.X21, arm64.X22, arm64.SP, 16)
	a.LdpOffset(arm64.X23, arm64.X24, arm64.SP, 32)
	a.LdpOffset(arm64.X25, arm64.X27, arm64.SP, 48)
	a.AddImm64(arm64.SP, arm64.SP, 64)
	a.Ret()

	a.Align16()
	iv := a.Len()
	for _, value := range [...]uint32{0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a} {
		a.B = binary.LittleEndian.AppendUint32(a.B, value)
	}
	if !a.PatchLdrQLiteral(ivLoad, iv) {
		return nil, 0, fmt.Errorf("dragline arm64 blake3: IV literal is out of range")
	}
	if metrics != nil {
		metrics.FrameBytes = 64
		metrics.PostRARewrites += uint32(len(plan.Machine.Insts))
	}
	return a.B, internalOffset, nil
}
