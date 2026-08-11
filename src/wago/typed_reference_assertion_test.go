//go:build (linux || darwin || windows) && (amd64 || arm64)

package wago

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

type proposalReplayModule struct {
	inst        *Instance
	compiled    *Compiled
	imports     Imports
	schema      map[string]proposalExternType
	hostExports map[string]*HostFuncRef
}

type proposalReplayState struct {
	rt            *Runtime
	standard      Imports
	standardTypes map[string]proposalExternType
	current       *proposalReplayModule
	named         map[string]*proposalReplayModule
	registered    map[string]*proposalReplayModule
	live          []*proposalReplayModule
	hostRefs      []*HostFuncRef
	externrefs    map[int64]ExternRef
}

type proposalReplayCounts struct {
	modules, unsupportedModules               int
	registers                                 int
	actions, unsupportedActions               int
	returns, unsupportedReturns               int
	traps, unsupportedTraps                   int
	exceptions, unsupportedExceptions         int
	invalid, supersededInvalid, malformedText int
	unlinkable, uninstantiable                int
}

func (c *proposalReplayCounts) add(other proposalReplayCounts) {
	c.modules += other.modules
	c.unsupportedModules += other.unsupportedModules
	c.registers += other.registers
	c.actions += other.actions
	c.unsupportedActions += other.unsupportedActions
	c.returns += other.returns
	c.unsupportedReturns += other.unsupportedReturns
	c.traps += other.traps
	c.unsupportedTraps += other.unsupportedTraps
	c.exceptions += other.exceptions
	c.unsupportedExceptions += other.unsupportedExceptions
	c.invalid += other.invalid
	c.supersededInvalid += other.supersededInvalid
	c.malformedText += other.malformedText
	c.unlinkable += other.unlinkable
	c.uninstantiable += other.uninstantiable
}

func TestProposalAssertionsReplay(t *testing.T) {
	root := filepath.Clean("../../tests/regressions/spectest-proposals")
	// These totals pin both executed assertions and commands whose provider is
	// rejected by an explicit proposal boundary. Unsupported commands are not
	// skipped: their provider rejection and exact command accounting are asserted.
	wants := map[string]proposalReplayCounts{
		"exception-handling": {
			modules: 12, unsupportedModules: 12, registers: 3,
			unsupportedReturns: 50, unsupportedTraps: 2, unsupportedExceptions: 18,
			invalid: 16, malformedText: 2, unlinkable: 2,
		},
		"tail-call": {
			modules: 6, unsupportedModules: 6,
			unsupportedReturns: 71, unsupportedTraps: 7,
			invalid: 24, malformedText: 11,
		},
		"threads": {
			modules: 5, unsupportedModules: 5, unsupportedActions: 65,
			unsupportedReturns: 154, unsupportedTraps: 55, invalid: 48,
		},
		"typed-function-references": {
			modules: 159, unsupportedModules: 62, registers: 16,
			actions: 2, unsupportedActions: 2,
			returns: 311, unsupportedReturns: 327,
			traps: 28, unsupportedTraps: 28,
			invalid: 332, supersededInvalid: 1, malformedText: 40, unlinkable: 47, uninstantiable: 19,
		},
	}
	for _, proposal := range []string{"exception-handling", "tail-call", "threads", "typed-function-references"} {
		proposal := proposal
		t.Run(proposal, func(t *testing.T) {
			dir := filepath.Join(root, proposal)
			paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
			if err != nil {
				t.Fatal(err)
			}
			var got proposalReplayCounts
			for _, path := range paths {
				base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
				t.Run(base, func(t *testing.T) {
					got.add(replayProposalAssertionsFile(t, dir, base))
				})
			}
			if want, ok := wants[proposal]; ok && got != want {
				t.Fatalf("proposal replay accounting = %+v, want %+v", got, want)
			}
			t.Logf("proposal replay accounting: %+v", got)
		})
	}
}

