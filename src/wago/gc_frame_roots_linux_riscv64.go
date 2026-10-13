//go:build linux && riscv64 && !tinygo && !wago_precompiled

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// newGCFrameRootPlan is the bounded RV64 exact-root producer for closed,
// table-free scalar modules. Allocation helpers publish safepoint roots and
// direct Wasm calls publish return-PC caller-frame maps.
func newGCFrameRootPlan(m *wasm.Module, exactRoots, genericExecution bool, diagnostic *string, analysis *wasm.ValidatedModuleAnalysis) *shared.GCModuleFrameRootPlan {
	if diagnostic != nil {
		*diagnostic = ""
	}
	if !exactRoots {
		return nil
	}
	if !genericExecution {
		// Exact RV64 products retain their existing persistent-root proofs until
		// their product witnesses are deliberately migrated to frame maps.
		return nil
	}
	reject := func(format string, args ...any) *shared.GCModuleFrameRootPlan {
		if diagnostic != nil {
			*diagnostic = fmt.Sprintf(format, args...)
		}
		return nil
	}
	if m == nil || len(m.Code) == 0 {
		return reject("generic GC module has no local function bodies")
	}
	if len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) > 1 || len(m.Tags) != 0 || len(m.Globals) > 256 || m.Start != nil || len(m.Elements) != 0 || m.DataCount != nil || len(m.Data) > 256 || len(m.StringRefs) != 0 {
		return reject("RV64 exact roots require a closed scalar allocation module")
	}
	for i := range m.Globals {
		if !rv64RootedScalarGlobal(&m.Globals[i]) {
			return reject("RV64 exact roots require literal numeric scalar global %d", i)
		}
	}
	if len(m.Memories) == 1 {
		memory := m.Memories[0]
		if memory.Shared || memory.Limits.Addr64 || !memory.Limits.HasMax || memory.Limits.Min > memory.Limits.Max || memory.Limits.Max > 256 {
			return reject("RV64 exact roots require bounded local memory32")
		}
	}
	remainingData := railshot.MaxModuleBytes
	for i := range m.Data {
		data := &m.Data[i]
		if len(m.Memories) != 1 || data.Mode.Kind != wasm.DataActive || data.Mode.Mem != 0 || len(data.Mode.Offset.Instrs) != 0 || len(data.Init) > remainingData || !rv64RootedI32Const(data.Mode.Offset.BodyBytes) {
			return reject("RV64 exact roots require bounded literal active memory32 data")
		}
		remainingData -= len(data.Init)
	}
	for i := range m.Exports {
		export := &m.Exports[i]
		if export.Index.Kind == wasm.ExternMem && len(m.Memories) == 1 && export.Index.Index == 0 {
			continue
		}
		if export.Index.Kind == wasm.ExternGlobal && uint64(export.Index.Index) < uint64(len(m.Globals)) {
			continue
		}
		if export.Index.Kind != wasm.ExternFunc {
			return reject("RV64 exact roots reject non-function export %d", i)
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	modulePlan, err := gcFramePrepareModuleRootPlan(m, &classifier, analysis)
	if err != nil {
		return reject("%v", err)
	}
	var safepointBase uint32
	for function := range m.Code {
		if !modulePlan.FunctionPending(function) {
			continue
		}
		ft, ok := m.LocalFuncType(function)
		if !ok {
			return reject("function %d has no validated signature", function)
		}
		plan, ok := modulePlan.BeginFunction(function)
		if !ok {
			return reject("function %d root plan ownership is invalid", function)
		}
		*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, SafepointBase: safepointBase}
		slot, local := 0, uint32(0)
		add := func(t wasm.ValType) bool {
			if collectorFrameRefType(m, t) {
				if len(plan.Locals) == shared.GCFrameTrackedLocalLimit {
					return false
				}
				plan.Locals = append(plan.Locals, shared.GCFrameLocal{Index: local, Offset: uint32(shared.RV64CallFrameHeaderBytes + slot*8)})
			}
			if wasm.EqualValType(t, wasm.V128) {
				slot += 2
			} else {
				slot++
			}
			local++
			return true
		}
		for _, t := range ft.Params {
			if !add(t) {
				return reject("function %d exceeds %d tracked collector locals", function, shared.GCFrameTrackedLocalLimit)
			}
		}
		for _, run := range m.Code[function].Locals.Runs {
			for i := uint32(0); i < run.Count; i++ {
				if !add(run.Type) {
					return reject("function %d exceeds %d tracked collector locals", function, shared.GCFrameTrackedLocalLimit)
				}
			}
		}
		plan.Conservative = true
		liveMasks, err := gcFrameAllLiveMasksArenaWithClassifier(m.Code[function].BodyBytes, len(plan.Locals), &classifier)
		if err != nil {
			return reject("function %d exact local liveness: %v", function, err)
		}
		plan.Locals, liveMasks, _, err = gcFrameCompactLiveLocalsArena(plan.Locals, liveMasks)
		if err != nil {
			return reject("function %d exact local liveness: %v", function, err)
		}
		if liveMasks.allocationN == 0 && liveMasks.callN == 0 {
			return reject("function %d is collecting without an RV64 root boundary", function)
		}
		if uint64(safepointBase)+uint64(liveMasks.allocationN) > uint64(shared.GCSafepointIDMax) {
			return reject("function %d exceeds the dense safepoint ID bound", function)
		}
		if !plan.SetLiveMasks(liveMasks.words, liveMasks.allocationN, liveMasks.callN) {
			return reject("function %d has malformed exact local liveness masks", function)
		}
		safepointBase += uint32(liveMasks.allocationN)
	}
	return modulePlan
}

func rv64RootedI32Const(body []byte) bool {
	r := wasm.ReaderFrom(body)
	op, err := r.Byte()
	if err != nil || op != 0x41 {
		return false
	}
	if _, err = r.I32(); err != nil {
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && !r.HasNext()
}

func rv64RootedScalarGlobal(global *wasm.Global) bool {
	if global == nil || len(global.Init.Instrs) != 0 {
		return false
	}
	r := wasm.ReaderFrom(global.Init.BodyBytes)
	op, err := r.Byte()
	if err != nil {
		return false
	}
	switch {
	case wasm.EqualValType(global.Type.Type, wasm.I32) && op == 0x41:
		_, err = r.I32()
	case wasm.EqualValType(global.Type.Type, wasm.I64) && op == 0x42:
		_, err = r.I64()
	case wasm.EqualValType(global.Type.Type, wasm.F32) && op == 0x43:
		_, err = r.LEU32()
	case wasm.EqualValType(global.Type.Type, wasm.F64) && op == 0x44:
		_, err = r.LEU64()
	default:
		return false
	}
	if err != nil {
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && !r.HasNext()
}
