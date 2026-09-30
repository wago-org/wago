//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type indexedHostCase struct {
	name            string
	params, results []wasm.ValType
}

func indexedHostCases() []indexedHostCase {
	cases := []indexedHostCase{{name: "mixed", params: []wasm.ValType{wasm.I32, wasm.F64, wasm.I64, wasm.F32}, results: []wasm.ValType{wasm.F32, wasm.I64, wasm.F64, wasm.I32}}}
	for _, n := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}, {4, 3}, {8, 4}, {16, 8}} {
		c := indexedHostCase{name: fmt.Sprintf("%d-%d", n[0], n[1]), params: make([]wasm.ValType, n[0]), results: make([]wasm.ValType, n[1])}
		for i := range c.params {
			c.params[i] = wasm.I64
		}
		for i := range c.results {
			c.results[i] = wasm.I64
		}
		cases = append(cases, c)
	}
	return cases
}

func indexedHostModule(c indexedHostCase) []byte {
	body := []byte{0}
	for i := range c.params {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x10, 0, 0x0b)
	return returningImportModule(wasmtest.FuncType(c.params, c.results), body)
}

func indexedHostValue(typ wasm.ValType, value int64) uint64 {
	switch typ {
	case wasm.I32:
		return I32(int32(value))
	case wasm.F32:
		return F32(float32(value))
	case wasm.F64:
		return F64(float64(value))
	default:
		return I64(value)
	}
}

// Inputs and outputs depend on position; every parameter affects every result.
func indexedHostCallback(c indexedHostCase, raw bool) HostCallFunc {
	return func(call HostCall) {
		sum := int64(7)
		for i, typ := range c.params {
			var value int64
			if raw {
				bits, _ := call.RawParam(i)
				switch typ {
				case wasm.I32:
					value = int64(AsI32(bits))
				case wasm.F32:
					value = int64(AsF32(bits))
				case wasm.F64:
					value = int64(AsF64(bits))
				default:
					value = AsI64(bits)
				}
			} else {
				switch typ {
				case wasm.I32:
					value = int64(call.I32(i))
				case wasm.F32:
					value = int64(call.F32(i))
				case wasm.F64:
					value = int64(call.F64(i))
				default:
					value = call.I64(i)
				}
			}
			sum += int64(i+1) * value
		}
		for i, typ := range c.results {
			value := sum + int64(13*i)
			if raw {
				call.SetRawResult(i, indexedHostValue(typ, value), 0)
				continue
			}
			switch typ {
			case wasm.I32:
				call.SetI32(i, int32(value))
			case wasm.F32:
				call.SetF32(i, float32(value))
			case wasm.F64:
				call.SetF64(i, float64(value))
			default:
				call.SetI64(i, value)
			}
		}
	}
}

func setupIndexedHost(tb testing.TB, c indexedHostCase, raw, caller bool) (*Instance, []uint64, []uint64) {
	tb.Helper()
	compiled, err := Compile(indexedHostModule(c))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { compiled.Close() })
	callback := indexedHostCallback(c, raw)
	var fn any = callback
	if caller {
		fn = CallerHostCallFunc(func(_ Caller, call HostCall) { callback(call) })
	}
	in, err := Instantiate(compiled, testImports("env.f", fn))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { in.Close() })
	if caller {
		if _, ok := in.syncHosts[0].fn.(CallerHostCallFunc); !ok {
			tb.Fatal("wrong caller lane")
		}
	} else if _, ok := in.syncHosts[0].fn.(HostCallFunc); !ok {
		tb.Fatal("wrong universal lane")
	}
	args, want := make([]uint64, len(c.params)), make([]uint64, len(c.results))
	sum := int64(7)
	for i, typ := range c.params {
		value := int64(3*i + 2)
		args[i] = indexedHostValue(typ, value)
		sum += int64(i+1) * value
	}
	for i, typ := range c.results {
		want[i] = indexedHostValue(typ, sum+int64(13*i))
	}
	// Public metadata changes must not change a live binding's layout.
	p, r, err := compiled.Signature("g")
	if err != nil {
		tb.Fatal(err)
	}
	for i := range p {
		p[i] = ValV128
	}
	for i := range r {
		r[i] = ValV128
	}
	for i := range compiled.Funcs {
		compiled.Funcs[i].Params = []ValType{ValV128}
		compiled.Funcs[i].Results = nil
	}
	return in, args, want
}

func checkIndexedHost(tb testing.TB, in *Instance, args, want []uint64) {
	tb.Helper()
	got, err := in.Invoke("g", args...)
	if err != nil {
		tb.Fatal(err)
	}
	if len(got) != len(want) {
		tb.Fatalf("result count %d != %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			tb.Fatalf("result %d: %x != %x", i, got[i], want[i])
		}
	}
}