func replayProposalAssertionsFile(t *testing.T, dir, base string) (counts proposalReplayCounts) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, base+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var sf proposalFixtureFile
	if err := json.Unmarshal(raw, &sf); err != nil {
		t.Fatalf("decode %s.json: %v", base, err)
	}

	table, err := NewTable(10, 20)
	if err != nil {
		t.Fatalf("create spectest.table: %v", err)
	}
	memory, err := NewMemory(1, 2)
	if err != nil {
		_ = table.Close()
		t.Fatalf("create spectest.memory: %v", err)
	}
	state := &proposalReplayState{
		rt:            NewRuntime(WithRuntimeConfig(compatibilityDefaultConfig())),
		standard:      proposalSpectestImports(table, memory),
		standardTypes: proposalSpectestTypes(),
		named:         make(map[string]*proposalReplayModule),
		registered:    make(map[string]*proposalReplayModule),
		externrefs:    make(map[int64]ExternRef),
	}
	defer state.close(t, table, memory)

	for _, cmd := range sf.Commands {
		switch cmd.Type {
		case "module":
			counts.modules++
			state.current = nil
			data, err := os.ReadFile(filepath.Join(dir, cmd.Filename))
			if err != nil {
				t.Fatalf("%s.wast:%d read module %q: %v", base, cmd.Line, cmd.Filename, err)
			}
			schema, exactImports, err := proposalModuleTypes(data)
			if err != nil {
				t.Fatalf("%s.wast:%d decode provider types: %v", base, cmd.Line, err)
			}
			if linkErr := state.exactLinkError(exactImports); linkErr != nil {
				t.Fatalf("%s.wast:%d valid provider module failed exact linking: %v", base, cmd.Line, linkErr)
			}
			mod, compileErr := state.rt.Compile(data)
			if compileErr != nil {
				if isExplicitProposalRejection(compileErr) {
					counts.unsupportedModules++
					m := &proposalReplayModule{schema: schema}
					state.current = m
					if cmd.Name != "" {
						state.named[cmd.Name] = m
					}
					continue
				}
				t.Fatalf("%s.wast:%d valid provider module compile failed: %v", base, cmd.Line, compileErr)
			}
			compiled := mod.Compiled()
			imports, err := state.importsFor(compiled, exactImports)
			if err != nil {
				_ = compiled.Close()
				t.Fatalf("%s.wast:%d provider imports: %v", base, cmd.Line, err)
			}
			inst, err := state.rt.Instantiate(context.Background(), mod, WithImports(imports))
			if err != nil {
				_ = compiled.Close()
				t.Fatalf("%s.wast:%d provider instantiate: %v", base, cmd.Line, err)
			}
			m := &proposalReplayModule{inst: inst, compiled: compiled, imports: imports, schema: schema}
			state.current = m
			state.live = append(state.live, m)
			if cmd.Name != "" {
				state.named[cmd.Name] = m
			}
		case "register":
			counts.registers++
			m := state.current
			if cmd.Name != "" {
				m = state.named[cmd.Name]
			}
			if m != nil && cmd.As != "" {
				state.registered[cmd.As] = m
			}
		case "action":
			target := state.actionTarget(cmd.Action)
			if target == nil || target.inst == nil {
				counts.unsupportedActions++
				continue
			}
			if _, err := state.executeAction(target, cmd.Action); err != nil {
				t.Fatalf("%s.wast:%d replay action: %v", base, cmd.Line, err)
			}
			counts.actions++
		case "assert_return":
			executed, err := state.replayAssertReturn(cmd.Action, cmd.Expected)
			if err != nil {
				t.Fatalf("%s.wast:%d replay assert_return: %v", base, cmd.Line, err)
			}
			if executed {
				counts.returns++
			} else {
				counts.unsupportedReturns++
			}
		case "assert_trap":
			executed, err := state.replayAssertTrap(cmd.Action, cmd.Text)
			if err != nil {
				t.Fatalf("%s.wast:%d replay assert_trap: %v", base, cmd.Line, err)
			}
			if executed {
				counts.traps++
			} else {
				counts.unsupportedTraps++
			}
		case "assert_exception":
			target := state.actionTarget(cmd.Action)
			if target == nil || target.inst == nil {
				counts.unsupportedExceptions++
				continue
			}
			if _, err := state.executeAction(target, cmd.Action); err == nil {
				t.Fatalf("%s.wast:%d action succeeded, want exception", base, cmd.Line)
			}
			counts.exceptions++
		case "assert_invalid":
			data, err := os.ReadFile(filepath.Join(dir, cmd.Filename))
			if err != nil {
				t.Fatalf("%s.wast:%d read invalid module %q: %v", base, cmd.Line, cmd.Filename, err)
			}
			module, validationErr := corewasm.DecodeModule(data)
			if validationErr == nil {
				validationErr = corewasm.ValidateModule(module)
			}
			if validationErr == nil {
				if base == "type-equivalence" && cmd.Filename == "type-equivalence.2.wasm" && cmd.Text == "unknown type" {
					// This proposal-era assertion predates standardized implicit
					// singleton recursion. The exact binary is valid under the final
					// rectype grammar, so pin the semantic drift rather than rejecting
					// a current-spec module or silently dropping the command.
					counts.supersededInvalid++
					continue
				}
				t.Fatalf("%s.wast:%d invalid module validated successfully, want %q", base, cmd.Line, cmd.Text)
			}
			if !proposalNegativeFailureMatches(cmd.Text, validationErr) {
				t.Fatalf("%s.wast:%d validation error = %v, want %q", base, cmd.Line, validationErr, cmd.Text)
			}
			counts.invalid++
		case "assert_malformed":
			// Wago intentionally has no WAT parser. Keep text-format proposal
			// negatives explicit and fully accounted rather than reporting a skip.
			data, err := os.ReadFile(filepath.Join(dir, cmd.Filename))
			if err != nil {
				t.Fatalf("%s.wast:%d read malformed text %q: %v", base, cmd.Line, cmd.Filename, err)
			}
			if cmd.ModuleType != "text" || filepath.Ext(cmd.Filename) != ".wat" || len(data) == 0 || cmd.Text == "" {
				t.Fatalf("%s.wast:%d malformed text fixture metadata is incomplete", base, cmd.Line)
			}
			counts.malformedText++
		case "assert_unlinkable", "assert_uninstantiable":
			data, err := os.ReadFile(filepath.Join(dir, cmd.Filename))
			if err != nil {
				t.Fatalf("%s.wast:%d read negative module %q: %v", base, cmd.Line, cmd.Filename, err)
			}
			_, exactImports, err := proposalModuleTypes(data)
			if err != nil {
				t.Fatalf("%s.wast:%d decode negative module types: %v", base, cmd.Line, err)
			}
			exactLinkErr := state.exactLinkError(exactImports)
			if cmd.Type == "assert_unlinkable" {
				if exactLinkErr == nil {
					t.Fatalf("%s.wast:%d exact linking succeeded, want %q", base, cmd.Line, cmd.Text)
				}
				if !proposalNegativeFailureMatches(cmd.Text, exactLinkErr) {
					t.Fatalf("%s.wast:%d exact link error = %v, want %q", base, cmd.Line, exactLinkErr, cmd.Text)
				}
				counts.unlinkable++
			} else if exactLinkErr != nil {
				t.Fatalf("%s.wast:%d uninstantiable fixture failed exact linking before its intended trap: %v", base, cmd.Line, exactLinkErr)
			}

			mod, compileErr := state.rt.Compile(data)
			if compileErr != nil {
				if cmd.Type == "assert_unlinkable" && isExplicitProposalRejection(compileErr) {
					// Exact linking above is the assertion oracle. Wago continues to
					// reject consumers that use executable typed-reference features.
					continue
				}
				t.Fatalf("%s.wast:%d valid negative module compile failed: %v", base, cmd.Line, compileErr)
			}
			compiled := mod.Compiled()
			imports, importErr := state.importsFor(compiled, exactImports)
			if cmd.Type == "assert_unlinkable" {
				if importErr == nil {
					_ = compiled.Close()
					t.Fatalf("%s.wast:%d Wago linking succeeded after exact linker rejected the module", base, cmd.Line)
				}
				if err := compiled.Close(); err != nil {
					t.Errorf("%s.wast:%d close unlinkable module: %v", base, cmd.Line, err)
				}
				if !proposalNegativeFailureMatches(cmd.Text, importErr) {
					t.Fatalf("%s.wast:%d Wago link error = %v, want %q", base, cmd.Line, importErr, cmd.Text)
				}
				continue
			}
			if importErr != nil {
				_ = compiled.Close()
				t.Fatalf("%s.wast:%d uninstantiable fixture failed Wago linking before its intended trap: %v", base, cmd.Line, importErr)
			}
			inst, instantiateErr := state.rt.Instantiate(context.Background(), mod, WithImports(imports))
			if instantiateErr == nil {
				if inst != nil {
					_ = inst.Close()
				}
				_ = compiled.Close()
				t.Fatalf("%s.wast:%d negative module instantiated successfully, want %q", base, cmd.Line, cmd.Text)
			}
			if inst != nil {
				_ = inst.Close()
			}
			if err := compiled.Close(); err != nil {
				t.Errorf("%s.wast:%d close rejected negative module: %v", base, cmd.Line, err)
			}
			if !proposalNegativeFailureMatches(cmd.Text, instantiateErr) {
				t.Fatalf("%s.wast:%d instantiate error = %v, want %q", base, cmd.Line, instantiateErr, cmd.Text)
			}
			counts.uninstantiable++
		default:
			t.Fatalf("%s.wast:%d unsupported command type %q", base, cmd.Line, cmd.Type)
		}
	}
	return counts
}

