//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type swarControl struct {
	base                           int
	kind                           byte
	results                        []swarClass
	params                         []swarClass
	entry, then, incoming, hasElse bool
	unreachable                    bool
	resultBound                    bool
	falseSite                      int
	start                          int
	sites                          []int
}

// Indexed signatures and loop parameters use the same exact class discipline
// as inline blocks and root returns. Dead operands are virtual and preserve
// each control frame's typing floor. Every live branch transports exact classes through real
// padded stack homes; no synthetic graph identities stand in for those copies.
type swarFlow struct {
	plugins           *pluginInstructionDirectory
	frameStorage      [1 + maxControlNesting]swarControl
	frames            []swarControl
	depth             int
	live              bool
	h, peak, budget   *int
	classes           *[maxSWAROperands]swarClass
	spill             func(int) int32
	recordHome        func(int, swarClass)
	helperBase        func(int) int
	hiddenRefs        func(int) []bool
	reserve           func(int) error
	emit              *integerEmitter
	types             []*wasm.CompType
	referenceTypes    internalReferenceTypes
	exceptionModule   *wasm.Module
	ownedExceptions   bool
	exceptionControls map[int]*exceptionControl
	nextExceptionRoot int
	usesLoops         bool
	privateRefs       bool
	typedRefs         bool
}

func (flow *swarFlow) floor() int { return flow.frames[flow.depth-1].base }

// Prepare operands in a nonexecuted region without weakening checks on known
// types. Only an explicitly stack-polymorphic control frame may synthesize
// missing operands; a nested block starts with ordinary typing even when its
// machine path is dead. Synthetic classes occupy virtual slots only.
func (flow *swarFlow) prepareDead(values ...swarClass) error {
	if flow.live {
		return nil
	}
	base := flow.floor()
	missing := max(0, len(values)-(*flow.h-base))
	if missing != 0 && !flow.frames[flow.depth-1].unreachable {
		return unsupported("SWAR dead control operand underflow")
	}
	if *flow.h+missing > len(flow.classes) {
		return unsupported("SWAR dead operand bound")
	}
	for i, wanted := range values {
		source := *flow.h - len(values) + i
		if source >= base {
			actual := flow.classes[source]
			if actual != swarUnknown && wanted != swarUnknown && actual != wanted {
				return unsupported("SWAR dead operand carrier class")
			}
		}
	}
	copy(flow.classes[base+missing:*flow.h+missing], flow.classes[base:*flow.h])
	for i := 0; i < missing; i++ {
		flow.classes[base+i] = values[i]
	}
	*flow.h += missing
	for i, wanted := range values {
		at := *flow.h - len(values) + i
		if flow.classes[at] == swarUnknown && wanted != swarUnknown {
			flow.classes[at] = wanted
		}
	}
	return nil
}

func (flow *swarFlow) prepareRepeatedDead(class swarClass, count int) error {
	var values [3]swarClass
	if count < 0 || count > len(values) {
		return unsupported("SWAR scalar/vector arity")
	}
	for i := 0; i < count; i++ {
		values[i] = class
	}
	return flow.prepareDead(values[:count]...)
}

func (flow *swarFlow) position() int {
	if flow.emit != nil {
		return flow.emit.Position()
	}
	return 0
}

func (flow *swarFlow) patch(site, position int) error {
	if flow.emit != nil {
		return flow.emit.Patch(site, position)
	}
	return nil
}

// Unconditional edges consume the payload. Check it without refining unknowns,
// so a dead br_table can validate the same original values against each label.
func (flow *swarFlow) checkBranch(values []swarClass) error {
	floor := flow.floor()
	missing := max(0, len(values)-(*flow.h-floor))
	if missing != 0 && !flow.frames[flow.depth-1].unreachable || *flow.h+missing > len(flow.classes) {
		return unsupported("SWAR branch operand bound/underflow")
	}
	for i, wanted := range values {
		at := *flow.h - len(values) + i
		if at >= floor && flow.classes[at] != swarUnknown && flow.classes[at] != wanted {
			return unsupported("SWAR branch carrier class")
		}
	}
	return nil
}

func (flow *swarFlow) match(base int, results []swarClass, exact bool) error {
	if err := flow.prepareDead(results...); err != nil {
		return err
	}
	if *flow.h-len(results) < base || exact && *flow.h != base+len(results) {
		return unsupported("SWAR control result stack")
	}
	for i, class := range results {
		if flow.classes[*flow.h-len(results)+i] != class {
			return unsupported("SWAR control result carrier class")
		}
	}
	return nil
}

