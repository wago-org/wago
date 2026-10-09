//go:build wago_regalloccheck

package wasm

type SourceControlKind uint8

const (
	SourceControlNone SourceControlKind = iota
	SourceControlBlock
	SourceControlLoop
	SourceControlIf
	SourceControlElse
	SourceControlEnd
)

// SourceBlock parameters are independent names. Only edge arguments establish
// them; entering a block never creates entry assumptions or semantic Defines.
// Locals precede stack parameters. The function exit has no local parameters.
type SourceBlock struct {
	LocalCount, StackCount  int
	Reachable, FunctionExit bool
	parameterStart          int
}

type SourceArm uint8

const (
	SourceFallthrough SourceArm = iota
	SourceThen
	SourceElse
	SourceBranch
	SourceBranchIfTrue
	SourceBranchIfFalse
	SourceTableCase
	SourceTableDefault
	SourceReturn
)

// SourceEdge contains only original semantic names, never physical carriers.
// Cases/default remain distinct even when their label destinations repeat.
type SourceEdge struct {
	From, To, Event, PC int
	Arm                 SourceArm
	Condition           SourceValueID
	Case                uint32
	ArgumentCount       int
	argumentStart       int
}

func (l *SourceLedger) BlockCount() int             { return len(l.blocks) }
func (l *SourceLedger) EdgeCount() int              { return len(l.edges) }
func (l *SourceLedger) Block(index int) SourceBlock { return l.blocks[index] }
func (l *SourceLedger) Edge(index int) SourceEdge   { return l.edges[index] }
func (l *SourceLedger) ExitBlock() int              { return l.exit }
func (l *SourceLedger) BlockParameter(block, index int) SourceValueID {
	p := l.blocks[block]
	if index < 0 || index >= p.LocalCount+p.StackCount {
		panic("source block parameter index")
	}
	return l.operands[p.parameterStart+index]
}
func (l *SourceLedger) EdgeArgument(edge, index int) SourceValueID {
	e := l.edges[edge]
	if index < 0 || index >= e.ArgumentCount {
		panic("source edge argument index")
	}
	return l.operands[e.argumentStart+index]
}

type sourceCFGInvariant struct{ message string }
type sourceControlFrame struct {
	kind                    ctrlKind
	height, inputs, outputs int
	label, exit, otherwise  int
	seenElse                bool
}
type sourceCFGBuilder struct {
	b          *sourceLedgerBuilder
	current    int
	dead       bool
	localTypes []ValType
	frames     []sourceControlFrame
}