func (s *proposalReplayState) replayAssertReturn(action proposalFixtureAction, expected []proposalFixtureValue) (bool, error) {
	target := s.actionTarget(action)
	if target == nil || target.inst == nil {
		return false, nil
	}
	results, err := s.executeAction(target, action)
	if err != nil {
		return true, err
	}
	if len(results) != len(expected) {
		return true, fmt.Errorf("result count = %d, want %d", len(results), len(expected))
	}
	for i, value := range expected {
		if err := s.compareActionResult(target.inst, i, results[i], value); err != nil {
			return true, err
		}
	}
	return true, nil
}

func (s *proposalReplayState) replayAssertTrap(action proposalFixtureAction, want string) (bool, error) {
	target := s.actionTarget(action)
	if target == nil || target.inst == nil {
		return false, nil
	}
	_, err := s.executeAction(target, action)
	if err == nil {
		return true, fmt.Errorf("action succeeded, want trap %q", want)
	}
	if !proposalNegativeFailureMatches(want, err) {
		return true, fmt.Errorf("trap = %v, want %q", err, want)
	}
	return true, nil
}

func (s *proposalReplayState) actionTarget(action proposalFixtureAction) *proposalReplayModule {
	if action.Module != "" {
		return s.named[action.Module]
	}
	return s.current
}