func (flow *swarFlow) condition() error {
	if err := flow.prepareDead(swarI32); err != nil {
		return err
	}
	if *flow.h == flow.floor() || flow.classes[*flow.h-1] != swarI32 {
		return unsupported("SWAR control condition class/underflow")
	}
	*flow.h--
	if flow.live && flow.emit != nil {
		flow.emit.Load(5, flow.spill(*flow.h), false)
	}
	return nil
}

func (flow *swarFlow) branch(label uint32, retainPayload bool) error {
	if uint64(label) >= uint64(flow.depth) {
		return unsupported("SWAR branch label/dead shape")
	}
	fr := &flow.frames[flow.depth-1-int(label)]
	values := fr.results
	if fr.kind != 0x03 && fr.kind != 0xff && flow.live {
		if err := flow.bindValues(values, fr.resultBound); err != nil {
			return err
		}
		fr.resultBound = true
	}
	if fr.kind == 0x03 {
		values = fr.params
	}
	if retainPayload {
		if err := flow.match(flow.floor(), values, false); err != nil {
			return err
		}
	} else if err := flow.checkBranch(values); err != nil {
		return err
	}
	*flow.budget += len(values)
	if *flow.budget > integerBudget {
		return unsupported("SWAR branch transfer budget")
	}
	if err := flow.reserve(128*len(values) + 64); err != nil {
		return err
	}
	if flow.live && flow.recordHome != nil {
		for i, class := range values {
			flow.recordHome(fr.base+i, class)
		}
	}
	if flow.live && flow.emit != nil {
		// Destinations are never above sources. Forward pair copies therefore
		// preserve overlapping mixed results. X5 retains br_table's raw index.
		pair := swarPair{enc.X7, enc.X9}
		for i, class := range values {
			if custom, ok := flow.plugins.customType(class); ok {
				for cell := int32(0); cell < custom.Size(); cell += 8 {
					flow.emit.Load(7, flow.spill(*flow.h-len(values)+i)+cell, true)
					flow.emit.Store(flow.spill(fr.base+i)+cell, 7, true)
				}
				continue
			}
			flow.emit.swarLoad(flow.spill(*flow.h-len(values)+i), pair)
			if class.narrow() {
				flow.emit.NormalizeI32(7)
			}
			if class != swarV128 {
				flow.emit.a.Addi(pair.high, enc.Zero, 0)
			}
			flow.emit.swarStore(flow.spill(fr.base+i), pair)
		}
		flow.emit.restoreExceptionHandlers(flow.depth - 1 - int(label))
		site := flow.emit.Jump()
		if fr.kind == 0x03 {
			if err := flow.patch(site, fr.start); err != nil {
				return err
			}
		} else {
			fr.sites = append(fr.sites, site)
		}
	}
	if flow.live && fr.kind != 0x03 {
		fr.incoming = true
	}
	return nil
}

// Reference branches use the ordinary exact label transfer, but their condition
// is an opaque 64-bit carrier and their taken/fallthrough operand arities differ.
func (flow *swarFlow) referenceBranch(label uint32, nonNull bool) error {
	if uint64(label) >= uint64(flow.depth) {
		return unsupported("SWAR reference branch label")
	}
	fr := &flow.frames[flow.depth-1-int(label)]
	values := fr.results
	if fr.kind == 0x03 {
		values = fr.params
	}
	if nonNull && (len(values) == 0 || values[len(values)-1] != swarFuncRef) {
		return unsupported("SWAR br_on_non_null requires a reference label result")
	}
	if err := flow.prepareDead(swarFuncRef); err != nil {
		return err
	}
	if *flow.h == flow.floor() || flow.classes[*flow.h-1] != swarFuncRef {
		return unsupported("SWAR reference branch operand class/underflow")
	}
	*flow.h--
	skip := -1
	if flow.live && flow.emit != nil {
		flow.emit.Load(5, flow.spill(*flow.h), true)
		condition := enc.NE
		if nonNull {
			condition = enc.EQ
		}
		skip = flow.emit.a.BranchLong(condition, enc.X5, enc.Zero)
	}
	if nonNull {
		*flow.h++ // the taken edge includes the reference in its label payload
	}
	if err := flow.branch(label, true); err != nil {
		return err
	}
	if flow.live && flow.emit != nil {
		if err := flow.patch(skip, flow.position()); err != nil {
			return err
		}
	}
	if nonNull {
		*flow.h--
	} else {
		if *flow.h >= len(flow.classes) {
			return unsupported("SWAR reference branch operand bound")
		}
		flow.classes[*flow.h] = swarFuncRef
		*flow.h++
		if flow.live {
			*flow.peak = max(*flow.peak, *flow.h)
		}
	}
	return nil
}

