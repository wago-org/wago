//go:build wago_profile

package shared

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Every emitting hook contributes one identifiable four-byte instruction to a
// deterministic mock image. Compare the complete physical call trace, including
// operand/register choices and patched destinations, with recording disabled.
type scalarSourceEvent struct {
	kind    string
	op      IntOp
	d, l, r uint8
	wide    bool
	value   int64
	operand ScalarOperand
}
type scalarSourceTarget struct {
	scalarTestTarget
	events  []scalarSourceEvent
	patches [][2]int
	scaled  bool
}

func (t *scalarSourceTarget) emit(e scalarSourceEvent) { t.events = append(t.events, e) }
func (t *scalarSourceTarget) Position() int            { return len(t.events) * 4 }
func (*scalarSourceTarget) Operand(_ IntOp, _ bool, o ScalarOperand) bool {
	return o.Kind == ScalarRegister
}
func (t *scalarSourceTarget) Constant(d uint8, v int64, w bool) {
	t.emit(scalarSourceEvent{kind: "constant", d: d, value: v, wide: w})
}
func (t *scalarSourceTarget) Move(d, r uint8, w bool) {
	t.emit(scalarSourceEvent{kind: "move", d: d, r: r, wide: w})
}
func (t *scalarSourceTarget) NormalizeI32(d uint8) {
	t.emit(scalarSourceEvent{kind: "normalize", d: d})
}
func (t *scalarSourceTarget) Load(d uint8, off int32, w bool) {
	t.emit(scalarSourceEvent{kind: "load", d: d, value: int64(off), wide: w})
}
func (t *scalarSourceTarget) Store(off int32, r uint8, w bool) {
	t.emit(scalarSourceEvent{kind: "store", r: r, value: int64(off), wide: w})
}
func (t *scalarSourceTarget) Binary(op IntOp, w bool, d, l uint8, r ScalarOperand) {
	t.emit(scalarSourceEvent{kind: "binary", op: op, wide: w, d: d, l: l, operand: r})
}
func (t *scalarSourceTarget) ScaledAdd(w bool, d, l, r, shift uint8) bool {
	if t.scaled {
		t.emit(scalarSourceEvent{kind: "scaled", wide: w, d: d, l: l, r: r, value: int64(shift)})
	}
	return t.scaled
}
func (t *scalarSourceTarget) BranchZero(r uint8) int {
	site := t.Position()
	t.emit(scalarSourceEvent{kind: "branch-zero", r: r})
	return site
}
func (t *scalarSourceTarget) BranchCompare(op IntOp, w bool, l uint8, r ScalarOperand) int {
	site := t.Position()
	t.emit(scalarSourceEvent{kind: "branch-compare", op: op, wide: w, l: l, operand: r})
	return site
}
func (t *scalarSourceTarget) Jump() int {
	site := t.Position()
	t.emit(scalarSourceEvent{kind: "jump"})
	return site
}
func (t *scalarSourceTarget) Patch(site, position int) error {
	t.patches = append(t.patches, [2]int{site, position})
	return nil
}
func (t *scalarSourceTarget) Return(r uint8, w bool, slots int) {
	t.emit(scalarSourceEvent{kind: "return", r: r, wide: w, value: int64(slots)})
}

func scalarSourceCompile(t *testing.T, code []byte, params, locals int, scaled bool) (*scalarSourceTarget, []NativeSourceRange) {
	t.Helper()
	types := make([]wasm.ValType, params+locals)
	for i := range types {
		types[i] = wasm.I32
	}
	widths := make([]bool, len(types))
	ft := &wasm.CompType{Params: types[:params], Results: []wasm.ValType{wasm.I32}}
	summary := AdmitScalar(code, ft, types)
	if !summary.Eligible {
		t.Fatal("source fixture not admitted")
	}
	var plain, recorded ScalarState
	defer plain.FinishWorker()
	defer recorded.FinishWorker()
	a, b := &scalarSourceTarget{scaled: scaled}, &scalarSourceTarget{scaled: scaled}
	wantSlots, err := plain.CompileScalar(code, summary, widths, params, a)
	if err != nil {
		t.Fatal(err)
	}
	var recorder ScalarSourceRecorder
	gotSlots, err := recorded.CompileScalarWithSources(code, summary, widths, params, b, &recorder, 17, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) || gotSlots != wantSlots || plain.Spills != recorded.Spills || plain.Reloads != recorded.Reloads || plain.Memory() != recorded.Memory() {
		t.Fatal("source recording changed physical lowering or worker scratch", a, b)
	}
	if recorded.sources != nil || recorded.target != nil || recorded.regs != nil {
		t.Fatal("compiler retained active source state")
	}
	ranges := append([]NativeSourceRange(nil), recorder.Ranges()...)
	if err := jitprofile.ValidateSources(ranges, uint64(b.Position())); err != nil {
		t.Fatal(err)
	}
	for _, r := range ranges {
		if r.Function != 17 || r.InlineParent != 0 || r.WasmOffset < 3 || r.WasmOffset >= uint32(3+len(code)) {
			t.Fatal("invalid function/local-declaration coordinate", r)
		}
	}
	recorder.Release()
	if recorder.Memory() != 0 || len(recorder.Ranges()) != 0 {
		t.Fatal("recorder backing retained after release")
	}
	return b, ranges
}

