package wago

import (
	"fmt"
	"sync/atomic"
	"unsafe"

	compilerprofile "github.com/wago-org/wago/src/core/compiler/profile"
	"github.com/wago-org/wago/src/core/runtime"
)

type instanceRailshotProfile struct {
	arena      *runtime.Arena
	native     []byte
	generation atomic.Uint64
	tier       *instanceCompilerTier
}

// SnapshotRailshotProfile returns this instance's native function-entry counts.
// When reset is true, each counter is atomically drained. The profile indexes
// use the module's original Wasm function index space, including imports.
func (in *Instance) SnapshotRailshotProfile(phase CompilerProfilePhase, reset bool) (CompilerProfile, error) {
	if in == nil {
		return CompilerProfile{}, fmt.Errorf("wago: nil instance")
	}
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.resourcesClosed || in.c == nil || !in.c.hasFunctionCounters() {
		return CompilerProfile{}, fmt.Errorf("wago: Railshot profiling is not available")
	}
	if in.profile == nil {
		return CompilerProfile{}, fmt.Errorf("wago: Railshot profiling is not available")
	}
	counts := make([]uint64, len(in.profile.native)/8)
	for i := range counts {
		counter := (*uint64)(unsafe.Pointer(&in.profile.native[i*8]))
		if reset {
			counts[i] = atomic.SwapUint64(counter, 0)
		} else {
			counts[i] = atomic.LoadUint64(counter)
		}
	}
	generation := nextProfileGeneration(&in.profile.generation)
	result := CompilerProfile{
		Version: compilerprofile.Version, ModuleHash: in.c.profileSourceHash(),
		Generation: generation, Source: compilerprofile.SourceRailshot, Phase: phase,
		FunctionCounts: counts,
	}
	if err := result.Validate(); err != nil {
		return CompilerProfile{}, err
	}
	return result, nil
}

func nextProfileGeneration(generation *atomic.Uint64) uint64 {
	for {
		before := generation.Load()
		if before == ^uint64(0) {
			return before
		}
		if generation.CompareAndSwap(before, before+1) {
			return before + 1
		}
	}
}
