package wago

import (
	"fmt"
	"slices"
	"sync/atomic"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime"
)

// instanceCompilerTier owns the stable wrapper-entry indirection for one
// tierable instance. Installation is deliberately one-shot, bounding retained
// executable images while allowing already-running Railshot frames to finish.
type instanceCompilerTier struct {
	arena     *runtime.Arena
	entries   []byte
	thunkMem  []byte
	thunkBase uintptr
	offsets   []uint32
	installed atomic.Pointer[compilerCodeGeneration]
	active    atomic.Uint32
}

// compilerCodeGeneration couples one immutable executable image to the exact
// metadata needed to interpret its native frames. Publish this before any entry
// points at the image so helper calls and collectors can always resolve the
// generation from a return PC.
type compilerCodeGeneration struct {
	compiled *Compiled
	base     uintptr
}

func newInstanceCompilerTier(c *Compiled, railshotBase uintptr) (*instanceCompilerTier, error) {
	if c == nil || len(c.Entry) == 0 {
		return nil, nil
	}
	arena, err := runtime.NewArena(len(c.Entry) * 8)
	if err != nil {
		return nil, err
	}
	t := &instanceCompilerTier{arena: arena, entries: arena.Alloc(len(c.Entry) * 8)}
	for i, off := range c.Entry {
		atomic.StoreUint64((*uint64)(unsafe.Pointer(&t.entries[i*8])), uint64(railshotBase)+uint64(off))
	}
	blob, offsets, err := compilerTierEntryThunks(len(c.Entry))
	if err != nil {
		_ = arena.Close()
		return nil, err
	}
	t.thunkMem, t.thunkBase, err = runtime.MapCode(blob)
	if err != nil {
		_ = arena.Close()
		return nil, err
	}
	t.offsets = offsets
	t.active.Store(uint32(CompilerRailshot))
	return t, nil
}

func (t *instanceCompilerTier) close() {
	if t == nil {
		return
	}
	if generation := t.installed.Swap(nil); generation != nil {
		generation.compiled.releaseCode()
	}
	if t.thunkMem != nil {
		_ = runtime.Unmap(t.thunkMem)
		t.thunkMem = nil
	}
	if t.arena != nil {
		_ = t.arena.Close()
		t.arena = nil
	}
}

func (in *Instance) compilerGenerationForPC(pc uintptr) (*Compiled, uintptr) {
	if in == nil {
		return nil, 0
	}
	if in.profile != nil && in.profile.tier != nil {
		if generation := in.profile.tier.installed.Load(); generation != nil && generation.compiled != nil && pc >= generation.base && pc-generation.base < uintptr(len(generation.compiled.code)) {
			return generation.compiled, generation.base
		}
	}
	if in.c != nil && pc >= in.base && pc-in.base < uintptr(len(in.c.code)) {
		return in.c, in.base
	}
	return nil, 0
}

func (in *Instance) wrapperEntry(local int) uintptr {
	if in != nil && in.profile != nil && in.profile.tier != nil && local >= 0 && local < len(in.profile.tier.offsets) {
		return in.profile.tier.thunkBase + uintptr(in.profile.tier.offsets[local])
	}
	return in.base + uintptr(in.c.Entry[local])
}

func (in *Instance) tierable() bool {
	return in != nil && in.profile != nil && in.profile.tier != nil
}

// ActiveCompiler reports the compiler currently reached through this
// instance's stable public entries.
func (in *Instance) ActiveCompiler() CompilerEngine {
	if in != nil && in.profile != nil && in.profile.tier != nil {
		return CompilerEngine(in.profile.tier.active.Load())
	}
	if in != nil && in.c != nil {
		return in.c.compiler
	}
	return CompilerRailshot
}

// InstallDragline atomically publishes a source-identical Dragline image behind
// every stable wrapper entry. The boundary is intentionally one-shot and
// publishes code-specific root metadata with the entry generation.
func (in *Instance) InstallDragline(candidate *Compiled) error {
	return in.installDragline(candidate, nil)
}