func (s *proposalReplayState) executeAction(target *proposalReplayModule, action proposalFixtureAction) ([]uint64, error) {
	switch action.Type {
	case "invoke":
		args := make([]uint64, len(action.Args))
		for i, value := range action.Args {
			bits, err := s.proposalValueBits(value)
			if err != nil {
				return nil, fmt.Errorf("argument %d: %w", i, err)
			}
			args[i] = bits
		}
		return target.inst.Invoke(action.Field, args...)
	case "get":
		value, err := target.inst.GlobalValue(action.Field)
		if err != nil {
			return nil, err
		}
		return []uint64{value.Bits()}, nil
	default:
		return nil, fmt.Errorf("unsupported action type %q", action.Type)
	}
}

func (s *proposalReplayState) proposalValueBits(value proposalFixtureValue) (uint64, error) {
	switch value.Type {
	case "i32", "f32":
		return proposalDecimalBits(value.Value, 32)
	case "i64", "f64":
		return proposalDecimalBits(value.Value, 64)
	case "funcref", "refnull":
		if value.Value == "" || value.Value == "null" {
			return 0, nil
		}
		return 0, fmt.Errorf("unsupported non-null %s argument", value.Type)
	case "externref":
		if value.Value == "null" {
			return 0, nil
		}
		id, err := strconv.ParseInt(value.Value, 10, 64)
		if err != nil {
			return 0, err
		}
		if ref, ok := s.externrefs[id]; ok {
			return ValueExternRef(ref).Bits(), nil
		}
		ref, err := s.rt.NewExternRef(id)
		if err != nil {
			return 0, err
		}
		s.externrefs[id] = ref
		return ValueExternRef(ref).Bits(), nil
	default:
		return 0, fmt.Errorf("unsupported scalar type %q", value.Type)
	}
}

func proposalDecimalBits(value string, width int) (uint64, error) {
	if strings.HasPrefix(value, "-") {
		v, err := strconv.ParseInt(value, 10, width)
		if width == 32 {
			return uint64(uint32(v)), err
		}
		return uint64(v), err
	}
	v, err := strconv.ParseUint(value, 10, width)
	return v, err
}

func (s *proposalReplayState) compareActionResult(inst *Instance, index int, got uint64, expected proposalFixtureValue) error {
	switch expected.Type {
	case "externref":
		if expected.Value == "null" {
			if got != 0 {
				return fmt.Errorf("result %d = non-null externref, want null", index)
			}
			return nil
		}
		id, err := strconv.ParseInt(expected.Value, 10, 64)
		if err != nil {
			return fmt.Errorf("expected result %d: %w", index, err)
		}
		value, ok := inst.ExternRefValue(ValueOf(ValExternRef, got).ExternRef())
		if !ok || value != id {
			return fmt.Errorf("result %d externref = %#v, %v; want %d", index, value, ok, id)
		}
		return nil
	case "funcref":
		if expected.Value == "null" {
			if got != 0 {
				return fmt.Errorf("result %d = non-null funcref, want null", index)
			}
		} else if got == 0 {
			return fmt.Errorf("result %d = null funcref, want non-null", index)
		}
		return nil
	case "refnull":
		if got != 0 {
			return fmt.Errorf("result %d = non-null reference, want null", index)
		}
		return nil
	default:
		want, err := s.proposalValueBits(expected)
		if err != nil {
			return fmt.Errorf("expected result %d: %w", index, err)
		}
		if got != want {
			return fmt.Errorf("result %d = %#x, want %#x", index, got, want)
		}
		return nil
	}
}

func proposalSpectestImports(table *Table, memory *Memory) Imports {
	noop := HostFunc(func(HostModule, []uint64, []uint64) {})
	return Imports{
		"spectest.print":         noop,
		"spectest.print_i32":     noop,
		"spectest.print_i64":     noop,
		"spectest.print_f32":     noop,
		"spectest.print_f64":     noop,
		"spectest.print_i32_f32": noop,
		"spectest.print_f64_f64": noop,
		"spectest.global_i32":    GlobalImport{Type: ValI32, Bits: I32(666)},
		"spectest.global_i64":    GlobalImport{Type: ValI64, Bits: I64(666)},
		"spectest.global_f32":    GlobalImport{Type: ValF32, Bits: F32(666)},
		"spectest.global_f64":    GlobalImport{Type: ValF64, Bits: F64(666)},
		"spectest.memory":        memory,
		"spectest.table":         table,
	}
}

