//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

const (
	MaxOwnedTableEntries    = 1024
	MaxOwnedElementSegments = 256
	trapIndirectBounds      = 8 // distinct from private memory-bounds tag5
	trapIndirectSignature   = 9
	trapTableBounds         = 10
)

// This public profile accepts a single byte-backed abstract funcref literal.
// The shared parser additionally accepts nofunc/global/structured expressions;
// keep those outside the qualified RV64 profile, including unused segments.
func tableElementExpression(expr wasm.Expr) (wasm.FuncrefElementExpr, error) {
	if len(expr.Instrs) != 0 || len(expr.BodyBytes) < 3 || len(expr.BodyBytes) > 7 {
		return wasm.FuncrefElementExpr{}, unsupported("element expression representation/byte bound")
	}
	switch expr.BodyBytes[0] {
	case 0xd0:
		r := wasm.ReaderFrom(expr.BodyBytes[1:])
		heap, err := r.S33()
		if err != nil || heap != -16 {
			return wasm.FuncrefElementExpr{}, unsupported("element null requires abstract func")
		}
	case 0xd2:
	default:
		return wasm.FuncrefElementExpr{}, unsupported("element requires ref.null or ref.func")
	}
	value, err := wasm.ParseFuncrefElementExpr(expr)
	if err != nil {
		return wasm.FuncrefElementExpr{}, unsupported("element reference literal")
	}
	return value, nil
}

// Element payloads may copy immutable construction-time globals under the
// retained public ABI. Declarative payloads are validated before being dropped;
// private owners and table initializers cannot resolve shared global cells.
func elementInitializerExpression(expr wasm.Expr, m *wasm.Module, mode wasm.ElemModeKind, public bool) (wasm.ElementExpr, error) {
	if len(expr.BodyBytes) != 0 && expr.BodyBytes[0] == 0x23 {
		if !public || mode != wasm.ElemActive && mode != wasm.ElemPassive && mode != wasm.ElemDeclarative {
			return wasm.ElementExpr{}, unsupported("element global requires a public segment")
		}
		if err := admitGlobalExpression(expr, m, wasm.FuncRef, m.ImportedGlobalCount()+len(m.Globals)); err != nil {
			return wasm.ElementExpr{}, err
		}
		value, err := wasm.ParseElementExpr(expr)
		if err != nil {
			return wasm.ElementExpr{}, unsupported("element global expression")
		}
		return value, nil
	}
	value, err := tableElementExpression(expr)
	return wasm.ElementExpr{RefType: wasm.FuncRef.Ref(), Null: value.Null, FuncIndex: value.FuncIndex}, err
}

// Table initialization shares the literal element profile and canonical public
// descriptors. An imported immutable funcref is materialized by the shared
// frontend as a synthetic active segment. Local globals remain outside this
// profile: Core table initializers may only read imported globals. Validate the
// source even when the initial table size is zero.
func admitTableInitializer(expr wasm.Expr, m *wasm.Module) (global bool, err error) {
	if len(expr.BodyBytes) != 0 && expr.BodyBytes[0] == 0x23 {
		if err := admitGlobalExpression(expr, m, wasm.FuncRef, m.ImportedGlobalCount()); err != nil {
			return false, err
		}
		return true, nil
	}
	value, err := tableElementExpression(expr)
	if err != nil {
		return false, err
	}
	if !value.Null && uint64(value.FuncIndex) >= uint64(m.ImportedFuncCount()+len(m.Code)) {
		return false, unsupported("table initializer function index")
	}
	return false, nil
}

