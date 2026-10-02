//go:build amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const (
	sharedAdapterThunkBytesAMD64       = 10
	legacySharedAdapterThunkBytesAMD64 = 12
	sharedAdapterStackDeltaPrefixAMD64 = 11
	sharedAdapterCallShrinkAMD64       = 3 // CALL rel32 (5) -> CALL RBP (2)
)

type sharedAdapterInfo struct {
	function uint32
	dispOff  uint32
	endOff   uint32
	group    uint32
}

func (f *fn) sharedAdapterInfo() sharedAdapterInfo {
	if f.moduleEH || f.adapterReturnReferenced || f.adapterReturnOff < 5 || f.adapterEndOff <= f.adapterReturnOff {
		return sharedAdapterInfo{}
	}
	return sharedAdapterInfo{dispOff: uint32(f.adapterReturnOff - 4), endOff: uint32(f.adapterEndOff)}
}

type sharedAdapterKey struct {
	hash    uint64
	length  int
	dispOff int
}

type sharedAdapterGroup struct {
	templateOff int
	length      int
	dispOff     int
	count       int
	sharedOff   int
	stackDelta  bool
}

func (g *sharedAdapterGroup) thunkBytes() int {
	if g.stackDelta {
		return sharedAdapterThunkBytesAMD64
	}
	return legacySharedAdapterThunkBytesAMD64
}

func (g *sharedAdapterGroup) prefixBytes() int {
	if g.stackDelta {
		return sharedAdapterStackDeltaPrefixAMD64
	}
	return 0
}

func (g *sharedAdapterGroup) sharedLength() int {
	return g.prefixBytes() + g.length - sharedAdapterCallShrinkAMD64
}

func shareAdaptersCodeBufferAMD64(codeBuffer *coreruntime.CodeBuffer, entry, internalEntry []int, relocs [][]callReloc, literalWords []uint64, literalOffsets []uint32, infos []sharedAdapterInfo, roots *shared.GCModuleFrameRootPlan, ms *ModuleStats) (int, error) {
	oldLen := len(codeBuffer.Bytes())
	groups, infos, sharedBytes := planSharedAdaptersAMD64(codeBuffer.Bytes(), entry, infos)
	if sharedBytes == 0 || !adapterTailIslandInRangeAMD64(oldLen, sharedBytes) {
		return 0, nil
	}
	if _, err := codeBuffer.AppendSpace(sharedBytes); err != nil {
		return 0, fmt.Errorf("amd64: grow shared adapter island: %w", err)
	}
	code := codeBuffer.Bytes()
	newLen, err := compactSharedAdaptersAMD64(code, oldLen, entry, internalEntry, relocs, literalWords, literalOffsets, roots, ms, groups, infos, sharedBytes)
	if err != nil {
		return 0, err
	}
	if err := codeBuffer.Truncate(newLen); err != nil {
		return 0, fmt.Errorf("amd64: truncate shared adapters: %w", err)
	}
	return sharedBytes, nil
}

func shareAdaptersAMD64(code []byte, entry, internalEntry []int, relocs [][]callReloc, literalWords []uint64, literalOffsets []uint32, infos []sharedAdapterInfo, roots *shared.GCModuleFrameRootPlan, ms *ModuleStats) ([]byte, int, error) {
	oldLen := len(code)
	groups, infos, sharedBytes := planSharedAdaptersAMD64(code, entry, infos)
	if sharedBytes == 0 || !adapterTailIslandInRangeAMD64(oldLen, sharedBytes) {
		return code, 0, nil
	}
	code = append(code, make([]byte, sharedBytes)...)
	newLen, err := compactSharedAdaptersAMD64(code, oldLen, entry, internalEntry, relocs, literalWords, literalOffsets, roots, ms, groups, infos, sharedBytes)
	if err != nil {
		return nil, 0, err
	}
	return code[:newLen], sharedBytes, nil
}