func proposalSpectestTypes() map[string]proposalExternType {
	fn := func(params ...corewasm.ValType) proposalExternType {
		return proposalExternType{kind: proposalExternFunc, fn: &corewasm.CompType{Kind: corewasm.CompFunc, Params: params}}
	}
	global := func(typ corewasm.ValType) proposalExternType {
		return proposalExternType{kind: proposalExternGlobal, global: corewasm.GlobalType{Type: typ}}
	}
	memoryMax, tableMax := uint64(2), uint64(20)
	return map[string]proposalExternType{
		"spectest.print":         fn(),
		"spectest.print_i32":     fn(corewasm.I32),
		"spectest.print_i64":     fn(corewasm.I64),
		"spectest.print_f32":     fn(corewasm.F32),
		"spectest.print_f64":     fn(corewasm.F64),
		"spectest.print_i32_f32": fn(corewasm.I32, corewasm.F32),
		"spectest.print_f64_f64": fn(corewasm.F64, corewasm.F64),
		"spectest.global_i32":    global(corewasm.I32),
		"spectest.global_i64":    global(corewasm.I64),
		"spectest.global_f32":    global(corewasm.F32),
		"spectest.global_f64":    global(corewasm.F64),
		"spectest.memory":        {kind: proposalExternMemory, memory: corewasm.MemType{Limits: corewasm.Limits{Min: 1, Max: memoryMax, HasMax: true}}},
		"spectest.table":         {kind: proposalExternTable, table: corewasm.TableType{Ref: corewasm.FuncRef.Ref(), Limits: corewasm.Limits{Min: 10, Max: tableMax, HasMax: true}}},
	}
}

type proposalExternKind uint8

const (
	proposalExternAbsent proposalExternKind = iota
	proposalExternFunc
	proposalExternGlobal
	proposalExternMemory
	proposalExternTable
	proposalExternTag
)

type proposalExternType struct {
	kind         proposalExternKind
	module       *corewasm.Module
	fn           *corewasm.CompType
	typeIndex    uint32
	hasTypeIndex bool
	global       corewasm.GlobalType
	memory       corewasm.MemType
	table        corewasm.TableType
}

type proposalImportType struct {
	key string
	typ proposalExternType
}

type proposalImportTypes struct {
	ordered []proposalImportType
	byKey   map[string]proposalExternType
}

func proposalModuleTypes(data []byte) (exports map[string]proposalExternType, imports proposalImportTypes, err error) {
	module, err := corewasm.DecodeModule(data)
	if err != nil {
		return nil, proposalImportTypes{}, err
	}
	imports.byKey = make(map[string]proposalExternType)
	for i := range module.Imports {
		imp := &module.Imports[i]
		typ := proposalExternType{module: module}
		switch imp.Type.Kind {
		case corewasm.ExternFunc:
			typ.kind = proposalExternFunc
			typ.typeIndex, typ.hasTypeIndex = imp.Type.FuncType().Index, true
			typ.fn, _ = module.TypeFunc(imp.Type.FuncType().Index)
		case corewasm.ExternGlobal:
			typ.kind, typ.global = proposalExternGlobal, imp.Type.GlobalType()
		case corewasm.ExternMem:
			typ.kind, typ.memory = proposalExternMemory, imp.Type.MemType()
		case corewasm.ExternTable:
			typ.kind, typ.table = proposalExternTable, imp.Type.TableType()
		case corewasm.ExternTag:
			typ.kind = proposalExternTag
			typ.typeIndex, typ.hasTypeIndex = imp.Type.TagType().Type.Index, true
			typ.fn, _ = module.TypeFunc(imp.Type.TagType().Type.Index)
		default:
			continue
		}
		key := imp.Module + "." + imp.Name
		imports.ordered = append(imports.ordered, proposalImportType{key: key, typ: typ})
		imports.byKey[key] = typ
	}
	exports = make(map[string]proposalExternType)
	for i := range module.Exports {
		exp := &module.Exports[i]
		typ := proposalExternType{module: module}
		switch exp.Index.Kind {
		case corewasm.ExternFunc:
			typ.kind = proposalExternFunc
			typ.fn, _ = module.FuncSignature(exp.Index.Index)
			if typeIdx, ok := module.FuncTypeIndex(exp.Index.Index); ok {
				typ.typeIndex, typ.hasTypeIndex = typeIdx.Index, true
			}
		case corewasm.ExternGlobal:
			typ.kind = proposalExternGlobal
			typ.global, _ = module.GlobalTypeByIndex(exp.Index.Index)
		case corewasm.ExternMem:
			typ.kind = proposalExternMemory
			typ.memory, _ = proposalMemoryTypeByIndex(module, exp.Index.Index)
		case corewasm.ExternTable:
			typ.kind = proposalExternTable
			typ.table, _ = module.TableType(exp.Index.Index)
		case corewasm.ExternTag:
			typ.kind = proposalExternTag
			if tag, ok := proposalTagTypeByIndex(module, exp.Index.Index); ok {
				typ.typeIndex, typ.hasTypeIndex = tag.Type.Index, true
				typ.fn, _ = module.TypeFunc(tag.Type.Index)
			}
		default:
			continue
		}
		exports[exp.Name] = typ
	}
	return exports, imports, nil
}

func proposalTagTypeByIndex(module *corewasm.Module, index uint32) (corewasm.TagType, bool) {
	var current uint32
	for i := range module.Imports {
		if module.Imports[i].Type.Kind != corewasm.ExternTag {
			continue
		}
		if current == index {
			return module.Imports[i].Type.TagType(), true
		}
		current++
	}
	local := int(index - current)
	if index < current || local < 0 || local >= len(module.Tags) {
		return corewasm.TagType{}, false
	}
	return module.Tags[local], true
}

