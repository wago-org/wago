package wago

import (
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func normalizeSnapshotLimit(limit uint64) uint64 {
	if limit == 0 {
		limit = uint64(DefaultArtifactLimits().MaxDecodedBytes)
	}
	return min(limit, uint64(maxInt()))
}

func snapshotLimitError(limit uint64) error {
	return &ResourceLimitError{Scope: "compiled snapshot", Resource: "metadata bytes", Requested: limit + 1, Limit: limit}
}

type snapshotBudget struct {
	remaining, limit uint64
	err              error
}

func (b *snapshotBudget) add(n int, width uintptr) {
	if b.err != nil || n == 0 {
		return
	}
	if width != 0 && uint64(n) > b.remaining/uint64(width) {
		b.err = snapshotLimitError(b.limit)
		return
	}
	bytes := uint64(n) * uint64(width)
	b.remaining -= bytes
	// Allow size-class/page rounding without an input-byte expansion factor.
	rounding := min(bytes, uint64(8192))
	if rounding > b.remaining {
		b.err = snapshotLimitError(b.limit)
		return
	}
	b.remaining -= rounding
}

func snapshotSlice[T any](b *snapshotBudget, values []T) {
	var value T
	b.add(len(values), unsafe.Sizeof(value))
}

// Count each destination copy, even when many source slices alias. The byte
// quota also bounds all later packed int counts before those sums are made.
// Strings and private immutable sidecars are shared, not cloned here.
func snapshotMetadataBytes(c *Compiled, limit uint64) (uint64, error) {
	limit = normalizeSnapshotLimit(limit)
	b := snapshotBudget{remaining: limit, limit: limit}
	b.add(1, unsafe.Sizeof(Compiled{}))
	snapshotSlice(&b, c.Entry)
	snapshotSlice(&b, c.InternalEntry)
	snapshotSlice(&b, c.Funcs)
	snapshotSlice(&b, c.importFuncSigs)
	snapshotSlice(&b, c.Types)
	snapshotSlice(&b, c.ValueTypes)
	snapshotSlice(&b, c.Imports)
	snapshotSlice(&b, c.GlobalImports)
	snapshotSlice(&b, c.Globals)
	snapshotSlice(&b, c.FuncTypeID)
	snapshotSlice(&b, c.Elems)
	snapshotSlice(&b, c.passiveElems)
	snapshotSlice(&b, c.Data)
	snapshotSlice(&b, c.PassiveData)
	snapshotSlice(&b, c.GCTypeDescs)
	b.add(len(c.Exports), 64)
	b.add(len(c.GlobalExports), 64)
	if b.err != nil {
		return 0, b.err
	}
	for _, sigs := range [][]FuncSig{c.Funcs, c.importFuncSigs} {
		for _, sig := range sigs {
			snapshotSlice(&b, sig.Params)
			snapshotSlice(&b, sig.Results)
			if b.err != nil {
				return 0, b.err
			}
		}
	}
	for _, typ := range c.Types {
		snapshotSlice(&b, typ.Supers)
		snapshotSlice(&b, typ.Params)
		snapshotSlice(&b, typ.Results)
		snapshotSlice(&b, typ.Fields)
		if b.err != nil {
			return 0, b.err
		}
	}
	for _, global := range c.Globals {
		snapshotSlice(&b, global.InitExpr)
		if b.err != nil {
			return 0, b.err
		}
	}
	for _, elems := range [][]ElemInit{c.Elems, c.passiveElems} {
		for _, elem := range elems {
			snapshotSlice(&b, elem.Offset.Expr)
			snapshotSlice(&b, elem.Values)
			if b.err != nil {
				return 0, b.err
			}
			for _, ref := range elem.Values {
				snapshotSlice(&b, ref.Expr)
				if b.err != nil {
					return 0, b.err
				}
			}
		}
	}
	for _, data := range c.Data {
		snapshotSlice(&b, data.Bytes)
		snapshotSlice(&b, data.Offset.Expr)
		if b.err != nil {
			return 0, b.err
		}
	}
	for _, data := range c.PassiveData {
		snapshotSlice(&b, data.Bytes)
		if b.err != nil {
			return 0, b.err
		}
	}
	for _, typ := range c.GCTypeDescs {
		snapshotSlice(&b, typ.Fields)
		if b.err != nil {
			return 0, b.err
		}
	}
	if ns := c.Names; ns != nil {
		b.add(1, unsafe.Sizeof(*ns))
		if ns.ModuleName != nil {
			b.add(1, unsafe.Sizeof(*ns.ModuleName))
		}
		for _, names := range []wasm.NameMap{ns.FunctionNames, ns.TypeNames, ns.TableNames, ns.MemoryNames, ns.GlobalNames, ns.ElementNames, ns.DataNames, ns.TagNames} {
			snapshotSlice(&b, names)
		}
		for _, indirect := range []wasm.IndirectNameMap{ns.LocalNames, ns.LabelNames, ns.FieldNames} {
			snapshotSlice(&b, indirect)
			if b.err != nil {
				return 0, b.err
			}
			for _, assoc := range indirect {
				snapshotSlice(&b, assoc.Names)
				if b.err != nil {
					return 0, b.err
				}
			}
		}
	}
	return limit - b.remaining, b.err
}
