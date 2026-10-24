//go:build wago_regalloccheck

package shared

import (
	"encoding/binary"
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// VerifySourceCallModuleAMD64 independently walks one entire final module in
// declaration order. This final-byte proof has no encoder-journal claim. It
// borrows validation facts only until return, after all workers have joined.
// Source functions0/1 forward two i32 arguments to identity function2; nothing
// else (including imports, references, inlining, islands or sharing) is admitted.
func VerifySourceCallModuleAMD64(ctx *codegen.SourceContext, m *wasm.Module, code []byte, entry, internal []int, admit bool) (r regalloccheck.Result) {
	r = sourceLeafUnavailable(regalloccheck.UnsupportedOperation, "no admitted final-module call recipe")
	a, features, ok := codegen.ValidatedSourceContext(ctx, m)
	if !ok || !admit || len(m.Code) != 3 || len(m.FuncTypes) != 3 || len(m.Types) != 1 || len(m.Types[0].SubTypes) != 1 || len(m.Imports) != 0 || len(m.Exports) != 0 || m.Start != nil || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 || len(m.StringRefs) != 0 || len(m.BranchHints) != 0 {
		return r
	}
	for i := range m.Code {
		body := "\x20\x00\x20\x01\x10\x02\x0b"
		if i == 2 {
			body = "\x20\x00\x0b"
		}
		if len(m.Code[i].Locals.Runs) != 0 || m.Code[i].LocalDeclBytes > 5 || string(m.Code[i].BodyBytes) != body {
			return r
		}
	}
	if len(code) > 512 || len(entry) != 3 || len(internal) != 3 {
		return r
	}
	if !codegen.ReserveFinalSourcePass(ctx, m) {
		return sourceLeafUnavailable(regalloccheck.ResourceLimit, "final-module source credits exhausted")
	}
	var ledgers [3]*wasm.SourceLedger
	defer func() {
		for _, l := range ledgers {
			if l != nil {
				l.Close()
			}
		}
	}()
	work := 16384 - 512 // Whole-image decode/construction is bounded before allocation.
	defer func() { r.Work = 16384 - work }()
	for i := range ledgers {
		l, sr, err := wasm.BuildSourceLedger(m, a, i, features, wasm.SourceLedgerLimits{BodyBytes: 7, Metadata: 64, Locals: 2, Events: 4, Values: 4, Operands: 12, Stack: 2, Blocks: 2, Edges: 1, ControlDepth: 1, Work: min(work, 1024)})
		ledgers[i] = l
		work -= sr.Work
		if err != nil || sr.Coverage != wasm.SourceContractsComplete {
			return sourceLeafUnavailable(sr.Reason, "final-module source contracts incomplete")
		}
		ft, valid := m.LocalFuncType(i)
		if !valid || len(ft.Params) != 2 || len(ft.Results) != 1 || ft.Params[0] != wasm.I32 || ft.Params[1] != wasm.I32 || ft.Results[0] != wasm.I32 {
			return r
		}
		if l.LocalCount() != 2 || l.BlockCount() != 2 || l.EdgeCount() != 1 {
			return r
		}
		if i < 2 {
			if l.EventCount() != 4 || l.ValueCount() != 4 || l.Event(2).Kind != wasm.InstrCall || l.Event(2).Index != 2 || l.Input(2, 0) != l.EntryLocal(0) || l.Input(2, 1) != l.EntryLocal(1) || l.Output(2, 0) != l.Input(3, 0) || !l.Event(3).FunctionEnd {
				return r
			}
		} else if l.EventCount() != 2 || l.ValueCount() != 3 || l.Input(1, 0) != l.EntryLocal(0) || !l.Event(1).FunctionEnd {
			return r
		}
		for j := 1; j <= l.ValueCount(); j++ {
			if l.Value(wasm.SourceValueID(j)).Type != wasm.I32 {
				return r
			}
		}
	}
	d, covered := decodeSourceCallModuleAMD64(code)
	if !covered {
		return r
	}
	// No decoder location was selected from these maps or relocation metadata.
	for i := 0; i < 3; i++ {
		if entry[i] != d.entry[i] || internal[i] != d.internal[i] {
			return sourceCallMismatch("final function map differs from declaration-order certificate")
		}
	}
	if d.adapterTarget != d.internal[0] || d.callers[0].target != d.internal[2] || d.callers[1].target != d.internal[2] {
		return sourceCallMismatch("physical call target differs from original source function")
	}
	if d.invalid != "" {
		return sourceCallMismatch(d.invalid)
	}
	for i := 0; i < 2; i++ {
		if d.trapPC[i] != 0 {
			return sourceCallMismatch("fence trap PC differs from pre-instruction entry sentinel")
		}
	}
	// Certify the callee before assigning any caller result identity. Its entire
	// code contains only GP copies, zero SP adjustments and RET: no caller-frame
	// access, FP effect, LR/return-slot store, module mutation or nested call.
	l := ledgers[2]
	g := sourceCallGraphInputs(l)
	for _, in := range d.callee {
		sourceCallAppend(&g, in)
	}
	g.Blocks[0].Operations = append(g.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Use, Location: leafReg(0), Value: regalloccheck.ValueID(l.Input(1, 0)), Where: "certified identity callee return"})
	r = g.Verify(regalloccheck.Limits{Blocks: 1, Values: 4, Operations: 64, Facts: 1024, Work: work})
	work -= r.Work
	if r.Verdict != regalloccheck.Verified {
		return r
	}
	for i, c := range d.callers {
		l = ledgers[i]
		g = sourceCallGraphInputs(l)
		for _, in := range c.before {
			sourceCallAppend(&g, in)
		}
		for j := 0; j < 2; j++ {
			g.Blocks[0].Operations = append(g.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Use, Location: leafReg(uint8(j)), Value: regalloccheck.ValueID(l.Input(2, j)), Where: "physical call ABI input"})
		}
		g.Blocks[0].Operations = append(g.Blocks[0].Operations,
			regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: regalloccheck.Effect{Kind: regalloccheck.Call}},
			regalloccheck.Operation{Kind: regalloccheck.Define, Location: leafReg(0), Value: regalloccheck.ValueID(l.Output(2, 0)), Where: "independently certified callee result"})
		for _, in := range c.after {
			sourceCallAppend(&g, in)
		}
		g.Blocks[0].Operations = append(g.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Use, Location: leafReg(0), Value: regalloccheck.ValueID(l.Input(3, 0)), Where: "final source caller return"})
		r = g.Verify(regalloccheck.Limits{Blocks: 1, Values: 4, Operations: 64, Facts: 1024, Work: work})
		work -= r.Work
		if r.Verdict != regalloccheck.Verified {
			return r
		}
	}
	r.Message = "final-byte direct-call and identity-callee proof (no encoder journal)"
	return r
}