func proposalMemoryTypeByIndex(module *corewasm.Module, index uint32) (corewasm.MemType, bool) {
	var current uint32
	for i := range module.Imports {
		if module.Imports[i].Type.Kind != corewasm.ExternMem {
			continue
		}
		if current == index {
			return module.Imports[i].Type.MemType(), true
		}
		current++
	}
	local := int(index - current)
	if index < current || local < 0 || local >= len(module.Memories) {
		return corewasm.MemType{}, false
	}
	return module.Memories[local], true
}

func (m *proposalReplayModule) exportType(field string) (proposalExternType, bool) {
	if m == nil {
		return proposalExternType{}, false
	}
	typ, ok := m.schema[field]
	return typ, ok
}

func (s *proposalReplayState) exactLinkError(imports proposalImportTypes) error {
	for _, imp := range imports.ordered {
		key, expected := imp.key, imp.typ
		moduleName, field, ok := strings.Cut(key, ".")
		if !ok {
			return fmt.Errorf("unknown import %q: malformed key", key)
		}
		if actual, found := s.standardTypes[key]; found {
			if !proposalExternTypesCompatible(actual, expected) {
				return fmt.Errorf("incompatible import type for %q", key)
			}
			continue
		}
		if moduleName == "spectest" {
			return fmt.Errorf("unknown import %q", key)
		}
		provider := s.registered[moduleName]
		if provider == nil {
			return fmt.Errorf("unknown import %q: provider module %q is not registered", key, moduleName)
		}
		actual, found := provider.exportType(field)
		if !found {
			return fmt.Errorf("unknown import %q", key)
		}
		if !proposalExternTypesCompatible(actual, expected) {
			return fmt.Errorf("incompatible import type for %q", key)
		}
	}
	return nil
}

func (s *proposalReplayState) importsFor(compiled *Compiled, exact proposalImportTypes) (Imports, error) {
	imports := make(Imports, len(s.standard))
	for key, value := range s.standard {
		imports[key] = value
	}
	for _, key := range compiled.Imports {
		value, err := s.resolveImport(key, proposalExternFunc, exact.byKey[key])
		if err != nil {
			return nil, err
		}
		imports[key] = value
	}
	if key, ok := compiled.MemoryImport(); ok {
		value, err := s.resolveImport(key, proposalExternMemory, exact.byKey[key])
		if err != nil {
			return nil, err
		}
		imports[key] = value
	}
	for _, key := range compiled.TableImports() {
		value, err := s.resolveImport(key, proposalExternTable, exact.byKey[key])
		if err != nil {
			return nil, err
		}
		imports[key] = value
	}
	for _, imp := range compiled.GlobalImports {
		key := imp.Module + "." + imp.Name
		value, err := s.resolveImport(key, proposalExternGlobal, exact.byKey[key])
		if err != nil {
			return nil, err
		}
		imports[key] = value
	}
	return imports, nil
}

func (s *proposalReplayState) resolveImport(key string, want proposalExternKind, expected proposalExternType) (any, error) {
	moduleName, field, ok := strings.Cut(key, ".")
	if !ok {
		return nil, fmt.Errorf("unknown import %q: malformed key", key)
	}
	if value, found := s.standard[key]; found {
		actual, typed := s.standardTypes[key]
		if proposalImportValueKind(value) != want || !typed || !proposalExternTypesCompatible(actual, expected) {
			return nil, fmt.Errorf("incompatible import type for %q", key)
		}
		return value, nil
	}
	if moduleName == "spectest" {
		return nil, fmt.Errorf("unknown import %q", key)
	}
	provider, found := s.registered[moduleName]
	if !found || provider == nil {
		return nil, fmt.Errorf("unknown import %q: provider module %q is not registered", key, moduleName)
	}
	actual, found := provider.exportType(field)
	if !found {
		return nil, fmt.Errorf("unknown import %q", key)
	}
	if actual.kind != want || !proposalExternTypesCompatible(actual, expected) {
		return nil, fmt.Errorf("incompatible import type for %q", key)
	}
	if provider.inst == nil {
		return nil, fmt.Errorf("provider module %q uses an unsupported compatible export required by %q", moduleName, key)
	}
	switch want {
	case proposalExternFunc:
		return s.exportedFunction(provider, field)
	case proposalExternGlobal:
		return provider.inst.ExportedGlobalObject(field)
	case proposalExternMemory:
		return provider.inst.ExportedMemory(field)
	case proposalExternTable:
		return provider.inst.ExportedTable(field)
	default:
		return nil, fmt.Errorf("unsupported import kind for %q", key)
	}
}

