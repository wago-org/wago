//go:build linux && amd64 && !tinygo

package wago

import (
	"os"
	"testing"
)

func TestForcedSyncBindingReusesCompiledCode(t *testing.T) {
	c := MustCompile(voidImportCallModule())
	defer c.Close()
	imports := Imports{"env.f": HostFunc(func(HostModule, []uint64, []uint64) {})}
	linked, err := c.linkModuleMode(imports, nil, true)
	if err != nil {
		t.Fatalf("forced synchronous binding: %v", err)
	}
	if linked != c || !c.dynamicImports || len(c.Code) == 0 {
		t.Fatalf("binding = %p owner=%p dynamic=%v code=%d", linked, c, c.dynamicImports, len(c.Code))
	}
}

func TestImportedInstancesShareCodeAcrossBindings(t *testing.T) {
	c := MustCompile(returningImportModule(returningI32Sig(), []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}))
	defer c.Close()
	instantiate := func(delta uint64) *Instance {
		t.Helper()
		in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(_ HostModule, params, results []uint64) {
			results[0] = params[0] + delta
		})}})
		if err != nil {
			t.Fatalf("Instantiate delta=%d: %v", delta, err)
		}
		return in
	}
	first := instantiate(1)
	defer first.Close()
	second := instantiate(2)
	defer second.Close()
	if first.c != c || second.c != c || first.base != second.base {
		t.Fatalf("instances did not share compiled image: c=%p first=%p/%#x second=%p/%#x", c, first.c, first.base, second.c, second.base)
	}
	for _, tc := range []struct {
		in   *Instance
		want int32
	}{{first, 8}, {second, 9}} {
		got, err := tc.in.Invoke("g", I32(7))
		if err != nil || AsI32(got[0]) != tc.want {
			t.Fatalf("Invoke = %v, %v; want %d", got, err, tc.want)
		}
	}
}

func TestImportedModuleCodeIsBindingIndependent(t *testing.T) {
	src, err := os.ReadFile("../../bench/corpus/jsonproc.wasm")
	if err != nil {
		t.Skip("jsonproc.wasm not present")
	}
	c, err := Compile(nil, src)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.dynamicImports || len(c.Code) == 0 || len(c.Entry) == 0 {
		t.Fatalf("imported module dynamic=%v code=%d entries=%d", c.dynamicImports, len(c.Code), len(c.Entry))
	}
	stubs := Imports{}
	for _, name := range c.Imports {
		stubs[name] = HostFunc(func(HostModule, []uint64, []uint64) {})
	}
	first, err := c.linkModule(stubs, nil)
	if err != nil {
		t.Fatalf("bind 1: %v", err)
	}
	second, err := c.linkModule(stubs, nil)
	if err != nil {
		t.Fatalf("bind 2: %v", err)
	}
	if first != c || second != c {
		t.Fatalf("binding changed compiled image: %p %p owner=%p", first, second, c)
	}
}
