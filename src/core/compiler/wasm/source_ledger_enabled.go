//go:build wago_regalloccheck

package wasm

import (
	"encoding/binary"
	"github.com/wago-org/wago/internal/regalloccheck"
)

// SourceValueID identifies an original Wasm definition or alias. It never names
// an allocator owner. IDs are one based and private to one SourceLedger.
type SourceValueID uint32

type SourceValueKind uint8

const (
	SourceParameter SourceValueKind = iota
	SourceConstant
	SourceResult
	SourceBlockParameter
)

// SourceValue is pointer free. Constants retain exact bits, including vector
// high halves, signed zero and NaN payloads. Scalars always have BitsHigh zero.
type SourceValue struct {
	Type           ValType
	Kind           SourceValueKind
	Bits, BitsHigh uint64
}

// SourceEvent retains source coordinates relative to Func.BodyBytes, before
// local-declaration bytes. EndPC is exclusive, including prefixed opcodes and
// every encoded immediate. Inputs are in original stack order, with an indirect
// call selector last; outputs are in declared result order.
// Unreachable describes the current lexical path. A structured end can also
// close a separately reachable false edge; consumers must inspect source edges.
type SourceEvent struct {
	PC, EndPC               int
	Kind                    InstrKind
	Index, Index2           uint32
	MemoryOffset            uint64
	MemoryAlign             uint32
	Lane                    LaneIdx
	Lanes                   [16]LaneIdx // copied shuffle immediate, never decoder scratch
	InputCount, OutputCount int
	FunctionEnd, Terminal   bool
	Block                   int
	Unreachable             bool
	Control                 SourceControlKind
	BlockType               BlockType
	inputStart, outputStart int
}

// SourceCoverage describes only construction of source contracts. Even Complete
// is not a regalloccheck.Verified machine-code result.
type SourceCoverage uint8

const (
	SourceIncomplete SourceCoverage = iota
	SourceContractsComplete
	SourceInvalid
)

type SourceLedgerResult struct {
	Coverage SourceCoverage
	Reason   regalloccheck.FailureReason
	PC       int
	Message  string
	Work     int
}

// SourceLedgerLimits also bound decode/type-resolution input before allocation.
// Zero selects a default; positive overrides can only lower the defaults.
type SourceLedgerLimits struct {
	BodyBytes, Metadata, Locals, Events, Values, Operands, Stack, Work int
	Blocks, Edges, ControlDepth                                        int
}

func DefaultSourceLedgerLimits() SourceLedgerLimits {
	return SourceLedgerLimits{BodyBytes: 1 << 20, Metadata: 1 << 18,
		Locals: 65535, Events: 262144, Values: 65536, Operands: 524288,
		Stack: 65536, Work: 4 << 20, Blocks: 4096, Edges: 16384, ControlDepth: 1024}
}

// SourceLedger owns immutable pools for one function. Keep m and all nested
// metadata immutable from validation through emission, just as required by
// ValidatedModuleAnalysis. Consumers must Close on every completion/panic path.
// No source body, validator scratch, or allocator object is stored in the pools.
type SourceLedger struct {
	module     *Module
	function   int
	features   ValidationFeatures
	values     []SourceValue
	events     []SourceEvent
	operands   []SourceValueID
	entryLocal []SourceValueID
	blocks     []SourceBlock
	edges      []SourceEdge
	exit       int
}

func (l *SourceLedger) ValidFor(m *Module, localFunction int, features ValidationFeatures) bool {
	return l != nil && m != nil && l.module == m && l.function == localFunction && l.features == features
}
func (l *SourceLedger) ValueCount() int { return len(l.values) }
func (l *SourceLedger) EventCount() int { return len(l.events) }
func (l *SourceLedger) LocalCount() int { return len(l.entryLocal) }
func (l *SourceLedger) Value(id SourceValueID) SourceValue {
	return l.values[int(id)-1]
}
func (l *SourceLedger) Event(index int) SourceEvent { return l.events[index] }
func (l *SourceLedger) Input(event, index int) SourceValueID {
	e := l.events[event]
	if index < 0 || index >= e.InputCount {
		panic("source ledger input index")
	}
	return l.operands[e.inputStart+index]
}
func (l *SourceLedger) Output(event, index int) SourceValueID {
	e := l.events[event]
	if index < 0 || index >= e.OutputCount {
		panic("source ledger output index")
	}
	return l.operands[e.outputStart+index]
}
func (l *SourceLedger) EntryLocal(index int) SourceValueID { return l.entryLocal[index] }
func (l *SourceLedger) Close() {
	if l != nil {
		*l = SourceLedger{}
	}
}