// Bound decoded resource metadata before validation or payload copies.
func ownedTableBounds(m *wasm.Module, public bool) error {
	if len(m.Imports) > MaxProductionImports {
		return unsupported("table import count bound")
	}
	if len(m.Tables)+m.ImportedTableCount() > 1 || len(m.Elements) > MaxOwnedElementSegments {
		return unsupported("private table/element count")
	}
	for index := 0; index < len(m.Tables)+m.ImportedTableCount(); index++ {
		t, _ := m.TableType(uint32(index))
		if t.Limits.Min > MaxOwnedTableEntries || (t.Limits.HasMax && t.Limits.Max > MaxOwnedTableEntries) {
			return unsupported("private table capacity")
		}
	}
	remaining := MaxOwnedTableEntries
	if public && len(m.Tables) == 1 && m.Tables[0].Init != nil {
		global, err := admitTableInitializer(*m.Tables[0].Init, m)
		if err != nil {
			return err
		}
		if global {
			// The shared frontend expands a global initializer to one active
			// segment containing the initial table length. Account for that
			// synthetic retained metadata before ordinary segments.
			if len(m.Elements) >= MaxOwnedElementSegments || m.Tables[0].Type.Limits.Min > uint64(remaining) {
				return unsupported("table global initializer aggregate bound")
			}
			remaining -= int(m.Tables[0].Type.Limits.Min)
		}
	}
	for _, e := range m.Elements {
		if len(e.Kind.Funcs) > remaining || len(e.Kind.Exprs) > remaining {
			return unsupported("element payload bound")
		}
		remaining -= len(e.Kind.Funcs) + len(e.Kind.Exprs)
		if remaining < 0 {
			return unsupported("element payload bound")
		}
		for _, expr := range e.Kind.Exprs {
			if _, err := elementInitializerExpression(expr, m, e.Mode.Kind, public); err != nil {
				return err
			}
		}
	}
	return nil
}

func admitOwnedTable(m *wasm.Module, production bool) (bool, error) {
	return admitTableProfile(m, production, false)
}

// Imported storage uses the retained canonical target ABI, never the private
// same-instance pilot. Count and capacity limits precede validation/copies.
func admitTableProfile(m *wasm.Module, production, importedTables bool) (bool, error) {
	if err := ownedTableBounds(m, production && importedTables); err != nil {
		return false, err
	}
	if m.ImportedTableCount() != 0 && (!production || !importedTables) {
		return false, unsupported("imported table requires the retained public table ABI")
	}
	hasTable := len(m.Tables)+m.ImportedTableCount() != 0
	if !hasTable && len(m.Elements) == 0 {
		return false, nil
	}
	if !production || !hasTable && !importedTables {
		return false, unsupported("isolated private table")
	}
	if hasTable {
		t, _ := m.TableType(0)
		if t.Limits.Addr64 || !wasm.EqualValType(wasm.RefVal(t.Ref), wasm.FuncRef) {
			return false, unsupported("private nullable funcref table32 only")
		}
	}
	if len(m.Tables) != 0 && m.Tables[0].Init != nil {
		if !importedTables {
			return false, unsupported("table initializer requires retained public table ABI")
		}
		if _, err := admitTableInitializer(*m.Tables[0].Init, m); err != nil {
			return false, err
		}
	}
	imported := m.ImportedFuncCount()
	for _, e := range m.Elements {
		if !hasTable && e.Mode.Kind != wasm.ElemDeclarative && e.Mode.Kind != wasm.ElemPassive {
			return false, unsupported("table-free elements must be passive or declarative")
		}
		if (e.Mode.Kind != wasm.ElemActive && e.Mode.Kind != wasm.ElemPassive && e.Mode.Kind != wasm.ElemDeclarative) || e.Mode.Table != 0 {
			return false, unsupported("element mode/table")
		}
		switch e.Kind.Kind {
		case wasm.ElemFuncs:
			if len(e.Kind.Exprs) != 0 {
				return false, unsupported("legacy element expression payload")
			}
		case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
			// Flag4 has an implicit funcref type; its decoder leaves Ref unset.
			if !importedTables || len(e.Kind.Funcs) != 0 || e.Kind.Kind == wasm.ElemTypedExprs && !wasm.EqualValType(wasm.RefVal(e.Kind.Ref), wasm.FuncRef) {
				return false, unsupported("public nullable funcref element expressions required")
			}
			for _, expr := range e.Kind.Exprs {
				value, err := elementInitializerExpression(expr, m, e.Mode.Kind, production && importedTables)
				if err != nil {
					return false, err
				}
				if !value.Null && !value.HasGlobal && uint64(value.FuncIndex) >= uint64(imported+len(m.Code)) {
					return false, unsupported("element expression function index")
				}
			}
		default:
			return false, unsupported("element payload kind")
		}
		if e.Mode.Kind == wasm.ElemActive {
			if err := admitI32Offset(e.Mode.Offset, m, importedTables); err != nil {
				return false, err
			}
		} else if len(e.Mode.Offset.BodyBytes) != 0 || len(e.Mode.Offset.Instrs) != 0 {
			return false, unsupported("inactive element offset")
		}
		for _, f := range e.Kind.Funcs {
			if uint64(f) < uint64(imported) && !importedTables || uint64(f) >= uint64(imported+len(m.Code)) {
				return false, unsupported("element function index/profile")
			}
		}
	}
	return hasTable, nil
}