func TestHostCallIndexedNative(t *testing.T) {
	for _, c := range indexedHostCases() {
		for _, raw := range []bool{false, true} {
			for _, caller := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/raw%t/caller%t", c.name, raw, caller), func(t *testing.T) {
					in, args, want := setupIndexedHost(t, c, raw, caller)
					checkIndexedHost(t, in, args, want)
					if c.name == "16-8" {
						if got := testing.AllocsPerRun(100, func() { checkIndexedHost(t, in, args, want) }); got != 0 {
							t.Fatalf("invocation allocations = %v", got)
						}
					}
				})
			}
		}
	}
}

func TestHostCallIndexedNativeReferences(t *testing.T) {
	for _, raw := range []bool{false, true} {
		for _, caller := range []bool{false, true} {
			t.Run(fmt.Sprintf("raw%t/caller%t", raw, caller), func(t *testing.T) {
				c := indexedHostCase{params: []wasm.ValType{wasm.ExternRef, wasm.I64, wasm.ExternRef}, results: []wasm.ValType{wasm.ExternRef, wasm.ExternRef, wasm.I64}}
				compiled := MustCompile(indexedHostModule(c))
				defer compiled.Close()
				callback := HostCallFunc(func(call HostCall) {
					if raw {
						a, _ := call.RawParam(0)
						b, _ := call.RawParam(2)
						n, _ := call.RawParam(1)
						call.SetRawResult(0, b, 0)
						call.SetRawResult(1, a, 0)
						call.SetRawResult(2, n+9, 0)
					} else {
						call.SetExternRef(0, call.ExternRef(2))
						call.SetExternRef(1, call.ExternRef(0))
						call.SetI64(2, call.I64(1)+9)
					}
				})
				var fn any = callback
				if caller {
					fn = CallerHostCallFunc(func(_ Caller, call HostCall) { callback(call) })
				}
				in, err := Instantiate(compiled, testImports("env.f", fn))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				a, err := in.NewExternRef("first")
				if err != nil {
					t.Fatal(err)
				}
				defer in.ReleaseExternRef(a)
				b, err := in.NewExternRef("last")
				if err != nil {
					t.Fatal(err)
				}
				defer in.ReleaseExternRef(b)
				checkIndexedHost(t, in, []uint64{a.token, 31, b.token}, []uint64{b.token, a.token, 40})
			})
		}
	}
}

func BenchmarkHostCallIndexedInvoke(b *testing.B) {
	for _, c := range indexedHostCases() {
		for _, raw := range []bool{false, true} {
			for _, caller := range []bool{false, true} {
				b.Run(fmt.Sprintf("%s/raw%t/caller%t", c.name, raw, caller), func(b *testing.B) {
					in, args, want := setupIndexedHost(b, c, raw, caller)
					checkIndexedHost(b, in, args, want)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if _, err := in.Invoke("g", args...); err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
					checkIndexedHost(b, in, args, want)
				})
			}
		}
	}
}

func TestHostCallOwnedSignatureMutation(t *testing.T) {
	for _, caller := range []bool{false, true} {
		t.Run(fmt.Sprintf("caller%t", caller), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			sig := FuncSig{Params: []ValType{ValI64, ValI64}, Results: []ValType{ValI64, ValI64}}
			callback := HostCallFunc(func(call HostCall) {
				// The public input is still caller-owned, even while its callback runs.
				sig.Params[0], sig.Results[0] = ValV128, ValV128
				call.SetI64(0, call.I64(0)+1)
				call.SetI64(1, call.I64(1)+2)
			})
			var fn any = callback
			if caller {
				fn = CallerHostCallFunc(func(_ Caller, call HostCall) { callback(call) })
			}
			owner, err := rt.NewHostFuncRef(fn, sig)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			c := indexedHostCase{params: []wasm.ValType{wasm.I64, wasm.I64}, results: []wasm.ValType{wasm.I64, wasm.I64}}
			mod, err := rt.Compile(indexedHostModule(c))
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			in, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.f", owner)))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			checkIndexedHost(t, in, []uint64{11, 29}, []uint64{12, 31})
		})
	}
}

func TestHostCallIndexedDispatchAllocations(t *testing.T) {
	c := indexedHostCases()[len(indexedHostCases())-1]
	for _, caller := range []bool{false, true} {
		callback := indexedHostCallback(c, false)
		var fn any = callback
		if caller {
			fn = CallerHostCallFunc(func(_ Caller, call HostCall) { callback(call) })
		}
		params, results := make([]ValType, 16), make([]ValType, 8)
		for i := range params {
			params[i] = ValI64
		}
		for i := range results {
			results[i] = ValI64
		}
		binding, err := bindSyncHostImport(fn, FuncSig{Params: params, Results: results})
		if err != nil {
			t.Fatal(err)
		}
		args, out := make([]uint64, 16), make([]uint64, 8)
		if got := testing.AllocsPerRun(1000, func() { binding.call(instanceHostModule{}, args, out) }); got != 0 {
			t.Fatalf("caller %t: %v allocations", caller, got)
		}
	}
}