type sourceLedgerLimit struct{}
type sourceLiteral struct {
	typ        ValType
	bits, high uint64
}
type sourceLedgerBuilder struct {
	ledger        *SourceLedger
	v             *funcValidator
	limits        SourceLedgerLimits
	work, pc      int
	stack, locals []SourceValueID
	literals      map[sourceLiteral]SourceValueID
	cfg           *sourceCFGBuilder
}

func (b *sourceLedgerBuilder) charge(n int) {
	if n < 0 || n > b.limits.Work-b.work {
		panic(sourceLedgerLimit{})
	}
	b.work += n
}
func (b *sourceLedgerBuilder) value(t ValType, kind SourceValueKind, bits uint64) SourceValueID {
	b.charge(1)
	if len(b.ledger.values) == b.limits.Values {
		panic(sourceLedgerLimit{})
	}
	b.ledger.values = append(b.ledger.values, SourceValue{Type: t, Kind: kind, Bits: bits})
	return SourceValueID(len(b.ledger.values))
}
func (b *sourceLedgerBuilder) literal(t ValType, bits uint64) SourceValueID {
	return b.literalBits(t, bits, 0)
}

func (b *sourceLedgerBuilder) literalBits(t ValType, bits, high uint64) SourceValueID {
	key := sourceLiteral{t, bits, high}
	if id := b.literals[key]; id != 0 {
		b.charge(1)
		return id
	}
	id := b.value(t, SourceConstant, bits)
	b.ledger.values[int(id)-1].BitsHigh = high
	b.literals[key] = id
	return id
}
func (b *sourceLedgerBuilder) appendOperands(ids []SourceValueID) int {
	b.charge(len(ids))
	if len(ids) > b.limits.Operands-len(b.ledger.operands) {
		panic(sourceLedgerLimit{})
	}
	start := len(b.ledger.operands)
	b.ledger.operands = append(b.ledger.operands, ids...)
	return start
}
func sourcePrimitive(t ValType) bool {
	return t == I32 || t == I64 || t == F32 || t == F64 || t == V128
}

func sourceLimits(request SourceLedgerLimits) (SourceLedgerLimits, bool) {
	l := DefaultSourceLedgerLimits()
	for _, p := range [][2]*int{{&l.BodyBytes, &request.BodyBytes}, {&l.Metadata, &request.Metadata},
		{&l.Locals, &request.Locals}, {&l.Events, &request.Events}, {&l.Values, &request.Values},
		{&l.Operands, &request.Operands}, {&l.Stack, &request.Stack}, {&l.Work, &request.Work}} {
		if *p[1] < 0 {
			return l, false
		}
		if *p[1] > 0 {
			*p[0] = min(*p[0], *p[1])
		}
	}
	for _, p := range [][2]*int{{&l.Blocks, &request.Blocks}, {&l.Edges, &request.Edges}, {&l.ControlDepth, &request.ControlDepth}} {
		if *p[1] < 0 {
			return l, false
		}
		if *p[1] > 0 {
			*p[0] = min(*p[0], *p[1])
		}
	}
	return l, true
}

