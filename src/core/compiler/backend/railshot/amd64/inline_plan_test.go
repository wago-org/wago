//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"reflect"
	"testing"
)

// Independent two-pass planner retained as a behavior oracle for the sparse plan.
func buildInlineTargetsTwoPass(m *wasm.Module, allHints []funcHints, policy CodegenPolicy, tailLoops ...bool) inlineTargetTable {
	allowTail := len(tailLoops) > 0 && tailLoops[0]
	if !policy.EnabledOption(optInline) {
		return inlineTargetTable{}
	}
	hasCall := false
	for i := range allHints {
		if allHints[i].flags.has(hintHasCall) {
			hasCall = true
			break
		}
	}
	if !hasCall {
		return inlineTargetTable{}
	}
	importedFuncs := m.ImportedFuncCount()
	if uint64(len(m.Code)) > uint64(^uint32(0)) {
		return inlineTargetTable{}
	}
	candidateCount, typeCount := 0, 0
	for i := range m.Code {
		ft, _, ok := inlineTargetFactsTwoPass(m, allHints, i, policy, allowTail)
		if !ok {
			continue
		}
		candidateCount++
		add := int(allHints[i].localCount) + len(ft.Results)
		if add < 0 || typeCount > int(^uint32(0))-add {
			// Inlining is optional. Fall back to direct calls instead of retaining a
			// sidecar whose compact 32-bit ranges cannot represent this module.
			return inlineTargetTable{}
		}
		typeCount += add
	}
	if candidateCount == 0 || candidateCount >= int(inlineTargetInvalid) {
		return inlineTargetTable{}
	}
	data := &inlineTargetData{
		slots:   make([]uint32, len(m.Code)),
		targets: make([]inlineTarget, 0, candidateCount),
		types:   make([]machineType, 0, typeCount),
	}
	targets := inlineTargetTable{first: importedFuncs, data: data, classifier: wasm.NewModuleInstructionClassifier(m, true)}
	for i := range m.Code {
		ft, facts, ok := inlineTargetFactsTwoPass(m, allHints, i, policy, allowTail)
		if !ok {
			continue
		}
		h := allHints[i]
		localStart := len(data.types)
		for _, p := range ft.Params {
			data.types = append(data.types, mtOf(p))
		}
		for _, run := range m.Code[i].Locals.Runs {
			for k := 0; k < int(run.Count); k++ {
				data.types = append(data.types, mtOf(run.Type))
			}
		}
		localEnd := len(data.types)
		for _, result := range ft.Results {
			data.types = append(data.types, mtOf(result))
		}
		resultEnd := len(data.types)
		res0 := mtNone
		if len(ft.Results) > 0 {
			res0 = data.types[localEnd]
		}
		addConst, addImmediate := inlineI32AddConst(m.Code[i].BodyBytes, ft)
		localDeclBytes := m.Code[i].LocalDeclBytes
		params := uint32(facts.params)
		if facts.inlineTupleLoop(policy) {
			params |= inlineTupleLoopFlag
		}
		if !policy.CompactNative && !profileEnabled && policy.EnabledOption(optTupleIfSelect) && facts.declaredLocals == 0 &&
			facts.params <= 4 && integerTupleSelectTarget(m, m.Code[i], ft) {
			params |= inlineTupleSelectFlag
		}
		if allowTail && tailLoopInlineCandidate(m, allHints, i, policy) {
			params |= inlineTailLoopFlag
			params |= uint32(tailInlineZeroMask(m.Code[i].BodyBytes, facts.params, int(h.localCount), targets.classifier)) << inlineTailZeroShift
			facts.hasControlFlow = true
		}
		if addConst {
			params |= inlineI32AddConstFlag
			localDeclBytes = uint32(addImmediate)
		} else if facts.declaredLocals == 0 && inlineI32AddParams(m.Code[i].BodyBytes, ft) {
			params |= inlineI32AddParamsFlag
		}
		if recursiveInlineCandidate(m, allHints, i, facts, policy) {
			params |= inlineRecursiveFlag
		}
		data.targets = append(data.targets, inlineTarget{
			body:           m.Code[i].BodyBytes,
			globalIdx:      importedFuncs + i,
			localDeclBytes: localDeclBytes,
			typeStart:      uint32(localStart),
			localTypeEnd:   uint32(localEnd),
			resultTypeEnd:  uint32(resultEnd),
			params:         params,
			res0:           res0,
			touchesMem:     facts.touchesMem,
			hasCtrl:        facts.hasControlFlow,
			omitStandalone: policy.CompactNative &&
				h.inlineCallSites == 1,
		})
		data.slots[i] = uint32(len(data.targets))
	}
	if policy.CompactNative {
		pruneNestedSizeInlineTargets(m, &targets)
	}
	return targets
}

