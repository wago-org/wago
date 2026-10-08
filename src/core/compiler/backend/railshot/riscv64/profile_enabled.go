//go:build linux && !tinygo && wago_profile

package riscv64

import (
	"fmt"
	"slices"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const profileEnabled = true

type profileModuleState struct{ profile *profileModuleCapture }
type profileMode struct {
	profileModuleState
	function *profileFunctionCapture
}
type profileModuleCapture struct {
	wantSources bool
	functions   []profileFunctionLayout
	budget      profileMetadataBudget
	shared      shared.ScalarSourceRecorder
	regions     []jitprofile.Region
	sources     []shared.NativeSourceRange
}
type profileFunctionCapture struct {
	module           *profileModuleCapture
	layout           profileFunctionLayout
	source           profileSourceRecorder
	regionStart      int
	kind             profileBodyKind
	shared, finished bool
	err              error
}

func newProfileModule(enabled, sources bool, functions int) profileModuleState {
	if !enabled {
		return profileModuleState{}
	}
	return profileModuleState{profile: &profileModuleCapture{wantSources: sources, functions: make([]profileFunctionLayout, 0, functions)}}
}
func (p profileModuleState) baseProfileMode() profileMode { return profileMode{profileModuleState: p} }
func (p profileMode) moduleProfile() profileModuleState   { return p.profileModuleState }
func (p profileModuleState) newFunctionProfile(fn *wasm.Func, function uint32) profileMode {
	mode := p.baseProfileMode()
	if p.profile != nil {
		mode.function = &profileFunctionCapture{module: p.profile, kind: profileRuntimeHelper, layout: profileFunctionLayout{Function: function, LocalDeclBytes: uint32(fn.LocalDeclBytes), WasmBodyBytes: uint32(len(fn.BodyBytes))}}
	}
	return mode
}

func (p *profileFunctionCapture) region(at int, kind profileBodyKind) {
	if p.err != nil || p.kind == kind {
		return
	}
	p.closeRegion(at)
	p.regionStart, p.kind = at, kind
}
func (p *profileFunctionCapture) closeRegion(at int) {
	if p.err != nil {
		return
	}
	if !profileAlignedSize(at) || at < p.regionStart {
		p.err = fmt.Errorf("rv64: invalid profile region boundary")
		return
	}
	if at == p.regionStart {
		return
	}
	if len(p.layout.BodyRegions) == maxProfileFunctionRanges {
		p.err = fmt.Errorf("rv64: profile body region bound")
		return
	}
	if len(p.layout.BodyRegions) == cap(p.layout.BodyRegions) {
		regions := make([]profileBodyRegion, len(p.layout.BodyRegions), min(maxProfileFunctionRanges, max(8, 2*cap(p.layout.BodyRegions))))
		copy(regions, p.layout.BodyRegions)
		p.layout.BodyRegions = regions
	}
	p.layout.BodyRegions = append(p.layout.BodyRegions, profileBodyRegion{Offset: p.regionStart, Size: at - p.regionStart, Kind: p.kind})
}

func (f *target) profileOrigin(kind profileBodyKind, pc uint32, valid bool) {
	p := f.mode.function
	if p == nil {
		return
	}
	p.region(f.Position(), kind)
	if p.module.wantSources && p.err == nil {
		p.err = p.source.switchOrigin(f.Position(), shared.EmissionOrigin{Function: p.layout.Function, PC: pc, Valid: valid})
	}
}
func (f *target) profileOpcode(pc uint32) { f.profileOrigin(profileGuestBody, pc, true) }
func (f *target) profileCold(pc uint32)   { f.profileOrigin(profileColdTrap, pc, true) }
func (f *target) profileUnknown()         { f.profileOrigin(profileRuntimeHelper, 0, false) }

func (f *target) compileProfileScalar(worker *shared.ScalarState, fn *wasm.Func, summary shared.ScalarSummary, widths []bool, params int) (int, error) {
	p := f.mode.function
	if p == nil {
		return worker.CompileScalar(fn.BodyBytes, summary, widths, params, f)
	}
	p.region(f.Position(), profileGuestBody)
	if !p.module.wantSources {
		return worker.CompileScalar(fn.BodyBytes, summary, widths, params, f)
	}
	p.shared = true
	slots, err := worker.CompileScalarWithSources(fn.BodyBytes, summary, widths, params, f, &p.module.shared, p.layout.Function, p.layout.LocalDeclBytes)
	if err == nil {
		p.layout.Sources = p.module.shared.Ranges()
	}
	return slots, err
}

func (f *target) finishProfileBody() error {
	p := f.mode.function
	if p == nil {
		return nil
	}
	p.closeRegion(f.Position())
	if p.err == nil && p.module.wantSources && !p.shared {
		p.layout.Sources, p.err = p.source.finish(f.Position())
	}
	p.layout.BodyBytes, p.finished = f.Position(), true
	return p.err
}

func (m profileMode) finishProfileFrame(prefix, body, epilogue, total int) error {
	p := m.function
	if p == nil {
		return nil
	}
	if p.err != nil {
		return p.err
	}
	if !p.finished || p.layout.BodyBytes != body {
		return fmt.Errorf("rv64: profile body framing mismatch")
	}
	p.layout.PrefixBytes, p.layout.EpilogueBytes, p.layout.CodeBytes = prefix, epilogue, total
	return p.layout.validate()
}

func (p profileModuleState) retainProfile(info *FunctionInfo) error {
	if p.profile == nil {
		return nil
	}
	fn := info.mode.function
	if fn == nil || fn.module != p.profile {
		return fmt.Errorf("rv64: missing profile function owner")
	}
	layout := fn.layout
	layout.Entry, layout.CodeBytes = info.Entry, info.CodeBytes
	if err := layout.validate(); err != nil {
		return err
	}
	count := len(layout.BodyRegions) + 2
	end := 0
	if n := len(p.profile.functions); n != 0 {
		last := p.profile.functions[n-1]
		end = last.Entry + last.CodeBytes
	}
	if layout.Entry != end {
		count++
	}
	if layout.PrefixBytes+layout.BodyBytes+layout.EpilogueBytes != layout.CodeBytes {
		count++
	}
	if err := p.profile.budget.charge(count, len(layout.Sources)); err != nil {
		return err
	}
	// Shared scratch is reused by the next function. Other body directories are
	// private to this capture and transfer to the module without another copy.
	if fn.shared {
		layout.Sources = slices.Clone(layout.Sources)
	}
	p.profile.functions = append(p.profile.functions, layout)
	info.mode.profileMode = profileMode{}
	return nil
}

func (p profileModuleState) prepareProfile(codeBytes int) error {
	if p.profile == nil {
		return nil
	}
	regions, sources, err := finalizeProfileMetadata(p.profile.functions, codeBytes)
	if err != nil {
		return err
	}
	p.profile.regions, p.profile.sources = regions, sources
	// Finalized directories own all published ranges. Drop compiler-only node
	// and per-function backing before staging the executable image.
	p.profile.shared.Release()
	p.profile.functions = nil
	return nil
}
func (p profileModuleState) installProfileStats(stats *ModuleStats) {
	if p.profile != nil {
		stats.ProfileRegions, stats.SourceRanges = p.profile.regions, p.profile.sources
	}
}
