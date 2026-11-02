//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/hex"
	"runtime"
	"strings"
	"testing"
)

// An externref global holding a converted GC reference must be readable and
// writable through the host API, consistently with externref results.
func TestExternrefGlobalHoldingConvertedGCReference(t *testing.T) {
	ctx := context.Background()
	encoded := externGlobalHostWasm
	if runtime.GOOS == "darwin" {
		data, err := hex.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		c, err := Compile(nil, data)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c, InstantiateOptions{})
		if in != nil {
			in.Close()
			t.Fatal("converted initializer published an instance")
		}
		if err == nil || !strings.Contains(err.Error(), "opaque converted publication is unsupported on this target") {
			t.Fatalf("converted initializer rejection: %v", err)
		}
		// The non-Linux adapter rejects converted initialization. Start with null
		// to exercise safe results and reject-before-write public ingress below.
		const initializedGlobal = "060b016f014107fb0000fb1b0b"
		if strings.Count(encoded, initializedGlobal) != 1 {
			t.Fatal("global fixture changed")
		}
		encoded = strings.Replace(encoded, initializedGlobal, "0606016f01d06f0b", 1)
	}
	inst := admissionModule(t, encoded)
	g, err := inst.ExportedGlobalObject("g")
	if err != nil {
		t.Fatal(err)
	}
	v, err := g.GetValue()
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if runtime.GOOS == "darwin" {
		if got := invokeI32(t, inst, "isnull"); got != 1 {
			t.Fatalf("initial global isnull = %d", got)
		}
		values, err := inst.InvokeValues(ctx, "make", ValueI32(7))
		if err != nil || len(values) != 1 {
			t.Fatalf("make(7) = %v, %v", values, err)
		}
		v = values[0]
	}
	out, err := inst.InvokeValues(ctx, "roundtrip", v)
	if err != nil || AsI32(out[0].Bits()) != 7 {
		t.Fatalf("roundtrip(GetValue) = %v, %v; want 7", out, err)
	}
	made, err := inst.InvokeValues(ctx, "make", ValueI32(42))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" {
		before, err := g.GetValue()
		if err != nil {
			t.Fatal(err)
		}
		if err := g.SetValue(made[0]); err == nil || !strings.Contains(err.Error(), "opaque converted publication is unsupported on this target") {
			t.Fatalf("SetValue converted rejection: %v", err)
		}
		after, err := g.GetValue()
		if err != nil || after != before {
			t.Fatalf("rejected SetValue changed global: %v -> %v, %v", before, after, err)
		}
		out, err := inst.InvokeValues(ctx, "roundtrip", made[0])
		if err != nil || len(out) != 1 || AsI32(out[0].Bits()) != 42 {
			t.Fatalf("roundtrip(make) = %v, %v; want 42", out, err)
		}
	} else {
		if err := g.SetValue(made[0]); err != nil {
			t.Fatalf("SetValue: %v", err)
		}
		if got := invokeI32(t, inst, "readg"); got != 42 {
			t.Fatalf("readg after SetValue = %d, want 42", got)
		}
	}
	if err := g.SetValue(ValueExternRef(NullExternRef())); err != nil {
		t.Fatalf("SetValue(null): %v", err)
	}
	if got := invokeI32(t, inst, "isnull"); got != 1 {
		t.Fatalf("isnull after null SetValue = %d, want 1", got)
	}
}

// externGlobalHostWasm encodes:
//
//	(module
//	  (type $s (struct (field i32)))
//	  (global $g (export "g") (mut externref) (extern.convert_any (struct.new $s (i32.const 7))))
//	  (func (export "make") (param i32) (result externref) (extern.convert_any (struct.new $s (local.get 0))))
//	  (func (export "readg") (result i32) (struct.get $s 0 (ref.cast (ref $s) (any.convert_extern (global.get $g)))))
//	  (func (export "isnull") (result i32) (ref.is_null (global.get $g)))
//	  (func (export "roundtrip") (param externref) (result i32)
//	    (struct.get $s 0 (ref.cast (ref $s) (any.convert_extern (local.get 0))))))
const externGlobalHostWasm = "0061736d010000000113045f017f0060017f016f6000017f60016f017f03050401020203060b016f014107fb0000fb1b0b07290501670300046d616b65000005726561646700010669736e756c6c000209726f756e647472697000030a2d0409002000fb0000fb1b0b0d002300fb1afb1600fb0200000b05002300d10b0d002000fb1afb1600fb0200000b0011046e616d65040401000173070401000167"