func sourceCallMismatch(message string) regalloccheck.Result {
	return regalloccheck.Result{Verdict: regalloccheck.Rejected, Reason: regalloccheck.ProvenanceMismatch, Message: message}
}

func sourceCallGraphInputs(l *wasm.SourceLedger) regalloccheck.Graph {
	g := regalloccheck.Graph{Widths: make([]uint8, l.ValueCount()), Blocks: make([]regalloccheck.Block, 1), Inputs: make([]regalloccheck.Binding, 2)}
	for i := range g.Widths {
		g.Widths[i] = 4
	}
	for i := range g.Inputs {
		g.Inputs[i] = regalloccheck.Binding{Location: leafReg(uint8(i)), Value: regalloccheck.ValueID(l.EntryLocal(i))}
	}
	return g
}
func sourceCallAppend(g *regalloccheck.Graph, in sourceBranchInstruction) {
	if in.copy {
		e := in.effect
		if e.Dst.Bank == regalloccheck.GP && e.Size == 4 {
			e.ClearTo = 8
		}
		g.Blocks[0].Operations = append(g.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: e})
	}
}

type sourceCallNativeAMD64 struct {
	before, after []sourceBranchInstruction
	target        int
}
type sourceCallModuleAMD64 struct {
	entry, internal [3]int
	adapterTarget   int
	trapPC          [2]uint32
	callers         [2]sourceCallNativeAMD64
	callee          []sourceBranchInstruction
	invalid         string
}

