//go:build arm64

package arm64

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const sharedAdapterThunkBytes = 8

type sharedAdapterInfo struct {
	function uint32
	callOff  uint32
	endOff   uint32
	group    uint32
}

func (f *fn) sharedAdapterInfo() sharedAdapterInfo {
	if f.adapterReturnReferenced || f.adapterReturnOff < 4 || f.adapterEndOff <= f.adapterReturnOff {
		return sharedAdapterInfo{}
	}
	return sharedAdapterInfo{callOff: uint32(f.adapterReturnOff - 4), endOff: uint32(f.adapterEndOff)}
}

type sharedAdapterKey struct {
	hash    uint64
	length  int
	callOff int
}

type sharedAdapterGroup struct {
	templateOff int
	length      int
	callOff     int
	count       int
	sharedOff   int
}

func shareAdaptersCodeBuffer(codeBuffer *coreruntime.CodeBuffer, entry, internalEntry []int, relocs [][]callReloc, infos []sharedAdapterInfo, roots *shared.GCModuleFrameRootPlan, ms *ModuleStats) (int, error) {
	oldLen := len(codeBuffer.Bytes())
	groups, infos, sharedBytes := planSharedAdapters(codeBuffer.Bytes(), entry, infos)
	if sharedBytes == 0 || !adapterTailIslandInRange(oldLen, sharedBytes) {
		return 0, nil
	}
	if _, err := codeBuffer.AppendSpace(sharedBytes); err != nil {
		return 0, fmt.Errorf("arm64: grow shared adapter island: %w", err)
	}
	code := codeBuffer.Bytes()
	newLen, err := compactSharedAdapters(code, oldLen, entry, internalEntry, relocs, roots, ms, groups, infos, sharedBytes)
	if err != nil {
		return 0, err
	}
	if err := codeBuffer.Truncate(newLen); err != nil {
		return 0, fmt.Errorf("arm64: truncate shared adapters: %w", err)
	}
	return sharedBytes, nil
}

func shareAdapters(code []byte, entry, internalEntry []int, relocs [][]callReloc, infos []sharedAdapterInfo, roots *shared.GCModuleFrameRootPlan, ms *ModuleStats) ([]byte, int, error) {
	oldLen := len(code)
	groups, infos, sharedBytes := planSharedAdapters(code, entry, infos)
	if sharedBytes == 0 || !adapterTailIslandInRange(oldLen, sharedBytes) {
		return code, 0, nil
	}
	code = append(code, make([]byte, sharedBytes)...)
	newLen, err := compactSharedAdapters(code, oldLen, entry, internalEntry, relocs, roots, ms, groups, infos, sharedBytes)
	if err != nil {
		return nil, 0, err
	}
	return code[:newLen], sharedBytes, nil
}

