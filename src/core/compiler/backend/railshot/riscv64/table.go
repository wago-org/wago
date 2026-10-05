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
)

// Bound decoded resource metadata before validation or payload copies.
func ownedTableBounds(m *wasm.Module) error {
	if len(m.Tables) > 1 || len(m.Elements) > MaxOwnedElementSegments {
		return unsupported("private table/element count")
	}
	for _, t := range m.Tables {
		if t.Type.Limits.Min > MaxOwnedTableEntries || (t.Type.Limits.HasMax && t.Type.Limits.Max > MaxOwnedTableEntries) {
			return unsupported("private table capacity")
		}
	}
	remaining := MaxOwnedTableEntries
	for _, e := range m.Elements {
		if len(e.Kind.Funcs) > remaining || len(e.Kind.Exprs) > remaining {
			return unsupported("element payload bound")
		}
		remaining -= len(e.Kind.Funcs) + len(e.Kind.Exprs)
		if remaining < 0 {
			return unsupported("element payload bound")
		}
	}
	return nil
}

func admitOwnedTable(m *wasm.Module, production bool) (bool, error) {
	if err := ownedTableBounds(m); err != nil {
		return false, err
	}
	if len(m.Tables) == 0 {
		if len(m.Elements) != 0 {
			return false, unsupported("elements without private table")
		}
		return false, nil
	}
	if !production {
		return false, unsupported("isolated private table")
	}
	t := m.Tables[0]
	if t.Init != nil || t.Type.Limits.Addr64 || !wasm.EqualValType(wasm.RefVal(t.Type.Ref), wasm.FuncRef) {
		return false, unsupported("private nullable funcref table32 only")
	}
	for _, e := range m.Elements {
		if e.Mode.Kind != wasm.ElemActive || e.Mode.Table != 0 || e.Kind.Kind != wasm.ElemFuncs || len(e.Kind.Exprs) != 0 {
			return false, unsupported("active legacy function-index elements only")
		}
		if _, err := plainConst(e.Mode.Offset, false); err != nil {
			return false, err
		}
		for _, f := range e.Kind.Funcs {
			if uint64(f) >= uint64(len(m.Code)) {
				return false, unsupported("element requires a defined function")
			}
		}
	}
	return true, nil
}

func (f *integerEmitter) indirectCall(sig functionSpec, base, indexSlot int) {
	f.Load(5, f.SpillOffset(indexSlot), false) // zero-normalize dirty incoming i32
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
	f.a.Constant64(enc.X5, int64(sig.key))
	f.trap(trapIndirectSignature, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
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
	// All dynamic guards precede writes to potentially smaller shared call arenas.
	f.restoreCallTransport()
	for j, wide := range sig.params {
		f.Load(5, f.SpillOffset(base+j), wide)
		f.a.Sd(enc.X5, enc.X10, int32(j*8))
	}
	f.a.Jalr(enc.RA, enc.X28, 0)
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