func inlineTargetFactsTwoPass(m *wasm.Module, allHints []funcHints, i int, policy CodegenPolicy, tailLoops ...bool) (*wasm.CompType, inlineFacts, bool) {
	body := m.Code[i].BodyBytes
	if len(body) == 0 {
		return nil, inlineFacts{}, false
	}
	ft, ok := m.LocalFuncType(i)
	if !ok || ft == nil {
		return nil, inlineFacts{}, false
	}
	h := allHints[i]
	if h.flags.has(hintModuleEH) {
		return nil, inlineFacts{}, false
	}
	facts := inlineFacts{
		bodyBytes:      len(body),
		hasLoop:        h.flags.has(hintHasLoop),
		hasControlFlow: h.flags.has(hintHasControlFlow),
		touchesMem:     h.flags.has(hintTouchesMemory | hintUsesBulkMem),
		params:         len(ft.Params),
		results:        len(ft.Results),
		declaredLocals: int(h.localCount) - len(ft.Params),
		callSites:      int(h.inlineCallSiteCount()),
		regABIIntOnly:  sigFitsRegABI(ft) && sigIsIntOnly(ft),
	}
	if h.flags.has(hintHasCall) {
		facts.calleeCount = 1
	}
	facts.boundedTupleLoop = boundedIntegerTupleLoopBody(m.Code[i], facts, wasm.NewModuleInstructionClassifier(m, true))
	return ft, facts, inlineOK(facts, policy) || recursiveInlineCandidate(m, allHints, i, facts, policy) || len(tailLoops) > 0 && tailLoops[0] && tailLoopInlineCandidate(m, allHints, i, policy)
}

func TestSparseInlinePlanParity(t *testing.T) {
	check := func(m *wasm.Module) {
		t.Helper()
		hints, _, _, err := computeModuleHints(m, m.GlobalCount(), m.ImportedFuncCount(), nil, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, policy := range []CodegenPolicy{shared.DefaultCodegenPolicy(currentCodegenPolicy().Selection), shared.CompactCodegenPolicy(currentCodegenPolicy().Selection)} {
			for _, tail := range []bool{false, true} {
				got := buildInlineTargets(m, hints, policy, tail)
				want := buildInlineTargetsTwoPass(m, hints, policy, tail)
				if got.first != want.first || !reflect.DeepEqual(got.data, want.data) {
					t.Fatalf("compact=%t tail=%t sparse inline plan differs from two-pass oracle\ngot=%+v\nwant=%+v", policy.CompactNative, tail, got.data, want.data)
				}
			}
		}
	}
	for _, n := range []int{1, 16, 17, 33, 257} {
		defs := make([]funcDef, n+1)
		defs[0] = funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x41, 7, 0x10, 1, 0x0b}}
		for i := 1; i <= n; i++ {
			defs[i] = funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x20, 0, 0x41, 1, 0x6a, 0x0b}}
			if i%3 == 0 {
				defs[i].body = []byte{0, 0x20, 0, 0x10, 1, 0x0b}
			}
		}
		check(modFuncs(t, defs...))
	}
	params, results := []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}
	check(modFuncs(t,
		funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}},
		funcDef{params, results, []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 17, 0x6a, 0x12, 1, 0x0b, 0x0b}},
	))
	check(modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32}, results: results, body: []byte{0, 0x20, 0, 0x10, 0, 0x0b}}))
}
