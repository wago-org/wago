//go:build amd64

package amd64

import (
	"bytes"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	a64 "github.com/wago-org/wago/src/core/encoder/amd64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const sharedAdapterTailJumpBytesAMD64 = 5

type adapterTailInfo struct {
	function  uint32
	returnOff uint32
	endOff    uint32
	group     uint32
}

func (f *fn) adapterTailInfo() adapterTailInfo {
	if f.adapterReturnReferenced || f.adapterReturnOff == 0 || f.adapterEndOff <= f.adapterReturnOff {
		return adapterTailInfo{}
	}
	return adapterTailInfo{returnOff: uint32(f.adapterReturnOff), endOff: uint32(f.adapterEndOff)}
}

type adapterTailKey struct {
	hash uint64
	len  int
}

type adapterTailGroup struct {
	templateOff int
	length      int
	count       int
	sharedOff   int
}

func shareAdapterTailsCodeBufferAMD64(codeBuffer *coreruntime.CodeBuffer, entry, internalEntry []int, relocs [][]callReloc, literalWords []uint64, literalOffsets []uint32, infos []adapterTailInfo, roots *shared.GCModuleFrameRootPlan, ms *ModuleStats) (int, error) {
	oldLen := len(codeBuffer.Bytes())
	groups, infos, sharedBytes := planSharedAdapterTailsAMD64(codeBuffer.Bytes(), entry, infos)
	if sharedBytes == 0 || !adapterTailIslandInRangeAMD64(oldLen, sharedBytes) {
		return 0, nil
	}
	if _, err := codeBuffer.AppendSpace(sharedBytes); err != nil {
		return 0, fmt.Errorf("amd64: grow shared adapter tail island: %w", err)
	}
	code := codeBuffer.Bytes()
	newLen, err := compactSharedAdapterTailsAMD64(code, oldLen, entry, internalEntry, relocs, literalWords, literalOffsets, roots, ms, groups, infos, sharedBytes)
	if err != nil {
		return 0, err
	}
	if err := codeBuffer.Truncate(newLen); err != nil {
		return 0, fmt.Errorf("amd64: truncate shared adapter tails: %w", err)
	}
	return sharedBytes, nil
}

func shareAdapterTailsAMD64(code []byte, entry, internalEntry []int, relocs [][]callReloc, literalWords []uint64, literalOffsets []uint32, infos []adapterTailInfo, roots *shared.GCModuleFrameRootPlan, ms *ModuleStats) ([]byte, int, error) {
	oldLen := len(code)
	groups, infos, sharedBytes := planSharedAdapterTailsAMD64(code, entry, infos)
	if sharedBytes == 0 || !adapterTailIslandInRangeAMD64(oldLen, sharedBytes) {
		return code, 0, nil
	}
	code = append(code, make([]byte, sharedBytes)...)
	newLen, err := compactSharedAdapterTailsAMD64(code, oldLen, entry, internalEntry, relocs, literalWords, literalOffsets, roots, ms, groups, infos, sharedBytes)
	if err != nil {
		return nil, 0, err
	}
	return code[:newLen], sharedBytes, nil
}

func adapterTailIslandInRangeAMD64(codeBytes, sharedBytes int) bool {
	return codeBytes >= 0 && sharedBytes >= 0 && codeBytes <= (1<<31-1)-sharedBytes
}

func planSharedAdapterTailsAMD64(code []byte, entry []int, infos []adapterTailInfo) ([]adapterTailGroup, []adapterTailInfo, int) {
	groups := make([]adapterTailGroup, 0, 16)
	byKey := make(map[adapterTailKey][]int, 16)
	for infoIndex := range infos {
		info := &infos[infoIndex]
		info.group = ^uint32(0)
		i := int(info.function)
		if info.returnOff == 0 || info.endOff-info.returnOff <= sharedAdapterTailJumpBytesAMD64 || i >= len(entry) {
			continue
		}
		start, end := entry[i]+int(info.returnOff), entry[i]+int(info.endOff)
		if start < 0 || end > len(code) || start >= end {
			continue
		}
		// This slice is emitted by emitRegABI from register/memory moves, POP and
		// RET only. Any internal code that embeds adapterReturnOff is excluded by
		// adapterReturnReferenced; opaque or plugin bytes never enter the wrapper.
		tail := code[start:end]
		key := adapterTailKey{hash: shared.AdapterShapeHash(tail, -1, 0), len: len(tail)}
		group := -1
		for _, candidate := range byKey[key] {
			g := &groups[candidate]
			if bytes.Equal(tail, code[g.templateOff:g.templateOff+g.length]) {
				group = candidate
				break
			}
		}
		if group < 0 {
			group = len(groups)
			groups = append(groups, adapterTailGroup{templateOff: start, length: len(tail)})
			byKey[key] = append(byKey[key], group)
		}
		groups[group].count++
		info.group = uint32(group)
	}

	sharedBytes := 0
	admitted := make([]bool, len(groups))
	for i := range groups {
		g := &groups[i]
		if g.count*g.length <= g.count*sharedAdapterTailJumpBytesAMD64+g.length {
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

func compactSharedAdapterTailsAMD64(code []byte, oldLen int, entry, internalEntry []int, relocs [][]callReloc, literalWords []uint64, literalOffsets []uint32, roots *shared.GCModuleFrameRootPlan, ms *ModuleStats, groups []adapterTailGroup, infos []adapterTailInfo, sharedBytes int) (int, error) {
	for i := range groups {
		g := &groups[i]
		if g.count*g.length <= g.count*sharedAdapterTailJumpBytesAMD64+g.length {
			continue
		}
		copy(code[oldLen+g.sharedOff:], code[g.templateOff:g.templateOff+g.length])
	}

	src, dst, removed := 0, 0, 0
	infoIndex := 0
	for i := range entry {
		oldEntry, oldInternal := entry[i], internalEntry[i]
		entry[i] = oldEntry - removed
		var info *adapterTailInfo
		if infoIndex < len(infos) && int(infos[infoIndex].function) == i {
			info = &infos[infoIndex]
			infoIndex++
		}
		if info != nil {
			branch := oldEntry + int(info.returnOff)
			end := oldEntry + int(info.endOff)
			keepEnd := branch + sharedAdapterTailJumpBytesAMD64
			copy(code[dst:], code[src:keepEnd])
			dst += keepEnd - src
			src = end
			deleted := end - keepEnd
			removed += deleted
			for j := range relocs[i] {
				if relocs[i][j].at >= info.endOff {
					relocs[i][j].at -= uint32(deleted)
				}
			}
			remapModuleLiteralPlanAMD64(literalWords, literalOffsets, i, int(info.endOff), deleted)
			if roots != nil {
				if plan := roots.Function(i); plan != nil {
					plan.ShiftCallsiteReturnOffsets(info.endOff, uint32(deleted))
				}
			}
			if ms != nil && i < len(ms.Funcs) && ms.Funcs[i] != nil {
				native := &ms.Funcs[i].NativeSize
				native.TotalBytes -= deleted
				native.HostAdapterBytes -= deleted
				native.HostAdapterTailBytes = sharedAdapterTailJumpBytesAMD64
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
	for _, info := range infos {
		i := int(info.function)
		branch := entry[i] + int(info.returnOff)
		target := dst + groups[info.group].sharedOff
		code[branch] = 0xe9
		asm.PatchRel32(branch+1, target)
	}
	for _, info := range infos {
		i := int(info.function)
		returnOff := int(info.returnOff)
		asm.PatchRel32(entry[i]+returnOff-4, internalEntry[i])
		if ms != nil && i < len(ms.Funcs) && ms.Funcs[i] != nil {
			native := &ms.Funcs[i].NativeSize
			native.HostAdapterShapeHash = shared.AdapterShapeHash(code[entry[i]:entry[i]+native.HostAdapterBytes], returnOff-4, 4)
			native.HostAdapterTailShapeHash = shared.AdapterShapeHash(code[entry[i]+returnOff:entry[i]+returnOff+sharedAdapterTailJumpBytesAMD64], -1, 0)
		}
	}
	return newLen, nil
}