// Every byte of the adapter, two callers (including cold fence traps) and leaf
// is decoded here. Fixed runtime ABI inputs are RBX/module, its stack fence at
// -72, trap-cell pointer at -104 and saved trap-entry SP at -24. Trap stores
// function ordinal+1, pre-instruction entry sentinel PC0 and stack-fence code13,
// then restores that SP.
func decodeSourceCallModuleAMD64(code []byte) (sourceCallModuleAMD64, bool) {
	d := sourceCallModuleAMD64{}
	pc := 0
	match := func(s string) bool {
		if pc+len(s) > len(code) || string(code[pc:pc+len(s)]) != s {
			return false
		}
		pc += len(s)
		return true
	}
	u32 := func() (uint32, bool) {
		if pc+4 > len(code) {
			return 0, false
		}
		v := binary.LittleEndian.Uint32(code[pc : pc+4])
		pc += 4
		return v, true
	}
	rel32 := func() (int, bool) { v, ok := u32(); return pc + int(int32(v)), ok }
	// Complete adapter0 ABI: module from RSI; argument-buffer i32 slots0/8;
	// save output-buffer RCX across the call; store the 64-bit ABI result.
	if !match("\x48\x89\xf3\x51\x48\x8b\x07\x48\x8b\x4f\x08\xe8") {
		return d, false
	}
	var ok bool
	d.adapterTarget, ok = rel32()
	if !ok || !match("\x59\x48\x89\x01\xc3") {
		return d, false
	}
	copyInstruction := func(frame uint64) (sourceBranchInstruction, bool) {
		in, next, invalid, ok := decodeSourceTransferPinsAMD64(code, pc, len(code), frame, 0, 14)
		pc = next
		if invalid != "" {
			d.invalid = invalid
		}
		return in, ok
	}
	for i := 0; i < 2; i++ {
		d.internal[i] = pc
		if i == 1 {
			d.entry[i] = pc
		}
		if !match("\x48\x81\xec") {
			return d, false
		}
		frame, ok := u32()
		if !ok || frame != 24 {
			return d, false
		}
		if !match("\x48\x8b\x73\xb8\x48\x39\xf4\x0f\x82") {
			return d, false
		}
		trap, ok := rel32()
		if !ok {
			return d, false
		}
		c := &d.callers[i]
		c.before = make([]sourceBranchInstruction, 0, 8)
		c.after = make([]sourceBranchInstruction, 0, 2)
		for j := 0; j < 8; j++ {
			in, ok := copyInstruction(24)
			if !ok {
				return d, false
			}
			if j == 4 || j == 5 {
				if in.effect.Dst.Bank != regalloccheck.Frame || in.effect.Src.Bank != regalloccheck.GP {
					return d, false
				}
			} else if in.effect.Dst.Bank != regalloccheck.GP || in.effect.Src.Bank != regalloccheck.GP {
				return d, false
			}
			c.before = append(c.before, in)
		}
		if !match("\xe8") {
			return d, false
		}
		c.target, ok = rel32()
		if !ok {
			return d, false
		}
		// Bound post-call result carriers. Other valid aliases require a distinct
		// callee-preservation/result-alias proof and remain inconclusive here.
		if !match("\x48\x89\xc7\x89\xf8") {
			return d, false
		}
		c.after = append(c.after, sourceBranchInstruction{copy: true, effect: regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: leafReg(7), Src: leafReg(0), Size: 8}}, sourceBranchInstruction{copy: true, effect: regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: leafReg(0), Src: leafReg(7), Size: 4}})
		if !match("\x48\x81\xc4") {
			return d, false
		}
		restore, ok := u32()
		if !ok {
			return d, false
		}
		if restore != frame {
			d.invalid = "unbalanced final caller frame"
		}
		if !match("\xc3") {
			return d, false
		}
		if trap != pc {
			d.invalid = "stack fence target differs from certified trap entry"
		}
		if !match("\xb8") {
			return d, false
		}
		d.trapPC[i], ok = u32()
		if !ok || !match("\xe9\x00\x00\x00\x00\x48\x8b\x73\x98\xc7\x46\x10") {
			return d, false
		}
		ordinal, ok := u32()
		if !ok {
			return d, false
		}
		if ordinal != uint32(i+1) {
			d.invalid = "trap function ordinal differs from source declaration"
		}
		if !match("\x89\x46\x14\xc7\x06\x0d\x00\x00\x00\x48\x8b\x63\xe8\xc3") {
			return d, false
		}
	}
	d.entry[2], d.internal[2] = pc, pc
	if !match("\x48\x81\xec\x00\x00\x00\x00") {
		return d, false
	}
	d.callee = make([]sourceBranchInstruction, 0, 3)
	for i := 0; i < 3; i++ {
		in, ok := copyInstruction(0)
		if !ok || in.effect.Dst.Bank != regalloccheck.GP || in.effect.Src.Bank != regalloccheck.GP {
			return d, false
		}
		d.callee = append(d.callee, in)
	}
	if !match("\x48\x81\xc4\x00\x00\x00\x00\xc3") || pc != len(code) {
		return d, false
	}
	return d, true
}
