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

// Each side has its own layout. Seeds put vectors at every position and include
// multiple vectors; the fuzz input is bounded to keep each check small.
func FuzzHostCallSlotMapping(f *testing.F) {
	for _, seed := range [][]byte{{}, {0}, {1, 0}, {0, 1}, {1, 1, 0, 1}, {0, 2, 3, 4, 5, 6, 7}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32 {
			data = data[:32]
		}
		catalog := []ValType{ValI64, ValV128, ValI32, ValF32, ValF64, ValExternRef, ValFuncRef, ValI31Ref}
		params := make([]ValType, len(data))
		results := make([]ValType, len(data)/2)
		for i, v := range data {
			params[i] = catalog[int(v)%len(catalog)]
		}
		for i := range results {
			results[i] = params[len(params)-1-i]
		}
		checkHostCallLayout(t, params, results)
		checkHostCallLayout(t, results, params)
	})
}

func checkHostCallLayout(t *testing.T, params, results []ValType) {
	t.Helper()
	// Build offsets by advancing a physical cursor, independently of hostCallSlot.
	offsets := func(types []ValType) ([]int, int) {
		out, cursor := make([]int, len(types)), 0
		for i, typ := range types {
			out[i] = cursor
			cursor++
			if typ == ValV128 {
				cursor++
			}
		}
		return out, cursor
	}
	po, pn := offsets(params)
	ro, rn := offsets(results)
	args, got, want := make([]uint64, pn), make([]uint64, rn), make([]uint64, rn)
	patterns := []uint64{0x8000000000000000, 0x80000000, 0x7ff8000000000042, 0x7fc00123, 0x0123456789abcdef}
	for i := range args {
		args[i] = patterns[i%len(patterns)] ^ uint64(i/len(patterns))
	}
	for i := range got {
		got[i] = uint64(1000 + i)
		want[i] = got[i]
	}
	sig := FuncSig{Params: params, Results: results}
	c := HostCall{params: args, results: got, sig: &sig}
	for i, typ := range params {
		lo, hi := c.RawParam(i)
		expectedHi := uint64(0)
		if typ == ValV128 {
			expectedHi = args[po[i]+1]
		}
		if lo != args[po[i]] || hi != expectedHi || c.paramSlotIndex(i, typ) != po[i] {
			t.Fatalf("parameter %d: %x/%x, offset %d", i, lo, hi, po[i])
		}
	}
	for i, typ := range results {
		lo, hi := patterns[i%len(patterns)], patterns[(i+1)%len(patterns)]
		c.SetRawResult(i, lo, hi)
		want[ro[i]] = lo
		if typ == ValV128 {
			want[ro[i]+1] = hi
		}
		if c.resultSlotIndex(i, typ) != ro[i] {
			t.Fatalf("result offset %d", i)
		}
		for j := range got {
			if got[j] != want[j] {
				t.Fatalf("write %d changed slot %d: %x != %x", i, j, got[j], want[j])
			}
		}
	}
	for _, index := range []int{-1, len(params)} {
		mustHostCallPanic(t, func() { c.RawParam(index) })
		mustHostCallPanic(t, func() { c.paramSlotIndex(index, ValI64) })
	}
	for _, index := range []int{-1, len(results)} {
		mustHostCallPanic(t, func() { c.SetRawResult(index, 0, 0) })
		mustHostCallPanic(t, func() { c.resultSlotIndex(index, ValI64) })
	}
	for i, typ := range params {
		wrong := ValI64
		if typ == wrong {
			wrong = ValI32
		}
		mustHostCallPanic(t, func() { c.paramSlotIndex(i, wrong) })
	}
	for i, typ := range results {
		wrong := ValI64
		if typ == wrong {
			wrong = ValI32
		}
		mustHostCallPanic(t, func() { c.resultSlotIndex(i, wrong) })
	}
}

func mustHostCallPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("expected checked access panic")
		}
	}()
	fn()
}

func TestHostCallZeroAndMixedLayouts(t *testing.T) {
	var c HostCall
	if c.ParamCount() != 0 || c.ResultCount() != 0 {
		t.Fatal("nonzero counts")
	}
	for _, i := range []int{-1, 0, 1} {
		mustHostCallPanic(t, func() { c.RawParam(i) })
		mustHostCallPanic(t, func() { c.SetRawResult(i, 1, 2) })
		mustHostCallPanic(t, func() { c.I64(i) })
		mustHostCallPanic(t, func() { c.SetI64(i, 1) })
	}
	layouts := [][]ValType{nil, {ValI64}, {ValV128, ValI64, ValI32}, {ValI64, ValV128, ValI32}, {ValI64, ValI32, ValV128}, {ValV128, ValI64, ValV128, ValV128}}
	for _, p := range layouts {
		for _, r := range layouts {
			checkHostCallLayout(t, p, r)
		}
	}
}