// BuildSourceLedger reuses the authoritative direct decoder and funcValidator
// type semantics. The checked-only identity layer classifies aliases and arity;
// stack-height differences alone do not establish consumed values. The current
// admission is scalar/SIMD numeric code, core structured control, and ordinary
// direct/indirect calls. Unsupported polymorphic block types, tail calls,
// references, and proposal instructions return SourceIncomplete with no ledger.
// features must be the same profile used to produce analysis; analysis itself
// stores module identity, not the validation feature/limit configuration.
// Successful source construction still needs independent physical observations
// and ABI/CFG mapping before any machine-code verification can succeed.
func BuildSourceLedger(m *Module, analysis *ValidatedModuleAnalysis, localFunction int,
	features ValidationFeatures, request SourceLedgerLimits) (ledger *SourceLedger, result SourceLedgerResult, validationErr error) {
	result.PC = -1
	limits, ok := sourceLimits(request)
	if !ok {
		result.Coverage = SourceInvalid
		result.Reason = regalloccheck.InvalidGraph
		result.Message = "negative source ledger limit"
		return
	}
	if !analysis.ValidFor(m) || localFunction < 0 || localFunction >= len(m.Code) {
		result.Coverage = SourceInvalid
		result.Reason = regalloccheck.InvalidGraph
		result.Message = "missing immutable validated module context"
		return
	}
	b := &sourceLedgerBuilder{limits: limits, pc: -1}
	defer func() {
		result.Work = b.work
		if failure := recover(); failure != nil {
			if invalid, ok := failure.(sourceCFGInvariant); ok {
				ledger = nil
				result = SourceLedgerResult{Coverage: SourceInvalid, Reason: regalloccheck.InvalidGraph, PC: b.pc, Message: invalid.message, Work: b.work}
			} else if _, limited := failure.(sourceLedgerLimit); !limited {
				panic(failure)
			} else {
				ledger = nil
				result = SourceLedgerResult{Coverage: SourceIncomplete, Reason: regalloccheck.ResourceLimit,
					PC: b.pc, Message: "source ledger construction limit", Work: b.work}
			}
		}
		if validationErr != nil {
			result.Coverage = SourceInvalid
			result.Reason = regalloccheck.InvalidGraph
			result.Message = validationErr.Error()
		}
		if result.Coverage != SourceContractsComplete {
			ledger = nil
		}
	}()
	fn := &m.Code[localFunction]
	if len(fn.BodyBytes) == 0 {
		result.Reason = regalloccheck.UnsupportedOperation
		result.Message = "tree-only source body"
		return
	}
	if len(fn.BodyBytes) > limits.BodyBytes {
		panic(sourceLedgerLimit{})
	}
	b.charge(len(fn.BodyBytes))
	// Bound every collection the private type/import environment can traverse.
	// Do this before it allocates directories or resolves parameter/result slices.
	metadata := 0
	addMetadata := func(n int) {
		if n > limits.Metadata-metadata {
			panic(sourceLedgerLimit{})
		}
		metadata += n
		b.charge(n)
	}
	for _, n := range []int{len(m.Types), len(m.Imports), len(m.FuncTypes), len(m.Code), len(m.Tables), len(m.Memories), len(m.Globals), len(m.Tags), len(m.Elements), len(m.Data)} {
		addMetadata(n)
	}
	for i := range m.Types {
		addMetadata(len(m.Types[i].SubTypes))
		for j := range m.Types[i].SubTypes {
			st := &m.Types[i].SubTypes[j]
			addMetadata(len(st.Supers))
			addMetadata(len(st.Comp.Params))
			addMetadata(len(st.Comp.Results))
			addMetadata(len(st.Comp.Fields))
		}
	}
	addMetadata(len(fn.Locals.Runs))
	owner := moduleValidator{m: m, features: features, limits: defaultValidationLimits}
	owner.ensureImportIndexes()
	if (!features.MultiMemory && len(owner.importIndexes[ExternMem])+len(m.Memories) > 1) ||
		(m.UsesCompactImports && !features.CompactImports) {
		result.Coverage = SourceInvalid
		result.Reason = regalloccheck.InvalidGraph
		result.Message = "source feature profile disagrees with validated module"
		return
	}
	abs := len(owner.importIndexes[ExternFunc]) + localFunction
	ft, found := owner.funcType(uint32(abs))
	if !found {
		result.Coverage = SourceInvalid
		result.Reason = regalloccheck.InvalidGraph
		result.Message = "source function type"
		return
	}
	count, overflow := LocalCount(ft.Params, fn.Locals.Runs)
	if overflow || count > uint64(limits.Locals) {
		panic(sourceLedgerLimit{})
	}
	b.charge(int(count))
	// Use the validator's indexed local lookup instead of rescanning every run
	// for every local. Charge its directory before it can be allocated.
	v := funcValidator{moduleValidator: &owner, localParams: ft.Params, localRuns: fn.Locals.Runs, localCount: count}
	v.beginFunc(abs)
	v.prepareLocalLookup()
	if len(fn.Locals.Runs) > 2 {
		b.charge(len(fn.Locals.Runs))
		v.indexLocalRuns()
	}
	v.resetLocalInitialization()
	b.v = &v
	b.ledger = &SourceLedger{module: m, function: localFunction, features: features}
	b.locals = make([]SourceValueID, int(count))
	b.literals = make(map[sourceLiteral]SourceValueID)
	for i := range b.locals {
		b.charge(1)
		t, found := v.localType(uint32(i))
		if !found || !sourcePrimitive(t) {
			result.Reason = regalloccheck.UnsupportedOperation
			result.Message = "unsupported entry local type"
			return
		}
		if i < len(ft.Params) {
			b.locals[i] = b.value(t, SourceParameter, 0)
		} else {
			b.locals[i] = b.literal(t, 0)
		}
	}
	for _, t := range ft.Results {
		b.charge(1)
		if !sourcePrimitive(t) {
			result.Reason = regalloccheck.UnsupportedOperation
			result.Message = "unsupported result type"
			return
		}
	}
	b.ledger.entryLocal = append([]SourceValueID(nil), b.locals...)
	// This environment is private to the builder; no validator or module cache
	// is mutated concurrently with another function's ledger construction.
	if err := v.pushCtrl(ctrlFunc, nil, ft.Results); err != nil {
		validationErr = err
		result.Coverage = SourceInvalid
		return
	}
	v.rd.reset(fn.BodyBytes)
	// Admitted vector-bearing immediates use the remaining metadata allowance.
	// Bound even transient decoding storage by the remaining
	// metadata allowance, rather than the decoder's ordinary default budget.
	decodeBytes := uint64(limits.Metadata-metadata) * 16
	v.rd.budget = &decodeBudget{remaining: decodeBytes, limits: DecodeLimits{MaxMetadataBytes: decodeBytes}}
	widths := moduleMemargWidths(m)
	b.cfg = newSourceCFG(b, ft)
	for len(v.ctrls) != 0 {
		b.pc = v.rd.off()
		result.PC = b.pc
		b.charge(1)
		if len(b.ledger.events) == limits.Events {
			panic(sourceLedgerLimit{})
		}
		if opcode, has := v.rd.peek(); has && sourceUnsupportedOpcode(opcode) {
			result.Reason = regalloccheck.UnsupportedOperation
			result.Message = "source opcode requires control/proposal contracts"
			return
		}
		if opcode, _ := v.rd.peek(); opcode == 0x0e {
			// Check a table's arm count before the authoritative decoder reserves
			// its transient label vector. The default arm also consumes an edge.
			probe := v.rd
			_, _ = probe.byte()
			count, err := probe.u32()
			if err != nil {
				validationErr = err
				result.Coverage = SourceInvalid
				return
			}
			if uint64(count)+1 > uint64(limits.Edges-len(b.ledger.edges)) {
				panic(sourceLedgerLimit{})
			}
			budget := *v.rd.budget
			budgetProbe := reader{budget: &budget}
			if reserveDecodedSlice[uint32](&budgetProbe, count) != nil {
				panic(sourceLedgerLimit{})
			}
		}
		if opcode, _ := v.rd.peek(); opcode == 0x1c {
			// Validated typed select has exactly one type. Probe the same decoder
			// reservation using a private budget copy, before any vector allocation.
			budget := *v.rd.budget
			probe := reader{budget: &budget}
			if reserveDecodedSlice[ValType](&probe, 1) != nil {
				panic(sourceLedgerLimit{})
			}
		}
		var op directOp
		if err := v.decodeDirectOp(&v.rd, widths, features.MultiMemory, &op); err != nil {
			validationErr = err
			result.Coverage = SourceInvalid
			return
		}
		e := SourceEvent{PC: b.pc, EndPC: v.rd.off(), Kind: op.instr.Kind, Index: op.instr.Index, Index2: op.instr.Index2,
			Block: b.cfg.current, Unreachable: !b.cfg.reachable(), Lane: op.instr.Lane}
		if op.instr.Kind == InstrI8x16Shuffle {
			b.charge(16)
			e.Lanes = op.instr.Lanes()
		}
		if sourceMemoryInstruction(op.instr.Kind) {
			ma := op.instr.MemArg()
			e.MemoryOffset = ma.Offset
			e.MemoryAlign = ma.Align
			if ma.Mem != nil {
				e.Index = uint32(*ma.Mem)
			}
		}
		if handled, supported, err := b.cfg.control(&op, &e); handled {
			if err != nil {
				validationErr = err
				result.Coverage = SourceInvalid
				return
			}
			if !supported {
				result.Reason = regalloccheck.UnsupportedOperation
				result.Message = "unsupported source control types"
				return
			}
			b.ledger.events = append(b.ledger.events, e)
			continue
		}
		if !b.cfg.reachable() {
			inputs, outputs, _, supported := b.signature(&op.instr, ft)
			if !supported && (op.instr.Kind == InstrLocalGet || op.instr.Kind == InstrLocalSet || op.instr.Kind == InstrLocalTee) {
				if err := v.stepDirectOp(&op); err != nil {
					validationErr = err
					result.Coverage = SourceInvalid
					return
				}
			}
			if !supported {
				result.Reason = regalloccheck.UnsupportedOperation
				result.Message = "unsupported dead source instruction"
				return
			}
			base := max(v.top().height, len(v.vals)-inputs)
			if outputs > limits.Stack-base {
				panic(sourceLedgerLimit{})
			}
			if err := v.stepDirectOp(&op); err != nil {
				validationErr = err
				result.Coverage = SourceInvalid
				return
			}
			b.cfg.deadStack()
			b.ledger.events = append(b.ledger.events, e)
			continue
		}
		inputs, outputs, alias, supported := b.signature(&op.instr, ft)
		if !supported {
			result.Reason = regalloccheck.UnsupportedOperation
			result.Message = "unsupported source instruction: " + op.instr.Kind.String()
			return
		}
		if inputs > len(b.stack) {
			result.Coverage = SourceInvalid
			result.Reason = regalloccheck.InvalidGraph
			result.Message = "source identity stack underflow"
			return
		}
		base := len(b.stack) - inputs
		// Reserve before stepDirectOp can push a large multi-result call into the
		// validator stack, or any operand/result pool can grow.
		if outputs > limits.Stack-base || inputs > limits.Operands-len(b.ledger.operands) ||
			outputs > limits.Operands-len(b.ledger.operands)-inputs {
			panic(sourceLedgerLimit{})
		}
		e.InputCount = inputs
		e.OutputCount = outputs
		e.inputStart = b.appendOperands(b.stack[base:])
		if err := v.stepDirectOp(&op); err != nil {
			validationErr = err
			result.Coverage = SourceInvalid
			return
		}
		{
			if len(v.vals) != base+outputs {
				result.Coverage = SourceInvalid
				result.Reason = regalloccheck.InvalidGraph
				result.Message = "source signature disagrees with validator"
				return
			}
			for i := 0; i < base; i++ {
				b.charge(1)
				if v.vals[i].unknown || v.vals[i].t != b.ledger.Value(b.stack[i]).Type {
					result.Coverage = SourceInvalid
					result.Reason = regalloccheck.InvalidGraph
					result.Message = "source prefix disagrees with validator"
					return
				}
			}
			var preserved SourceValueID
			if alias {
				if op.instr.Kind == InstrLocalGet {
					preserved = b.locals[op.instr.Index]
				} else {
					preserved = b.stack[base]
				}
			}
			if op.instr.Kind == InstrLocalSet || op.instr.Kind == InstrLocalTee {
				b.locals[op.instr.Index] = b.stack[base]
			}
			b.stack = b.stack[:base]
			if outputs > limits.Stack-len(b.stack) {
				panic(sourceLedgerLimit{})
			}
			e.outputStart = len(b.ledger.operands)
			for i := 0; i < outputs; i++ {
				t := v.vals[base+i].t
				if v.vals[base+i].unknown || !sourcePrimitive(t) {
					result.Reason = regalloccheck.UnsupportedOperation
					result.Message = "unsupported or polymorphic result"
					return
				}
				id := preserved
				if id == 0 {
					if op.instr.Kind == InstrV128Const {
						b.charge(16)
					}
					if bits, high, constant := sourceConstant(&op.instr); constant {
						id = b.literalBits(t, bits, high)
					} else {
						id = b.value(t, SourceResult, 0)
					}
				} else if b.ledger.Value(id).Type != t {
					result.Coverage = SourceInvalid
					result.Reason = regalloccheck.InvalidGraph
					result.Message = "source alias type mismatch"
					return
				}
				b.stack = append(b.stack, id)
				b.appendOperands([]SourceValueID{id})
			}
		}
		b.ledger.events = append(b.ledger.events, e)
	}
	if v.rd.has() {
		result.Coverage = SourceInvalid
		result.Reason = regalloccheck.InvalidGraph
		result.Message = "trailing source bytes"
		return
	}
	ledger = b.ledger
	result.Coverage = SourceContractsComplete
	result.Reason = regalloccheck.NoFailure
	result.Message = "source contracts complete; physical mapping required"
	return
}

