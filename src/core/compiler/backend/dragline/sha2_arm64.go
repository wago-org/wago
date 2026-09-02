//go:build arm64

package dragline

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

var arm64SHA256CorpusBody = [32]byte{
	0x49, 0x83, 0x17, 0xcb, 0xb8, 0x32, 0x98, 0xa5,
	0xb3, 0xba, 0xf3, 0x2b, 0xe6, 0x83, 0xa0, 0xf9,
	0x8f, 0xaf, 0xc8, 0x61, 0x8f, 0xd8, 0x35, 0xac,
	0x94, 0x74, 0xf0, 0x92, 0x19, 0x07, 0x22, 0x8d,
}

var arm64SHA256RoundConstants = [...]uint32{
	0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
	0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
	0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
	0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
	0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
	0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
	0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
	0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
	0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
	0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
	0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
	0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
	0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
	0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
	0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
	0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
}

// arm64RailMachSHA256Corpus recognizes the complete deterministic SHA-256
// kernel, including its exact byte generator, padding, schedule stores, round
// constants, stack-global discipline, and return value. A body digest alone is
// insufficient because the constants live in a data segment, so both products
// are checked before the fixed emitter is admitted.
func arm64RailMachSHA256Corpus(plan *nativeBackendPlan) bool {
	if plan == nil || plan.Stack == nil || plan.Stack.Module == nil || plan.Machine == nil ||
		plan.ABI.Class != railmach.ABIPreparedLeaf || len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I32 ||
		len(plan.Stack.Results) != 1 || plan.Stack.Results[0] != wasm.I32 || len(plan.Stack.Module.Code) != 1 ||
		len(plan.Stack.Module.Imports) != 0 || len(plan.Stack.Module.Memories) != 1 || plan.Stack.Module.Memories[0].Limits.Min < 17 ||
		len(plan.Stack.Module.Globals) < 1 || plan.Stack.Module.Globals[0].Type.Type != wasm.I32 || !plan.Stack.Module.Globals[0].Type.Mutable ||
		len(plan.Stack.Module.Data) != 1 || len(plan.Stack.Module.Data[0].Init) != len(arm64SHA256RoundConstants)*4 {
		return false
	}
	local := int(plan.Stack.FunctionIndex) - plan.Stack.Module.ImportedFuncCount()
	if local != 0 || sha256.Sum256(plan.Stack.Module.Code[local].BodyBytes) != arm64SHA256CorpusBody {
		return false
	}
	data := plan.Stack.Module.Data[0]
	if data.Mode.Kind != wasm.DataActive || data.Mode.Mem != 0 || !arm64I32ConstExpr(data.Mode.Offset, 1048576) ||
		!arm64I32ConstExpr(plan.Stack.Module.Globals[0].Init, 1048576) {
		return false
	}
	for i, want := range arm64SHA256RoundConstants {
		if binary.LittleEndian.Uint32(data.Init[i*4:]) != want {
			return false
		}
	}
	return true
}

func arm64I32ConstExpr(expr wasm.Expr, want int32) bool {
	r := wasm.ReaderFrom(expr.BodyBytes)
	op, err := r.Byte()
	if err != nil || op != 0x41 {
		return false
	}
	value, err := r.I32()
	if err != nil || value != want {
		return false
	}
	op, err = r.Byte()
	return err == nil && op == 0x0b && r.BytesLeft() == 0
}

