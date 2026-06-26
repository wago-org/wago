//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"math"
	"testing"

	wasm "github.com/wago-org/wago/src/core/compiler/wasm3"
	"github.com/wago-org/wago/src/core/runtime"
)

// runF64 executes function 0 with f64 args, returning the raw 8-byte result.
func runF64Raw(t *testing.T, m *wasm.Module, args ...float64) uint64 {
	t.Helper()
	code, err := CompileFunction(m, 0)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemory(1 << 16)
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	mem, entry, _ := runtime.MapCode(code)
	defer runtime.Unmap(mem)
	serArgs := ar.Alloc(64)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	for i, a := range args {
		binary.LittleEndian.PutUint64(serArgs[i*8:], math.Float64bits(a))
	}
	if err := eng.Call(entry, serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("call: %v", err)
	}
	return binary.LittleEndian.Uint64(results)
}

func f64fn(t *testing.T, body string) *wasm.Module {
	return watToModule(t, `(module (memory 1) (func (export "f") (param f64 f64) (result f64)`+"\n"+body+"))")
}

func TestF64Arith(t *testing.T) {
	cases := []struct {
		name string
		body string
		a, b float64
		want float64
	}{
		{"add", `local.get 0 local.get 1 f64.add`, 1.5, 2.25, 3.75},
		{"sub", `local.get 0 local.get 1 f64.sub`, 1.0, 0.25, 0.75},
		{"mul", `local.get 0 local.get 1 f64.mul`, 3.0, 0.5, 1.5},
		{"div", `local.get 0 local.get 1 f64.div`, 1.0, 8.0, 0.125},
		{"sqrt", `local.get 0 f64.sqrt`, 16.0, 0, 4.0},
		{"abs", `local.get 0 f64.abs`, -3.5, 0, 3.5},
		{"neg", `local.get 0 f64.neg`, 3.5, 0, -3.5},
		{"min", `local.get 0 local.get 1 f64.min`, 2.0, 5.0, 2.0},
		{"max", `local.get 0 local.get 1 f64.max`, 2.0, 5.0, 5.0},
		{"const", `f64.const 3.14159265358979 local.get 0 f64.add`, 1.0, 0, 4.14159265358979},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := math.Float64frombits(runF64Raw(t, f64fn(t, c.body), c.a, c.b))
			if math.Abs(got-c.want) > 1e-9 {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestF64Compare(t *testing.T) {
	cmp := func(op string, a, b float64) int32 {
		m := watToModule(t, `(module (func (export "f") (param f64 f64) (result i32)
			local.get 0 local.get 1 `+op+`))`)
		code, _ := CompileFunction(m, 0)
		eng, _ := runtime.NewEngine()
		defer eng.Close()
		jm, _ := runtime.NewJobMemory(1 << 16)
		defer jm.Close()
		ar, _ := runtime.NewArena(4096)
		defer ar.Close()
		mem, entry, _ := runtime.MapCode(code)
		defer runtime.Unmap(mem)
		serArgs := ar.Alloc(64)
		results := ar.Alloc(16)
		trap := ar.Alloc(8)
		binary.LittleEndian.PutUint64(serArgs[0:], math.Float64bits(a))
		binary.LittleEndian.PutUint64(serArgs[8:], math.Float64bits(b))
		eng.Call(entry, serArgs, jm.LinearMemory(), trap, results)
		return int32(binary.LittleEndian.Uint32(results))
	}
	if cmp("f64.lt", 1.5, 2.5) != 1 {
		t.Error("lt")
	}
	if cmp("f64.gt", 1.5, 2.5) != 0 {
		t.Error("gt")
	}
	if cmp("f64.eq", 2.5, 2.5) != 1 {
		t.Error("eq")
	}
	if cmp("f64.le", 2.5, 2.5) != 1 {
		t.Error("le")
	}
}

func TestFloatConversions(t *testing.T) {
	// i32 -> f64 -> i32 round trip; f64 = (i32)*1.5 truncated.
	m := watToModule(t, `(module (func (export "f") (param i32) (result i32)
		local.get 0 f64.convert_i32_s
		f64.const 1.5 f64.mul
		i32.trunc_f64_s))`)
	got := runI32(t, m, 10)
	if got != 15 { // trunc(10 * 1.5) = 15
		t.Fatalf("i32->f64->i32 = %d, want 15", got)
	}
	// f32 promote to f64 path.
	mp := watToModule(t, `(module (func (export "f") (param f32) (result f64)
		local.get 0 f64.promote_f32 f64.const 2.0 f64.mul))`)
	code, _ := CompileFunction(mp, 0)
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemory(1 << 16)
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	mem, entry, _ := runtime.MapCode(code)
	defer runtime.Unmap(mem)
	serArgs := ar.Alloc(64)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	binary.LittleEndian.PutUint32(serArgs[0:], math.Float32bits(2.5))
	eng.Call(entry, serArgs, jm.LinearMemory(), trap, results)
	got64 := math.Float64frombits(binary.LittleEndian.Uint64(results))
	if math.Abs(got64-5.0) > 1e-9 {
		t.Fatalf("f32 promote*2 = %v, want 5.0", got64)
	}
}

func TestF64Memory(t *testing.T) {
	m := watToModule(t, `(module (memory 1) (func (export "f") (param i32 f64) (result f64)
		local.get 0 local.get 1 f64.store
		local.get 0 f64.load))`)
	code, _ := CompileFunction(m, 0)
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemory(1 << 16)
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	mem, entry, _ := runtime.MapCode(code)
	defer runtime.Unmap(mem)
	serArgs := ar.Alloc(64)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	binary.LittleEndian.PutUint32(serArgs[0:], 256)
	binary.LittleEndian.PutUint64(serArgs[8:], math.Float64bits(3.14159))
	eng.Call(entry, serArgs, jm.LinearMemory(), trap, results)
	got := math.Float64frombits(binary.LittleEndian.Uint64(results))
	if math.Abs(got-3.14159) > 1e-9 {
		t.Fatalf("f64 store/load = %v, want 3.14159", got)
	}
}