func newSourceCFG(b *sourceLedgerBuilder, ft *CompType) *sourceCFGBuilder {
	g := &sourceCFGBuilder{b: b}
	b.charge(len(b.locals))
	g.localTypes = make([]ValType, len(b.locals))
	for i, id := range b.locals {
		g.localTypes[i] = b.ledger.Value(id).Type
	}
	if b.limits.Blocks < 2 || b.limits.ControlDepth < 1 {
		panic(sourceLedgerLimit{})
	}
	b.ledger.blocks = []SourceBlock{{LocalCount: len(b.locals), Reachable: true, parameterStart: b.appendOperands(b.locals)}}
	exit, ok := g.newBlock(nil, ft.Results, true)
	if !ok {
		panic(sourceCFGInvariant{"unsupported function exit type"})
	}
	b.ledger.exit = exit
	g.frames = []sourceControlFrame{{kind: ctrlFunc, label: exit, exit: exit, outputs: len(ft.Results)}}
	return g
}
func (g *sourceCFGBuilder) reachable() bool { return !g.dead && g.b.ledger.blocks[g.current].Reachable }
func (g *sourceCFGBuilder) newBlock(locals, stack []ValType, exit bool) (int, bool) {
	b := g.b
	for _, types := range [][]ValType{locals, stack} {
		for _, t := range types {
			b.charge(1)
			if !sourcePrimitive(t) {
				return 0, false
			}
		}
	}
	if len(b.ledger.blocks) == b.limits.Blocks || len(stack) > b.limits.Stack || len(locals)+len(stack) > b.limits.Values-len(b.ledger.values) || len(locals)+len(stack) > b.limits.Operands-len(b.ledger.operands) {
		panic(sourceLedgerLimit{})
	}
	block := SourceBlock{LocalCount: len(locals), StackCount: len(stack), FunctionExit: exit, parameterStart: len(b.ledger.operands)}
	for _, types := range [][]ValType{locals, stack} {
		for _, t := range types {
			id := b.value(t, SourceBlockParameter, 0)
			b.appendOperands([]SourceValueID{id})
		}
	}
	b.ledger.blocks = append(b.ledger.blocks, block)
	return len(b.ledger.blocks) - 1, true
}
func (g *sourceCFGBuilder) stackTypes() ([]ValType, bool) {
	b := g.b
	b.charge(len(b.v.vals))
	if len(b.v.vals) > b.limits.Stack {
		panic(sourceLedgerLimit{})
	}
	types := make([]ValType, len(b.v.vals))
	for i, v := range b.v.vals {
		if v.unknown || !sourcePrimitive(v.t) {
			return nil, false
		}
		types[i] = v.t
	}
	return types, true
}
func (g *sourceCFGBuilder) enter(block int) {
	b := g.b
	p := b.ledger.blocks[block]
	b.charge(p.LocalCount + p.StackCount)
	if p.LocalCount != 0 {
		if p.LocalCount != len(b.locals) {
			panic(sourceCFGInvariant{"source local parameter shape"})
		}
		for i := range b.locals {
			b.locals[i] = b.ledger.BlockParameter(block, i)
		}
	}
	b.stack = b.stack[:0]
	for i := 0; i < p.StackCount; i++ {
		b.stack = append(b.stack, b.ledger.BlockParameter(block, p.LocalCount+i))
	}
	g.current = block
	g.dead = !p.Reachable
}
func (g *sourceCFGBuilder) deadStack() {
	b := g.b
	if len(b.v.vals) > b.limits.Stack {
		panic(sourceLedgerLimit{})
	}
	b.charge(len(b.v.vals))
	b.stack = b.stack[:0]
	for range b.v.vals {
		b.stack = append(b.stack, 0)
	}
}
func (g *sourceCFGBuilder) edge(to int, arm SourceArm, condition SourceValueID, ordinal uint32, prefix, payload []SourceValueID) {
	if !g.reachable() {
		return
	}
	b := g.b
	target := b.ledger.blocks[to]
	if len(b.ledger.edges) == b.limits.Edges {
		panic(sourceLedgerLimit{})
	}
	count := target.LocalCount + len(prefix) + len(payload)
	if len(prefix)+len(payload) != target.StackCount || (target.LocalCount != 0 && target.LocalCount != len(b.locals)) {
		panic(sourceCFGInvariant{"source edge shape"})
	}
	if count > b.limits.Operands-len(b.ledger.operands) {
		panic(sourceLedgerLimit{})
	}
	start := len(b.ledger.operands)
	if target.LocalCount != 0 {
		b.appendOperands(b.locals)
	}
	b.appendOperands(prefix)
	b.appendOperands(payload)
	for i := 0; i < count; i++ {
		b.charge(1)
		from := b.ledger.operands[start+i]
		to := b.ledger.BlockParameter(to, i)
		if from == 0 || b.ledger.Value(from).Type != b.ledger.Value(to).Type {
			panic(sourceCFGInvariant{"source edge identity/type"})
		}
	}
	b.ledger.edges = append(b.ledger.edges, SourceEdge{From: g.current, To: to, Event: len(b.ledger.events), PC: b.pc, Arm: arm, Condition: condition, Case: ordinal, ArgumentCount: count, argumentStart: start})
	b.ledger.blocks[to].Reachable = true
}
func (g *sourceCFGBuilder) recordInputs(e *SourceEvent, ids []SourceValueID) {
	if !g.reachable() {
		return
	}
	e.InputCount = len(ids)
	e.inputStart = g.b.appendOperands(ids)
}
func (g *sourceCFGBuilder) label(index uint32) sourceControlFrame {
	if uint64(index) >= uint64(len(g.frames)) {
		panic(sourceCFGInvariant{"source label depth"})
	}
	return g.frames[len(g.frames)-1-int(index)]
}
func (g *sourceCFGBuilder) branch(target sourceControlFrame, arm SourceArm, condition SourceValueID, ordinal uint32) {
	if !g.reachable() {
		return
	}
	arity := target.outputs
	if target.kind == ctrlLoop {
		arity = target.inputs
	}
	stack := g.b.stack
	if arity > len(stack) || target.height > len(stack)-arity {
		panic(sourceCFGInvariant{"source branch stack"})
	}
	g.edge(target.label, arm, condition, ordinal, stack[:target.height], stack[len(stack)-arity:])
}