func (flow *swarFlow) instruction(op byte, r *wasm.Reader) (done, trap bool, err error) {
	switch op {
	case 0x00:
		trap = true
		if flow.live && flow.emit != nil {
			flow.emit.trap(TrapUnreachable, flow.emit.Jump())
		}
		flow.live = false
		flow.frames[flow.depth-1].unreachable = true
		*flow.h = flow.floor()
	case 0x02, 0x03, 0x04, 0x1f:
		bt, e := internalReferenceBlockType(r, flow.privateRefs && flow.typedRefs, flow.referenceTypes)
		if e != nil {
			return false, false, e
		}
		if flow.depth == len(flow.frames) {
			if len(flow.frames) >= integerBudget+1 {
				return false, false, unsupported("SWAR control nesting/dead shape")
			}
			n := min(len(flow.frames)*2, integerBudget+1)
			grown := make([]swarControl, n)
			copy(grown, flow.frames)
			flow.frames = grown
		}
		var params, results []swarClass
		if bt != -64 {
			if bt >= 0 {
				if bt >= int64(len(flow.types)) || flow.types[bt] == nil {
					return false, false, unsupported("SWAR indexed control signature")
				}
				params, results, e = swarControlSignature(flow.types[bt], flow.referenceTypes, flow.privateRefs, flow.privateRefs && flow.typedRefs, flow.budget, flow.plugins)
				if e != nil {
					return false, false, e
				}
			} else {
				if (bt < -5 || bt > -1) && !(flow.privateRefs && (bt == -16 || flow.plugins.hasCustom() && bt == -17)) {
					return false, false, unsupported("SWAR control carrier signature")
				}
				class := swarByteClass(byte(bt & 0x7f))
				if bt == -16 || bt == -17 {
					class = swarFuncRef
				}
				results = []swarClass{class}
			}
		}
		if bt < 0 {
			*flow.budget += len(params) + len(results)
		}
		if *flow.budget > integerBudget {
			return false, false, unsupported("SWAR control signature budget")
		}
		if err := flow.reserve(128*(len(params)+len(results)) + 512); err != nil {
			return false, false, err
		}
		fr := swarControl{kind: op, params: params, results: results, entry: flow.live, falseSite: -1}
		if op == 0x04 {
			if err := flow.condition(); err != nil {
				return false, false, err
			}
			if flow.live && flow.emit != nil {
				fr.falseSite = flow.emit.BranchZero(5)
			}
		}
		if err := flow.bindValues(params, false); err != nil {
			return false, false, err
		}
		if err := flow.match(flow.floor(), params, false); err != nil {
			return false, false, err
		}
		fr.base = *flow.h - len(params)
		if op == 0x03 {
			flow.usesLoops, trap = true, true
			fr.start = flow.position()
			if flow.live && flow.emit != nil {
				flow.emit.emitProductionInterruptPoll()
			}
		}
		if op == 0x1f {
			if flow.exceptionModule == nil {
				return false, false, unsupported("SWAR try_table source proof")
			}
			control, err := parseExceptionCatches(r, flow.exceptionModule, flow.depth, &flow.nextExceptionRoot)
			if err != nil {
				return false, false, err
			}
			for _, clause := range control.clauses {
				target := &flow.frames[clause.target]
				values := target.results
				if target.kind == 0x03 {
					values = target.params
				}
				arity := len(clause.payload)
				if clause.root >= 0 {
					arity++
				}
				if arity != len(values) {
					return false, false, unsupported("SWAR exception payload arity")
				}
				for i, typ := range clause.payload {
					if swarControlClass(typ, flow.referenceTypes, true, flow.typedRefs) != values[i] {
						return false, false, unsupported("SWAR exception payload class")
					}
				}
				if clause.root >= 0 && values[arity-1] != swarFuncRef {
					return false, false, unsupported("SWAR catch_ref class")
				}
				if flow.live && target.kind != 0x03 {
					target.incoming = true
				}
			}
			if flow.exceptionControls == nil {
				flow.exceptionControls = make(map[int]*exceptionControl)
			}
			if flow.live && flow.emit != nil && flow.emit.mode.ownedExceptionReferences {
				refs := flow.hiddenRefs(*flow.h)
				for i := range control.clauses {
					control.clauses[i].hiddenRefs = refs
					control.clauses[i].collectorPC = flow.emit.sourcePC
				}
			}
			flow.exceptionControls[flow.depth] = control
			if flow.live && flow.emit != nil {
				flow.emit.beginExceptionHandler(control, flow.depth)
			}
		}
		flow.frames[flow.depth] = fr
		flow.depth++
	case 0x05:
		fr := &flow.frames[flow.depth-1]
		if fr.kind != 0x04 || fr.hasElse {
			return false, false, unsupported("SWAR else shape")
		}
		if flow.live && fr.kind != 0xff {
			if err := flow.bindValues(fr.results, fr.resultBound); err != nil {
				return false, false, err
			}
			fr.resultBound = true
		}
		if err := flow.match(fr.base, fr.results, true); err != nil {
			return false, false, err
		}
		if flow.live {
			if flow.emit != nil {
				fr.sites = append(fr.sites, flow.emit.Jump())
			}
		}
		if fr.entry {
			if err := flow.patch(fr.falseSite, flow.position()); err != nil {
				return false, false, err
			}
		}
		fr.then, fr.hasElse = flow.live, true
		fr.unreachable = false
		flow.live, *flow.h = fr.entry, fr.base
		for _, class := range fr.params {
			flow.classes[*flow.h] = class
			*flow.h++
		}
	case 0x0b:
		fr := &flow.frames[flow.depth-1]
		if fr.kind == 0x04 && !fr.hasElse {
			if len(fr.params) != len(fr.results) {
				return false, false, unsupported("SWAR result if requires else")
			}
			for i, class := range fr.params {
				if class != fr.results[i] {
					return false, false, unsupported("SWAR result if requires else")
				}
			}
		}
		if flow.live && fr.kind != 0xff {
			if err := flow.bindValues(fr.results, fr.resultBound); err != nil {
				return false, false, err
			}
			fr.resultBound = true
		}
		if err := flow.match(fr.base, fr.results, true); err != nil {
			return false, false, err
		}
		if fr.kind == 0x1f {
			control := flow.exceptionControls[flow.depth-1]
			if flow.emit != nil && control.active {
				skip := -1
				if flow.live || fr.incoming {
					flow.emit.restoreExceptionHandlers(flow.depth - 1)
					skip = flow.emit.Jump()
				}
				flow.emit.exception.active = flow.emit.exception.active[:len(flow.emit.exception.active)-1]
				if err := flow.emit.emitExceptionHandler(control, func(clause *exceptionCatch) error {
					target := &flow.frames[clause.target]
					flow.emit.copyExceptionPayloadAt(clause, target.base, flow.spill)
					flow.emit.restoreExceptionHandlers(clause.target)
					site := flow.emit.Jump()
					if target.kind == 0x03 {
						return flow.patch(site, target.start)
					}
					target.sites = append(target.sites, site)
					return nil
				}); err != nil {
					return false, false, err
				}
				if skip >= 0 {
					if err := flow.patch(skip, flow.position()); err != nil {
						return false, false, err
					}
				}
			} else if flow.emit != nil {
				flow.emit.emitInactiveExceptionCopies(control)
			}
			delete(flow.exceptionControls, flow.depth-1)
		}
		for _, site := range fr.sites {
			if err := flow.patch(site, flow.position()); err != nil {
				return false, false, err
			}
		}
		if fr.kind == 0x04 && !fr.hasElse && fr.entry {
			if err := flow.patch(fr.falseSite, flow.position()); err != nil {
				return false, false, err
			}
		}
		flow.live = flow.live || fr.incoming || fr.then || fr.kind == 0x04 && !fr.hasElse && fr.entry
		*flow.h = fr.base
		for _, class := range fr.results {
			if *flow.h >= len(flow.classes) {
				return false, false, unsupported("SWAR control operand bound")
			}
			flow.classes[*flow.h] = class
			*flow.h++
		}
		if flow.live {
			*flow.peak = max(*flow.peak, *flow.h)
		}
		flow.depth--
		done = flow.depth == 0
	case 0x08, 0x0a:
		if flow.exceptionModule == nil {
			return false, false, unsupported("SWAR throw source proof")
		}
		if op == 0x08 {
			tag, err := r.U32()
			if err != nil {
				return false, false, err
			}
			sig, err := exceptionTagSignature(flow.exceptionModule, tag)
			if err != nil {
				return false, false, err
			}
			var classes [shared.MaxEHTagPayloadWords]swarClass
			for i, typ := range sig.Params {
				classes[i] = swarControlClass(typ, flow.referenceTypes, true, flow.typedRefs)
			}
			if err := flow.match(flow.floor(), classes[:len(sig.Params)], false); err != nil {
				return false, false, err
			}
			*flow.h -= len(sig.Params)
			if flow.live && flow.emit != nil {
				flow.emit.throwExceptionAt(tag, sig.Params, *flow.h, flow.spill)
			}
		} else {
			if err := flow.prepareDead(swarFuncRef); err != nil {
				return false, false, err
			}
			if *flow.h == flow.floor() || flow.classes[*flow.h-1] != swarFuncRef {
				return false, false, unsupported("SWAR throw_ref class")
			}
			*flow.h--
			if flow.live && flow.emit != nil {
				flow.emit.throwExceptionReference(flow.helperBase(*flow.h))
			}
		}
		trap = true
		flow.live = false
		flow.frames[flow.depth-1].unreachable = true
		*flow.h = flow.floor()
	case 0x0c, 0x0d, 0x0f:
		label := uint32(flow.depth - 1)
		if op != 0x0f {
			label, err = r.U32()
			if err != nil {
				return false, false, err
			}
		}
		skip := -1
		if op == 0x0d {
			if err := flow.condition(); err != nil {
				return false, false, err
			}
			if flow.live && flow.emit != nil {
				skip = flow.emit.BranchZero(5)
			}
		}
		if err := flow.branch(label, op == 0x0d); err != nil {
			return false, false, err
		}
		if op == 0x0d && flow.live {
			if err := flow.patch(skip, flow.position()); err != nil {
				return false, false, err
			}
		} else if op != 0x0d {
			flow.live = false
			flow.frames[flow.depth-1].unreachable = true
			*flow.h = flow.floor()
		}
	case 0x0e:
		n, e := r.U32()
		if e != nil {
			return false, false, e
		}
		if n > maxTableEntries || int(n) > integerBudget-*flow.budget {
			return false, false, unsupported("SWAR branch table bound")
		}
		*flow.budget += int(n)
		if err := flow.condition(); err != nil {
			return false, false, err
		}
		var signature []swarClass
		for i := uint32(0); i <= n; i++ {
			label, e := r.U32()
			if e != nil {
				return false, false, e
			}
			if uint64(label) >= uint64(flow.depth) {
				return false, false, unsupported("SWAR branch table label")
			}
			results := flow.frames[flow.depth-1-int(label)].results
			if flow.frames[flow.depth-1-int(label)].kind == 0x03 {
				results = flow.frames[flow.depth-1-int(label)].params
			}
			if i == 0 {
				signature = results
			} else {
				if len(results) != len(signature) {
					return false, false, unsupported("SWAR branch table arity")
				}
			}
			skip := -1
			if i < n && flow.live && flow.emit != nil {
				flow.emit.Constant(6, int64(i), false)
				skip = flow.emit.a.BranchLong(enc.NE, enc.X5, enc.X6)
			}
			if err := flow.branch(label, false); err != nil {
				return false, false, err
			}
			if i < n && flow.live {
				if err := flow.patch(skip, flow.position()); err != nil {
					return false, false, err
				}
			}
		}
		flow.live = false
		flow.frames[flow.depth-1].unreachable = true
		*flow.h = flow.floor()
	}
	return done, trap, nil
}

// Bind a block's erased signature to explicitly produced custom operands. Once
// a live incoming edge fixes a result, all other edges must agree exactly.
func (flow *swarFlow) bindValues(values []swarClass, bound bool) error {
	if !flow.plugins.hasCustom() || !flow.live || len(values) > *flow.h-flow.floor() {
		return nil
	}
	for i, wanted := range values {
		actual := flow.classes[*flow.h-len(values)+i]
		if actual == wanted || actual == swarUnknown {
			continue
		}
		if !bound && actual >= swarCustomBase && flow.plugins.physicalClass(actual) == wanted {
			values[i] = actual
			continue
		}
		if actual >= swarCustomBase || wanted >= swarCustomBase {
			return unsupported("incompatible custom type at control join")
		}
	}
	return nil
}
