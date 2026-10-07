//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"reflect"
	"testing"
)

func TestPrivateOwnerResultShapes(t *testing.T) {
	for _, kind := range []string{"typed", "HostCall", "Caller"} {
		for _, tc := range []struct {
			name, results, body string
			want                []uint64
		}{
			{"narrow", "(result i32)", "", []uint64{0x80000001}},
			{"wide", "(result i64)", "drop i64.const 0xabcdef0180000001", []uint64{0xabcdef0180000001}},
			{"empty", "", "drop", nil},
			{"multiple", "(result i32 i64)", "i64.const 0xabcdef0180000001", []uint64{0x80000001, 0xabcdef0180000001}},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				imports := NewImports()
				calls := 0
				step := func(v int32) int32 {
					calls++
					if uint32(v) != 0x80000000 {
						panic("input not canonical")
					}
					return v + 1
				}
				var callback any = step
				if kind == "HostCall" {
					callback = func(c HostCall) { c.SetI32(0, step(c.I32(0))) }
				}
				if kind == "Caller" {
					callback = func(_ Caller, c HostCall) { c.SetI32(0, step(c.I32(0))) }
				}
				imports.HostFunc("env", "step", callback).Params(ValI32).Results(ValI32)
				module := fmt.Sprintf(`(module (import "env" "step" (func $step (param i32)(result i32)))
      (func (export "run") (param i64 i32) %s local.get 0 i32.wrap_i64 call $step %s))`, tc.results, tc.body)
				compiled, err := Compile(goHostSegmentConfig(), watToWasm(t, module))
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				in, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				s, err := fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				if h := s.state.privateHost; h == nil || h.owner == nil && h.viewOwner == nil {
					t.Fatal("private owner not admitted")
				}
				for i := 0; i < 3; i++ {
					got, err := s.Invoke2(0x1234567880000000, 0xffff000000000000)
					if err != nil || len(got) != len(tc.want) || len(got) > 0 && !reflect.DeepEqual(got, tc.want) {
						t.Fatalf("got%x err%v want%x", got, err, tc.want)
					}
				}
				if calls != 3 {
					t.Fatalf("callbacks%d", calls)
				}
			})
		}
	}
}
