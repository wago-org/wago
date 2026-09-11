package wago

import "fmt"

// PreparedI32ToI32 is a bound local Wasm export with signature (i32) -> i32.
// It has PreparedFunction's ownership and concurrency rules, but its hot call
// has no name lookup, variadic arguments, slot slices, or result decoding at
// the call site.
type PreparedI32ToI32 struct {
	prepared *PreparedFunction
}

// PreparedI32I32ToI32 is a bound local Wasm export with signature
// (i32, i32) -> i32. It has the same ownership and concurrency rules as
// PreparedI32ToI32.
type PreparedI32I32ToI32 struct {
	prepared *PreparedFunction
}

// PrepareI32ToI32 resolves export and verifies its exact scalar signature once.
func (in *Instance) PrepareI32ToI32(export string) (*PreparedI32ToI32, error) {
	prepared, err := in.PrepareFunction(export)
	if err != nil {
		return nil, err
	}
	if !prepared.hasSignature([]ValType{ValI32}, []ValType{ValI32}) {
		return nil, fmt.Errorf("wago: prepare function %q: expected signature (i32) -> i32", export)
	}
	return &PreparedI32ToI32{prepared: prepared}, nil
}

// PrepareI32I32ToI32 resolves export and verifies its exact scalar signature once.
func (in *Instance) PrepareI32I32ToI32(export string) (*PreparedI32I32ToI32, error) {
	prepared, err := in.PrepareFunction(export)
	if err != nil {
		return nil, err
	}
	if !prepared.hasSignature([]ValType{ValI32, ValI32}, []ValType{ValI32}) {
		return nil, fmt.Errorf("wago: prepare function %q: expected signature (i32, i32) -> i32", export)
	}
	return &PreparedI32I32ToI32{prepared: prepared}, nil
}

func (fn *PreparedFunction) hasSignature(params, results []ValType) bool {
	if fn == nil || len(fn.paramTypes) != len(params) || len(fn.resultTypes) != len(results) {
		return false
	}
	for i := range params {
		if fn.paramTypes[i] != params[i] {
			return false
		}
	}
	for i := range results {
		if fn.resultTypes[i] != results[i] {
			return false
		}
	}
	return true
}

// Call invokes the bound export.
func (fn *PreparedI32ToI32) Call(a int32) (int32, error) {
	if fn == nil || fn.prepared == nil {
		return 0, fmt.Errorf("wago: invoke closed prepared function")
	}
	results, err := fn.prepared.Invoke1(I32(a))
	if err != nil {
		return 0, err
	}
	return AsI32(results[0]), nil
}

// Call invokes the bound export.
func (fn *PreparedI32I32ToI32) Call(a, b int32) (int32, error) {
	if fn == nil || fn.prepared == nil {
		return 0, fmt.Errorf("wago: invoke closed prepared function")
	}
	results, err := fn.prepared.Invoke2(I32(a), I32(b))
	if err != nil {
		return 0, err
	}
	return AsI32(results[0]), nil
}
