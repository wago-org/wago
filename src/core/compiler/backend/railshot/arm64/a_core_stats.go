//go:build arm64

package arm64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func parsePinGlobalK(s string) int {
	switch s {
	case "0":
		return 0
	case "1":
		return 1
	case "2":
		return 2
	case "3":
		return 3
	default: // "", "auto", or anything unrecognized
		return -1
	}
}

func (ms *ModuleStats) finalizeCompileResourceStats() {
	if !diagnosticsEnabled || ms == nil {
		return
	}
	c := &ms.Compile
	c.StageNanos[shared.CompileStageFunctions] = 0
	c.FunctionAttempts = 0
	for _, s := range ms.Funcs {
		if !diagnosticsEnabled || s == nil {
			continue
		}
		c.StageNanos[shared.CompileStageFunctions] += s.CompileNanos
		c.FunctionAttempts += s.FunctionAttempts
	}
}

func (ms *ModuleStats) setNodeScratchStats(sc *scratch) {
	if !diagnosticsEnabled || ms == nil {
		return
	}
	ms.Compile.NodeScratchReserved = 0
	ms.Compile.NodeScratchPeak = 0
	ms.Compile.NodeScratchRetained = 0
	ms.Compile.NodeScratchDiscarded = 0
	ms.Compile.ControlScratchReserved = 0
	ms.Compile.ControlScratchPeak = 0
	ms.Compile.ControlScratchRetained = 0
	ms.Compile.ControlScratchDiscarded = 0
	ms.addNodeScratchStats(sc)
}

func (s *CodegenStats) addFlush() {
	if diagnosticsEnabled && s != nil {
		s.Flushes++
	}
}

func (s *CodegenStats) addFlushBelow() {
	if diagnosticsEnabled && s != nil {
		s.FlushBelows++
	}
}

func (s *CodegenStats) addFlushRoot(deferred bool) {
	if diagnosticsEnabled && s != nil {
		s.FlushRoots++
		if deferred {
			s.FlushDeferredRoots++
		}
	}
}

func (s *CodegenStats) addFlushBelowRoot(deferred bool) {
	if diagnosticsEnabled && s != nil {
		s.FlushBelowRoots++
		if deferred {
			s.FlushBelowDeferred++
		}
	}
}

func (s *CodegenStats) addCallFlush() {
	if diagnosticsEnabled && s != nil {
		s.CallFlushes++
	}
}

func (s *CodegenStats) addLocalSetDeferred() {
	if diagnosticsEnabled && s != nil {
		s.LocalSetDeferred++
	}
}

func (s *CodegenStats) addCondense() {
	if diagnosticsEnabled && s != nil {
		s.Condenses++
	}
}

func (s *CodegenStats) addSpill() {
	if diagnosticsEnabled && s != nil {
		s.Spills++
	}
}

func (s *CodegenStats) addReload() {
	if diagnosticsEnabled && s != nil {
		s.Reloads++
	}
}

func (s *CodegenStats) addForcedLoad() {
	if diagnosticsEnabled && s != nil {
		s.MemRefsForcedByStore++
	}
}

func (s *CodegenStats) addTrapStub() {
	if diagnosticsEnabled && s != nil {
		s.TrapStubs++
	}
}

func (s *CodegenStats) addTrapGroup() {
	if diagnosticsEnabled && s != nil {
		s.TrapGroups++
	}
}

func (s *CodegenStats) addBoundsCheck() {
	if diagnosticsEnabled && s != nil {
		s.BoundsChecks++
	}
}

func (s *CodegenStats) addBoundsElidable() {
	if diagnosticsEnabled && s != nil {
		s.BoundsChecksElidable++
	}
}

func (s *CodegenStats) addPinnedLocal() {
	if diagnosticsEnabled && s != nil {
		s.PinnedLocals++
	}
}

func (s *CodegenStats) addPinnedGlobalValue() {
	if diagnosticsEnabled && s != nil {
		s.PinnedGlobalsValue++
	}
}

func (s *CodegenStats) addGCSpillReloadBytes(n int) {
	if diagnosticsEnabled && s != nil && n > 0 {
		s.GCCodeBytes.SpillReload += n
	}
}

func (s *CodegenStats) addGCTrapStubBytes(n int) {
	if diagnosticsEnabled && s != nil && n > 0 {
		s.GCCodeBytes.TrapStub += n
	}
}

// call records one call lowering of the given kind.
func (s *CodegenStats) call(kind string) {
	if !diagnosticsEnabled || s == nil {
		return
	}
	if s.Calls == nil {
		s.Calls = make(map[string]int)
	}
	s.Calls[kind]++
}

func (s *CodegenStats) addInlineSiteBytes(n int) {
	if diagnosticsEnabled && s != nil {
		s.InlineSiteBytes += n
	}
}

// peep records one peephole/instruction-selection rewrite by stable name.
func (s *CodegenStats) peep(name string) {
	if !diagnosticsEnabled || s == nil {
		return
	}
	if s.Peephole == nil {
		s.Peephole = make(map[string]int)
	}
	s.Peephole[name]++
}

func (s *CodegenStats) peepN(name string, n int) {
	for ; n > 0; n-- {
		s.peep(name)
	}
}

// Keep environment reads out of ordinary builds, including package initialization.
func diagnosticEnv(key string) string {
	if !diagnosticsEnabled {
		return ""
	}
	return os.Getenv(key)
}
