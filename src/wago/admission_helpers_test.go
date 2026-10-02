//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"testing"
)

func admissionModule(t *testing.T, encoded string) *Instance {
	t.Helper()
	data, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Compile(nil, data)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	inst, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	t.Cleanup(func() { inst.Close() })
	return inst
}

func invokeI32(t *testing.T, inst *Instance, export string) int32 {
	t.Helper()
	got, err := inst.Invoke(export)
	if err != nil {
		t.Fatalf("%s: %v", export, err)
	}
	return AsI32(got[0])
}
