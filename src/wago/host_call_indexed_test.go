package wago

import (
	"fmt"
	"testing"
)

func TestHostCallIndexedSlots(t *testing.T) {
	for _, wide := range []bool{false, true} {
		t.Run(fmt.Sprintf("wide%t", wide), func(t *testing.T) {
			types := []ValType{ValI64, ValI32, ValI64}
			params := []uint64{10, 20, 30}
			if wide {
				types = []ValType{ValI64, ValV128, ValI64}
				params = []uint64{10, 20, 21, 30}
			}
			sig := FuncSig{Params: types, Results: types}
			results := make([]uint64, len(params))
			call := HostCall{params: params, results: results, sig: &sig}
			for i := range types {
				lo, hi := call.RawParam(i)
				call.SetRawResult(i, lo+1, hi+1)
			}
			if call.I64(2) != 30 || results[len(results)-1] != 31 {
				t.Fatalf("wrong last slot: %v", results)
			}
			call.SetI64(2, 40)
			if results[len(results)-1] != 40 {
				t.Fatal("typed result missed last slot")
			}
			if wide && (results[1] != 21 || results[2] != 22) {
				t.Fatalf("wrong vector lanes: %v", results)
			}
		})
	}
	for _, test := range []struct {
		name string
		fn   func(HostCall)
	}{
		{"negative", func(c HostCall) { c.I64(-1) }},
		{"past-end", func(c HostCall) { c.RawParam(2) }},
		{"wrong-type", func(c HostCall) { c.I32(1) }},
		{"result-past-end", func(c HostCall) { c.SetRawResult(2, 0, 0) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected checked access panic")
				}
			}()
			sig := FuncSig{Params: []ValType{ValI64, ValI64}, Results: []ValType{ValI64, ValI64}}
			test.fn(HostCall{params: []uint64{1, 2}, results: make([]uint64, 2), sig: &sig})
		})
	}
}

func BenchmarkHostCallIndexed(b *testing.B) {
	for _, n := range []int{1, 4, 16, 64} {
		for _, raw := range []bool{false, true} {
			b.Run(fmt.Sprintf("arity%d/raw%t", n, raw), func(b *testing.B) {
				types := make([]ValType, n)
				args := make([]uint64, n)
				results := make([]uint64, n)
				for i := range types {
					types[i] = ValI64
					args[i] = uint64(i)
				}
				fn := HostCallFunc(func(call HostCall) {
					for i := 0; i < call.ParamCount(); i++ {
						call.SetI64(i, call.I64(i)+1)
					}
				})
				if raw {
					fn = func(call HostCall) {
						for i := 0; i < call.ParamCount(); i++ {
							lo, hi := call.RawParam(i)
							call.SetRawResult(i, lo+1, hi)
						}
					}
				}
				binding, err := bindSyncHostImport(fn, FuncSig{Params: types, Results: types})
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					binding.callUnchecked(instanceHostModule{}, args, results)
				}
				if results[n-1] != uint64(n) {
					b.Fatal(results)
				}
			})
		}
	}
}

func TestHostCallIndexedUsesCurrentScalarTypes(t *testing.T) {
	sig := FuncSig{Params: []ValType{ValI64, ValI64}, Results: []ValType{ValI64, ValI64}}
	call := HostCall{params: []uint64{1, 2}, results: make([]uint64, 2), sig: &sig}
	if call.I64(1) != 2 {
		t.Fatal("initial scalar value")
	}
	sig.Params[1] = ValI32
	sig.Results[1] = ValI32
	call.SetI32(1, call.I32(1)+1)
	if call.results[1] != 3 {
		t.Fatal("current scalar type was not used")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("stale I64 type accepted")
		}
	}()
	call.I64(1)
}

// Mixed-slot views test the existing raw accessor fallback only. Native v128
// host callbacks remain unsupported and are not used by this benchmark.
func BenchmarkHostCallIndexedMixed(b *testing.B) {
	for _, n := range []int{4, 16, 64} {
		b.Run(fmt.Sprintf("arity%d", n), func(b *testing.B) {
			types := make([]ValType, n)
			slots := n
			for i := range types {
				types[i] = ValI64
				if i%3 == 1 {
					types[i] = ValV128
					slots++
				}
			}
			args := make([]uint64, slots)
			results := make([]uint64, slots)
			for i := range args {
				args[i] = uint64(i)
			}
			sig := FuncSig{Params: types, Results: types}
			call := HostCall{params: args, results: results, sig: &sig}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := range types {
					lo, hi := call.RawParam(j)
					call.SetRawResult(j, lo+1, hi+1)
				}
			}
			if results[len(results)-1] != uint64(slots) {
				b.Fatal(results)
			}
		})
	}
}
