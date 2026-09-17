package railssa

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	// Keep dense local-SSA construction bounded by the same 14 MiB nominal
	// transient ceiling as the original one-million-cell implementation. Each
	// cell owns two retained EnvValueIDs plus one retained liveness byte. A
	// block parameter is represented directly in EntryValues once discovered;
	// live-out sets are derived from successor live-in sets.
	maxLocalSSATransientBytes     = 14 << 20
	localSSATransientBytesPerCell = 2*4 + 1
	maxLocalSSACells              = maxLocalSSATransientBytes / localSSATransientBytesPerCell
)

type EnvValueID uint32

type DefinitionKind uint8

const (
	DefinitionInvalid DefinitionKind = iota
	DefinitionInitial
	DefinitionLocalSet
	DefinitionBlockParam
)

// Definition is a symbolic local value used while semantic instructions are
// built. Local gets resolve to one of these IDs and are never emitted as ops.
type Definition struct {
	Kind  DefinitionKind
	Type  wasm.ValType
	Local uint32
	Block BlockID
	Instr uint32
}

type BlockParam struct {
	Block BlockID
	Local uint32
	Value EnvValueID
}

type EdgeArgument struct {
	Edge     uint32
	Param    EnvValueID
	Argument EnvValueID
}

// LocalSSA is the dense local-environment portion of RailSSA construction.
// EntryValues and ExitValues are block-major [block][local] matrices.
type LocalSSA struct {
	LocalCount uint32

	Definitions []Definition
	Params      []BlockParam
	EdgeArgs    []EdgeArgument

	EntryValues []EnvValueID
	ExitValues  []EnvValueID
	// InstructionValues records the environment definition read by local.get
	// or created by local.set/local.tee. Those opcode classes are disjoint, so
	// one source-indexed slab represents both without ambiguity.
	InstructionValues []EnvValueID
	Reachable         []bool
	LiveIn            []bool

	work        []BlockID
	ready       []bool
	before      []EnvValueID
	liveScratch []bool
}

func (s *LocalSSA) entry(block BlockID) []EnvValueID {
	start := uint64(block) * uint64(s.LocalCount)
	return s.EntryValues[start : start+uint64(s.LocalCount)]
}

func (s *LocalSSA) exit(block BlockID) []EnvValueID {
	start := uint64(block) * uint64(s.LocalCount)
	return s.ExitValues[start : start+uint64(s.LocalCount)]
}