func sourceUnsupportedOpcode(opcode byte) bool {
	switch opcode {
	case 0x08, 0x12, 0x13, 0x14, 0x15, 0x1f, 0xfb, 0xfe:
		return true
	default:
		return false
	}
}

func sourceConstant(in *Instruction) (uint64, uint64, bool) {
	switch in.Kind {
	case InstrI32Const:
		return uint64(uint32(in.I32)), 0, true
	case InstrI64Const:
		return uint64(in.I64), 0, true
	case InstrF32Const:
		return uint64(in.F32Bits), 0, true
	case InstrF64Const:
		return in.F64Bits, 0, true
	case InstrV128Const:
		lanes := in.Lanes()
		var bits [16]byte
		for i, lane := range lanes {
			bits[i] = byte(lane)
		}
		return binary.LittleEndian.Uint64(bits[:8]), binary.LittleEndian.Uint64(bits[8:]), true
	default:
		return 0, 0, false
	}
}

func sourceMemoryInstruction(kind InstrKind) bool {
	if int(kind) >= len(opEffects) {
		return false
	}
	if e := opEffects[kind]; e.cat == effLoad || e.cat == effStore {
		return true
	}
	switch simdEffects[kind].cat {
	case simdEffLoad, simdEffStore, simdEffMemLoadLane, simdEffMemStoreLane:
		return true
	}
	return false
}

