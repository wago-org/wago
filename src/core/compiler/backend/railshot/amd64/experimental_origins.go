//go:build amd64

package amd64

func (f *fn) sourceInstructionPC(offset uint32) uint32 {
	if f.m == nil {
		return f.tracePCBase + offset
	}
	if origins := f.m.ExperimentalInstructionOrigins; origins != nil {
		index := int(f.traceFuncIdx) - f.m.ImportedFuncCount()
		if index >= 0 && index < len(origins) && int(offset) < len(origins[index]) {
			offset = origins[index][offset]
		}
	}
	return f.tracePCBase + offset
}