// InstallDraglineTier publishes only the local original-Wasm function indexes
// selected by a bounded CompilerTierPlan. Unselected public entries continue to
// reach Railshot. The Dragline image is still retained as one immutable module
// because private direct calls within a selected cluster use its own code.
func (in *Instance) InstallDraglineTier(candidate *Compiled, plan CompilerTierPlan) error {
	if len(plan.Functions) == 0 {
		return fmt.Errorf("wago: compiler tier plan selects no functions")
	}
	if !slices.IsSorted(plan.Functions) {
		return fmt.Errorf("wago: compiler tier plan functions are not sorted")
	}
	for i, function := range plan.Functions {
		if i != 0 && plan.Functions[i-1] == function {
			return fmt.Errorf("wago: compiler tier plan repeats function %d", function)
		}
	}
	return in.installDragline(candidate, plan.Functions)
}

func (in *Instance) installDragline(candidate *Compiled, functions []uint32) error {
	if in == nil {
		return fmt.Errorf("wago: nil instance")
	}
	if candidate == nil {
		return fmt.Errorf("wago: nil Dragline candidate")
	}
	if err := candidate.validate(); err != nil {
		return fmt.Errorf("wago: Dragline candidate: %w", err)
	}
	if candidate.compiler != CompilerDragline {
		return fmt.Errorf("wago: compiler tier candidate is %s, want dragline", candidate.compiler)
	}
	if cloneFunctions := candidate.compactNativeFunctions(); len(cloneFunctions) != 0 && !slices.Equal(functions, cloneFunctions) {
		return fmt.Errorf("wago: compact native clone selection %v does not match installation %v", cloneFunctions, functions)
	}
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.resourcesClosed || in.closed {
		return fmt.Errorf("wago: instance is closed")
	}
	if !in.tierable() || !in.c.isTierable() {
		return fmt.Errorf("wago: instance was not compiled with tiering enabled")
	}
	t := in.profile.tier
	if t.installed.Load() != nil {
		return fmt.Errorf("wago: compiler tier is already installed")
	}
	if len(candidate.Entry) != len(in.c.Entry) || candidate.NumImports != in.c.NumImports || len(candidate.Funcs) != len(in.c.Funcs) {
		return fmt.Errorf("wago: Dragline candidate function layout differs from the instance")
	}
	for _, function := range functions {
		if function < uint32(in.c.NumImports) || function >= uint32(in.c.NumImports+len(in.c.Entry)) {
			return fmt.Errorf("wago: compiler tier plan function %d is not a local function", function)
		}
	}
	if candidate.boundsMode != in.c.boundsMode {
		return fmt.Errorf("wago: Dragline candidate bounds mode differs from the instance")
	}
	if candidate.needsExactNativeGCRoots() && candidate.genericGCFrameRoots() == nil {
		return fmt.Errorf("wago: Dragline candidate has no exact native GC root map")
	}
	if in.c.needsExactNativeGCRoots() && in.c.genericGCFrameRoots() == nil {
		return fmt.Errorf("wago: Railshot generation has no exact native GC root map")
	}
	if !candidate.codeCache.sourceHashAvailable || !in.c.codeCache.sourceHashAvailable || candidate.profileSourceHash() != in.c.profileSourceHash() {
		return fmt.Errorf("wago: Dragline candidate source identity differs from the instance")
	}
	base, err := candidate.acquireCode()
	if err != nil {
		return fmt.Errorf("wago: acquire Dragline tier: %w", err)
	}
	generation := &compilerCodeGeneration{compiled: candidate, base: base}
	// Root metadata must become discoverable before an entry can reach the new
	// image. The one-shot installation contract means this store cannot race a
	// competing candidate publication.
	t.installed.Store(generation)
	if len(functions) == 0 {
		for i, off := range candidate.Entry {
			atomic.StoreUint64((*uint64)(unsafe.Pointer(&t.entries[i*8])), uint64(base)+uint64(off))
		}
	} else {
		for _, function := range functions {
			local := int(function) - in.c.NumImports
			off := candidate.Entry[local]
			atomic.StoreUint64((*uint64)(unsafe.Pointer(&t.entries[local*8])), uint64(base)+uint64(off))
		}
	}
	t.active.Store(uint32(CompilerDragline))
	return nil
}