func scalarSourceExpect(t *testing.T, ranges []NativeSourceRange, event int, pc int) {
	t.Helper()
	r, found := jitprofile.LookupSource(ranges, uint64(event*4))
	if pc < 0 {
		if found {
			t.Fatalf("event %d acquired invented origin %+v", event, r)
		}
		return
	}
	if !found || r.WasmOffset != uint32(pc+3) || r.Function != 17 {
		t.Fatalf("event %d: origin %+v/%v, want opcode %d", event, r, found, pc)
	}
}

func TestScalarSourceDeferredFoldedAndRecycledOrigins(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		code                 []byte
		params               int
		constant, constantPC int
		binaryPC             map[IntOp]int
		returnPC             int
	}{
		{"reordered-deferred", []byte{0x20, 0, 0x20, 1, 0x41, 3, 0x6c, 0x6a, 0x0b}, 2, 3, 4, map[IntOp]int{IntMul: 6, IntAdd: 7}, 8},
		{"folded", []byte{0x41, 1, 0x41, 2, 0x6a, 0x0b}, 0, 3, 4, nil, 5},
		{"recycled-id", []byte{0x41, 1, 0x1a, 0x41, 2, 0x0b}, 0, 2, 3, nil, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, ranges := scalarSourceCompile(t, tc.code, tc.params, 0, false)
			seen := make(map[IntOp]bool)
			constants, returns := 0, 0
			for i, e := range target.events {
				switch e.kind {
				case "load":
					scalarSourceExpect(t, ranges, i, -1)
				case "constant":
					constants++
					if e.value != int64(tc.constant) {
						t.Fatal("wrong folded/recycled constant", e)
					}
					scalarSourceExpect(t, ranges, i, tc.constantPC)
				case "binary":
					pc, ok := tc.binaryPC[e.op]
					if !ok {
						t.Fatal("unexpected operation", e)
					}
					seen[e.op] = true
					scalarSourceExpect(t, ranges, i, pc)
				case "return":
					returns++
					scalarSourceExpect(t, ranges, i, tc.returnPC)
				default:
					t.Fatal("unexpected fixture emission", e)
				}
			}
			if constants != 1 || returns != 1 || len(seen) != len(tc.binaryPC) {
				t.Fatal("missing fixture emission")
			}
		})
	}
}