func proposalExternTypesCompatible(actual, expected proposalExternType) bool {
	if actual.kind != expected.kind {
		return false
	}
	switch actual.kind {
	case proposalExternFunc:
		return proposalExternFuncTypesEquivalent(actual, expected)
	case proposalExternGlobal:
		if actual.global.Mutable != expected.global.Mutable {
			return false
		}
		if actual.global.Mutable {
			return proposalValTypesEquivalent(actual.global.Type, actual.module, expected.global.Type, expected.module, 0)
		}
		return proposalValTypeSubtype(actual.global.Type, actual.module, expected.global.Type, expected.module, 0)
	case proposalExternMemory:
		return actual.memory.Shared == expected.memory.Shared && proposalLimitsCompatible(actual.memory.Limits, expected.memory.Limits)
	case proposalExternTable:
		return proposalValTypesEquivalent(corewasm.RefVal(actual.table.Ref), actual.module, corewasm.RefVal(expected.table.Ref), expected.module, 0) && proposalLimitsCompatible(actual.table.Limits, expected.table.Limits)
	case proposalExternTag:
		return proposalExternFuncTypesEquivalent(actual, expected)
	default:
		return false
	}
}

func proposalExternFuncTypesEquivalent(a, b proposalExternType) bool {
	if a.module != nil && b.module != nil && a.hasTypeIndex && b.hasTypeIndex {
		ak, aok := a.module.StructuralTypeKeyChecked(a.typeIndex)
		bk, bok := b.module.StructuralTypeKeyChecked(b.typeIndex)
		if aok && bok {
			return ak == bk
		}
	}
	return proposalFuncTypesEquivalent(a.fn, a.module, b.fn, b.module, 0)
}

func proposalFuncTypesEquivalent(a *corewasm.CompType, am *corewasm.Module, b *corewasm.CompType, bm *corewasm.Module, depth int) bool {
	if a == nil || b == nil || a.Kind != corewasm.CompFunc || b.Kind != corewasm.CompFunc || len(a.Params) != len(b.Params) || len(a.Results) != len(b.Results) || depth > 32 {
		return false
	}
	for i := range a.Params {
		if !proposalValTypesEquivalent(a.Params[i], am, b.Params[i], bm, depth+1) {
			return false
		}
	}
	for i := range a.Results {
		if !proposalValTypesEquivalent(a.Results[i], am, b.Results[i], bm, depth+1) {
			return false
		}
	}
	return true
}

func proposalValTypesEquivalent(a corewasm.ValType, am *corewasm.Module, b corewasm.ValType, bm *corewasm.Module, depth int) bool {
	return proposalValTypeSubtype(a, am, b, bm, depth) && proposalValTypeSubtype(b, bm, a, am, depth)
}

func proposalValTypeSubtype(a corewasm.ValType, am *corewasm.Module, b corewasm.ValType, bm *corewasm.Module, depth int) bool {
	if depth > 32 || a.Kind() != b.Kind() {
		return false
	}
	if a.Kind() != corewasm.ValRef {
		return a.Num() == b.Num()
	}
	if a.Ref().Nullable() && !b.Ref().Nullable() || b.Ref().Exact() && !a.Ref().Exact() {
		return false
	}
	return proposalHeapTypeSubtype(a.Ref().Heap(), am, b.Ref().Heap(), bm, depth+1)
}

func proposalHeapTypeSubtype(a corewasm.HeapType, am *corewasm.Module, b corewasm.HeapType, bm *corewasm.Module, depth int) bool {
	if depth > 32 {
		return false
	}
	if a.Kind() == corewasm.HeapAbs && b.Kind() == corewasm.HeapAbs {
		return proposalAbsHeapSubtype(a.Abs(), b.Abs())
	}
	if a.Kind() == corewasm.HeapTypeIndex {
		afn, aok := proposalIndexedFuncType(am, a.Type())
		if b.Kind() == corewasm.HeapAbs {
			return aok && proposalAbsHeapSubtype(corewasm.HeapFunc, b.Abs())
		}
		if b.Kind() == corewasm.HeapTypeIndex {
			bfn, bok := proposalIndexedFuncType(bm, b.Type())
			return aok && bok && proposalFuncTypesEquivalent(afn, am, bfn, bm, depth+1)
		}
	}
	return false
}

func proposalIndexedFuncType(module *corewasm.Module, index corewasm.TypeIdx) (*corewasm.CompType, bool) {
	if module == nil || index.Rec {
		return nil, false
	}
	return module.TypeFunc(index.Index)
}

func proposalAbsHeapSubtype(a, b corewasm.AbsHeapType) bool {
	if a == b {
		return true
	}
	switch a {
	case corewasm.HeapNoFunc:
		return b == corewasm.HeapFunc
	case corewasm.HeapNoExtern:
		return b == corewasm.HeapExtern
	case corewasm.HeapNone:
		return b == corewasm.HeapAny || b == corewasm.HeapEq || b == corewasm.HeapStruct || b == corewasm.HeapArray || b == corewasm.HeapI31
	case corewasm.HeapI31, corewasm.HeapStruct, corewasm.HeapArray:
		return b == corewasm.HeapEq || b == corewasm.HeapAny
	case corewasm.HeapEq:
		return b == corewasm.HeapAny
	}
	return false
}

func proposalLimitsCompatible(actual, expected corewasm.Limits) bool {
	if actual.Addr64 != expected.Addr64 || actual.Min < expected.Min {
		return false
	}
	if !expected.HasMax {
		return true
	}
	return actual.HasMax && actual.Max <= expected.Max
}