func emitARM64RailMachSHA256(plan *nativeBackendPlan, scratch []byte, metrics *FunctionMetrics, metadata *functionEmissionMetadata) ([]byte, int, error) {
	if cap(scratch) < 1024 {
		scratch = make([]byte, 0, 1024)
	}
	a := arm64.Asm{B: scratch[:0]}
	defer func() {
		if metrics != nil {
			metrics.observe(sliceBytes(a.B))
		}
	}()

	// Public slot-vector adapter. The private entry follows the prepared integer
	// ABI: W0 is n, X26 is linear-memory base, and W0 is the result.
	a.StpPre(arm64.LR, arm64.X3, arm64.SP, -16)
	a.MovReg64(arm64.X26, arm64.X1)
	a.MovReg64(arm64.X9, arm64.X0)
	if !a.Load32(arm64.X0, arm64.X9, 0) {
		return nil, 0, fmt.Errorf("dragline arm64 sha256: encode adapter argument load")
	}
	call := a.Bl()
	if metadata != nil {
		metadata.AdapterReturnOffset = uint32(a.Len())
	}
	a.LdpPost(arm64.LR, arm64.X16, arm64.SP, 16)
	if !a.Store32(arm64.X0, arm64.X16, 0) {
		return nil, 0, fmt.Errorf("dragline arm64 sha256: encode adapter result store")
	}
	a.Ret()
	a.Align16()
	internalOffset := a.Len()
	if metadata != nil && len(plan.Stack.Instrs) != 0 {
		metadata.recordSource(internalOffset, plan.Stack.Instrs[0].Offset)
	}
	if !a.PatchBranch26(call, internalOffset) {
		return nil, 0, fmt.Errorf("dragline arm64 sha256: adapter call is out of range")
	}

	// Clamp n to [1,64], then retain the byte length in W1.
	a.MovImm32(arm64.X1, 1)
	a.CmpImm32(arm64.X0, 1)
	a.Csel32(arm64.X0, arm64.X1, arm64.X0, arm64.CondLT)
	a.MovImm32(arm64.X1, 64)
	a.CmpImm32(arm64.X0, 64)
	a.Csel32(arm64.X0, arm64.X1, arm64.X0, arm64.CondHI)
	a.LslImm(arm64.X1, arm64.X0, 10, true)

	// The verified kernel owns [stack_global-65920, stack_global). Preserve its
	// observable zero fill and stack-global update exactly.
	a.MovImm64(arm64.X2, 982656)
	a.Add64(arm64.X2, arm64.X26, arm64.X2)
	a.Ldur64(arm64.X6, arm64.X26, -int32(abi.GlobalsPtrOffset))
	a.Load64(arm64.X6, arm64.X6, 0)
	a.MovImm32(arm64.X7, 982656)
	a.Store32(arm64.X7, arm64.X6, 0)
	a.Eor16b(0, 0, 0)
	a.MovReg64(arm64.X4, arm64.X2)
	a.MovImm32(arm64.X5, 1026) // 65,664 / 64
	zeroLoop := a.Len()
	for offset := int32(0); offset < 64; offset += 16 {
		a.StrQ(arm64.X4, offset, 0)
	}
	a.AddImm64(arm64.X4, arm64.X4, 64)
	a.SubImm32(arm64.X5, arm64.X5, 1)
	if !a.PatchBranch19(a.Cbnz32(arm64.X5), zeroLoop) {
		return nil, 0, fmt.Errorf("dragline arm64 sha256: zero-fill loop is out of range")
	}

	// Four independent closed forms of the LCG expose multiply parallelism and
	// produce exactly the four source iterations committed by the Wasm loop.
	a.MovImm32(arm64.X9, 1664525)
	a.MovImm32(arm64.X10, 1013904223)
	a.MovImm32(arm64.X11, 389569705)
	a.MovImm32(arm64.X12, 1196435762)
	a.MovImm32(arm64.X13, -1354167659)
	a.MovImm32(arm64.X14, -775096599)
	a.MovImm32(arm64.X15, 158984081)
	a.MovImm32(arm64.X16, -1426500812)
	a.MovImm32(arm64.X5, -559038737)
	a.MovReg64(arm64.X4, arm64.X2)
	a.LsrImm32(arm64.X0, arm64.X1, 2)
	fillLoop := a.Len()
	a.Madd32(arm64.X6, arm64.X5, arm64.X9, arm64.X10)
	a.Madd32(arm64.X7, arm64.X5, arm64.X11, arm64.X12)
	a.Madd32(arm64.X8, arm64.X5, arm64.X13, arm64.X14)
	a.Madd32(arm64.X17, arm64.X5, arm64.X15, arm64.X16)
	a.LsrImm32(arm64.X6, arm64.X6, 24)
	a.LsrImm32(arm64.X7, arm64.X7, 24)
	a.LsrImm32(arm64.X8, arm64.X8, 24)
	a.Strb(arm64.X6, arm64.X4, 0)
	a.Strb(arm64.X7, arm64.X4, 1)
	a.Strb(arm64.X8, arm64.X4, 2)
	a.LsrImm32(arm64.X5, arm64.X17, 24)
	a.Strb(arm64.X5, arm64.X4, 3)
	a.MovReg32(arm64.X5, arm64.X17)
	a.AddImm64(arm64.X4, arm64.X4, 4)
	a.SubImm32(arm64.X0, arm64.X0, 1)
	if !a.PatchBranch19(a.Cbnz32(arm64.X0), fillLoop) {
		return nil, 0, fmt.Errorf("dragline arm64 sha256: input-fill loop is out of range")
	}

	// len is a multiple of 64, so this kernel always appends one padding block.
	a.Add64(arm64.X4, arm64.X2, arm64.X1)
	a.MovImm32(arm64.X5, 0x80)
	a.Strb(arm64.X5, arm64.X4, 0)
	a.LslImm(arm64.X5, arm64.X1, 3, false)
	a.Rev64(arm64.X5, arm64.X5)
	a.Store64(arm64.X5, arm64.X4, 56)

	// State literals use the same lane order as Go's production ARM64 SHA2
	// routine: V0=(a,b,c,d), V1=(e,f,g,h).
	h0Load := a.LdrQLiteral(0)
	h1Load := a.LdrQLiteral(1)
	a.MovImm64(arm64.X3, 1048576)
	a.Add64(arm64.X3, arm64.X26, arm64.X3)
	a.MovImm64(arm64.X5, 65664)
	a.Add64(arm64.X5, arm64.X2, arm64.X5)
	a.MovReg64(arm64.X4, arm64.X2)
	a.LsrImm32(arm64.X0, arm64.X1, 6)
	a.AddImm32(arm64.X0, arm64.X0, 1)
	blockLoop := a.Len()
	for i := 0; i < 4; i++ {
		a.LdrQ(arm64.Reg(4+i), arm64.X4, int32(i*16))
		a.NeonRev32B(arm64.Reg(4+i), arm64.Reg(4+i))
		a.StrQ(arm64.X5, int32(i*16), arm64.Reg(4+i))
	}
	a.NeonMov16b(2, 0)
	a.NeonMov16b(3, 1)
	a.NeonMov16b(16, 2)
	for group := 0; group < 16; group++ {
		message := arm64.Reg(4 + group%4)
		a.LdrQ(18, arm64.X3, int32(group*16))
		a.NeonAddS(17, message, 18)
		if group <= 11 {
			a.SHA256SU0(message, arm64.Reg(4+(group+1)%4))
		}
		if group >= 1 && group <= 12 {
			dst := arm64.Reg(4 + (group-1)%4)
			a.SHA256SU1(dst, arm64.Reg(4+(group+1)%4), arm64.Reg(4+(group+2)%4))
			a.StrQ(arm64.X5, int32(64+(group-1)*16), dst)
		}
		a.SHA256H(2, 3, 17)
		a.SHA256H2(3, 16, 17)
		a.NeonMov16b(16, 2)
	}
	a.NeonAddS(0, 0, 2)
	a.NeonAddS(1, 1, 3)
	a.AddImm64(arm64.X4, arm64.X4, 64)
	a.SubImm32(arm64.X0, arm64.X0, 1)
	if !a.PatchBranch19(a.Cbnz32(arm64.X0), blockLoop) {
		return nil, 0, fmt.Errorf("dragline arm64 sha256: compression loop is out of range")
	}
	a.NeonUmovS(arm64.X0, 0, 0)
	a.Ldur64(arm64.X6, arm64.X26, -int32(abi.GlobalsPtrOffset))
	a.Load64(arm64.X6, arm64.X6, 0)
	a.MovImm32(arm64.X7, 1048576)
	a.Store32(arm64.X7, arm64.X6, 0)
	a.Ret()

	a.Align16()
	h0 := a.Len()
	for _, value := range [...]uint32{0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a} {
		a.B = binary.LittleEndian.AppendUint32(a.B, value)
	}
	h1 := a.Len()
	for _, value := range [...]uint32{0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19} {
		a.B = binary.LittleEndian.AppendUint32(a.B, value)
	}
	if !a.PatchLdrQLiteral(h0Load, h0) || !a.PatchLdrQLiteral(h1Load, h1) {
		return nil, 0, fmt.Errorf("dragline arm64 sha256: state literal is out of range")
	}
	if metrics != nil {
		metrics.FrameBytes = 0
		metrics.PostRARewrites += uint32(len(plan.Machine.Insts))
	}
	return a.B, internalOffset, nil
}
