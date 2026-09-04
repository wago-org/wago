//go:build arm64

package arm64

import (
	"bytes"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const sharedAdapterTailBranchBytes = 4

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

func shareAdapterTailsCodeBuffer(codeBuffer *coreruntime.CodeBuffer, entry, internalEntry []int, relocs *callRelocTable, infos []adapterTailInfo, roots *shared.GCModuleFrameRootPlan, ms *ModuleStats) (int, error) {
	oldLen := len(codeBuffer.Bytes())
	groups, infos, sharedBytes := planSharedAdapterTails(codeBuffer.Bytes(), entry, infos)
	if sharedBytes == 0 || !adapterTailIslandInRange(oldLen, sharedBytes) {
		return 0, nil
	}
	if _, err := codeBuffer.AppendSpace(sharedBytes); err != nil {
		return 0, fmt.Errorf("arm64: grow shared adapter tail island: %w", err)
	}
	code := codeBuffer.Bytes()
	newLen, err := compactSharedAdapterTails(code, oldLen, entry, internalEntry, relocs, roots, ms, groups, infos, sharedBytes)
	if err != nil {
		return 0, err
	}
	if err := codeBuffer.Truncate(newLen); err != nil {
		return 0, fmt.Errorf("arm64: truncate shared adapter tails: %w", err)
	}
	return sharedBytes, nil
}

func shareAdapterTails(code []byte, entry, internalEntry []int, relocs *callRelocTable, infos []adapterTailInfo, roots *shared.GCModuleFrameRootPlan, ms *ModuleStats) ([]byte, int, error) {
	oldLen := len(code)
	groups, infos, sharedBytes := planSharedAdapterTails(code, entry, infos)
	if sharedBytes == 0 || !adapterTailIslandInRange(oldLen, sharedBytes) {
		return code, 0, nil
	}
	code = append(code, make([]byte, sharedBytes)...)
	newLen, err := compactSharedAdapterTails(code, oldLen, entry, internalEntry, relocs, roots, ms, groups, infos, sharedBytes)
	if err != nil {
		return nil, 0, err
	}
	return code[:newLen], sharedBytes, nil
}

func adapterTailIslandInRange(codeBytes, sharedBytes int) bool {
	// The island is placed at the end. Conservatively retain local tails once
	// the complete image can span ARM64 B's signed 26-bit word displacement.
	return codeBytes >= 0 && sharedBytes >= 0 && codeBytes <= (1<<27)-sharedBytes
}

func planSharedAdapterTails(code []byte, entry []int, infos []adapterTailInfo) ([]adapterTailGroup, []adapterTailInfo, int) {
	groups := make([]adapterTailGroup, 0, 16)
	byKey := make(map[adapterTailKey][]int, 16)
	for infoIndex := range infos {
		info := &infos[infoIndex]
		info.group = ^uint32(0)
		i := int(info.function)
		if info.returnOff == 0 || info.endOff-info.returnOff <= sharedAdapterTailBranchBytes || i >= len(entry) {
			continue
		}
		start, end := entry[i]+int(info.returnOff), entry[i]+int(info.endOff)
		if start < 0 || end > len(code) || start >= end || start%4 != 0 || end%4 != 0 {
			continue
		}
		tail := code[start:end]
		if !adapterTailPositionIndependent(tail) {
			continue
		}
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
		if g.count*g.length <= g.count*sharedAdapterTailBranchBytes+g.length {
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

func adapterTailPositionIndependent(tail []byte) bool {
	if len(tail)&3 != 0 {
		return false
	}
	for off := 0; off < len(tail); off += 4 {
		word := rdWord(tail, off)
		if isPCRelativeWord(word) || word&0x3b000000 == 0x18000000 {
			return false
		}
	}
	return true
}

func compactSharedAdapterTails(code []byte, oldLen int, entry, internalEntry []int, relocs *callRelocTable, roots *shared.GCModuleFrameRootPlan, ms *ModuleStats, groups []adapterTailGroup, infos []adapterTailInfo, sharedBytes int) (int, error) {
	// Save one exact template per admitted group in the appended range before
	// compaction overwrites function-local tails.
	for i := range groups {
		g := &groups[i]
		if g.count*g.length <= g.count*sharedAdapterTailBranchBytes+g.length {
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
		sharedTail := info != nil
		if sharedTail {
			branch := oldEntry + int(info.returnOff)
			end := oldEntry + int(info.endOff)
			keepEnd := branch + sharedAdapterTailBranchBytes
			copy(code[dst:], code[src:keepEnd])
			dst += keepEnd - src
			src = end
			deleted := end - keepEnd
			removed += deleted
			functionRelocs := relocs.serialFunction(i)
			if relocs.results != nil {
				functionRelocs = relocs.parallelFunction(i)
			}
			for j := range functionRelocs {
				if functionRelocs[j].at >= info.endOff {
					functionRelocs[j].at -= uint32(deleted)
				}
			}
			if roots != nil {
				if plan := roots.Function(i); plan != nil {
					plan.ShiftCallsiteReturnOffsets(info.endOff, uint32(deleted))
				}
			}
			if ms != nil && i < len(ms.Funcs) && ms.Funcs[i] != nil {
				native := &ms.Funcs[i].NativeSize
				native.TotalBytes -= deleted
				native.HostAdapterBytes -= deleted
				native.HostAdapterTailBytes = sharedAdapterTailBranchBytes
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
		target := dst + groups[info.group].sharedOff
		branch := entry[i] + int(info.returnOff)
		asm.PatchU32(branch, 0x14000000)
		if !asm.PatchBranch26(branch, target) {
			return 0, fmt.Errorf("arm64: shared adapter tail branch at %#x to %#x exceeds B range", branch, target)
		}
	}
	for _, info := range infos {
		i := int(info.function)
		returnOff := int(info.returnOff)
		call := entry[i] + returnOff - 4
		asm.PatchU32(call, 0x94000000)
		if !asm.PatchBranch26(call, internalEntry[i]) {
			return 0, fmt.Errorf("arm64: adapter call for function %d exceeds BL range", i)
		}
		if ms != nil && i < len(ms.Funcs) && ms.Funcs[i] != nil {
			native := &ms.Funcs[i].NativeSize
			native.HostAdapterShapeHash = shared.AdapterShapeHash(code[entry[i]:entry[i]+native.HostAdapterBytes], returnOff-4, 4)
			native.HostAdapterTailShapeHash = shared.AdapterShapeHash(code[entry[i]+returnOff:entry[i]+returnOff+4], -1, 0)
		}
	}
	return newLen, nil
}