func planSharedAdaptersAMD64(code []byte, entry []int, infos []sharedAdapterInfo) ([]sharedAdapterGroup, []sharedAdapterInfo, int) {
	groups := make([]sharedAdapterGroup, 0, 16)
	byKey := make(map[sharedAdapterKey][]int, 16)
	for infoIndex := range infos {
		info := &infos[infoIndex]
		info.group = ^uint32(0)
		i := int(info.function)
		dispOff := int(info.dispOff)
		if info.endOff <= legacySharedAdapterThunkBytesAMD64 || dispOff < 1 || dispOff+4 > int(info.endOff) || i >= len(entry) {
			continue
		}
		start, end := entry[i], entry[i]+int(info.endOff)
		if start < 0 || end > len(code) || start >= end || code[start+dispOff-1] != 0xe8 {
			continue
		}
		adapter := code[start:end]
		key := sharedAdapterKey{hash: shared.AdapterShapeHash(adapter, dispOff, 4), length: len(adapter), dispOff: dispOff}
		group := -1
		for _, candidate := range byKey[key] {
			g := &groups[candidate]
			if equalSharedAdapterAMD64(adapter, code[g.templateOff:g.templateOff+g.length], dispOff) {
				group = candidate
				break
			}
		}
		if group < 0 {
			group = len(groups)
			groups = append(groups, sharedAdapterGroup{templateOff: start, length: len(adapter), dispOff: dispOff})
			byKey[key] = append(byKey[key], group)
		}
		groups[group].count++
		info.group = uint32(group)
	}

	sharedBytes := 0
	admitted := make([]bool, len(groups))
	for i := range groups {
		g := &groups[i]
		legacyLength := g.length - sharedAdapterCallShrinkAMD64
		if g.count*g.length <= g.count*legacySharedAdapterThunkBytesAMD64+legacyLength {
			continue
		}
		// The prefix costs eleven bytes while each thunk saves two, so six
		// thunks are the first exact crossover against the legacy LEA/JMP form.
		g.stackDelta = stackDeltaAdapterThunkEnabled && g.count >= 6
		sharedLength := g.sharedLength()
		admitted[i] = true
		g.sharedOff = sharedBytes
		sharedBytes += sharedLength
	}
	if sharedBytes == 0 {
		return nil, nil, 0
	}
	admittedInfos := infos[:0]
	for _, info := range infos {
		if int(info.group) < len(admitted) && admitted[info.group] {
			admittedInfos = append(admittedInfos, info)
		}
	}
	return groups, admittedInfos, sharedBytes
}

func equalSharedAdapterAMD64(a, b []byte, dispOff int) bool {
	if len(a) != len(b) || dispOff < 1 || dispOff+4 > len(a) {
		return false
	}
	return bytes.Equal(a[:dispOff], b[:dispOff]) && bytes.Equal(a[dispOff+4:], b[dispOff+4:])
}

func copySharedAdapterAMD64(dst, src []byte, dispOff int) {
	opcode := dispOff - 1
	copy(dst, src[:opcode])
	dst[opcode], dst[opcode+1] = 0xff, 0xd5 // CALL RBP
	copy(dst[opcode+2:], src[dispOff+4:])
}

func copyStackDeltaSharedAdapterAMD64(dst, src []byte, dispOff int) {
	// POP consumes the sign-extended delta pushed by the thunk. LEA obtains a
	// fixed address in this shared prefix, and ADD recovers the internal entry.
	copy(dst, []byte{0x5d, 0x48, 0x8d, 0x05, 0, 0, 0, 0, 0x48, 0x01, 0xc5})
	copySharedAdapterAMD64(dst[sharedAdapterStackDeltaPrefixAMD64:], src, dispOff)
}

