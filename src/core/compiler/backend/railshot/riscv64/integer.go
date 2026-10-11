//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

const (
	TrapUnreachable     = 2
	TrapDivideByZero    = 3
	TrapIntegerOverflow = 4
	integerBudget       = 16384
	maxTableEntries     = 1024
)

type integerSummary struct {
	maxStack         int
	context          bool
	calls            int
	tableBridgeBytes int
	importTails      int
}
type integerControl struct {
	base                           int
	kind                           byte
	params, results                []bool
	paramRefs, resultRefs          []bool
	entry, then, incoming, hasElse bool
	unreachable                    bool // current typing floor, independent of machine reachability
	start, falseSite               int
	sites                          []int
}
type integerTrap struct {
	site, code int
	sourcePC   uint32
}
type integerEmitter struct {
	target
	traps      []integerTrap
	calls      []directCall
	propagate  []int
	frameBytes int
}

// The separate fallback uses fixed temporary registers and canonical operand
// slots, not shared value identities or a second register allocator. One walker
// performs complete pre-emission admission and then lowering of the same bytes.
func walkInteger(fn *wasm.Func, ft *wasm.CompType, state *moduleState, emit *integerEmitter) (integerSummary, error) {
	floating := state != nil && state.production
	if fn == nil {
		return integerSummary{}, unsupported("missing integer function")
	}
	profile := memoryProfile{}
	referenceTypes := internalReferenceTypes{}
	if state != nil {
		if len(state.types) > MaxFunctions {
			return integerSummary{}, unsupported("integer control type directory bound")
		}
		profile = makeMemoryProfile(state.memories, state.indexedMemories)
		referenceTypes.scalar = state.types
		referenceTypes.declarationExtern = state.declarationExternRefs
		referenceTypes.executionExtern = state.core3RefIsNullExecution
		referenceTypes.declarationNonNullExtern = state.declarationNonnullRefs
		referenceTypes.publicReferenceCarriers = state.publicReferenceCarriers
	}
	var locals [256]bool
	var localRefs [256]bool
	privateRefs := floating && (state.ownedTable || state.importReferences)
	functionRefs := state != nil && state.referenceFunctions
	if functionRefs {
		if !privateRefs || state.importedFunctions < 0 || state.importedFunctions > MaxHostFunctionImports || state.importedFunctions > len(state.functions) || state.dynamicTailCalls && (!state.localTailCalls || !state.importTailCalls || !state.foreignTableCalls) {
			return integerSummary{}, unsupported("private reference functions require same-image direct calls")
		}
		if len(state.functions) > MaxFunctions {
			return integerSummary{}, unsupported("private reference function directory bound")
		}
		for i, sig := range state.functions {
			if i < state.importedFunctions && !(state.publicReferenceCarriers && publicReferenceCarrierTarget(sig.signature)) {
				if err := integerNumericSignature(sig.signature, floating); err != nil {
					return integerSummary{}, err
				}
			}
			if _, _, err := admitDeclaredControlSignature(sig.signature, referenceTypes, true, state.referenceCalls, false); err != nil {
				return integerSummary{}, err
			}
		}
		if _, _, err := admitDeclaredControlSignature(ft, referenceTypes, true, state.referenceCalls, false); err != nil {
			return integerSummary{}, err
		}
	} else if err := integerNumericSignature(ft, floating); err != nil {
		return integerSummary{}, err
	}

	// Public ref.func bodies use the existing instance descriptor directory;
	// they do not require a table or a reference-valued global.
	descriptorRefs := privateRefs
	nlocals := len(ft.Params)
	for i, t := range ft.Params {
		localRefs[i] = functionRefs && t.Kind() == wasm.ValRef
		locals[i] = scalarWide(t) || localRefs[i]
	}
	for _, run := range fn.Locals.Runs {
		if run.Count != 0 && !scalarType(run.Type, floating) && !(privateRefs && internalFunctionReferenceWithTypes(run.Type, state.referenceCalls, referenceTypes)) {
			return integerSummary{}, unsupported("noninteger local")
		}
		if int(run.Count) > 256-nlocals {
			return integerSummary{}, unsupported("local bound")
		}
		for i := uint32(0); i < run.Count; i++ {
			localRefs[nlocals] = internalFunctionReferenceWithTypes(run.Type, true, referenceTypes)
			locals[nlocals] = scalarWide(run.Type) || localRefs[nlocals]
			nlocals++
		}
	}
	if len(fn.BodyBytes) == 0 || len(fn.BodyBytes) > 64<<10 {
		return integerSummary{}, unsupported("integer body bound")
	}
	var stack [512]bool
	var stackRefs [512]bool
	// Unreachable untyped selects may produce a value whose carrier class is
	// still polymorphic. Keep it distinct from a known i32 until a typed use.
	var stackUnknown [512]bool
	var ctrl [1 + maxControlNesting]integerControl
	var rootResults, rootRefs [256]bool
	for j, t := range ft.Results {
		rootRefs[j] = functionRefs && t.Kind() == wasm.ValRef
		rootResults[j] = scalarWide(t) || rootRefs[j]
	}
	var inlineResults = [2][1]bool{{false}, {true}}
	var inlineReference = [1]bool{true}
	ctrl[0] = integerControl{kind: 0xff, results: rootResults[:len(ft.Results)], resultRefs: rootRefs[:len(ft.Results)], entry: true}
	depth, h, peak, budget := 1, 0, 0, nlocals
	live := true
	contextUsed := false
	callCount := 0
	tableBridgeBytes := 0
	importTails := 0
	popAny := func() (bool, bool, error) {
		if h <= ctrl[depth-1].base {
			if !live {
				return false, false, nil
			}
			return false, false, fmt.Errorf("rv64: validated operand stack underflow")
		}
		h--
		return stack[h], stackRefs[h], nil
	}
	popClass := func(reference bool) (bool, error) {
		wide, ref, err := popAny()
		if err == nil && live && ref != reference {
			return false, unsupported("private reference carrier class")
		}
		return wide, err
	}
	pop := func() (bool, error) { return popClass(false) }
	popWidth := func(wide bool) error {
		w, err := pop()
		if err == nil && live && w != wide {
			return unsupported("memory operand width")
		}
		return err
	}
	push := func(w bool) error {
		if h >= len(stack) {
			return unsupported("integer operand bound")
		}
		stack[h] = w
		stackRefs[h] = false
		stackUnknown[h] = false
		h++
		peak = max(peak, h)
		return nil
	}
	pushReference := func() error {
		if err := push(true); err != nil {
			return err
		}
		stackRefs[h-1] = true
		return nil
	}
	// Match against the current typing floor, never the target label's lower
	// base. Missing values are legal only in this frame's explicit dead region.
	match := func(values, references []bool, exact, refine bool) error {
		floor := ctrl[depth-1].base
		missing := max(0, len(values)-(h-floor))
		if missing != 0 && !ctrl[depth-1].unreachable || h+missing > len(stack) {
			return unsupported("integer control operand bound/underflow")
		}
		if exact && h+missing != floor+len(values) {
			return unsupported("integer control result stack")
		}
		for i, wide := range values {
			at := h - len(values) + i
			ref := len(references) != 0 && references[i]
			if at >= floor && !stackUnknown[at] && (stack[at] != wide || stackRefs[at] != ref) {
				return unsupported("integer control carrier class")
			}
		}
		if !refine {
			return nil
		}
		if missing != 0 {
			copy(stack[floor+missing:h+missing], stack[floor:h])
			copy(stackRefs[floor+missing:h+missing], stackRefs[floor:h])
			copy(stackUnknown[floor+missing:h+missing], stackUnknown[floor:h])
			h += missing
		}
		for i, wide := range values {
			at := h - len(values) + i
			stack[at], stackRefs[at], stackUnknown[at] = wide, len(references) != 0 && references[i], false
		}
		return nil
	}
	condition := func() error {
		if err := match(inlineResults[0][:], nil, false, true); err != nil {
			return err
		}
		h--
		return nil
	}
	position := func() int {
		if emit == nil {
			return 0
		}
		return emit.Position()
	}
	patch := func(site, pos int) error {
		if emit == nil {
			return nil
		}
		return emit.Patch(site, pos)
	}
	branch := func(label uint32, retainPayload bool) error {
		if int(label) >= depth {
			return fmt.Errorf("rv64: invalid validated label")
		}
		fr := &ctrl[depth-1-int(label)]
		values, references := fr.results, fr.resultRefs
		if fr.kind == 0x03 {
			values, references = fr.params, fr.paramRefs
		}
		if err := match(values, references, false, retainPayload); err != nil {
			return err
		}
		budget += len(values)
		if budget > integerBudget {
			return unsupported("integer branch transfer budget")
		}
		if !live {
			return nil
		}
		if h-len(values) < fr.base {
			return fmt.Errorf("rv64: missing validated branch values")
		}
		if emit != nil {
			// Destinations are at or below sources; forward copying is safe for
			// overlapping vectors. Keep br_table's index in X5 intact.
			for j, wide := range values {
				emit.Load(7, emit.SpillOffset(h-len(values)+j), wide)
				emit.Store(emit.SpillOffset(fr.base+j), 7, wide)
			}
		}
		if emit != nil {
			site := emit.Jump()
			if fr.kind == 0x03 {
				if err := patch(site, fr.start); err != nil {
					return err
				}
			} else {
				fr.sites = append(fr.sites, site)
			}
		}
		if fr.kind != 0x03 {
			fr.incoming = true
		}
		return nil
	}
	r := wasm.ReaderFrom(fn.BodyBytes)
	for depth > 0 {
		if emit != nil && emit.mode.production {
			emit.sourcePC = uint32(fn.LocalDeclBytes) + uint32(r.Offset())
			emit.profileOpcode(emit.sourcePC)
		}
		op, err := r.Byte()
		if err != nil {
			return integerSummary{}, err
		}
		budget++
		if budget > integerBudget {
			return integerSummary{}, unsupported("integer instruction/label budget")
		}
		switch op {
		case 0x00:
			if live && emit != nil {
				emit.trap(TrapUnreachable, emit.Jump())
			}
			live = false
			ctrl[depth-1].unreachable = true
			h = ctrl[depth-1].base
		case 0x01:
		case 0x02, 0x03, 0x04:
			bt, e := internalReferenceBlockType(&r, privateRefs && state.referenceCalls, referenceTypes)
			if e != nil {
				return integerSummary{}, e
			}
			var params, results, paramRefs, resultRefs []bool
			switch bt {
			case -64: // empty
			case -1:
				results = inlineResults[0][:]
			case -2:
				results = inlineResults[1][:]
			case -16: // nullable abstract funcref, an internal full-width carrier
				if !privateRefs {
					return integerSummary{}, unsupported("reference block without table profile")
				}
				results, resultRefs = inlineResults[1][:], inlineReference[:]
			case -3, -4:
				if !floating {
					return integerSummary{}, unsupported("FP block outside production")
				}
				if bt == -3 {
					results = inlineResults[0][:]
				} else {
					results = inlineResults[1][:]
				}
			default:
				if bt < 0 || state == nil || bt >= int64(len(state.types)) {
					return integerSummary{}, unsupported("noninteger block signature")
				}
				params, results, paramRefs, resultRefs, e = integerControlSignature(state.types[bt], referenceTypes, privateRefs, privateRefs && state.referenceCalls, floating, &budget)
				if e != nil {
					return integerSummary{}, e
				}
			}
			if bt < 0 {
				budget += len(params) + len(results)
			}
			if budget > integerBudget {
				return integerSummary{}, unsupported("integer control signature budget")
			}
			if depth == len(ctrl) {
				return integerSummary{}, unsupported("integer control bound")
			}
			if op == 0x04 {
				if e = condition(); e != nil {
					return integerSummary{}, e
				}
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), false)
				}
			}
			if e = match(params, paramRefs, false, true); e != nil {
				return integerSummary{}, e
			}
			h -= len(params)
			fr := integerControl{base: h, kind: op, params: params, results: results, paramRefs: paramRefs, resultRefs: resultRefs, entry: live, start: position(), falseSite: -1}
			if op == 0x03 && live && emit != nil {
				emit.emitProductionInterruptPoll()
			}
			if op == 0x04 && live && emit != nil {
				fr.falseSite = emit.BranchZero(5)
			}
			ctrl[depth] = fr
			depth++
			for i, wide := range params {
				if err = push(wide); err != nil {
					return integerSummary{}, err
				}
				stackRefs[h-1] = len(paramRefs) != 0 && paramRefs[i]
			}
		case 0x05:
			fr := &ctrl[depth-1]
			if fr.kind != 0x04 || fr.hasElse {
				return integerSummary{}, fmt.Errorf("rv64: invalid validated else")
			}
			if err = match(fr.results, fr.resultRefs, true, true); err != nil {
				return integerSummary{}, err
			}
			fr.then = live
			fr.hasElse = true
			fr.unreachable = false
			if live && emit != nil {
				fr.sites = append(fr.sites, emit.Jump())
			}
			if fr.entry && emit != nil {
				if err = patch(fr.falseSite, position()); err != nil {
					return integerSummary{}, err
				}
			}
			live = fr.entry
			h = fr.base
			for i, wide := range fr.params {
				if err = push(wide); err != nil {
					return integerSummary{}, err
				}
				stackRefs[h-1] = len(fr.paramRefs) != 0 && fr.paramRefs[i]
			}
		case 0x0b:
			fr := ctrl[depth-1]
			if fr.kind == 0x04 && !fr.hasElse && !integerSameClasses(fr.params, fr.paramRefs, fr.results, fr.resultRefs) {
				return integerSummary{}, unsupported("integer result if requires else")
			}
			if err = match(fr.results, fr.resultRefs, true, true); err != nil {
				return integerSummary{}, err
			}
			if emit != nil {
				for _, site := range fr.sites {
					if err = patch(site, position()); err != nil {
						return integerSummary{}, err
					}
				}
				if fr.kind == 0x04 && !fr.hasElse && fr.entry {
					if err = patch(fr.falseSite, position()); err != nil {
						return integerSummary{}, err
					}
				}
			}
			live = live || fr.incoming || fr.then || fr.kind == 0x04 && !fr.hasElse && fr.entry
			h = fr.base
			for index, wide := range fr.results {
				if err = push(wide); err != nil {
					return integerSummary{}, err
				}
				stackRefs[h-1] = len(fr.resultRefs) != 0 && fr.resultRefs[index]
			}
			depth--
			if depth == 0 && live && emit != nil {
				if len(fr.results) == 1 {
					emit.Load(5, emit.SpillOffset(0), fr.results[0])
					emit.Return(5, fr.results[0], 0)
				} else {
					emit.a.Ld(enc.Scratch0, enc.SP, 8)
					for j, wide := range fr.results {
						emit.Load(5, emit.SpillOffset(j), wide)
						emit.a.Sd(enc.X5, enc.Scratch0, int32(j*8))
					}
					emit.Constant(10, 0, true)
				}
			}
		case 0x0c, 0x0d:
			label, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			if op == 0x0d {
				if e = condition(); e != nil {
					return integerSummary{}, e
				}
				skip := -1
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), false)
					skip = emit.BranchZero(5)
				}
				if e = branch(label, true); e != nil {
					return integerSummary{}, e
				}
				if live && emit != nil {
					if e = patch(skip, position()); e != nil {
						return integerSummary{}, e
					}
				}
			} else {
				if e = branch(label, false); e != nil {
					return integerSummary{}, e
				}
				live = false
				ctrl[depth-1].unreachable = true
				h = ctrl[depth-1].base
			}
		case 0xd5, 0xd6:
			label, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			if !privateRefs || !state.referenceCalls || uint64(label) >= uint64(depth) {
				return integerSummary{}, unsupported("reference branch profile/label")
			}
			fr := &ctrl[depth-1-int(label)]
			values, references := fr.results, fr.resultRefs
			if fr.kind == 0x03 {
				values, references = fr.params, fr.paramRefs
			}
			nonNull := op == 0xd6
			prefix := len(values)
			if nonNull {
				if prefix == 0 || len(references) == 0 || !references[prefix-1] {
					return integerSummary{}, unsupported("br_on_non_null requires a reference label result")
				}
				prefix--
			}
			floor := ctrl[depth-1].base
			if h <= floor && !ctrl[depth-1].unreachable {
				return integerSummary{}, unsupported("reference branch operand underflow")
			}
			if h > floor && !stackUnknown[h-1] && !stackRefs[h-1] {
				return integerSummary{}, unsupported("reference branch operand class")
			}
			if _, err = popClass(true); err != nil {
				return integerSummary{}, err
			}
			// Validate label payloads against this control frame's typing floor,
			// including concrete operands in dead code. Missing dead operands are
			// virtual; they never cause loads or copies in the emitted function.
			missing := max(0, prefix-(h-floor))
			if missing != 0 && !ctrl[depth-1].unreachable || h+missing >= len(stack) {
				return integerSummary{}, unsupported("reference branch payload bound/underflow")
			}
			for j := 0; j < prefix; j++ {
				at := h - prefix + j
				ref := len(references) != 0 && references[j]
				if at >= floor && !stackUnknown[at] && (stack[at] != values[j] || stackRefs[at] != ref) {
					return integerSummary{}, unsupported("reference branch payload class")
				}
			}
			if missing != 0 {
				copy(stack[floor+missing:h+missing], stack[floor:h])
				copy(stackRefs[floor+missing:h+missing], stackRefs[floor:h])
				copy(stackUnknown[floor+missing:h+missing], stackUnknown[floor:h])
				h += missing
			}
			// Label payloads are popped and restored with their declared types;
			// this also specializes present unknown and missing virtual values.
			for j := 0; j < prefix; j++ {
				at := h - prefix + j
				stack[at] = values[j]
				stackRefs[at] = len(references) != 0 && references[j]
				stackUnknown[at] = false
			}
			skip := -1
			if live && emit != nil {
				emit.Load(5, emit.SpillOffset(h), true)
				condition := enc.NE // non-null fallthrough skips the taken null edge
				if nonNull {
					condition = enc.EQ
				}
				skip = emit.a.BranchLong(condition, enc.X5, enc.Zero)
			}
			if nonNull {
				if err = pushReference(); err != nil {
					return integerSummary{}, err
				}
			}
			if err = branch(label, true); err != nil {
				return integerSummary{}, err
			}
			if live && emit != nil {
				if err = patch(skip, position()); err != nil {
					return integerSummary{}, err
				}
			}
			if nonNull {
				h-- // null fallthrough consumes the reference, retaining the prefix
			} else if err = pushReference(); err != nil {
				return integerSummary{}, err
			}
		case 0x0e:
			n, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			if n > maxTableEntries || int(n) > integerBudget-budget {
				return integerSummary{}, unsupported("branch table bound")
			}
			budget += int(n)
			if e = condition(); e != nil {
				return integerSummary{}, e
			}
			if live && emit != nil {
				emit.Load(5, emit.SpillOffset(h), false)
			}
			signatureArity := -1
			for i := uint32(0); i <= n; i++ {
				label, e := r.U32()
				if e != nil {
					return integerSummary{}, e
				}
				if uint64(label) >= uint64(depth) {
					return integerSummary{}, unsupported("integer branch table label")
				}
				fr := &ctrl[depth-1-int(label)]
				values := fr.results
				if fr.kind == 0x03 {
					values = fr.params
				}
				if signatureArity < 0 {
					signatureArity = len(values)
				} else if signatureArity != len(values) {
					return integerSummary{}, unsupported("integer branch table arity")
				}
				// Each target checks the original payload independently. Unknown
				// dead values can match heterogeneous equal-arity label types.
				skip := -1
				if i < n && live && emit != nil {
					emit.Constant(6, int64(i), false)
					skip = emit.a.BranchLong(enc.NE, enc.X5, enc.X6)
				}
				if e = branch(label, false); e != nil {
					return integerSummary{}, e
				}
				if i < n && live && emit != nil {
					if e = patch(skip, position()); e != nil {
						return integerSummary{}, e
					}
				}
			}
			live = false
			ctrl[depth-1].unreachable = true
			h = ctrl[depth-1].base
		case 0x0f:
			if err = branch(uint32(depth-1), false); err != nil {
				return integerSummary{}, err
			}
			live = false
			ctrl[depth-1].unreachable = true
			h = ctrl[depth-1].base
		case 0x1a:
			if _, _, err = popAny(); err != nil {
				return integerSummary{}, err
			}
		case 0x1b, 0x1c:
			var typedWide, typedRef bool
			if op == 0x1c {
				n, e := r.U32()
				if e != nil {
					return integerSummary{}, e
				}
				bt, e := internalSelectType(&r, privateRefs && state.referenceCalls, referenceTypes)
				if e != nil {
					return integerSummary{}, e
				}
				if n != 1 || bt != 0x7f && bt != 0x7e && (!floating || bt != 0x7d && bt != 0x7c) && !(privateRefs && bt == 0x70) {
					return integerSummary{}, unsupported("noninteger typed select")
				}
				typedRef = bt == 0x70
				typedWide = bt == 0x7e || bt == 0x7c || typedRef
			}
			// Missing operands are polymorphic only at this frame's explicit
			// unreachable floor. Concrete dead operands retain their classes.
			floor := ctrl[depth-1].base
			missing := max(0, 3-(h-floor))
			if missing != 0 && !ctrl[depth-1].unreachable {
				return integerSummary{}, unsupported("select operand underflow")
			}
			if h+missing > len(stack) {
				return integerSummary{}, unsupported("select operand bound")
			}
			if missing != 0 {
				copy(stack[floor+missing:h+missing], stack[floor:h])
				copy(stackRefs[floor+missing:h+missing], stackRefs[floor:h])
				copy(stackUnknown[floor+missing:h+missing], stackUnknown[floor:h])
				for i := 0; i < missing; i++ {
					stack[floor+i], stackRefs[floor+i] = false, false
					stackUnknown[floor+i] = true
				}
				h += missing
			}
			cond := h - 1
			if !stackUnknown[cond] && (stack[cond] || stackRefs[cond]) {
				return integerSummary{}, unsupported("select condition class/width")
			}
			left, right := h-3, h-2
			w, unknown := typedWide, false
			if op == 0x1c {
				for _, at := range [2]int{left, right} {
					if !stackUnknown[at] && (stack[at] != typedWide || stackRefs[at] != typedRef) {
						return integerSummary{}, unsupported("typed select operand class/width")
					}
				}
			} else {
				if !stackUnknown[left] && stackRefs[left] || !stackUnknown[right] && stackRefs[right] {
					return integerSummary{}, unsupported("untyped select reference operand")
				}
				if !stackUnknown[left] && !stackUnknown[right] && stack[left] != stack[right] {
					return integerSummary{}, unsupported("untyped select operand width")
				}
				unknown = stackUnknown[left] && stackUnknown[right]
				w = stack[left]
				if stackUnknown[left] {
					w = stack[right]
				}
			}
			h -= 3
			if live && emit != nil {
				emit.Load(5, emit.SpillOffset(cond), false)
				emit.Load(6, emit.SpillOffset(h+1), w)
				emit.Load(7, emit.SpillOffset(h), w)
				site := emit.BranchZero(5)
				emit.Move(5, 7, w)
				end := emit.Jump()
				if err = patch(site, position()); err != nil {
					return integerSummary{}, err
				}
				emit.Move(5, 6, w)
				if err = patch(end, position()); err != nil {
					return integerSummary{}, err
				}
				emit.Store(emit.SpillOffset(h), 5, w)
			}
			if err = push(w); err != nil {
				return integerSummary{}, err
			}
			stackRefs[h-1] = typedRef
			stackUnknown[h-1] = unknown
		case 0x20, 0x21, 0x22:
			idx, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			if int(idx) >= nlocals {
				return integerSummary{}, fmt.Errorf("rv64: invalid validated local")
			}
			w := locals[idx]
			if op == 0x20 {
				if live && emit != nil {
					emit.Load(5, emit.LocalOffset(int(idx)), w)
					emit.Store(emit.SpillOffset(h), 5, w)
				}
				if err = push(w); err != nil {
					return integerSummary{}, err
				}
				stackRefs[h-1] = localRefs[idx]
			} else {
				if _, err = popClass(localRefs[idx]); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), w)
					emit.Store(emit.LocalOffset(int(idx)), 5, w)
				}
				if op == 0x22 {
					if err = push(w); err != nil {
						return integerSummary{}, err
					}
					stackRefs[h-1] = localRefs[idx]
				}
			}
		case 0xd0, 0xd1, 0xd2, 0xd4, 0x25, 0x26:
			if !privateRefs {
				return integerSummary{}, unsupported("reference instruction without retained reference state")
			}
			switch op {
			case 0xd0:
				heap, e := r.S33()
				if e != nil {
					return integerSummary{}, e
				}
				if heap != -16 && !(state.core3RefIsNullExecution && heap == -17) && (!(state.dynamicTailCalls && !functionRefs || state.referenceCalls) || heap < 0 || !referenceTypes.function(uint32(heap))) {
					return integerSummary{}, unsupported("private abstract funcref null only")
				}
				if live && emit != nil {
					emit.Constant(5, 0, true)
					emit.Store(emit.SpillOffset(h), 5, true)
				}
				if err = pushReference(); err != nil {
					return integerSummary{}, err
				}
			case 0xd1:
				if _, err = popClass(true); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), true)
					emit.a.Sltiu(enc.X5, enc.X5, 1)
					emit.Store(emit.SpillOffset(h), 5, false)
				}
				if err = push(false); err != nil {
					return integerSummary{}, err
				}
			case 0xd4:
				if !state.referenceCalls {
					return integerSummary{}, unsupported("ref.as_non_null requires private typed-reference profile")
				}
				// An unknown value at an unreachable floor is polymorphic. An
				// explicitly pushed numeric value still cannot become a reference.
				if h <= ctrl[depth-1].base && !ctrl[depth-1].unreachable {
					return integerSummary{}, unsupported("ref.as_non_null operand underflow")
				}
				if h > ctrl[depth-1].base && !stackUnknown[h-1] && !stackRefs[h-1] {
					return integerSummary{}, unsupported("ref.as_non_null operand class")
				}
				if _, err = popClass(true); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.referenceAsNonNull(emit.SpillOffset(h))
				}
				if err = pushReference(); err != nil {
					return integerSummary{}, err
				}
			case 0xd2:
				idx, e := r.U32()
				if e != nil {
					return integerSummary{}, e
				}
				if !descriptorRefs || uint64(idx) < uint64(state.importedFunctions) && !state.importReferences || uint64(idx) >= uint64(len(state.functions)) {
					return integerSummary{}, unsupported("private ref.func index")
				}
				if live && emit != nil {
					emit.functionReference(idx)
					emit.Store(emit.SpillOffset(h), 5, true)
				}
				if err = pushReference(); err != nil {
					return integerSummary{}, err
				}
			case 0x25, 0x26:
				idx, e := r.U32()
				if e != nil {
					return integerSummary{}, e
				}
				tableWide, tableOK := state.tableWidth(idx)
				if !tableOK {
					return integerSummary{}, unsupported("private table index")
				}
				compactReference, compactOK := state.tableCompactReference(idx)
				if !compactOK {
					return integerSummary{}, unsupported("private table reference layout")
				}
				if op == 0x26 {
					if functionRefs {
						err = match(inlineResults[1][:], inlineReference[:], false, true)
						if err == nil {
							h--
						}
					} else {
						_, err = popClass(true)
					}
					if err != nil {
						return integerSummary{}, err
					}
					if live && emit != nil {
						emit.Load(28, emit.SpillOffset(h), true)
					}
				}
				if functionRefs {
					if tableWide {
						err = popWidth(true)
					} else {
						err = condition() // Exact i32 at this frame's current floor.
					}
				} else {
					err = popWidth(tableWide)
				}
				if err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), tableWide)
					emit.privateTableEntry(idx, compactReference)
					if op == 0x25 {
						if compactReference {
							emit.a.Ld(enc.X5, enc.X6, 0)
						} else {
							emit.a.Ld(enc.X5, enc.X6, 24)
						}
						emit.Store(emit.SpillOffset(h), 5, true)
					} else {
						if compactReference {
							emit.a.Sd(enc.X28, enc.X6, 0)
						} else {
							emit.storePrivateReference()
						}
					}
				}
				if op == 0x25 {
					if err = pushReference(); err != nil {
						return integerSummary{}, err
					}
				}
			}
		case 0x10:
			idx, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			if state == nil || uint64(idx) >= uint64(len(state.functions)) {
				return integerSummary{}, unsupported("nonlocal direct call")
			}
			sig := state.functions[idx]
			callCount++
			if callCount > maxDirectCalls {
				return integerSummary{}, unsupported("direct call count/slot budget")
			}
			var paramRefs, resultRefs []bool
			if functionRefs {
				sig.params, sig.results, paramRefs, resultRefs, err = integerControlSignature(sig, referenceTypes, true, state.referenceCalls, floating, &budget)
				if err != nil {
					return integerSummary{}, err
				}
				if err = match(sig.params, paramRefs, false, true); err != nil {
					return integerSummary{}, err
				}
				h -= len(sig.params)
			} else {
				if err = integerCallable(sig, floating); err != nil {
					return integerSummary{}, err
				}
				budget += len(sig.params) + len(sig.results)
				if budget > integerBudget {
					return integerSummary{}, unsupported("direct call count/slot budget")
				}
				for range sig.params {
					if _, err = pop(); err != nil {
						return integerSummary{}, err
					}
				}
			}
			if live && emit != nil {
				emit.directCall(idx, sig, h, state.importedFunctions)
			}
			for i, wide := range sig.results {
				if err = push(wide); err != nil {
					return integerSummary{}, err
				}
				stackRefs[h-1] = len(resultRefs) != 0 && resultRefs[i]
			}
		case 0x12:
			idx, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			if state == nil || !state.localTailCalls || !state.production || uint64(idx) >= uint64(len(state.functions)) || idx < uint32(state.importedFunctions) && !state.importTailCalls {
				return integerSummary{}, unsupported("defined local tail target required")
			}
			sig := state.functions[idx]
			importedTail := idx < uint32(state.importedFunctions)
			if importedTail {
				importTails++
			}
			callCount++
			if callCount > maxDirectCalls {
				return integerSummary{}, unsupported("local tail count/slot budget")
			}
			if functionRefs {
				// The private directory already admits both signatures. Carrier
				// classes alone cannot establish exact heap/nullability results.
				if err = admitReferenceTailResults(ft, sig.signature); err != nil {
					return integerSummary{}, err
				}
				var paramRefs []bool
				sig.params, sig.results, paramRefs, _, err = integerControlSignature(sig, referenceTypes, true, state.referenceCalls, floating, &budget)
				if err != nil {
					return integerSummary{}, err
				}
				if err = match(sig.params, paramRefs, false, true); err != nil {
					return integerSummary{}, err
				}
				h -= len(sig.params)
			} else {
				if err = integerCallable(sig, floating); err != nil {
					return integerSummary{}, err
				}
				if err = admitLocalTailSignature(ft, sig.signature); err != nil {
					return integerSummary{}, err
				}
				budget += len(sig.params) + len(sig.results)
				if budget > integerBudget {
					return integerSummary{}, unsupported("local tail count/slot budget")
				}
				for j := len(sig.params) - 1; j >= 0; j-- {
					if err = popWidth(sig.params[j]); err != nil {
						return integerSummary{}, err
					}
				}
			}
			if live && emit != nil {
				if importedTail {
					emit.importTailCall(idx, sig.signature, h, emit.SpillOffset, 8)
				} else {
					emit.localTailCall(idx, sig, h, state.importedFunctions)
				}
			}
			live = false
			ctrl[depth-1].unreachable = true
			h = ctrl[depth-1].base
		case 0x14:
			typeIndex, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			if state == nil || !state.production || !state.referenceCalls || !state.foreignTableCalls || !privateRefs || uint64(typeIndex) >= uint64(len(state.types)) {
				return integerSummary{}, unsupported("call_ref retained owner/signature contract")
			}
			sig := state.types[typeIndex]
			callParams := sig.params
			callResults := sig.results
			callParamRefs := []bool(nil)
			callResultRefs := []bool(nil)
			referenceCarriers := false
			if err = integerCallable(sig, floating); err != nil {
				if !state.publicReferenceCarrierType(typeIndex) || sig.signature == nil || !publicReferenceCarrierTarget(sig.signature) {
					return integerSummary{}, err
				}
				callParams, callResults, callParamRefs, callResultRefs, err = integerControlSignature(sig, referenceTypes, true, true, floating, &budget)
				if err != nil {
					return integerSummary{}, err
				}
				referenceCarriers = true
			}
			callCount++
			if !referenceCarriers {
				budget += len(sig.params) + len(sig.results)
			}
			budget++ // callable reference
			tableBridgeBytes += nativeTableCallCodeBound(len(sig.params), len(sig.results))
			if callCount > maxDirectCalls || budget > integerBudget {
				return integerSummary{}, unsupported("call_ref count/slot budget")
			}
			if functionRefs {
				if err = match(inlineResults[1][:], inlineReference[:], false, true); err != nil {
					return integerSummary{}, err
				}
				h--
			} else if _, err = popClass(true); err != nil {
				return integerSummary{}, err
			}
			referenceSlot := h
			if functionRefs {
				if err = match(callParams, callParamRefs, false, true); err != nil {
					return integerSummary{}, err
				}
				h -= len(callParams)
			} else {
				for j := len(sig.params) - 1; j >= 0; j-- {
					if err = popWidth(sig.params[j]); err != nil {
						return integerSummary{}, err
					}
				}
			}
			if live && emit != nil {
				emit.referenceCall(sig.signature, sig.key, h, referenceSlot, emit.SpillOffset, 8)
			}
			for i, wide := range callResults {
				if len(callResultRefs) != 0 && callResultRefs[i] {
					err = pushReference()
				} else {
					err = push(wide)
				}
				if err != nil {
					return integerSummary{}, err
				}
			}

		case 0x15:
			typeIndex, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			if state == nil || !state.production || !state.dynamicTailCalls || !state.localTailCalls || !state.importTailCalls || !state.foreignTableCalls || functionRefs && !state.referenceCalls || !privateRefs || uint64(typeIndex) >= uint64(len(state.types)) {
				return integerSummary{}, unsupported("private dynamic typed tail profile required")
			}
			sig := state.types[typeIndex]
			callParams := sig.params
			callParamRefs := []bool(nil)
			referenceCarriers := false
			if err = integerCallable(sig, floating); err != nil {
				if !state.publicReferenceCarrierType(typeIndex) || sig.signature == nil || !publicReferenceCarrierTarget(sig.signature) {
					return integerSummary{}, err
				}
				callParams, _, callParamRefs, _, err = integerControlSignature(sig, referenceTypes, true, true, floating, &budget)
				if err != nil {
					return integerSummary{}, err
				}
				referenceCarriers = true
			}
			if functionRefs {
				err = admitReferenceTailResults(ft, sig.signature)
			} else {
				err = admitLocalTailSignature(ft, sig.signature)
			}
			if err != nil {
				return integerSummary{}, err
			}
			importTails++
			callCount++
			if !referenceCarriers {
				budget += len(sig.params) + len(sig.results)
			}
			budget++
			if callCount > maxDirectCalls || budget > integerBudget {
				return integerSummary{}, unsupported("typed reference tail count/slot budget")
			}
			if functionRefs {
				if err = match(inlineResults[1][:], inlineReference[:], false, true); err != nil {
					return integerSummary{}, err
				}
				h--
			} else if _, err = popClass(true); err != nil {
				return integerSummary{}, err
			}
			referenceSlot := h
			if functionRefs {
				if err = match(callParams, callParamRefs, false, true); err != nil {
					return integerSummary{}, err
				}
				h -= len(callParams)
			} else {
				for j := len(sig.params) - 1; j >= 0; j-- {
					if err = popWidth(sig.params[j]); err != nil {
						return integerSummary{}, err
					}
				}
			}
			if live && emit != nil {
				emit.dynamicReferenceTailCall(typeIndex, sig.signature, h, referenceSlot, emit.SpillOffset, 8)
			}
			live = false
			ctrl[depth-1].unreachable = true
			h = ctrl[depth-1].base
		case 0x11, 0x13:
			typeIndex, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			tableIndex, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			if state == nil || !state.production || !state.ownedTable || uint64(typeIndex) >= uint64(len(state.types)) {
				return integerSummary{}, unsupported("private table call_indirect shape")
			}
			tableWide, tableOK := state.tableWidth(tableIndex)
			if !tableOK {
				return integerSummary{}, unsupported("private table call_indirect index")
			}
			sig := state.types[typeIndex]
			if err = integerCallable(sig, floating); err != nil {
				return integerSummary{}, err
			}
			tail := op == 0x13
			if tail {
				if !state.dynamicTailCalls && (tableIndex != 0 || tableWide) {
					return integerSummary{}, unsupported("indexed indirect tail call")
				}
				// The local profile excludes imported/foreign targets; the dynamic
				// profile authenticates those through retained native factories.
				if !state.localTailCalls || state.dynamicTailCalls && (!state.importTailCalls || !state.foreignTableCalls) || !state.dynamicTailCalls && (state.foreignTableCalls || state.importedFunctions != 0) {
					return integerSummary{}, unsupported("owned defined indirect tail profile required")
				}
				if functionRefs {
					err = admitReferenceTailResults(ft, sig.signature)
				} else {
					err = admitLocalTailSignature(ft, sig.signature)
				}
				if err != nil {
					return integerSummary{}, err
				}
				if state.dynamicTailCalls {
					importTails++
				}
			} else {
				tableBridgeBytes += nativeTableCallCodeBound(len(sig.params), len(sig.results))
			}
			callCount++
			budget += len(sig.params) + len(sig.results)
			if callCount > maxDirectCalls || budget > integerBudget {
				return integerSummary{}, unsupported("indirect call count/slot budget")
			}
			if functionRefs {
				if tableWide {
					err = popWidth(true)
				} else {
					err = condition()
				}
			} else if tail {
				err = popWidth(tableWide)
			} else if tableWide {
				err = popWidth(true)
			} else {
				_, err = pop()
			}
			if err != nil {
				return integerSummary{}, err
			}
			indexSlot := h
			if functionRefs {
				if err = match(sig.params, nil, false, true); err != nil {
					return integerSummary{}, err
				}
				h -= len(sig.params)
			} else {
				for j := len(sig.params) - 1; j >= 0; j-- {
					if tail {
						err = popWidth(sig.params[j])
					} else {
						_, err = pop()
					}
					if err != nil {
						return integerSummary{}, err
					}
				}
			}
			if live && emit != nil {
				if tail {
					if state.dynamicTailCalls {
						emit.dynamicIndirectTailCall(typeIndex, sig.signature, sig.key, h, indexSlot, tableWide, tableIndex, emit.SpillOffset, 8)
					} else {
						emit.localIndirectTailCall(sig.signature, sig.key, h, indexSlot, emit.SpillOffset)
					}
				} else {
					emit.indirectCall(sig, h, indexSlot, tableWide, tableIndex)
				}
			}
			if tail {
				live = false
				ctrl[depth-1].unreachable = true
				h = ctrl[depth-1].base
				continue
			}
			for _, wide := range sig.results {
				if err = push(wide); err != nil {
					return integerSummary{}, err
				}
			}
		case 0x23, 0x24:
			idx, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			if state == nil || int(idx) >= len(state.globals) {
				return integerSummary{}, unsupported("global index")
			}
			g := state.globals[idx]
			reference := wasm.EqualValType(g.typ, wasm.FuncRef)
			if reference && (!privateRefs || !state.importReferences || g.mutable && !state.localMutableReferences) {
				return integerSummary{}, unsupported("immutable retained reference global required")
			}
			contextUsed = true
			if op == 0x24 {
				if !g.mutable {
					return integerSummary{}, unsupported("immutable global set")
				}
				if _, err = popClass(reference); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), g.wide)
					emit.global(idx, g.wide, true)
				}
			} else {
				if live && emit != nil {
					emit.global(idx, g.wide, false)
					emit.Store(emit.SpillOffset(h), 5, g.wide)
				}
				if reference {
					err = pushReference()
				} else {
					err = push(g.wide)
				}
				if err != nil {
					return integerSummary{}, err
				}
			}
		case 0xfc:
			var imm wasm.InstructionImmediate
			if err = profile.classifier.ClassifyInto(&r, op, &imm); err != nil {
				return integerSummary{}, err
			}
			if sub, ok := saturatingFloatKind(imm.Kind); ok && floating {
				if _, err = pop(); err != nil {
					return integerSummary{}, err
				}
				w, resultWide := sub&2 != 0, sub&4 != 0
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), w)
					emit.floatTrunc(w, resultWide, sub&1 != 0, true)
					emit.Store(emit.SpillOffset(h), 5, resultWide)
				}
				if err = push(resultWide); err != nil {
					return integerSummary{}, err
				}
				continue
			}
			if state == nil {
				return integerSummary{}, unsupported("bulk instruction without state")
			}
			if imm.Kind == wasm.InstrTableInit || imm.Kind == wasm.InstrElemDrop {
				if !privateRefs || imm.Kind == wasm.InstrTableInit && !state.ownedTable || uint64(imm.Index) >= uint64(state.elementCount) || imm.Kind == wasm.InstrElemDrop && imm.Index2 != 0 {
					return integerSummary{}, unsupported("private element index/shape")
				}
				if imm.Kind == wasm.InstrElemDrop {
					if live && emit != nil {
						emit.privateElementDrop(imm.Index)
					}
				} else {
					tableWide, tableOK := state.tableWidth(imm.Index2)
					if !tableOK {
						return integerSummary{}, unsupported("private table.init index")
					}
					if functionRefs {
						operands := [3]bool{}
						operands[0] = tableWide
						if err = match(operands[:], nil, false, true); err != nil {
							return integerSummary{}, err
						}
					}
					for i, reg := range []uint8{5, 7, 6} {
						wide := i == 2 && tableWide
						if err = popWidth(wide); err != nil {
							return integerSummary{}, err
						}
						if live && emit != nil {
							emit.Load(reg, emit.SpillOffset(h), wide)
						}
					}
					if live && emit != nil {
						emit.privateTableInitAt(imm.Index, imm.Index2)
					}
				}
				continue
			}
			if imm.Kind == wasm.InstrTableFill || imm.Kind == wasm.InstrTableCopy || imm.Kind == wasm.InstrTableGrow {
				tableIndex, tableWide := uint32(0), state.table64
				sourceTableIndex := uint32(0)
				if imm.Kind == wasm.InstrTableGrow || imm.Kind == wasm.InstrTableFill || imm.Kind == wasm.InstrTableCopy {
					tableIndex = imm.Index
					var tableOK bool
					tableWide, tableOK = state.tableWidth(tableIndex)
					if !tableOK || imm.Kind == wasm.InstrTableGrow && !state.ownedTableGrowth {
						return integerSummary{}, unsupported("bounded private table mutation shape")
					}
					if imm.Kind == wasm.InstrTableCopy {
						sourceTableIndex = imm.Index2
						sourceWide, sourceOK := state.tableWidth(sourceTableIndex)
						if !sourceOK || sourceWide != tableWide {
							return integerSummary{}, unsupported("cross-table private copy width")
						}
					}
				}
				if !state.ownedTable || !privateRefs || imm.Kind != wasm.InstrTableCopy && imm.Index2 != 0 {
					return integerSummary{}, unsupported("bounded private table mutation shape")
				}
				if functionRefs {
					widths, references := [3]bool{}, [3]bool{}
					n := 3
					if imm.Kind == wasm.InstrTableGrow {
						n, widths[0], widths[1], references[0] = 2, true, tableWide, true
					} else if imm.Kind == wasm.InstrTableFill {
						widths[0], widths[1], widths[2], references[1] = tableWide, true, tableWide, true
					} else if imm.Kind == wasm.InstrTableCopy && tableWide {
						widths = [3]bool{true, true, true}
					}
					if err = match(widths[:n], references[:n], false, true); err != nil {
						return integerSummary{}, err
					}
				}
				wideCount := tableWide && (imm.Kind == wasm.InstrTableGrow || imm.Kind == wasm.InstrTableFill || imm.Kind == wasm.InstrTableCopy)
				if err = popWidth(wideCount); err != nil {
					return integerSummary{}, err
				}
				deltaSlot := h
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), wideCount)
				}
				wideSource := imm.Kind == wasm.InstrTableCopy && tableWide
				if imm.Kind == wasm.InstrTableCopy {
					err = popWidth(wideSource)
				} else {
					_, err = popClass(true)
				}
				if err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(7, emit.SpillOffset(h), imm.Kind != wasm.InstrTableCopy || wideSource)
				}
				if imm.Kind != wasm.InstrTableGrow {
					wideDestination := tableWide && (imm.Kind == wasm.InstrTableFill || imm.Kind == wasm.InstrTableCopy)
					if err = popWidth(wideDestination); err != nil {
						return integerSummary{}, err
					}
					if live && emit != nil {
						emit.Load(6, emit.SpillOffset(h), wideDestination)
					}
				}
				if live && emit != nil {
					switch imm.Kind {
					case wasm.InstrTableCopy:
						emit.privateTableCopyBetween(tableIndex, sourceTableIndex)
					case wasm.InstrTableFill:
						emit.privateTableFillAt(tableIndex)
					case wasm.InstrTableGrow:
						emit.privateTableGrow(deltaSlot, tableWide, tableIndex)
						emit.Store(emit.SpillOffset(h), 5, tableWide)
					}
				}
				if imm.Kind == wasm.InstrTableGrow {
					if err = push(tableWide); err != nil {
						return integerSummary{}, err
					}
				}
				continue
			}
			if imm.Kind == wasm.InstrTableSize {
				wide, ok := state.tableWidth(imm.Index)
				if !privateRefs || !ok {
					return integerSummary{}, unsupported("private table.size index")
				}
				if live && emit != nil {
					emit.privateTableDescriptor(enc.X6, imm.Index)
					emit.a.Lwu(enc.X5, enc.X6, 0)
					emit.Store(emit.SpillOffset(h), 5, wide)
				}
				if err = push(wide); err != nil {
					return integerSummary{}, err
				}
				continue
			}
			switch imm.Kind {
			case wasm.InstrMemoryInit, wasm.InstrDataDrop:
				if int(imm.Index) >= len(state.passiveData) || imm.Kind == wasm.InstrMemoryInit && uint64(imm.Index2) >= uint64(state.memoryCount) {
					return integerSummary{}, unsupported("bulk data/memory index")
				}
			case wasm.InstrMemoryCopy, wasm.InstrMemoryFill:
				if uint64(imm.Index) >= uint64(state.memoryCount) || imm.Kind == wasm.InstrMemoryCopy && uint64(imm.Index2) >= uint64(state.memoryCount) {
					return integerSummary{}, unsupported("bulk memory index")
				}
			default:
				return integerSummary{}, unsupported("unsupported prefixed instruction")
			}
			contextUsed = true
			dataOffset := int32((len(state.globals)+1)*8 + int(imm.Index)*16)
			if emit != nil && emit.mode.production {
				dataOffset = int32(imm.Index) * 16
			}
			if imm.Kind == wasm.InstrDataDrop {
				if live && emit != nil {
					emit.dataDrop(dataOffset)
				}
				break
			}
			if state.memory == nil {
				return integerSummary{}, unsupported("bulk instruction without memory")
			}
			dst, src := imm.Index, imm.Index2
			if imm.Kind == wasm.InstrMemoryInit {
				dst, src = imm.Index2, 0
			}
			widths := profile.bulkWidths(imm.Kind, dst, src)
			for i := 2; i >= 0; i-- {
				if err = popWidth(widths[i]); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(uint8(5+i), emit.SpillOffset(h), widths[i])
				}
			}
			if live && emit != nil {
				emit.memoryBulkAt(imm.Kind, dataOffset, dst, src)
			}
		case 0xfb:
			var imm wasm.InstructionImmediate
			if err = profile.classifier.ClassifyInto(&r, op, &imm); err != nil {
				return integerSummary{}, err
			}
			if state == nil || !state.i31Core {
				return integerSummary{}, unsupported("GC instruction outside exact i31 profile")
			}
			switch imm.Kind {
			case wasm.InstrRefI31:
				if _, err = popClass(false); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), false)
					emit.i31Wrap()
					emit.Store(emit.SpillOffset(h), 5, true)
				}
				if err = pushReference(); err != nil {
					return integerSummary{}, err
				}
			case wasm.InstrI31GetS, wasm.InstrI31GetU:
				if _, err = popClass(true); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), true)
					emit.i31Get(imm.Kind == wasm.InstrI31GetS)
					emit.Store(emit.SpillOffset(h), 5, false)
				}
				if err = push(false); err != nil {
					return integerSummary{}, err
				}
			default:
				return integerSummary{}, unsupported("unsupported GC instruction in exact i31 profile")
			}
		case 0x28, 0x29, 0x2a, 0x2b, 0x38, 0x39, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40:
			if !floating && (op == 0x2a || op == 0x2b || op == 0x38 || op == 0x39) {
				return integerSummary{}, unsupported("FP memory outside production")
			}
			if state == nil || state.memory == nil {
				return integerSummary{}, unsupported("memory instruction without memory")
			}
			contextUsed = true
			var imm wasm.InstructionImmediate
			if err = profile.classifier.ClassifyInto(&r, op, &imm); err != nil {
				return integerSummary{}, err
			}
			memoryIndex := imm.MemIndex
			if op == 0x3f || op == 0x40 {
				memoryIndex = imm.Index
			}
			if uint64(memoryIndex) >= uint64(state.memoryCount) || !state.indexedMemories && (memoryIndex != 0 || imm.HasMemIndex) {
				return integerSummary{}, unsupported("nonzero memory index")
			}
			address64 := profile.wide(memoryIndex)
			if op == 0x3f || op == 0x40 {
				if op == 0x40 {
					if err = popWidth(address64); err != nil {
						return integerSummary{}, err
					}
				}
				if live && emit != nil {
					if op == 0x40 {
						emit.Load(5, emit.SpillOffset(h), address64)
					}
					emit.memorySizeGrowTypedAt(memoryIndex, op == 0x40, address64)
					emit.Store(emit.SpillOffset(h), 5, address64)
				}
				if err = push(address64); err != nil {
					return integerSummary{}, err
				}
			} else {
				size, w, signed, store, ok := memoryOpcode(op)
				if !ok {
					return integerSummary{}, unsupported("noninteger memory access")
				}
				if store {
					if err = popWidth(w); err != nil {
						return integerSummary{}, err
					}
					if live && emit != nil {
						emit.Load(6, emit.SpillOffset(h), w)
					}
				}
				if err = popWidth(address64); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), address64)
					emit.memoryAccessTypedAt(memoryIndex, imm.MemOffset, size, w, signed, store, address64)
					if !store {
						emit.Store(emit.SpillOffset(h), 5, w)
					}
				}
				if !store {
					if err = push(w); err != nil {
						return integerSummary{}, err
					}
				}
			}

		case 0x43, 0x44:
			if !floating {
				return integerSummary{}, unsupported("FP constant outside production")
			}
			w := op == 0x44
			var v uint64
			if w {
				v, err = r.LEU64()
			} else {
				var x uint32
				x, err = r.LEU32()
				v = uint64(x)
			}
			if err != nil {
				return integerSummary{}, err
			}
			if live && emit != nil {
				emit.Constant(5, int64(v), w)
				emit.Store(emit.SpillOffset(h), 5, w)
			}
			if err = push(w); err != nil {
				return integerSummary{}, err
			}
		case 0x41, 0x42:
			w := op == 0x42
			var v int64
			if w {
				v, err = r.I64()
			} else {
				var x int32
				x, err = r.I32()
				v = int64(x)
			}
			if err != nil {
				return integerSummary{}, err
			}
			if live && emit != nil {
				emit.Constant(5, v, w)
				emit.Store(emit.SpillOffset(h), 5, w)
			}
			if err = push(w); err != nil {
				return integerSummary{}, err
			}
		default:
			if w, unary, resultWide, ok := floatOpcode(op); ok && floating {
				if !unary {
					if _, err = pop(); err != nil {
						return integerSummary{}, err
					}
					if live && emit != nil {
						emit.Load(6, emit.SpillOffset(h), w)
					}
				}
				if _, err = pop(); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), w)
					emit.floatOperation(op)
					emit.Store(emit.SpillOffset(h), 5, resultWide)
				}
				if err = push(resultWide); err != nil {
					return integerSummary{}, err
				}
				continue
			}
			operation, w, unary, resultWide, ok := integerOpcode(op)
			if !ok {
				return integerSummary{}, unsupported(fmt.Sprintf("integer opcode 0x%x", op))
			}
			if !unary {
				if _, err = pop(); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(6, emit.SpillOffset(h), w)
				}
			}
			if _, err = pop(); err != nil {
				return integerSummary{}, err
			}
			if live && emit != nil {
				emit.Load(5, emit.SpillOffset(h), w)
				emit.operation(operation, w, unary, resultWide)
				emit.Store(emit.SpillOffset(h), 5, resultWide)
			}
			if err = push(resultWide); err != nil {
				return integerSummary{}, err
			}
		}
	}
	if r.HasNext() {
		return integerSummary{}, fmt.Errorf("rv64: trailing validated bytecode")
	}
	return integerSummary{maxStack: peak, context: contextUsed, calls: callCount, tableBridgeBytes: tableBridgeBytes, importTails: importTails}, nil
}

