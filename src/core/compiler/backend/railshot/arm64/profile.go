//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"sort"
)

const profileEnabled = jitprofile.Enabled

func recordProfileRegions(ms *ModuleStats, entry []int, imports, codeSize, functionsEnd, sharedBytes int) {
	if !diagnosticsEnabled || ms == nil {
		return
	}
	layouts := make([]shared.ProfileFunctionLayout, 0, len(ms.Funcs))
	for i, f := range ms.Funcs {
		if f != nil {
			layouts = append(layouts, shared.ProfileFunctionLayout{Entry: entry[i], Index: imports + f.FuncIdx, Name: f.Name, Size: f.NativeSize})
		}
	}
	var islands []jitprofile.Region
	if sharedBytes > 0 {
		islands = append(islands, jitprofile.Region{Offset: uint64(functionsEnd), Size: uint64(sharedBytes), Kind: "shared-adapter", Function: -1})
	}

	ms.ProfileRegions = shared.ProfileRegions(layouts, islands, codeSize)
	for i, f := range ms.Funcs {
		if f == nil || f.NativeSize.TotalBytes == 0 {
			continue
		}
		frameBase := uint32(len(ms.SourceFrames))
		for _, frame := range f.SourceFrames {
			if frame.Parent != 0 {
				frame.Parent += frameBase
			}
			ms.SourceFrames = append(ms.SourceFrames, frame)
		}
		bodyStart := f.NativeSize.HostAdapterBytes + f.NativeSize.AdapterToInternalPaddingBytes
		bodySize := f.NativeSize.InternalFunctionBytes - f.NativeSize.LiteralPoolBytes - f.NativeSize.SharedTrapBodyBytes
		for _, site := range f.CodeSites {
			if site.Offset < uint64(f.SourceInternalOffset) {
				continue
			}
			rel := site.Offset - uint64(f.SourceInternalOffset)
			if bodySize < 0 || rel > uint64(bodySize) || site.Size > uint64(bodySize)-rel {
				continue
			}
			site.Offset = uint64(entry[i]+bodyStart) + rel
			ms.CodeSites = append(ms.CodeSites, site)
		}
		for _, r := range f.SourceRanges {
			if r.Offset < uint64(f.SourceInternalOffset) {
				continue
			}
			rel := r.Offset - uint64(f.SourceInternalOffset)
			if bodySize < 0 || rel > uint64(bodySize) || r.Size > uint64(bodySize)-rel {
				continue
			}
			r.Offset = uint64(entry[i]+bodyStart) + rel
			if r.InlineParent != 0 {
				r.InlineParent += frameBase
			}
			ms.SourceRanges = append(ms.SourceRanges, r)
		}
	}
	sort.Slice(ms.SourceRanges, func(i, j int) bool { return ms.SourceRanges[i].Offset < ms.SourceRanges[j].Offset })
	sort.Slice(ms.CodeSites, func(i, j int) bool { return ms.CodeSites[i].Offset < ms.CodeSites[j].Offset })
}

// Capture original check branches before shared trap lowering repurposes the
// scratch site table. Finalization remaps these ranges with the executable bytes.
func (f *fn) collectProfileSources(internalOff int) {
	if f.stats == nil || !f.stats.RecordSources {
		return
	}
	f.stats.SourceRanges = nil
	f.stats.SourceFrames = f.profileInlineFrames()
	f.stats.SourceInternalOffset = internalOff
	f.stats.CodeSites = f.profileCodeSites()
	imports := f.m.ImportedFuncCount()
	for trapCode, sites := range f.scratchState().trapSites {
		if trapCode != trapMemOOB && trapCode != trapUnreachable && trapCode != trapDivZero && trapCode != trapDivOverflow {
			continue
		}
		for _, site := range sites {
			function, originPC, deferred := f.profileTrapOrigin(int(site.branch))
			if deferred {
				site.function = function
				site.pc = originPC
			}
			if (trapCode == trapDivZero || trapCode == trapDivOverflow) && !deferred {
				continue
			}
			if site.pc == 0 {
				continue
			}
			idx := int(site.function) - imports
			if idx < 0 || idx >= len(f.m.Code) {
				continue
			}
			body := f.m.Code[idx]
			if site.pc < body.LocalDeclBytes {
				continue
			}
			pc := site.pc - body.LocalDeclBytes
			if uint64(pc) >= uint64(len(body.BodyBytes)) {
				continue
			}
			op := body.BodyBytes[pc]
			if (trapCode == trapDivZero || trapCode == trapDivOverflow) && !(op >= 0x6d && op <= 0x70 || op >= 0x7f && op <= 0x82) {
				continue
			}
			// Only publish checks with verified opcode provenance. Deferred nodes
			// retain their origin independently of the eventual emission context.
			if trapCode == trapMemOOB && (op < 0x28 || op > 0x3e) || trapCode == trapUnreachable && op != 0 {
				continue
			}

			start := int(site.branch &^ 1)
			size := 4
			if start < internalOff || start < 0 || start+size > len(f.a.B) {
				continue
			}
			f.stats.SourceRanges = append(f.stats.SourceRanges, shared.NativeSourceRange{Offset: uint64(start), Size: uint64(size), Function: site.function, WasmOffset: site.pc, InlineParent: f.profileTrapCaller(int(site.branch))})
		}
	}
	sort.Slice(f.stats.SourceRanges, func(i, j int) bool { return f.stats.SourceRanges[i].Offset < f.stats.SourceRanges[j].Offset })
	f.stats.SourceRanges = shared.OverlayNativeSources(f.profileEmissionRanges(), f.stats.SourceRanges)
}