// BuildLocalSSA resolves local.get/set/tee through CFG joins. Conflicting
// predecessor definitions become block parameters; every incoming edge carries
// an explicit argument for each such parameter.
func BuildLocalSSA(f *StackFunc, cfg *CFG, reuse *LocalSSA) (*LocalSSA, error) {
	if f == nil || cfg == nil {
		return nil, fmt.Errorf("railssa: local SSA requires function and CFG")
	}
	cells := uint64(len(f.Locals)) * uint64(len(cfg.Blocks))
	if cells > maxLocalSSACells {
		return nil, fmt.Errorf("railssa: local SSA requires %d cells (%d transient bytes), exceeds %d-byte budget", cells, cells*localSSATransientBytesPerCell, maxLocalSSATransientBytes)
	}
	if reuse == nil {
		reuse = new(LocalSSA)
	}
	definitions := reuse.Definitions[:0]
	params := reuse.Params[:0]
	edgeArgs := reuse.EdgeArgs[:0]
	entryValues := resizeClear(reuse.EntryValues, int(cells))
	exitValues := resizeClear(reuse.ExitValues, int(cells))
	instructionValues := resizeClear(reuse.InstructionValues, len(f.Instrs))
	reachable := resizeClear(reuse.Reachable, len(cfg.Blocks))
	liveIn := resizeClear(reuse.LiveIn, int(cells))
	work := reuse.work[:0]
	ready := resizeClear(reuse.ready, len(cfg.Blocks))
	before := resizeClear(reuse.before, len(f.Locals))
	liveScratch := resizeClear(reuse.liveScratch, len(f.Locals))
	*reuse = LocalSSA{
		LocalCount: uint32(len(f.Locals)), Definitions: definitions, Params: params, EdgeArgs: edgeArgs,
		EntryValues: entryValues, ExitValues: exitValues, InstructionValues: instructionValues, Reachable: reachable,
		LiveIn: liveIn,
		work:   work, ready: ready, before: before, liveScratch: liveScratch,
	}
	s := reuse
	s.Definitions = append(s.Definitions, Definition{}) // ID zero is invalid.
	for local, typ := range f.Locals {
		s.Definitions = append(s.Definitions, Definition{Kind: DefinitionInitial, Type: typ, Local: uint32(local), Instr: ^uint32(0)})
	}
	for instruction, instr := range f.Instrs {
		if instr.Kind != wasm.InstrLocalSet && instr.Kind != wasm.InstrLocalTee {
			continue
		}
		local := instr.U32()
		if int(local) >= len(f.Locals) {
			return nil, fmt.Errorf("railssa: instruction %d sets invalid local %d", instruction, local)
		}
		value := EnvValueID(len(s.Definitions))
		s.Definitions = append(s.Definitions, Definition{Kind: DefinitionLocalSet, Type: f.Locals[local], Local: local, Instr: uint32(instruction)})
		s.InstructionValues[instruction] = value
	}

	// Reachability is independent of local dataflow and prevents dead structured
	// tails from manufacturing block parameters.
	s.work = append(s.work, 0)
	s.Reachable[0] = true
	for len(s.work) != 0 {
		block := s.work[len(s.work)-1]
		s.work = s.work[:len(s.work)-1]
		record := cfg.Blocks[block]
		for _, succ := range cfg.Succs[record.SuccStart : record.SuccStart+uint32(record.SuccCount)] {
			if !s.Reachable[succ] {
				s.Reachable[succ] = true
				s.work = append(s.work, succ)
			}
		}
	}
	if err := computeLocalLiveness(f, cfg, s); err != nil {
		return nil, err
	}

	if len(cfg.Blocks) != 0 {
		for local := range f.Locals {
			s.entry(0)[local] = EnvValueID(local + 1)
		}
	}
	for pass := 0; pass <= len(cfg.Blocks)*2; pass++ {
		changed := false
		for blockID := range cfg.Blocks {
			block := BlockID(blockID)
			if !s.Reachable[block] {
				continue
			}
			incoming := s.entry(block)
			if block != 0 {
				record := cfg.Blocks[block]
				for local := range f.Locals {
					merged := EnvValueID(0)
					conflict := false
					for _, pred := range cfg.Preds[record.PredStart : record.PredStart+uint32(record.PredCount)] {
						if !s.Reachable[pred] || !s.ready[pred] {
							continue
						}
						value := s.exit(pred)[local]
						if merged == 0 {
							merged = value
						} else if value != merged {
							conflict = true
						}
					}
					cell := blockID*len(f.Locals) + local
					existing := incoming[local]
					hasParam := s.isBlockParam(existing, block, uint32(local))
					if s.LiveIn[cell] && (conflict || hasParam) {
						if !hasParam {
							existing = EnvValueID(len(s.Definitions))
							s.Definitions = append(s.Definitions, Definition{Kind: DefinitionBlockParam, Type: f.Locals[local], Local: uint32(local), Block: block, Instr: ^uint32(0)})
						}
						merged = existing
					}
					if merged != 0 && incoming[local] != merged {
						incoming[local] = merged
						changed = true
					}
				}
			}
			if block != 0 && !hasValues(incoming) && len(f.Locals) != 0 {
				continue
			}
			outgoing := s.exit(block)
			copy(s.before, outgoing)
			copy(outgoing, incoming)
			record := cfg.Blocks[block]
			for instruction := record.InstStart; instruction < record.InstStart+record.InstCount; instruction++ {
				instr := f.Instrs[instruction]
				switch instr.Kind {
				case wasm.InstrLocalGet:
					s.InstructionValues[instruction] = outgoing[instr.U32()]
				case wasm.InstrLocalSet, wasm.InstrLocalTee:
					outgoing[instr.U32()] = s.InstructionValues[instruction]
				}
			}
			if !equalEnv(s.before, outgoing) || !s.ready[block] {
				changed = true
			}
			s.ready[block] = true
		}
		if !changed {
			break
		}
		if pass == len(cfg.Blocks)*2 {
			return nil, fmt.Errorf("railssa: local SSA did not converge after %d passes", pass+1)
		}
	}
	for value, definition := range s.Definitions {
		if definition.Kind == DefinitionBlockParam {
			s.Params = append(s.Params, BlockParam{Block: definition.Block, Local: definition.Local, Value: EnvValueID(value)})
		}
	}
	for edgeIndex, edge := range cfg.Edges {
		if !s.Reachable[edge.From] || !s.Reachable[edge.To] {
			continue
		}
		for _, param := range s.Params {
			if param.Block != edge.To {
				continue
			}
			argument := s.exit(edge.From)[param.Local]
			if argument == 0 {
				return nil, fmt.Errorf("railssa: edge %d has no local %d argument", edgeIndex, param.Local)
			}
			s.EdgeArgs = append(s.EdgeArgs, EdgeArgument{Edge: uint32(edgeIndex), Param: param.Value, Argument: argument})
		}
	}
	if err := VerifyLocalSSA(f, cfg, s); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *LocalSSA) isBlockParam(value EnvValueID, block BlockID, local uint32) bool {
	return value != 0 && int(value) < len(s.Definitions) && s.Definitions[value].Kind == DefinitionBlockParam &&
		s.Definitions[value].Block == block && s.Definitions[value].Local == local
}