func compactSharedAdaptersAMD64(code []byte, oldLen int, entry, internalEntry []int, relocs [][]callReloc, literalWords []uint64, literalOffsets []uint32, roots *shared.GCModuleFrameRootPlan, ms *ModuleStats, groups []sharedAdapterGroup, infos []sharedAdapterInfo, sharedBytes int) (int, error) {
	for i := range groups {
		g := &groups[i]
		legacyLength := g.length - sharedAdapterCallShrinkAMD64
		if g.count*g.length <= g.count*legacySharedAdapterThunkBytesAMD64+legacyLength {
			continue
		}
		sharedLength := g.sharedLength()
		dst := code[oldLen+g.sharedOff : oldLen+g.sharedOff+sharedLength]
		if g.stackDelta {
			copyStackDeltaSharedAdapterAMD64(dst, code[g.templateOff:g.templateOff+g.length], g.dispOff)
		} else {
			copySharedAdapterAMD64(dst, code[g.templateOff:g.templateOff+g.length], g.dispOff)
		}
	}

	src, dst, removed := 0, 0, 0
	infoIndex := 0
	for i := range entry {
		oldEntry, oldInternal := entry[i], internalEntry[i]
		entry[i] = oldEntry - removed
		var info *sharedAdapterInfo
		if infoIndex < len(infos) && int(infos[infoIndex].function) == i {
			info = &infos[infoIndex]
			infoIndex++
		}
		if info != nil {
			g := &groups[info.group]
			thunkBytes := g.thunkBytes()
			copy(code[dst:], code[src:oldEntry])
			dst += oldEntry - src
			for j := 0; j < thunkBytes; j++ {
				code[dst+j] = 0
			}
			dst += thunkBytes
			src = oldEntry + int(info.endOff)
			deleted := int(info.endOff) - thunkBytes
			removed += deleted
			for j := range relocs[i] {
				if relocs[i][j].at >= int(info.endOff) {
					relocs[i][j].at -= deleted
				}
			}
			remapModuleLiteralPlanAMD64(literalWords, literalOffsets, i, int(info.endOff), deleted)
			if roots != nil {
				if plan := roots.Function(i); plan != nil {
					for j := range plan.Callsites {
						if plan.Callsites[j].ReturnOffset >= info.endOff {
							plan.Callsites[j].ReturnOffset -= uint32(deleted)
						}
					}
				}
			}
			if ms != nil && i < len(ms.Funcs) && ms.Funcs[i] != nil {
				native := &ms.Funcs[i].NativeSize
				native.TotalBytes -= deleted
				native.HostAdapterBytes = thunkBytes
				native.HostAdapterTailBytes = 0
				ms.Funcs[i].CodeBytes -= deleted
				ms.Funcs[i].GCCodeBytes.Total -= deleted
			}
		}
		internalEntry[i] = oldInternal - removed
	}
	copy(code[dst:], code[src:oldLen])
	dst += oldLen - src
	copy(code[dst:], code[oldLen:oldLen+sharedBytes])
	newLen := dst + sharedBytes

	asm := &encoder.Asm{B: code[:newLen]}
	for _, info := range infos {
		i := int(info.function)
		g := &groups[info.group]
		thunk, sharedAt := entry[i], dst+g.sharedOff
		if g.stackDelta {
			code[thunk] = 0x68 // PUSH sign-extended internal-entry delta.
			anchor := sharedAt + 8
			delta := int64(internalEntry[i]) - int64(anchor)
			if delta < -1<<31 || delta > 1<<31-1 {
				return 0, fmt.Errorf("amd64: shared adapter stack delta out of range: %d", delta)
			}
			binary.LittleEndian.PutUint32(code[thunk+1:thunk+5], uint32(int32(delta)))
			code[thunk+5] = 0xe9 // JMP shared adapter; do not perturb the RSB.
			asm.PatchRel32(thunk+6, sharedAt)
		} else {
			copy(code[thunk:thunk+legacySharedAdapterThunkBytesAMD64], []byte{0x48, 0x8d, 0x2d, 0, 0, 0, 0, 0xe9, 0, 0, 0, 0})
			asm.PatchRel32(thunk+3, internalEntry[i])
			asm.PatchRel32(thunk+8, sharedAt)
		}
		sharedReturn := sharedAt + g.prefixBytes() + (g.dispOff - 1) + 2
		if roots != nil {
			if plan := roots.Function(i); plan != nil && plan.AdapterReturnOffset != 0 {
				plan.AdapterReturnOffset = uint32(sharedReturn - entry[i])
			}
		}
		if ms != nil && i < len(ms.Funcs) && ms.Funcs[i] != nil {
			native := &ms.Funcs[i].NativeSize
			thunkBytes := g.thunkBytes()
			if g.stackDelta {
				native.HostAdapterShapeHash = shared.AdapterShapeHash(code[thunk:thunk+thunkBytes], 1, 9)
			} else {
				native.HostAdapterShapeHash = shared.AdapterShapeHash(code[thunk:thunk+thunkBytes], 3, 9)
			}
			native.HostAdapterTailShapeHash = 0
		}
	}
	return newLen, nil
}

func remapModuleLiteralPlanAMD64(words []uint64, offsets []uint32, function, threshold, deleted int) {
	if deleted == 0 || function < 0 || function+1 >= len(offsets) {
		return
	}
	plan := words[offsets[function]:offsets[function+1]]
	if len(plan) == 0 {
		return
	}
	keyCount := int(plan[0])
	for i := 1 + 3*keyCount; i < len(plan); i++ {
		encoded := plan[i]
		site := int(uint32(encoded >> 32))
		if site >= threshold {
			plan[i] = uint64(uint32(site-deleted))<<32 | uint64(uint32(encoded))
		}
	}
}