func (f *integerEmitter) indirectCall(sig functionSpec, base, indexSlot int) {
	f.Load(5, f.SpillOffset(indexSlot), false) // zero-normalize dirty incoming i32
	f.ownedIndirectTarget(sig.key)
	// All dynamic guards precede writes to potentially smaller shared call arenas.
	f.restoreCallTransport()
	for j, wide := range sig.params {
		f.Load(5, f.SpillOffset(base+j), wide)
		f.a.Sd(enc.X5, enc.X10, int32(j*8))
	}
	if f.mode.foreignTableCalls {
		f.nativeTableCall(sig.key, len(sig.params), len(sig.results))
	} else {
		f.a.Jalr(enc.RA, enc.X28, 0)
	}
	f.restoreCallTransport()
	for j, wide := range sig.results {
		if wide {
			f.a.Ld(enc.X5, enc.X13, int32(j*8))
		} else {
			f.a.Lwu(enc.X5, enc.X13, int32(j*8))
		}
		f.Store(f.SpillOffset(base+j), 5, wide)
	}
}

// X5 is the normalized table index on entry; X28 is the guarded framed
// target on success. All guards finish before either call arena is written.
func (f *integerEmitter) ownedIndirectTarget(key uint64) {
	f.a.Ld(enc.X6, enc.X25, -80)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.a.Lwu(enc.X7, enc.X6, 0)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.GEU, enc.X5, enc.X7))
	f.a.Slli(enc.X5, enc.X5, 5)
	f.a.Add(enc.X6, enc.X6, enc.X5)
	// The descriptor's eight-byte header precedes 32-byte entries.
	f.a.Ld(enc.X28, enc.X6, 8)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X28, enc.Zero))
	f.a.Ld(enc.X7, enc.X6, 16)
	f.a.Constant64(enc.X5, int64(key))
	f.trap(trapIndirectSignature, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
	if f.mode.foreignTableCalls {
		// The owner supplies retained immutable canonical descriptors. Verify the
		// copied table row still names that exact entry/key/home before root writes.
		f.a.Ld(enc.X14, enc.X6, 32)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X14, enc.Zero))
		f.a.Constant64(enc.X5, 7)
		f.a.And(enc.X5, enc.X14, enc.X5)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X5, enc.Zero))
		f.a.Ld(enc.X7, enc.X14, 0)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X7, enc.X28))
		f.a.Ld(enc.X7, enc.X14, 8)
		f.a.Constant64(enc.X5, int64(key))
		f.trap(trapIndirectSignature, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
		f.a.Ld(enc.X7, enc.X14, 16)
		f.a.Ld(enc.X5, enc.X6, 24)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
		return
	}
	// Preserve the framed local-wrapper and exact instance identity contracts.
	// Sharing linear memory does not make two consumers the same instance.
	f.a.Ld(enc.X7, enc.X6, 24)
	f.a.Constant64(enc.X5, int64(abi.FuncRefLocalWrapperHomeTag))
	f.a.Or(enc.X5, enc.X5, enc.X25)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
	f.a.Ld(enc.X7, enc.X6, 32)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X7, enc.Zero))
	f.a.Ld(enc.X7, enc.X7, 32)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X7, enc.Zero))
	f.a.Ld(enc.X6, enc.X25, -abi.FuncRefDescPtrOffset)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.a.Ld(enc.X6, enc.X6, 32)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X7, enc.X6))
}

