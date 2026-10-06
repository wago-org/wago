//go:build amd64

package amd64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Bounded immutable prefixes avoid control-edge frame traffic. Zero disables
// the policy for comparisons.
var ifPrefixRematEnabled = os.Getenv("WAGO_AMD64_IF_PREFIX_REMAT") != "0"

// ifPrefixLocal admits one non-trapping local read, optionally plus an i32
// constant. Every leaf is reconstructible; no owned register, frame operand,
// guest load, or observable operation may be detached from the operand stack.
func ifPrefixLocal(root *elem) *elem {
	if root == nil || rootMachineType(root) != mtI32 || root.st.hasGCRoot() {
		return nil
	}
	value := root
	if root.isDeferred() {
		if root.deferredOp() != opAdd || root.arg0 == nil || root.arg1 == nil {
			return nil
		}
		value, constant := root.arg0, root.arg1
		if value.isValue() && value.st.kind == stConst {
			value, constant = constant, value
		}
		if !constant.isValue() || constant.st.kind != stConst || constant.valueType() != mtI32 {
			return nil
		}
		if value.isValue() && value.valueType() == mtI32 && !value.st.hasGCRoot() &&
			(value.st.kind == stLocalRef || value.st.kind == stLocalReg) {
			return value
		}
		return nil
	}
	if value.isValue() && (value.st.kind == stLocalRef || value.st.kind == stLocalReg) {
		return value
	}
	return nil
}

// The read-only proof is bounded independently of function size. Nested control,
// branches, calls, GC, atomics and unknown instructions all retain normal stack
// convergence. Trapping numeric/memory instructions stay in their original arms.
func (f *fn) ifPrefixUnchanged(reader *wasm.Reader, local int) bool {
	r := *reader
	start := r.Offset()
	seenElse := false
	var imm wasm.InstructionImmediate
	for ops := 0; ops < 48 && r.Offset()-start <= 256; ops++ {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if op == 0x0b {
			return seenElse
		}
		if op == 0x05 {
			if seenElse {
				return false
			}
			seenElse = true
			continue
		}
		allowed := op == 0x01 || op == 0x1a || op == 0x1b ||
			op >= 0x20 && op <= 0x22 || op >= 0x28 && op <= 0x3e ||
			op == 0x41 || op == 0x42 || op >= 0x45 && op <= 0x8a
		if !allowed || f.classifier.ClassifyInto(&r, op, &imm) != nil {
			return false
		}
		if (op == 0x21 || op == 0x22) && int(imm.Index)+f.localBase == local {
			return false
		}
	}
	return false
}

func (f *fn) deferIfPrefix(reader *wasm.Reader, fr *ctrlFrame) {
	if !ifPrefixRematEnabled || fr.paramN != 0 || fr.resultN != 1 || fr.res0 != mtI32 ||
		f.depth() != 2 || f.moduleEH || f.gcFrameRoots != nil || f.interruptible || len(f.customInstructions) != 0 {
		return
	}
	root := baseOfValentBlock(f.s.back()).prev
	if root == f.s.head {
		return
	}
	local := ifPrefixLocal(root)
	if local == nil || !f.ifPrefixUnchanged(reader, local.st.index()) {
		return
	}
	// Removing the sole prefix leaves a zero-height frame: its cold type-prefix
	// pointer is unused and can retain this existing arena-owned recipe. Its
	// original profiling identity and child links remain intact. AMD64 stack
	// nodes are not recycled during the function.
	if root.isDeferred() {
		f.consumeBlockBelow(root)
	}
	f.erase(root)
	f.ensureCtrlMerge(fr).baseTypeTop = root
	fr.set(ctrlIfDeferredPrefix, true)
	f.stats.peep("if-prefix-remat")
}

func (f *fn) restoreIfPrefix(fr *ctrlFrame) {
	if !fr.has(ctrlIfDeferredPrefix) {
		return
	}
	if fr.height != 0 || fr.has(ctrlColdBaseTypes) || f.depth() != 1 {
		panic("amd64: invalid deferred if prefix contract")
	}
	root := f.ctrlMerge(fr).baseTypeTop
	local := ifPrefixLocal(root)
	if local == nil {
		panic("amd64: lost deferred if prefix recipe")
	}
	x := local.st.index()
	// Local residency may have changed while emitting the arms. Rebind the
	// unchanged Wasm local through its current home, never its old borrowed reg.
	st := storage{kind: stLocalRef, typ: mtI32, idx: uint32(x)}
	if f.localConstZero(x) {
		st = zeroStorage(mtI32)
	} else if pr, _, ok := f.pinReg(x); ok {
		f.recoverLocal(x)
		st = storage{kind: stLocalReg, typ: mtI32, reg: pr, idx: uint32(x)}
	}
	f.replaceStorage(local, st)
	// The local was detached during replacement; restore its summary as well.
	f.s.recordStorageEffects(st)
	result := f.s.back()
	first := root
	if root.isDeferred() {
		first = root.arg0
		f.s.push(root.arg0)
		f.s.push(root.arg1)
	}
	f.s.push(root)
	f.s.exposeLogicalRoot(root)
	// The new prefix precedes any previously cached slot-only prefix.
	f.s.spilledPrefix = nil
	// Rotate the reconstructed prefix below the existing result without moving
	// the result's storage or changing its allocator ownership.
	head := f.s.head
	head.next, first.prev = first, head
	root.next, result.prev = result, root
	result.next, head.prev = head, result
}
