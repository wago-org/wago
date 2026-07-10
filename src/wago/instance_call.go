package wago

import (
	"context"
	"fmt"
	"time"
)

// Call is the high-level, context-aware, typed invocation: arguments and results
// are typed Values checked against the export's signature. It wraps the low-level
// Invoke (untyped uint64 slots). ctx is honored for cancellation before the call
// begins. When the instance was created through a Runtime, its BeforeInvoke and
// AfterInvoke hooks fire around the call. Reference Values carry one opaque
// uint64 token slot; non-null funcrefs are valid only in the Runtime store (or
// standalone private store) that issued them. Accepting a reference-typed module
// remains controlled by compiler feature support. v128
// parameters/results are not expressible as a Value; use Invoke for those.
func (in *Instance) Call(ctx context.Context, export string, args ...Value) ([]Value, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	params, results, err := in.c.Signature(export)
	if err != nil {
		return nil, err
	}
	if len(args) != len(params) {
		return nil, fmt.Errorf("%s expects %d arg(s), got %d", export, len(params), len(args))
	}
	slots := make([]uint64, len(args))
	for i, a := range args {
		if params[i] == ValV128 {
			return nil, fmt.Errorf("%s param %d is v128; use Invoke for v128 values", export, i)
		}
		if a.typ != params[i] {
			return nil, fmt.Errorf("%s param %d is %s, got %s", export, i, params[i], a.typ)
		}
		slots[i] = a.bits
	}
	for i, r := range results {
		if r == ValV128 {
			return nil, fmt.Errorf("%s result %d is v128; use Invoke for v128 values", export, i)
		}
	}

	// Fast path: no runtime or no invoke hooks — invoke directly, zero overhead.
	if in.rt == nil || (len(in.rt.hooks.beforeInvoke) == 0 && len(in.rt.hooks.afterInvoke) == 0) {
		return in.callInner(export, slots, results)
	}

	ictx := &InvokeContext{Runtime: in.rt, Instance: in, Export: export, Args: args, Start: time.Now(), Metadata: map[string]any{}}
	for _, fn := range in.rt.hooks.beforeInvoke {
		if err := fn(ictx); err != nil {
			// A BeforeInvoke veto aborts the call; report it to AfterInvoke too so
			// paired hooks can unwind.
			for _, af := range in.rt.hooks.afterInvoke {
				af(ictx, nil, err)
			}
			return nil, err
		}
	}
	out, err := in.callInner(export, slots, results)
	for _, fn := range in.rt.hooks.afterInvoke {
		fn(ictx, out, err)
	}
	return out, err
}

// callInner performs the actual invocation and result decoding.
func (in *Instance) callInner(export string, slots []uint64, results []ValType) ([]Value, error) {
	raw, err := in.Invoke(export, slots...)
	if err != nil {
		return nil, err
	}
	out := make([]Value, len(results))
	for i, r := range results {
		out[i] = Value{typ: r, bits: raw[i]}
	}
	return out, nil
}

// GlobalValue returns an exported global's current value, typed. Non-null
// funcrefs are translated from internal descriptors to opaque store-owned tokens.
func (in *Instance) GlobalValue(name string) (Value, error) {
	idx, err := in.exportedGlobalIndex(name)
	if err != nil {
		return Value{}, err
	}
	g := in.c.Globals[idx]
	if g.Type == ValV128 {
		return Value{}, fmt.Errorf("exported global %q is v128; use GlobalV128", name)
	}
	bits := readGlobalObject(in.globalCells[idx], g.Type)
	if g.Type == ValFuncRef && bits != 0 {
		store, err := in.funcrefStoreForEgress()
		if err != nil {
			return Value{}, fmt.Errorf("global %q: invalid funcref value: %w", name, err)
		}
		token, err := store.issue(in, bits)
		if err != nil {
			return Value{}, fmt.Errorf("global %q: invalid funcref value: %w", name, err)
		}
		bits = token
	}
	if g.Type == ValExternRef {
		return Value{}, fmt.Errorf("exported global %q is externref; externref globals are unsupported", name)
	}
	return Value{typ: g.Type, bits: bits}, nil
}

// SetGlobalValue writes a mutable exported global, checking the value's type
// against the global's declared type. Non-null funcref tokens are resolved only
// through the instance's exact reference store before native-visible storage.
func (in *Instance) SetGlobalValue(name string, v Value) error {
	idx, err := in.exportedGlobalIndex(name)
	if err != nil {
		return err
	}
	g := in.c.Globals[idx]
	if v.typ != g.Type {
		return fmt.Errorf("global %q is %s, got %s", name, g.Type, v.typ)
	}
	if !g.Mutable {
		return fmt.Errorf("exported global %q is immutable", name)
	}
	bits := v.bits
	if g.Type == ValFuncRef && bits != 0 {
		if in.refStore == nil {
			return fmt.Errorf("global %q: invalid funcref token", name)
		}
		descriptor, ok := in.refStore.resolve(bits)
		if !ok {
			return fmt.Errorf("global %q: invalid funcref token", name)
		}
		bits = descriptor
	}
	if g.Type == ValExternRef {
		return fmt.Errorf("global %q is externref; externref globals are unsupported", name)
	}
	if g.Type == ValV128 {
		return fmt.Errorf("global %q is v128; use SetGlobalV128", name)
	}
	writeGlobalObject(in.globalCells[idx], g.Type, bits)
	return nil
}