func proposalImportValueKind(value any) proposalExternKind {
	switch value.(type) {
	case HostFunc, *HostFuncRef, *InstanceExport:
		return proposalExternFunc
	case GlobalImport, *Global:
		return proposalExternGlobal
	case *Memory:
		return proposalExternMemory
	case *Table:
		return proposalExternTable
	default:
		return proposalExternAbsent
	}
}

func (s *proposalReplayState) exportedFunction(provider *proposalReplayModule, field string) (any, error) {
	gfi, ok := provider.compiled.Exports[field]
	if !ok || gfi < 0 {
		return nil, fmt.Errorf("unknown import function %q", field)
	}
	if gfi >= provider.compiled.NumImports {
		return provider.inst.ExportedFunc(field)
	}
	key := provider.compiled.Imports[gfi]
	if value, ok := provider.imports[key].(*InstanceExport); ok {
		return value, nil
	}
	if value, ok := provider.imports[key].(*HostFuncRef); ok {
		return value, nil
	}
	fn, ok := provider.imports[key].(HostFunc)
	if !ok || fn == nil || gfi >= len(provider.compiled.importFuncSigs) {
		return nil, fmt.Errorf("exported imported function %q has no typed owner", field)
	}
	if provider.hostExports == nil {
		provider.hostExports = make(map[string]*HostFuncRef)
	}
	if ref := provider.hostExports[field]; ref != nil {
		return ref, nil
	}
	ref, err := s.rt.NewHostFuncRef(fn, provider.compiled.importFuncSigs[gfi])
	if err != nil {
		return nil, err
	}
	provider.hostExports[field] = ref
	s.hostRefs = append(s.hostRefs, ref)
	return ref, nil
}

func proposalNegativeFailureMatches(want string, err error) bool {
	if err == nil {
		return false
	}
	want = strings.ToLower(want)
	got := strings.ToLower(err.Error())
	if strings.HasPrefix(want, "type mismatch") {
		return strings.Contains(got, "type mismatch")
	}
	for _, prefix := range []string{"unknown function", "unknown global", "unknown tag"} {
		if strings.HasPrefix(want, prefix) {
			return strings.Contains(got, prefix)
		}
	}
	switch want {
	case "incompatible import type":
		return strings.Contains(got, "incompatible import type") ||
			strings.Contains(got, "signature mismatch") ||
			strings.Contains(got, "type mismatch") ||
			strings.Contains(got, "mutability mismatch") ||
			strings.Contains(got, "element type") && strings.Contains(got, "incompatible")
	case "unknown import":
		return strings.Contains(got, "unknown import")
	case "unknown table":
		return strings.Contains(got, "unknown table")
	case "invalid result arity":
		return strings.Contains(got, "select type arity") || strings.Contains(got, "invalid result arity")
	case "size minimum must not be greater than maximum":
		return strings.Contains(got, "invalid limits") && strings.Contains(got, "max < min")
	case "constant expression required":
		// The typed-reference corpus predates extended-constant expressions.
		// When Wago accepts the arithmetic expression under that enabled proposal,
		// the declaration is still invalid if its resulting type is incompatible.
		return strings.Contains(got, "constant expression required") || strings.Contains(got, "type mismatch")
	case "out of bounds table access":
		return strings.Contains(got, "out of bounds") && (strings.Contains(got, "table") || strings.Contains(got, "element segment"))
	case "out of bounds memory access":
		return strings.Contains(got, "out of bounds") && (strings.Contains(got, "memory") || strings.Contains(got, "data segment"))
	case "unreachable":
		return strings.Contains(got, "unreachable")
	case "indirect call type mismatch":
		return strings.Contains(got, "indirect call") && (strings.Contains(got, "wrong signature") || strings.Contains(got, "type mismatch"))
	case "undefined element", "uninitialized element":
		return strings.Contains(got, "indirect call") && strings.Contains(got, "out of bounds")
	default:
		return strings.Contains(got, want)
	}
}

func (s *proposalReplayState) close(t *testing.T, table *Table, memory *Memory) {
	t.Helper()
	for i := len(s.live) - 1; i >= 0; i-- {
		m := s.live[i]
		if m.inst != nil {
			if err := m.inst.Close(); err != nil {
				t.Errorf("close replay instance %d: %v", i, err)
			}
		}
		if m.compiled != nil {
			if err := m.compiled.Close(); err != nil {
				t.Errorf("close replay compiled module %d: %v", i, err)
			}
		}
	}
	for i := len(s.hostRefs) - 1; i >= 0; i-- {
		if err := s.hostRefs[i].Close(); err != nil {
			t.Errorf("close replay host function %d: %v", i, err)
		}
	}
	if err := table.Close(); err != nil {
		t.Errorf("close spectest.table: %v", err)
	}
	if err := memory.Close(); err != nil {
		t.Errorf("close spectest.memory: %v", err)
	}
	if err := s.rt.Close(); err != nil {
		t.Errorf("close proposal replay runtime: %v", err)
	}
}
