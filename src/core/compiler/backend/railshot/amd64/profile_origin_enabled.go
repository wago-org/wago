//go:build amd64 && wago_profile

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// Origins live beside existing deferred nodes only in profiling builds.
// Neither node payloads nor emitted runtime trap payloads change.
type profileOrigin struct {
	function, pc, caller uint32
	valid                bool
	node                 bool
}
type profileFnState struct {
	sites          shared.CodeSites
	creationOrigin profileOrigin
	inlineParent   uint32
	inlineFrames   []shared.NativeInlineFrame
	nodes          map[*elem]profileOrigin
	traps          map[int]profileOrigin
	trapBranches   []int
	active         profileOrigin
	emission       shared.SourceEmission
}

func (f *fn) rememberProfileNode(node *elem) {
	if f.nodes == nil {
		f.nodes = make(map[*elem]profileOrigin)
	}
	origin := f.creationOrigin
	if !origin.valid {
		origin = profileOrigin{function: f.traceFuncIdx, pc: f.wasmPC, caller: f.inlineParent, valid: true, node: true}
	}
	f.nodes[node] = origin
}
func (f *fn) enterProfileNode(node *elem) profileOrigin {
	previous := f.active
	f.switchProfileOrigin(f.nodes[node])
	return previous
}
func (f *fn) recordProfileTrap(branch int) {
	origin := f.active
	if !origin.valid || !origin.node {
		if f.inlineParent == 0 {
			return
		}
		origin = profileOrigin{function: f.traceFuncIdx, pc: f.wasmPC, caller: f.inlineParent, valid: true}
	}
	if f.traps == nil {
		f.traps = make(map[int]profileOrigin)
	}
	f.traps[branch] = origin
	f.trapBranches = append(f.trapBranches, branch)
}
func (f *fn) profileTrapOrigin(branch int) (uint32, uint32, bool) {
	origin, ok := f.traps[branch]
	return origin.function, origin.pc, ok && origin.valid
}

func (f *fn) switchProfileOrigin(origin profileOrigin) {
	f.emission.Switch(f.a.Len(), shared.EmissionOrigin{Function: origin.function, PC: origin.pc, InlineParent: origin.caller, Valid: origin.valid})
	f.active = origin
}
func (f *fn) rewindProfileEmission(at int) {
	f.sites.Rewind(at)
	f.emission.Rewind(at)
	for len(f.trapBranches) > 0 && f.trapBranches[len(f.trapBranches)-1] >= at {
		delete(f.traps, f.trapBranches[len(f.trapBranches)-1])
		f.trapBranches = f.trapBranches[:len(f.trapBranches)-1]
	}
}
func (f *fn) profileEmissionRanges() []shared.NativeSourceRange { return f.emission.Ranges() }

func (f *fn) recordProfileCodeSite(start int, kind string) {
	if f.stats != nil && f.stats.RecordSources {
		f.sites.Add(start, f.a.Len(), kind)
	}
}
func (f *fn) profileCodeSites() []shared.NativeCodeSite { return f.sites.Ranges() }

func (f *fn) enterProfileInline() uint32 {
	previous := f.inlineParent
	f.inlineFrames = append(f.inlineFrames, shared.NativeInlineFrame{Function: f.traceFuncIdx, WasmOffset: f.wasmPC, Parent: previous})
	f.inlineParent = uint32(len(f.inlineFrames))
	return previous
}
func (f *fn) leaveProfileInline(previous uint32)              { f.inlineParent = previous }
func (f *fn) profileTrapCaller(branch int) uint32             { return f.traps[branch].caller }
func (f *fn) profileInlineFrames() []shared.NativeInlineFrame { return f.inlineFrames }

// The compact add-immediate inline path bypasses the bytecode driver. Override
// only newly created deferred nodes, never the origin of an operand reused by
// constant folding, and never runtime trap coordinates.
type profileInlineAddState struct {
	parent   uint32
	creation profileOrigin
}

func (f *fn) enterProfileInlineAdd(t *inlineTarget) profileInlineAddState {
	previous := profileInlineAddState{f.inlineParent, f.creationOrigin}
	body := f.m.Code[t.globalIdx-f.m.ImportedFuncCount()]
	if len(body.BodyBytes) >= 2 && body.BodyBytes[len(body.BodyBytes)-2] == 0x6a {
		f.enterProfileInline()
		f.creationOrigin = profileOrigin{function: uint32(t.globalIdx), pc: body.LocalDeclBytes + uint32(len(body.BodyBytes)-2), caller: f.inlineParent, valid: true, node: true}
	}
	return previous
}
func (f *fn) leaveProfileInlineAdd(previous profileInlineAddState) {
	f.inlineParent, f.creationOrigin = previous.parent, previous.creation
}

func (f *fn) enterProfileInstruction() profileOrigin {
	previous := f.active
	f.switchProfileOrigin(profileOrigin{function: f.traceFuncIdx, pc: f.wasmPC, caller: f.inlineParent, valid: true})
	return previous
}
