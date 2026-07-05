package wago

import (
	"context"
	"fmt"
)

// Call is the high-level, context-aware, typed invocation: arguments and results
// are typed Values checked against the export's signature. It wraps the low-level
// Invoke (untyped uint64 slots). ctx is honored for cancellation before the call
// begins. v128 parameters/results are not expressible as a Value; use Invoke for
// those.
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
