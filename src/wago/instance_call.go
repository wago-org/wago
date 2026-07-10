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
// uint64 token slot; accepting a reference-typed module remains controlled by
// the compiler feature support. v128 parameters/results are not expressible as
// a Value; use Invoke for those.
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

// GlobalValue returns an exported global's current value, typed.
func (in *Instance) GlobalValue(name string) (Value, error) {
	bits, err := in.Global(name)
	if err != nil {
		return Value{}, err
	}
	idx, ok := in.c.GlobalExports[name]
	if !ok || idx < 0 || idx >= len(in.c.Globals) {
		return Value{}, fmt.Errorf("no exported global %q", name)
	}
	return Value{typ: in.c.Globals[idx].Type, bits: bits}, nil
}

// SetGlobalValue writes a mutable exported global, checking the value's type
// against the global's declared type.
func (in *Instance) SetGlobalValue(name string, v Value) error {
	idx, ok := in.c.GlobalExports[name]
	if !ok || idx < 0 || idx >= len(in.c.Globals) {
		return fmt.Errorf("no exported global %q", name)
	}
	if want := in.c.Globals[idx].Type; v.typ != want {
		return fmt.Errorf("global %q is %s, got %s", name, want, v.typ)
	}
	return in.SetGlobal(name, v.bits)
}