func integerOpcode(op byte) (shared.IntOp, bool, bool, bool, bool) {
	if o, w, ok := shared.ScalarOpcode(op); ok {
		return o, w, false, w && !(o >= shared.IntEq && o <= shared.IntGeU), true
	}
	switch op {
	case 0x45:
		return shared.IntEqz, false, true, false, true
	case 0x50:
		return shared.IntEqz, true, true, false, true
	case 0x67, 0x68, 0x69:
		return []shared.IntOp{shared.IntClz, shared.IntCtz, shared.IntPopcnt}[op-0x67], false, true, false, true
	case 0x79, 0x7a, 0x7b:
		return []shared.IntOp{shared.IntClz, shared.IntCtz, shared.IntPopcnt}[op-0x79], true, true, true, true
	case 0x6d, 0x6e, 0x6f, 0x70:
		return []shared.IntOp{shared.IntDivS, shared.IntDivU, shared.IntRemS, shared.IntRemU}[op-0x6d], false, false, false, true
	case 0x7f, 0x80, 0x81, 0x82:
		return []shared.IntOp{shared.IntDivS, shared.IntDivU, shared.IntRemS, shared.IntRemU}[op-0x7f], true, false, true, true
	case 0x77, 0x78:
		return []shared.IntOp{shared.IntRotl, shared.IntRotr}[op-0x77], false, false, false, true
	case 0x89, 0x8a:
		return []shared.IntOp{shared.IntRotl, shared.IntRotr}[op-0x89], true, false, true, true
	case 0xa7:
		return shared.IntWrap, true, true, false, true
	case 0xac:
		return shared.IntSExt32, false, true, true, true
	case 0xad:
		return shared.IntZExt32, false, true, true, true
	case 0xc0, 0xc1:
		return []shared.IntOp{shared.IntSExt8, shared.IntSExt16}[op-0xc0], false, true, false, true
	case 0xc2, 0xc3, 0xc4:
		return []shared.IntOp{shared.IntSExt8, shared.IntSExt16, shared.IntSExt32}[op-0xc2], true, true, true, true
	}
	return 0, false, false, false, false
}

