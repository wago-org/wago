//go:build linux && amd64 && !tinygo

package wago

import "testing"

func TestPrivateFixedOwnerPublicationFallback(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i32)(result i32)))
 (memory (export "memory") 1)
 (func (export "run") (param i32 i32)(result i32) local.get 0 call $step))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var in *Instance
	calls := 0
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int32) int32 {
		calls++
		if _, err := in.ExportedMemory("memory"); err != nil {
			panic(HostTrap{Err: err})
		}
		return v + 1
	}).Params(ValI32).Results(ValI32)
	in, err = Instantiate(c, InstantiateOptions{Imports: imports})
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
	if s.state.privateHost == nil || s.state.privateHost.owner == nil {
		t.Fatal("private owner absent")
	}
	out, err, admitted := s.tryPrivateFixed2(41, 0)
	if !admitted || err != nil || len(out) != 1 || out[0] != 42 {
		t.Fatalf("first call%v %v admitted%v", out, err, admitted)
	}
	if _, err, admitted := s.tryPrivateFixed2(41, 0); admitted || err != nil {
		t.Fatalf("published owner admitted%v err%v", admitted, err)
	}
	out, err = s.Invoke2(41, 0)
	if err != nil || len(out) != 1 || out[0] != 42 || calls != 2 || s.state.active.Load() {
		t.Fatalf("fallback%v %v calls%d", out, err, calls)
	}
}
