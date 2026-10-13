//go:build linux && riscv64 && !tinygo && !wago_precompiled

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// newGCFrameRootPlan is the first bounded RV64 exact-root producer. It admits
// closed, table-free scalar modules whose only collection boundaries are GC
// allocation helpers. Native Wasm calls remain fail-closed until RV64 emits
// their return-PC callsite maps as well as allocation safepoints.
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
	if len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Tags) != 0 || len(m.Globals) != 0 || m.Start != nil || len(m.Elements) != 0 || m.DataCount != nil || len(m.Data) != 0 || len(m.StringRefs) != 0 {
		return reject("RV64 exact roots require a closed scalar allocation module")
	}
	for i := range m.Exports {
		if m.Exports[i].Index.Kind != wasm.ExternFunc {
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
		if ok, err := rv64GCFrameAllocationOnly(m.Code[function].BodyBytes, &classifier); err != nil {
			return reject("function %d instruction scan: %v", function, err)
		} else if !ok {
			return reject("function %d contains a native call without an RV64 callsite map", function)
		}
		plan.Conservative = true
		liveMasks, err := gcFrameAllLiveMasksArenaWithClassifier(m.Code[function].BodyBytes, len(plan.Locals), &classifier)
		if err != nil {
			return reject("function %d exact local liveness: %v", function, err)
		}
		if liveMasks.callN != 0 {
			return reject("function %d contains a native call without an RV64 callsite map", function)
		}
		plan.Locals, liveMasks, _, err = gcFrameCompactLiveLocalsArena(plan.Locals, liveMasks)
		if err != nil {
			return reject("function %d exact local liveness: %v", function, err)
		}
		if liveMasks.allocationN == 0 {
			return reject("function %d is collecting without an RV64 allocation safepoint", function)
		}
		if uint64(safepointBase)+uint64(liveMasks.allocationN) > uint64(shared.GCSafepointIDMax) {
			return reject("function %d exceeds the dense safepoint ID bound", function)
		}
		if !plan.SetLiveMasks(liveMasks.words, liveMasks.allocationN, 0) {
			return reject("function %d has malformed exact local liveness masks", function)
		}
		safepointBase += uint32(liveMasks.allocationN)
	}
	return modulePlan
}

func rv64GCFrameAllocationOnly(body []byte, classifier *wasm.ModuleInstructionClassifier) (bool, error) {
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false, err
		}
		var imm wasm.InstructionImmediate
		if err := classifier.ClassifyInto(r, op, &imm); err != nil {
			return false, err
		}
		switch imm.Kind {
		case wasm.InstrCall, wasm.InstrCallIndirect, wasm.InstrCallRef,
			wasm.InstrReturnCall, wasm.InstrReturnCallIndirect, wasm.InstrReturnCallRef:
			return false, nil
		}
	}
	return true, nil
}