func planSharedAdapters(code []byte, entry []int, infos []sharedAdapterInfo) ([]sharedAdapterGroup, []sharedAdapterInfo, int) {
	groups := make([]sharedAdapterGroup, 0, 16)
	byKey := make(map[sharedAdapterKey][]int, 16)
	for infoIndex := range infos {
		info := &infos[infoIndex]
		info.group = ^uint32(0)
		i := int(info.function)
		if info.endOff <= sharedAdapterThunkBytes || info.callOff+4 > info.endOff || i >= len(entry) {
			continue
		}
		start, end := entry[i], entry[i]+int(info.endOff)
		callOff := int(info.callOff)
		if start < 0 || end > len(code) || start >= end || start%4 != 0 || end%4 != 0 || !sharedAdapterPositionIndependent(code[start:end], callOff) {
			continue
		}
		adapter := code[start:end]
		key := sharedAdapterKey{hash: shared.AdapterShapeHash(adapter, callOff, 4), length: len(adapter), callOff: callOff}
		group := -1
		for _, candidate := range byKey[key] {
			g := &groups[candidate]
			if equalSharedAdapter(adapter, code[g.templateOff:g.templateOff+g.length], callOff) {
				group = candidate
				break
			}
		}
		if group < 0 {
			group = len(groups)
			groups = append(groups, sharedAdapterGroup{templateOff: start, length: len(adapter), callOff: callOff})
			byKey[key] = append(byKey[key], group)
		}
		groups[group].count++
		info.group = uint32(group)
	}

	sharedBytes := 0
	admitted := make([]bool, len(groups))
	for i := range groups {
		g := &groups[i]
		if g.count*g.length <= g.count*sharedAdapterThunkBytes+g.length {
			continue
		}
		admitted[i] = true
		g.sharedOff = sharedBytes
		sharedBytes += g.length
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

func sharedAdapterPositionIndependent(adapter []byte, callOff int) bool {
	if len(adapter)&3 != 0 || callOff < 0 || callOff+4 > len(adapter) || callOff&3 != 0 {
		return false
	}
	for off := 0; off < len(adapter); off += 4 {
		word := binary.LittleEndian.Uint32(adapter[off:])
		if off == callOff {
			if word&0xfc000000 != 0x94000000 { // BL imm26
				return false
			}
			continue
		}
		if isPCRelativeWord(word) || word&0x3b000000 == 0x18000000 {
			return false
		}
	}
	return true
}

func equalSharedAdapter(a, b []byte, callOff int) bool {
	if len(a) != len(b) || callOff < 0 || callOff+4 > len(a) {
		return false
	}
	for i := range a {
		if i >= callOff && i < callOff+4 {
			continue
		}
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func compactSharedAdapters(code []byte, oldLen int, entry, internalEntry []int, relocs [][]callReloc, roots *shared.GCModuleFrameRootPlan, ms *ModuleStats, groups []sharedAdapterGroup, infos []sharedAdapterInfo, sharedBytes int) (int, error) {
	for i := range groups {
		g := &groups[i]
		if g.count*g.length <= g.count*sharedAdapterThunkBytes+g.length {
			continue
		}
		copy(code[oldLen+g.sharedOff:], code[g.templateOff:g.templateOff+g.length])
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
			copy(code[dst:], code[src:oldEntry])
			dst += oldEntry - src
			for j := 0; j < sharedAdapterThunkBytes; j++ {
				code[dst+j] = 0
			}
			dst += sharedAdapterThunkBytes
			src = oldEntry + int(info.endOff)
			deleted := int(info.endOff) - sharedAdapterThunkBytes
			removed += deleted
			for j := range relocs[i] {
				if relocs[i][j].at >= info.endOff {
					relocs[i][j].at -= uint32(deleted)
				}
			}
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
				native.HostAdapterBytes = sharedAdapterThunkBytes
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

	asm := &a64.Asm{B: code[:newLen]}
	for i := range groups {
		g := &groups[i]
		if g.count*g.length <= g.count*sharedAdapterThunkBytes+g.length {
			continue
		}
		sharedAt := dst + g.sharedOff
		asm.PatchU32(sharedAt+g.callOff, 0xd63f0220) // BLR X17
	}
	for _, info := range infos {
		i := int(info.function)
		g := &groups[info.group]
		thunk, sharedAt := entry[i], dst+g.sharedOff
		asm.PatchU32(thunk, 0x10000000|uint32(X17)) // ADR X17, internal entry
		if !asm.PatchAdr(thunk, internalEntry[i]) {
			return 0, fmt.Errorf("arm64: shared adapter thunk for function %d exceeds ADR range", i)
		}
		asm.PatchU32(thunk+4, 0x14000000)
		if !asm.PatchBranch26(thunk+4, sharedAt) {
			return 0, fmt.Errorf("arm64: shared adapter branch for function %d exceeds B range", i)
		}
		if roots != nil {
			if plan := roots.Function(i); plan != nil && plan.AdapterReturnOffset != 0 {
				plan.AdapterReturnOffset = uint32(sharedAt + g.callOff + 4 - entry[i])
			}
		}
		if ms != nil && i < len(ms.Funcs) && ms.Funcs[i] != nil {
			native := &ms.Funcs[i].NativeSize
			native.HostAdapterShapeHash = shared.AdapterShapeHash(code[thunk:thunk+sharedAdapterThunkBytes], 0, sharedAdapterThunkBytes)
			native.HostAdapterTailShapeHash = 0
		}
	}
	return newLen, nil
}
