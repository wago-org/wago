//go:build !tinygo

package wago

import (
	"fmt"
	"math"
	"reflect"
)

var hostModuleType = reflect.TypeOf((*HostModule)(nil)).Elem()

// reflectSyncHost adapts a native Go function to a HostFunc by reflection.
// The function's numeric params/results must match sig — i32↔int32/uint32,
// i64↔int64/uint64, f32↔float32, f64↔float64 (named types with those kinds are
// accepted, e.g. `type Errno uint32`). An optional leading HostModule parameter
// receives the calling instance.
func reflectSyncHost(v any, sig FuncSig) (HostFunc, error) {
	rv := reflect.ValueOf(v)
	rt := rv.Type()
	if rt.Kind() != reflect.Func {
		return nil, fmt.Errorf("host import must be a function, got %T", v)
	}
	off := 0
	if rt.NumIn() > 0 && rt.In(0) == hostModuleType {
		off = 1
	}
	if got := rt.NumIn() - off; got != len(sig.Params) {
		return nil, fmt.Errorf("host func takes %d wasm params, import expects %d", got, len(sig.Params))
	}
	if rt.NumOut() != len(sig.Results) {
		return nil, fmt.Errorf("host func returns %d values, import expects %d", rt.NumOut(), len(sig.Results))
	}
	inTypes := make([]reflect.Type, rt.NumIn())
	for i := range inTypes {
		inTypes[i] = rt.In(i)
	}
	for i, pt := range sig.Params {
		if !goTypeMatches(inTypes[off+i], pt) {
			return nil, fmt.Errorf("host func param %d is %s, want %s", i, inTypes[off+i], pt)
		}
	}
	for i, rtp := range sig.Results {
		if !goTypeMatches(rt.Out(i), rtp) {
			return nil, fmt.Errorf("host func result %d is %s, want %s", i, rt.Out(i), rtp)
		}
	}
	wantMod := off == 1
	params := append([]ValType(nil), sig.Params...)
	results := append([]ValType(nil), sig.Results...)
	return func(m HostModule, args, res []uint64) {
		in := make([]reflect.Value, len(inTypes))
		if wantMod {
			in[0] = reflect.ValueOf(m)
		}
		for i, pt := range params {
			in[off+i] = decodeArg(inTypes[off+i], pt, args[i])
		}
		out := rv.Call(in)
		for i, rt := range results {
			res[i] = encodeResult(out[i], rt)
		}
	}, nil
}

func goTypeMatches(t reflect.Type, vt ValType) bool {
	switch vt {
	case ValI32:
		return t.Kind() == reflect.Int32 || t.Kind() == reflect.Uint32
	case ValI64:
		return t.Kind() == reflect.Int64 || t.Kind() == reflect.Uint64
	case ValF32:
		return t.Kind() == reflect.Float32
	case ValF64:
		return t.Kind() == reflect.Float64
	}
	return false
}

// decodeArg builds a reflect.Value of Go type t from a wasm slot (vt gives the
// wasm type; i32/f32 occupy the low 32 bits).
func decodeArg(t reflect.Type, vt ValType, bits uint64) reflect.Value {
	var v reflect.Value
	switch vt {
	case ValI32:
		if t.Kind() == reflect.Uint32 {
			v = reflect.ValueOf(uint32(bits))
		} else {
			v = reflect.ValueOf(int32(uint32(bits)))
		}
	case ValI64:
		if t.Kind() == reflect.Uint64 {
			v = reflect.ValueOf(bits)
		} else {
			v = reflect.ValueOf(int64(bits))
		}
	case ValF32:
		v = reflect.ValueOf(math.Float32frombits(uint32(bits)))
	case ValF64:
		v = reflect.ValueOf(math.Float64frombits(bits))
	}
	if v.Type() != t {
		v = v.Convert(t) // named types (e.g. `type Errno uint32`)
	}
	return v
}

// encodeResult packs a returned reflect.Value into a wasm slot per vt.
func encodeResult(v reflect.Value, vt ValType) uint64 {
	switch vt {
	case ValI32:
		if v.Kind() == reflect.Uint32 {
			return uint64(uint32(v.Uint()))
		}
		return uint64(uint32(v.Int()))
	case ValI64:
		if v.Kind() == reflect.Uint64 {
			return v.Uint()
		}
		return uint64(v.Int())
	case ValF32:
		return uint64(math.Float32bits(float32(v.Float())))
	case ValF64:
		return math.Float64bits(v.Float())
	}
	return 0
}