// signature uses the validator's authoritative primitive effects. Explicit
// cases describe original alias/call semantics, not allocator state transitions.
func (b *sourceLedgerBuilder) signature(in *Instruction, ft *CompType) (inputs, outputs int, alias, supported bool) {
	if int(in.Kind) >= len(opEffects) {
		return
	}
	switch simdEffects[in.Kind].cat {
	case simdEffLoad, simdEffSplat, simdEffExtract, simdEffUnary, simdPopV128PushI32:
		return 1, 1, false, true
	case simdEffStore, simdEffMemStoreLane:
		return 2, 0, false, true
	case simdEffMemLoadLane, simdEffReplace, simdEffShift, simdEffBinary:
		return 2, 1, false, true
	case simdEffTernary, simdBitselect:
		return 3, 1, false, true
	case simdConst:
		return 0, 1, false, true
	case simdNone:
		// Only non-SIMD instructions may use the scalar/control rules below.
	default:
		return
	}
	switch opEffects[in.Kind].cat {
	case effUnary, effTest, effConv, effLoad:
		return 1, 1, false, true
	case effBinary, effCompare:
		return 2, 1, false, true
	case effStore:
		return 2, 0, false, true
	}
	switch in.Kind {
	case InstrNop:
		return 0, 0, false, true
	case InstrI32Const, InstrI64Const, InstrF32Const, InstrF64Const:
		return 0, 1, false, true
	case InstrDrop:
		return 1, 0, false, true
	case InstrSelect:
		return 3, 1, false, true
	case InstrLocalGet:
		if uint64(in.Index) >= uint64(len(b.locals)) {
			return
		}
		return 0, 1, true, true
	case InstrLocalSet, InstrLocalTee:
		if uint64(in.Index) >= uint64(len(b.locals)) {
			return
		}
		if in.Kind == InstrLocalTee {
			return 1, 1, true, true
		}
		return 1, 0, true, true
	case InstrGlobalGet, InstrMemorySize:
		return 0, 1, false, true
	case InstrGlobalSet:
		return 1, 0, false, true
	case InstrMemoryGrow:
		return 1, 1, false, true
	case InstrReturn:
		return len(ft.Results), 0, false, true
	case InstrCall, InstrCallIndirect:
		var callee *CompType
		if in.Kind == InstrCall {
			callee, _ = b.v.funcType(in.Index)
		} else {
			callee = b.v.funcTypeFromTypeIdx(TypeIdx{Index: in.Index})
		}
		if callee == nil {
			return
		}
		b.charge(len(callee.Params))
		b.charge(len(callee.Results))
		for _, t := range callee.Params {
			if !sourcePrimitive(t) {
				return
			}
		}
		for _, t := range callee.Results {
			if !sourcePrimitive(t) {
				return
			}
		}
		inputs = len(callee.Params)
		outputs = len(callee.Results)
		if in.Kind == InstrCallIndirect {
			inputs++
		}
		return inputs, outputs, false, true
	case InstrI32TruncSatF32S, InstrI32TruncSatF32U, InstrI32TruncSatF64S, InstrI32TruncSatF64U,
		InstrI64TruncSatF32S, InstrI64TruncSatF32U, InstrI64TruncSatF64S, InstrI64TruncSatF64U:
		return 1, 1, false, true
	default:
		return
	}
}