func (f *integerEmitter) trap(code, site int) {
	f.traps = append(f.traps, integerTrap{site: site, code: code, sourcePC: f.sourcePC})
}
func (f *integerEmitter) operation(op shared.IntOp, w, unary, resultWide bool) {
	if !unary {
		switch op {
		case shared.IntDivS, shared.IntDivU, shared.IntRemS, shared.IntRemU:
			f.trap(TrapDivideByZero, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
			if op == shared.IntDivS {
				min := int64(-1 << 63)
				if !w {
					min = -1 << 31
				}
				f.Constant(7, min, w)
				skip := f.a.BranchLong(enc.NE, enc.X5, enc.X7)
				f.Constant(7, -1, w)
				f.trap(TrapIntegerOverflow, f.a.BranchLong(enc.EQ, enc.X6, enc.X7))
				if e := f.Patch(skip, f.Position()); e != nil {
					f.err = e
				}
			}
			if w {
				switch op {
				case shared.IntDivS:
					f.a.Div(enc.X5, enc.X5, enc.X6)
				case shared.IntDivU:
					f.a.Divu(enc.X5, enc.X5, enc.X6)
				case shared.IntRemS:
					f.a.Rem(enc.X5, enc.X5, enc.X6)
				case shared.IntRemU:
					f.a.Remu(enc.X5, enc.X5, enc.X6)
				}
			} else {
				switch op {
				case shared.IntDivS:
					f.a.Divw(enc.X5, enc.X5, enc.X6)
				case shared.IntDivU:
					f.a.Divuw(enc.X5, enc.X5, enc.X6)
				case shared.IntRemS:
					f.a.Remw(enc.X5, enc.X5, enc.X6)
				case shared.IntRemU:
					f.a.Remuw(enc.X5, enc.X5, enc.X6)
				}
			}
		case shared.IntRotl, shared.IntRotr:
			f.a.Sub(enc.X28, enc.Zero, enc.X6)
			if w {
				if op == shared.IntRotl {
					f.a.Sll(enc.X7, enc.X5, enc.X6)
					f.a.Srl(enc.X28, enc.X5, enc.X28)
				} else {
					f.a.Srl(enc.X7, enc.X5, enc.X6)
					f.a.Sll(enc.X28, enc.X5, enc.X28)
				}
			} else {
				if op == shared.IntRotl {
					f.a.Sllw(enc.X7, enc.X5, enc.X6)
					f.a.Srlw(enc.X28, enc.X5, enc.X28)
				} else {
					f.a.Srlw(enc.X7, enc.X5, enc.X6)
					f.a.Sllw(enc.X28, enc.X5, enc.X28)
				}
			}
			f.a.Or(enc.X5, enc.X7, enc.X28)
		default:
			f.Binary(op, w, 5, 5, shared.ScalarOperand{Kind: shared.ScalarRegister, Reg: 6})
		}
	} else {
		switch op {
		case shared.IntEqz:
			f.a.Sltiu(enc.X5, enc.X5, 1)
		case shared.IntWrap, shared.IntZExt32:
			f.NormalizeI32(5)
		case shared.IntSExt8:
			f.a.Slli(enc.X5, enc.X5, 56)
			f.a.Srai(enc.X5, enc.X5, 56)
		case shared.IntSExt16:
			f.a.Slli(enc.X5, enc.X5, 48)
			f.a.Srai(enc.X5, enc.X5, 48)
		case shared.IntSExt32:
			f.a.Addiw(enc.X5, enc.X5, 0)
		case shared.IntClz, shared.IntCtz, shared.IntPopcnt:
			width := 32
			if w {
				width = 64
			}
			initial := width
			if op == shared.IntPopcnt {
				initial = 0
			}
			f.Constant(7, int64(initial), true)
			done := f.a.BranchLong(enc.EQ, enc.X5, enc.Zero)
			f.Constant(7, 0, true)
			start := f.Position()
			if op == shared.IntPopcnt {
				f.a.Addi(enc.X6, enc.X5, -1)
				f.a.And(enc.X5, enc.X5, enc.X6)
				f.a.Addi(enc.X7, enc.X7, 1)
				again := f.a.BranchLong(enc.NE, enc.X5, enc.Zero)
				if e := f.Patch(again, start); e != nil {
					f.err = e
				}
			} else {
				if op == shared.IntClz {
					f.a.Srli(enc.X6, enc.X5, uint8(width-1))
				} else {
					f.a.Addi(enc.X28, enc.Zero, 1)
					f.a.And(enc.X6, enc.X5, enc.X28)
				}
				stop := f.a.BranchLong(enc.NE, enc.X6, enc.Zero)
				if op == shared.IntClz {
					f.a.Slli(enc.X5, enc.X5, 1)
					if !w {
						f.NormalizeI32(5)
					}
				} else {
					f.a.Srli(enc.X5, enc.X5, 1)
				}
				f.a.Addi(enc.X7, enc.X7, 1)
				back := f.Jump()
				if e := f.Patch(back, start); e != nil {
					f.err = e
				}
				if e := f.Patch(stop, f.Position()); e != nil {
					f.err = e
				}
			}
			if e := f.Patch(done, f.Position()); e != nil {
				f.err = e
			}
			f.Move(5, 7, true)
		default:
			f.err = fmt.Errorf("rv64: unsupported integer unary %d", op)
		}
	}
	if !resultWide {
		f.NormalizeI32(5)
	}
}

func compileIntegerMode(fn *wasm.Func, ft *wasm.CompType, state *moduleState, summary integerSummary, mode emissionMode) ([]byte, FunctionInfo, []directCall, error) {
	functionRefs := state != nil && state.referenceFunctions
	if functionRefs != mode.referenceFunctions || functionRefs && (!mode.production || state.localTailCalls != mode.localTailCalls) {
		return nil, FunctionInfo{}, nil, unsupported("reference function admission/emission mode mismatch")
	}

	if mode.localTailCalls && (!mode.production || !mode.interruptible) {
		return nil, FunctionInfo{}, nil, unsupported("local tails require cooperative production")
	}
	if mode.foreignTableCalls && summary.tableBridgeBytes > MaxCodeBytes-(64<<10) {
		return nil, FunctionInfo{}, nil, unsupported("foreign table bridge admission bound")
	}
	var widths [256]bool
	referenceTypes := internalReferenceTypes{}
	if state != nil {
		referenceTypes.scalar = state.types
		referenceTypes.declarationExtern = state.declarationExternRefs
		referenceTypes.executionExtern = state.core3RefIsNullExecution
		referenceTypes.declarationNonNullExtern = state.declarationNonnullRefs
		referenceTypes.publicReferenceCarriers = state.publicReferenceCarriers
	}
	n := len(ft.Params)
	for i, t := range ft.Params {
		widths[i] = scalarWide(t) || functionRefs && t.Kind() == wasm.ValRef
	}
	for _, run := range fn.Locals.Runs {
		for i := uint32(0); i < run.Count; i++ {
			widths[n] = scalarWide(run.Type) || internalFunctionReferenceWithTypes(run.Type, true, referenceTypes)
			n++
		}
	}
	f := integerEmitter{target: target{nlocals: n, mode: mode}}
	if regallocCheckEnabled {
		defer f.checkEndLifetimes()
	}
	if summary.calls != 0 {
		f.header = callFrameHeader
	}
	frame, err := checkedFrameBytes(f.frameHeader(), n, summary.maxStack)
	if err != nil {
		return nil, FunctionInfo{}, nil, err
	}
	f.frameBytes = frame
	actual, err := walkInteger(fn, ft, state, &f)
	if err != nil {
		return nil, FunctionInfo{}, nil, err
	}
	if actual != summary {
		return nil, FunctionInfo{}, nil, fmt.Errorf("rv64: integer admission changed")
	}
	f.profileUnknown()
	exits := []int{f.Jump()}
	if mode.production {
		for _, tr := range f.traps {
			if err = f.Patch(tr.site, f.Position()); err != nil {
				return nil, FunctionInfo{}, nil, err
			}
			f.profileCold(tr.sourcePC)
			f.emitProductionTrap(productionTrapCode(tr.code), tr.sourcePC)
		}
	} else {
		for code := TrapUnreachable; code <= TrapMemoryBounds; code++ {
			pos := f.Position()
			used := false
			for _, tr := range f.traps {
				if tr.code == code {
					if err = f.Patch(tr.site, pos); err != nil {
						return nil, FunctionInfo{}, nil, err
					}
					used = true
				}
			}
			if used {
				f.Constant(5, int64(code), false)
				f.a.Sw(enc.X5, enc.X12, 0)
				f.Constant(10, 0, true)
				exits = append(exits, f.Jump())
			}
		}
	}
	f.profileUnknown()
	if len(f.propagate) != 0 {
		for _, site := range f.propagate {
			if err = f.Patch(site, f.Position()); err != nil {
				return nil, FunctionInfo{}, nil, err
			}
		}
		// Preserve the callee's trap code and serialized cell; just unwind.
		f.Constant(10, 0, true)
	}
	for _, site := range exits {
		if err = f.Patch(site, f.Position()); err != nil {
			return nil, FunctionInfo{}, nil, err
		}
	}
	if f.err != nil {
		return nil, FunctionInfo{}, nil, f.err
	}
	if f.a.Err() != nil {
		return nil, FunctionInfo{}, nil, f.a.Err()
	}
	if err := f.finishProfileBody(); err != nil {
		return nil, FunctionInfo{}, nil, err
	}
	info := FunctionInfo{Params: len(ft.Params), Results: len(ft.Results), SpillSlots: summary.maxStack, Result64: len(ft.Results) == 1 && (scalarWide(ft.Results[0]) || functionRefs && ft.Results[0].Kind() == wasm.ValRef), Fallback: true, FrameHeaderBytes: f.frameHeader(), DirectCalls: len(f.calls), mode: mode}
	contextOffset := -1
	if summary.context {
		contextOffset = -8
	}
	code, info, err := frameFunctionCalls(f.a.B, widths[:n], len(ft.Params), summary.maxStack, true, contextOffset, info, f.calls)
	if err == nil && info.FrameBytes != f.frameBytes {
		return nil, FunctionInfo{}, nil, fmt.Errorf("rv64: tail frame layout changed")
	}
	if err == nil && mode.foreignTableCalls && len(code) > MaxCodeBytes {
		return nil, FunctionInfo{}, nil, unsupported("foreign table function image bound")
	}
	return code, info, f.calls, err
}