func VerifyLocalSSA(f *StackFunc, cfg *CFG, s *LocalSSA) error {
	if f == nil || cfg == nil || s == nil || int(s.LocalCount) != len(f.Locals) {
		return fmt.Errorf("railssa: malformed local SSA header")
	}
	wantCells := len(cfg.Blocks) * len(f.Locals)
	if len(s.EntryValues) != wantCells || len(s.ExitValues) != wantCells || len(s.LiveIn) != wantCells || len(s.InstructionValues) != len(f.Instrs) {
		return fmt.Errorf("railssa: malformed local SSA dense storage")
	}
	for _, param := range s.Params {
		if int(param.Block) >= len(cfg.Blocks) || int(param.Local) >= len(f.Locals) || int(param.Value) >= len(s.Definitions) {
			return fmt.Errorf("railssa: invalid block parameter %#v", param)
		}
		definition := s.Definitions[param.Value]
		if definition.Kind != DefinitionBlockParam || definition.Block != param.Block || definition.Local != param.Local || definition.Type != f.Locals[param.Local] {
			return fmt.Errorf("railssa: inconsistent block parameter %#v", param)
		}
		if s.entry(param.Block)[param.Local] != param.Value {
			return fmt.Errorf("railssa: block parameter %d is not its entry local", param.Value)
		}
		if !s.LiveIn[int(param.Block)*len(f.Locals)+int(param.Local)] {
			return fmt.Errorf("railssa: dead local %d has block parameter in block %d", param.Local, param.Block)
		}
	}
	for instruction, instr := range f.Instrs {
		if instr.Kind == wasm.InstrLocalGet && s.InstructionValues[instruction] != 0 {
			value := s.InstructionValues[instruction]
			if int(value) >= len(s.Definitions) || s.Definitions[value].Type != f.Locals[instr.U32()] {
				return fmt.Errorf("railssa: local.get %d has invalid value %d", instruction, value)
			}
		}
	}
	for _, argument := range s.EdgeArgs {
		if int(argument.Edge) >= len(cfg.Edges) || int(argument.Param) >= len(s.Definitions) || int(argument.Argument) >= len(s.Definitions) {
			return fmt.Errorf("railssa: invalid edge argument %#v", argument)
		}
		param := s.Definitions[argument.Param]
		value := s.Definitions[argument.Argument]
		if param.Kind != DefinitionBlockParam || param.Type != value.Type || cfg.Edges[argument.Edge].To != param.Block {
			return fmt.Errorf("railssa: inconsistent edge argument %#v", argument)
		}
	}
	return nil
}

func computeLocalLiveness(f *StackFunc, cfg *CFG, s *LocalSSA) error {
	if len(f.Locals) == 0 {
		return nil
	}
	scratch := s.liveScratch
	for pass := 0; pass <= len(cfg.Blocks); pass++ {
		changed := false
		for blockID := len(cfg.Blocks) - 1; blockID >= 0; blockID-- {
			block := BlockID(blockID)
			if !s.Reachable[block] {
				continue
			}
			start := blockID * len(f.Locals)
			clear(scratch)
			record := cfg.Blocks[block]
			for _, succ := range cfg.Succs[record.SuccStart : record.SuccStart+uint32(record.SuccCount)] {
				if !s.Reachable[succ] {
					continue
				}
				succStart := int(succ) * len(f.Locals)
				for local, live := range s.LiveIn[succStart : succStart+len(f.Locals)] {
					scratch[local] = scratch[local] || live
				}
			}
			for instruction := record.InstStart + record.InstCount; instruction > record.InstStart; {
				instruction--
				instr := f.Instrs[instruction]
				switch instr.Kind {
				case wasm.InstrLocalGet:
					scratch[instr.U32()] = true
				case wasm.InstrLocalSet, wasm.InstrLocalTee:
					scratch[instr.U32()] = false
				}
			}
			in := s.LiveIn[start : start+len(f.Locals)]
			if !equalBool(in, scratch) {
				copy(in, scratch)
				changed = true
			}
		}
		if !changed {
			return nil
		}
	}
	return fmt.Errorf("railssa: local liveness did not converge after %d passes", len(cfg.Blocks)+1)
}

func resizeClear[T comparable](values []T, length int) []T {
	if cap(values) < length {
		return make([]T, length)
	}
	values = values[:length]
	clear(values)
	return values
}

func hasValues(values []EnvValueID) bool {
	for _, value := range values {
		if value != 0 {
			return true
		}
	}
	return false
}

func equalEnv(a, b []EnvValueID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalBool(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
