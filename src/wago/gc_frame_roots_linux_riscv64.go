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
	if len(m.Imports) != 0 || len(m.Tables) > 2 || len(m.Memories) > 1 || len(m.Tags) != 0 || len(m.Globals) > 256 || len(m.Elements) > 256 || len(m.Data) > 256 || len(m.StringRefs) != 0 {
		return reject("RV64 exact roots require a closed scalar allocation module")
	}
	if m.DataCount != nil && (uint64(*m.DataCount) != uint64(len(m.Data)) || *m.DataCount > 256) {
		return reject("RV64 exact roots require an exact bounded data count")
	}
	if m.Start != nil {
		index := int(*m.Start)
		sig, ok := m.LocalFuncType(index)
		if !ok || index >= len(m.Code) || len(sig.Params) != 0 || len(sig.Results) != 0 {
			return reject("RV64 exact roots require a bounded local void start")
		}
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
	for i := range m.Tables {
		table := &m.Tables[i]
		if !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) || table.Type.Limits.Addr64 || !table.Type.Limits.HasMax ||
			table.Type.Limits.Min > table.Type.Limits.Max || table.Type.Limits.Max > 256 {
			return reject("RV64 exact roots require one bounded local funcref table32")
		}
		if table.Init != nil {
			expr := *table.Init
			if len(expr.Instrs) != 0 || len(expr.BodyBytes) < 3 || len(expr.BodyBytes) > 7 || expr.BodyBytes[0] != 0xd0 && expr.BodyBytes[0] != 0xd2 {
				return reject("RV64 exact roots reject table initializer expression")
			}
			if expr.BodyBytes[0] == 0xd0 {
				r := wasm.ReaderFrom(expr.BodyBytes[1:])
				heap, err := r.S33()
				if err != nil || heap != -16 {
					return reject("RV64 exact roots require a funcref table initializer")
				}
			}
			value, err := wasm.ParseFuncrefElementExpr(expr)
			if err != nil || !value.Null && uint64(value.FuncIndex) >= uint64(len(m.FuncTypes)) {
				return reject("RV64 exact roots reject table initializer target")
			}
		}
	}
	remainingElements := 256
	for i := range m.Elements {
		elem := &m.Elements[i]
		if len(m.Tables) == 0 {
			return reject("RV64 exact roots require bounded local function elements")
		}
		values := 0
		switch elem.Kind.Kind {
		case wasm.ElemFuncs:
			if len(elem.Kind.Exprs) != 0 || elem.Kind.Ref != (wasm.RefType{}) {
				return reject("RV64 exact roots require canonical legacy function elements")
			}
			values = len(elem.Kind.Funcs)
			for _, function := range elem.Kind.Funcs {
				if uint64(function) >= uint64(len(m.FuncTypes)) {
					return reject("RV64 exact roots reject element function %d", function)
				}
			}
		case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
			if len(elem.Kind.Funcs) != 0 || elem.Kind.Kind == wasm.ElemFuncExprs && elem.Kind.Ref != (wasm.RefType{}) ||
				elem.Kind.Kind == wasm.ElemTypedExprs && !wasm.EqualValType(wasm.RefVal(elem.Kind.Ref), wasm.FuncRef) {
				return reject("RV64 exact roots require exact nullable funcref expression elements")
			}
			values = len(elem.Kind.Exprs)
			for j, expr := range elem.Kind.Exprs {
				if len(expr.Instrs) != 0 || len(expr.BodyBytes) < 3 || len(expr.BodyBytes) > 7 || expr.BodyBytes[0] != 0xd0 && expr.BodyBytes[0] != 0xd2 {
					return reject("RV64 exact roots reject element expression %d:%d", i, j)
				}
				if expr.BodyBytes[0] == 0xd0 {
					r := wasm.ReaderFrom(expr.BodyBytes[1:])
					heap, err := r.S33()
					if err != nil || heap != -16 {
						return reject("RV64 exact roots reject non-funcref null element %d:%d", i, j)
					}
				}
				value, err := wasm.ParseFuncrefElementExpr(expr)
				if err != nil || !value.Null && uint64(value.FuncIndex) >= uint64(len(m.FuncTypes)) {
					return reject("RV64 exact roots reject element expression %d:%d", i, j)
				}
			}
		default:
			return reject("RV64 exact roots reject unknown function element kind")
		}
		if values > remainingElements {
			return reject("RV64 exact roots require bounded function element payloads")
		}
		switch elem.Mode.Kind {
		case wasm.ElemActive:
			if uint64(elem.Mode.Table) >= uint64(len(m.Tables)) || len(elem.Mode.Offset.Instrs) != 0 || !rv64RootedI32Const(elem.Mode.Offset.BodyBytes) {
				return reject("RV64 exact roots require bounded literal active function elements")
			}
		case wasm.ElemPassive, wasm.ElemDeclarative:
			if elem.Mode.Table != 0 || len(elem.Mode.Offset.Instrs) != 0 || len(elem.Mode.Offset.BodyBytes) != 0 {
				return reject("RV64 exact roots require canonical inactive function elements")
			}
		default:
			return reject("RV64 exact roots reject unknown function element modes")
		}
		remainingElements -= values
	}
	remainingData := railshot.MaxModuleBytes
	for i := range m.Data {
		data := &m.Data[i]
		if len(data.Init) > remainingData {
			return reject("RV64 exact roots require bounded data payloads")
		}
		switch data.Mode.Kind {
		case wasm.DataActive:
			if len(m.Memories) != 1 || data.Mode.Mem != 0 || len(data.Mode.Offset.Instrs) != 0 || !rv64RootedI32Const(data.Mode.Offset.BodyBytes) {
				return reject("RV64 exact roots require literal active memory32 data")
			}
		case wasm.DataPassive:
			if data.Mode.Mem != 0 || len(data.Mode.Offset.Instrs) != 0 || len(data.Mode.Offset.BodyBytes) != 0 {
				return reject("RV64 exact roots require canonical passive data")
			}
		default:
			return reject("RV64 exact roots reject unknown data modes")
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