// Private references are immutable canonical descriptor handles, never numeric
// carriers. Public scopes also admit retained foreign local-wrapper descriptors.
func (f *integerEmitter) functionReference(index uint32) {
	f.a.Ld(enc.X5, enc.X25, -abi.FuncRefDescPtrOffset)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X5, enc.Zero))
	f.a.Constant64(enc.X6, int64(index+1)*40)
	f.a.Add(enc.X5, enc.X5, enc.X6)
}

// Returns the entry address in X6. X28 may hold a full-width reference.
func (f *integerEmitter) privateTableEntry() {
	f.a.Ld(enc.X6, enc.X25, -80)
	f.trap(trapTableBounds, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.a.Lwu(enc.X7, enc.X6, 0)
	f.trap(trapTableBounds, f.a.BranchLong(enc.GEU, enc.X5, enc.X7))
	f.a.Slli(enc.X5, enc.X5, 5)
	f.a.Add(enc.X6, enc.X6, enc.X5)
	f.a.Addi(enc.X6, enc.X6, 8)
}

func (f *integerEmitter) storePrivateReference() {
	null := f.a.BranchLong(enc.EQ, enc.X28, enc.Zero)
	if f.mode.foreignTableCalls {
		f.canonicalReferenceGuard(enc.X28, enc.X5, enc.X7)
	} else {
		// The table can only hold the current instance's local wrapper descriptors.
		// Validate the complete tag/context before the first write.
		f.a.Ld(enc.X7, enc.X28, 16)
		f.a.Constant64(enc.X5, int64(abi.FuncRefLocalWrapperHomeTag))
		f.a.Or(enc.X5, enc.X5, enc.X25)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
		f.a.Ld(enc.X7, enc.X28, 32)
		f.a.Ld(enc.X5, enc.X25, -abi.FuncRefDescPtrOffset)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X5, enc.Zero))
		f.a.Ld(enc.X5, enc.X5, 32)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
	}
	// No interrupt poll splits an entry: reuse never observes a partial copy.
	for offset := int32(0); offset < 32; offset += 8 {
		f.a.Ld(enc.X7, enc.X28, offset)
		f.a.Sd(enc.X7, enc.X6, offset)
	}
	done := f.Jump()
	if err := f.Patch(null, f.Position()); err != nil {
		f.err = err
	}
	for offset := int32(0); offset < 32; offset += 8 {
		f.a.Sd(enc.Zero, enc.X6, offset)
	}
	if err := f.Patch(done, f.Position()); err != nil {
		f.err = err
	}
}

// Public reference carriers can only originate from ref.func, authenticated
// table.get, null and their full-width local/control copies. Runtime scopes
// retain every possible descriptor owner through entry/resume/cold unwinding.
// A copied carrier must still be the exact immutable local-wrapper descriptor.
// No signature equality is required for table storage; call_indirect checks it.
func (f *integerEmitter) canonicalReferenceGuard(reference, first, second enc.Reg) {
	f.a.Constant64(first, 7)
	f.a.And(first, reference, first)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, first, enc.Zero))
	f.a.Ld(first, reference, 24)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, first, reference))
	f.a.Ld(first, reference, 16)
	f.a.Srli(first, first, abi.FuncRefEntryTagShift)
	f.a.Constant64(second, int64(abi.FuncRefLocalWrapperTagValue))
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, first, second))
	f.a.Ld(first, reference, 32)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, first, enc.Zero))
	f.a.Ld(first, reference, 0)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, first, enc.Zero))
}