func TestScalarSourceCompareFusionAliasesAndSyntheticGaps(t *testing.T) {
	compare := []byte{0x20, 0, 0x20, 1, 0x48, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 2, 0x0b, 0x0b}
	target, ranges := scalarSourceCompile(t, compare, 2, 0, false)
	branches := 0
	for i, e := range target.events {
		if e.kind == "branch-compare" {
			branches++
			scalarSourceExpect(t, ranges, i, 4)
		}
		if e.kind == "constant" {
			pc := 7
			if e.value == 2 {
				pc = 10
			}
			scalarSourceExpect(t, ranges, i, pc)
		}
	}
	if branches != 1 {
		t.Fatal("comparison was not fused")
	}
	alias := []byte{0x20, 0, 0x41, 7, 0x6a, 0x22, 0, 0x20, 0, 0x6a, 0x0b}
	target, ranges = scalarSourceCompile(t, alias, 1, 0, false)
	binaries := 0
	for i, e := range target.events {
		if e.kind == "binary" {
			pc := 4
			if binaries != 0 {
				pc = 9
			}
			scalarSourceExpect(t, ranges, i, pc)
			binaries++
		}
	}
	if binaries != 2 {
		t.Fatal("alias fixture lost operation")
	}
	target, ranges = scalarSourceCompile(t, []byte{0x20, 0, 0x0b}, 0, 1, false)
	for i, e := range target.events {
		if e.kind == "constant" {
			scalarSourceExpect(t, ranges, i, -1)
		}
		if e.kind == "return" {
			scalarSourceExpect(t, ranges, i, 2)
		}
	}
	scaled := []byte{0x20, 0, 0x20, 1, 0x41, 2, 0x74, 0x6a, 0x0b}
	target, ranges = scalarSourceCompile(t, scaled, 2, 0, true)
	fused := 0
	for i, e := range target.events {
		if e.kind == "scaled" {
			fused++
			scalarSourceExpect(t, ranges, i, 7)
		}
	}
	if fused != 1 {
		t.Fatal("scaled-add fixture was not fused")
	}
}

func TestScalarSourceSpillScopeAndBound(t *testing.T) {
	target := &scalarSourceTarget{}
	r := &ScalarSourceRecorder{function: 17, localDecls: 3}
	s := &ScalarState{}
	s.sources, s.target, s.regs = r, target, scalarTestRegs
	s.add(scalarNode{})
	s.sourceInstruction(7)
	id := s.add(scalarNode{kind: ScalarConstant, refs: 1})
	s.node(id).kind, s.node(id).reg = ScalarRegister, 1
	s.owners[1] = id
	s.sourceInstruction(11)
	s.spill(id)
	s.materialize(id, 0)
	r.switchOrigin(target.Position(), 0)
	if len(target.events) != 2 || target.events[0].kind != "store" || target.events[1].kind != "load" {
		t.Fatal("spill/reload fixture")
	}
	for i := range target.events {
		scalarSourceExpect(t, r.Ranges(), i, 7)
	}
	if r.active != 0 {
		t.Fatal("source scope not closed")
	}
	var bounded ScalarSourceRecorder
	for i := 0; i <= MaxScalarSourceRanges; i++ {
		bounded.switchOrigin(4*i, uint32(i+1))
	}
	if bounded.err != nil || len(bounded.Ranges()) != MaxScalarSourceRanges {
		t.Fatal("source cap rejected its exact bound", bounded.err)
	}
	bounded.switchOrigin(4*(MaxScalarSourceRanges+1), 0)
	if bounded.err == nil || !strings.Contains(bounded.err.Error(), "range bound") || cap(bounded.emission.ranges) != MaxScalarSourceRanges {
		t.Fatal("source directory exceeded bound", bounded.err)
	}
	if bounded.Memory() != uint64(MaxScalarSourceRanges)*uint64(unsafe.Sizeof(NativeSourceRange{})) {
		t.Fatal("source backing accounting")
	}
}

func TestScalarSourceReuseFailureAndNilRecorder(t *testing.T) {
	code := []byte{0x41, 1, 0x0b}
	ft := &wasm.CompType{Results: []wasm.ValType{wasm.I32}}
	summary := AdmitScalar(code, ft, nil)
	var state ScalarState
	var recorder ScalarSourceRecorder
	for _, base := range []uint32{0, 29} {
		target := &scalarSourceTarget{}
		if _, err := state.CompileScalarWithSources(code, summary, nil, 0, target, &recorder, base+2, base); err != nil {
			t.Fatal(err)
		}
		for _, r := range recorder.Ranges() {
			if r.Function != base+2 || r.WasmOffset != base && r.WasmOffset != base+2 {
				t.Fatal("source state crossed function boundary", r)
			}
		}
	}
	for _, tc := range []struct {
		code []byte
		base uint32
	}{{[]byte{0xff}, 0}, {code, ^uint32(0)}} {
		if _, err := state.CompileScalarWithSources(tc.code, summary, nil, 0, &scalarSourceTarget{}, &recorder, 0, tc.base); err == nil || len(recorder.Ranges()) != 0 || state.sources != nil || state.target != nil || state.regs != nil {
			t.Fatal("failed source compile retained output/active state", err)
		}
	}
	target := &scalarTestTarget{}
	// Warm existing scalar storage, then ensure both ordinary and explicitly nil
	// recording calls retain the existing zero-allocation repeated compile path.
	if _, err := state.CompileScalar(code, summary, nil, 0, target); err != nil {
		t.Fatal(err)
	}
	for _, recording := range []bool{false, true} {
		allocations := testing.AllocsPerRun(10, func() {
			var err error
			if recording {
				_, err = state.CompileScalarWithSources(code, summary, nil, 0, target, nil, 0, 0)
			} else {
				_, err = state.CompileScalar(code, summary, nil, 0, target)
			}
			if err != nil {
				panic(err)
			}
		})
		if allocations != 0 {
			t.Fatal("nil source recorder allocated", allocations)
		}
	}
	state.FinishWorker()
	if state.Memory() != 0 || state.sources != nil {
		t.Fatal("worker retained source ownership")
	}
	recorder.Release()
}

