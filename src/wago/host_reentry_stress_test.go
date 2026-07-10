//go:build !tinygo

package wago

import (
	gruntime "runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// TestNestedHostReentrySurvivesGCAndTrap exercises two live native activations
// for one instance: outer wasm -> Go host -> inner wasm. The inner trap must
// unwind only the nested activation; the host callback and outer wasm call then
// continue with intact control frames and foreign-stack state.
func TestNestedHostReentrySurvivesGCAndTrap(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	sig := wasmtest.FuncType(i32, i32)
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("reenter")...), 0x00, 0x00)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("inner", 0, 1),
			wasmtest.ExportEntry("outer", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			// inner(x): trap when x==0, otherwise x+1.
			wasmtest.Code([]byte{0x20, 0x00, 0x45, 0x04, 0x40, 0x00, 0x0b, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}),
			// outer(x): reenter(x)+2.
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x41, 0x02, 0x6a, 0x0b}),
		)),
	)
	c := MustCompile(mod)
	var in *Instance
	hostCalls, nestedTraps := 0, 0
	var err error
	in, err = Instantiate(c, InstantiateOptions{Imports: Imports{"env.reenter": HostFunc(func(_ HostModule, p, r []uint64) {
		hostCalls++
		gruntime.GC()
		out, callErr := in.Invoke("inner", p[0])
		if callErr != nil {
			nestedTraps++
			r[0] = I32(40)
			return
		}
		r[0] = out[0]
	})}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	for i := 0; i < 100; i++ {
		out, err := in.Invoke("outer", I32(5))
		if err != nil || AsI32(out[0]) != 8 {
			t.Fatalf("outer(5) iteration %d = %v, %v; want 8", i, out, err)
		}
		out, err = in.Invoke("outer", I32(0))
		if err != nil || AsI32(out[0]) != 42 {
			t.Fatalf("outer(0) iteration %d = %v, %v; want recovered 42", i, out, err)
		}
	}
	if hostCalls != 200 || nestedTraps != 100 {
		t.Fatalf("host calls/traps = %d/%d, want 200/100", hostCalls, nestedTraps)
	}
}