// control validates every opcode through the authoritative validator, including
// dead lexical code. Its reachability and value identities are separate from
// the validator's lexical unreachable/type-polymorphic state.
func (g *sourceCFGBuilder) control(op *directOp, e *SourceEvent) (handled, supported bool, err error) {
	b := g.b
	v := b.v
	switch op.kind {
	case directBlock, directLoop, directIf:
		e.BlockType = op.blockType
		switch op.kind {
		case directBlock:
			e.Kind = InstrBlock
			e.Control = SourceControlBlock
		case directLoop:
			e.Kind = InstrLoop
			e.Control = SourceControlLoop
		case directIf:
			e.Kind = InstrIf
			e.Control = SourceControlIf
		}
		if len(g.frames) == b.limits.ControlDepth {
			panic(sourceLedgerLimit{})
		}
		ins, outs, err := v.blockSig(op.blockType)
		if err != nil {
			return true, false, err
		}
		for _, ts := range [][]ValType{ins, outs} {
			for _, t := range ts {
				b.charge(1)
				if !sourcePrimitive(t) {
					return true, false, nil
				}
			}
		}
		// Polymorphic pops at the active frame's bottom do not reduce the
		// validator stack. Bound the actual post-entry height before pushCtrl
		// can materialize a large indexed signature, even on a dead source path.
		available := len(v.vals)
		if op.kind == directIf && available > v.top().height {
			available--
		}
		base := max(v.top().height, available-len(ins))
		if len(ins) > b.limits.Stack-base {
			panic(sourceLedgerLimit{})
		}
		b.charge(2*len(ins) + 1)
		condition := SourceValueID(0)
		if op.kind == directIf && g.reachable() {
			if len(b.stack) == 0 {
				panic(sourceCFGInvariant{"source if condition"})
			}
			condition = b.stack[len(b.stack)-1]
			g.recordInputs(e, b.stack[len(b.stack)-1:])
			b.stack = b.stack[:len(b.stack)-1]
		}
		if err := v.stepDirectOp(op); err != nil {
			return true, false, err
		}
		types, ok := g.stackTypes()
		if !ok || len(types) < len(ins) {
			return true, false, nil
		}
		height := len(types) - len(ins)
		if len(outs) > b.limits.Stack-height {
			panic(sourceLedgerLimit{})
		}
		b.charge(height + len(outs))
		exitTypes := make([]ValType, 0, height+len(outs))
		exitTypes = append(exitTypes, types[:height]...)
		exitTypes = append(exitTypes, outs...)
		exit, ok := g.newBlock(g.localTypes, exitTypes, false)
		if !ok {
			return true, false, nil
		}
		frame := sourceControlFrame{kind: ctrlBlock, height: height, inputs: len(ins), outputs: len(outs), label: exit, exit: exit, otherwise: -1}
		switch op.kind {
		case directLoop:
			frame.kind = ctrlLoop
			header, ok := g.newBlock(g.localTypes, types, false)
			if !ok {
				return true, false, nil
			}
			g.edge(header, SourceFallthrough, 0, 0, nil, b.stack)
			frame.label = header
			g.enter(header)
		case directIf:
			frame.kind = ctrlIf
			thenBlock, ok := g.newBlock(g.localTypes, types, false)
			if !ok {
				return true, false, nil
			}
			elseBlock, ok := g.newBlock(g.localTypes, types, false)
			if !ok {
				return true, false, nil
			}
			g.edge(thenBlock, SourceThen, condition, 0, nil, b.stack)
			g.edge(elseBlock, SourceElse, condition, 0, nil, b.stack)
			frame.otherwise = elseBlock
			g.enter(thenBlock)
		default:
			if !g.reachable() {
				g.deadStack()
			}
		}
		g.frames = append(g.frames, frame)
		return true, true, nil
	case directElse:
		e.Control = SourceControlElse
		if len(g.frames) == 0 || g.frames[len(g.frames)-1].kind != ctrlIf {
			return true, false, v.stepDirectOp(op)
		}
		frame := &g.frames[len(g.frames)-1]
		b.charge(2*frame.outputs + frame.inputs)
		if err := v.stepDirectOp(op); err != nil {
			return true, false, err
		}
		if g.reachable() {
			g.edge(frame.exit, SourceFallthrough, 0, 0, b.stack[:frame.height], b.stack[len(b.stack)-frame.outputs:])
		}
		frame.seenElse = true
		g.enter(frame.otherwise)
		return true, true, nil
	case directEnd:
		e.Control = SourceControlEnd
		if len(g.frames) == 0 {
			return true, false, v.stepDirectOp(op)
		}
		frame := g.frames[len(g.frames)-1]
		b.charge(2 * frame.outputs)
		e.FunctionEnd = frame.kind == ctrlFunc
		e.Terminal = e.FunctionEnd
		if g.reachable() {
			if frame.outputs > len(b.stack) || frame.height > len(b.stack)-frame.outputs {
				panic(sourceCFGInvariant{"source end stack"})
			}
			g.recordInputs(e, b.stack[len(b.stack)-frame.outputs:])
		}
		if err := v.stepDirectOp(op); err != nil {
			return true, false, err
		}
		if g.reachable() {
			g.edge(frame.exit, SourceFallthrough, 0, 0, b.stack[:frame.height], b.stack[len(b.stack)-frame.outputs:])
		}
		if frame.kind == ctrlIf && !frame.seenElse {
			g.enter(frame.otherwise)
			if g.reachable() {
				g.edge(frame.exit, SourceFallthrough, 0, 0, b.stack[:frame.height], b.stack[len(b.stack)-frame.outputs:])
			}
		}
		g.frames = g.frames[:len(g.frames)-1]
		g.enter(frame.exit)
		return true, true, nil
	case directInstr:
		switch op.instr.Kind {
		case InstrUnreachable:
			e.Terminal = true
			if err := v.stepDirectOp(op); err != nil {
				return true, false, err
			}
			g.dead = true
			g.deadStack()
			return true, true, nil
		case InstrBr, InstrBrIf, InstrBrTable, InstrReturn:
		default:
			return false, false, nil
		}
		kind := op.instr.Kind
		condition := SourceValueID(0)
		target := g.frames[0]
		if kind == InstrBr || kind == InstrBrIf {
			if _, err := v.label(op.instr.Index); err != nil {
				return true, false, err
			}
			target = g.label(op.instr.Index)
		}
		if (kind == InstrBrIf || kind == InstrBrTable) && g.reachable() {
			if len(b.stack) == 0 {
				panic(sourceCFGInvariant{"source branch condition"})
			}
			condition = b.stack[len(b.stack)-1]
			g.recordInputs(e, b.stack[len(b.stack)-1:])
			b.stack = b.stack[:len(b.stack)-1]
		}
		if kind == InstrBr || kind == InstrReturn {
			arity := target.outputs
			if target.kind == ctrlLoop {
				arity = target.inputs
			}
			if g.reachable() {
				if arity > len(b.stack) {
					panic(sourceCFGInvariant{"source branch payload"})
				}
				g.recordInputs(e, b.stack[len(b.stack)-arity:])
			}
		}
		if kind == InstrBrTable {
			for _, label := range op.instr.Indices() {
				types, err := v.label(label)
				if err != nil {
					return true, false, err
				}
				b.charge(len(types) + 1)
			}
			types, err := v.label(op.instr.Index)
			if err != nil {
				return true, false, err
			}
			b.charge(len(types) + 1)
		} else {
			arity := target.outputs
			if target.kind == ctrlLoop {
				arity = target.inputs
			}
			b.charge(2*arity + 1)
		}
		if err := v.stepDirectOp(op); err != nil {
			return true, false, err
		}
		switch kind {
		case InstrBr:
			g.branch(target, SourceBranch, 0, 0)
			e.Terminal = true
			g.dead = true
			g.deadStack()
		case InstrReturn:
			g.branch(target, SourceReturn, 0, 0)
			e.Terminal = true
			g.dead = true
			g.deadStack()
		case InstrBrIf:
			g.branch(target, SourceBranchIfTrue, condition, 0)
			types, ok := g.stackTypes()
			if !ok {
				return true, false, nil
			}
			continuation, ok := g.newBlock(g.localTypes, types, false)
			if !ok {
				return true, false, nil
			}
			g.edge(continuation, SourceBranchIfFalse, condition, 0, nil, b.stack)
			g.enter(continuation)
		case InstrBrTable:
			for i, label := range op.instr.Indices() {
				b.charge(1)
				g.branch(g.label(label), SourceTableCase, condition, uint32(i))
			}
			g.branch(g.label(op.instr.Index), SourceTableDefault, condition, uint32(len(op.instr.Indices())))
			e.Terminal = true
			g.dead = true
			g.deadStack()
		}
		return true, true, nil
	default:
		return true, false, nil
	}
}