func TestScalarSourceSidecarFollowsLiveArena(t *testing.T) {
	var code []byte
	for i := 0; i < 1000; i++ {
		code = append(code, 0x20, 0, 0x41, 1, 0x6a, 0x21, 0)
	}
	code = append(code, 0x20, 0, 0x0b)
	ft := &wasm.CompType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	summary := AdmitScalar(code, ft, ft.Params)
	if !summary.Eligible {
		t.Fatal("large source reuse fixture not admitted")
	}
	var state ScalarState
	var recorder ScalarSourceRecorder
	target := &scalarSourceTarget{}
	if _, err := state.CompileScalarWithSources(code, summary, []bool{false}, 1, target, &recorder, 17, 3); err != nil {
		t.Fatal(err)
	}
	if cap(recorder.nodes) > 64 || len(recorder.nodes) > 8 || state.nodes[0].order < 2000 {
		t.Fatal("source sidecar followed total node creation instead of recycled arena", len(recorder.nodes), cap(recorder.nodes))
	}
	binaries := 0
	for i, e := range target.events {
		if e.kind == "binary" {
			scalarSourceExpect(t, recorder.Ranges(), i, binaries*7+4)
			binaries++
		}
	}
	if binaries != 1000 {
		t.Fatal("missing repeated operation", binaries)
	}
	// Reused storage must clear prior definition origins for synthetic zero
	// nodes, even when their arena IDs collide with the previous function.
	zero := []byte{0x20, 0, 0x0b}
	ft.Params = nil
	target = &scalarSourceTarget{}
	if _, err := state.CompileScalarWithSources(zero, AdmitScalar(zero, ft, []wasm.ValType{wasm.I32}), []bool{false}, 0, target, &recorder, 17, 3); err != nil {
		t.Fatal(err)
	}
	for i, e := range target.events {
		if e.kind == "constant" {
			scalarSourceExpect(t, recorder.Ranges(), i, -1)
		}
	}
	state.FinishWorker()
	recorder.Release()
}

func TestScalarSourceRetainsTargetCoordinates(t *testing.T) {
	code := []byte{0x41, 7, 0x0b}
	ft := &wasm.CompType{Results: []wasm.ValType{wasm.I32}}
	target := &scalarSourceTarget{}
	target.emit(scalarSourceEvent{kind: "existing-prefix"})
	target.emit(scalarSourceEvent{kind: "existing-prefix"})
	start := target.Position()
	var state ScalarState
	var recorder ScalarSourceRecorder
	if _, err := state.CompileScalarWithSources(code, AdmitScalar(code, ft, nil), nil, 0, target, &recorder, 17, 3); err != nil {
		t.Fatal(err)
	}
	ranges := recorder.Ranges()
	if len(ranges) != 2 || ranges[0].Offset != uint64(start) || ranges[1].Offset != uint64(start+4) {
		t.Fatal("source offsets do not retain exact target coordinates", ranges)
	}
	for offset := 0; offset < start; offset++ {
		if _, found := jitprofile.LookupSource(ranges, uint64(offset)); found {
			t.Fatal("preexisting target bytes gained source provenance", offset)
		}
	}
	scalarSourceExpect(t, ranges, start/4, 0)
	scalarSourceExpect(t, ranges, start/4+1, 2)
	if _, found := jitprofile.LookupSource(ranges, uint64(target.Position())); found {
		t.Fatal("source range extended beyond final instruction")
	}
	state.FinishWorker()
	recorder.Release()
}
